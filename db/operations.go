package db

import (
	"context"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/doug-martin/goqu/v9"
)

// GetOperationID returns the UUID associated with the operation name passed in.
// Accepts a variable number of QueryOptions, though only transactions are
// currently supported.
func (d *Database) GetOperationID(ctx context.Context, name string, opts ...QueryOption) (string, error) {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	query := db.From(t.UpdateOperations).
		Select("id").
		Where(goqu.Ex{
			"name": name,
		}).
		Executor()

	var result string
	if _, err = query.ScanValContext(ctx, &result); err != nil {
		return "", err
	}
	return result, nil
}

// GetOperation returns a *UpdateOperation associated with the UUID passed in.
// Accepts a variable number of QueryOptions, though only transactions are
// currently supported.
func (d *Database) GetOperation(ctx context.Context, id string, opts ...QueryOption) (*UpdateOperation, error) {
	var (
		err    error
		db     GoquDatabase
		result UpdateOperation
	)

	_, db = d.querySettings(opts...)

	query := db.From(t.UpdateOperations).
		Select("id", "name").
		Where(goqu.Ex{
			"id": id,
		}).
		Executor()

	if _, err = query.ScanStructContext(ctx, &result); err != nil {
		return nil, err
	}

	return &result, err
}
