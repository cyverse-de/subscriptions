package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// resource_types is reference data the migrations seed and resetDB deliberately
// preserves, so every test here removes its own rows. Without that, the extra
// type leaks into resource_types_list and every plan golden that names one.
// This registers the cleanup only; the test still creates the row through the
// API, which is what it is exercising.
func cleanupResourceType(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := testDB.Exec(`DELETE FROM resource_types WHERE name = $1`, name); err != nil {
			t.Fatalf("unable to remove the resource type this test added: %s", err)
		}
	})
}

// resourceTypeIDFor returns the UUID of a resource type by name.
func resourceTypeIDFor(t *testing.T, name string) string {
	t.Helper()
	return queryString(t, `SELECT id::text FROM resource_types WHERE name = $1`, name)
}

func TestAddResourceType(t *testing.T) {
	resetDB(t)
	cleanupResourceType(t, "test.widgets")

	t.Run("creates the resource type", func(t *testing.T) {
		body := `{"name": "test.widgets", "unit": "widgets", "consumable": true}`
		assertGolden(t, "v1_resource_type_created",
			do(t, http.MethodPost, "/v1/resource-types", body), http.StatusOK)

		stored := queryInt(t,
			`SELECT count(*) FROM resource_types WHERE name = $1 AND unit = $2`, "test.widgets", "widgets")
		if stored != 1 {
			t.Errorf("stored resource_types rows = %d, want 1", stored)
		}
	})

	// SaveResourceType maps a duplicate to db.ErrResourceTypeConflict, which the
	// handler turns into a 409 rather than letting the constraint violation
	// surface as a 500.
	t.Run("a duplicate name is a conflict", func(t *testing.T) {
		body := `{"name": "test.widgets", "unit": "widgets", "consumable": true}`
		assertGolden(t, "v1_resource_type_conflict",
			do(t, http.MethodPost, "/v1/resource-types", body), http.StatusConflict)
	})

	t.Run("a missing unit is refused", func(t *testing.T) {
		body := `{"name": "test.nounit"}`
		assertGolden(t, "v1_resource_type_missing_unit",
			do(t, http.MethodPost, "/v1/resource-types", body), http.StatusBadRequest)

		stored := queryInt(t, `SELECT count(*) FROM resource_types WHERE name = $1`, "test.nounit")
		if stored != 0 {
			t.Errorf("stored resource_types rows = %d, want 0", stored)
		}
	})
}

func TestGetResourceTypeDetails(t *testing.T) {
	resetDB(t)

	const cpuHoursID = "99e3bc7e-950a-11ec-84a4-406c8f3e9cbb"

	t.Run("returns a seeded resource type", func(t *testing.T) {
		assertGolden(t, "v1_resource_type_get",
			do(t, http.MethodGet, "/v1/resource-types/"+cpuHoursID, ""), http.StatusOK)
	})

	// gorm.ErrRecordNotFound is mapped to 404 here. goqu reports absence as
	// sql.ErrNoRows instead, so this is the assertion that catches a rewrite
	// turning a clean 404 into a 500.
	t.Run("an unknown ID is a not-found", func(t *testing.T) {
		assertGolden(t, "v1_resource_type_not_found",
			do(t, http.MethodGet, "/v1/resource-types/00000000-0000-0000-0000-000000000000", ""),
			http.StatusNotFound)
	})
}

func TestUpdateResourceType(t *testing.T) {
	resetDB(t)
	cleanupResourceType(t, "test.gadgets")

	body := `{"name": "test.gadgets", "unit": "gadgets", "consumable": false}`
	if got := do(t, http.MethodPost, "/v1/resource-types", body); got.status != http.StatusOK {
		t.Fatalf("unable to create the resource type: status %d, body %s", got.status, got.body)
	}
	id := resourceTypeIDFor(t, "test.gadgets")

	updated := `{"name": "test.gadgets", "unit": "gizmos", "consumable": true}`
	assertGolden(t, "v1_resource_type_updated",
		do(t, http.MethodPut, fmt.Sprintf("/v1/resource-types/%s", id), updated), http.StatusOK)

	unit := queryString(t, `SELECT unit FROM resource_types WHERE id = $1`, id)
	if unit != "gizmos" {
		t.Errorf("stored unit = %q, want \"gizmos\"", unit)
	}
}
