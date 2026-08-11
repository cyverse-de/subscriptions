package apitest

import (
	"net/http"
	"slices"
	"testing"
)

// GET /v1/usages/{username}/updates lists the audit rows QMS writes for every
// usage change. The golden covers one update; the multi-row case is asserted in
// Go below, because widening the harness's unorderedFields is not an option
// here: it keys on "result", which every /v1 response uses.
func TestListUsageUpdates(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)
	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)

	assertGolden(t, "v1_usage_updates_single",
		do(t, http.MethodGet, "/v1/usages/"+testUser+"/updates", ""), http.StatusOK)
}

// The listing is an audit trail, so it comes back oldest first. The effective
// dates are rewritten after the fact so that the expected order is neither the
// order the rows were written in nor their order by value, which is what makes
// this an assertion about the ORDER BY rather than about how Postgres happens
// to store the rows.
func TestListUsageUpdatesReturnsEveryUpdateInOrder(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)
	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)
	recordV1Usage(t, testUser, "cpu.hours", "ADD", 5)
	recordV1Usage(t, testUser, "cpu.hours", "ADD", 7)

	if _, err := testDB.Exec(`
		UPDATE updates
		   SET effective_date = CASE value
		           WHEN 7 THEN TIMESTAMPTZ '2026-01-01 00:00:00Z'
		           WHEN 10 THEN TIMESTAMPTZ '2026-01-02 00:00:00Z'
		           WHEN 5 THEN TIMESTAMPTZ '2026-01-03 00:00:00Z'
		       END`,
	); err != nil {
		t.Fatalf("unable to set the effective dates: %s", err)
	}

	got := do(t, http.MethodGet, "/v1/usages/"+testUser+"/updates", "")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
	}

	result, ok := mustDecode(t, got)["result"].([]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}

	values := make([]float64, 0, len(result))
	for _, entry := range result {
		update, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("unexpected update entry: %v", entry)
		}
		value, _ := update["value"].(float64)
		values = append(values, value)
	}

	if !slices.Equal(values, []float64{7, 10, 5}) {
		t.Errorf("update values = %v, want [7 10 5]", values)
	}
}
