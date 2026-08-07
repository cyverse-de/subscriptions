package apitest

import (
	"net/http"
	"testing"
)

// PUT /users subscribes a named user to a plan. The username comes from the
// request body, since the route declares no path parameters.
func TestAddUser(t *testing.T) {
	testCases := []struct {
		name string
		// wantUsers is how many rows the call leaves behind for each username,
		// keyed by name. A blank key checks for the empty-username row the
		// handler used to create.
		wantUsers  map[string]int
		golden     string
		body       string
		wantStatus int
	}{
		{
			name:       "subscribes the requested user",
			golden:     "add_user_created",
			body:       `{"username": "brandnewuser", "planName": "Basic", "paid": true, "periods": 1}`,
			wantStatus: http.StatusOK,
			wantUsers:  map[string]int{"brandnewuser": 1, "": 0},
		},
		{
			// The suffix is stripped, so a fully qualified name addresses the
			// same person as the bare one.
			name:       "strips the username suffix",
			golden:     "add_user_created_suffixed",
			body:       `{"username": "brandnewuser` + UsernameSuffix + `", "planName": "Basic", "paid": true, "periods": 1}`,
			wantStatus: http.StatusOK,
			wantUsers:  map[string]int{"brandnewuser": 1, "": 0},
		},
		{
			name:       "a missing username is refused",
			golden:     "add_user_no_username",
			body:       `{"planName": "Basic", "paid": true, "periods": 1}`,
			wantStatus: http.StatusBadRequest,
			wantUsers:  map[string]int{"": 0},
		},
		{
			// A missing or unknown plan used to reach SetActiveSubscription
			// with a nil plan and panic there (db/userplans.go:125), which
			// middleware.Recover turned into a 500. Note the wire name is
			// "planName", not "plan_name", so a caller sending "plan_name"
			// lands in this case.
			name:       "a missing plan name is refused",
			golden:     "add_user_no_plan",
			body:       `{"username": "brandnewuser"}`,
			wantStatus: http.StatusBadRequest,
			wantUsers:  map[string]int{"brandnewuser": 0},
		},
		{
			name:       "an unknown plan name is refused",
			golden:     "add_user_unknown_plan",
			body:       `{"username": "brandnewuser", "planName": "No Such Plan", "paid": true, "periods": 1}`,
			wantStatus: http.StatusBadRequest,
			wantUsers:  map[string]int{"brandnewuser": 0},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resetDB(t)
			assertGolden(t, tc.golden, do(t, http.MethodPut, "/users", tc.body), tc.wantStatus)

			for username, want := range tc.wantUsers {
				got := queryInt(t, `SELECT count(*) FROM users WHERE username = $1`, username)
				if got != want {
					t.Errorf("users rows named %q = %d, want %d", username, got, want)
				}
			}
		})
	}
}

// The created user is actually subscribed, not just inserted — the point of the
// endpoint is the subscription, and a blank username used to get it instead.
func TestAddUserCreatesTheSubscription(t *testing.T) {
	resetDB(t)

	body := `{"username": "brandnewuser", "planName": "Pro", "paid": true, "periods": 1}`
	if got := do(t, http.MethodPut, "/users", body); got.status != http.StatusOK {
		t.Fatalf("status %d, body %s", got.status, got.body)
	}

	plan := queryString(t, `
		SELECT p.name
		  FROM subscriptions s
		  JOIN users u ON s.user_id = u.id
		  JOIN plans p ON p.id = s.plan_id
		 WHERE u.username = $1`, "brandnewuser")
	if plan != "Pro" {
		t.Errorf("subscribed plan = %q, want \"Pro\"", plan)
	}
}
