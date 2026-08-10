package apitest

import (
	"encoding/json"
	"net/http"
	"testing"
)

// A user who has never recorded a usage is a routine state, not an edge case,
// and both of these listings answer it with an empty JSON array. No golden can
// cover it: every golden in the suite is seeded or fixture-backed and therefore
// non-empty, so a query rewrite that leaves its destination slice nil would turn
// "[]" into "null" -- a wire-contract change a client's .map() breaks on -- with
// every recorded response still byte-identical.
func TestEmptyUsageListingsAreArrays(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	testCases := []struct {
		name string
		path string
	}{
		{name: "usages", path: "/v1/usages/" + testUser},
		{name: "usage updates", path: "/v1/usages/" + testUser + "/updates"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := do(t, http.MethodGet, tc.path, "")
			if got.status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
			}

			result, found := mustDecode(t, got)["result"]
			if !found {
				t.Fatalf("the response has no result field: %s", got.body)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("unable to re-encode the result: %s", err)
			}
			if string(encoded) != "[]" {
				t.Errorf("result = %s, want []", encoded)
			}
		})
	}
}

// GET /v1/usages/{username} enrols a user the service has never seen in the
// basic plan before answering, so this read path is also a write path. Every
// other test reaches it with the user already subscribed through
// PUT /v1/users/{username}, which enrols them by a different code path, so
// nothing else covers the enrolment this route performs -- and the subscription
// it writes is what every later quota and overage check reads.
func TestUsageListingSubscribesAnUnknownUser(t *testing.T) {
	resetDB(t)

	got := do(t, http.MethodGet, "/v1/usages/"+testUser, "")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
	}

	if users := queryInt(t, `SELECT count(*) FROM users WHERE username = $1`, trimmedUser); users != 1 {
		t.Errorf("users rows = %d, want 1", users)
	}

	if plan := activePlanFor(t, trimmedUser); plan != "Basic" {
		t.Errorf("active plan = %q, want \"Basic\"", plan)
	}

	// The subscription carries the plan's default quotas, which is the whole
	// reason enrolment happens here rather than a bare subscriptions row.
	quotas := queryString(t, `
		SELECT string_agg(rt.name || '=' || q.quota::text, ',' ORDER BY rt.name)
		  FROM quotas q
		  JOIN subscriptions s ON q.subscription_id = s.id
		  JOIN users ON s.user_id = users.id
		  JOIN resource_types rt ON q.resource_type_id = rt.id
		 WHERE users.username = $1`, trimmedUser)
	if quotas != "cpu.hours=200,data.size=5368709120" {
		t.Errorf("quotas = %q, want the Basic plan defaults", quotas)
	}
}
