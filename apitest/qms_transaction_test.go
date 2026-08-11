package apitest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

// execSQL runs a statement against the test database, failing the test if it doesn't succeed.
func execSQL(t *testing.T, statement string) {
	t.Helper()
	if _, err := testDB.Exec(statement); err != nil {
		t.Fatalf("unable to run %q: %s", statement, err)
	}
}

// killBackendOnInsert installs a trigger that terminates its own backend, which is the only way to reach a failing
// ROLLBACK from outside the database: the statement fails with a connection error and the transaction it belongs to
// can no longer be rolled back.
func killBackendOnInsert(t *testing.T, table string) {
	t.Helper()

	execSQL(t, `CREATE OR REPLACE FUNCTION apitest_kill_backend() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_terminate_backend(pg_backend_pid());
			RETURN NEW;
		END $$`)
	execSQL(t, `CREATE TRIGGER apitest_kill_backend BEFORE INSERT ON `+table+`
		FOR EACH ROW EXECUTE FUNCTION apitest_kill_backend()`)

	t.Cleanup(func() {
		execSQL(t, `DROP TRIGGER IF EXISTS apitest_kill_backend ON `+table)
		execSQL(t, `DROP FUNCTION IF EXISTS apitest_kill_backend()`)
	})
}

// assertQMSError checks that a response carries the /v1 error envelope: a string "error" field and the status text.
// Consumers decode "error" as a string, so an object there is a deserialization failure during the outage the
// response exists to report.
func assertQMSError(t *testing.T, got response, wantStatus int) string {
	t.Helper()

	if got.status != wantStatus {
		t.Errorf("status = %d, want %d; body: %s", got.status, wantStatus, got.body)
	}

	decoded := mustDecode(t, got)
	message, isString := decoded["error"].(string)
	if !isString {
		t.Fatalf("the error field is %#v, want a string; body: %s", decoded["error"], got.body)
	}
	if message == "" {
		t.Errorf("the error field is empty; body: %s", got.body)
	}
	if status := decoded["status"]; status != http.StatusText(wantStatus) {
		t.Errorf("status field = %#v, want %q", status, http.StatusText(wantStatus))
	}
	return message
}

// failAtCommit installs a deferred constraint trigger that raises when the transaction inserting into the table
// commits. Deferring it is what puts the failure after the handler has produced its answer, which is the ordering a
// commit that fails on a lost connection or a serialization conflict would produce.
const commitFailureMessage = "apitest deferred failure"

func failAtCommit(t *testing.T, table string) {
	t.Helper()

	execSQL(t, `CREATE OR REPLACE FUNCTION apitest_fail_at_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION '`+commitFailureMessage+`';
		END $$`)
	execSQL(t, `CREATE CONSTRAINT TRIGGER apitest_fail_at_commit AFTER INSERT ON `+table+`
		DEFERRABLE INITIALLY DEFERRED
		FOR EACH ROW EXECUTE FUNCTION apitest_fail_at_commit()`)

	t.Cleanup(func() {
		execSQL(t, `DROP TRIGGER IF EXISTS apitest_fail_at_commit ON `+table)
		execSQL(t, `DROP FUNCTION IF EXISTS apitest_fail_at_commit()`)
	})
}

// A write reported as successful and then thrown away by a failed COMMIT is the worst outcome available: the caller
// records a plan that doesn't exist and nothing tells it to retry. The response is held back until the transaction
// has committed for exactly this case.
func TestCommitFailureIsNotReportedAsSuccess(t *testing.T) {
	resetDB(t)
	failAtCommit(t, "plans")

	body := `{"name": "apitest.commit", "description": "a plan that never commits",` +
		`"plan_quota_defaults": [{"quota_value": 1234,` +
		`"resource_type": {"name": "cpu.hours", "unit": "cpu hours"},` +
		`"effective_date": "` + effectiveDate + `"}],` +
		`"plan_rates": [{"effective_date": "` + effectiveDate + `", "rate": 9.99}]}`
	got := do(t, http.MethodPost, "/v1/plans", body)

	message := assertQMSError(t, got, http.StatusInternalServerError)
	if !strings.Contains(message, commitFailureMessage) {
		t.Errorf("the error message does not report the commit failure: %s", message)
	}

	// The response and the database have to agree about whether the plan exists.
	if count := queryInt(t, `SELECT count(*) FROM plans WHERE name = $1`, "apitest.commit"); count != 0 {
		t.Errorf("plans named apitest.commit = %d, want 0", count)
	}
}

// POST /v1/subscriptions reports each subscription's outcome in a 200, and a commit failure is one of the outcomes it
// has to be able to report. Buffering the response would break that contract, so its per-item transactions run
// through the wrapper that leaves the response to the handler.
func TestCommitFailureIsReportedPerSubscription(t *testing.T) {
	resetDB(t)
	failAtCommit(t, "subscriptions")

	body := fmt.Sprintf(`{"subscriptions": [{"username": %q, "plan_name": "Pro", "paid": true}]}`, testUser)
	got := do(t, http.MethodPost, "/v1/subscriptions", body)

	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
	}

	decoded := mustDecode(t, got)
	results, isList := decoded["result"].([]any)
	if !isList || len(results) != 1 {
		t.Fatalf("result = %#v, want one entry; body: %s", decoded["result"], got.body)
	}
	entry, isObject := results[0].(map[string]any)
	if !isObject {
		t.Fatalf("the result entry is %#v, want an object", results[0])
	}
	reason, isString := entry["failure_reason"].(string)
	if !isString || !strings.Contains(reason, commitFailureMessage) {
		t.Errorf("failure_reason = %#v, want the commit failure; body: %s", entry["failure_reason"], got.body)
	}
}

// txError writes its error response and then asks for a rollback, so a ROLLBACK that fails must not take the
// handler's answer with it. goqu's Wrap replaces the callback's error with the rollback error, which discards the
// marker that tells the wrapper a response has already been produced.
func TestFailedRollbackKeepsTheHandlersResponse(t *testing.T) {
	resetDB(t)
	killBackendOnInsert(t, "resource_types")

	got := do(t, http.MethodPost, "/v1/resource-types", `{"name": "apitest.rollback", "unit": "count"}`)

	message := assertQMSError(t, got, http.StatusInternalServerError)

	// The response has to carry the message the handler wrote, not the wrapper's account of the transaction that was
	// carrying it. A discarded txAbort leaves the bare rollback error here instead.
	if !strings.HasPrefix(message, "unable to save resource type") {
		t.Errorf("the error message is not the one the handler wrote: %s", message)
	}

	// Evidence that the connection really did die, and therefore that the ROLLBACK this test is about really did
	// fail. Without it the test would keep passing if the trigger ever stopped taking the backend down.
	if !strings.Contains(message, "bad connection") {
		t.Errorf("the insert did not fail with a lost connection, so no ROLLBACK failed: %s", message)
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

			// A failure to BEGIN is still a /v1 response. Reporting it in echo's envelope instead would give a
			// consumer that decodes "error" as a string a deserialization failure during the outage.
			assertQMSError(t, got, http.StatusInternalServerError)
		})
	}
}
