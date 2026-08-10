package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// The columns backing model.UpdateOperation and model.Update. goqu fails a scan when a returned column has no matching
// struct field, so the column lists are always spelled out rather than selected with a wildcard. The update columns are
// qualified because their query joins users, which has a column of the same name.
var (
	updateOperationColumns = []any{"id", "name"}

	updateColumns = []any{
		t.Updates.Col("id"),
		t.Updates.Col("value_type"),
		t.Updates.Col("value"),
		t.Updates.Col("effective_date"),
		t.Updates.Col("update_operation_id"),
		t.Updates.Col("resource_type_id"),
		t.Updates.Col("user_id"),
		t.Updates.Col("metadata"),
	}
)

// GetUpdateOperationByName looks up the update operation with the given name. It returns an error matching ErrNotFound
// when no update operation has that name.
//
// The name must stay an explicit predicate. The GORM version this replaces passed the name on the destination struct
// instead, and GORM's First derives conditions only from primary key fields, so the query matched whichever operation
// sorted first: every usage update was audited as ADD regardless of what was asked for, and an unrecognized update type
// was accepted rather than refused. TestUsageWritesAnUpdatesRow in apitest pins both halves of that fix.
func GetUpdateOperationByName(ctx context.Context, tx *goqu.TxDatabase, name string) (*model.UpdateOperation, error) {
	wrapMsg := fmt.Sprintf("unable to look up update operation '%s'", name)

	var updateOperation model.UpdateOperation
	found, err := tx.From(t.UOps).
		Select(updateOperationColumns...).
		Where(goqu.C("name").Eq(name)).
		Executor().
		ScanStructContext(ctx, &updateOperation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	return &updateOperation, nil
}

// SaveUpdate records an update in the database, recording the generated identifier on the update the caller is holding.
func SaveUpdate(ctx context.Context, tx *goqu.TxDatabase, update *model.Update) error {
	wrapMsg := "unable to record the update"

	var id string
	found, err := tx.Insert(t.Updates).
		Rows(goqu.Record{
			"value_type":          update.ValueType,
			"value":               update.Value,
			"effective_date":      update.EffectiveDate,
			"update_operation_id": *update.UpdateOperationID,
			"resource_type_id":    *update.ResourceTypeID,
			"user_id":             update.UserID,
			"metadata":            update.Metadata,
		}).
		Returning("id").
		Executor().
		ScanValContext(ctx, &id)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return fmt.Errorf("%s: the insert returned no row", wrapMsg)
	}
	update.ID = &id

	return nil
}

// ListUpdatesForUser lists every update recorded for the given user, along with the resource type and user each one
// refers to, oldest first. The updates table is an audit trail, so effective date is the order it is read in; the
// identifier breaks ties, because two updates recorded in the same request carry the same date.
func ListUpdatesForUser(ctx context.Context, tx *goqu.TxDatabase, username string) ([]model.Update, error) {
	wrapMsg := fmt.Sprintf("unable to list the updates for user '%s'", username)

	// Initialized rather than declared nil so that a user with no updates marshals as [] and not null: goqu only
	// touches the destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	updates := []model.Update{}
	err := tx.From(t.Updates).
		Select(updateColumns...).
		Join(t.Users, goqu.On(t.Updates.Col("user_id").Eq(t.Users.Col("id")))).
		Where(t.Users.Col("username").Eq(username)).
		Order(t.Updates.Col("effective_date").Asc(), t.Updates.Col("id").Asc()).
		Executor().
		ScanStructsContext(ctx, &updates)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	if err = loadUpdateDetails(ctx, tx, updates); err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return updates, nil
}

// loadUpdateDetails loads the resource type and user associated with each update, the way GORM's Preload did.
func loadUpdateDetails(ctx context.Context, tx *goqu.TxDatabase, updates []model.Update) error {
	resourceTypeIDs := make([]string, 0, len(updates))
	userIDs := make([]string, 0, len(updates))
	for _, update := range updates {
		if update.ResourceTypeID != nil {
			resourceTypeIDs = append(resourceTypeIDs, *update.ResourceTypeID)
		}
		if update.UserID != nil {
			userIDs = append(userIDs, *update.UserID)
		}
	}

	resourceTypes, err := resourceTypesByID(ctx, tx, resourceTypeIDs)
	if err != nil {
		return err
	}
	users, err := usersByID(ctx, tx, userIDs)
	if err != nil {
		return err
	}

	for i := range updates {
		if updates[i].ResourceTypeID != nil {
			if resourceType, ok := resourceTypes[*updates[i].ResourceTypeID]; ok {
				updates[i].ResourceType = *resourceType
			}
		}
		if updates[i].UserID != nil {
			if user, ok := users[*updates[i].UserID]; ok {
				updates[i].User = *user
			}
		}
	}

	return nil
}
