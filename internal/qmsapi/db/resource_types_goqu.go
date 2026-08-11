package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// resourceTypeColumns lists every column backing model.ResourceType. goqu fails
// a scan when a returned column has no matching struct field, so the column list
// is always spelled out rather than selected with a wildcard.
var resourceTypeColumns = []any{"id", "name", "unit", "consumable"}

// resourceTypesByID looks up the given resource types and indexes them by identifier. It backs the association loads
// that GORM performed with Preload, which issued exactly this query and matched the rows up in Go.
func resourceTypesByID(ctx context.Context, tx *goqu.TxDatabase, ids []string) (map[string]*model.ResourceType, error) {
	byID := make(map[string]*model.ResourceType, len(ids))
	if len(ids) == 0 {
		return byID, nil
	}
	ids = uniqueIDs(ids)

	resourceTypes := []*model.ResourceType{}
	err := tx.From(t.RT).
		Select(resourceTypeColumns...).
		Where(goqu.C("id").In(ids)).
		Executor().
		ScanStructsContext(ctx, &resourceTypes)
	if err != nil {
		return nil, fmt.Errorf("unable to look up resource types: %w", err)
	}

	for _, resourceType := range resourceTypes {
		byID[*resourceType.ID] = resourceType
	}

	return byID, nil
}

// GetResourceTypeByName looks up the resource type with the given name. It returns an error matching ErrNotFound when no
// resource type has that name.
func GetResourceTypeByName(ctx context.Context, tx *goqu.TxDatabase, name string) (*model.ResourceType, error) {
	wrapMsg := fmt.Sprintf("unable to look up resource type '%s'", name)

	var resourceType model.ResourceType
	found, err := tx.From(t.RT).
		Select(resourceTypeColumns...).
		Where(goqu.C("name").Eq(name)).
		Executor().
		ScanStructContext(ctx, &resourceType)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	return &resourceType, nil
}

// GetResourceTypeByID looks up the resource type with the given identifier. It returns an error matching ErrNotFound
// when no resource type has that identifier.
func GetResourceTypeByID(ctx context.Context, tx *goqu.TxDatabase, id string) (*model.ResourceType, error) {
	wrapMsg := fmt.Sprintf("unable to look up resource type '%s'", id)

	var resourceType model.ResourceType
	found, err := tx.From(t.RT).
		Select(resourceTypeColumns...).
		Where(goqu.C("id").Eq(id)).
		Executor().
		ScanStructContext(ctx, &resourceType)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	return &resourceType, nil
}

// ListResourceTypes lists all of the resource types defined in the database.
func ListResourceTypes(ctx context.Context, tx *goqu.TxDatabase) (*model.ResourceTypeList, error) {
	wrapMsg := "unable to list resource types"

	// Initialized rather than declared nil so that an empty result marshals as [] and not null: goqu only touches the
	// destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	// Ordered by name: without an ORDER BY the response follows the heap, so updating any row -- a rename, a
	// consumable flag flipped -- silently reorders the list for every client afterwards.
	resourceTypes := []*model.ResourceType{}
	err := tx.From(t.RT).
		Select(resourceTypeColumns...).
		Order(t.RT.Col("name").Asc()).
		Executor().
		ScanStructsContext(ctx, &resourceTypes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return &model.ResourceTypeList{ResourceTypes: resourceTypes}, nil
}

// UpdateResourceType updates an existing resource type.
func UpdateResourceType(ctx context.Context, tx *goqu.TxDatabase, resourceType model.ResourceType) error {
	wrapMsg := "unable to update resource type"

	// Make sure that the incoming resource type has an identifier associated with it.
	if resourceType.ID == nil || *resourceType.ID == "" {
		return fmt.Errorf("%s: no resource type ID specified", wrapMsg)
	}

	_, err := tx.Update(t.RT).
		Set(goqu.Record{
			"name":       resourceType.Name,
			"unit":       resourceType.Unit,
			"consumable": resourceType.Consumable,
		}).
		Where(goqu.C("id").Eq(*resourceType.ID)).
		Executor().
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return nil
}

// SaveResourceType saves a new resource type, returning ErrResourceTypeConflict if one with the same name already
// exists.
func SaveResourceType(ctx context.Context, tx *goqu.TxDatabase, resourceType model.ResourceType) (*model.ResourceType, error) {
	wrapMsg := "unable to save resource type"

	// The ID column is left out of the insert so that the database generates it; a client-supplied ID would let the
	// insert land on a row the duplicate check never looks at.
	var saved model.ResourceType
	found, err := tx.Insert(t.RT).
		Rows(goqu.Record{
			"name":       resourceType.Name,
			"unit":       resourceType.Unit,
			"consumable": resourceType.Consumable,
		}).
		OnConflict(goqu.DoNothing()).
		Returning(resourceTypeColumns...).
		Executor().
		ScanStructContext(ctx, &saved)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// ON CONFLICT DO NOTHING suppresses the insert instead of failing it, so an empty RETURNING result is what a
	// duplicate name looks like. The sentinel is returned unwrapped because the handler renders it as the 409 body.
	if !found {
		return nil, ErrResourceTypeConflict
	}

	return &saved, nil
}
