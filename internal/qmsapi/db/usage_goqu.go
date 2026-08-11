package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// UpsertUsage either inserts a new usage record into the database or updates an existing one.
func UpsertUsage(ctx context.Context, tx *goqu.TxDatabase, usage *model.Usage) error {
	wrapMsg := "unable to insert or update the usage"

	// The conflict target is the unique index on (subscription_id, resource_type_id): without it a repeated update for
	// the same subscription and resource type would insert a second row instead of replacing the running total. The two
	// key columns are reassigned alongside the usage value to match the clause this replaces; only the value can
	// actually differ, since the other two are what the row conflicted on.
	var id string
	found, err := tx.Insert(t.Usages).
		Rows(goqu.Record{
			"usage":            usage.Usage,
			"subscription_id":  *usage.SubscriptionID,
			"resource_type_id": *usage.ResourceTypeID,
		}).
		OnConflict(goqu.DoUpdate("subscription_id,resource_type_id", goqu.Record{
			"usage":            goqu.I("excluded.usage"),
			"subscription_id":  goqu.I("excluded.subscription_id"),
			"resource_type_id": goqu.I("excluded.resource_type_id"),
		})).
		Returning("id").
		Executor().
		ScanValContext(ctx, &id)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return fmt.Errorf("%s: the upsert returned no row", wrapMsg)
	}
	usage.ID = &id

	return nil
}
