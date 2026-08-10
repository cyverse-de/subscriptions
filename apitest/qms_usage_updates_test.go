package apitest

import (
	"net/http"
	"testing"
)

// GET /v1/usages/{username}/updates lists the audit rows QMS writes for every
// usage change. The query carries no ORDER BY (controllers/usages.go:193), so
// the golden covers one update, where order is unobservable, and the multi-row
// case is counted instead. Widening the harness's unorderedFields is not an
// option here: it keys on "result", which every /v1 response uses.
func TestListUsageUpdates(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)
	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)

	assertGolden(t, "v1_usage_updates_single",
		do(t, http.MethodGet, "/v1/usages/"+testUser+"/updates", ""), http.StatusOK)
}

func TestListUsageUpdatesReturnsEveryUpdate(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)
	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)
	recordV1Usage(t, testUser, "cpu.hours", "ADD", 5)

	got := do(t, http.MethodGet, "/v1/usages/"+testUser+"/updates", "")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
	}

	result, ok := mustDecode(t, got)["result"].([]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}
	if len(result) != 2 {
		t.Errorf("updates returned = %d, want 2", len(result))
	}
}
