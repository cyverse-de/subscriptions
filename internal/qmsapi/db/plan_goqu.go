package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// The columns backing model.Plan, model.PlanQuotaDefault and model.PlanRate. goqu fails a scan when a returned column
// has no matching struct field, so the column lists are always spelled out rather than selected with a wildcard. The
// quota default columns are qualified because their query joins resource_types, which has columns of the same name.
var (
	planColumns = []any{"id", "name", "description"}

	planQuotaDefaultColumns = []any{
		t.PQD.Col("id"),
		t.PQD.Col("plan_id"),
		t.PQD.Col("quota_value"),
		t.PQD.Col("resource_type_id"),
		t.PQD.Col("effective_date"),
	}

	planRateColumns = []any{"id", "plan_id", "effective_date", "rate"}
)

// GetPlan looks up the plan with the given name, along with its quota defaults and rates. It returns an error matching
// ErrNotFound when no plan has that name.
func GetPlan(ctx context.Context, tx *goqu.TxDatabase, planName string) (*model.Plan, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan name '%s'", planName)

	var plan model.Plan
	found, err := tx.From(t.Plans).
		Select(planColumns...).
		Where(goqu.C("name").Eq(planName)).
		Executor().
		ScanStructContext(ctx, &plan)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	if err = loadPlanDetails(ctx, tx, &plan); err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return &plan, nil
}

// getPlanRateByID looks up the plan rate with the given identifier. It returns an error matching ErrNotFound when no
// plan rate has that identifier.
func getPlanRateByID(ctx context.Context, tx *goqu.TxDatabase, planRateID string) (*model.PlanRate, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan rate ID '%s'", planRateID)

	var planRate model.PlanRate
	found, err := tx.From(t.PlanRates).
		Select(planRateColumns...).
		Where(goqu.C("id").Eq(planRateID)).
		Executor().
		ScanStructContext(ctx, &planRate)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	return &planRate, nil
}

// loadPlanDetails loads the quota defaults and rates for a plan. Both listings are sorted by effective date because
// model.Plan's GetDefaultQuotaValues and GetActivePlanRate walk them in that order to find the currently active entry.
func loadPlanDetails(ctx context.Context, tx *goqu.TxDatabase, plan *model.Plan) error {
	// Initialized rather than declared nil so that a plan with no entries marshals as [] and not null: goqu only
	// touches the destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	quotaDefaults := []model.PlanQuotaDefault{}
	err := tx.From(t.PQD).
		Select(planQuotaDefaultColumns...).
		Join(t.RT, goqu.On(t.PQD.Col("resource_type_id").Eq(t.RT.Col("id")))).
		Where(t.PQD.Col("plan_id").Eq(*plan.ID)).
		Order(t.PQD.Col("effective_date").Asc(), t.RT.Col("name").Asc()).
		Executor().
		ScanStructsContext(ctx, &quotaDefaults)
	if err != nil {
		return fmt.Errorf("unable to look up the plan quota defaults: %w", err)
	}

	resourceTypeIDs := make([]string, 0, len(quotaDefaults))
	for _, quotaDefault := range quotaDefaults {
		resourceTypeIDs = append(resourceTypeIDs, *quotaDefault.ResourceTypeID)
	}
	resourceTypes, err := resourceTypesByID(ctx, tx, resourceTypeIDs)
	if err != nil {
		return err
	}
	for i := range quotaDefaults {
		if resourceType, ok := resourceTypes[*quotaDefaults[i].ResourceTypeID]; ok {
			quotaDefaults[i].ResourceType = *resourceType
		}
	}
	plan.PlanQuotaDefaults = quotaDefaults

	planRates := []model.PlanRate{}
	err = tx.From(t.PlanRates).
		Select(planRateColumns...).
		Where(goqu.C("plan_id").Eq(*plan.ID)).
		Order(goqu.C("effective_date").Asc()).
		Executor().
		ScanStructsContext(ctx, &planRates)
	if err != nil {
		return fmt.Errorf("unable to look up the plan rates: %w", err)
	}
	plan.PlanRates = planRates

	return nil
}

// getPlanByID looks up the plan with the given identifier, along with its quota defaults and rates.
func getPlanByID(ctx context.Context, tx *goqu.TxDatabase, planID string) (*model.Plan, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan ID '%s'", planID)

	var plan model.Plan
	found, err := tx.From(t.Plans).
		Select(planColumns...).
		Where(goqu.C("id").Eq(planID)).
		Executor().
		ScanStructContext(ctx, &plan)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	if err = loadPlanDetails(ctx, tx, &plan); err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return &plan, nil
}
