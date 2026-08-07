package apitest

import (
	"net/http"
	"testing"
)

// PUT /user/{username}/updates is how resource-usage-api and data-usage-api
// record consumption. Both send the username in the path and leave the body's
// nested user object for the handler to fill in, so these tests pin that the
// path wins and that the running total lands in the usages table.
func TestAddUserUpdate(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	t.Run("record a usage", func(t *testing.T) {
		got := do(t, http.MethodPut, "/user/"+trimmedUser+"/updates",
			updateBody("cpu.hours", "cpu hours", "SET", 12.5))
		assertGolden(t, "update_set_cpu_hours", got, http.StatusOK)
	})

	t.Run("the running total is stored", func(t *testing.T) {
		usage := queryFloat(t, `
			SELECT us.usage
			  FROM usages us
			  JOIN subscriptions s ON us.subscription_id = s.id
			  JOIN users u ON s.user_id = u.id
			  JOIN resource_types rt ON us.resource_type_id = rt.id
			 WHERE u.username = $1 AND rt.name = 'cpu.hours'`, trimmedUser)
		if usage != 12.5 {
			t.Errorf("stored usage = %v, want 12.5", usage)
		}
	})

	t.Run("an ADD accumulates onto it", func(t *testing.T) {
		recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "ADD", 2.5)

		usage := queryFloat(t, `
			SELECT us.usage
			  FROM usages us
			  JOIN subscriptions s ON us.subscription_id = s.id
			  JOIN users u ON s.user_id = u.id
			  JOIN resource_types rt ON us.resource_type_id = rt.id
			 WHERE u.username = $1 AND rt.name = 'cpu.hours'`, trimmedUser)
		if usage != 15 {
			t.Errorf("stored usage = %v, want 15", usage)
		}
	})

	// This route writes an audit row per update, and — unlike QMS, which
	// records ADD for every operation because its lookup filters on the primary
	// key instead of the name — it records the operation that was actually
	// requested. The merge has to keep this implementation, not QMS's.
	t.Run("an audit row records each operation", func(t *testing.T) {
		operations := queryString(t, `
			SELECT string_agg(o.name, ',' ORDER BY up.created_at)
			  FROM updates up
			  JOIN users u ON up.user_id = u.id
			  JOIN update_operations o ON up.update_operation_id = o.id
			 WHERE u.username = $1`, trimmedUser)
		if operations != "SET,ADD" {
			t.Errorf("recorded operations = %q, want \"SET,ADD\"", operations)
		}
	})
}

// The path username is authoritative: whatever the body claims is discarded.
// Callers depend on this — resource-usage-api sends an Update whose user object
// it never populates.
func TestAddUserUpdateIgnoresTheBodyUsername(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	subscribeUser(t, "otheruser", "Basic")

	body := `{"update": {"value_type": "usages", "value": 7, "effective_date": "` + effectiveDate + `",` +
		`"resource_type": {"name": "cpu.hours", "unit": "cpu hours"},` +
		`"operation": {"name": "SET"}, "user": {"username": "otheruser"}}}`
	if got := do(t, http.MethodPut, "/user/"+trimmedUser+"/updates", body); got.status != http.StatusOK {
		t.Fatalf("status %d, body %s", got.status, got.body)
	}

	charged := queryFloat(t, `
		SELECT coalesce(sum(us.usage), 0)
		  FROM usages us
		  JOIN subscriptions s ON us.subscription_id = s.id
		  JOIN users u ON s.user_id = u.id
		 WHERE u.username = $1`, trimmedUser)
	if charged != 7 {
		t.Errorf("usage charged to the path user = %v, want 7", charged)
	}

	untouched := queryFloat(t, `
		SELECT coalesce(sum(us.usage), 0)
		  FROM usages us
		  JOIN subscriptions s ON us.subscription_id = s.id
		  JOIN users u ON s.user_id = u.id
		 WHERE u.username = $1`, "otheruser")
	if untouched != 0 {
		t.Errorf("usage charged to the body user = %v, want 0", untouched)
	}
}

// The suffix is stripped from the path username, so a fully qualified name and
// a bare one address the same person.
func TestAddUserUpdateStripsTheUsernameSuffix(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	recordUsage(t, testUser, "cpu.hours", "cpu hours", "SET", 3)

	rows := queryInt(t, `SELECT count(*) FROM users`)
	if rows != 1 {
		t.Errorf("users rows = %d, want 1: the suffixed name should resolve to the existing user", rows)
	}
}

// Bad input has to be rejected with a 4xx rather than a 500, because callers
// retry on 5xx. The body shapes here are the ones a caller can realistically
// produce.
func TestAddUserUpdateRejectsBadInput(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	testCases := []struct {
		name       string
		golden     string
		body       string
		wantStatus int
	}{
		{
			name:       "unknown resource type",
			golden:     "update_unknown_resource_type",
			body:       updateBody("no.such.resource", "cpu hours", "SET", 1),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown unit",
			golden:     "update_unknown_unit",
			body:       updateBody("cpu.hours", "no such unit", "SET", 1),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown operation",
			golden:     "update_unknown_operation",
			body:       updateBody("cpu.hours", "cpu hours", "MULTIPLY", 1),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing the nested update object",
			golden:     "update_missing_update",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing the nested user object",
			golden:     "update_missing_user",
			body:       `{"update": {"value": 1, "resource_type": {"name": "cpu.hours", "unit": "cpu hours"}}}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertGolden(t, tc.golden, do(t, http.MethodPut, "/user/"+trimmedUser+"/updates", tc.body), tc.wantStatus)
		})
	}
}
