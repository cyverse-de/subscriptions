package httpmodel

import (
	"fmt"
	"time"

	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
)

// Note: the names in the comments may deviate a bit from the actual structure names in order to avoid producing
// confusing Swagger docs.

// A plan quota default key.
type PlanQuotaDefaultKey struct {
	ResourceTypeName string
	EffectiveDate    int64
}

// KeyFromPlanQuotaDefault generates a plan quota default key from a PlanQuotaDefault object.
func KeyFromPlanQuotaDefault(planQuotaDefault model.PlanQuotaDefault) PlanQuotaDefaultKey {
	return PlanQuotaDefaultKey{
		ResourceTypeName: planQuotaDefault.ResourceType.Name,
		EffectiveDate:    planQuotaDefault.EffectiveDate.UnixMilli(),
	}
}

// KeyFromNewPlanQuotaDefault generates a plan quota defual key from a NewPlanQuotaDefaultObject.
func KeyFromNewPlanQuotaDefault(newPlanQuotaDefault NewPlanQuotaDefault) PlanQuotaDefaultKey {
	return PlanQuotaDefaultKey{
		ResourceTypeName: newPlanQuotaDefault.ResourceType.Name,
		EffectiveDate:    newPlanQuotaDefault.EffectiveDate.UnixMilli(),
	}
}

// NewPlan
//
// swagger:model
type NewPlan struct {

	// The plan name
	//
	// required: true
	Name string `json:"name"`

	// A brief description of the plan
	//
	// required: true
	Description string `json:"description"`

	// The default quota values associated with the plan
	PlanQuotaDefaults []NewPlanQuotaDefault `json:"plan_quota_defaults"`

	// The rates associated with the plan
	PlanRates []NewPlanRate `json:"plan_rates"`
}

// Validate verifies that all the required fields in a new plan are present.
func (p NewPlan) Validate() error {
	var err error

	// The plan name and description are both required.
	if p.Name == "" {
		return fmt.Errorf("a plan name is required")
	}
	if p.Description == "" {
		return fmt.Errorf("a plan description is required")
	}

	// Validate each of the default quota values.
	for _, d := range p.PlanQuotaDefaults {
		err = d.Validate()
		if err != nil {
			return err
		}
	}

	// Verify that the resource type and effective date are unique for all of the plan quota defaults.
	uniquePlanQuotaDefaults := make(map[PlanQuotaDefaultKey]bool)
	for _, d := range p.PlanQuotaDefaults {
		key := KeyFromNewPlanQuotaDefault(d)
		if uniquePlanQuotaDefaults[key] {
			return fmt.Errorf("multiple plan quota defaults found with the same resource type and effective date")
		}
		uniquePlanQuotaDefaults[key] = true
	}

	// Verify each of the plan rates.
	for _, pr := range p.PlanRates {
		err = pr.Validate()
		if err != nil {
			return err
		}
	}

	// Verify that the effective date it unique for all of the plan rates.
	uniquePlanRates := make(map[int64]bool)
	for _, pr := range p.PlanRates {
		if uniquePlanRates[pr.EffectiveDate.UnixMilli()] {
			return fmt.Errorf("multiple plan rates found with the same effective date")
		}
		uniquePlanRates[pr.EffectiveDate.UnixMilli()] = true
	}

	return nil
}

// ToDBModel converts a plan to its equivalent database model.
func (p NewPlan) ToDBModel() model.Plan {

	// Convert each of the plan quota defaults.
	planQuotaDefaults := make([]model.PlanQuotaDefault, len(p.PlanQuotaDefaults))
	for i, planQuotaDefault := range p.PlanQuotaDefaults {
		planQuotaDefaults[i] = planQuotaDefault.ToDBModel()
	}

	// Convert each of the plan rates.
	planRates := make([]model.PlanRate, len(p.PlanRates))
	for i, planRate := range p.PlanRates {
		planRates[i] = planRate.ToDBModel()
	}

	return model.Plan{
		Name:              p.Name,
		Description:       p.Description,
		PlanQuotaDefaults: planQuotaDefaults,
		PlanRates:         planRates,
	}
}

// NewPlanQuotaDefaultList
//
// swagger:model
type NewPlanQuotaDefaultList struct {

	// The list of plan quota default values.
	//
	// required: true
	PlanQuotaDefaults []NewPlanQuotaDefault `json:"plan_quota_defaults"`
}

// Validate verifies that all of the required fields in each quota default are present.
func (pqdl NewPlanQuotaDefaultList) Validate() error {

	// Validate each of the quota default values.
	for _, d := range pqdl.PlanQuotaDefaults {
		err := d.Validate()
		if err != nil {
			return err
		}
	}

	// Verify that the resource type and effective date are unique for all of the plan quota defaults.
	uniquePlanQuotaDefaults := make(map[PlanQuotaDefaultKey]bool)
	for _, d := range pqdl.PlanQuotaDefaults {
		key := KeyFromNewPlanQuotaDefault(d)
		if uniquePlanQuotaDefaults[key] {
			return fmt.Errorf("multiple plan quota defaults found with the same resource type and effective date")
		}
		uniquePlanQuotaDefaults[key] = true
	}

	return nil
}

// ToDBModel converts a list of plan quota defaults to their equivalent database models and returns them without the
// wrapper object.
func (pqdl NewPlanQuotaDefaultList) ToDBModel() []model.PlanQuotaDefault {

	// Convert each of the plan quota defaults.
	planQuotaDefaults := make([]model.PlanQuotaDefault, len(pqdl.PlanQuotaDefaults))
	for i, planQuotaDefault := range pqdl.PlanQuotaDefaults {
		planQuotaDefaults[i] = planQuotaDefault.ToDBModel()
	}

	return planQuotaDefaults
}

// NewPlanQuotaDefault
//
// swagger:model
type NewPlanQuotaDefault struct {

	// The plan ID
	PlanID *string `json:"-"`

	// The default quota value
	//
	// required: true
	QuotaValue float64 `json:"quota_value"`

	// The resource type ID
	ResourceTypeID *string `json:"-"`

	// The resource type
	//
	// required: true
	ResourceType NewPlanResourceType `json:"resource_type"`

	// The effective date
	//
	// required: true
	EffectiveDate time.Time `json:"effective_date"`
}

// Validate verifies that all the required fields in a quota default are present.
func (d NewPlanQuotaDefault) Validate() error {

	// The default quota value is required.
	if d.QuotaValue <= 0 {
		return fmt.Errorf("default quota values must be specified and greater than zero")
	}

	// The effective date has to be specified.
	if d.EffectiveDate.IsZero() {
		return fmt.Errorf("the effective date of the plan quota default must be specified")
	}

	return d.ResourceType.Validate()
}

// ToDBModel converts a plan quota default to its equivalent database model.
func (d NewPlanQuotaDefault) ToDBModel() model.PlanQuotaDefault {
	return model.PlanQuotaDefault{
		QuotaValue:    d.QuotaValue,
		ResourceType:  d.ResourceType.ToDBModel(),
		EffectiveDate: d.EffectiveDate,
	}
}

// NewPlanResourceType
//
// swagger:model
type NewPlanResourceType struct {

	// The resource type name
	//
	// required: true
	Name string `json:"name"`
}

// Validate verifies that all the required fields in a resource type are present.
func (rt NewPlanResourceType) Validate() error {

	// The resource type name is required.
	if rt.Name == "" {
		return fmt.Errorf("the resource type name is required")
	}

	return nil
}

// ToDBModel converts a resource type to its equivalent database model.
func (rt NewPlanResourceType) ToDBModel() model.ResourceType {
	return model.ResourceType{Name: rt.Name}
}

// NewPlanRate
//
// swagger:model
type NewPlanRate struct {

	// The date when the plan becomes effective
	//
	// required: true
	EffectiveDate time.Time `json:"effective_date"`

	// The rate
	//
	// required: true
	Rate float64 `json:"rate"`
}

// Validate verifies that all plan rate fields are valid.
func (pr NewPlanRate) Validate() error {

	// The rate can't be negative.
	if pr.Rate < 0 {
		return fmt.Errorf("the plan rate must not be less than zero")
	}

	// The effective date has to be specified.
	if pr.EffectiveDate.IsZero() {
		return fmt.Errorf("the effective date of the plan rate must be specified")
	}

	return nil
}

// ToDBModel converts a resource type to its equivalent database model.
func (pr NewPlanRate) ToDBModel() model.PlanRate {
	return model.PlanRate{
		EffectiveDate: pr.EffectiveDate,
		Rate:          pr.Rate,
	}
}

// NewPlanRateList
//
// swagger:model
type NewPlanRateList struct {

	// The list of plan rates.
	//
	// required: true
	PlanRates []NewPlanRate `json:"plan_rates"`
}

func (prl *NewPlanRateList) Validate() error {

	// Validate each of the plan rates.
	for _, pr := range prl.PlanRates {
		err := pr.Validate()
		if err != nil {
			return err
		}
	}

	// Check for multiple plan rates with the same effective date.
	uniquePlanRates := make(map[int64]bool)
	for _, pr := range prl.PlanRates {
		if uniquePlanRates[pr.EffectiveDate.UnixMilli()] {
			return fmt.Errorf("multiple plan rates found with the same effective date")
		}
		uniquePlanRates[pr.EffectiveDate.UnixMilli()] = true
	}

	return nil
}

func (prl *NewPlanRateList) ToDBModel() []model.PlanRate {

	// Convert each plan rate in the list to its corresponding database model.
	planRates := make([]model.PlanRate, len(prl.PlanRates))
	for i, pr := range prl.PlanRates {
		planRates[i] = pr.ToDBModel()
	}

	return planRates
}
