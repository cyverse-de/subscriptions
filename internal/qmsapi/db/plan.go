package db

import (
	"context"
	"fmt"

	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const (
	PlanNameBasic = "Basic"
)

// GetPlanGORM looks up the plan with the given name.
func GetPlanGORM(ctx context.Context, db *gorm.DB, planName string) (*model.Plan, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan name '%s'", planName)
	var err error
	var plan = model.Plan{}
	err = db.
		WithContext(ctx).
		Where("name=?", planName).
		Preload("PlanQuotaDefaults", func(db *gorm.DB) *gorm.DB {
			return db.
				Joins("INNER JOIN resource_types ON plan_quota_defaults.resource_type_id = resource_types.id").
				Order("plan_quota_defaults.effective_date asc, resource_types.name asc")
		}).
		Preload("PlanQuotaDefaults.ResourceType").
		Preload("PlanRates", func(db *gorm.DB) *gorm.DB {
			return db.Order("effective_date asc")
		}).
		First(&plan).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}
	return &plan, nil
}

// CheckPlanNameExistenceGORM determines whether or not a subscription plan with a given name exists.
func CheckPlanNameExistenceGORM(ctx context.Context, db *gorm.DB, planName string) (bool, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan Name `%s`", planName)
	var err error

	// Query the database.
	var exists bool
	var plan = model.Plan{}
	err = db.Model(plan).
		Select("count(*) > 0").
		Where("name = ?", planName).
		Find(&exists).
		Error

	// Return the result.
	if err != nil {
		return false, errors.Wrap(err, wrapMsg)
	}
	return exists, nil
}

// CheckPlanExistenceGORM determines whether or not a subscription plan with the given identifier exists.
func CheckPlanExistenceGORM(ctx context.Context, db *gorm.DB, planID string) (bool, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan ID '%s'", planID)
	var err error

	var exists bool
	var plan = model.Plan{}
	err = db.Model(plan).
		Select("count(*) > 0").
		Where("id = ?", planID).
		Find(&exists).
		Error

	// Return the result.
	if err != nil {
		return false, errors.Wrap(err, wrapMsg)
	}
	return exists, nil
}

// GetPlanByIDGORM looks up the plan with the given identifier.
func GetPlanByIDGORM(ctx context.Context, db *gorm.DB, planID string) (*model.Plan, error) {
	wrapMsg := fmt.Sprintf("unable to look up plan ID '%s'", planID)
	var err error

	plan := model.Plan{ID: &planID}
	err = db.
		WithContext(ctx).
		Preload("PlanQuotaDefaults", func(db *gorm.DB) *gorm.DB {
			return db.
				Joins("INNER JOIN resource_types ON plan_quota_defaults.resource_type_id = resource_types.id").
				Order("plan_quota_defaults.effective_date asc, resource_types.name asc")
		}).
		Preload("PlanQuotaDefaults.ResourceType").
		Preload("PlanRates", func(db *gorm.DB) *gorm.DB {
			return db.Order("effective_date asc")
		}).
		First(&plan).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return &plan, nil
}

// GetActivePlanRateGORM returns the currently active rate for a subscription plan.
func GetActivePlanRateGORM(ctx context.Context, db *gorm.DB, planID string) (*model.PlanRate, error) {
	wrapMsg := fmt.Sprintf("unable to look up the active plan rate for '%s'", planID)
	var err error

	// The plan ID has to be an explicit condition: GORM only derives conditions from the primary key fields of the
	// destination struct, and PlanID isn't one of them.
	planRate := model.PlanRate{PlanID: &planID}
	err = db.
		WithContext(ctx).
		Where("plan_id = ?", planID).
		Where("effective_date <= CURRENT_TIMESTAMP").
		Order("effective_date desc").
		Limit(1).
		Find(&planRate).
		Error

	// Return the result.
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}
	return &planRate, nil
}

// GetActivePlanQuotaDefaultsGORM returns the currently active quota defaults for a subscription plan.
func GetActivePlanQuotaDefaultsGORM(ctx context.Context, db *gorm.DB, planID string) ([]model.PlanQuotaDefault, error) {
	wrapMsg := fmt.Sprintf("unable to look up the active plan quota defaults for '%s'", planID)
	var err error

	var planQuotaDefaults []model.PlanQuotaDefault
	err = db.
		WithContext(ctx).
		Select("DISTINCT ON (resource_type_id) resource_type_id", "id", "plan_id", "quota_value", "effective_date").
		Where("effective_date <= CURRENT_TIMESTAMP AND plan_id = ?", planID).
		Order("resource_type_id").
		Order("effective_date desc").
		Find(&planQuotaDefaults).
		Error
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return planQuotaDefaults, nil
}

// ListPlansGORM lists all of the plans that are currently available.
func ListPlansGORM(ctx context.Context, db *gorm.DB) ([]*model.Plan, error) {
	wrapMsg := "unable to list plans"
	var err error

	// List the plans.
	var plans []*model.Plan
	err = db.
		WithContext(ctx).
		Preload("PlanQuotaDefaults", func(db *gorm.DB) *gorm.DB {
			return db.
				Joins("INNER JOIN resource_types ON plan_quota_defaults.resource_type_id = resource_types.id").
				Order("plan_quota_defaults.effective_date asc, resource_types.name asc")
		}).
		Preload("PlanQuotaDefaults.ResourceType").
		Preload("PlanRates", func(db *gorm.DB) *gorm.DB {
			return db.Order("effective_date asc")
		}).
		Find(&plans).Error
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return plans, nil
}

func GetDefaultQuotaForPlan(ctx context.Context, db *gorm.DB, planID string) ([]model.PlanQuotaDefault, error) {
	wrapMsg := "unable to look up plan name"
	var err error

	var planQuotaDefaults []model.PlanQuotaDefault
	err = db.WithContext(ctx).Model(&planQuotaDefaults).Where("plan_id=?", planID).Scan(&planQuotaDefaults).Error
	//err = db.Find(&planQuotaDefaults).Error
	if err != nil {
		return nil, errors.Wrap(err, wrapMsg)
	}

	return planQuotaDefaults, nil
}

// GetPlansByName builds a map from plan name to plan details.
func GetPlansByName(ctx context.Context, db *gorm.DB) (map[string]*model.Plan, error) {
	plans, err := ListPlansGORM(ctx, db)
	if err != nil {
		return nil, err
	}

	// Build the map from the plan name to the plan details.
	result := make(map[string]*model.Plan)
	for _, plan := range plans {
		result[plan.Name] = plan
	}

	return result, nil
}

func SavePlanQuotaDefaultsGORM(ctx context.Context, db *gorm.DB, planQuotaDefaults []model.PlanQuotaDefault) error {
	wrapMsg := "unable to save the plan quota defaults"

	err := db.Create(planQuotaDefaults).Error
	if err != nil {
		return errors.Wrap(err, wrapMsg)
	}

	return nil
}

func SavePlanRatesGORM(ctx context.Context, db *gorm.DB, planRates []model.PlanRate) error {
	wrapMsg := "unable to save the plan rates"

	err := db.Create(planRates).Error
	if err != nil {
		return errors.Wrap(err, wrapMsg)
	}

	return nil
}
