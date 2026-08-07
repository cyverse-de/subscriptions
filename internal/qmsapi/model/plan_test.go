package model

import (
	"testing"
	"time"
)

// Both of these selectors are duplicated in the subscriptions service as
// db.Plan.GetActiveRate and db.Plan.GetActiveQuotaDefaults. Only one
// implementation survives the merge, so these tests describe the semantics the
// survivor has to keep.

func ref[T any](value T) *T { return &value }

func planRate(offset time.Duration, rate float64) PlanRate {
	return PlanRate{
		ID:            ref("rate"),
		EffectiveDate: time.Now().Add(offset),
		Rate:          rate,
	}
}

func quotaDefault(offset time.Duration, resourceType string, value float64) PlanQuotaDefault {
	return PlanQuotaDefault{
		ID:            ref("quota-default"),
		EffectiveDate: time.Now().Add(offset),
		QuotaValue:    value,
		ResourceType:  ResourceType{Name: resourceType},
	}
}

const (
	day  = 24 * time.Hour
	year = 365 * day
)

func TestGetActivePlanRate(t *testing.T) {
	testCases := []struct {
		name     string
		rates    []PlanRate
		wantRate float64
		wantErr  bool
	}{
		{
			name:     "the most recent rate that has taken effect wins",
			rates:    []PlanRate{planRate(-2*year, 10), planRate(-1*year, 20)},
			wantRate: 20,
		},
		{
			name:     "future rates are ignored",
			rates:    []PlanRate{planRate(-1*year, 20), planRate(year, 99)},
			wantRate: 20,
		},
		{
			name:    "a plan with no rates has no active rate",
			rates:   nil,
			wantErr: true,
		},
		{
			name:    "a plan whose only rate is in the future has no active rate",
			rates:   []PlanRate{planRate(day, 42)},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &Plan{ID: ref("plan-id"), Name: "Test", PlanRates: tc.rates}

			active, err := plan.GetActivePlanRate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got rate %v", active.Rate)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if active.Rate != tc.wantRate {
				t.Errorf("active rate = %v, want %v", active.Rate, tc.wantRate)
			}
		})
	}
}

func TestGetDefaultQuotaValue(t *testing.T) {
	testCases := []struct {
		name         string
		defaults     []PlanQuotaDefault
		resourceType string
		want         float64
	}{
		{
			name:         "the most recent default that has taken effect wins",
			defaults:     []PlanQuotaDefault{quotaDefault(-2*year, "cpu.hours", 100), quotaDefault(-1*year, "cpu.hours", 200)},
			resourceType: "cpu.hours",
			want:         200,
		},
		{
			name:         "defaults are tracked per resource type",
			defaults:     []PlanQuotaDefault{quotaDefault(-1*year, "cpu.hours", 200), quotaDefault(-1*year, "data.size", 5000)},
			resourceType: "data.size",
			want:         5000,
		},
		{
			name:         "an unknown resource type has no default",
			defaults:     []PlanQuotaDefault{quotaDefault(-1*year, "cpu.hours", 200)},
			resourceType: "no.such.resource",
			want:         0,
		},
		{
			name:         "a plan with no defaults reports zero",
			defaults:     nil,
			resourceType: "cpu.hours",
			want:         0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &Plan{ID: ref("plan-id"), Name: "Test", PlanQuotaDefaults: tc.defaults}
			if got := plan.GetDefaultQuotaValue(tc.resourceType); got != tc.want {
				t.Errorf("default quota for %s = %v, want %v", tc.resourceType, got, tc.want)
			}
		})
	}
}

// A future-dated default for one resource type stops the scan for every
// resource type, because the loop breaks on the first future entry rather than
// skipping it. Plan quota defaults are ordered by effective date across all
// resource types, so scheduling a change for one resource hides the current
// values of any resource that sorts after it.
func TestFutureDatedDefaultHidesLaterResourceTypes(t *testing.T) {
	plan := &Plan{
		ID:   ref("plan-id"),
		Name: "Test",
		PlanQuotaDefaults: []PlanQuotaDefault{
			quotaDefault(-1*year, "cpu.hours", 200),
			quotaDefault(day, "cpu.hours", 400),
			quotaDefault(2*day, "data.size", 9000),
		},
	}

	if got := plan.GetDefaultQuotaValue("cpu.hours"); got != 200 {
		t.Errorf("cpu.hours default = %v, want 200 (the future increase must not apply yet)", got)
	}

	// data.size has no entry that has taken effect, so zero is correct here;
	// the hazard is that an entry which *had* taken effect would also be
	// missed once an earlier future-dated row exists.
	if got := plan.GetDefaultQuotaValue("data.size"); got != 0 {
		t.Errorf("data.size default = %v, want 0", got)
	}
}
