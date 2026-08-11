package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// The update operations a usage update may name. They are the two rows the migrations seed into update_operations.
const (
	UpdateTypeSet = "SET"
	UpdateTypeAdd = "ADD"
)

// ApplyUsage applies an update operation to the usage a subscription has recorded against a resource type. The usage
// carries the operand rather than the result; on return it carries the stored row's id and value.
//
// The addition happens in the statement so that concurrent ADDs cannot lose each other: computing the total in Go from
// a previously read value and writing it back drops an increment whenever two updates overlap, silently.
func ApplyUsage(ctx context.Context, tx *goqu.TxDatabase, updateType string, usage *model.Usage) error {
	wrapMsg := "unable to insert or update the usage"

	var storedUsage any
	switch updateType {
	case UpdateTypeSet:
		storedUsage = goqu.I("excluded.usage")
	case UpdateTypeAdd:
		storedUsage = goqu.L("usages.usage + excluded.usage")
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedUpdateType, updateType)
	}

	// The conflict target is the unique index on (subscription_id, resource_type_id): without it a repeated update for
	// the same subscription and resource type would insert a second row instead of updating the running total. The key
	// columns are not reassigned, since they are what the row conflicted on and so already hold these values.
	var stored model.Usage
	found, err := tx.Insert(t.Usages).
		Rows(goqu.Record{
			"usage":            usage.Usage,
			"subscription_id":  *usage.SubscriptionID,
			"resource_type_id": *usage.ResourceTypeID,
		}).
		OnConflict(goqu.DoUpdate("subscription_id,resource_type_id", goqu.Record{
			"usage": storedUsage,
		})).
		Returning(t.Usages.Col("id"), t.Usages.Col("usage")).
		Executor().
		ScanStructContext(ctx, &stored)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return fmt.Errorf("%s: the upsert returned no row", wrapMsg)
	}
	usage.ID = stored.ID
	usage.Usage = stored.Usage

	return nil
}
