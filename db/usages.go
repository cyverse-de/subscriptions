package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/doug-martin/goqu/v9"
)

// LoadUsageDetails retrieves the full usage row for the given resource type and
// subscription, including the identity and audit columns that ApplyUsage leaves
// behind. Returns a nil usage when no row exists. Accepts a variable number of
// QueryOptions, though only WithTX is currently supported.
func (d *Database) LoadUsageDetails(
	ctx context.Context,
	resourceTypeID, subscriptionID string,
	opts ...QueryOption,
) (*Usage, error) {
	_, db := d.querySettings(opts...)

	query := db.From(t.Usages).
		Select(
			t.Usages.Col("id").As("id"),
			t.Usages.Col("usage").As("usage"),
			t.Usages.Col("subscription_id").As("subscription_id"),
			t.Usages.Col("created_by").As("created_by"),
			t.Usages.Col("created_at").As("created_at"),
			t.Usages.Col("last_modified_by").As("last_modified_by"),
			t.Usages.Col("last_modified_at").As("last_modified_at"),
			t.RT.Col("id").As(goqu.C("resource_types.id")),
			t.RT.Col("name").As(goqu.C("resource_types.name")),
			t.RT.Col("unit").As(goqu.C("resource_types.unit")),
			t.RT.Col("consumable").As(goqu.C("resource_types.consumable")),
		).
		Join(t.RT, goqu.On(goqu.I("usages.resource_type_id").Eq(goqu.I("resource_types.id")))).
		Where(goqu.And(
			goqu.I("usages.resource_type_id").Eq(resourceTypeID),
			goqu.I("usages.subscription_id").Eq(subscriptionID),
		))
	d.LogSQL(query)

	var usage Usage
	found, err := query.Executor().ScanStructContext(ctx, &usage)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	return &usage, nil
}

// ApplyUsage applies an update operation to the usage a subscription has
// recorded against a resource type and returns the stored result. Accepts a
// variable number of QueryOptions, though only WithTX is currently supported.
//
// The arithmetic belongs in the statement rather than in Go. Two concurrent ADDs
// that each read the row, add their value and write the total back serialize on
// the row lock, and the second one overwrites the first with a total it computed
// from the value it read before the first committed -- silently dropping an
// increment, with no error and no way to notice after the fact.
func (d *Database) ApplyUsage(
	ctx context.Context,
	updateType string,
	value float64,
	resourceTypeID, subscriptionID string,
	opts ...QueryOption,
) (float64, error) {
	_, db := d.querySettings(opts...)

	// ADD folds the new value into whatever the row holds at the moment the statement reaches it, which is what makes
	// it safe under concurrency; SET replaces the value outright and never needed the old one.
	var storedUsage any
	switch updateType {
	case UpdateTypeSet:
		storedUsage = goqu.I("excluded.usage")
	case UpdateTypeAdd:
		storedUsage = goqu.L("usages.usage + excluded.usage")
	default:
		return 0, fmt.Errorf("invalid update type: %s", updateType)
	}

	// The conflict target is the unique index on (resource_type_id, subscription_id). created_by is deliberately absent
	// from the update: it describes the insert that created the row, and the statement this replaces overwrote it on
	// every update.
	query := db.Insert(t.Usages).
		Rows(goqu.Record{
			"usage":            value,
			"resource_type_id": resourceTypeID,
			"subscription_id":  subscriptionID,
			"created_by":       "de",
			"last_modified_by": "de",
		}).
		OnConflict(goqu.DoUpdate("resource_type_id,subscription_id", goqu.Record{
			"usage":            storedUsage,
			"last_modified_by": "de",
		})).
		Returning(t.Usages.Col("usage"))
	d.LogSQL(query)

	var stored float64
	found, err := query.Executor().ScanValContext(ctx, &stored)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("the usage upsert for subscription %s returned no row", subscriptionID)
	}

	return stored, nil
}

// CalculateUsage upserts a new usage value, ignore the updates tables. Should only
// be used to administratively update a usage value in the case where it gets
// out of sync with the updates. Accepts a variable number of QueryOptions,
// though only WithTX is currently supported.
func (d *Database) CalculateUsage(ctx context.Context, updateType string, usage *Usage, opts ...QueryOption) error {
	stored, err := d.ApplyUsage(ctx, updateType, usage.Usage, usage.ResourceType.ID, usage.SubscriptionID, opts...)
	if err != nil {
		return err
	}
	log.Debugf("the stored usage value is %f", stored)

	usage.Usage = stored

	return nil
}
