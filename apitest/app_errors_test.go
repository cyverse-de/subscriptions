package apitest

import (
	"net/http"
	"strings"
	"testing"
)

// A UUID that parses but matches nothing, for the routes that take two and only
// one of them is under test.
const absentUUID = "00000000-0000-0000-0000-000000000000"

// PostgreSQL rejects a malformed uuid with "invalid input syntax for type uuid",
// so a caller's typo used to come back as a 500 quoting the driver. These routes
// take a UUID from the path or the body and have to answer 400 without reaching
// the database at all.
func TestMalformedUUIDsAreRejected(t *testing.T) {
	resetDB(t)

	testCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "update an add-on", method: http.MethodPost, path: "/addons/not-a-uuid", body: `{"addon": {"name": "x"}}`},
		{name: "delete an add-on", method: http.MethodDelete, path: "/addons/not-a-uuid"},
		{name: "list a subscription's add-ons", method: http.MethodGet, path: "/subscriptions/not-a-uuid/addons"},
		{
			name: "get a subscription add-on", method: http.MethodGet,
			path: "/subscriptions/" + absentUUID + "/addons/not-a-uuid",
		},
		{
			name: "add a subscription add-on by subscription", method: http.MethodPut,
			path: "/subscriptions/not-a-uuid/addons/" + absentUUID,
		},
		{
			name: "add a subscription add-on by add-on", method: http.MethodPut,
			path: "/subscriptions/" + absentUUID + "/addons/not-a-uuid",
		},
		{
			name: "delete a subscription add-on", method: http.MethodDelete,
			path: "/subscriptions/" + absentUUID + "/addons/not-a-uuid",
		},
		{name: "get a plan", method: http.MethodGet, path: "/plans/not-a-uuid"},
		{
			// This one arrives in the body rather than the path: the handler
			// ignores its path parameters entirely.
			name: "update a subscription add-on", method: http.MethodPost,
			path: "/subscriptions/" + absentUUID + "/addons/" + absentUUID,
			body: `{"subscription_addon": {"uuid": "not-a-uuid"}}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := do(t, testCase.method, testCase.path, testCase.body)

			if got.status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d; body: %s", got.status, http.StatusBadRequest, got.body)
			}
			if !strings.Contains(string(got.body), "must be a UUID") {
				t.Errorf("the response does not say the value must be a UUID: %s", got.body)
			}
			assertNoDatabaseDetail(t, got)
		})
	}
}

// referenceBrokenResourceType adds an add-on and a usage row pointing at an
// unscannable resource type, so the queries that join to it fail. Both rows go
// away with the resource type through ON DELETE CASCADE.
//
// The rows are new rather than a seeded resource type made unscannable in place.
// Updating a seeded row moves it in the heap, which reorders every listing that
// reads it -- which is how the missing ORDER BY on the resource type listing
// turned up.
func referenceBrokenResourceType(t *testing.T, username string) {
	t.Helper()

	unscannableResourceType(t, "apitest.broken")

	if _, err := testDB.Exec(`
		INSERT INTO addons (name, description, resource_type_id, default_amount)
		SELECT 'apitest.broken', 'an add-on nothing can read', id, 1 FROM resource_types WHERE name = 'apitest.broken'`,
	); err != nil {
		t.Fatalf("unable to add the add-on: %s", err)
	}

	if _, err := testDB.Exec(`
		INSERT INTO usages (subscription_id, resource_type_id, usage)
		SELECT s.id, rt.id, 1
		  FROM subscriptions s
		  JOIN users u ON s.user_id = u.id
		  CROSS JOIN resource_types rt
		 WHERE u.username = $1 AND rt.name = 'apitest.broken'`, username,
	); err != nil {
		t.Fatalf("unable to add the usage: %s", err)
	}
}

// A 500 body used to carry the driver's own text, which names tables, columns
// and constraints. The detail belongs in the log, not in a response to whoever
// provoked it.
func TestServerErrorsDoNotLeakDatabaseDetail(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	referenceBrokenResourceType(t, trimmedUser)

	testCases := []struct {
		name string
		path string
	}{
		{name: "a user summary", path: "/summary/" + trimmedUser},
		{name: "a usage listing", path: "/users/" + trimmedUser + "/usages"},
		{name: "an add-on listing", path: "/addons"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := do(t, http.MethodGet, testCase.path, "")

			if got.status != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusInternalServerError, got.body)
			}
			assertNoDatabaseDetail(t, got)
			if !strings.Contains(string(got.body), "the request could not be completed") {
				t.Errorf("the response does not carry the generic message: %s", got.body)
			}
		})
	}
}
