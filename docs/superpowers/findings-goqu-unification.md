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
