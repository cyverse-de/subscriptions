package db

import (
	"context"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/doug-martin/goqu/v9"
)

// GetCurrentQuota returns the current quota value for a resource type and
// user plan. Also returns a boolean that is true when the actual quota value
// was found and returned and is false when the actual quota was not found and
// the default value was returned. Accepts a variable number of QuotaOptions,
// but only WithTX is currently supported.
func (d *Database) GetCurrentQuota(ctx context.Context, resourceTypeID, subscriptionID string, opts ...QueryOption) (float64, bool, error) {
	var (
		err        error
		db         GoquDatabase
		quotaValue float64
	)

	_, db = d.querySettings(opts...)

	quotasE := db.From("quotas").
		Select(goqu.C("quota")).
		Where(goqu.And(
			goqu.I("resource_type_id").Eq(resourceTypeID),
			goqu.I("subscription_id").Eq(subscriptionID),
		)).
		Limit(1).
		Executor()

	if _, err := quotasE.ScanValContext(ctx, &quotaValue); err != nil {
		return quotaValue, false, err
	}

	quotaFound, err := quotasE.ScanValContext(ctx, &quotaValue)
	if err != nil {
		return quotaValue, false, err
	}

	return quotaValue, quotaFound, nil
}

// LoadQuotaDetails retrieves details about a quota from the database.
func (d *Database) LoadQuotaDetails(
	ctx context.Context,
	resourceTypeID, subscriptionID string,
	opts ...QueryOption,
) (*Quota, error) {
	_, db := d.querySettings(opts...)

	// Build the query.
	query := db.From(t.Quotas).
		Select(
			t.Quotas.Col("id").As("id"),
			t.Quotas.Col("quota").As("quota"),
			t.Quotas.Col("subscription_id").As("subscription_id"),
			t.Quotas.Col("created_by").As("created_by"),
			t.Quotas.Col("created_at").As("created_at"),
			t.Quotas.Col("last_modified_by").As("last_modified_by"),
			t.Quotas.Col("last_modified_at").As("last_modified_at"),
			t.RT.Col("id").As(goqu.C("resource_types.id")),
			t.RT.Col("name").As(goqu.C("resource_types.name")),
			t.RT.Col("unit").As(goqu.C("resource_types.unit")),
			t.RT.Col("consumable").As(goqu.C("resource_types.consumable")),
		).
		Join(t.RT, goqu.On(goqu.I("quotas.resource_type_id").Eq(goqu.I("resource_types.id")))).
		Where(goqu.And(
			goqu.I("resource_type_id").Eq(resourceTypeID),
			goqu.I("subscription_id").Eq(subscriptionID),
		))

	// Execute the query.
	var quota Quota
	found, err := query.Executor().ScanStructContext(ctx, &quota)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	return &quota, nil
}

// UpsertQuota inserts or updates a quota into the database for the given
// resource type and user plan. Accepts a variable number of QueryOptions,
// though only WithTX is currently supported.
func (d *Database) UpsertQuota(ctx context.Context, value float64, resourceTypeID, subscriptionID string, opts ...QueryOption) error {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	updateRecord := goqu.Record{
		"quota":            value,
		"resource_type_id": resourceTypeID,
		"subscription_id":  subscriptionID,
		"created_by":       "de",
		"last_modified_by": "de",
	}

	upsertE := db.Insert("quotas").
		Rows(updateRecord).
		OnConflict(
			goqu.DoUpdate(
				"resource_type_id, subscription_id",
				goqu.C("quota").Set(goqu.I("excluded.quota"))),
		).Executor()

	logStatement(upsertE)

	_, err = upsertE.ExecContext(ctx)
	if err != nil {
		return err
	}

	return nil
}
