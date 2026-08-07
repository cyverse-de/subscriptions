package apitest

import (
	"net/http"
	"testing"
)

// POST /v1/plans/{plan_id}/quota-defaults is the only implementation of this
// operation now: the service's own POST /quotas/defaults was a routed stub that
// always answered "not implemented", and has been removed. These tests exist so
// that removal is backed by evidence the surviving route works.
func TestAddPlanQuotaDefaults(t *testing.T) {
	resetDB(t)

	const basicPlanID = "99e47c22-950a-11ec-84a4-406c8f3e9cbb"
	const cpuHoursID = "99e3bc7e-950a-11ec-84a4-406c8f3e9cbb"

	// plan_quota_defaults is reference data seeded by the migrations, so
	// resetDB deliberately leaves it alone and this test has to undo its own
	// write. Without this, the extra default leaks into every golden that
	// asserts a plan's contents.
	const distinctiveQuota = 4321
	t.Cleanup(func() {
		if _, err := testDB.Exec(
			`DELETE FROM plan_quota_defaults WHERE plan_id = $1 AND quota_value = $2`,
			basicPlanID, distinctiveQuota,
		); err != nil {
			t.Fatalf("unable to remove the quota default this test added: %s", err)
		}
	})

	t.Run("adds a default to an existing plan", func(t *testing.T) {
		body := `{"plan_quota_defaults": [{"quota_value": 4321,` +
			`"resource_type": {"uuid": "` + cpuHoursID + `", "name": "cpu.hours", "unit": "cpu hours"},` +
			`"effective_date": "` + effectiveDate + `"}]}`
		got := do(t, http.MethodPost, "/v1/plans/"+basicPlanID+"/quota-defaults", body)
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
		}

		stored := queryFloat(t, `
			SELECT pqd.quota_value
			  FROM plan_quota_defaults pqd
			  JOIN resource_types rt ON rt.id = pqd.resource_type_id
			 WHERE pqd.plan_id = $1 AND rt.name = 'cpu.hours' AND pqd.quota_value = 4321`,
			basicPlanID)
		if stored != 4321 {
			t.Errorf("stored quota default = %v, want 4321", stored)
		}
	})

	t.Run("a zero quota value is refused", func(t *testing.T) {
		body := `{"plan_quota_defaults": [{"quota_value": 0,` +
			`"resource_type": {"uuid": "` + cpuHoursID + `", "name": "cpu.hours", "unit": "cpu hours"},` +
			`"effective_date": "` + effectiveDate + `"}]}`
		got := do(t, http.MethodPost, "/v1/plans/"+basicPlanID+"/quota-defaults", body)
		if got.status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body: %s", got.status, got.body)
		}
	})

	// The stub this replaced lived at POST /quotas/defaults on the service's own
	// route tree. It is gone, and nothing should resurrect it.
	t.Run("the removed stub route is no longer served", func(t *testing.T) {
		got := do(t, http.MethodPost, "/quotas/defaults", `{}`)
		if got.status != http.StatusNotFound && got.status != http.StatusMethodNotAllowed {
			t.Errorf("POST /quotas/defaults status = %d, want 404 or 405; body: %s", got.status, got.body)
		}
	})
}
