package db

import (
	"context"
	"fmt"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

const (
	PlanNameBasic = "Basic"
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

// loadPlanDetails loads the quota defaults and rates for a plan.
func loadPlanDetails(ctx context.Context, tx *goqu.TxDatabase, plan *model.Plan) error {
	return loadPlanDetailsBatch(ctx, tx, []*model.Plan{plan})
}

// loadPlanDetailsBatch loads the quota defaults and rates for a set of plans, a table at a time rather than a plan at a
// time, so that a listing costs the same number of statements however many plans it covers. Both listings are sorted by
// effective date because model.Plan's GetDefaultQuotaValues and GetActivePlanRate walk them in that order to find the
// currently active entry. Neither sort mentions the plan, because grouping the rows in Go preserves the relative order
// of the rows belonging to any one plan.
func loadPlanDetailsBatch(ctx context.Context, tx *goqu.TxDatabase, plans []*model.Plan) error {
	if len(plans) == 0 {
		return nil
	}

	planIDs := make([]string, 0, len(plans))
	quotaDefaultsByPlan := make(map[string][]model.PlanQuotaDefault, len(plans))
	planRatesByPlan := make(map[string][]model.PlanRate, len(plans))
	for _, plan := range plans {
		planIDs = append(planIDs, *plan.ID)

		// Seeded with empty slices rather than left absent so that a plan with no entries marshals as [] and not null:
		// goqu only touches the destination once per row, where GORM's Find replaced it with an empty slice before
		// reading any.
		quotaDefaultsByPlan[*plan.ID] = []model.PlanQuotaDefault{}
		planRatesByPlan[*plan.ID] = []model.PlanRate{}
	}

	quotaDefaults := []model.PlanQuotaDefault{}
	err := tx.From(t.PQD).
		Select(planQuotaDefaultColumns...).
		Join(t.RT, goqu.On(t.PQD.Col("resource_type_id").Eq(t.RT.Col("id")))).
		Where(t.PQD.Col("plan_id").In(planIDs)).
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
		planID := *quotaDefaults[i].PlanID
		quotaDefaultsByPlan[planID] = append(quotaDefaultsByPlan[planID], quotaDefaults[i])
	}

	planRates := []model.PlanRate{}
	err = tx.From(t.PlanRates).
		Select(planRateColumns...).
		Where(goqu.C("plan_id").In(planIDs)).
		Order(goqu.C("effective_date").Asc()).
		Executor().
		ScanStructsContext(ctx, &planRates)
	if err != nil {
		return fmt.Errorf("unable to look up the plan rates: %w", err)
	}
	for _, planRate := range planRates {
		planID := *planRate.PlanID
		planRatesByPlan[planID] = append(planRatesByPlan[planID], planRate)
	}

	for _, plan := range plans {
		plan.PlanQuotaDefaults = quotaDefaultsByPlan[*plan.ID]
		plan.PlanRates = planRatesByPlan[*plan.ID]
	}

	return nil
}

// plansByID looks up the given plans, along with the quota defaults and rates of each one, and indexes them by
// identifier. It backs the association loads that GORM performed with Preload, which issued exactly this query and
// matched the rows up in Go.
func plansByID(ctx context.Context, tx *goqu.TxDatabase, ids []string) (map[string]*model.Plan, error) {
	byID := make(map[string]*model.Plan, len(ids))
	if len(ids) == 0 {
		return byID, nil
	}
	ids = uniqueIDs(ids)

	plans := []*model.Plan{}
	err := tx.From(t.Plans).
		Select(planColumns...).
		Where(goqu.C("id").In(ids)).
		Executor().
		ScanStructsContext(ctx, &plans)
	if err != nil {
		return nil, fmt.Errorf("unable to look up plans: %w", err)
	}

	if err = loadPlanDetailsBatch(ctx, tx, plans); err != nil {
		return nil, err
	}

	for _, plan := range plans {
		byID[*plan.ID] = plan
	}

	return byID, nil
}

// planRatesByID looks up the given plan rates and indexes them by identifier. It backs the association loads that GORM
// performed with Preload, which issued exactly this query and matched the rows up in Go.
func planRatesByID(ctx context.Context, tx *goqu.TxDatabase, ids []string) (map[string]*model.PlanRate, error) {
	byID := make(map[string]*model.PlanRate, len(ids))
	if len(ids) == 0 {
		return byID, nil
	}
	ids = uniqueIDs(ids)

	planRates := []*model.PlanRate{}
	err := tx.From(t.PlanRates).
		Select(planRateColumns...).
		Where(goqu.C("id").In(ids)).
		Executor().
		ScanStructsContext(ctx, &planRates)
	if err != nil {
		return nil, fmt.Errorf("unable to look up plan rates: %w", err)
	}

	for _, planRate := range planRates {
		byID[*planRate.ID] = planRate
	}

	return byID, nil
}

// GetPlanByID looks up the plan with the given identifier, along with its quota defaults and rates. It returns an error
// matching ErrNotFound when no plan has that identifier.
func GetPlanByID(ctx context.Context, tx *goqu.TxDatabase, planID string) (*model.Plan, error) {
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

// ListPlans lists all of the plans that are currently available, along with the quota defaults and rates of each one.
func ListPlans(ctx context.Context, tx *goqu.TxDatabase) ([]*model.Plan, error) {
	wrapMsg := "unable to list plans"

	// Initialized rather than declared nil so that an empty result marshals as [] and not null: goqu only touches the
	// destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	// Deliberately unordered: the query this replaces had no ORDER BY, and adding one would reorder the response.
	plans := []*model.Plan{}
	err := tx.From(t.Plans).
		Select(planColumns...).
		Executor().
		ScanStructsContext(ctx, &plans)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	if err = loadPlanDetailsBatch(ctx, tx, plans); err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return plans, nil
}

// GetPlansByName builds a map from plan name to plan details.
func GetPlansByName(ctx context.Context, tx *goqu.TxDatabase) (map[string]*model.Plan, error) {
	plans, err := ListPlans(ctx, tx)
	if err != nil {
		return nil, err
	}

	// Build the map from the plan name to the plan details.
	result := make(map[string]*model.Plan, len(plans))
	for _, plan := range plans {
		result[plan.Name] = plan
	}

	return result, nil
}

// CheckPlanExistence determines whether or not a subscription plan with the given identifier exists.
func CheckPlanExistence(ctx context.Context, tx *goqu.TxDatabase, planID string) (bool, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan ID '%s'", planID)
	return planExists(ctx, tx, goqu.C("id").Eq(planID), wrapMsg)
}

// CheckPlanNameExistence determines whether or not a subscription plan with a given name exists.
func CheckPlanNameExistence(ctx context.Context, tx *goqu.TxDatabase, planName string) (bool, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan Name `%s`", planName)
	return planExists(ctx, tx, goqu.C("name").Eq(planName), wrapMsg)
}

// planExists reports whether any plan matches the given condition. The existence tests deliberately count rows rather
// than looking the plan up and reporting ErrNotFound: their callers turn a false result into a 404 of their own, and an
// absent plan is not an error here.
func planExists(ctx context.Context, tx *goqu.TxDatabase, condition goqu.Expression, wrapMsg string) (bool, error) {
	var exists bool
	_, err := tx.From(t.Plans).
		Select(goqu.L("count(*) > 0")).
		Where(condition).
		Executor().
		ScanValContext(ctx, &exists)
	if err != nil {
		return false, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return exists, nil
}

// GetActivePlanRate returns the currently active rate for a subscription plan, which is the rate with the most recent
// effective date that has already passed. It returns an error matching ErrNotFound when the plan has no rate in effect.
func GetActivePlanRate(ctx context.Context, tx *goqu.TxDatabase, planID string) (*model.PlanRate, error) {
	wrapMsg := fmt.Sprintf("unable to look up the active plan rate for '%s'", planID)

	planRate := model.PlanRate{PlanID: &planID}
	found, err := tx.From(t.PlanRates).
		Select(planRateColumns...).
		Where(
			goqu.C("plan_id").Eq(planID),
			goqu.C("effective_date").Lte(goqu.L("CURRENT_TIMESTAMP")),
		).
		Order(goqu.C("effective_date").Desc()).
		Limit(1).
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

// GetActivePlanQuotaDefaults returns the currently active quota defaults for a subscription plan, which are the quota
// defaults for each resource type with the most recent effective date that has already passed.
func GetActivePlanQuotaDefaults(
	ctx context.Context, tx *goqu.TxDatabase, planID string,
) ([]model.PlanQuotaDefault, error) {
	wrapMsg := fmt.Sprintf("unable to look up the active plan quota defaults for '%s'", planID)

	// Initialized rather than declared nil so that a plan with no active quota defaults marshals as [] and not null.
	planQuotaDefaults := []model.PlanQuotaDefault{}
	err := tx.From(t.PQD).
		Distinct(goqu.C("resource_type_id")).
		Select("resource_type_id", "id", "plan_id", "quota_value", "effective_date").
		Where(
			goqu.C("effective_date").Lte(goqu.L("CURRENT_TIMESTAMP")),
			goqu.C("plan_id").Eq(planID),
		).
		Order(goqu.C("resource_type_id").Asc(), goqu.C("effective_date").Desc()).
		Executor().
		ScanStructsContext(ctx, &planQuotaDefaults)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	resourceTypeIDs := make([]string, 0, len(planQuotaDefaults))
	for _, planQuotaDefault := range planQuotaDefaults {
		resourceTypeIDs = append(resourceTypeIDs, *planQuotaDefault.ResourceTypeID)
	}
	resourceTypes, err := resourceTypesByID(ctx, tx, resourceTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	for i := range planQuotaDefaults {
		if resourceType, ok := resourceTypes[*planQuotaDefaults[i].ResourceTypeID]; ok {
			planQuotaDefaults[i].ResourceType = *resourceType
		}
	}

	return planQuotaDefaults, nil
}

// SavePlan saves a new plan along with its quota defaults and rates, recording the generated plan identifier on the
// plan the caller is holding. GORM saved the quota defaults and rates as associations of the plan; they are separate
// statements here, which is what it emitted anyway.
//
// Only the plan's own identifier is recorded: the quota defaults and rates keep the nil IDs they arrived with, so a
// caller that needs them has to re-read the plan with GetPlanByID, which is what every caller does today.
func SavePlan(ctx context.Context, tx *goqu.TxDatabase, plan *model.Plan) error {
	wrapMsg := "unable to save the plan"

	var planID string
	found, err := tx.Insert(t.Plans).
		Rows(goqu.Record{"name": plan.Name, "description": plan.Description}).
		Returning("id").
		Executor().
		ScanValContext(ctx, &planID)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return fmt.Errorf("%s: the insert returned no row", wrapMsg)
	}
	plan.ID = &planID

	// An empty listing is skipped rather than passed on, because the two saves reject one. GORM drew the same
	// distinction: its top-level Create rejected a zero-length slice, but it skipped an association that had no rows,
	// so a plan created with neither quota defaults nor rates succeeds.
	if len(plan.PlanQuotaDefaults) > 0 {
		for i := range plan.PlanQuotaDefaults {
			plan.PlanQuotaDefaults[i].PlanID = &planID
		}
		if err = SavePlanQuotaDefaults(ctx, tx, plan.PlanQuotaDefaults); err != nil {
			return fmt.Errorf("%s: %w", wrapMsg, err)
		}
	}

	if len(plan.PlanRates) > 0 {
		for i := range plan.PlanRates {
			plan.PlanRates[i].PlanID = &planID
		}
		if err = SavePlanRates(ctx, tx, plan.PlanRates); err != nil {
			return fmt.Errorf("%s: %w", wrapMsg, err)
		}
	}

	return nil
}

// SavePlanQuotaDefaults saves the given plan quota defaults. Each one must carry a plan ID and identify its resource
// type, either directly or through the embedded resource type. Saving nothing is an error, matching what GORM's Create
// did with a zero-length slice; callers with a possibly-empty listing have to decide for themselves, the way SavePlan
// does.
//
// The generated identifiers are deliberately not read back. GORM recorded them on the structs it was given, but no
// caller reads them, and the only key that could match a RETURNING row to the quota default that produced it includes
// the effective date, which Postgres stores at a coarser precision than a Go time.Time carries. Matching rows by
// position instead is not an option: Postgres does not promise RETURNING emits rows in VALUES order.
func SavePlanQuotaDefaults(ctx context.Context, tx *goqu.TxDatabase, planQuotaDefaults []model.PlanQuotaDefault) error {
	wrapMsg := "unable to save the plan quota defaults"

	if len(planQuotaDefaults) == 0 {
		return fmt.Errorf("%s: %w", wrapMsg, ErrEmptySlice)
	}

	rows := make([]any, 0, len(planQuotaDefaults))
	for _, planQuotaDefault := range planQuotaDefaults {
		if planQuotaDefault.PlanID == nil {
			return fmt.Errorf("%s: no plan ID specified", wrapMsg)
		}
		resourceTypeID, err := quotaDefaultResourceTypeID(planQuotaDefault)
		if err != nil {
			return fmt.Errorf("%s: %w", wrapMsg, err)
		}
		rows = append(rows, goqu.Record{
			"plan_id":          *planQuotaDefault.PlanID,
			"quota_value":      planQuotaDefault.QuotaValue,
			"resource_type_id": resourceTypeID,
			"effective_date":   planQuotaDefault.EffectiveDate,
		})
	}

	_, err := tx.Insert(t.PQD).Rows(rows...).Executor().ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return nil
}

// quotaDefaultResourceTypeID determines which resource type a plan quota default applies to. The embedded resource type
// takes precedence over the foreign key column, because that is the order GORM resolved the association in: its callers
// look the resource type up by name and assign the whole struct, leaving ResourceTypeID unset.
func quotaDefaultResourceTypeID(planQuotaDefault model.PlanQuotaDefault) (string, error) {
	if planQuotaDefault.ResourceType.ID != nil && *planQuotaDefault.ResourceType.ID != "" {
		return *planQuotaDefault.ResourceType.ID, nil
	}
	if planQuotaDefault.ResourceTypeID != nil && *planQuotaDefault.ResourceTypeID != "" {
		return *planQuotaDefault.ResourceTypeID, nil
	}
	return "", fmt.Errorf("no resource type ID specified for the quota default of resource type '%s'",
		planQuotaDefault.ResourceType.Name)
}

// SavePlanRates saves the given plan rates, each of which must carry a plan ID. Saving nothing is an error, and the
// generated identifiers are not read back, both for the same reasons they are in SavePlanQuotaDefaults.
func SavePlanRates(ctx context.Context, tx *goqu.TxDatabase, planRates []model.PlanRate) error {
	wrapMsg := "unable to save the plan rates"

	if len(planRates) == 0 {
		return fmt.Errorf("%s: %w", wrapMsg, ErrEmptySlice)
	}

	rows := make([]any, 0, len(planRates))
	for _, planRate := range planRates {
		if planRate.PlanID == nil {
			return fmt.Errorf("%s: no plan ID specified", wrapMsg)
		}
		rows = append(rows, goqu.Record{
			"plan_id":        *planRate.PlanID,
			"effective_date": planRate.EffectiveDate,
			"rate":           planRate.Rate,
		})
	}

	_, err := tx.Insert(t.PlanRates).Rows(rows...).Executor().ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return nil
}
