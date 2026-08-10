# goqu-unification findings

Characterization work for the goqu rewrite (see
`.superpowers/sdd/2026-08-10-goqu-unification/`) sometimes turns up existing
service behavior that looks like a bug but is being pinned deliberately,
because the branch's policy is "reproduce current behavior exactly, bugs
included." Each entry below records one such case: what the route actually
does, why, which golden file pins it, and what a later task must NOT do while
translating the underlying query to goqu. Append new entries here rather than
opening a second document.

## `GET /v1/plans/{plan_id}/active-quota-defaults`: `resource_type` is always empty

**Observed behavior:** each entry in the `result` array carries a
`resource_type` sub-object containing only `"consumable": false` — never
`id`, `name`, or `unit`, regardless of which resource type the quota default
actually belongs to.

**Root cause:** `GetActivePlanQuotaDefaults`
(`internal/qmsapi/db/plan.go:142-149`) selects only
`resource_type_id, id, plan_id, quota_value, effective_date` and never joins
or preloads `resource_types`:

```go
err = db.
    WithContext(ctx).
    Select("DISTINCT ON (resource_type_id) resource_type_id", "id", "plan_id", "quota_value", "effective_date").
    Where("effective_date <= CURRENT_TIMESTAMP AND plan_id = ?", planID).
    Order("resource_type_id").
    Order("effective_date desc").
    Find(&planQuotaDefaults).
    Error
```

so the embedded `ResourceType` struct on each `model.PlanQuotaDefault` is left
at its Go zero value. `ID *string`, `Name string`, and `Unit string` all carry
`json:"...,omitempty"` tags (`internal/qmsapi/model/resource.go:36`, `:41`,
`:46`), so their zero values (`nil`, `""`, `""`) are dropped from the JSON
response entirely. `Consumable bool` has no `omitempty` tag
(`internal/qmsapi/model/resource.go:52`), so its zero value (`false`) is the
only field left in the output — which is what makes the response look like a
real (but wrong) `consumable: false` value rather than an obviously-missing
field.

**Golden that pins it:** `apitest/testdata/v1_plan_active_quota_defaults.json`

**Warning for the goqu rewrite:** the natural way to translate
`GetActivePlanQuotaDefaults` to goqu is to join `resource_types` and select
its columns, since the query already joins that table by
`resource_type_id`. **Do not do this.** Adding the join/preload would
populate `resource_type` correctly, which is a behavior change, and would
fail the branch's golden-diff success criterion. Reproduce the current,
under-populated shape exactly. Fixing this is deliberately deferred to a
follow-up change outside this branch.

## `GET /v1/users` and `GET /v1/usages/{username}/updates`: no `ORDER BY`, rows come back in arbitrary order

**Observed behavior:** both listings can return their rows in any order,
because neither query sorts.

**Root cause:**
- `GetAllUsers` (`internal/qmsapi/controllers/users.go:38`) runs
  `s.GORMDB.Find(&data)` with no `.Order(...)`.
- `Server.userUpdates`, which backs `GetAllUsageUpdatesForUser`
  (`internal/qmsapi/controllers/usages.go:189-204`), runs a GORM `Find` on
  `updates` joined to `users` — also with no `.Order(...)`. The route handler
  is at `usages.go:238-264`.

Postgres makes no ordering guarantee for a `Find` without `ORDER BY`; with more
than one row, physical row order (and therefore JSON array order) is
unspecified and can change between runs or after a rewrite of the query.

**Why it matters for goldens:** a golden file is a byte-for-byte comparison,
so a golden built from a multi-row response would be flaky against these
queries today, and — the sharper risk — a query that *happens* to return rows
in a stable order right now would silently break as soon as it's rewritten,
even without a real behavior change.

**How each is pinned:** rather than widening the harness's
`unorderedFields` (which keys on `"result"`, a field name shared by every
`/v1` response — sorting it would also sort `subscriptions_list`, whose whole
point is proving `sort-field=username&sort-dir=asc` works), each route is
pinned two ways:
- A golden over the single-row case, where order is unobservable:
  `apitest/testdata/v1_users_single.json` (`TestListUsersSingle`) and
  `apitest/testdata/v1_usage_updates_single.json`
  (`TestListUsageUpdates`).
- An order-insensitive assertion in Go over a multi-row case, checking only
  that every row comes back (`TestListUsersReturnsEveryUser` in
  `apitest/qms_root_test.go`; `TestListUsageUpdatesReturnsEveryUpdate` in
  `apitest/qms_usage_updates_test.go`).

**Warning for the goqu rewrite:** translating a GORM `Find` to goqu is a
natural place to add an `ORDER BY` — goqu's query builder makes sorting easy
to reach for, and it looks like a strict improvement. **Do not add one to
either query.** The absent ordering is current behavior; adding it here would
be a real behavior change that happens not to break either golden (both only
have one row), so it would ship silently. The single-row goldens plus the
in-Go multi-row count/membership assertions are a deliberate workaround for
an untestable ordering, not an oversight to "fix" by sorting.

## `GET /summary/{user}`: every nested quota and usage has a blank `subscription_id`

**Observed behavior:** in the response's `subscription.quotas[]` and
`subscription.usages[]` arrays, every entry carries `"subscription_id": ""`
rather than the real subscription UUID, even though the subscription itself
is unambiguous (it's the same subscription the response is about).

**Root cause — two different bugs, not one:**

- **Usage:** the value is fetched from the database and then thrown away.
  `SubscriptionUsages` (`db/userplans.go:253-268`) selects
  `usages.subscription_id` into `Usage.SubscriptionID`
  (`db/userplans.go:257`), so the struct genuinely holds the UUID after the
  query runs. `Usage.ToQMSUsage()` (`db/types.go:441-450`) then builds its
  return value without copying that field into `qms.Usage.SubscriptionId` —
  the return literal only sets `Uuid`, `Usage`, `ResourceType`, `CreatedBy`,
  `CreatedAt`, `LastModifiedBy`, `LastModifiedAt`. A real value is read and
  silently dropped in the mapping function.
- **Quota:** the value is never wired up to begin with. `Quota`
  (`db/types.go:453-462`) has no `SubscriptionID` field at all, and
  `SubscriptionQuotas`'s `Select` (`db/userplans.go:317-331`) never fetches
  `quotas.subscription_id` — unlike its `Usage` counterpart, it doesn't even
  reach the database for this column. `Quota.ToQMSQuota()`
  (`db/types.go:464-474`) has nothing to copy even if it wanted to.

**Why this isn't the same thing as the addon precedent:** `ToQMSSubscription`
(`db/types.go:169-174`) blanks the nested addon's `SubscriptionId` too
(`addons[i].SubscriptionId = ""`), and it is tempting to read the quota/usage
gaps as "the same deliberate choice, just applied consistently." It isn't.
For addons, `SubscriptionAddon.ToQMSType()` (`db/types.go:704-715`) first
populates `SubscriptionId` from a real column, and the very next line in
`ToQMSSubscription` explicitly overwrites it back to `""`. Someone populated
the field and then chose to erase it — that's legible intent, and it's a
single, deliberate site. Usage's blanking happens because a mapping function
was never updated to include a field that was already available; quota's
happens because the field and the query column were never added. Neither of
those is a decision anyone made on purpose about what should appear in a
`/summary/{user}` response; they're two different omissions that happen to
produce the same visible symptom.

**The field is not optional on the wire.** `qms.Quota` and `qms.Usage` both
declare `SubscriptionId string` with a `json:"subscription_id"` tag and no
`omitempty` — the schema expects a real value here, which is exactly why the
JSON shows an explicit `""` rather than the field being absent.

**Golden that pins it:** `apitest/testdata/summary_basic.json` and
`apitest/testdata/summary_unknown_user.json` — both show `"subscription_id":
""` on every entry in `quotas` and `usages`.

**Currently harmless, but not because it's correct:** the one caller of this
endpoint, resource-usage-api's `HTTPSummarizer.LoadSummary`
(`internal/summarizer/httpsummarizer.go:83-113`), reads only `Uuid`,
`Quota`/`Usage`, `ResourceType`, and `LastModifiedAt` off each entry; its own
`clients.Quota`/`clients.Usage` structs don't even have a field to receive
`SubscriptionID` if it were populated. No other consumer of
`GET /summary/{user}` was found across terrain, apps, app-exposer, or
data-usage-api. That bounds today's blast radius; it does not make the wire
contract correct, and a future caller that does read `subscription_id` off a
nested quota or usage would silently get the wrong thing (an empty string,
not an error).

**Why this is recorded despite being out of scope for the rewrite:**
`GET /summary/{user}` is already goqu and is not touched by any later task in
this branch's plan, so nobody translating a GORM query to goqu will pass
through this code and rediscover it. Recording it here is the only way the
knowledge survives past this task.

## `PUT /quotas`: a resource type identified only by name produces a 500

**Observed behavior:** the request body shape the wire contract documents —
`AddQuotaRequest.Quota.ResourceType` identified by `name`/`unit`, no `uuid` —
returns HTTP 500 with `{"error_code":"INTERNAL","message":"pq: invalid input
syntax for type uuid: \"\""}`.

**Root cause:** `addQuota` (`app/quotas.go:30-36`) passes
`request.Quota.ResourceType.Uuid` straight to `d.UpsertQuota` as the
`resource_type_id` column value, with no fallback to a name lookup:

```go
err = d.UpsertQuota(
    ctx,
    float64(request.Quota.Quota),
    request.Quota.ResourceType.Uuid,
    subscriptionID,
    db.WithTX(tx),
)
```

`PUT /plans`'s handler resolves the same situation correctly: `addPlan`
(`app/plans.go:70-75`) calls `d.LookupResoureType`
(`db/resourcetypes.go:117-129`), which falls back to a name lookup when `ID`
is empty. `addQuota` has no equivalent call, so a caller that only knows the
resource type's name — the only identification the task brief's read of the
wire contract documents — sends an empty string as the UUID, which Postgres
rejects at the `quotas.resource_type_id` column.

**Golden that pins it:** `apitest/testdata/goqu_quota_added.json`
(`TestGoquAddQuota` in `apitest/goqu_duplicates_test.go`), with `wantStatus`
set to `http.StatusInternalServerError` to match the observed behavior rather
than the brief's `http.StatusOK` expectation.

**Why this is recorded despite having no known caller:** `PUT /quotas` is one
of the six goqu routes being kept without a verified caller (spec §Scope);
this task exists specifically to surface exactly this kind of latent bug
before a caller shows up and hits it. Not a regression to fix on this
branch — reproduce it, don't repair it.

## `GET /plans/{plan_id}` (goqu route): `plan_rates` and `effective_date` are silently dropped

**Observed behavior:** `goqu_plan_get.json` shows `"plan_rates": null` for a
plan that has a rate (the Basic plan's `plan_rates` array is non-empty in
both `goqu_plans_list.json` and the `/v1` counterpart
`plans_get_basic.json`), and every entry in `plan_quota_defaults[].effective_date`
is `null` even though the same field is a real timestamp everywhere else this
plan appears (`goqu_plans_list.json`, `plans_get_basic.json`).

**Root cause:** `getPlan` (`app/plans.go:132-166`) builds
`response.Plan` by hand instead of calling `Plan.ToQMSPlan()`
(`db/types.go:216-234`), which is what `listPlans` uses
(`app/plans.go:27`). The manual construction never sets `PlanRates` at all —
`plan.Rates`, which `db.GetPlanByID` does populate via `loadPlanDetails`
(`db/plans.go:91-103, 142`) — is simply not read. Likewise, the
`qms.QuotaDefault` literal it builds (`app/plans.go:153-162`) sets `Uuid`,
`QuotaValue`, and `ResourceType` but never `EffectiveDate`, even though
`q.EffectiveDate` (`db.PlanQuotaDefault`) holds a real value fetched by the
same call. The data is fetched correctly; the handler's own response mapping
drops it.

**Golden that pins it:** `apitest/testdata/goqu_plan_get.json`
(`TestGoquPlanReads/get_a_plan_by_ID`). Still returns `http.StatusOK` — this
is a response-shape gap, not an error.

**Why this is recorded:** `GET /plans/{plan_id}` (the goqu route, distinct
from `/v1/plans/{id}`, which is unaffected and calls a different handler)
had never been exercised by a test before this task. A later change that
collapses this route onto the `/v1` implementation, or that refactors
`getPlan` to use `ToQMSPlan()` for consistency with `listPlans`, would be a
visible behavior change for any caller that starts depending on this route
and needs to know it's fixing something rather than breaking something.
