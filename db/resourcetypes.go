package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/doug-martin/goqu/v9"
)

// GetResourceTypeID returns the UUID associated with the name and unit passed in.
// Accepts a variable number of QueryOptions, though only transactions are
// currently supported.
func (d *Database) GetResourceTypeID(ctx context.Context, name, unit string, opts ...QueryOption) (string, error) {
	var (
		err error
		db  GoquDatabase
	)

	querySettings := &QuerySettings{}
	for _, opt := range opts {
		opt(querySettings)
	}

	if querySettings.tx != nil {
		db = querySettings.tx
	} else {
		db = d.goquDB
	}

	query := db.From(t.RT).
		Select("id").
		Where(goqu.Ex{
			"name": name,
			"unit": unit,
		}).
		Executor()

	var result string
	if _, err = query.ScanValContext(ctx, &result); err != nil {
		return "", err
	}
	return result, nil
}

// GetResourceType returns a *ResourceType associated with the UUID passed in.
// Accepts a variable number of QueryOptions, though only transactions are
// currently supported.
func (d *Database) GetResourceType(ctx context.Context, id string, opts ...QueryOption) (*ResourceType, error) {
	var (
		err    error
		db     GoquDatabase
		result ResourceType
	)

	querySettings := &QuerySettings{}
	for _, opt := range opts {
		opt(querySettings)
	}

	if querySettings.tx != nil {
		db = querySettings.tx
	} else {
		db = d.goquDB
	}

	query := db.From(t.RT).
		Select(
			t.RT.Col("id"),
			t.RT.Col("name"),
			t.RT.Col("unit"),
			t.RT.Col("consumable"),
		).
		Where(t.RT.Col("id").Eq(id)).
		Executor()

	if _, err = query.ScanStructContext(ctx, &result); err != nil {
		return nil, err
	}

	return &result, err
}

// GetResourceTypeByName returns a *ResourceType associated with the name passed
// in. Accepts a variable number of QueryOptions, though only transactions are
// currently supported.
func (d *Database) GetResourceTypeByName(ctx context.Context, name string, opts ...QueryOption) (*ResourceType, error) {
	var (
		err          error
		db           GoquDatabase
		resourceType ResourceType
	)

	_, db = d.querySettings(opts...)

	query := db.From(t.RT).
		Select(
			t.RT.Col("id"),
			t.RT.Col("name"),
			t.RT.Col("unit"),
			t.RT.Col("consumable"),
		).
		Where(t.RT.Col("name").Eq(name)).
		Executor()

	if _, err = query.ScanStructContext(ctx, &resourceType); err != nil {
		return nil, err
	}

	return &resourceType, nil
}

// LookupResourceType attempts to look up a resource type using either its ID or name in that order. It's an error to
// attempt a lookup with no ID or name specified.
func (d *Database) LookupResoureType(
	ctx context.Context,
	lookup *ResourceType,
	opts ...QueryOption,
) (*ResourceType, error) {
	if lookup.ID != "" {
		return d.GetResourceType(ctx, lookup.ID, opts...)
	} else if lookup.Name != "" {
		return d.GetResourceTypeByName(ctx, lookup.Name, opts...)
	} else {
		return nil, fmt.Errorf("either the resource type ID or name must be specified")
	}
}
