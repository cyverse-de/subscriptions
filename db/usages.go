package db

import (
	"context"
	"fmt"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exec"
)

// GetCurrentUsage returns the current usage value for the resource type specifed
// by the resource type UUID and associated with the user plan UUID passed in.
// Also returns whether or not the usage was actually found or the default value
// was returned. Accepts a variable number of QueryOptions, though only WithTX
// is currently supported.
func (d *Database) GetCurrentUsage(ctx context.Context, resourceTypeID, subscriptionID string, opts ...QueryOption) (float64, bool, error) {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	usagesE := db.From("usages").
		Select(goqu.C("usage")).
		Where(goqu.And(
			goqu.I("resource_type_id").Eq(resourceTypeID),
			goqu.I("subscription_id").Eq(subscriptionID),
		)).
		Limit(1).
		Executor()

	var usageValue float64
	usageFound, err := usagesE.ScanValContext(ctx, &usageValue)
	if err != nil {
		return usageValue, false, err
	}

	return usageValue, usageFound, nil
}

// UpsertUsage will insert or update a record usage in the database for the
// resource type and user plan indicated. Accepts a variable number of
// QueryOptions, though only WithTX is currently supported.
func (d *Database) UpsertUsage(ctx context.Context, update bool, value float64, resourceTypeID, subscriptionID string, opts ...QueryOption) error {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	updateRecord := goqu.Record{
		"usage":            value,
		"resource_type_id": resourceTypeID,
		"subscription_id":  subscriptionID,
		"last_modified_by": "de",
		"created_by":       "de",
	}

	var upsertE exec.QueryExecutor
	if !update {
		upsertE = db.Insert("usages").Rows(updateRecord).Executor()
	} else {
		upsertE = db.Update("usages").Set(updateRecord).Where(
			goqu.And(
				goqu.I("resource_type_id").Eq(resourceTypeID),
				goqu.I("subscription_id").Eq(subscriptionID),
			),
		).Executor()
	}

	logStatement(upsertE)

	_, err = upsertE.ExecContext(ctx)
	if err != nil {
		return err
	}

	return nil
}

// CalculateUsage upserts a new usage value, ignore the updates tables. Should only
// be used to administratively update a usage value in the case where it gets
// out of sync with the updates. Accepts a variable number of QueryOptions,
// though only WithTX is currently supported.
func (d *Database) CalculateUsage(ctx context.Context, updateType string, usage *Usage, opts ...QueryOption) error {
	var (
		err           error
		newUsageValue float64
	)

	currentUsageValue, doUpdate, err := d.GetCurrentUsage(ctx, usage.ResourceType.ID, usage.SubscriptionID, opts...)
	if err != nil {
		return err
	}
	log.Debugf("the current usage value is %f", currentUsageValue)

	switch updateType {
	case UpdateTypeSet:
		newUsageValue = usage.Usage
	case UpdateTypeAdd:
		newUsageValue = currentUsageValue + usage.Usage
	default:
		return fmt.Errorf("invalid update type: %s", updateType)
	}

	usage.Usage = newUsageValue

	if err = d.UpsertUsage(ctx, doUpdate, newUsageValue, usage.ResourceType.ID, usage.SubscriptionID, opts...); err != nil {
		return err
	}

	return nil
}
