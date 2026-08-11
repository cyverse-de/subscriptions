package apitest

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/cyverse-de/subscriptions/db"
	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// applyUsage is one implementation of "apply this update to the stored usage". The service has two, one per route
// tree, and both have to be safe under concurrency.
type applyUsage func(ctx context.Context, tx *goqu.TxDatabase, operation string, value float64, resourceTypeID, subscriptionID string) error

// usageAppliers names the two implementations so the same table runs against both.
var usageAppliers = map[string]applyUsage{
	// The NATS and /user routes, through db.Database.
	"db.ApplyUsage": func(ctx context.Context, tx *goqu.TxDatabase, operation string, value float64, resourceTypeID, subscriptionID string) error {
		_, err := db.New(testDB).ApplyUsage(ctx, operation, value, resourceTypeID, subscriptionID, db.WithTX(tx))
		return err
	},
	// POST /v1/usages, through the qmsapi database layer.
	"qmsdb.ApplyUsage": func(ctx context.Context, tx *goqu.TxDatabase, operation string, value float64, resourceTypeID, subscriptionID string) error {
		return qmsdb.ApplyUsage(ctx, tx, operation, &model.Usage{
			Usage:          value,
			SubscriptionID: &subscriptionID,
			ResourceTypeID: &resourceTypeID,
		})
	},
}

// Two usage updates for the same subscription and resource type serialize on the row's lock, so the second one runs
// with the first one's value already committed. Computing the total in Go instead of in the statement loses the second
// increment: the value it adds to was read before the first update committed, and writing the sum back overwrites it.
//
// These tests provoke that ordering deliberately rather than racing goroutines and hoping, so a regression fails every
// run instead of one in a hundred.
func TestConcurrentUsageUpdatesDoNotLoseIncrements(t *testing.T) {
	testCases := []struct {
		name string
		// seed is applied and committed before either update starts. Leaving it out exercises the insert side of the
		// upsert, where the two statements conflict on the unique index rather than on an existing row's lock.
		seed           float64
		seedUsage      bool
		firstOperation string
		first          float64
		second         float64
		want           float64
	}{
		{
			name: "both add to an existing usage",
			seed: 10, seedUsage: true,
			firstOperation: db.UpdateTypeAdd, first: 5, second: 3, want: 18,
		},
		{
			name:           "both add with no usage row yet",
			firstOperation: db.UpdateTypeAdd, first: 5, second: 3, want: 8,
		},
		{
			name: "a set followed by an add",
			seed: 10, seedUsage: true,
			firstOperation: db.UpdateTypeSet, first: 4, second: 3, want: 7,
		},
	}

	for implementation, apply := range usageAppliers {
		for _, testCase := range testCases {
			t.Run(implementation+"/"+testCase.name, func(t *testing.T) {
				resetDB(t)
				subscribeUser(t, trimmedUser, "Basic")

				ctx := context.Background()
				database := db.New(testDB)
				subscriptionID := queryString(t, `
					SELECT s.id FROM subscriptions s JOIN users u ON s.user_id = u.id WHERE u.username = $1`, trimmedUser)
				resourceTypeID := queryString(t, `SELECT id FROM resource_types WHERE name = 'cpu.hours'`)

				// inTransaction runs one update in its own committed transaction.
				inTransaction := func(operation string, value float64) error {
					tx, err := database.BeginTx(ctx, &sql.TxOptions{})
					if err != nil {
						return err
					}
					if err = apply(ctx, tx, operation, value, resourceTypeID, subscriptionID); err != nil {
						return errors.Join(err, tx.Rollback())
					}
					return tx.Commit()
				}

				if testCase.seedUsage {
					if err := inTransaction(db.UpdateTypeSet, testCase.seed); err != nil {
						t.Fatalf("unable to seed the usage: %s", err)
					}
				}

				// The first transaction stays open, holding the row, until the second one is provably waiting for it.
				firstTx, err := database.BeginTx(ctx, &sql.TxOptions{})
				if err != nil {
					t.Fatalf("unable to begin the first transaction: %s", err)
				}
				if err = apply(ctx, firstTx, testCase.firstOperation, testCase.first, resourceTypeID, subscriptionID); err != nil {
					t.Fatalf("the first update failed: %s", err)
				}

				secondDone := make(chan error, 1)
				go func() {
					secondDone <- inTransaction(db.UpdateTypeAdd, testCase.second)
				}()

				waitForBlockedUpdate(t)

				if err = firstTx.Commit(); err != nil {
					t.Fatalf("unable to commit the first transaction: %s", err)
				}

				select {
				case err := <-secondDone:
					if err != nil {
						t.Fatalf("the second update failed: %s", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("the second update never finished after the first transaction committed")
				}

				got := queryFloat(t, `
					SELECT usage FROM usages WHERE subscription_id = $1 AND resource_type_id = $2`,
					subscriptionID, resourceTypeID)
				if got != testCase.want {
					t.Errorf("usage = %v, want %v", got, testCase.want)
				}

				// One row, not two: the upsert has to find the existing row rather than insert beside it.
				if rows := queryInt(t, `
					SELECT count(*) FROM usages WHERE subscription_id = $1 AND resource_type_id = $2`,
					subscriptionID, resourceTypeID); rows != 1 {
					t.Errorf("usage rows = %d, want 1", rows)
				}
			})
		}
	}
}

// waitForBlockedUpdate waits until a backend is waiting on a lock, which is how the test knows the second transaction
// has reached its write and is queued behind the first rather than still starting up. Without it the first transaction
// could commit before the second one touched anything, and the test would pass against code that loses the increment.
func waitForBlockedUpdate(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if queryInt(t, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'`) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no backend ever blocked on a lock, so the two updates did not overlap")
}
