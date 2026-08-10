package apitest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The /v1 handlers all run inside a transaction opened by the wrappers in
// internal/qmsapi/controllers/root.go. The failures those wrappers exist to
// handle -- a BEGIN that can't get a connection, a COMMIT that fails after the
// handler has already produced its answer, a ROLLBACK on a dead connection --
// never happen on the happy path, so they are provoked here rather than waited
// for.

// serveWithContext sends a request through the real router with the given request context and fails the test if the
// router doesn't answer within the timeout. The timeout is the assertion: a transaction started without the request
// context waits for a free connection forever.
func serveWithContext(t *testing.T, requestCtx context.Context, method, path string, timeout time.Duration) response {
	t.Helper()

	req := httptest.NewRequest(method, path, nil).WithContext(requestCtx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		testRouter.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
		return response{status: rec.Code, body: rec.Body.Bytes()}
	case <-time.After(timeout):
		t.Fatalf("%s %s did not answer within %s", method, path, timeout)
		return response{}
	}
}

// A client that has disconnected shouldn't leave a handler waiting for a database connection. Both transaction
// wrappers start their transaction with the request context, so a cancelled request is reported rather than parked.
func TestTransactionsHonorTheRequestContext(t *testing.T) {
	// Hold the only connection the pool is allowed to hand out. A transaction started with context.Background()
	// waits for it to come back; one started with the request context gives up immediately.
	testDB.SetMaxOpenConns(1)
	held, err := testDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("unable to reserve the connection: %s", err)
	}
	t.Cleanup(func() {
		if err := held.Close(); err != nil {
			t.Errorf("unable to release the reserved connection: %s", err)
		}
		testDB.SetMaxOpenConns(0)
	})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	// One route of each kind: GET /v1/plans writes its response inside the transaction callback, while
	// GET /v1/subscriptions has its handler write the response after the transaction closes.
	testCases := []struct {
		name string
		path string
	}{
		{name: "callback writes the response", path: "/v1/plans"},
		{name: "handler writes the response", path: "/v1/subscriptions"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := serveWithContext(t, cancelled, http.MethodGet, testCase.path, 10*time.Second)
			if got.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d; body: %s", got.status, http.StatusInternalServerError, got.body)
			}
		})
	}
}
