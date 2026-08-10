package apitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/cyverse-de/subscriptions/db"
	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
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

	// A not-found row is mapped to 404 here. goqu reports absence as
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

// A failed homonym lookup used to be reported as 409 Conflict along with the raw
// database error, so a server fault reached the caller as "you picked a name
// that's taken". Only a real collision is a conflict. Nothing in the golden set
// reaches this path, which is why it stayed wrong: the lookup has to fail while
// the GetResourceTypeByID above it succeeds, so the failure is injected into the
// row the homonym query reads and nothing else.
func TestUpdateResourceTypeReportsAFailedHomonymLookup(t *testing.T) {
	resetDB(t)
	unscannableResourceType(t, "test.unscannable")

	cpuHoursID := resourceTypeIDFor(t, "cpu.hours")
	updated := `{"name": "test.unscannable", "unit": "gizmos", "consumable": true}`
	got := do(t, http.MethodPut, "/v1/resource-types/"+cpuHoursID, updated)
	if got.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500; body: %s", got.status, got.body)
	}

	// The update has to have been rolled back along with the failure.
	name := queryString(t, `SELECT name FROM resource_types WHERE id = $1`, cpuHoursID)
	if name != "cpu.hours" {
		t.Errorf("the resource type name is now %q, want \"cpu.hours\"", name)
	}
}

// An empty listing has to marshal as [] rather than null, which no golden can
// check: resource_types is seeded reference data, so every recorded response is
// non-empty. The two layers disagree by default — GORM's Find replaced the
// destination with an empty slice before reading any rows, while goqu only
// touches it once per row — so a converted list query that declares its
// destination nil silently changes the wire contract for callers that iterate
// the result. The rows are removed inside a transaction that is rolled back,
// because resetDB deliberately preserves them and the cascade would take the
// seeded plan quota defaults with it.
func TestListResourceTypesEmptyIsAnArray(t *testing.T) {
	tx, err := db.New(testDB).Begin()
	if err != nil {
		t.Fatalf("unable to start the transaction: %s", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil {
			t.Fatalf("unable to roll the transaction back: %s", err)
		}
	})

	if _, err = tx.Exec(`DELETE FROM resource_types`); err != nil {
		t.Fatalf("unable to empty the resource_types table: %s", err)
	}

	listing, err := qmsdb.ListResourceTypes(context.Background(), tx)
	if err != nil {
		t.Fatalf("unable to list the resource types: %s", err)
	}
	if len(listing.ResourceTypes) != 0 {
		t.Fatalf("listed %d resource types, want 0", len(listing.ResourceTypes))
	}

	encoded, err := json.Marshal(listing.ResourceTypes)
	if err != nil {
		t.Fatalf("unable to encode the resource type listing: %s", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("an empty listing encoded as %s, want []", encoded)
	}
}
