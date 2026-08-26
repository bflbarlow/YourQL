# F-1 · SQL executed without context — cancellation cannot stop a running query

**Severity:** High · **Effort:** Small · **Risk categories:** answer delivery, reliability, user trust

## Problem Statement

YourQL exposes cancellation to users in three places — the Wails UI cancel
button, the headless `POST /api/conversations/{id}/cancel` endpoint, and the
pipeline-level timeout (`context.WithTimeout(ctx,
GetTimeoutSetting("pipeline_timeout_seconds", 180))` in
`pkg/services/discussion_engine.go`). All three operate on Go contexts.
However, the context never reaches the database layer:

```go
// pkg/services/sql_execution.go, executeSQLWithMode (~line 229)
rows, err := db.Query(sqlQuery)   // <-- ctx is not in scope here
```

The function signature chain is context-free:

```go
func executeSQL(conn *models.DataSource, sqlQuery string) (*QueryResult, error)
func ExecuteSQLWithMode(conn *models.DataSource, sqlQuery string, isExploration bool) (*engine.QueryResult, error)
```

`pkg/engine.QueryExecutor.ExecuteQuery(ctx, ...)` *does* receive a context —
the adapter drops it on the floor when it calls into services.

## Consequences

1. **Cancelled work keeps running and billing.** A Snowflake or BigQuery
   scan cancelled by the user continues consuming warehouse credits until it
   finishes server-side.
2. **Pipeline hangs past its deadline.** The 180s pipeline timeout fires at
   the loop level between LLM rounds, but a single slow query inside round N
   blocks the goroutine indefinitely; the deadline is only observed after
   the driver returns.
3. **Cancel endpoint appears broken.** `headless_handlers.go` cancels the
   context, but nothing downstream observes it during DB execution, so the
   request only completes when the database does.
4. **Introspection has the same gap** — schema-gathering queries in
   `db_mysql.go`, `db_postgres.go`, etc., all use bare `db.Query` /
   `db.QueryRow`.

## Solution Design

Thread `context.Context` through the entire execution path. The engine
interface already carries it; only services and drivers need changing.

### Target signatures

```go
// pkg/services/sql_execution.go
func executeSQLWithMode(ctx context.Context, conn *models.DataSource, sqlQuery string, isExploration bool) (*engine.QueryResult, error)
func ExecuteSQLWithMode(ctx context.Context, conn *models.DataSource, sqlQuery string, isExploration bool) (*engine.QueryResult, error)

// pkg/services/db_driver.go
type NativeQuerier interface {
    QueryRowsNative(ctx context.Context, conn *models.DataSource, sqlQuery string) (columns []string, rows [][]interface{}, err error)
}
```

### Core execution changes

```go
rows, err := db.QueryContext(ctx, sqlQuery)
...
if err := ctx.Err(); err != nil {           // check before scanning results
    return nil, fmt.Errorf("query cancelled: %w", err)
}
for rows.Next() { ... }                      // Scan honors ctx via rows.Close? No —
if err := rows.Err(); err != nil {           // rows.Err() surfaces ctx cancellation mid-iteration
    if ctx.Err() != nil {
        return nil, fmt.Errorf("query cancelled: %w", ctx.Err())
    }
    return nil, fmt.Errorf("error iterating rows: %w", err)
}
```

Note: `database/sql` cancels a `QueryContext` mid-iteration by closing the
rows when ctx is done; `rows.Err()` then returns the context error. Wrap it
so the user sees "query cancelled" rather than a raw driver string
(charter §3.9).

Also add a **per-query timeout** distinct from the pipeline timeout:

```go
qctx, cancel := context.WithTimeout(ctx, time.Duration(queryTimeoutSeconds)*time.Second)
defer cancel()
```

with `queryTimeoutSeconds` defaulting to, say, 120 and configurable per data
source via `ParseConfig()` (additive column, charter §3.2-compliant). This
prevents one pathological query from eating the whole pipeline budget.

### Introspection changes

Each driver's introspection functions gain a `ctx` parameter;
`database_introspection.go` passes the pipeline ctx with its own shorter
timeout (e.g., 30s) so a hung metadata query fails fast with a clear error.

### Callers to update (mechanical)

- `pkg/services/discussion_engine.go` — already holds ctx; pass down.
- `pkg/services/engine_adapters.go` — `QueryExecutorAdapter.ExecuteQuery`
  currently discards its ctx; forward it.
- `app.go` bound methods that call `executeSQL` directly (e.g., manual query
  testing endpoints) — construct `context.Background()` with a timeout where
  no request ctx exists, or use Wails' ctx stored on startup.
- `headless_handlers.go` / `headless.go` — request contexts are available.
- `export.go`, `total_export.go`, any other `executeSQL` callers — grep for
  `ExecuteSQLWithMode(` and `executeSQL(`.

## Implementation Plan

1. Add `ctx` parameter to `NativeQuerier` interface; fix BigQuery + Google
   Sheets implementations (both support ctx natively in their client libs).
2. Change `executeSQLWithMode` / `ExecuteSQLWithMode`; compiler-driven sweep
   of callers.
3. Update adapters to forward ctx instead of dropping it.
4. Update introspection signatures; add 30s introspection timeout.
5. Add per-data-source optional `query_timeout_seconds` config (additive).
6. Run full multi-driver verification per charter §5.3.

## Risk Assessment

- **Failure mode:** a driver ignores ctx (some CGo drivers cancel lazily).
  Mitigation: behavior degrades to today's semantics; no regression.
- **Regression risk:** low — signature change is compile-time enforced.
  The only semantic change is queries now aborting when they should.
- **Charter alignment:** improves reliability (#3) and user control without
  touching safety (#2) or correctness (#1). Proceed per §4.3 ("clear bug in
  answer delivery").

## Tests Required

- Engine mock test: executor receives non-nil ctx; cancelling ctx before
  call returns promptly (mock honors it).
- Services test with SQLite file data source: start a long query
  (`WITH RECURSIVE cte(x) AS (...) SELECT x FROM cte` counting to ~10^8),
  cancel ctx after 50ms, assert return within ~200ms with cancellation
  error.
- Assert `ValidateReadOnlySQL` still runs *before* any connection attempt
  (ordering test).
