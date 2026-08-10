# goqu unification

Replace GORM with goqu across the subscriptions service so there is one query
layer, and build the test coverage that proves the swap changed no behavior.

Branch: `goqu-unification`.

## Background

The service has two route trees over one database connection:

- `app/` + `db/` — the service's own routes, using goqu over sqlx.
- `internal/qmsapi/` — the QMS `/v1` API, lifted from cyverse/qms, using GORM.

`internal/qmsapi/` is roughly 1,300 lines of controllers plus 850 lines of GORM
query code. `main.go` opens one `*sqlx.DB` and `App.RegisterQMSAPI` layers GORM
over `a.db.DB`, so both trees already share a connection. Only the query builder
differs.

## Which routes are in active use

Determined by reading the callers, not by inference. Checked repos: terrain,
apps, app-exposer, resource-usage-api, data-usage-api.

Terrain reaches the service through two configured base URIs:

- `terrain.subscriptions.base-uri` — `src/terrain/clients/qms_addons.clj`
- `terrain.qms.base-uri` — `src/terrain/clients/qms.clj`

Both now resolve to the subscriptions service:
`deployments/ansible/roles/services/terrain/templates/terrain.properties.j2:183`
templates `terrain.qms.base-uri` from `baseurls_subscriptions`. The `/v1` tree is
live in production.

| Route | Layer | Verified caller |
|---|---|---|
| `GET /summary/:user` | goqu | resource-usage-api `internal/summarizer/httpsummarizer.go:31` |
| `GET /users/:u/overages` | goqu | app-exposer `quota/enforcer.go:236`; data-usage-api `subscriptions/subscriptions.go:137` |
| `GET /users/:u/usages` | goqu | data-usage-api `subscriptions/subscriptions.go:106` |
| `PUT /user/:u/updates` | goqu | data-usage-api `:168`; resource-usage-api `clients/subscriptions.go:51` |
| 9 × add-on routes | goqu | terrain `clients/qms_addons.clj` |
| `GET /v1/plans`, `GET /v1/plans/:id` | GORM | terrain `clients/qms.clj:100,107` |
| `GET /v1/usages/:u`, `POST /v1/usages` | GORM | terrain `qms.clj:37,44` |
| `GET`/`POST /v1/subscriptions` | GORM | terrain `qms.clj:53,62` |
| `GET /v1/users/:u/subscriptions` | GORM | terrain `qms.clj:69` |
| `POST /v1/users/:u/plan/:rt/quota` | GORM | terrain `qms.clj:76` |
| `PUT /v1/users/:u/:plan_name` | GORM | terrain `qms.clj:85` |
| `GET /v1/users/:u/plan` | GORM | terrain `qms.clj:93` |
| `GET /v1/resource-types` | GORM | terrain `qms.clj:114` |

There are no GORM add-on handlers. `grep -rni addon internal/qmsapi/` returns
nothing; all nine add-on routes are goqu in `app/addons.go`.

Where the two trees duplicate each other, the caller is on the `/v1` side. One
pair is live on both sides and neither can be dropped: `GET /users/:u/usages`
(data-usage-api) and `GET /v1/usages/:u` (terrain).

## Scope

**Rewrite** the 23 `/v1` operations from GORM to goqu, at their existing paths,
with request and response shapes unchanged. Eleven have a verified terrain
caller; twelve do not, and are rewritten anyway rather than deleted.

`internal/qmsapi/router.go` holds 25 registrations for those 23 operations:
`POST /v1/subscriptions` and `GET /v1/subscriptions` are each registered twice,
with and without a trailing slash. Both aliases have to survive the rewrite.

**Leave alone** every goqu route, including the seven that duplicate a `/v1`
route. They are already goqu, so they cost nothing toward removing GORM;
deleting them belongs to the separate question of collapsing duplicates.

**Out of scope:** collapsing duplicate routes, and fixing bugs the rewrite
surfaces. Both are follow-up branches.

### Duplicate pairs, retained on both sides

| goqu | `/v1` (GORM) |
|---|---|
| `GET /plans` | `GET /v1/plans` |
| `PUT /plans` | `POST /v1/plans` |
| `GET /plans/:plan_id` | `GET /v1/plans/:plan_id` |
| `PUT /quotas` | `POST /v1/users/:u/plan/:rt/quota` |
| `PUT /users` | `PUT /v1/users/:u/:plan_name` |
| `PUT /users/:u/usages` | `POST /v1/usages` |
| `GET /users/:u/updates` | `GET /v1/usages/:u/updates` |

Covering both sides of each pair produces the recorded-output comparison the
collapsing decision needs.

## Testing

`apitest/` already provides what this needs: the real router against a real
PostgreSQL testcontainer with the real migrations, golden files with UUID and
timestamp redaction, and direct database assertions. No new infrastructure.

`assertGolden` writes a missing golden and then fails deliberately. Creating a
baseline is therefore a read-and-accept step per file, not a regeneration pass.

### Coverage to add

`/v1` routes with no HTTP-level test:

- `GET /v1`
- `POST /v1/plans`
- `GET /v1/plans/:id/active-rate`
- `GET /v1/plans/:id/active-quota-defaults`
- `POST /v1/plans/:id/rates`
- `GET /v1/usages/:u/updates`
- `GET /v1/users`
- `POST /v1/resource-types`
- `GET /v1/resource-types/:id`
- `PUT /v1/resource-types/:id`

`PUT /v1/users/:username` is exercised by `createUser` in
`apitest/qms_subscriptions_test.go` but only for status; it needs a golden.

goqu routes with no coverage:

- `GET /summary/:user` — the flagship endpoint, called by resource-usage-api
- `GET /` — greeting, status only
- `GET /plans`, `PUT /plans`, `GET /plans/:plan_id`, `PUT /quotas`,
  `PUT /users/:u/usages`, `GET /users/:u/updates`

`PUT /users` is already covered by `apitest/users_test.go` and its five
`add_user_*` goldens.

Each route that writes gets a database assertion alongside its golden, and each
lookup route gets its not-found case, since 404-versus-500 is the error-mapping
risk below.

### New files

`apitest/qms_plans_test.go`, `apitest/qms_resource_types_test.go`,
`apitest/qms_root_test.go`, `apitest/summary_test.go`, plus additions to
`apitest/qms_subscriptions_test.go` and a file for the retained goqu duplicates.

### Reference-table cleanup

`resetDB` deliberately leaves the migration-seeded reference tables alone
(`plans`, `plan_rates`, `plan_quota_defaults`, `resource_types`, `addons`,
`update_operations`) because goldens assert their seeded UUIDs exactly.

Four new tests write to those tables: `POST /v1/plans`,
`POST /v1/plans/:id/rates`, `POST /v1/resource-types`, and
`PUT /v1/resource-types/:id`. Each must remove its own rows in `t.Cleanup`,
following `apitest/plans_test.go:20-32`. Without that, a leaked row appears in
every golden that lists the table.

## Sequencing

1. Write the new tests against the current GORM code. Review each generated
   golden individually. Commit.
2. Rewrite the `/v1` query layer to goqu, route group by route group, with the
   goldens green after each group.
3. Remove `gorm.io/gorm` and `gorm.io/driver/postgres` from `go.mod`, delete
   `internal/qmsapi/db/gorm.go`, drop `GORMDB` from `controllers.Server`, and
   simplify `App.RegisterQMSAPI` — it currently returns an error only because
   opening the GORM layer can fail.

## Success criterion

`go test ./...` green with zero modifications to any golden file.

A golden diff means either a regression or a change that has to be justified in
review. There is no regeneration flag, by design.

## Handling bugs the rewrite surfaces

Reproduce current behavior exactly, bugs included, so the rewrite is provably
behavior-preserving. Record each suspected bug in a findings list as it is
found. Fixing them is a follow-up branch, one at a time, each golden change
reviewed on its own.

This follows the precedent in `apitest/qms_divergence_test.go`, which pins QMS
semantics as the ones that survive the merge and documents the GORM `First`
lookup bug that used to record every usage update as `ADD`.

## Risks

**Result ordering.** GORM's `Preload` issues separate queries with its own
ordering, which goqu joins will not naturally reproduce. This is the most likely
source of spurious golden diffs. The harness has an `unorderedFields` set that
sorts a field's contents before comparison, but widening it weakens the test.
Prefer an explicit `ORDER BY` that matches current output; widen
`unorderedFields` only where the current output is genuinely arbitrary.

**Error mapping.** GORM signals absence with `gorm.ErrRecordNotFound`; goqu over
sqlx gives `sql.ErrNoRows`. Every `errors.Is(err, gorm.ErrRecordNotFound)` site
is a 404-versus-500 decision, and a missed one turns a clean 404 into a 500. The
not-found tests above exist to catch this.

**Write-on-read.** `GET /v1/users/:u/plan` creates the user and subscribes them
to the default plan when it has not seen them before, returning 200 rather than
404 (`apitest/qms_subscriptions_test.go`, golden `user_plan_autocreated`).
Terrain depends on it. The goqu version has to keep the side effect.

**Not a risk, recorded to avoid re-litigating it.** `internal/qmsapi/query` is
Echo query-parameter validation built on `go-playground/validator`, with no
database access. It is imported by `controllers/users.go` and
`controllers/subscriptions.go` and is unaffected by the rewrite.
