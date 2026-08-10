# goqu Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove GORM from `cyverse-de/subscriptions` so the whole service uses goqu, without changing any observable HTTP behavior.

**Architecture:** The service has two route trees over one `*sqlx.DB`. `app/` + `db/` use goqu. `internal/qmsapi/` is the QMS `/v1` API and is the only GORM code. Phase 1 builds a golden-file test net around every route that will be touched; Phase 2 rewrites the `/v1` query layer to goqu with those goldens as the gate; Phase 3 deletes GORM. No route is added, removed, or renamed.

**Tech Stack:** Go 1.25, Echo v4, goqu v9, sqlx, testcontainers-go + postgres:17, golang-migrate.

**Spec:** `docs/superpowers/specs/2026-08-10-goqu-unification-design.md`

## Global Constraints

- Branch is `goqu-unification`. Never commit to `main`.
- **Success criterion: `go test ./...` green with zero modifications to any existing golden file under `apitest/testdata/`.** A changed golden is a regression until proven otherwise.
- Reproduce current behavior exactly, bugs included. Record suspected bugs in `docs/superpowers/findings-goqu-unification.md`; do not fix them on this branch.
- No route paths, HTTP methods, request shapes, or response shapes change. `internal/qmsapi/router.go` keeps all 25 registrations, including the duplicate trailing-slash registrations of `POST /v1/subscriptions` and `GET /v1/subscriptions`.
- Docker must be running; the `apitest` suite starts a real postgres container and `log.Fatalf`s if it cannot.
- Follow the repo's Go conventions in `CLAUDE.md`: `goimports`, typed errors over string matching, context threaded through every DB call, all DB functions take a transaction.
- Seeded UUIDs used throughout (asserted verbatim by the harness, never redacted):
  - Basic plan `99e47c22-950a-11ec-84a4-406c8f3e9cbb`
  - Regular plan `c6d39580-98dc-11ec-bbe3-406c8f3e9cbb`
  - Pro plan `cdf7ac7a-98dc-11ec-bbe3-406c8f3e9cbb`
  - resource type cpu.hours `99e3bc7e-950a-11ec-84a4-406c8f3e9cbb`
  - resource type data.size `99e3f91e-950a-11ec-84a4-406c8f3e9cbb`

---

## How these tests work — read before Task 1

These are **characterization tests**, not classic TDD. The cycle is different and an engineer following a normal red-green loop will be confused.

`assertGolden` (`apitest/harness_test.go`) compares a redacted response against `apitest/testdata/<name>.json`. When the golden does not exist it **writes the file and then fails the test on purpose**:

```
golden file testdata/foo.json did not exist and has been created; review it and re-run
```

There is deliberately no regeneration flag. So the loop for every Phase 1 task is:

1. Write the test.
2. Run it. Expect **FAIL** with the "did not exist and has been created" message.
3. **Read the generated golden file and confirm it is sane** — correct shape, no leaked rows from other tests, no unexpected nulls. This is a real review step, not a formality; whatever is in this file becomes the contract Phase 2 must reproduce.
4. Re-run. Expect **PASS**.
5. Commit the test and the golden together.

### Reference-table cleanup — the trap in this suite

`resetDB` clears only `subscription_addons`, `updates`, `usages`, `quotas`, `subscriptions`, `users`. It deliberately leaves `plans`, `plan_rates`, `plan_quota_defaults`, `resource_types`, `addons`, and `update_operations` alone, because goldens assert their migration-seeded UUIDs exactly.

Every test that writes to one of those tables **must delete its own rows in `t.Cleanup`**, following `apitest/plans_test.go:20-32`. A leaked row shows up in every later golden that lists the table, and the failure appears in an unrelated test. Tasks 2 and 4 below both write reference data.

### Ordering — do not reach for `unorderedFields`

`GET /v1/users` (`controllers/users.go:38`) and `GetAllUsageUpdatesForUser` (`controllers/usages.go:193`) both use GORM `Find` with no `ORDER BY`, so multi-row responses come back in arbitrary order.

The harness's `unorderedFields` set keys on the JSON field name, and both of these land under the envelope's `result` key (`model.Response.Result`, `model/root.go`). Adding `"result"` to `unorderedFields` would sort the top-level array of **every** `/v1` response — including `subscriptions_list`, whose whole point is verifying `sort-field=username&sort-dir=asc` works. That would silently destroy a real assertion.

So: **golden these two with a single fixture row**, where order is unobservable, and cover the multi-row case with a separate order-insensitive assertion in Go. Tasks 1 and 5 spell this out.

---

## File Structure

**Created in Phase 1:**

| File | Responsibility |
|---|---|
| `apitest/qms_root_test.go` | `GET /v1`, `GET /v1/users` |
| `apitest/qms_resource_types_test.go` | resource-type create / read-by-id / update |
| `apitest/qms_plans_test.go` | plan create, rates, active-rate, active-quota-defaults |
| `apitest/qms_usage_updates_test.go` | `GET /v1/usages/:username/updates` |
| `apitest/summary_test.go` | `GET /summary/:user`, `GET /` |
| `apitest/goqu_duplicates_test.go` | the 6 uncovered goqu routes that duplicate a `/v1` route |
| `docs/superpowers/findings-goqu-unification.md` | running list of suspected bugs, not fixed here |

**Modified in Phase 1:** `apitest/qms_subscriptions_test.go` (add a golden for `PUT /v1/users/:username`).

**Rewritten in Phase 2:** `internal/qmsapi/db/{resource_types,user,usage,plan,subscriptions}.go`, and the inline GORM calls in `internal/qmsapi/controllers/{root,users,usages,plans,resource_types,subscriptions}.go`.

**Deleted in Phase 3:** `internal/qmsapi/db/gorm.go`.

---

# PHASE 1 — Build the test net

Every Phase 1 task runs against the **current GORM code**, unmodified.

---

### Task 1: `/v1` root and user listing

**Files:**
- Create: `apitest/qms_root_test.go`
- Test: same file

**Interfaces:**
- Consumes: `do`, `assertGolden`, `resetDB`, `mustDecode`, `testUser`, `UsernameSuffix` from `apitest/harness_test.go` and `apitest/fixtures_test.go`; `createUser` from `apitest/qms_subscriptions_test.go`.
- Produces: goldens `v1_root`, `v1_users_single`.

- [ ] **Step 1: Write the test**

```go
package apitest

import (
	"net/http"
	"sort"
	"testing"
)

// GET /v1 reports service information. Nothing calls it except health checks,
// which only look at the status code, but it is part of the surface the goqu
// rewrite has to preserve.
func TestV1Root(t *testing.T) {
	resetDB(t)
	assertGolden(t, "v1_root", do(t, http.MethodGet, "/v1", ""), http.StatusOK)
}

// GET /v1/users lists every user. The listing query carries no ORDER BY, so the
// golden covers the single-user case where order is unobservable; the
// multi-user case is asserted order-insensitively below rather than by widening
// the harness's unorderedFields, which keys on "result" and would sort every
// other /v1 response too.
func TestListUsersSingle(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)

	assertGolden(t, "v1_users_single", do(t, http.MethodGet, "/v1/users", ""), http.StatusOK)
}

func TestListUsersReturnsEveryUser(t *testing.T) {
	resetDB(t)
	createUser(t, "alice"+UsernameSuffix)
	createUser(t, "bob"+UsernameSuffix)

	got := do(t, http.MethodGet, "/v1/users", "")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
	}

	result, ok := mustDecode(t, got)["result"].([]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}

	usernames := make([]string, 0, len(result))
	for _, entry := range result {
		user, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("unexpected user entry: %v", entry)
		}
		name, _ := user["username"].(string)
		usernames = append(usernames, name)
	}
	sort.Strings(usernames)

	if len(usernames) != 2 || usernames[0] != "alice" || usernames[1] != "bob" {
		t.Errorf("usernames = %v, want [alice bob]", usernames)
	}
}
```

- [ ] **Step 2: Run and expect the goldens to be created**

Run: `go test ./apitest/ -run 'TestV1Root|TestListUsers' -v`
Expected: FAIL, twice, with `golden file testdata/v1_root.json did not exist and has been created` and the same for `v1_users_single.json`.

If `TestListUsersReturnsEveryUser` fails on the username assertion instead, the suffix is not being stripped as expected — record it in the findings file and match actual behavior rather than "fixing" it.

- [ ] **Step 3: Review both generated goldens**

Run: `cat apitest/testdata/v1_root.json apitest/testdata/v1_users_single.json`

Confirm `v1_root.json` has `service`, `title`, `api_version` and a `status` of `OK`. Note that `version` is empty because `controllers.Server` is constructed without a `Version` in `app.RegisterQMSAPI` — that is current behavior and the golden must pin it.

Confirm `v1_users_single.json` contains exactly one user, with `testuser` (suffix stripped) and a redacted `<uuid>`.

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run 'TestV1Root|TestListUsers' -v`
Expected: PASS.

- [ ] **Step 5: Run the whole suite to check for leakage**

Run: `go test ./apitest/`
Expected: PASS. If another test now fails, this task leaked rows — fix the fixture, not the other test's golden.

- [ ] **Step 6: Commit**

```bash
git add apitest/qms_root_test.go apitest/testdata/v1_root.json apitest/testdata/v1_users_single.json
git commit -m "Characterize the /v1 root and user listing endpoints"
```

---

### Task 2: Resource-type routes

**Files:**
- Create: `apitest/qms_resource_types_test.go`

**Interfaces:**
- Consumes: `do`, `assertGolden`, `resetDB`, `testDB`.
- Produces: goldens `v1_resource_type_created`, `v1_resource_type_conflict`, `v1_resource_type_missing_unit`, `v1_resource_type_get`, `v1_resource_type_not_found`, `v1_resource_type_updated`.

Request body is `model.ResourceType` (`internal/qmsapi/model/resource.go`): `name`, `unit`, `consumable`. The handler nils out any supplied `id` before saving (`controllers/resource_types.go:85`).

- [ ] **Step 1: Write the test**

```go
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
```

- [ ] **Step 2: Run and expect the goldens to be created**

Run: `go test ./apitest/ -run 'ResourceType' -v`
Expected: FAIL with a "did not exist and has been created" message per golden. Re-run until all six exist; each run creates the goldens it reaches before the first `t.Fatalf`.

- [ ] **Step 3: Review the generated goldens**

Run: `cat apitest/testdata/v1_resource_type_*.json`

Check that the create response carries a redacted `<uuid>` and the submitted name/unit; that the conflict body is the message from `db.ErrResourceTypeConflict` ("a resource type with the same name already exists"); and that `v1_resource_type_get.json` shows the **seeded** cpu.hours UUID verbatim rather than `<uuid>`, which proves the redactor is treating it as reference data.

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run 'ResourceType' -v`
Expected: PASS.

- [ ] **Step 5: Verify no reference-table leakage**

Run: `go test ./apitest/ && go test ./apitest/ -count=1`
Expected: PASS both times. Running twice is the check that `t.Cleanup` actually removed the test rows — a leak fails the second run.

- [ ] **Step 6: Commit**

```bash
git add apitest/qms_resource_types_test.go apitest/testdata/v1_resource_type_*.json
git commit -m "Characterize the /v1 resource-type routes"
```

---

### Task 3: Plan rate and quota-default reads

**Files:**
- Create: `apitest/qms_plans_test.go`
- Modify: `apitest/plans_test.go:14` (remove the function-local `basicPlanID`, now declared at package scope)

**Interfaces:**
- Consumes: `do`, `assertGolden`, `resetDB`.
- Produces: package-level consts `basicPlanID`, `unknownPlanID`, used again by Tasks 4 and 8; goldens `v1_plan_active_rate`, `v1_plan_active_quota_defaults`, `v1_plan_active_rate_unknown`, `v1_plan_active_quota_defaults_unknown`.

- [ ] **Step 1: Write the test**

```go
package apitest

import (
	"net/http"
	"testing"
)

// apitest/plans_test.go declares a function-local basicPlanID with the same
// value inside TestAddPlanQuotaDefaults. That shadows this one rather than
// colliding with it, so both compile — but remove the local one as part of this
// task so there is a single declaration in the package.
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
```

- [ ] **Step 2: Run and expect the goldens to be created**

Run: `go test ./apitest/ -run TestActivePlanRateAndQuotaDefaults -v`
Expected: FAIL with the creation message for each golden.

**If either unknown-plan case returns 500 rather than 404**, do not change the handler. Set `wantStatus` to the observed status, and add an entry to `docs/superpowers/findings-goqu-unification.md` describing it. Phase 2 must reproduce whatever it does today.

- [ ] **Step 3: Review the generated goldens**

Run: `cat apitest/testdata/v1_plan_active_*.json`

Confirm the active-rate response carries a single rate, and the active-quota-defaults response one entry per resource type. Both should show seeded UUIDs verbatim.

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run TestActivePlanRateAndQuotaDefaults -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apitest/qms_plans_test.go apitest/plans_test.go apitest/testdata/v1_plan_active_*.json
git commit -m "Characterize the /v1 active plan rate and quota default reads"
```

---

### Task 4: Plan creation and rate addition

**Files:**
- Modify: `apitest/qms_plans_test.go`

**Interfaces:**
- Consumes: `basicPlanID` from Task 3.
- Produces: goldens `v1_plan_created`, `v1_plan_no_name`, `v1_plan_rates_added`.

Request body for `POST /v1/plans` is `httpmodel.NewPlan`: `name`, `description`, `plan_quota_defaults`, `plan_rates`. A quota default is `{"quota_value": N, "resource_type": {...}, "effective_date": "..."}`; a rate is `{"effective_date": "...", "rate": N}` (`internal/qmsapi/httpmodel/new_plan.go:260`). `POST /v1/plans/{id}/rates` takes `{"plan_rates": [...]}`.

- [ ] **Step 1: Append the tests**

```go
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
```

- [ ] **Step 2: Run and expect the goldens to be created**

Run: `go test ./apitest/ -run 'TestAddPlan' -v`
Expected: FAIL with the creation message for each golden.

- [ ] **Step 3: Review the generated goldens**

Run: `cat apitest/testdata/v1_plan_created.json apitest/testdata/v1_plan_no_name.json apitest/testdata/v1_plan_rates_added.json`

Confirm the created plan echoes back its quota defaults and rates, and that the validation failure carries the message from `NewPlan.Validate` ("a plan name is required").

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run 'TestAddPlan' -v`
Expected: PASS.

- [ ] **Step 5: Verify cleanup, twice**

Run: `go test ./apitest/ -count=1 && go test ./apitest/ -count=1`
Expected: PASS both times, with `plans_list` and `plans_get_basic` unchanged. If `plans_list` fails, the plan or rate rows leaked.

- [ ] **Step 6: Commit**

```bash
git add apitest/qms_plans_test.go apitest/testdata/v1_plan_created.json apitest/testdata/v1_plan_no_name.json apitest/testdata/v1_plan_rates_added.json
git commit -m "Characterize /v1 plan creation and rate addition"
```

---

### Task 5: Usage-update listing

**Files:**
- Create: `apitest/qms_usage_updates_test.go`

**Interfaces:**
- Consumes: `recordV1Usage` and `createUser` from `apitest/qms_divergence_test.go` and `apitest/qms_subscriptions_test.go`.
- Produces: golden `v1_usage_updates_single`.

- [ ] **Step 1: Write the test**

```go
package apitest

import (
	"net/http"
	"testing"
)

// GET /v1/usages/{username}/updates lists the audit rows QMS writes for every
// usage change. The query carries no ORDER BY (controllers/usages.go:193), so
// the golden covers one update, where order is unobservable, and the multi-row
// case is counted instead. Widening the harness's unorderedFields is not an
// option here: it keys on "result", which every /v1 response uses.
func TestListUsageUpdates(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)
	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)

	assertGolden(t, "v1_usage_updates_single",
		do(t, http.MethodGet, "/v1/usages/"+testUser+"/updates", ""), http.StatusOK)
}

func TestListUsageUpdatesReturnsEveryUpdate(t *testing.T) {
	resetDB(t)
	createUser(t, testUser)
	recordV1Usage(t, testUser, "cpu.hours", "SET", 10)
	recordV1Usage(t, testUser, "cpu.hours", "ADD", 5)

	got := do(t, http.MethodGet, "/v1/usages/"+testUser+"/updates", "")
	if got.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", got.status, got.body)
	}

	result, ok := mustDecode(t, got)["result"].([]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}
	if len(result) != 2 {
		t.Errorf("updates returned = %d, want 2", len(result))
	}
}
```

- [ ] **Step 2: Run and expect the golden to be created**

Run: `go test ./apitest/ -run TestListUsageUpdates -v`
Expected: FAIL with `golden file testdata/v1_usage_updates_single.json did not exist and has been created`.

- [ ] **Step 3: Review the generated golden**

Run: `cat apitest/testdata/v1_usage_updates_single.json`

Confirm one update with a redacted `<uuid>` and `<timestamp>`, the cpu.hours resource type at its seeded UUID, and a value of 10.

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run TestListUsageUpdates -v`
Expected: PASS, both tests.

- [ ] **Step 5: Commit**

```bash
git add apitest/qms_usage_updates_test.go apitest/testdata/v1_usage_updates_single.json
git commit -m "Characterize the /v1 usage update listing"
```

---

### Task 6: Golden for `PUT /v1/users/{username}`

**Files:**
- Modify: `apitest/qms_subscriptions_test.go`

**Interfaces:**
- Produces: golden `v1_user_added`.

`createUser` already calls this route but only checks the status. Leave `createUser` alone — many tests depend on it — and add a separate test that pins the response body.

- [ ] **Step 1: Append the test**

```go
// createUser checks only the status of PUT /v1/users/{username}; this pins the
// response body, which terrain sees when it subscribes a user indirectly.
func TestAddUserResponse(t *testing.T) {
	resetDB(t)
	assertGolden(t, "v1_user_added",
		do(t, http.MethodPut, "/v1/users/"+testUser, ""), http.StatusOK)

	stored := queryInt(t, `SELECT count(*) FROM users WHERE username = $1`, trimmedUser)
	if stored != 1 {
		t.Errorf("users rows = %d, want 1", stored)
	}
}
```

- [ ] **Step 2: Run and expect the golden to be created**

Run: `go test ./apitest/ -run TestAddUserResponse -v`
Expected: FAIL with the creation message.

- [ ] **Step 3: Review the generated golden**

Run: `cat apitest/testdata/v1_user_added.json`

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run TestAddUserResponse -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apitest/qms_subscriptions_test.go apitest/testdata/v1_user_added.json
git commit -m "Pin the response body of PUT /v1/users/{username}"
```

---

### Task 7: goqu survivors — summary and greeting

**Files:**
- Create: `apitest/summary_test.go`

**Interfaces:**
- Consumes: `subscribeUser`, `recordUsage` from `apitest/fixtures_test.go`.
- Produces: goldens `summary_basic`, `summary_unknown_user`.

`GET /summary/:user` is the endpoint resource-usage-api calls (`internal/summarizer/httpsummarizer.go:31`) and has had no coverage at all. It is goqu already, so this is confirming it works rather than protecting a rewrite — but it is the service's flagship read and belongs in the net.

- [ ] **Step 1: Write the test**

```go
package apitest

import (
	"net/http"
	"testing"
)

// GET /summary/{user} is what resource-usage-api calls to build a user's
// subscription summary. It has a write side effect: a user the service has
// never seen is created and subscribed to the default plan.
func TestUserSummary(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 42)

	assertGolden(t, "summary_basic",
		do(t, http.MethodGet, "/summary/"+trimmedUser, ""), http.StatusOK)
}

func TestUserSummaryForAnUnknownUser(t *testing.T) {
	resetDB(t)

	assertGolden(t, "summary_unknown_user",
		do(t, http.MethodGet, "/summary/nosuchuser", ""), http.StatusOK)
}

func TestGreeting(t *testing.T) {
	got := do(t, http.MethodGet, "/", "")
	if got.status != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", got.status, got.body)
	}
	if string(got.body) != "Hello from subscriptions." {
		t.Errorf("body = %q, want \"Hello from subscriptions.\"", got.body)
	}
}
```

- [ ] **Step 2: Run and expect the goldens to be created**

Run: `go test ./apitest/ -run 'TestUserSummary|TestGreeting' -v`
Expected: FAIL for the two summary goldens; `TestGreeting` passes immediately since it asserts a literal, not a golden.

If `summary_unknown_user` comes back non-200, match the observed status and note it in the findings file.

- [ ] **Step 3: Review the generated goldens**

Run: `cat apitest/testdata/summary_basic.json apitest/testdata/summary_unknown_user.json`

Confirm `summary_basic.json` shows the Basic plan at its seeded UUID, the recorded cpu.hours usage of 42, and quotas. Confirm `summary_unknown_user.json` shows the auto-created default subscription rather than an error.

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run 'TestUserSummary|TestGreeting' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apitest/summary_test.go apitest/testdata/summary_basic.json apitest/testdata/summary_unknown_user.json
git commit -m "Characterize the user summary and greeting endpoints"
```

---

### Task 8: The retained goqu duplicate routes

**Files:**
- Create: `apitest/goqu_duplicates_test.go`

**Interfaces:**
- Produces: goldens `goqu_plans_list`, `goqu_plan_get`, `goqu_plan_added`, `goqu_quota_added`, `goqu_usage_added`, `goqu_user_updates`.

These six routes duplicate a `/v1` route and have no verified caller, but they are staying (spec §Scope). Covering them confirms they work and gives the later collapse-duplicates decision recorded output from both implementations to compare.

Request bodies come from `github.com/cyverse-de/p/go/qms`: `AddPlanRequest` wraps a `Plan` under `"plan"`, `AddQuotaRequest` wraps a `Quota` under `"quota"`, and `AddUsage` is flat (`username`, `resource_name`, `update_type`, `usage_value`, `resource_unit`).

- [ ] **Step 1: Write the test**

```go
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

func TestGoquAddQuota(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	subscriptionID := subscriptionIDFor(t, trimmedUser)
	body := `{"quota": {"quota": 500, "subscription_id": "` + subscriptionID + `",` +
		`"resource_type": {"name": "cpu.hours", "unit": "cpu hours"}}}`
	assertGolden(t, "goqu_quota_added", do(t, http.MethodPut, "/quotas", body), http.StatusOK)
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
```

- [ ] **Step 2: Run and expect the goldens to be created**

Run: `go test ./apitest/ -run TestGoqu -v`
Expected: FAIL with a creation message per golden. Re-run until all six exist.

Several of these routes have never been exercised. If one returns an unexpected status or an error body, **match the observed behavior in the golden and record it in the findings file** — that is the point of characterizing them. Do not fix a handler on this branch.

- [ ] **Step 3: Review the generated goldens**

Run: `cat apitest/testdata/goqu_*.json`

Compare `goqu_plans_list.json` against `plans_list.json` and `goqu_plan_get.json` against `plans_get_basic.json`. They describe the same data through different implementations; note any structural differences in the findings file, since that comparison is the deliverable for the later collapse decision.

- [ ] **Step 4: Run again and expect green**

Run: `go test ./apitest/ -run TestGoqu -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite twice**

Run: `go test ./... -count=1 && go test ./apitest/ -count=1`
Expected: PASS both times.

- [ ] **Step 6: Commit**

```bash
git add apitest/goqu_duplicates_test.go apitest/testdata/goqu_*.json docs/superpowers/findings-goqu-unification.md
git commit -m "Characterize the retained goqu routes that duplicate a /v1 route"
```

---

### Task 9: Lock the baseline

**Files:**
- Create: `docs/superpowers/findings-goqu-unification.md` (if Tasks 1-8 have not already created it)

- [ ] **Step 1: Record the golden inventory**

Run: `ls apitest/testdata/ | wc -l && ls apitest/testdata/`

- [ ] **Step 2: Confirm the whole suite is green from clean**

Run: `go test ./... -count=1`
Expected: PASS, every package.

- [ ] **Step 3: Confirm the working tree is clean**

Run: `git status --porcelain`
Expected: empty. Everything from Tasks 1-8 is committed.

- [ ] **Step 4: Tag the baseline so Phase 2 can diff against it**

```bash
git tag goqu-baseline
git log --oneline main..HEAD
```

- [ ] **Step 5: Commit the findings file if it has content**

```bash
git add docs/superpowers/findings-goqu-unification.md
git commit -m "Record behavior observed while characterizing the API" || true
```

---

# PHASE 2 — Rewrite the `/v1` query layer to goqu

**The gate for every task in this phase is the same:** `go test ./... -count=1` passes and `git diff goqu-baseline -- apitest/testdata/` is empty. If a golden changes, the rewrite changed behavior. Revert and try again; do not edit the golden.

Work bottom-up: the `internal/qmsapi/db` helpers first, then the controllers that call them.

---

### Task 10: goqu transaction and not-found plumbing

**Files:**
- Modify: `internal/qmsapi/controllers/root.go:34-57`
- Modify: `internal/qmsapi/db/errors.go`

**Interfaces:**
- Produces: a transaction helper on `Server` with the same contract as the current `transaction(fn func(tx *gorm.DB) error) error`, but taking a goqu transaction; and `db.ErrNotFound`, a typed sentinel replacing `gorm.ErrRecordNotFound` at the boundary.

This task is the foundation for the rest of Phase 2 and changes no behavior on its own.

Two contracts must survive intact:

**`txAbort`** (`root.go:20-32`). `model.Error` writes the response and returns `nil`, so returning it straight from a transaction callback would commit the very write the response reports as failed. `txError` wraps it in an error the transaction machinery can see, and `transaction` unwraps it afterwards so Echo sees the handler's real return value. The goqu version needs exactly this, because `db.Tx.Wrap` in the existing goqu layer (`db/db.go`) has its own rollback semantics.

**Not-found mapping.** Every `errors.Is(err, gorm.ErrRecordNotFound)` site is a 404-vs-500 decision. goqu over sqlx reports absence as `sql.ErrNoRows`. Per `CLAUDE.md`, use a typed error and `errors.As`/`errors.Is`, never string matching.

- [ ] **Step 1: Add the typed not-found error**

```go
package db

import "errors"

var (
	ErrResourceTypeConflict = errors.New("a resource type with the same name already exists")

	// ErrNotFound reports a row that does not exist. The GORM layer signalled
	// this with gorm.ErrRecordNotFound; goqu over sqlx reports sql.ErrNoRows,
	// so queries translate at the boundary and callers keep matching on one
	// sentinel. Every match on this is a 404-versus-500 decision.
	ErrNotFound = errors.New("record not found")
)
```

- [ ] **Step 2: Run the suite to confirm nothing is affected yet**

Run: `go test ./... -count=1`
Expected: PASS. Adding an unused sentinel changes nothing.

- [ ] **Step 3: Add the goqu transaction helper alongside the GORM one**

Keep the existing `transaction` method until its last caller is converted. Add the goqu equivalent next to it, reusing `txAbort` and `txError` **unchanged** so the response-vs-rollback contract is shared by both during the migration.

The goqu layer's transaction type is `*goqu.TxDatabase`, obtained from `(*db.Database).Begin()` and driven with `tx.Wrap(func() error)` — see `db/db.go:47` and the usage in `app/app.go`'s `addUserUpdate`. `Server` already holds the `*sql.DB`; add the goqu database next to it rather than rebuilding it per request:

```go
// Server defines the REST API of the qms
type Server struct {
	Router *echo.Echo
	DB     *sql.DB
	// GoquDB is the query layer the /v1 handlers use. It replaces GORMDB; both
	// are present only while the rewrite is in progress.
	GoquDB         *db.Database
	GORMDB         *gorm.DB
	Service        string
	Title          string
	Version        string
	ReportOverages bool
	UsernameSuffix string
}

// goquTransaction runs fn inside a database transaction, unwrapping the response
// written by any txError call within it so that echo sees the handler's real
// return value. It is the goqu counterpart of transaction and shares txAbort, so
// a handler converted from one to the other keeps its rollback semantics.
func (s Server) goquTransaction(fn func(tx *goqu.TxDatabase) error) error {
	tx, err := s.GoquDB.Begin()
	if err != nil {
		return err
	}

	err = tx.Wrap(func() error { return fn(tx) })

	var abort txAbort
	if errors.As(err, &abort) {
		return abort.response
	}

	return err
}
```

Wire `GoquDB` in `app.RegisterQMSAPI` (`app/app.go:105`) with `db.New(a.db)`, alongside the existing `GORMDB`.

Note that `db.Database` is `github.com/cyverse-de/subscriptions/db`, not `internal/qmsapi/db`; both are package `db`, so one of them needs an import alias. `app/app.go` already aliases the internal one as `qmsdb` — follow that.

- [ ] **Step 4: Verify the build and the suite**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/qmsapi/db/errors.go internal/qmsapi/controllers/root.go
git commit -m "Add goqu transaction and not-found plumbing alongside the GORM versions"
```

---

### Task 11: Resource-type queries

**Files:**
- Modify: `internal/qmsapi/db/resource_types.go` (all 5 functions)
- Modify: `internal/qmsapi/controllers/resource_types.go:41,88,127-128,182`

**Interfaces:**
- Consumes: `db.ErrNotFound` and the goqu transaction helper from Task 10.
- Produces: `GetResourceTypeByName`, `GetResourceTypeByID`, `ListResourceTypes`, `UpdateResourceType`, `SaveResourceType` with unchanged signatures except that the `*gorm.DB` parameter becomes the goqu transaction type.

Gated by Task 2's goldens. Note `SaveResourceType` must keep mapping a duplicate to `ErrResourceTypeConflict` (409, not 500), and `GetResourceTypeDetails` must keep mapping absence to 404.

- [ ] **Step 1: Confirm the gate passes before changing anything**

Run: `go test ./apitest/ -run 'ResourceType' -count=1 -v`
Expected: PASS.

- [ ] **Step 2: Rewrite the five query functions against goqu**

Follow the patterns in `db/resourcetypes.go`, the goqu implementation that already exists for the service's own routes. Translate `sql.ErrNoRows` to `db.ErrNotFound` at each query boundary.

- [ ] **Step 3: Replace the inline GORM calls in the controller**

`s.GORMDB.Find(&data)` at line 41 and `s.GORMDB.Take(&resourceType)` at line 127 become goqu queries; the `errors.Is(err, gorm.ErrRecordNotFound)` at line 128 becomes `errors.Is(err, db.ErrNotFound)`; `s.transaction` at line 182 becomes the goqu helper.

- [ ] **Step 4: Run the resource-type gate**

Run: `go test ./apitest/ -run 'ResourceType' -count=1 -v`
Expected: PASS, with no golden file modified.

- [ ] **Step 5: Run the full suite and diff the goldens**

Run: `go test ./... -count=1 && git diff --stat goqu-baseline -- apitest/testdata/`
Expected: PASS, and an empty diff.

- [ ] **Step 6: Commit**

```bash
git add internal/qmsapi/db/resource_types.go internal/qmsapi/controllers/resource_types.go
git commit -m "Rewrite the resource-type queries against goqu"
```

---

### Task 12: User and usage queries

**Files:**
- Modify: `internal/qmsapi/db/user.go`, `internal/qmsapi/db/usage.go`
- Modify: `internal/qmsapi/controllers/users.go:38,65,139,208,305,413`
- Modify: `internal/qmsapi/controllers/usages.go:78,105,193,223`
- Modify: `internal/qmsapi/controllers/util.go:16`

**Interfaces:**
- Consumes: Task 10's plumbing.
- Produces: `GetUser`, `UserExists`, `UpsertUsage` with the `*gorm.DB` parameter replaced.

Gated by Tasks 1, 5, 6 and the existing `usage_*`, `user_plan_*`, `add_user_*` goldens.

Three behaviors to preserve deliberately:
- **`GET /v1/users/{username}/plan` writes on read.** `GetSubscriptionDetails` creates a user the service has never seen and subscribes them to the default plan, returning 200 rather than 404. Terrain depends on it: a user who has never touched QMS still gets a plan back on first login. The golden `user_plan_autocreated` pins it. A goqu rewrite that "corrects" this into a 404 breaks terrain.
- `GetAllUsers` (`users.go:38`) and `userUpdates` (`usages.go:193`) have **no `ORDER BY`**. Do not add one — that would be a behavior change, and Tasks 1 and 5 golden only the single-row case precisely so this stays honest. Record the absent ordering in the findings file instead.
- `UpsertUsage` uses a GORM `clause` for its conflict handling; the goqu version needs the same `ON CONFLICT` semantics or repeated usage updates will insert instead of update, which `TestUsageWritesAnUpdatesRow` will catch.

- [ ] **Step 1: Confirm the gate passes before changing anything**

Run: `go test ./apitest/ -run 'TestListUsers|TestListUsageUpdates|TestAddUserResponse|TestUsageEndpoints|TestUsageWritesAnUpdatesRow' -count=1 -v`
Expected: PASS.

- [ ] **Step 2: Rewrite `db/user.go` and `db/usage.go` against goqu**

- [ ] **Step 3: Replace the inline GORM calls in the three controllers**

- [ ] **Step 4: Run the gate**

Run: `go test ./apitest/ -run 'TestListUsers|TestListUsageUpdates|TestAddUserResponse|TestUsageEndpoints|TestUsageWritesAnUpdatesRow' -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite and diff the goldens**

Run: `go test ./... -count=1 && git diff --stat goqu-baseline -- apitest/testdata/`
Expected: PASS, and an empty diff.

- [ ] **Step 6: Commit**

```bash
git add internal/qmsapi/db/user.go internal/qmsapi/db/usage.go internal/qmsapi/controllers/users.go internal/qmsapi/controllers/usages.go internal/qmsapi/controllers/util.go
git commit -m "Rewrite the user and usage queries against goqu"
```

---

### Task 13: Plan queries

**Files:**
- Modify: `internal/qmsapi/db/plan.go` (all 10 functions)
- Modify: `internal/qmsapi/controllers/plans.go:43,83,131,204,261,327,439`

**Interfaces:**
- Consumes: Task 10's plumbing.
- Produces: `GetPlan`, `CheckPlanNameExistence`, `CheckPlanExistence`, `GetPlanByID`, `GetActivePlanRate`, `GetActivePlanQuotaDefaults`, `ListPlans`, `GetDefaultQuotaForPlan`, `GetPlansByName`, `SavePlanQuotaDefaults`, `SavePlanRates` with the `*gorm.DB` parameter replaced.

Gated by Tasks 3 and 4, plus the existing `plans_list`, `plans_get_basic` goldens and `TestAddPlanQuotaDefaults`.

**This is where the Preload-ordering risk bites.** `ListPlans`, `GetPlan` and `GetPlanByID` each use `Preload` with an inner ordering closure (`db/plan.go:24-33,92-101,166-175`). GORM issues those as separate queries; a goqu join will not reproduce their order for free. Read each closure and carry its `ORDER BY` into the goqu query explicitly. The harness already sorts `plan_quota_defaults` and `plan_rates` via `unorderedFields`, so a mismatch there will not fail — but the top-level plan list is **not** in that set and will.

- [ ] **Step 1: Confirm the gate passes before changing anything**

Run: `go test ./apitest/ -run 'Plan' -count=1 -v`
Expected: PASS.

- [ ] **Step 2: Read every Preload closure in `db/plan.go` and write down its ordering**

Run: `grep -n -A4 'Preload' internal/qmsapi/db/plan.go`

- [ ] **Step 3: Rewrite the ten query functions against goqu, carrying each ordering across**

- [ ] **Step 4: Replace the inline GORM calls in `controllers/plans.go`**

- [ ] **Step 5: Run the gate**

Run: `go test ./apitest/ -run 'Plan' -count=1 -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite and diff the goldens**

Run: `go test ./... -count=1 && git diff --stat goqu-baseline -- apitest/testdata/`
Expected: PASS, and an empty diff. A diff in `plans_list.json` means an ordering mismatch — fix the query, not the golden.

- [ ] **Step 7: Commit**

```bash
git add internal/qmsapi/db/plan.go internal/qmsapi/controllers/plans.go
git commit -m "Rewrite the plan queries against goqu"
```

---

### Task 14: Subscription queries

**Files:**
- Modify: `internal/qmsapi/db/subscriptions.go` (446 lines, all functions)
- Modify: `internal/qmsapi/controllers/subscriptions.go:34,81,223,232,316`

**Interfaces:**
- Consumes: Task 10's plumbing, Task 13's plan queries.
- Produces: `activeNow`, `overlapping`, `notExpiredAt`, `withSubscriptionDetails`, `SubscribeUser`, `SubscribeUserToDefaultPlan`, `GetActiveSubscription`, `HasActiveSubscription`, `GetSubscriptionDetails`, `ListSubscriptions`, `GetActiveSubscriptionDetails`, `DeactivateSubscriptions`, `UpsertQuota` with the `*gorm.DB` parameter replaced; `NewSubscriptionAdder` and `AddSubscription` taking the goqu transaction.

The largest and highest-risk task. Gated by `apitest/qms_divergence_test.go` in full — nine tests encoding subscription semantics that were hard-won, including the unforced-upgrade rule, open-ended subscription handling, and per-item bulk failure reporting.

Three things to preserve exactly:
- The `activeNow` / `notExpiredAt` / `overlapping` scopes are the single `activeAsOf` notion that a prior bug fix consolidated. Do not re-derive them independently per query; that is precisely the bug that was fixed.
- `DeactivateSubscriptions` uses `gorm.Expr("effective_start_date")` in an `UpdateColumn` (`subscriptions.go:412`) — a column-to-column assignment, not a bound value. In goqu this is a `goqu.I("effective_start_date")`, not a literal.
- `UpsertQuota` and `SubscribeUser` use GORM `clause` conflict handling; the `ON CONFLICT` targets and actions must match.

- [ ] **Step 1: Confirm the gate passes before changing anything**

Run: `go test ./apitest/ -run 'Subscription|Unforced|Overlapping|BulkSubscription|CurrentPlan|QuotaUpdate' -count=1 -v`
Expected: PASS.

- [ ] **Step 2: Rewrite the four scope helpers first, then the queries that compose them**

- [ ] **Step 3: Rewrite `DeactivateSubscriptions`, being explicit about the column-to-column assignment**

- [ ] **Step 4: Rewrite `UpsertQuota` and `SubscribeUser`, preserving the conflict clauses**

- [ ] **Step 5: Replace the inline GORM calls in `controllers/subscriptions.go`**

- [ ] **Step 6: Run the divergence suite**

Run: `go test ./apitest/ -run 'Subscription|Unforced|Overlapping|BulkSubscription|CurrentPlan|QuotaUpdate' -count=1 -v`
Expected: PASS, all nine divergence tests included.

- [ ] **Step 7: Run the full suite and diff the goldens**

Run: `go test ./... -count=1 && git diff --stat goqu-baseline -- apitest/testdata/`
Expected: PASS, and an empty diff.

- [ ] **Step 8: Commit**

```bash
git add internal/qmsapi/db/subscriptions.go internal/qmsapi/controllers/subscriptions.go
git commit -m "Rewrite the subscription queries against goqu"
```

---

# PHASE 3 — Remove GORM

---

### Task 15: Delete the GORM layer

**Files:**
- Delete: `internal/qmsapi/db/gorm.go`
- Modify: `internal/qmsapi/controllers/root.go` (drop the `GORMDB` field and the GORM `transaction` method)
- Modify: `app/app.go:99-122` (`RegisterQMSAPI`)
- Modify: `go.mod`, `go.sum`

`RegisterQMSAPI` currently returns an error **only** because `InitGORMConnection` can fail. Once GORM is gone the error path disappears. `main.go:100` and `apitest/harness_test.go:130` both check that error, so both need updating. Keep `RegisterQMSAPI` separate from `New` regardless — `app/app.go:96-98` explains that `New` is also called without a database by tests that return before touching one.

- [ ] **Step 1: Confirm no GORM references remain outside the files being deleted**

Run: `grep -rn 'gorm' --include='*.go' . | grep -v '_test.go'`
Expected: hits only in `internal/qmsapi/db/gorm.go` and the `GORMDB` field and `transaction` method in `controllers/root.go`. Any other hit means a Phase 2 task was incomplete — go back and finish it.

- [ ] **Step 2: Delete `gorm.go` and drop `GORMDB` and the GORM `transaction` method**

```bash
git rm internal/qmsapi/db/gorm.go
```

- [ ] **Step 3: Simplify `RegisterQMSAPI` and its two callers**

Change the signature to `func (a *App) RegisterQMSAPI(usernameSuffix string)`, drop the `fmt.Errorf` wrapper, and update `main.go` and `apitest/harness_test.go` to call it without checking an error.

- [ ] **Step 4: Build and confirm the suite still passes**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS.

- [ ] **Step 5: Drop the GORM modules**

```bash
go mod tidy
grep -c gorm go.mod
```
Expected: `0`. Both `gorm.io/gorm` and `gorm.io/driver/postgres` are gone.

- [ ] **Step 6: Verify the full gate one last time**

Run: `go test ./... -count=1 && git diff --stat goqu-baseline -- apitest/testdata/`
Expected: PASS, and an empty diff. **This empty diff across the whole of Phases 2 and 3 is the deliverable.**

- [ ] **Step 7: Lint**

Run: `golangci-lint run`
Expected: clean. Per `CLAUDE.md`, treat warnings as errors unless fixing one would cause a difficult breakage.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "Remove GORM from the subscriptions service

Every /v1 query now runs through goqu. No golden file changed across the
rewrite, so the endpoints behave exactly as they did before."
```

- [ ] **Step 9: Report the findings**

Summarize `docs/superpowers/findings-goqu-unification.md` for the user: each suspected bug reproduced rather than fixed, and what a follow-up branch would need to change. Do not fix them here.

---

## Verification

Final state, all of which must hold:

- `go test ./... -count=1` passes.
- `git diff goqu-baseline -- apitest/testdata/` is empty for every pre-existing golden.
- `grep -rn 'gorm' --include='*.go' .` returns nothing.
- `grep -c gorm go.mod` returns 0.
- `golangci-lint run` is clean.
- `internal/qmsapi/router.go` still has 25 route registrations.
