package apitest

import (
	"net/http"
	"testing"
)

const (
	basicPlanID   = "99e47c22-950a-11ec-84a4-406c8f3e9cbb"
	unknownPlanID = "00000000-0000-0000-0000-000000000000"
)

// The active rate and active quota defaults are computed from migration-seeded
// reference data, so these responses are fully deterministic. Both pick the most
// recent row whose effective_date has already passed.
func TestActivePlanRateAndQuotaDefaults(t *testing.T) {
	resetDB(t)

	testCases := []struct {
		name       string
		golden     string
		path       string
		wantStatus int
	}{
		{
			name:       "active rate",
			golden:     "v1_plan_active_rate",
			path:       "/v1/plans/" + basicPlanID + "/active-rate",
			wantStatus: http.StatusOK,
		},
		{
			name:       "active quota defaults",
			golden:     "v1_plan_active_quota_defaults",
			path:       "/v1/plans/" + basicPlanID + "/active-quota-defaults",
			wantStatus: http.StatusOK,
		},
		{
			name:       "active rate for an unknown plan",
			golden:     "v1_plan_active_rate_unknown",
			path:       "/v1/plans/" + unknownPlanID + "/active-rate",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "active quota defaults for an unknown plan",
			golden:     "v1_plan_active_quota_defaults_unknown",
			path:       "/v1/plans/" + unknownPlanID + "/active-quota-defaults",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertGolden(t, tc.golden, do(t, http.MethodGet, tc.path, ""), tc.wantStatus)
		})
	}
}

// Creating a plan writes to plans, plan_quota_defaults and plan_rates, all of
// which resetDB preserves as reference data, so this removes its own rows.
func TestAddPlan(t *testing.T) {
	resetDB(t)

	const cpuHoursID = "99e3bc7e-950a-11ec-84a4-406c8f3e9cbb"

	t.Cleanup(func() {
		if _, err := testDB.Exec(
			`DELETE FROM plans WHERE name = $1`, "Test Plan",
		); err != nil {
			t.Fatalf("unable to remove the plan this test added: %s", err)
		}
	})

	t.Run("creates the plan", func(t *testing.T) {
		body := `{"name": "Test Plan", "description": "a plan for tests",` +
			`"plan_quota_defaults": [{"quota_value": 1234,` +
			`"resource_type": {"uuid": "` + cpuHoursID + `", "name": "cpu.hours", "unit": "cpu hours"},` +
			`"effective_date": "` + effectiveDate + `"}],` +
			`"plan_rates": [{"effective_date": "` + effectiveDate + `", "rate": 9.99}]}`
		assertGolden(t, "v1_plan_created", do(t, http.MethodPost, "/v1/plans", body), http.StatusOK)

		stored := queryInt(t, `SELECT count(*) FROM plans WHERE name = $1`, "Test Plan")
		if stored != 1 {
			t.Errorf("stored plans rows = %d, want 1", stored)
		}
	})

	t.Run("a missing name is refused", func(t *testing.T) {
		body := `{"description": "a plan with no name"}`
		assertGolden(t, "v1_plan_no_name", do(t, http.MethodPost, "/v1/plans", body), http.StatusBadRequest)
	})
}

func TestAddPlanRates(t *testing.T) {
	resetDB(t)

	// A distinctive rate so the cleanup can find exactly this test's row, in the
	// same spirit as the distinctiveQuota in plans_test.go.
	const distinctiveRate = 87.65
	t.Cleanup(func() {
		if _, err := testDB.Exec(
			`DELETE FROM plan_rates WHERE plan_id = $1 AND rate = $2`, basicPlanID, distinctiveRate,
		); err != nil {
			t.Fatalf("unable to remove the plan rate this test added: %s", err)
		}
	})

	body := `{"plan_rates": [{"effective_date": "` + effectiveDate + `", "rate": 87.65}]}`
	assertGolden(t, "v1_plan_rates_added",
		do(t, http.MethodPost, "/v1/plans/"+basicPlanID+"/rates", body), http.StatusOK)

	stored := queryInt(t,
		`SELECT count(*) FROM plan_rates WHERE plan_id = $1 AND rate = $2`, basicPlanID, distinctiveRate)
	if stored != 1 {
		t.Errorf("stored plan_rates rows = %d, want 1", stored)
	}
}
