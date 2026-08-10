package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// createUser subscribes a user to the default plan, which is how every other
// user-scoped endpoint gets something to operate on.
func createUser(t *testing.T, username string) {
	t.Helper()
	got := do(t, http.MethodPut, "/v1/users/"+username, "")
	if got.status != http.StatusOK {
		t.Fatalf("unable to create the test user: status %d, body %s", got.status, got.body)
	}
}

// Terrain calls PUT /v1/users/{username} indirectly through its subscription
// routes, and GET /v1/users/{username}/plan directly for /terrain/qms/user/plan.
func TestUserSubscriptionEndpoints(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	t.Run("get current subscription details", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/users/"+testUser+"/plan", "")
		assertGolden(t, "user_plan_basic", got, http.StatusOK)
	})

	t.Run("list user subscriptions", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/users/"+testUser+"/subscriptions", "")
		assertGolden(t, "user_subscriptions_basic", got, http.StatusOK)
	})

	// Reading the plan of a user QMS has never seen creates that user and
	// subscribes them to the default plan, so this GET has a write side
	// effect and returns 200 rather than 404. Terrain relies on it: a user
	// who has never touched QMS still gets a plan back on first login.
	t.Run("previously unknown user is created on read", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/users/nosuchuser"+UsernameSuffix+"/plan", "")
		assertGolden(t, "user_plan_autocreated", got, http.StatusOK)
	})
}

// PUT /v1/users/{username}/{plan_name} backs both the admin and the
// service-account plan-change routes in terrain. The query parameters are the
// ones terrain forwards.
func TestUpdateSubscription(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	got := do(t, http.MethodPut, "/v1/users/"+testUser+"/Pro?paid=true&periods=1", "")
	assertGolden(t, "user_update_subscription_pro", got, http.StatusOK)

	t.Run("subscription details reflect the new plan", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/users/"+testUser+"/plan", "")
		assertGolden(t, "user_plan_pro", got, http.StatusOK)
	})
}

// POST /v1/users/{username}/plan/{resource-type}/quota is the admin quota
// override. Note it addresses the resource type by name, unlike the
// subscriptions service, which requires UUIDs.
func TestUpdateCurrentSubscriptionQuota(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	testCases := []struct {
		name         string
		golden       string
		resourceType string
		body         string
		wantStatus   int
	}{
		{
			name:         "set a cpu.hours quota",
			golden:       "quota_update_cpu_hours",
			resourceType: "cpu.hours",
			body:         `{"quota": 500}`,
			wantStatus:   http.StatusOK,
		},
		{
			name:         "unknown resource type",
			golden:       "quota_update_unknown_resource_type",
			resourceType: "no.such.resource",
			body:         `{"quota": 500}`,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "missing quota value",
			golden:       "quota_update_missing_quota",
			resourceType: "cpu.hours",
			body:         `{}`,
			wantStatus:   http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/v1/users/%s/plan/%s/quota", testUser, tc.resourceType)
			assertGolden(t, tc.golden, do(t, http.MethodPost, path, tc.body), tc.wantStatus)
		})
	}
}

// POST /v1/usages is what terrain's admin usage route calls, and
// GET /v1/usages/{username} backs /terrain/qms/user/usages.
func TestUsageEndpoints(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	t.Run("record a usage", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"username": %q, "resource_name": "cpu.hours", "usage_value": 12.5, "update_type": "SET", "metadata": "{}"}`,
			testUser,
		)
		assertGolden(t, "usage_add_set", do(t, http.MethodPost, "/v1/usages", body), http.StatusOK)
	})

	t.Run("read it back", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/usages/"+testUser, "")
		assertGolden(t, "usage_list_after_set", got, http.StatusOK)
	})

	t.Run("add to the usage", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"username": %q, "resource_name": "cpu.hours", "usage_value": 2.5, "update_type": "ADD", "metadata": "{}"}`,
			testUser,
		)
		assertGolden(t, "usage_add_add", do(t, http.MethodPost, "/v1/usages", body), http.StatusOK)
	})

	t.Run("usage reflects both updates", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/usages/"+testUser, "")
		assertGolden(t, "usage_list_after_add", got, http.StatusOK)
	})

	// An update type the service doesn't implement is bad input, so it has to be
	// a 400. It used to be a 500: the operation lookup never failed (it ignored
	// the requested name), so the request fell through to a formatted error that
	// httpStatusCode, which compares sentinels with ==, could not classify.
	t.Run("an unknown update type is refused", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"username": %q, "resource_name": "cpu.hours", "usage_value": 1, "update_type": "MULTIPLY", "metadata": "{}"}`,
			testUser,
		)
		assertGolden(t, "usage_add_bad_update_type", do(t, http.MethodPost, "/v1/usages", body), http.StatusBadRequest)
	})
}

// The bulk subscription endpoints. POST is the one terrain's admin
// /subscriptions route calls; GET backs the paginated admin listing.
func TestBulkSubscriptionEndpoints(t *testing.T) {
	resetDB(t)

	t.Run("create subscriptions", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"subscriptions": [{"username": %q, "plan_name": "Pro", "paid": true, "periods": 1}]}`,
			testUser,
		)
		got := do(t, http.MethodPost, "/v1/subscriptions?force=true", body)
		assertGolden(t, "subscriptions_bulk_create", got, http.StatusOK)
	})

	t.Run("list subscriptions", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/subscriptions?offset=0&limit=50&sort-field=username&sort-dir=asc", "")
		assertGolden(t, "subscriptions_list", got, http.StatusOK)
	})

	t.Run("list with a search term that matches nothing", func(t *testing.T) {
		got := do(t, http.MethodGet, "/v1/subscriptions?offset=0&limit=50&search=nomatch", "")
		assertGolden(t, "subscriptions_list_empty", got, http.StatusOK)
	})
}

// createUser checks only the status of PUT /v1/users/{username}; this pins the
// response body, which terrain sees when it subscribes a user indirectly.
func TestAddUserResponse(t *testing.T) {
	resetDB(t)
	assertGolden(t, "v1_user_added",
		do(t, http.MethodPut, "/v1/users/"+testUser, ""), http.StatusOK)

	stored := queryInt(t, `SELECT count(*) FROM users WHERE username = $1`, trimmedUser)
	if stored != 1 {
		t.Errorf("users rows = %d, want 1", stored)
	}
}
