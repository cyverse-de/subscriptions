package apitest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The behaviors pinned here are the ones the subscriptions service implements
// differently. Responses alone don't capture them, so these assert database
// state. When QMS is merged into subscriptions, exactly one implementation of
// each survives, and these tests say which one that has to be.

// recordV1Usage posts a usage update through the /v1 API and fails the test if it isn't accepted.
func recordV1Usage(t *testing.T, username, resource, updateType string, value float64) {
	t.Helper()
	body := fmt.Sprintf(
		`{"username": %q, "resource_name": %q, "usage_value": %v, "update_type": %q, "metadata": "{}"}`,
		username, resource, value, updateType,
	)
	got := do(t, http.MethodPost, "/v1/usages", body)
	if got.status != http.StatusOK {
		t.Fatalf("recording usage failed: status %d, body %s", got.status, got.body)
	}
}

// QMS writes an audit row to `updates` for every usage change, alongside the
// running total in `usages`. The subscriptions service computes the total in
// SQL and writes no `updates` row on its usage path, so a merge that keeps the
// wrong implementation loses the audit trail without any visible API change.
func TestUsageWritesAnUpdatesRow(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)
	recordV1Usage(t, testUser, "cpu.hours", "ADD", 5)

	updateCount := queryInt(t, `
		SELECT count(*)
		  FROM updates u
		  JOIN users ON u.user_id = users.id
		 WHERE users.username = $1`, "testuser")
	if updateCount != 2 {
		t.Errorf("updates rows = %d, want 2 (one per usage change)", updateCount)
	}

	// Each row records the operation that was actually requested. This used to
	// say ADD for both, because the lookup passed the name on the destination
	// struct and GORM's First only filters on the primary key -- so it returned
	// whichever operation sorted first regardless of what was asked for.
	operations := queryString(t, `
		SELECT string_agg(o.name, ',' ORDER BY u.effective_date)
		  FROM updates u
		  JOIN users ON u.user_id = users.id
		  JOIN update_operations o ON u.update_operation_id = o.id
		 WHERE users.username = $1`, "testuser")
	if operations != "SET,ADD" {
		t.Errorf("recorded operations = %q, want \"SET,ADD\"", operations)
	}

	usage := queryString(t, `
		SELECT us.usage::text
		  FROM usages us
		  JOIN subscriptions s ON us.subscription_id = s.id
		  JOIN users ON s.user_id = users.id
		 WHERE users.username = $1`, "testuser")
	if usage != "15" {
		t.Errorf("running usage total = %s, want 15", usage)
	}
}

// subscribe posts a single subscription request and returns the one result.
func subscribe(t *testing.T, username, planName string, force bool) map[string]any {
	t.Helper()
	body := fmt.Sprintf(
		`{"subscriptions": [{"username": %q, "plan_name": %q, "paid": true, "periods": 1}]}`,
		username, planName,
	)
	got := do(t, http.MethodPost, fmt.Sprintf("/v1/subscriptions?force=%t", force), body)
	if got.status != http.StatusOK {
		t.Fatalf("subscribing %s to %s failed: status %d, body %s", username, planName, got.status, got.body)
	}

	results, ok := mustDecode(t, got)["result"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected exactly one subscription result, got: %s", got.body)
	}
	result := results[0].(map[string]any)

	if reason, failed := result["failure_reason"].(string); failed {
		t.Fatalf("subscribing %s to %s reported a failure: %s", username, planName, reason)
	}
	return result
}

// activeSubscriptions matches the subscriptions in effect right now. It has to
// agree with the service's own activeAsOf predicate, open-ended subscriptions
// included, or these tests would silently stop seeing the rows they check.
const activeSubscriptions = `
	  FROM subscriptions s
	  JOIN users ON s.user_id = users.id
	  JOIN plans p ON p.id = s.plan_id
	 WHERE users.username = $1
	   AND s.effective_start_date <= CURRENT_TIMESTAMP
	   AND (s.effective_end_date IS NULL OR s.effective_end_date >= CURRENT_TIMESTAMP)`

// subscribeForPeriod force-subscribes a user for an explicit period, which is
// how a subscription that starts in the future gets created.
func subscribeForPeriod(t *testing.T, username, planName string, start, end time.Time) {
	t.Helper()
	body := fmt.Sprintf(
		`{"subscriptions": [{"username": %q, "plan_name": %q, "paid": true, "start_date": %q, "end_date": %q}]}`,
		username, planName, start.Format(time.RFC3339), end.Format(time.RFC3339),
	)
	got := do(t, http.MethodPost, "/v1/subscriptions?force=true", body)
	if got.status != http.StatusOK {
		t.Fatalf("scheduling %s for %s failed: status %d, body %s", planName, username, got.status, got.body)
	}
}

// activePlanFor returns the name of the plan the user is subscribed to now.
// Every active plan is aggregated rather than one being picked arbitrarily, so
// a test expecting a single plan fails loudly — naming both plans — if the
// service ever leaves two subscriptions running at once.
func activePlanFor(t *testing.T, username string) string {
	t.Helper()
	return queryString(t, `
		SELECT coalesce(string_agg(p.name, ',' ORDER BY s.effective_start_date DESC), '')`+
		activeSubscriptions, username)
}

// insertOpenEndedSubscription creates a subscription with no effective end
// date, the shape legacy QMS rows have. It's written directly because the API
// always supplies an end date and can no longer produce one.
func insertOpenEndedSubscription(t *testing.T, username, planName string) {
	t.Helper()

	_, err := testDB.Exec(
		`INSERT INTO users (username) VALUES ($1) ON CONFLICT (username) DO NOTHING`, username,
	)
	if err != nil {
		t.Fatalf("unable to create the user: %s", err)
	}

	result, err := testDB.Exec(`
		INSERT INTO subscriptions (user_id, plan_id, plan_rate_id, effective_start_date, effective_end_date)
		SELECT u.id, p.id, r.id, CURRENT_TIMESTAMP - interval '1 day', NULL
		  FROM users u
		  JOIN plans p ON p.name = $2
		  JOIN plan_rates r ON r.plan_id = p.id
		 WHERE u.username = $1
		 ORDER BY r.effective_date DESC
		 LIMIT 1`, username, planName)
	if err != nil {
		t.Fatalf("unable to insert an open-ended subscription: %s", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("open-ended subscription insert affected %d rows, want 1 (err: %v)", rows, err)
	}
}

// Without force, a subscription request only takes effect if it raises the
// user's cpu.hours allocation, so an admin re-running a bulk subscription
// can't silently downgrade anyone. The subscriptions service compares plan
// *names* instead and would downgrade in either direction, so this is the
// semantic the merge has to keep.
func TestUnforcedSubscriptionChangeOnlyUpgrades(t *testing.T) {
	testCases := []struct {
		name           string
		startingPlan   string
		requestedPlan  string
		wantNew        bool
		wantEndingPlan string
	}{
		{
			name:           "a larger plan is an upgrade",
			startingPlan:   "Basic",
			requestedPlan:  "Pro",
			wantNew:        true,
			wantEndingPlan: "Pro",
		},
		{
			name:           "a smaller plan is refused",
			startingPlan:   "Pro",
			requestedPlan:  "Basic",
			wantNew:        false,
			wantEndingPlan: "Pro",
		},
		{
			name:           "the same plan is refused",
			startingPlan:   "Pro",
			requestedPlan:  "Pro",
			wantNew:        false,
			wantEndingPlan: "Pro",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resetDB(t)

			// Establish the starting subscription with force, so only the
			// request under test exercises the comparison rule.
			subscribe(t, testUser, tc.startingPlan, true)

			result := subscribe(t, testUser, tc.requestedPlan, false)
			if newSubscription, _ := result["new_subscription"].(bool); newSubscription != tc.wantNew {
				t.Errorf("new_subscription = %v, want %v", newSubscription, tc.wantNew)
			}

			// Whatever the response says, the active subscription in the
			// database is what the rest of the DE reads.
			if active := activePlanFor(t, trimmedUser); active != tc.wantEndingPlan {
				t.Errorf("active plan = %q, want %q", active, tc.wantEndingPlan)
			}
		})
	}
}

// A user QMS has never subscribed has no allocation to compare against, so an
// unforced request has to create the subscription rather than treat the
// missing one as a reason to refuse.
func TestUnforcedSubscriptionForNewUser(t *testing.T) {
	resetDB(t)

	result := subscribe(t, testUser, "Pro", false)
	if newSubscription, _ := result["new_subscription"].(bool); !newSubscription {
		t.Error("new_subscription = false, want true for a user with no prior subscription")
	}
	if active := activePlanFor(t, trimmedUser); active != "Pro" {
		t.Errorf("active plan = %q, want \"Pro\"", active)
	}
}

// A subscription with no effective end date runs indefinitely, so it is still
// active and an unforced request for a lesser plan has to be refused. Nothing
// else covers the null-end-date branch of the active-subscription lookup: the
// API always writes an end date, so a merge could drop that branch and every
// other test would stay green while legacy subscriptions stopped counting.
func TestUnforcedChangeSeesAnOpenEndedSubscription(t *testing.T) {
	resetDB(t)
	insertOpenEndedSubscription(t, trimmedUser, "Pro")

	result := subscribe(t, testUser, "Basic", false)
	if newSubscription, _ := result["new_subscription"].(bool); newSubscription {
		t.Error("new_subscription = true, want false: the open-ended subscription is still active")
	}
	if active := activePlanFor(t, trimmedUser); active != "Pro" {
		t.Errorf("active plan = %q, want \"Pro\"", active)
	}
}

// Upgrading over an open-ended subscription has to close it. The deactivation
// queries compare against the effective end date, and every such comparison is
// false for null, so an open-ended subscription survives unless it's handled
// explicitly — leaving the user on two plans at once.
func TestUnforcedUpgradeClosesAnOpenEndedSubscription(t *testing.T) {
	resetDB(t)
	insertOpenEndedSubscription(t, trimmedUser, "Basic")

	subscribe(t, testUser, "Pro", false)

	if active := activePlanFor(t, trimmedUser); active != "Pro" {
		t.Errorf("active plan = %q, want \"Pro\" (both plans listed means neither was deactivated)", active)
	}
}

// An unforced request covers a whole period, not just its first instant, so a
// better subscription scheduled to start later in that period still counts as
// the plan to beat. Comparing only against the subscription in effect on the
// start date would find nothing and let the request cancel the scheduled one.
func TestUnforcedChangeDoesNotCancelALaterSubscription(t *testing.T) {
	resetDB(t)

	// A paid upgrade scheduled to start months from now.
	start := time.Now().AddDate(0, 5, 0)
	end := start.AddDate(0, 3, 0)
	subscribeForPeriod(t, testUser, "Pro", start, end)

	result := subscribe(t, testUser, "Basic", false)
	if newSubscription, _ := result["new_subscription"].(bool); newSubscription {
		t.Error("new_subscription = true, want false: a better subscription is already scheduled")
	}

	// A cancelled subscription is collapsed to zero length rather than deleted.
	scheduled := queryInt(t, `
		SELECT count(*)
		  FROM subscriptions s
		  JOIN users ON s.user_id = users.id
		  JOIN plans p ON p.id = s.plan_id
		 WHERE users.username = $1
		   AND p.name = 'Pro'
		   AND s.effective_end_date > s.effective_start_date`, trimmedUser)
	if scheduled != 1 {
		t.Errorf("intact scheduled Pro subscriptions = %d, want 1", scheduled)
	}
}

// A request that can't be satisfied is reported per item, in a 200 response,
// rather than as an HTTP error for the whole batch. Terrain's admin route
// parses each result out of the body, so the status and the failure_reason
// field are both part of the contract.
func TestBulkSubscriptionFailuresAreReportedPerItem(t *testing.T) {
	resetDB(t)

	body := fmt.Sprintf(
		`{"subscriptions": [{"username": %q, "plan_name": "No Such Plan", "paid": true, "periods": 1}]}`,
		testUser,
	)
	got := do(t, http.MethodPost, "/v1/subscriptions?force=true", body)
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 even though the item failed: %s", got.status, got.body)
	}

	results, ok := mustDecode(t, got)["result"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected exactly one subscription result, got: %s", got.body)
	}
	reason, failed := results[0].(map[string]any)["failure_reason"].(string)
	if !failed {
		t.Fatalf("failure_reason is missing from the failed result: %s", got.body)
	}
	if !strings.Contains(reason, "No Such Plan") {
		t.Errorf("failure_reason = %q, want it to name the plan that doesn't exist", reason)
	}
}

// The reads that answer "what is this user on right now" have to filter by the
// active-subscription predicate, not just take the most recently starting row.
// A subscription scheduled to start later sorts first by effective start date,
// so dropping that filter would surface the future plan — and every other test
// here would stay green, because they never give a user a subscription that
// hasn't started.
func TestCurrentPlanIgnoresASubscriptionThatHasNotStarted(t *testing.T) {
	resetDB(t)

	// The bulk endpoint stores the username verbatim while the user-scoped
	// routes trim the suffix, so this user is named without one to keep both
	// halves of the test pointed at the same row.
	const username = "testuser"

	subscribe(t, username, "Basic", true)
	start := time.Now().AddDate(0, 5, 0)
	subscribeForPeriod(t, username, "Pro", start, start.AddDate(0, 3, 0))

	// GET /v1/users/{username}/plan backs terrain's /terrain/qms/user/plan.
	t.Run("the user's current subscription", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/users/"+username+"/plan", "")
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, body %s", got.status, got.body)
		}

		result, ok := mustDecode(t, got)["result"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected response shape: %s", got.body)
		}
		plan, _ := result["plan"].(map[string]any)
		if name, _ := plan["name"].(string); name != "Basic" {
			t.Errorf("current plan = %q, want \"Basic\": the Pro subscription hasn't started", name)
		}
	})

	// GET /v1/subscriptions backs the paginated admin listing.
	t.Run("the admin subscription listing", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/subscriptions?offset=0&limit=50", "")
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, body %s", got.status, got.body)
		}

		result, ok := mustDecode(t, got)["result"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected response shape: %s", got.body)
		}
		listed, _ := result["subscriptions"].([]any)
		var names []string
		for _, subscription := range listed {
			plan, _ := subscription.(map[string]any)["plan"].(map[string]any)
			name, _ := plan["name"].(string)
			names = append(names, name)
		}
		if len(names) != 1 || names[0] != "Basic" {
			t.Errorf("listed plans = %v, want [Basic]: the Pro subscription hasn't started", names)
		}
	})
}

// Creating an overlapping subscription deactivates the previous one, so a user
// never has two active subscriptions at once. Anything reading "the current
// plan" depends on that invariant holding.
func TestOverlappingSubscriptionsAreDeactivated(t *testing.T) {
	resetDB(t)

	for _, plan := range []string{"Basic", "Pro", "Commercial"} {
		body := fmt.Sprintf(
			`{"subscriptions": [{"username": %q, "plan_name": %q, "paid": true, "periods": 1}]}`,
			testUser, plan,
		)
		if got := do(t, http.MethodPost, "/v1/subscriptions?force=true", body); got.status != http.StatusOK {
			t.Fatalf("unable to subscribe to %s: %s", plan, got.body)
		}
	}

	activeCount := queryInt(t, `SELECT count(*)`+activeSubscriptions, trimmedUser)
	if activeCount != 1 {
		t.Errorf("active subscriptions = %d, want exactly 1", activeCount)
	}

	totalCount := queryInt(t, `
		SELECT count(*)
		  FROM subscriptions s
		  JOIN users ON s.user_id = users.id
		 WHERE users.username = $1`, trimmedUser)
	if totalCount != 3 {
		t.Errorf("total subscriptions = %d, want 3 (the history is retained)", totalCount)
	}
}

// Both ways of subscribing a user have to store the same username. The bulk
// endpoint takes the name from the request body and used to store it verbatim,
// while PUT /v1/users/{username} and the usage endpoints trim the configured
// suffix -- so an admin bulk-subscribing "user@domain" created a second user
// row that the user's own /terrain/qms/user/plan never saw.
//
// Terrain reaches both paths: POST /terrain/admin/qms/subscriptions is the bulk
// endpoint, while /terrain/qms/user/* are the trimming ones.
func TestBulkSubscriptionsTrimTheUsernameSuffix(t *testing.T) {
	resetDB(t)

	body := fmt.Sprintf(
		`{"subscriptions": [{"username": %q, "plan_name": "Pro", "paid": true, "periods": 1}]}`,
		testUser,
	)
	if got := do(t, http.MethodPost, "/v1/subscriptions?force=true", body); got.status != http.StatusOK {
		t.Fatalf("unable to create the subscription: %s", got.body)
	}

	// The suffixed name is not stored.
	suffixed := queryInt(t, `SELECT count(*) FROM users WHERE username = $1`, testUser)
	if suffixed != 0 {
		t.Errorf("users rows named %q = %d, want 0", testUser, suffixed)
	}

	// Reaching the same person through a trimming endpoint finds the same row
	// rather than creating a second one.
	createUser(t, testUser)
	total := queryInt(t, `SELECT count(*) FROM users`)
	if total != 1 {
		t.Errorf("total users = %d, want 1 (both endpoints address one person)", total)
	}

	subs := queryInt(t, `
		SELECT count(*) FROM subscriptions s JOIN users u ON s.user_id = u.id
		 WHERE u.username = $1`, "testuser")
	if subs < 1 {
		t.Errorf("subscriptions on the trimmed user = %d, want at least 1", subs)
	}
}

// QMS resolves the resource type by name against whatever subscription is
// currently active. The subscriptions service requires the caller to supply
// both the subscription and resource-type UUIDs, so terrain's admin quota
// route only works against this implementation.
func TestQuotaUpdateResolvesResourceTypeByName(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	path := fmt.Sprintf("/v1/users/%s/plan/cpu.hours/quota", testUser)
	if got := do(t, http.MethodPost, path, `{"quota": 750}`); got.status != http.StatusOK {
		t.Fatalf("unable to set the quota: %s", got.body)
	}

	quota := queryString(t, `
		SELECT q.quota::text
		  FROM quotas q
		  JOIN subscriptions s ON q.subscription_id = s.id
		  JOIN users ON s.user_id = users.id
		  JOIN resource_types rt ON q.resource_type_id = rt.id
		 WHERE users.username = $1
		   AND rt.name = 'cpu.hours'`, "testuser")
	if quota != "750" {
		t.Errorf("cpu.hours quota = %s, want 750", quota)
	}
}
