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

## `PUT /users/{username}/usages`: the response drops the row's identity and audit fields

**Observed behavior:** `apitest/testdata/goqu_usage_added.json` shows the
returned `usage` object with `"uuid": ""`, `"created_at": null`,
`"created_by": ""`, `"last_modified_at": null`, and `"last_modified_by": ""`
— all zero values — even though the usage row this request just wrote (or
updated) has real values for every one of those columns.

**Root cause:** `addUsage` (`app/usages.go:76-153`) builds its response
usage manually at `app/usages.go:134-143`:

```go
response.Usage = &qms.Usage{
    Usage:          u,
    SubscriptionId: subscription.ID,
    ResourceType: &qms.ResourceType{
        Uuid:       resourceType.ID,
        Name:       resourceType.Name,
        Unit:       resourceType.Unit,
        Consumable: resourceType.Consumable,
    },
}
```

`u` here is a bare `float64` — the current usage value returned by
`d.GetCurrentUsage` (`app/usages.go:128`), not the `db.Usage` row it comes
from — so there is no row to read `Uuid`, `CreatedAt`, `CreatedBy`, or
`LastModifiedBy`/`LastModifiedAt` from at the point this struct literal is
built. `getUsages` (`app/usages.go:15-58`), the read-side handler for the
same table, populates all five of these fields correctly
(`app/usages.go:40-53`) because it builds its response from the actual
`db.Usage` rows `d.SubscriptionUsages` returns, not from a derived scalar.

**Golden that pins it:** `apitest/testdata/goqu_usage_added.json`
(`TestGoquAddUsage` in `apitest/goqu_duplicates_test.go`). Still returns
`http.StatusOK` — like the `GET /plans/{plan_id}` gap above, this is a
response-shape omission, not an error.

**Current blast radius:** `PUT /users/{username}/usages` is one of the six
goqu routes kept without a verified caller (spec §Scope) — no caller was
found across terrain, apps, app-exposer, resource-usage-api, or
data-usage-api. As with `GET /summary/{user}`'s blanked `subscription_id`
above, that bounds today's impact; it does not make the response correct. A
caller that reads `uuid` off this response to reference the usage row
later — the obvious thing to do with a primary key a create/update endpoint
hands back — would get an empty string instead of an error, and a caller
reading `created_at`/`last_modified_at` for display or auditing would get
nothing where the sibling `GET` route would have given them a real
timestamp.

**Why this is recorded:** this task is exactly the point where the gap
becomes visible — the route had no prior test, so nobody had looked at what
it actually returns. Recording it here, in the same terms as the
`GET /plans/{plan_id}` gap above, keeps the two shape-omission findings this
task turned up consistent with each other rather than one being written down
and the other only living in a report.

## Golden-vs-`/v1`-counterpart comparison for the six retained goqu duplicates

Each of the six goqu routes covered in `apitest/goqu_duplicates_test.go`
duplicates an actively-called `/v1` route. This table records the structural
differences between each pair's golden, since that comparison — not just
"does the goqu route work" — is what a later decision about collapsing the
duplicates needs.

| goqu golden | `/v1` counterpart | comparison |
|---|---|---|
| `goqu_plans_list.json` (`GET /plans`) | `plans_list.json` (`GET /v1/plans`) | Same data, different envelope and field names. `/v1` wraps in `{"status": "OK", "result": [...]}`; goqu wraps in `{"header": {"map": {}}, "error": null, "plans": [...]}`. Within each plan, `/v1` uses `"id"` where goqu uses `"uuid"` (plan, quota default, plan rate, and resource type all rename this way). Otherwise the same fields, same values, same nesting. |
| `goqu_plan_get.json` (`GET /plans/{id}`) | `plans_get_basic.json` (`GET /v1/plans/{id}`) | Same envelope/naming differences as above, **plus a real content gap**: goqu's `plan_rates` is `null` and every `plan_quota_defaults[].effective_date` is `null`, while `/v1`'s response has both fully populated. This is the `getPlan` handler bug recorded above — both routes read the same underlying data via `db.GetPlanByID`, but the goqu HTTP handler drops fields the `/v1` handler keeps. |
| `goqu_plan_added.json` (`PUT /plans`) | `v1_plan_created.json` (`POST /v1/plans`) | Very different shape, not just renamed fields. `/v1`'s create returns almost nothing on success — `{"status": "OK", "result": "Success"}`, no plan data at all. goqu's `PUT /plans` returns the full created plan (`uuid`, `name`, `description`, empty `plan_quota_defaults`/`plan_rates` arrays for this request). A caller wanting the new plan's ID back would need the goqu route; `/v1` doesn't give it one. |
| `goqu_quota_added.json` (`PUT /quotas`) | `quota_update_cpu_hours.json` (`POST /v1/users/{u}/plan/{rt}/quota`) | Not a like-for-like comparison as recorded, since the goqu side is the 500 error body above — but structurally, even on a success path these two return fundamentally different things. `/v1`'s quota-update endpoint returns the **entire subscription** (plan, plan_rate, all quotas, all usages, user), nested three levels deep. goqu's route is built to return just the single updated `quota` object. Collapsing these isn't a rename exercise — it changes what the caller gets back, from a subscription snapshot to a single record or vice versa. |
| `goqu_usage_added.json` (`PUT /users/{u}/usages`) | `usage_add_set.json` (`POST /v1/usages`) | Very different shape. `/v1`'s usage-update endpoint returns a bare string message — `{"status": "OK", "result": "successfully updated the usage for: testuser"}` — no usage data at all. goqu's route returns a `usage` object (incomplete, per the finding above, but structured — `usage` value, `resource_type`, `subscription_id`). A caller wanting the resulting usage value back programmatically would need the goqu route; `/v1` only gives a human-readable message. |
| `goqu_user_updates.json` (`GET /users/{u}/updates`) | `v1_usage_updates_single.json` (`GET /v1/usages/{u}/updates`) | Same data, different shape beyond the envelope. `/v1` uses `"resource_types"` (plural) as the key for the nested resource type object; goqu uses `"resource_type"` (singular). `/v1`'s `metadata` is `"{}"` (a JSON object literal as a string); goqu's is `""` (empty string) — a different default representation of "no metadata," not just a naming difference. Field-for-field naming otherwise lines up (`id`/`uuid`, `user`, `operation`, `value`, `value_type`, `effective_date`). |

**Structural takeaway:** three of the six pairs (`plans_list`, `plan_get`
modulo its bug, `user_updates`) are close enough that collapsing them onto
one shape is mostly a field-rename exercise. The other three
(`plan_added`, `quota_added`, `usage_added`) are not — the `/v1` write
endpoints return bare success-message envelopes with no created/updated
data, while the goqu routes return the actual object. Collapsing those three
would either change what the `/v1` routes return today (a real behavior
change for their actual callers) or drop what the goqu routes return
(removing the reason to have kept them). Whoever makes the collapse call
later needs to know that asymmetry going in.
