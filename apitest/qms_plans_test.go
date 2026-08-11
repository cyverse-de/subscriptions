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

// A plan whose only rate takes effect in the future has no active rate. The
// route used to answer 200 with a zero-valued model.PlanRate carrying nothing
// but the plan ID, so a billing caller reading result.rate charged zero instead
// of erroring. It now reports the absence as a 404, which is what every other
// single-row lookup in the converted layer does.
func TestActivePlanRateWithNoRateInEffect(t *testing.T) {
	resetDB(t)

	const planName = "Future Rate Plan"

	// plans and plan_rates are reference data resetDB preserves, so this test
	// removes its own rows. The plan_rates foreign key cascades on delete, so
	// dropping the plan takes its rate with it.
	t.Cleanup(func() {
		if _, err := testDB.Exec(`DELETE FROM plans WHERE name = $1`, planName); err != nil {
			t.Fatalf("unable to remove the plan this test added: %s", err)
		}
	})

	var planID string
	if err := testDB.QueryRow(
		`INSERT INTO plans ("name", description) VALUES ($1, $2) RETURNING id`,
		planName, "its only rate takes effect a year from now",
	).Scan(&planID); err != nil {
		t.Fatalf("unable to add the plan: %s", err)
	}
	if _, err := testDB.Exec(`
		INSERT INTO plan_rates (plan_id, effective_date, rate)
		VALUES ($1, CURRENT_TIMESTAMP + interval '1 year', $2)`, planID, 42.0,
	); err != nil {
		t.Fatalf("unable to add the future-dated plan rate: %s", err)
	}

	got := do(t, http.MethodGet, "/v1/plans/"+planID+"/active-rate", "")
	if got.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", got.status, got.body)
	}

	// The plan itself exists, so the message has to distinguish "no rate in
	// effect" from the unknown-plan 404 the same route returns.
	body := mustDecode(t, got)
	errMsg, _ := body["error"].(string)
	wantMsg := "no active rate found for plan ID " + planID
	if errMsg != wantMsg {
		t.Errorf("error = %q, want %q", errMsg, wantMsg)
	}
	if _, reported := body["result"]; reported {
		t.Errorf("the response still carries a result: %s", got.body)
	}
}

// A driver error must not reach the response body. The unscannable resource
// type makes the listing's scan fail, which is the same class of failure --
// lib/pq's own message -- that a constraint violation produces on a write path.
// The caller learns which operation failed and nothing about the schema.
func TestResourceTypeListingDatabaseErrorIsNotLeaked(t *testing.T) {
	resetDB(t)
	unscannableResourceType(t, "test.unscannable")

	got := do(t, http.MethodGet, "/v1/resource-types", "")
	if got.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", got.status, got.body)
	}
	assertNoDatabaseDetail(t, got)

	// The operation that failed is the part of the old message worth keeping.
	errMsg, _ := mustDecode(t, got)["error"].(string)
	if errMsg != "unable to list the resource types" {
		t.Errorf("error = %q, want %q", errMsg, "unable to list the resource types")
	}
}
