package apitest

import (
	"net/http"
	"testing"
)

// A user with no subscription is the ordinary case for anyone the service has
// not seen yet, and the routes below all reach for their active subscription
// before doing anything else. Reaching for one that isn't there used to hand an
// empty string to a query comparing it against a uuid column, so PostgreSQL
// rejected the statement and the caller saw a 500 that said nothing about the
// cause. resource-usage-api reads GET /users/{username}/usages to decide whether
// a first data-usage reading exists, and could not tell "none recorded" apart
// from a genuine fault.

// newUser is a name no fixture creates, so the service has never seen it.
const newUser = "zzznewuser"

func TestRoutesCopeWithAUserWithoutASubscription(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "listing usages",
			method: http.MethodGet,
			path:   "/users/" + newUser + "/usages",
		},
		{
			name:   "recording a usage",
			method: http.MethodPut,
			path:   "/users/" + newUser + "/usages",
			body:   `{"resource_name": "cpu.hours", "usage_value": 10, "update_type": "SET"}`,
		},
		{
			name:   "a usage update",
			method: http.MethodPut,
			path:   "/user/" + newUser + "/updates",
			body:   updateBodyOfType("usages", "cpu.hours", "cpu hours", "SET", 10),
		},
		{
			name:   "a quota update",
			method: http.MethodPut,
			path:   "/user/" + newUser + "/updates",
			body:   updateBodyOfType("quotas", "cpu.hours", "cpu hours", "SET", 10),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resetDB(t)

			got := do(t, testCase.method, testCase.path, testCase.body)
			if got.status != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
			}
			if decoded := mustDecode(t, got); decoded["error"] != nil {
				t.Errorf("the response carries an error: %v", decoded["error"])
			}
		})
	}
}

// Listing usages reports that there are none rather than failing, which is the
// answer resource-usage-api needs to tell "no reading yet" from a server fault.
func TestUsagesForAUserWithoutASubscriptionAreEmpty(t *testing.T) {
	resetDB(t)

	got := do(t, http.MethodGet, "/users/"+newUser+"/usages", "")
	assertGolden(t, "usages_unknown_user", got, http.StatusOK)
}

// Reading usages does not subscribe anyone. The write paths enrol a user who
// has no subscription, but a GET that quietly created one would bill a plan to
// anybody whose name was typed into a URL.
func TestListingUsagesDoesNotCreateASubscription(t *testing.T) {
	resetDB(t)

	if got := do(t, http.MethodGet, "/users/"+newUser+"/usages", ""); got.status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
	}

	subscriptions := queryInt(t, `SELECT count(*) FROM subscriptions`)
	if subscriptions != 0 {
		t.Errorf("the request created %d subscription(s), want 0", subscriptions)
	}
}

// The write paths do enrol the user, so the usage they just recorded has a
// subscription to hang off and a later read can find it.
func TestRecordingUsageSubscribesAUserWithoutASubscription(t *testing.T) {
	testCases := []struct {
		name string
		path string
		body string
	}{
		{
			name: "through the usages route",
			path: "/users/" + newUser + "/usages",
			body: `{"resource_name": "cpu.hours", "usage_value": 10, "update_type": "SET"}`,
		},
		{
			name: "through the updates route",
			path: "/user/" + newUser + "/updates",
			body: updateBodyOfType("usages", "cpu.hours", "cpu hours", "SET", 10),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resetDB(t)

			if got := do(t, http.MethodPut, testCase.path, testCase.body); got.status != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
			}

			plan := queryString(t, `
				SELECT p.name
				  FROM subscriptions s
				  JOIN users u ON s.user_id = u.id
				  JOIN plans p ON s.plan_id = p.id
				 WHERE u.username = $1`, newUser)
			if plan != "Basic" {
				t.Errorf("the user was subscribed to the %s plan, want Basic", plan)
			}

			usage := queryFloat(t, `
				SELECT us.usage
				  FROM usages us
				  JOIN subscriptions s ON us.subscription_id = s.id
				  JOIN users u ON s.user_id = u.id
				 WHERE u.username = $1`, newUser)
			if usage != 10 {
				t.Errorf("the recorded usage is %v, want 10", usage)
			}
		})
	}
}
