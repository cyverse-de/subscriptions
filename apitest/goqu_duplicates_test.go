package apitest

import (
	"net/http"
	"testing"
)

// These routes duplicate a /v1 route and have no verified caller in terrain,
// apps, app-exposer, resource-usage-api or data-usage-api. They are kept
// deliberately, so they are covered deliberately. Pairing each golden with its
// /v1 counterpart is what a later decision about collapsing them would compare.

func TestGoquPlanReads(t *testing.T) {
	resetDB(t)

	t.Run("list plans", func(t *testing.T) {
		assertGolden(t, "goqu_plans_list", do(t, http.MethodGet, "/plans", ""), http.StatusOK)
	})

	t.Run("get a plan by ID", func(t *testing.T) {
		assertGolden(t, "goqu_plan_get",
			do(t, http.MethodGet, "/plans/"+basicPlanID, ""), http.StatusOK)
	})
}

func TestGoquAddPlan(t *testing.T) {
	resetDB(t)

	t.Cleanup(func() {
		if _, err := testDB.Exec(`DELETE FROM plans WHERE name = $1`, "Goqu Test Plan"); err != nil {
			t.Fatalf("unable to remove the plan this test added: %s", err)
		}
	})

	body := `{"plan": {"name": "Goqu Test Plan", "description": "added through the goqu route"}}`
	assertGolden(t, "goqu_plan_added", do(t, http.MethodPut, "/plans", body), http.StatusOK)

	stored := queryInt(t, `SELECT count(*) FROM plans WHERE name = $1`, "Goqu Test Plan")
	if stored != 1 {
		t.Errorf("stored plans rows = %d, want 1", stored)
	}
}

// PUT /quotas binds the resource type straight from the request body without
// looking it up by name the way PUT /plans does (app/quotas.go:33 vs.
// app/plans.go:70). A body that identifies the resource type by name alone —
// which is all the wire contract in the task brief documents — leaves
// ResourceType.Uuid empty and the insert fails with a Postgres uuid syntax
// error. See docs/superpowers/findings-goqu-unification.md.
func TestGoquAddQuota(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	subscriptionID := subscriptionIDFor(t, trimmedUser)
	body := `{"quota": {"quota": 500, "subscription_id": "` + subscriptionID + `",` +
		`"resource_type": {"name": "cpu.hours", "unit": "cpu hours"}}}`
	assertGolden(t, "goqu_quota_added", do(t, http.MethodPut, "/quotas", body), http.StatusInternalServerError)
}

func TestGoquAddUsage(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	body := `{"username": "` + trimmedUser + `", "resource_name": "cpu.hours",` +
		`"update_type": "SET", "usage_value": 77, "resource_unit": "cpu hours"}`
	assertGolden(t, "goqu_usage_added",
		do(t, http.MethodPut, "/users/"+trimmedUser+"/usages", body), http.StatusOK)
}

func TestGoquUserUpdates(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 12)

	assertGolden(t, "goqu_user_updates",
		do(t, http.MethodGet, "/users/"+trimmedUser+"/updates", ""), http.StatusOK)
}
