package apitest

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// The handlers used to build their per-request logger by assigning to the
// package-level one, so a username stayed attached to every line the process
// logged afterwards. An operator reading the logs saw requests attributed to
// whoever happened to come before them, which is how the uuid failure on
// GET /users/{username}/usages was first misread as a fault in an unrelated
// user's request. The concurrent case was also a data race on a shared package
// variable, so this file is worth running under -race.

// A username from one request must not appear in the logs of the next.
func TestAUsernameDoesNotLeakIntoALaterRequest(t *testing.T) {
	resetDB(t)

	// A route that logs a username, and would have left it on the package logger.
	if logged := captureLogs(t, func() {
		do(t, http.MethodPut, "/user/alice/updates", updateBody("cpu.hours", "cpu hours", "SET", 1))
	}); !strings.Contains(logged, "alice") {
		t.Fatalf("the request for alice did not log its own username:\n%s", logged)
	}

	testCases := []struct {
		name   string
		method string
		path   string
	}{
		// A request about a different user entirely.
		{name: "a request about another user", method: http.MethodGet, path: "/users/bob/usages"},
		// A request with no username in it at all.
		{name: "a request about no one", method: http.MethodGet, path: "/addons"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			logged := captureLogs(t, func() {
				if got := do(t, testCase.method, testCase.path, ""); got.status != http.StatusOK {
					t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
				}
			})

			if strings.Contains(logged, "alice") {
				t.Errorf("the earlier request's username leaked into this one's logs:\n%s", logged)
			}
		})
	}
}

// The request log names the subject of the request, taken from the route, so
// an operator can find every line for a user. Handlers must not be the source
// of that field: they only run on some routes, and one that sets it leaves it
// on requests about everyone else.
func TestTheRequestLogNamesTheUserTheRequestIsAbout(t *testing.T) {
	resetDB(t)

	testCases := []struct {
		name     string
		path     string
		wantUser string
	}{
		{name: "the username parameter", path: "/users/carol/usages", wantUser: "user=carol"},
		{name: "the user parameter", path: "/summary/dave", wantUser: "user=dave"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			logged := captureLogs(t, func() {
				if got := do(t, http.MethodGet, testCase.path, ""); got.status != http.StatusOK {
					t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
				}
			})

			if !strings.Contains(logged, testCase.wantUser) {
				t.Errorf("the request log does not carry %q:\n%s", testCase.wantUser, logged)
			}
		})
	}

	// A request that matched no route has no user to name, and must not borrow
	// one from whatever ran before it.
	logged := captureLogs(t, func() {
		do(t, http.MethodGet, "/nonexistent-route", "")
	})
	if strings.Contains(logged, "user=") {
		t.Errorf("a request with no user in it logged one:\n%s", logged)
	}
}

// Concurrent requests must each log their own username. Before the fix this
// raced on the package logger, so the assertion below could see another
// goroutine's user.
func TestConcurrentRequestsLogTheirOwnUsername(t *testing.T) {
	resetDB(t)

	const requests = 8

	logged := captureLogs(t, func() {
		var wg sync.WaitGroup
		for i := range requests {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				do(t, http.MethodGet, fmt.Sprintf("/users/user%d/usages", n), "")
			}(i)
		}
		wg.Wait()
	})

	// Every request logged is for the user named in its own URI.
	for line := range strings.SplitSeq(logged, "\n") {
		if !strings.Contains(line, "uri=/users/") {
			continue
		}
		for i := range requests {
			user := fmt.Sprintf("user%d", i)
			if strings.Contains(line, "uri=/users/"+user+"/") && strings.Contains(line, "user=") &&
				!strings.Contains(line, "user="+user) {
				t.Errorf("a request for %s was logged against another user:\n%s", user, line)
			}
		}
	}
}
