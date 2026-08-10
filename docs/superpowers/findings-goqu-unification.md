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
