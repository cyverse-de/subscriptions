package db

import (
	"context"

	"github.com/doug-martin/goqu/v9"
	"github.com/pkg/errors"
)

// GetUserID returns a user's UUID associated with their username.
// Accepts a variable number of QueryOptions, though only WithTX is currently
// supported.
func (d *Database) GetUserID(ctx context.Context, username string, opts ...QueryOption) (string, error) {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	usersT := goqu.T("users")
	query := db.From(usersT).
		Select("id").
		Where(goqu.Ex{
			"username": username,
		}).
		Executor()

	var result string
	if _, err = query.ScanValContext(ctx, &result); err != nil {
		return "", err
	}
	return result, nil
}

// GetUser returns a *User assocated with the UUID passed in.
// Accepts a variable number of QueryOptions, though online WithTX is currently
// supported.
func (d *Database) GetUser(ctx context.Context, id string, opts ...QueryOption) (*User, error) {
	var (
		err    error
		db     GoquDatabase
		result User
	)

	_, db = d.querySettings(opts...)

	usersT := goqu.T("users")
	query := db.From(usersT).
		Select("id", "username").
		Where(goqu.Ex{
			"id": id,
		}).
		Executor()

	if _, err = query.ScanStructContext(ctx, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (d *Database) UserExists(ctx context.Context, username string, opts ...QueryOption) (bool, error) {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	users := goqu.T("users")
	count, err := db.From(users).Where(users.Col("username").Eq(username)).Count()
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (d *Database) AddUser(ctx context.Context, username string, opts ...QueryOption) (string, error) {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	ds := db.Insert("users").Rows(
		goqu.Record{
			"username": username,
		},
	).
		Returning(goqu.C("id")).
		Executor()

	var id string

	if _, err = ds.ScanValContext(ctx, &id); err != nil {
		return "", err
	}

	return id, nil
}

// EnsureUser ensures that a user with the given username exists in the database then returns the user information.
// This function accepts a variable number of QueryOptions, but only WithTX is currently supported.
func (d *Database) EnsureUser(ctx context.Context, username string, opts ...QueryOption) (*User, error) {
	var (
		wrapMsg = "unable to ensure that the user exists in the database"
		err     error
		db      GoquDatabase
		result  User
	)

	// Prepare to execute the statement.
	_, db = d.querySettings(opts...)

	// Build the statement.
	usersT := goqu.T("users")
	statement := db.From("ins").
		With("ins",
			db.Insert(usersT).
				Returning("id", "username").
				Rows(goqu.Record{"username": username}).
				OnConflict(goqu.DoNothing())).
		UnionAll(
			db.From(usersT).
				Select("id", "username").
				Where(goqu.Ex{"username": username}))

	// Log the SQL statement if we're debugging database calls.
	d.LogSQL(statement)

	// Execute the statement and fetch the result.
	found, err := statement.Executor().ScanStructContext(ctx, &result)
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}
	if !found {
		return nil, nil
	}

	return &result, nil
}
