package apitest

import (
	"net/http"
	"testing"
)

// KNOWN BUG, recorded deliberately: PUT /users reads the username from a path
// parameter the route doesn't declare.
//
//	app.Router.PUT("/users", app.AddUserHTTPHandler)
//	...
//	request.Username = c.Param("username")
//
// There is no :username in that path, so Param returns "" and overwrites
// whatever the body supplied. The endpoint cannot create the user it was asked
// to create. Captured as-is so the merge can't change it by accident; fix it as
// its own change, with these goldens updated in the same commit.
//
// QMS serves the working version of this operation at PUT /v1/users/{username},
// which is why nothing has noticed.
func TestAddUserCannotCreateTheRequestedUser(t *testing.T) {
	testCases := []struct {
		name string
		// wantBlankUsers is how many rows the call leaves behind under the
		// empty username, which is the damage the bug actually does.
		wantBlankUsers int
		golden         string
		body           string
		wantStatus     int
	}{
		{
			// A second bug, independent of the username one: an absent or
			// unknown plan name resolves to a nil plan that nothing checks, so
			// SetActiveSubscription dereferences it (db/userplans.go:125).
			// Only middleware.Recover keeps that from dropping the connection,
			// and the caller gets a 500 where a 400 belongs. Note the wire name
			// is "planName", not "plan_name".
			name:           "no plan name panics",
			golden:         "add_user_no_plan_panics",
			body:           `{"username": "brandnewuser"}`,
			wantStatus:     http.StatusInternalServerError,
			wantBlankUsers: 0,
		},
		{
			name:           "unknown plan name panics too",
			golden:         "add_user_unknown_plan_panics",
			body:           `{"username": "brandnewuser", "planName": "No Such Plan", "paid": true, "periods": 1}`,
			wantStatus:     http.StatusInternalServerError,
			wantBlankUsers: 0,
		},
		{
			name:           "a valid plan still creates the wrong user",
			golden:         "add_user_valid_plan",
			body:           `{"username": "brandnewuser", "planName": "Basic", "paid": true, "periods": 1}`,
			wantStatus:     http.StatusOK,
			wantBlankUsers: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resetDB(t)
			assertGolden(t, tc.golden, do(t, http.MethodPut, "/users", tc.body), tc.wantStatus)

			created := queryInt(t, `SELECT count(*) FROM users WHERE username = $1`, "brandnewuser")
			if created != 0 {
				t.Errorf("users rows named brandnewuser = %d, want 0 while the bug stands", created)
			}

			// When the call gets far enough to commit, the blank name isn't
			// just dropped, it's persisted and subscribed to a plan — so real
			// deployments are likely carrying a "" user. The panicking cases
			// leave nothing behind, because the deferred rollback still runs.
			blank := queryInt(t, `SELECT count(*) FROM users WHERE username = ''`)
			if blank != tc.wantBlankUsers {
				t.Errorf("users rows with an empty username = %d, want %d", blank, tc.wantBlankUsers)
			}
		})
	}
}
