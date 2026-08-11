package apitest

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/cyverse-de/go-mod/logging"
	"github.com/sirupsen/logrus"
)

// captureLogs collects everything written to the shared logger while fn runs.
// The logger is process-wide, so these tests hold a mutex rather than running in
// parallel with anything that logs.
var logCaptureMutex sync.Mutex

func captureLogs(t *testing.T, fn func()) string {
	t.Helper()

	logCaptureMutex.Lock()
	defer logCaptureMutex.Unlock()

	var captured bytes.Buffer
	originalOut := logging.Log.Logger.Out
	originalLevel := logging.Log.Logger.Level
	logging.Log.Logger.SetOutput(&captured)
	logging.Log.Logger.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logging.Log.Logger.SetOutput(originalOut)
		logging.Log.Logger.SetLevel(originalLevel)
	})

	fn()

	return captured.String()
}

// Several route trees log nothing of their own -- the add-on handlers contain no
// log statements at all -- so the request logger is the only thing that can show
// an operator that a request arrived and what it answered.
func TestEveryRequestIsLogged(t *testing.T) {
	resetDB(t)

	testCases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantLevel  string
	}{
		{
			name: "a successful add-on listing", method: http.MethodGet, path: "/addons",
			wantStatus: http.StatusOK, wantLevel: "level=info",
		},
		{
			name: "a request a handler rejected", method: http.MethodPut, path: "/addons", body: `{}`,
			wantStatus: http.StatusBadRequest, wantLevel: "level=warning",
		},
		{
			// Echo answers this one itself, so it also covers the requests no
			// handler ever sees.
			name: "a request for a route that does not exist", method: http.MethodGet, path: "/nonexistent-route",
			wantStatus: http.StatusNotFound, wantLevel: "level=warning",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var got response
			logged := captureLogs(t, func() {
				got = do(t, testCase.method, testCase.path, testCase.body)
			})

			if got.status != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", got.status, testCase.wantStatus, got.body)
			}
			if !strings.Contains(logged, testCase.wantLevel) {
				t.Errorf("the request was not logged at %s:\n%s", testCase.wantLevel, logged)
			}
			for _, fragment := range []string{testCase.method, testCase.path} {
				if !strings.Contains(logged, fragment) {
					t.Errorf("the request log does not mention %q:\n%s", fragment, logged)
				}
			}
		})
	}
}

// The liveness and readiness probes call the greeting route twice every five
// seconds. Logging those would bury real traffic, so the request logger skips
// them -- and would stop being useful if that skip ever widened.
func TestTheProbeRouteIsNotLogged(t *testing.T) {
	logged := captureLogs(t, func() {
		if got := do(t, http.MethodGet, "/", ""); got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}
	})

	if strings.Contains(logged, "level=info") {
		t.Errorf("the probe route was logged:\n%s", logged)
	}
}

// Query logging is controlled by the log level rather than by a flag on the
// database, because handlers build a Database per request and a flag set on one
// instance never reached the others.
func TestQueriesAreLoggedAtDebugLevel(t *testing.T) {
	resetDB(t)

	logged := captureLogs(t, func() {
		if got := do(t, http.MethodGet, "/addons", ""); got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}
	})

	if !strings.Contains(logged, "SELECT") {
		t.Errorf("the add-on query was not logged at debug level:\n%s", logged)
	}
}
