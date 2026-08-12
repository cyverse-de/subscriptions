package apitest

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyverse-de/subscriptions/db"
)

// GetOrCreateActiveSubscription builds on two lookups that report "absent" as a
// nil result with no error. Dereferencing either one turns a recoverable
// condition into a panic, which middleware.Recover would report as the same
// opaque 500 this service is trying to stop emitting.

// A default plan that isn't there is a deployment problem, and the response has
// to be able to say so rather than dying on a nil *Plan. GetActiveRate takes a
// value receiver, so the deref happens inside SetActiveSubscription.
func TestSubscribingWithNoDefaultPlanFails(t *testing.T) {
	resetDB(t)

	if _, err := testDB.Exec(
		`UPDATE plans SET name = $1 WHERE name = $2`, db.DefaultPlanName+"Renamed", db.DefaultPlanName,
	); err != nil {
		t.Fatalf("unable to rename the default plan: %s", err)
	}
	t.Cleanup(func() {
		if _, err := testDB.Exec(
			`UPDATE plans SET name = $1 WHERE name = $2`, db.DefaultPlanName, db.DefaultPlanName+"Renamed",
		); err != nil {
			t.Fatalf("unable to restore the default plan: %s", err)
		}
	})

	got := do(t, http.MethodPut, "/users/"+newUser+"/usages",
		`{"resource_name": "cpu.hours", "usage_value": 10, "update_type": "SET"}`)

	if got.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusInternalServerError, got.body)
	}
	// A panic would have been recovered into a 500 too, so the point of the
	// assertion is that the handler returned rather than unwound.
	assertNoDatabaseDetail(t, got)
	if !strings.Contains(string(got.body), "the request could not be completed") {
		t.Errorf("the response does not carry the generic message: %s", got.body)
	}
}

// EnsureUser upserts and reads back in a single statement, so a transaction
// that inserts the same new username first leaves the insert with nothing to
// return and the read behind the statement's snapshot: the user exists, and the
// call still reports absent. Callers must not dereference that.
func TestEnsureUserReportsAbsentWhenAnotherTransactionInsertsFirst(t *testing.T) {
	resetDB(t)

	d := db.New(testDB)
	ctx := context.Background()
	const username = "contendeduser"

	first, err := d.Begin()
	if err != nil {
		t.Fatalf("unable to begin the first transaction: %s", err)
	}
	second, err := d.Begin()
	if err != nil {
		t.Fatalf("unable to begin the second transaction: %s", err)
	}
	defer func() { _ = second.Rollback() }()

	// The first transaction inserts the user and holds the unique index without
	// committing.
	if _, err = d.EnsureUser(ctx, username, db.WithTX(first)); err != nil {
		t.Fatalf("the first transaction could not create the user: %s", err)
	}

	// The second subscribes the same brand-new user, so its own EnsureUser --
	// the call whose result used to be dereferenced -- is the one that blocks.
	var wg sync.WaitGroup
	var subscription *db.Subscription
	var subscribeErr error
	wg.Go(func() {
		subscription, subscribeErr = d.GetOrCreateActiveSubscription(ctx, username, db.WithTX(second))
	})

	// Commit only once the second transaction is blocked, so its statement
	// snapshot predates the row it is about to fail to see.
	time.Sleep(300 * time.Millisecond)
	if err = first.Commit(); err != nil {
		t.Fatalf("unable to commit the first transaction: %s", err)
	}
	wg.Wait()

	if subscribeErr == nil {
		t.Fatalf("subscribing succeeded despite the contended user lookup, returning %v", subscription)
	}
	if !strings.Contains(subscribeErr.Error(), "concurrent") {
		t.Errorf("the error does not explain the cause: %s", subscribeErr)
	}
}
