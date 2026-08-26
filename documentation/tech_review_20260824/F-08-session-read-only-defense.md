# F-8 · Session-level read-only enforcement as defense-in-depth

**Severity:** High · **Effort:** Medium · **Risk categories:** data safety (core invariant), user trust

## Problem Statement

The Data Source Read-Only Invariant (charter §0 — absolute, no trade-off) is
currently enforced by exactly one mechanism: `engine.ValidateReadOnlySQL`,
a lexical validator, applied at the single choke point
(`executeSQLWithMode`). The validator is well-engineered — single-statement
state machine, comment stripping, string-literal masking, `SELECT … INTO`
detection, biased toward blocking — but it is fundamentally *pattern-based*.
The charter itself acknowledges the complementary principle: the app "must
not rely solely on the *user's* database role/permissions as the only
safeguard." Symmetrically, it must not rely solely on the pattern matcher.

Residual bypass classes for any regex/lexer approach, however careful:

- Dialect-specific write-ish constructs not yet enumerated (e.g.,
  PostgreSQL `SELECT func_that_writes()`, Snowflake `SELECT … FROM TABLE(FLATTEN(…))`
  side-effect-free but UDF calls in general, SQL Server `OPENROWSET`/
  `OPENQUERY` passthrough, MySQL user-defined functions).
- Multi-statement or comment tricks surviving future refactors of the
  stripper (regression risk lives precisely where the tests are thin — F-4).
- Future code paths that construct queries and skip the choke point by
  accident.

The definitive fix is to make the **database engine itself refuse writes**,
so any validator miss converts from "potential write" into a guaranteed,
harmless driver error.

## Solution Design

Per-driver session hardening, applied immediately after connection creation
in `executeSQLWithMode`, before executing the query. All mechanisms are
session-scoped or transaction-scoped: they never modify the user's server
configuration (which would itself violate §0).

| Driver | Mechanism | Notes |
|---|---|---|
| PostgreSQL | `db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})` and run the query inside; commit/rollback after scan | Enforced server-side since PG 9.0? (13+ for full coverage of temp writes). Cleanest option. |
| Redshift | Same as PostgreSQL (PG-derived); `TxOptions{ReadOnly}` supported via pgx | Verify with live Redshift; fall back to `SET default_transaction_read_only = on` session statement if needed. |
| MySQL / MariaDB | `SET SESSION TRANSACTION READ ONLY` after open | Session-scoped, expires at close. Note: applies to the *next* transaction; with autocommit=1 statements are each implicit transactions — also send `SET SESSION TRANSACTION READ ONLY` works for autocommit DML in MySQL 5.7+/8. Verify per version. |
| SQL Server | Open an explicit transaction with `BEGIN TRANSACTION` + execute under a login-level guard is unavailable → use `SET TRANSACTION ISOLATION LEVEL READ COMMITTED;` plus rely on wrapping query in `BEGIN TRAN … ROLLBACK` | Pragmatic pattern: run query then unconditional `ROLLBACK`; combined with the validator this makes even a hypothetical write non-persistent. |
| SQLite (file data source) | Open DSN with `?mode=ro` (`modernc.org/sqlite` supports `file:path?mode=ro`) | Write attempts fail at the VFS layer. Trivial and complete. |
| Snowflake | Execute within `ALTER SESSION SET AUTOCOMMIT = FALSE` + explicit read-only transaction if supported; otherwise document reliance on validator + scope-limited credentials | Snowflake has no session read-only switch; keep validator as primary, add guidance text. |
| BigQuery | Client already uses read-only scope (F-9 asserts); additionally set `QueryConfig{…}` default dataset only; jobs API with `useQueryCache` irrelevant | Scope enforcement is the real control here. |
| Google Sheets | Read-only OAuth scope (F-9) | API cannot mutate with readonly scope — strongest possible guarantee already available. |

### Implementation shape

```go
// pkg/services/sql_execution.go
func hardenSession(ctx context.Context, db *sql.DB, connType string) (rollback func(), err error)
```

- For tx-based drivers returns `(commit/rollback closure, nil)`; the query
  runs via `tx.QueryContext`.
- For statement-based (MySQL) runs the SET and returns a no-op cleanup.
- Fail closed: if the hardening statement itself errors, abort the request
  ("could not establish read-only session") rather than proceeding
  unguarded. Log at debug level with driver detail behind tech-details.

Interaction with F-1: hardening composes naturally once ctx flows through
(`BeginTx(ctx, ...)` needs it). Land F-1 first or together.

## Implementation Plan

1. SQLite `mode=ro` first (30 minutes, closes a whole class for file data
   sources).
2. Postgres/Redshift `TxOptions{ReadOnly}`.
3. MySQL/MariaDB session statement; verify against MySQL 5.7, 8.x, MariaDB
   10.x in docker.
4. SQL Server rollback-wrapped execution.
5. Snowflake/BigQuery/Sheets: verify scopes (F-9), document residual
   reliance on validator in AGENT_READ_FIRST.md §1.4.
6. Tests (F-4 suite): for each docker-able driver, attempt
   `INSERT INTO …` through `executeSQLWithMode` twice — assert (a) validator
   blocks it, then (b) delete/comment the validator call in a test-only fork
   (or test the harden helper directly) and assert the *driver* rejects it.
   This proves both layers independently.
7. Update charter §1.4 bullet list to name the new second layer (same-commit
   doc rule).

## Risk Assessment

- **Failure mode A:** read-only transaction breaks legitimate CTE reads on
  some engine → caught in multi-DB verification (§5.3); gating is per-driver
  so blast radius is one driver, reverted by flipping its case off.
- **Failure mode B:** hardening adds one round-trip latency per query.
  Mitigation: SET statements are sub-millisecond relative to LLM round-trips;
  acceptable.
- **Failure mode C:** fail-closed policy turns transient DB errors into
  failed answers. Mitigation: distinguish "hardening refused" from
  "connection error"; only hardening-specific errors abort.
- Charter alignment: §4.3 case 2 verbatim — "improves safety without
  reducing capability." Requires RISK_ANALYSIS_LOG.md entry per §4.2
  (touches sql_execution.go).
