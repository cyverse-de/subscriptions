package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// userColumns lists every column backing model.User. goqu fails a scan when a returned column has no matching struct
// field, so the column list is always spelled out rather than selected with a wildcard.
var userColumns = []any{"id", "username"}

// GetUser looks up the user details, adding the user to the database if necessary.
func GetUser(ctx context.Context, tx *goqu.TxDatabase, username string) (*model.User, error) {
	wrapMsg := "unable to look up or add the user"

	// The conflicting row is reassigned its own username rather than left alone so that RETURNING yields a row for an
	// existing user too; ON CONFLICT DO NOTHING would suppress it and leave the caller without an ID.
	var id string
	found, err := tx.Insert(t.Users).
		Rows(goqu.Record{"username": username}).
		OnConflict(goqu.DoUpdate("username", goqu.Record{"username": goqu.I("excluded.username")})).
		Returning("id").
		Executor().
		ScanValContext(ctx, &id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: the upsert returned no row", wrapMsg)
	}

	return &model.User{ID: &id, Username: username}, nil
}

// ListUsers lists every user registered in the database.
func ListUsers(ctx context.Context, tx *goqu.TxDatabase) ([]model.User, error) {
	wrapMsg := "unable to list the users"

	// Initialized rather than declared nil so that an empty result marshals as [] and not null: goqu only touches the
	// destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	// Deliberately unordered: the query this replaces had no ORDER BY, and adding one would reorder the response.
	users := []model.User{}
	err := tx.From(t.Users).
		Select(userColumns...).
		Executor().
		ScanStructsContext(ctx, &users)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return users, nil
}

// usersByID looks up the given users and indexes them by identifier. It backs the association loads that GORM
// performed with Preload, which issued exactly this query and matched the rows up in Go.
func usersByID(ctx context.Context, tx *goqu.TxDatabase, ids []string) (map[string]*model.User, error) {
	byID := make(map[string]*model.User, len(ids))
	if len(ids) == 0 {
		return byID, nil
	}

	users := []*model.User{}
	err := tx.From(t.Users).
		Select(userColumns...).
		Where(goqu.C("id").In(ids)).
		Executor().
		ScanStructsContext(ctx, &users)
	if err != nil {
		return nil, fmt.Errorf("unable to look up users: %w", err)
	}

	for _, user := range users {
		byID[*user.ID] = user
	}

	return byID, nil
}

// UserExists determines whether or not the user exists in the database.
func UserExists(ctx context.Context, tx *goqu.TxDatabase, username string) (bool, error) {
	wrapMsg := "unable to determine whether user exists"

	var id string
	found, err := tx.From(t.Users).
		Select("id").
		Where(goqu.C("username").Eq(username)).
		Executor().
		ScanValContext(ctx, &id)
	if err != nil {
		return false, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return found, nil
}
