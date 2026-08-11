package model

import (
	"fmt"
	"time"
)

// Plan
//
// swagger:model
type Plan struct {
	// The plan identifier
	//
	// readOnly: true
	ID *string `db:"id" json:"id,omitempty"`

	// The plan name
	Name string `db:"name" json:"name,omitempty"`

	// A brief description of the plan
	//
	// required: true
	Description string `db:"description" json:"description,omitempty"`

	// The default quota values associated with the plan
	PlanQuotaDefaults []PlanQuotaDefault `db:"-" json:"plan_quota_defaults,omitempty"`

	// The rates associated with the plan.
	PlanRates []PlanRate `db:"-" json:"plan_rates,omitempty"`
}

// Returns the currently active rate for a subscription plan. The active plan rate is the plan with the most recent
// effective timestamp that occurs at or befor the curren time. This function assumes that the plan quota defaults are
// sorted in ascending order by effective date.
func (p *Plan) GetActivePlanRate() (*PlanRate, error) {
	currentTime := time.Now()

	// Find the active plan rate.
	var activePlanRate *PlanRate
	for _, pr := range p.PlanRates {
		if pr.EffectiveDate.After(currentTime) {
			break
		}
		activePlanRate = &pr
	}

	// It's an error for a plan not to have an active rate.
	if activePlanRate == nil {
		return nil, fmt.Errorf("no active rate found for subscription plan %s", *p.ID)
	}

	return activePlanRate, nil
}

// GetDefaultQuotaValue returns the default quota value associated with the resource type with the given name. This
// function assumes that the plan quota defaults are sorted in ascending order by effective date.
func (p *Plan) GetDefaultQuotaValue(resourceTypeName string) float64 {

	// Find the active plan quota default value for the given resource type.
	pqd := p.GetDefaultQuotaValues()[resourceTypeName]
	if pqd == nil {
		return 0
	}
	return pqd.QuotaValue
}

// GetDefaultQuotaValues returns the active quota values for a plan. This function assumes that the plan quota defaults
// are sorted in ascending order by effective date.
func (p *Plan) GetDefaultQuotaValues() map[string]*PlanQuotaDefault {
	currentTime := time.Now()

	// Find the active plan quota defaults for each resource type.
	result := make(map[string]*PlanQuotaDefault)
	for _, planQuotaDefault := range p.PlanQuotaDefaults {
		if planQuotaDefault.EffectiveDate.After(currentTime) {
			break
		}
		result[planQuotaDefault.ResourceType.Name] = &planQuotaDefault
	}

	return result
}

// PlanQuotaDefault define the structure for an Api Plan and Quota.
//
// Associations carry db:"-" because the goqu query layer loads each of them with its own query and assigns it in Go,
// the way GORM's Preload did; leaving them mapped would make goqu look for columns no query selects.
type PlanQuotaDefault struct {
	// The plan quota default identifier
	//
	// readOnly: true
	ID *string `db:"id" json:"id,omitempty"`

	// The plan ID
	PlanID *string `db:"plan_id" json:"-"`

	// The default quota value
	//
	// required: true
	QuotaValue float64 `db:"quota_value" json:"quota_value,omitempty"`

	// The resource type ID
	ResourceTypeID *string `db:"resource_type_id" json:"-"`

	// The resource type
	//
	// required: true
	ResourceType ResourceType `db:"-" json:"resource_type,omitempty"`

	// The effective date
	//
	// required: true
	EffectiveDate time.Time `db:"effective_date" json:"effective_date,omitempty"`
}

// PlanRates
//
// swagger:model
type PlanRate struct {
	// The plan rate identifier
	//
	// readOnly: true
	ID *string `db:"id" json:"id,omitempty"`

	// The plan ID
	PlanID *string `db:"plan_id" json:"-"`

	// The date that the plan rate becomes effective
	EffectiveDate time.Time `db:"effective_date" json:"effective_date,omitempty"`

	// The rate
	Rate float64 `db:"rate" json:"rate"`
}

// Subscription define the structure for the API subscription.
//
// swagger:model
type Subscription struct {
	// The subscription identifier
	//
	// readOnly: true
	ID *string `db:"id" json:"id,omitempty"`

	// The date and time the subscription becomes active
	EffectiveStartDate *time.Time `db:"effective_start_date" json:"effective_start_date,omitempty"`

	// The date and time the subscription expires
	EffectiveEndDate *time.Time `db:"effective_end_date" json:"effective_end_date,omitempty"`

	// The user identifier
	UserID *string `db:"user_id" json:"-"`

	// The user associated with the subscription
	User *User `db:"-" json:"user,omitempty"`

	// The identifier of the plan associated with the subscription
	PlanID *string `db:"plan_id" json:"-"`

	// The plan associated with the subscription
	Plan *Plan `db:"-" json:"plan,omitempty"`

	// The quotas associated with the subscription
	Quotas []Quota `db:"-" json:"quotas"`

	// The recorded usage amounts associated with the subscription
	Usages []Usage `db:"-" json:"usages"`

	// True if the user paid for the subscription.
	Paid bool `db:"paid" json:"paid"`

	// The ID of the plan rate at the time the subscription was created.
	PlanRateID *string `db:"plan_rate_id" json:"-"`

	// The plan rate at the time the subscription was created.
	PlanRate *PlanRate `db:"-" json:"plan_rate,omitempty"`
}

// GetCurrentUsageValue returns the current usage value for the resource type with the given resource type ID. Be
// careful to ensure that all user plan details have been loaded before calling this function.
func (up *Subscription) GetCurrentUsageValue(resourceTypeID string) float64 {
	var usageValue float64
	for _, usage := range up.Usages {
		if *usage.ResourceTypeID == resourceTypeID {
			usageValue = usage.Usage
		}
	}
	return usageValue
}
