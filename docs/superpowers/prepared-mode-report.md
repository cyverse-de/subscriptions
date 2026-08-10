# Enabling goqu prepared mode service-wide

Companion to the `internal/qmsapi/db moved from parameterized SQL to
string-interpolated SQL` entry in `findings-goqu-unification.md`, which recorded
the problem and deferred the fix. This records the fix.

The change is in two commits on `goqu-unification`:

| # | commit | subject |
|---|---|---|
| 1 | `555c235` | Execute the db-layer queries through goqu executors |
| 2 | HEAD | Bind query parameters instead of interpolating them |

## Where `SetDefaultPrepared(true)` went, and why

In an `init()` in `db/tables` (`db/tables/tables.go`).

`SetDefaultPrepared` is process-global, so the only question that matters is
whether it is guaranteed to run before the first query is *built* — in every
binary, not just the service. `main.go` alone fails that test: `apitest`
constructs its router directly (`apitest/harness_test.go:126-128`) without going
through `main`, so the suite would have exercised interpolated SQL while
production ran prepared SQL. Gating on a suite that tests the other mode is
worse than not gating at all.

`db/tables` is the one package that every query-building package in the repo
imports, and it is not a coincidence that can lapse quietly: the package exists
to hold the `goqu.T(...)` table identifiers, so a package that builds a query
against a table in this schema has a reason to import it. Verified rather than
assumed:

```
$ go list -deps ./db | grep cyverse-de/subscriptions
github.com/cyverse-de/subscriptions/db/tables
github.com/cyverse-de/subscriptions/errors
github.com/cyverse-de/subscriptions/db

$ go list -deps ./internal/qmsapi/db | grep cyverse-de/subscriptions
github.com/cyverse-de/subscriptions/db/tables
github.com/cyverse-de/subscriptions/internal/qmsapi/model/timestamp
github.com/cyverse-de/subscriptions/internal/qmsapi/model
github.com/cyverse-de/subscriptions/internal/qmsapi/db
```

`db/tables` is the unique common ancestor of both query-building packages, and
Go guarantees an imported package's `init()` runs before the importing package's
own initialization and before `main`. So the setting is in place before the
first dataset is constructed in the service binary, in `apitest`, and in any
test binary someone adds later without knowing this setting exists.

The alternatives were considered and rejected. `main.go` misses the tests. A
dedicated `internal/goqusetup` package imported blank from both `db` and
`internal/qmsapi/db` is exactly as forgettable as `main.go` — a blank import is
a discipline, not a constraint, and this repo already shows what happens to that
discipline: the postgres dialect registration is a blank import duplicated
between `main.go:27` and `apitest/harness_test.go:39`. An `init()` in each of
the two db packages would work but duplicates the setting and its rationale.

Residual fragility, stated plainly: a future package could build a goqu query
without importing `db/tables` (nothing in the compiler prevents it, and
`db/users.go` already writes `goqu.T("users")` inline rather than using
`t.Users`). Such a package would still be covered as long as it lives under the
existing `db` or `internal/qmsapi/db` trees, and a brand-new top-level
query-building package is a large enough change that the import is worth
checking then. The comment on the `init()` says why it lives there.

## The four call sites that discarded their bind arguments

All four were in the pre-existing `db/` layer. Each rendered the dataset to a
string, threw away the args slice with `_`, and executed the string alone —
correct only while the values are interpolated into that string, and a runtime
bind error the moment they are not.

| file | function | before | after |
|---|---|---|---|
| `db/operations.go:26` | `GetOperationID` | `qs, _, err := query.ToSQL()` → `db.ScanValContext(ctx, &result, qs)` | `query.Executor().ScanValContext(ctx, &result)` |
| `db/resourcetypes.go:37` | `GetResourceTypeID` | same shape | same fix |
| `db/users.go:27` | `GetUserID` | same shape | same fix |
| `db/updates.go:58` | `UserUpdates` | `qs, _, err := query.ToSQL()` → `d.db.QueryxContext(ctx, qs)` + a manual `rows.StructScan` loop | `query.Executor().ScanStructsContext(ctx, &results)` |

`Executor()` was used rather than the `SelectDataset.ScanVal*` convenience
methods deliberately: those add an implicit `Limit(1)`, which would have changed
the emitted SQL. `Executor()` preserves the statement shape exactly and only
changes how the values travel. This is the idiom the rest of the layer already
uses (`db/userplans.go:147`, `db/plans.go:133`).

Two notes on `UserUpdates`, the one that was not a mechanical substitution:

- It was reaching around goqu to raw sqlx (`d.db`), so it built its query
  against the possibly-transactional `GoquDatabase` from `querySettings(opts...)`
  and then ran it on the non-transactional connection. A `WithTX` option would
  have been silently ignored. Its only caller (`app/app.go:198`) passes no
  options, so no behavior changes today — a latent bug removed, not a fix.
- The nested-struct scan it did by hand is something goqu does natively, and
  provably so for this exact column list: its sibling `GetUserUpdate`
  (`db/updates.go:154` before the change) already scanned the identical
  `As(goqu.C("users.id"))`-style aliases through `ScanStructContext`.

That made `Database.db` unread, so the field and its comment ("Used when a
method needs direct access to sqlx for struct scanning") went with it. Every
query in the layer now runs through goqu.

## The exhaustive search for others

Run against the final tree. The premise is that the dangerous shape is *any*
SQL that reaches an executor as a pre-rendered string, so the search is wider
than `ToSQL`.

```
$ grep -rn "ToSQL()" --include="*.go" .
db/db.go:49:	sql, args, err := statement.ToSQL()
db/types.go:40:	ToSQL() (sql string, params []interface{}, err error)

$ grep -rn "ToSQL()" --include="*.go" . | grep ", _,"
(no output)

$ grep -rnE "\.(Exec|ExecContext|Query|QueryContext|QueryRow|QueryRowContext|ScanVal|ScanValContext|ScanVals|ScanValsContext|ScanStruct|ScanStructContext|ScanStructs|ScanStructsContext|Prepare|PrepareContext)\(" \
    --include="*.go" . | grep -vE "_test\.go|db/types\.go" | grep -E "\b(qs|sql|sqlStr|query|stmt|statement)\b\s*\)"
(no output)

$ grep -rnE "d\.db\.|testDB\.|\bconn\.(Exec|Query|Get|Select)" --include="*.go" . | grep -v "_test.go"
(no output)

$ grep -rn "Sprintf" --include="*.go" . | grep -viE "_test.go|error|msg|fail|unable|wrap"
main.go:104:	srv := fmt.Sprintf(":%s", strconv.Itoa(*listenPort))
```

Reading of the results:

- Only two `ToSQL()` references survive, and neither executes anything: the
  logging helper in `db/db.go`, which keeps the args, and the `SQLStatement`
  interface declaration in `db/types.go`.
- No executor call anywhere is handed a pre-rendered query-string variable. The
  `GoquDatabase` interface (`db/types.go:14-35`) still *declares* the
  string-taking methods, because it mirrors goqu's own `Database` surface, but
  nothing in the repo calls them that way any more.
- No raw sqlx execution outside tests. The remaining raw SQL in the repo is in
  `apitest` fixtures, which uses `database/sql` placeholders directly and never
  went through goqu.
- The only `fmt.Sprintf` outside error formatting builds a listen address, not
  SQL. (`apitest/harness_test.go:171` interpolates a table name into a `DELETE`,
  but from a hardcoded package-level list in test-only code.)

Separately, the converted `internal/qmsapi/db` layer has zero `ToSQL()` calls
and 43 executor calls, so it was immune to this class before the change and
still is.

## The latent bug prepared mode exposed: the wrong dialect name

Enabling prepared mode failed the suite almost completely — 60-odd tests, all
with `pq: syntax error at or near ")"`. Root cause, found by rendering a
representative dataset:

```
sql="SELECT \"id\", \"name\", \"unit\", \"consumable\" FROM \"resource_types\" WHERE (\"name\" = ?)" args=[cpu.hours]
```

The placeholder is `?`, not `$1`. `db.New` asked for `goqu.New("postgresql", ...)`
(`db/db.go:21`), but the dialect package registers itself under a different
name:

```
$ grep -rn "RegisterDialect" .../goqu/v9@v9.19.0/dialect/postgres/postgres.go
15:	goqu.RegisterDialect("postgres", DialectOptions())
```

goqu falls back to its default dialect for an unrecognized name, silently. So
**every goqu query this service has ever issued was rendered with goqu's default
dialect, not the PostgreSQL one.** That was invisible for as long as it was:
the default dialect's output happens to be valid PostgreSQL for the statements
this service builds (same `"` quoting, and `RETURNING` and `ON CONFLICT` are
supported), and its placeholder is only reached in prepared mode. Interpolation
was hiding it.

Fixed in commit 2 (`goqu.New("postgres", dbconn)`), because prepared mode cannot
be enabled without it. It is folded into that commit rather than split out for
that reason. The 69 goldens are unchanged across the dialect switch, which is
the evidence that the previously-wrong dialect was not also producing
differently-*behaving* SQL.

## Evidence that parameters are actually being bound

A green suite proves the change didn't break anything; it does not prove the
setting took effect. So this captures what PostgreSQL itself received, on the
server side, rather than what Go thinks it sent.

Method: a throwaway test stood up its own `postgres:17` container, ran the real
migrations, set `log_statement = 'all'` (`ALTER SYSTEM` + `pg_reload_conf()`),
then called one real function from each layer through the production
`db.New(...)` handle and dumped the container log between markers. The same
harness was run at commit 1 (prepared off) and at commit 2 (prepared on). It was
deleted afterwards; it is a measuring instrument, not a test.

The two functions: `db.Database.GetUserID` — one of the four converted call
sites, in the pre-existing `db/` layer — and `internal/qmsapi/db.UserExists`,
in the converted `/v1` layer.

**Before** (at commit 1, `SetDefaultPrepared` not called):

```
LOG:  statement: SELECT 'MARKER_LEGACY'
LOG:  statement: SELECT "id" FROM "users" WHERE ("username" = 'evidence@example.org')
LOG:  statement: SELECT 'MARKER_CONVERTED'
LOG:  statement: BEGIN READ WRITE
LOG:  statement: SELECT "id" FROM "users" WHERE ("username" = 'evidence@example.org')
LOG:  statement: COMMIT
LOG:  statement: SELECT 'MARKER_END'
```

**After** (at commit 2):

```
LOG:  statement: SELECT 'MARKER_LEGACY'
LOG:  execute <unnamed>: SELECT "id" FROM "users" WHERE ("username" = $1)
DETAIL:  Parameters: $1 = 'evidence@example.org'
LOG:  statement: SELECT 'MARKER_CONVERTED'
LOG:  statement: BEGIN READ WRITE
LOG:  execute <unnamed>: SELECT "id" FROM "users" WHERE ("username" = $1)
DETAIL:  Parameters: $1 = 'evidence@example.org'
LOG:  statement: COMMIT
LOG:  statement: SELECT 'MARKER_END'
```

Both layers move from `LOG: statement:` (simple query protocol, value inlined in
the SQL text) to `LOG: execute <unnamed>:` with a separate `DETAIL: Parameters:`
line (extended query protocol, value bound out-of-band). The value never appears
in the statement text after the change — which is the whole point, both for the
escaping dependency and for what lands in `pg_stat_statements` and the logs.

Go-side renders of the same shapes, for reference (`args` populated, placeholders
in the text):

```
SELECT: sql="SELECT \"id\", \"name\", \"unit\", \"consumable\" FROM \"resource_types\" WHERE (\"name\" = $1)"       args=[cpu.hours]
IN:     sql="SELECT \"id\", \"name\", \"unit\", \"consumable\" FROM \"resource_types\" WHERE (\"id\" IN ($1, $2))"  args=[a b]
INSERT: sql="INSERT INTO \"users\" (\"username\") VALUES ($1) RETURNING \"id\""                                     args=[bob]
```

## The 65535-parameter limit

PostgreSQL caps a single statement at 65535 bind parameters. Interpolation had
no equivalent cap, so this is a genuinely new constraint, and the honest answer
is: **no path reaches it in normal operation, and one path can reach it if a
caller asks it to.**

The exposure is `GET /v1/subscriptions` (the admin listing,
`ListSubscriptions`), for two compounding reasons.

**`limit` is unbounded.** `ValidateIntQueryParam(ctx, "limit", &limit, "gte=0")`
(`internal/qmsapi/controllers/subscriptions.go:288`) validates only `gte=0`;
there is no ceiling short of `int32`. The default is 50.

**The batch loaders do not deduplicate.** For a page of N subscriptions,
`loadSubscriptionDetailsBatch` (`subscriptions_goqu.go:314`) builds its ID
slices one entry per row, not one per distinct value, and each becomes an `IN`
list of that length:

| loader | parameters | note |
|---|---|---|
| `usersByID` | N | one per subscription |
| `plansByID` | N | N copies of a handful of plan IDs in practice |
| `planRatesByID` | ≤ N | |
| `quotasBySubscriptionID` | N | |
| `usagesBySubscriptionID` | N | |
| `resourceTypesByID` (via quotas) | one per quota **row** | ~2N with two resource types per subscription |
| `resourceTypesByID` (via usages) | one per usage **row** | ~2N |

The first statement to hit the ceiling is therefore one of the
`resourceTypesByID` calls, at roughly N ≈ 32,768 subscriptions returned in a
single page; `usersByID` and `plansByID` follow at N = 65,536.

Concretely:

- At the default `limit=50`: about 100 parameters at the worst statement. Three
  orders of magnitude of headroom.
- To fail, a caller must both pass an explicit `limit` in the tens of thousands
  *and* the database must hold that many active subscriptions. The failure is a
  clean error from `lib/pq` surfacing as a 500, not corruption or a wrong
  answer.
- `ListSubscriptionsForUser` takes no limit at all, but is scoped to one user; a
  single user with 65,535 subscriptions is not a real scenario.

Multi-row inserts are not a concern. `SavePlanQuotaDefaults` binds 4 parameters
per row (`plan_goqu.go:410-415`) and `SavePlanRates` binds 3
(`plan_goqu.go:453-457`), so a single request would need roughly 16,000 quota
defaults or 21,800 rates in one plan body to reach the cap. Both are
request-body-driven, so a client could construct one, but neither is a shape any
real caller produces.

Recommendation, not done here (it is a behavior-neutral optimization, and this
branch's scope is the interpolation fix): deduplicate the ID slices before
building the `IN` lists. It raises the ceiling by whatever the duplication factor
is — large, since `plansByID` currently sends N copies of a handful of plan IDs
— and shrinks the statements on every request, not just the pathological ones.
Bounding `limit` with a `lte=` check is the other half, and is the one that
turns a 500 into a 400.

## Gate results

Run before any change, after commit 1, and after commit 2.

| | `go test ./... -count=1` | `git diff goqu-baseline -- apitest/testdata/` | `golangci-lint run` |
|---|---|---|---|
| baseline (`d404ceb`) | pass | empty | `0 issues.` |
| after commit 1 (`555c235`) | pass | empty | `0 issues.` |
| after commit 2 (HEAD) | pass | empty | `0 issues.` |

All 69 goldens are byte-identical to the pre-conversion baseline. Test packages
reporting `ok`: `apitest`, `app`, `errors`, `internal/qmsapi/model`.
`golangci-lint` 2.10.1, the version CI pins.

Commit 1 was verified green on its own before commit 2 was written, which is
what makes the mid-work failure attributable: when the suite broke, it broke on
commit 2's change alone, and the cause was the dialect name rather than anything
in commit 1.

## Files changed

```
db/db.go                                       dialect name, logStatement helper, Database.db removed
db/operations.go                               GetOperationID → Executor()
db/resourcetypes.go                            GetResourceTypeID → Executor()
db/users.go                                    GetUserID → Executor()
db/updates.go                                  UserUpdates → Executor(), manual scan loop removed
db/quotas.go                                   log.Info(ToSQL()) → logStatement
db/usages.go                                   log.Info(ToSQL()) → logStatement
db/tables/tables.go                            SetDefaultPrepared(true) in init()
docs/superpowers/findings-goqu-unification.md  interpolation entry marked resolved
docs/superpowers/prepared-mode-report.md       this file
```

No route paths, methods, request shapes, or response shapes changed. Nothing
under `apitest/testdata/` was touched.

## Self-review

- **The dialect fix is inside commit 2 rather than its own commit.** It is an
  independently valuable bug fix and a reviewer might reasonably want it
  isolated. It is folded in because prepared mode does not function without it —
  a commit enabling prepared mode without it would fail the gate — and because
  the brief asked for two commits. The commit message leads with it.
- **`init()` for a global setting is not free.** It is invisible at the call
  sites it affects, and `db/tables` is a slightly odd home for a query-generation
  mode. The alternatives all had a worse failure mode (silently missing the test
  binary), which is the specific failure the brief called out. The tradeoff is
  documented in the code comment, not just here.
- **The evidence harness was deleted.** It needed `ALTER SYSTEM` and a container
  log read, and it is slow and Docker-dependent for something that is not
  asserting anything. The captured output above is the artifact. If someone wants
  to re-run it, the method is described in full in the evidence section.
- **`Executor()` over the `ScanVal*` convenience methods** costs a little
  verbosity to avoid an implicit `LIMIT 1` that would have altered the SQL. Worth
  it here, where the whole exercise is "the responses must not change".
- **The parameter-limit exposure is real but not closed.** A caller passing
  `?limit=40000` against a large database would now get a 500 where it
  previously got a very slow query. Judged out of scope to fix on this branch,
  and recorded above and in the findings doc rather than left implicit.
- **Not verified: behavior under `standard_conforming_strings = off`.** The
  change removes the dependency on that setting by construction — no value is
  interpolated any more, so goqu's escaping is never consulted — but no test was
  written that runs the suite against a server with the setting off. That would
  be a genuinely stronger proof than the parameter-binding evidence above, and it
  is cheap to add later if wanted.
