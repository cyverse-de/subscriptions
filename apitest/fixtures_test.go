package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// testUser is the fully qualified username used throughout these tests. The
// handlers strip everything after the "@", which is what callers rely on when
// they forward a username from Keycloak.
const testUser = "testuser" + UsernameSuffix

// trimmedUser is what testUser becomes once the handlers are done with it, and
// therefore what actually lands in the users table.
const trimmedUser = "testuser"

// subscribeUser inserts a user subscribed to the named plan, with the quotas
// that plan's defaults imply. QMS owns the endpoints that create subscriptions
// today, so until the merge lands this service's tests build the fixture
// directly rather than through an API this service doesn't serve.
func subscribeUser(t *testing.T, username, planName string) {
	t.Helper()

	if _, err := testDB.Exec(
		`INSERT INTO users (username) VALUES ($1) ON CONFLICT (username) DO NOTHING`, username,
	); err != nil {
		t.Fatalf("unable to create the user: %s", err)
	}

	result, err := testDB.Exec(`
		INSERT INTO subscriptions (user_id, plan_id, plan_rate_id, effective_start_date, effective_end_date)
		SELECT u.id, p.id, r.id, CURRENT_TIMESTAMP - interval '1 day', CURRENT_TIMESTAMP + interval '1 year'
		  FROM users u
		  JOIN plans p ON p.name = $2
		  JOIN plan_rates r ON r.plan_id = p.id
		 WHERE u.username = $1
		 ORDER BY r.effective_date DESC
		 LIMIT 1`, username, planName)
	if err != nil {
		t.Fatalf("unable to create the subscription: %s", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("subscription insert affected %d rows, want 1 (err: %v)", rows, err)
	}

	// The quota values a plan implies, taking the most recent default that has
	// already taken effect for each resource type.
	if _, err := testDB.Exec(`
		INSERT INTO quotas (subscription_id, resource_type_id, quota)
		SELECT DISTINCT ON (pqd.resource_type_id) s.id, pqd.resource_type_id, pqd.quota_value
		  FROM subscriptions s
		  JOIN users u ON s.user_id = u.id
		  JOIN plan_quota_defaults pqd ON pqd.plan_id = s.plan_id
		 WHERE u.username = $1
		   AND pqd.effective_date <= CURRENT_TIMESTAMP
		 ORDER BY pqd.resource_type_id, pqd.effective_date DESC`, username,
	); err != nil {
		t.Fatalf("unable to create the quotas: %s", err)
	}
}

// setQuota overrides a single quota for the user's subscription, which is how a
// test puts someone into (or out of) overage.
func setQuota(t *testing.T, username, resourceName string, value float64) {
	t.Helper()
	if _, err := testDB.Exec(`
		UPDATE quotas
		   SET quota = $3
		  FROM subscriptions s, users u, resource_types rt
		 WHERE quotas.subscription_id = s.id
		   AND s.user_id = u.id
		   AND quotas.resource_type_id = rt.id
		   AND u.username = $1
		   AND rt.name = $2`, username, resourceName, value,
	); err != nil {
		t.Fatalf("unable to set the %s quota: %s", resourceName, err)
	}
}

// recordUsage sends a usage update the way resource-usage-api does, through
// PUT /user/{username}/updates, and fails the test if it isn't accepted.
func recordUsage(t *testing.T, username, resourceName, unit, operation string, value float64) {
	t.Helper()
	got := do(t, http.MethodPut, "/user/"+username+"/updates", updateBody(resourceName, unit, operation, value))
	if got.status != http.StatusOK {
		t.Fatalf("recording usage failed: status %d, body %s", got.status, got.body)
	}
}

// effectiveDate is a fixed timestamp so request bodies are reproducible. The
// wire form is RFC 3339, which is what ptypes.Timestamp marshals to now that
// these messages go over encoding/json rather than protojson.
const effectiveDate = "2026-01-15T12:00:00Z"

// updateBody renders the AddUpdateRequest body callers send, matching what
// resource-usage-api builds. The username is deliberately left out of the
// nested user object: the handler overwrites it from the path, and pinning that
// is part of the point.
func updateBody(resourceName, unit, operation string, value float64) string {
	return fmt.Sprintf(
		`{"update": {"value_type": "usages", "value": %v, "effective_date": %q,`+
			`"resource_type": {"name": %q, "unit": %q}, "operation": {"name": %q}, "user": {}}}`,
		value, effectiveDate, resourceName, unit, operation,
	)
}

// queryFloat runs a single-value query against the test database.
func queryFloat(t *testing.T, query string, args ...any) float64 {
	t.Helper()
	var value float64
	if err := testDB.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %s\nquery: %s", err, query)
	}
	return value
}

// queryString runs a single-string query against the test database.
func queryString(t *testing.T, query string, args ...any) string {
	t.Helper()
	var value string
	if err := testDB.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %s\nquery: %s", err, query)
	}
	return value
}

// queryInt runs a single-integer query against the test database.
func queryInt(t *testing.T, query string, args ...any) int {
	t.Helper()
	var value int
	if err := testDB.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %s\nquery: %s", err, query)
	}
	return value
}
