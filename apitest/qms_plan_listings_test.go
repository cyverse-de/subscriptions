package apitest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cyverse-de/subscriptions/db"
	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/doug-martin/goqu/v9"
)

// An empty listing has to marshal as [] rather than null, which no golden can
// check: plans and plan_quota_defaults are seeded reference data, so every
// recorded response is non-empty. The two layers disagree by default — GORM's
// Find replaced the destination with an empty slice before reading any rows,
// while goqu only touches it once per row — so a converted list query that
// declares its destination nil silently changes the wire contract for callers
// that iterate the result. Each case empties the rows its listing reads inside
// a transaction that is rolled back, because resetDB deliberately preserves
// this reference data.
func TestEmptyPlanListingsAreArrays(t *testing.T) {
	testCases := []struct {
		name    string
		prepare string
		list    func(ctx context.Context, tx *goqu.TxDatabase) (any, error)
	}{
		{
			name:    "plans",
			prepare: `DELETE FROM plans`,
			list: func(ctx context.Context, tx *goqu.TxDatabase) (any, error) {
				return qmsdb.ListPlans(ctx, tx)
			},
		},
		{
			name:    "active plan quota defaults",
			prepare: `DELETE FROM plan_quota_defaults`,
			list: func(ctx context.Context, tx *goqu.TxDatabase) (any, error) {
				return qmsdb.GetActivePlanQuotaDefaults(ctx, tx, basicPlanID)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.New(testDB).Begin()
			if err != nil {
				t.Fatalf("unable to start the transaction: %s", err)
			}
			t.Cleanup(func() {
				if err := tx.Rollback(); err != nil {
					t.Fatalf("unable to roll the transaction back: %s", err)
				}
			})

			if _, err = tx.Exec(tc.prepare); err != nil {
				t.Fatalf("unable to empty the table the listing reads: %s", err)
			}

			listing, err := tc.list(context.Background(), tx)
			if err != nil {
				t.Fatalf("unable to list: %s", err)
			}

			encoded, err := json.Marshal(listing)
			if err != nil {
				t.Fatalf("unable to encode the listing: %s", err)
			}
			if string(encoded) != "[]" {
				t.Errorf("an empty listing encoded as %s, want []", encoded)
			}
		})
	}
}
