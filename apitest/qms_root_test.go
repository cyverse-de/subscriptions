package apitest

import (
	"net/http"
	"slices"
	"testing"
)

// GET /v1 reports service information. Nothing calls it except health checks,
// which only look at the status code, but it is part of the surface the goqu
// rewrite has to preserve.
func TestV1Root(t *testing.T) {
	resetDB(t)
	assertGolden(t, "v1_root", do(t, http.MethodGet, "/v1", ""), http.StatusOK)
}

// GET /v1/users lists every user. The golden covers the single-user case; the
// multi-user case is asserted in Go below rather than by widening the harness's
// unorderedFields, which keys on "result" and would sort every other /v1
// response too.
func TestListUsersSingle(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	assertGolden(t, "v1_users_single", do(t, http.MethodGet, "/v1/users", ""), http.StatusOK)
}

// The listing sorts by username, so the users come back in an order that has
// nothing to do with the order they were created in.
func TestListUsersReturnsEveryUserInOrder(t *testing.T) {
	resetDB(t)
	createUser(t, "carol"+UsernameSuffix)
	createUser(t, "alice"+UsernameSuffix)
	createUser(t, "bob"+UsernameSuffix)

	got := do(t, http.MethodGet, "/v1/users", "")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
	}

	result, ok := mustDecode(t, got)["result"].([]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}

	usernames := make([]string, 0, len(result))
	for _, entry := range result {
		user, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("unexpected user entry: %v", entry)
		}
		name, _ := user["username"].(string)
		usernames = append(usernames, name)
	}

	if !slices.Equal(usernames, []string{"alice", "bob", "carol"}) {
		t.Errorf("usernames = %v, want [alice bob carol]", usernames)
	}
}
