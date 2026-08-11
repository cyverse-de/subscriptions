package apitest

import (
	"net/http"
	"testing"
)

// GET /summary/{user} is what resource-usage-api calls to build a user's
// subscription summary. It has a write side effect: a user the service has
// never seen is created and subscribed to the default plan.
func TestUserSummary(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 42)

	assertGolden(t, "summary_basic",
		do(t, http.MethodGet, "/summary/"+trimmedUser, ""), http.StatusOK)
}

func TestUserSummaryForAnUnknownUser(t *testing.T) {
	resetDB(t)

	assertGolden(t, "summary_unknown_user",
		do(t, http.MethodGet, "/summary/nosuchuser", ""), http.StatusOK)
}

func TestGreeting(t *testing.T) {
	got := do(t, http.MethodGet, "/", "")
	if got.status != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", got.status, got.body)
	}
	if string(got.body) != "Hello from subscriptions." {
		t.Errorf("body = %q, want \"Hello from subscriptions.\"", got.body)
	}
}
