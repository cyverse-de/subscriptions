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

## `goquTransaction` vs `transaction`: a failed ROLLBACK discards the `txAbort` marker

**Observed behavior:** the two transaction helpers in
`internal/qmsapi/controllers/root.go` — `transaction` (GORM, `:54`) and
`goquTransaction` (goqu, `:68`) — agree on every path except one: when the
callback returns an error *and the ROLLBACK itself then fails*, the GORM helper
still returns the callback's error, while the goqu helper returns the rollback
error instead. If the callback's error was a `txAbort`, the goqu helper's
`errors.As` unwrapping never fires and the already-written response is not
returned to echo.

**Root cause:** it is a difference between the two libraries' wrappers, not
between the two helpers' own code. `goqu`'s
`(*TxDatabase).Wrap` (`goqu/v9@v9.19.0/database.go:631-648`) *replaces* the
callback's error with the rollback error:

```go
defer func() {
    if p := recover(); p != nil {
        _ = td.Rollback()
        panic(p)
    }
    if err != nil {
        if rollbackErr := td.Rollback(); rollbackErr != nil {
            err = rollbackErr
        }
    } else {
        if commitErr := td.Commit(); commitErr != nil {
            err = commitErr
        }
    }
}()
return fn()
```

GORM's `(*DB).Transaction` (`gorm@v1.31.2/finisher_api.go:662-667`) instead
discards the rollback result entirely — `tx.Rollback()` is called for effect in
a deferred block and its error is never assigned to the named return — so the
callback's error always survives.

**Why it is nonetheless safe to share `txAbort` between the two — but only on
the `txAbort` path:** `txAbort` is only ever constructed by `txError`
(`root.go:33`), which builds it from `model.Error`, and `model.Error`
(`internal/qmsapi/model/root.go:62`) has already written the response body via
`ctx.JSON` by the time the wrapper sees anything. So on that path the response
is committed, and `App.New`'s custom `HTTPErrorHandler` (`app/app.go:48-55`)
returns early whenever `c.Response().Committed` is true. Under GORM the helper
returns `abort.response` (normally `nil`, since `ctx.JSON` succeeded) and echo
does nothing; under goqu the helper returns the rollback error and echo's
handler drops it on the `Committed` check. Same bytes on the wire either way.

**On the ordinary-error path the divergence *is* observable**, and the claim
should not be overstated: a callback that returns a plain error has written no
response, so `Committed` is false and echo's handler falls through to its
`default` branch (`app/app.go:70-74`), rendering
`common.NewErrorResponse(err)`. If the ROLLBACK then fails, goqu substitutes the
rollback error and the client gets that message instead of the handler's real
one — the same 500 status, different body text. Nothing in the golden set
covers it (it needs a failing ROLLBACK), so it is a latent message-fidelity
difference, not a status-code difference.

Nor is a failed `Rollback` proof of a broken connection: `database/sql` returns
`sql.ErrTxDone` whenever the transaction was already committed or rolled back,
on a perfectly healthy connection. The goqu layer already commits and rolls
back caller-supplied transactions under `WithTXRollbackCommit`
(`db/db.go:137-143`; the `doRollback`/`doCommit` blocks that act on it are at
`db/addons.go:408`, `:441`, `:477`, `:514`), so a converted call that routes
through one of those can reach the wrapper with the transaction already
finished and get `ErrTxDone` rather than a dead socket.

**Panic and commit paths do agree**, which is worth stating explicitly since
they are the paths a reviewer is most likely to worry about: both wrappers roll
back and re-panic on a panic (goqu recovers, rolls back, and re-panics; GORM
rolls back in its defer and lets the panic propagate), so `middleware.Recover`
(`app/app.go:46`) still turns it into a 500; and both return the commit error
when the callback succeeded but COMMIT failed.

**Warning for Tasks 11-14:** do not "fix" this by capturing the callback's
error in a closure variable and preferring it over `Wrap`'s return, and do not
write a goqu-flavored copy of `txAbort`/`txError`. The first would put
`goquTransaction` out of step with the rest of the goqu layer, which drives
transactions with a bare `tx.Wrap` (`app/app.go`'s `addUserUpdate`, `:287`);
the second would fork the contract the whole migration depends on. Both would
be doing it to fix what is at worst a 500-body message. The single shared
`txAbort` is what makes a handler moved from `transaction` to
`goquTransaction` behave identically; keep it single.

### Convert each handler whole: never let one request hold both a GORM and a goqu transaction

This is the one genuine non-equivalence, and the per-entity migration in Tasks
11-14 walks straight into it.

GORM's `Transaction` detects that it is already inside a transaction and nests
with a `SAVEPOINT` (`finisher_api.go:640-654`). `goquTransaction` always calls
`Begin()`, so it can only ever open a *new* transaction. The dangerous shape is
not `goquTransaction` inside `goquTransaction` — it is a **partially converted
handler**: code still running inside a GORM `transaction` callback that calls a
newly-converted goqu query, or the reverse. In that mixed case the obvious
remedy is unavailable, because the two transaction handles are not
interconvertible: a `*gorm.DB` transaction cannot be threaded into a goqu
query, and a `*goqu.TxDatabase` cannot be threaded into a GORM one.

**The failure mode is a hang, not an error.** Both transactions come from the
same `*sql.DB` pool, so they sit on different connections. If they touch the
same rows, the second blocks on the first's locks while the goroutine holding
the first is blocked waiting for the second to return — a self-deadlock that
resolves only when a lock or statement timeout fires. It produces no error a
test can assert on and no response diff, so **the golden-file gate cannot catch
it.** This is the one Phase 2 hazard that passes CI and fails in production.

**The rule:** convert a handler and everything it calls in a single change. No
request may hold a GORM transaction and a goqu transaction at the same time. If
a handler is too large to convert at once, convert its *queries* to goqu only
after its transaction has been converted, never before.

**All fifteen `/v1` transaction-opening sites** must be converted — the ten that
go through the helper, plus five that call `s.GORMDB.Transaction(...)` directly
and so are invisible to a search for `s.transaction`:

- via `s.transaction` (10): `plans.go:131`, `:204`, `:261`, `:327`, `:439`;
  `resource_types.go:182`; `users.go:65`, `:139`, `:208`, `:305`
- raw `s.GORMDB.Transaction` (5): `subscriptions.go:232`, `:316`;
  `usages.go:78`, `:223`; `users.go:413`

**None of the fifteen is nested today**, which is what makes a staged migration
possible at all — but the property is easy to destroy and worth re-checking
after each conversion. The two that look nested are not:
`subscriptions.go:232` is indented because it sits in a `for` loop over
`body.Subscriptions`, and the `SubscriptionAdder.AddSubscription` it calls
takes the `tx` it is handed (`subscriptions.go:81`) rather than opening its
own; and `usages.go:78`'s `addUsage` is reached from `AddUsages`
(`usages.go:177`), which is a plain handler body, not a transaction callback.

## Four not-found conventions coexist: `qmsdb.ErrNotFound` only replaces one of them

`qmsdb.ErrNotFound` (`internal/qmsapi/db/errors.go`) was added to replace
`gorm.ErrRecordNotFound` as the 404-versus-500 signal. It replaces **that one
convention only**, and the repo has three others that `errors.Is(err,
qmsdb.ErrNotFound)` will not match. A converted query that surfaces one of them
produces a silent 500 where a 404 is owed — exactly the bug the sentinel exists
to prevent, reintroduced by a different route.

The four, and who answers to each:

1. **`gorm.ErrRecordNotFound`** — what `qmsdb.ErrNotFound` replaces. Live sites:
   `internal/qmsapi/db/plan.go:34`, `:102`; `db/resource_types.go:20`, `:36`;
   `db/user.go:39`; `db/subscriptions.go:195`, `:197`;
   `controllers/resource_types.go:128`; `controllers/usages.go:105`.
2. **`suberrors.Err*NotFound`** (top-level `errors` package):
   `ErrUserNotFound` (`errors/errors.go:14`), `ErrAddonNotFound` (`:24`),
   `ErrSubAddonNotFound` (`:25`). The **goqu layer already returns these
   today** — `db/addons.go:302`, `:372`, `:506`. Any Task 11-14 query that
   reuses a top-level `db` helper inherits them.
3. **A second, unrelated `ErrUserNotFound` local to `controllers`**
   (`usages.go:27`). This is a *different value* from `suberrors.ErrUserNotFound`
   despite carrying the identical message string `"user name not found"`, so the
   two are indistinguishable in a log and non-interchangeable under `errors.Is`.
   It is the one `httpStatusCode` (`usages.go:37-49`) actually matches to return
   404; the `suberrors` twin would fall through to the `default` 500.
4. **`sql.ErrNoRows`** — what goqu over sqlx will actually raise once converted.

**The rule for Tasks 11-14:** translate to `qmsdb.ErrNotFound` **inside the
`internal/qmsapi/db` query functions themselves**, at the point where
`sql.ErrNoRows` is first observed — never in a handler. Handlers should keep
matching one sentinel. A handler that starts sniffing for `sql.ErrNoRows`, or
that matches whichever of the two `ErrUserNotFound` values happens to be in
scope, is how convention 3's trap gets re-sprung.

**Related hazard while converting:** most existing not-found checks compare with
`==`, not `errors.Is` (`plan.go:34`, `:102`; `resource_types.go:20`, `:36`;
`user.go:39`; `subscriptions.go:195`, `:197`; `usages.go:105` — only
`controllers/resource_types.go:128` uses `errors.Is`). `==` happens to work
against GORM because GORM returns the bare sentinel, but it breaks the moment a
converted query wraps its error with `%w`. Use `errors.Is` in converted code,
per `CLAUDE.md`.

## `goquTransaction` starts its transaction without a context

`db.Database` exposes only `Begin()` (`db/db.go:47`) — it never surfaces goqu's
`BeginTx(ctx, opts)` — and `Wrap` issues its COMMIT/ROLLBACK without a context
either. So `goquTransaction` cannot carry the request context on the
transaction itself.

**This is parity with the GORM helper, not a regression:** `transaction` calls
`s.GORMDB.Transaction(fn)` with no context either. It is called out because
`CLAUDE.md` asks for context on transaction-starting calls, and a reader
comparing the two helpers will notice the gap and may try to "fix" it mid-
migration.

**What Tasks 11-14 must do:** keep threading the request context *per query*,
which is how the GORM callbacks do it today (`plans.go:159` uses
`tx.WithContext(context)`; `internal/qmsapi/db/user.go:18` likewise). In goqu
that means using the `*Context` variants on the transaction — `ScanStructContext`,
`ScanStructsContext`, `ScanValContext`, `ExecContext` — rather than their
context-free siblings. Reaching for the bare variants because `Wrap` took no
context would silently drop per-query cancellation that the GORM code had.
Giving `db.Database` a `BeginTx` is a reasonable follow-up, but it is a change
to the shared goqu layer used by the non-`/v1` routes, so it does not belong in
a per-entity conversion task.

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

## Task 11: converting a shared query function means forking it, not editing it

Task 11's brief said to change all five functions in
`internal/qmsapi/db/resource_types.go` so their `*gorm.DB` parameter becomes a
`*goqu.TxDatabase`. Taken literally that breaks three handlers that no task
converts until later, because two of the five have callers outside
`controllers/resource_types.go`:

| function | external callers |
|---|---|
| `GetResourceTypeByName` | `plans.go:144`, `usages.go:88`, `users.go:141` |
| `ListResourceTypes` | `plans.go:346` |
| `GetResourceTypeByID`, `UpdateResourceType`, `SaveResourceType` | none |

All four external call sites sit **inside GORM transaction callbacks**. Changing
the shared signature would either fail to compile or force those handlers to
hold a GORM transaction and a goqu transaction at once — the self-deadlock
described above, which the golden gate cannot catch.

**The pattern Tasks 12-14 should reuse:** put the goqu implementations in a new
`*_goqu.go` file under the canonical names, rename the GORM originals in place
with a `GORM` suffix (`GetResourceTypeByNameGORM`, `ListResourceTypesGORM`, …),
and repoint the not-yet-converted callers at the renamed functions. The rename
is mechanical and behavior-free, it makes the remaining GORM surface
self-labelling, and Task 15 deletes each `*GORM` function once its last caller
is gone. Converting a shared query in place is only safe when every caller is
converted in the same change.

## `PUT /v1/resource-types/{id}`: a failed homonym lookup is reported as 409, not 500

`UpdateResourceType`'s handler checks for an existing resource type with the
new name, and reports **any** error from that lookup as
`http.StatusConflict`:

```go
homonym, err := db.GetResourceTypeByName(context, tx, inboundResourceType.Name)
if err != nil {
    return txError(ctx, err.Error(), http.StatusConflict)
}
```

A dead connection or a malformed query is not a naming conflict, so a genuine
server fault is reported to the client as "you picked a name that's taken,"
with the raw database error as the message body. The sibling check three lines
above (`GetResourceTypeByID`) maps its errors to 500, which is what makes the
409 here look like a copy-and-paste slip rather than a decision.

**Nothing pins it:** no golden covers a failing lookup, so this is invisible to
the gate in both directions.

**How the goqu conversion preserved it:** the GORM version returned
`(nil, nil)` for "no such name," so only a real failure reached the `err != nil`
branch. The goqu version returns an error matching `qmsdb.ErrNotFound` instead,
so the converted handler reads
`if err != nil && !errors.Is(err, db.ErrNotFound)` — absence is still not a
conflict, and every other error still yields the same 409 it did before. Do not
"fix" the status code while converting; it is current behavior, and repairing it
belongs in a follow-up change outside this branch.

## Converted read handlers now open a transaction, which changes the body of a database-down 500

`ListResourceTypes`, `AddResourceType`, and `GetResourceTypeDetails` ran their
queries directly against `s.GORMDB` with no explicit transaction. Their goqu
replacements take a `*goqu.TxDatabase` (per `CLAUDE.md`: every database function
takes a transaction), so each handler now wraps its work in
`s.goquTransaction`.

The one observable consequence is on the `Begin()` failure path. Previously a
database failure surfaced from the query and was rendered by `model.Error`, so
the client got the QMS envelope (`{"status": "Internal Server Error", "error":
"…"}`). A failure to BEGIN happens before the handler body runs, so
`goquTransaction` returns the raw error to echo, and `App.New`'s
`HTTPErrorHandler` default branch (`app/app.go:70-74`) renders
`common.NewErrorResponse(err)` — a different JSON shape, same 500 status.

This only fires when the database is unreachable, and it already applied to
every handler that used `s.transaction`, so it is not new to the goqu layer. It
is recorded because Tasks 12-14 convert more transaction-less handlers and will
widen the set of routes it applies to, and because it is not reachable from any
golden.

## Every list conversion must initialize its destination slice: goqu turns an empty listing into `null`

This is the first behavior difference the golden gate provably cannot catch,
and it applies to every list endpoint Tasks 12-14 convert.

**The two layers disagree on the empty case.** GORM's `Find` replaces the
destination slice before it reads a single row
(`gorm@v1.31.2/scan.go:295-302`):

```go
if reflectValue.Cap() == 0 {
    db.Statement.ReflectValue.Set(reflect.MakeSlice(reflectValue.Type(), 0, 20))
}
```

so a query matching nothing still leaves a non-nil, zero-length slice, which
`encoding/json` renders as `[]`. goqu's `scanIntoSlice`
(`goqu/v9@v9.19.0/exec/scanner.go:132-141`) only touches the destination inside
`for s.Next()`:

```go
for s.Next() {
    row := reflect.New(elemType)
    if rowErr := it(row.Interface()); rowErr != nil { return rowErr }
    util.AppendSliceElement(val, row)
}
```

so zero rows leaves a `var xs []T` destination nil, which renders as `null`. A
client doing `result.map(...)` breaks on `null`.

**No golden can catch it.** Every list golden in the suite is backed by seeded
reference data or by fixtures the test creates, so all of them are non-empty.
The conversion is byte-identical on every recorded response and still changes
the wire contract for the case none of them records. Tasks 12-14 convert
listings where emptiness is routine rather than pathological — a user with no
updates, no usages, no subscriptions — so the exposure there is far larger than
it was for `/v1/resource-types`, which the migrations seed with two rows.

**The rule:** initialize the destination of every converted list query
(`xs := []T{}`, not `var xs []T`), and cover it with a Go assertion rather than
a golden. `TestListResourceTypesEmptyIsAnArray`
(`apitest/qms_resource_types_test.go`) is the pattern: empty the table inside a
transaction that is rolled back, call the query function with that transaction,
and assert the encoded form is `[]`. Rolling back matters — `resetDB`
deliberately preserves reference tables, and deleting `resource_types` for real
would cascade the seeded plan quota defaults away and break later goldens.

**A note on consistency, since it will come up:** the pre-existing goqu code in
the top-level `db` package (`db/plans.go:51`, `db/addons.go:117`) declares its
destinations nil and therefore already returns `null` on empty. Accepting
`null` in `/v1` would make the two trees agree. That is a real argument, but it
is an argument for a follow-up branch that changes both trees deliberately —
not for changing `/v1`'s contract as a side effect of a refactor whose success
criterion is "nothing changed."

## Converting a GORM `First()` to `ScanStruct`: check the predicate is unique

`First()` emits `ORDER BY <primary key> LIMIT 1`; goqu's `ScanStruct` emits
neither. It stops reading after the first row it gets, so the shape is similar,
but two things are lost: the deterministic choice of *which* row, and the
`LIMIT` that kept the database from producing the rest.

In the resource-type conversion this is provably safe — `GetResourceTypeByName`
filters on `name`, which is `UNIQUE` (`migrations/000002_plans.up.sql:30`), and
`GetResourceTypeByID` filters on the primary key — so at most one row can
match either way, and the transcription matches the house idiom at
`db/resourcetypes.go:70-80`, which also omits both clauses.

**The rule for Tasks 12-14:** before transcribing a `First()` into a bare
`ScanStruct`, check whether the filter is backed by a unique constraint. When it
isn't, the conversion silently swaps a deterministic row for an arbitrary one
*and* streams the whole result set to pick it — so add an explicit `.Limit(1)`
plus the `ORDER BY` that `First()` was applying. No golden will catch this
either when the fixture data happens to have one matching row.

## Two packages named `db` in `controllers`, both with a `GetResourceTypeByName`

`internal/qmsapi/controllers` imports two different packages that are both
named `db`: `github.com/cyverse-de/subscriptions/db` (root.go:9, the goqu layer
the non-`/v1` routes use) and `github.com/cyverse-de/subscriptions/internal/qmsapi/db`
(the `/v1` query layer). Go scopes imports per file, so this compiles, but it
means a bare `db.GetResourceTypeByName` resolves to a different function
depending on which file you are reading — and **both packages really do export
that name** (`db/resourcetypes.go:89` and
`internal/qmsapi/db/resource_types.go`). The two return different types and
signal not-found differently, which is the soil the "four not-found
conventions" trap grows in.

**The convention:** alias the internal package as `qmsdb` in the `controllers`
package, matching `app/app.go`, which already imports it that way.
`controllers/resource_types.go` was converted to the alias in Task 11. The
remaining controller files still import it bare; Tasks 12-14 should switch each
one as they convert it, rather than in a separate sweep, so the alias change
travels with the conversion it protects.

## Task 12: `GetActiveSubscriptionDetails` drags in seven more query functions, not zero

The brief scoped Task 12 as "two query functions": `UpsertUsage` and
`GetActiveSubscriptionDetails`. The second one is a two-line wrapper, and its
transitive closure is what the task actually is:

```
GetActiveSubscriptionDetails
├── GetActiveSubscription          (subscriptions join users, activeNow, First)
│   └── SubscribeUserToDefaultPlan
│       ├── GetUser                (users upsert)
│       ├── GetPlan                (plans + quota defaults + rates)
│       └── SubscribeUserToPlan    (subscriptions insert + cascaded quotas insert)
└── GetSubscriptionDetails         (subscription + 9 GORM preloads)
```

None of these could be left on GORM, because the "convert each handler whole"
rule forbids a request holding both kinds of transaction. **Tasks 13-15 should
expect the same shape**: a `/v1` query function that looks small usually sits on
top of GORM's `Preload`/association machinery, and every association it loads is
another goqu query to write. Sizing one of these tasks by counting the function
names in the brief will underestimate it by roughly an order of magnitude.

The functions this task converted, all under the coexist-then-delete pattern
(goqu takes the canonical name, GORM gets a `GORM` suffix): `GetUser`,
`UserExists`, `GetPlan`, `SubscribeUserToPlan`, `SubscribeUserToDefaultPlan`,
`GetActiveSubscription`, `GetSubscriptionDetails`,
`GetActiveSubscriptionDetails`, `UpsertUsage`. Three queries that lived inline in
`controllers/usages.go` moved into the db package as `GetUpdateOperationByName`,
`SaveUpdate` and `ListUpdatesForUser` — the first because the ErrNotFound rule
says absence is translated inside a query function, never in a handler, and the
other two to keep the file's queries in one layer.

### Capture the GORM SQL before writing the goqu, don't infer it

The reliable way to translate a `Preload` graph is to read the statements GORM
actually emits rather than reason about what it ought to emit. Temporarily give
`InitGORMConnection` (`internal/qmsapi/db/gorm.go`) a
`&gorm.Config{Logger: logger.Default.LogMode(logger.Info)}`, run the goldens
that cover the handler, and every statement is in the test output. Do the same
for the converted code afterwards with `goquDB.Logger(...)` in `db.New`
(`db/db.go`) and diff the two lists. Both edits are one line and get reverted
before committing.

Two things that capture turned up which inference would have missed:

- GORM's `First()` appends the primary key to an **existing** `ORDER BY` rather
  than replacing it, so `GetActiveSubscription` really emits
  `ORDER BY subscriptions.effective_start_date desc, subscriptions.id LIMIT 1`.
  The effective start date is not unique, so dropping the `id` tie-break would
  make the chosen subscription arbitrary for a user with two subscriptions
  starting at the same instant. The goqu version keeps both terms.
- GORM's cascaded association insert for a new subscription's quotas emits
  `ON CONFLICT ("id") DO UPDATE SET "subscription_id"="excluded"."subscription_id"`.
  That is dead weight, not behavior: the id is a fresh `uuid_generate_v1()`, so
  the conflict can never fire. The goqu version is a plain multi-row insert.

### `model` structs needed `db` tags, and the untagged default is a silent mismatch

goqu maps a struct field to a column by its `db` tag, falling back to
`strings.ToLower(FieldName)` (`goqu/v9@v9.19.0/internal/util/column_map.go`).
The `/v1` model structs had no `db` tags, so `Subscription.EffectiveStartDate`
mapped to a column named `effectivestartdate`, `PlanQuotaDefault.PlanID` to
`planid`, and so on. Task 11 did not hit this because every
`model.ResourceType` field happens to lowercase to its real column name.

Every field of every model struct the goqu layer scans now carries an explicit
`db` tag, and each association field carries `db:"-"`. The `-` matters twice
over: goqu treats an untagged **struct** field as a nested record and would look
for columns like `resourcetype.id`, and it treats an untagged slice field as a
leaf column. Associations are loaded by their own query and assigned in Go,
exactly as `Preload` did, so none of them is ever selected.

The tags are additive — GORM reads `gorm` tags and field names, and the JSON
encoding is untouched — but they are a shared edit to `internal/qmsapi/model`,
so Tasks 13-15 should check whether a struct they need is already tagged before
adding tags of their own.

### `GetPlan` now reports a missing plan as `ErrNotFound` rather than `(nil, nil)`

The GORM `GetPlan` returned `(nil, nil)` for an unknown plan name, and
`controllers/users.go:316` depends on that: it renders its own
`plan name '%s' not found` 400 when the returned plan is nil. The goqu
replacement follows the sentinel convention instead, so **Task 14 must convert
that nil check into `errors.Is(err, qmsdb.ErrNotFound)` when it repoints
`users.go` at the goqu function**, or a bad plan name will become a 500 with a
different body. `users.go` still calls `GetPlanGORM` today, so nothing has moved
yet.

The one path where the change is already live is
`SubscribeUserToDefaultPlan`, which looks up the `Basic` plan. Under GORM a
missing Basic plan produced a nil dereference and a panic-derived 500; under
goqu it produces an ordinary error and a 500 with a legible message. Basic is
seeded by the migrations and no route deletes plans, so the case is
unreachable — recorded because it is a real, if theoretical, difference in the
converted code.

### Two new Go assertions, because the cases they cover cannot be goldened

`apitest/qms_usages_test.go` adds:

- `TestEmptyUsageListingsAreArrays` — `GET /v1/usages/{username}` and
  `GET /v1/usages/{username}/updates` for a user with neither. This is the
  nil-slice hazard recorded above, and it is not hypothetical here: the usages
  array is `subscription.Usages`, which `GET /v1/usages/{username}` returns
  directly, and `model.Subscription.Usages` has no `omitempty`.
- `TestUsageListingSubscribesAnUnknownUser` — `GET /v1/usages/{username}` for a
  user the service has never seen. This read path *enrols* the user in the basic
  plan, and **no test covered the enrolment**: every other test reaches the
  route through `createUser`, which enrols them via `PUT /v1/users/{username}`
  first. The goqu `SubscribeUserToDefaultPlan` would have shipped unexercised.

Both were confirmed to pass unchanged against the `goqu-baseline` tag in a
scratch worktree before the conversion was trusted, which is what makes them
characterization tests rather than descriptions of the new code.

## Five query pairs are now goqu-vs-goqu duplicates across the two `db` packages

The "Two packages named `db` in `controllers`" section above records one name
collision, `GetResourceTypeByName`. It is not the only one, and Task 12 made the
situation qualitatively worse rather than just larger.

**Before Phase 2, every such pair was GORM-vs-goqu**, which a half-finished
migration justifies: one implementation was on the way out. Task 12 converted the
`/v1` side of five of them, so those pairs are now **goqu-vs-goqu, over the same
tables, with different Go types and different not-found conventions** — which is
precisely the soil the four-not-found-conventions trap grows in, and there is no
longer a migration in progress to explain it away.

| table | top-level `db` (goqu) | `internal/qmsapi/db` (goqu, as of Task 12) |
|---|---|---|
| `updates` (read) | `UserUpdates` (`db/updates.go:15`) | `ListUpdatesForUser` (`updates_goqu.go`) |
| `updates` (write) | `AddUserUpdate` (`db/updates.go:88`) | `SaveUpdate` (`updates_goqu.go`) |
| `users` (existence) | `UserExists` (`db/users.go:64`) | `UserExists` (`user_goqu.go`) |
| `users` (upsert) | `AddUser` (`db/users.go:81`), `EnsureUser` (`:108`) | `GetUser` (`user_goqu.go`) |
| `usages` (upsert) | `UpsertUsage` (`db/usages.go:45`) | `UpsertUsage` (`usage_goqu.go`) |

The two `UserExists` and the two `UpsertUsage` share a name outright, so which
one a reader is looking at depends on which file the import block is in — the
same hazard as `GetResourceTypeByName`, now three times over.

The pairs are not interchangeable, which is why this is a note and not a fix:
the top-level versions return `db.*` types and signal absence with
`suberrors.Err*NotFound`, the `/v1` versions return `model.*` types and signal it
with `qmsdb.ErrNotFound`, and the two `UpsertUsage` signatures differ
structurally (the top-level one takes an explicit `update bool` and scalar
arguments; the `/v1` one takes a `*model.Usage`). Collapsing them means choosing
a type and a convention per pair and rewriting the callers.

**This is recorded, deliberately not acted on.** Consolidation is a change to the
shared goqu layer that the non-`/v1` routes depend on, so it belongs to Task 16
or a follow-up branch, where it can be done once with its own gate — not as a
side effect of a per-controller conversion whose success criterion is "nothing
changed."

### `updates_goqu.go` has no GORM sibling to delete

Every other `*_goqu.go` file in `internal/qmsapi/db` sits beside a GORM original
that Task 16 removes. `updates_goqu.go` does not: its three functions
(`GetUpdateOperationByName`, `SaveUpdate`, `ListUpdatesForUser`) were lifted out
of `controllers/usages.go`, where they had been inline GORM calls, rather than
converted from a named function in the db package. There is no `updates.go` to
look for and nothing to delete beside it — the GORM code it replaced is already
gone.

## Positional binding of a multi-row `RETURNING` is silently wrong data

`saveSubscriptionQuotas` (`internal/qmsapi/db/subscriptions_goqu.go`) originally
ran `INSERT ... RETURNING id` over several `VALUES` rows, scanned the ids into a
`[]string`, and assigned `quotas[i].ID = &ids[i]`.

Postgres does emit `RETURNING` rows in `VALUES` order in practice, but it does
not document that as a guarantee, and nothing about the code would detect a
mismatch: the arity check passes either way, and the only visible symptom would
be each quota carrying another resource type's identifier. Every current caller
discards these ids — `GetActiveSubscriptionDetails` re-reads the subscription
through `GetSubscriptionDetails` — so no golden and no assertion can fail.

**The rule for Tasks 13-15:** when a cascaded insert has to hand generated keys
back to the structs that produced them, return a natural key alongside the
generated one and match by value. `saveSubscriptionQuotas` returns
`("id", "resource_type_id")` and indexes by resource type, which is unambiguous
because the unique index on `quotas (resource_type_id, subscription_id)` allows
one quota per resource type per subscription. Dropping the write-back entirely
was the other option and was rejected: GORM populated these ids, Tasks 14 and 15
both convert callers of `SubscribeUserToPlan`, and a caller that starts reading
`subscription.Quotas[i].ID` should find a correct value rather than a nil one.
