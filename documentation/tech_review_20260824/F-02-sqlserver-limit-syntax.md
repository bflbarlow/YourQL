# F-2 · Auto-LIMIT appends invalid SQL for SQL Server

**Severity:** High · **Effort:** Medium · **Risk categories:** answer accuracy, answer delivery

## Problem Statement

`applyDefaultLimit` (`pkg/services/sql_execution.go`) rewrites user/LLM
queries that exceed `queryLengthThreshold` (200 chars) by appending a
row limit. Two defects:

### Defect 1 — LIMIT is not T-SQL

```go
return cleaned + " LIMIT " + fmt.Sprintf("%d", limitValue)
```

and the UNION wrapper:

```go
return "SELECT * FROM (" + cleaned + ") subq LIMIT " + fmt.Sprintf("%d", limitValue)
```

T-SQL supports neither trailing `LIMIT n` nor aliasing a derived table with
a bare identifier followed by `LIMIT`. SQL Server has no `LIMIT` clause at
all; it uses `SELECT TOP (n) …` or `OFFSET … FETCH`. Consequence: any SQL
Server query longer than 200 characters without an existing limit — which
describes most real LLM-generated queries — is rewritten into a guaranteed
syntax error. The error goes back to the LLM for retry (charter's error
retry path), the model likely retries similarly long, gets the same
rewrite, and the loop burns its `maxErrorRetries` before failing the user's
question. This is a *systematic* failed-answer machine for one of the eleven
supported drivers.

The same applies to the CTE path (`cleaned[:i+1] + " LIMIT …"`) — invalid in
T-SQL regardless of position.

### Defect 2 — LIMIT detection matches literals and identifiers

```go
if regexp.MustCompile(`(?i)\bLIMIT\b`).MatchString(upper) {
    return sqlQuery   // assume a limit already exists
}
```

`\bLIMIT\b` matches inside string literals (`WHERE note = 'keep limit high'`)
and column/table names (`limit_status`, `rate_limit_hits`). Such queries are
incorrectly assumed bounded and skip limiting entirely — a silent row-limit
bypass, i.e., the safety knob quietly does nothing.

## Solution Design

Make rewriting driver-aware and literal-aware.

### Step 1 — Literal masking for detection

Reuse the engine's existing pure helper:

```go
masked := strings.ToUpper(engine.MaskStringLiterals(engine.StripSQLComments(strings.TrimSpace(sqlQuery))))
if regexp.MustCompile(`\bLIMIT\b|\bFETCH\b|\bTOP\s*\(`).MatchString(masked) {
    return sqlQuery
}
```

Masking after comment stripping matches the documented contract of
`MaskStringLiterals` ("call only after comment stripping"). Adding
`TOP (` / `FETCH` to the pattern prevents double-limiting SQL Server queries
that already bound themselves.

### Step 2 — Driver-aware limit application

Add a small capability enum rather than sprinkling `conn.Type ==`
conditionals:

```go
type limitSyntax int
const (
    limitSuffix limitSyntax = iota // MySQL, MariaDB, PostgreSQL, SQLite, Redshift
    limitTop                        // SQL Server
)

func limitSyntaxFor(connType string) limitSyntax {
    if connType == "sqlserver" { return limitTop }
    return limitSuffix
}
```

**SQL Server rewrite strategies:**

- *Plain SELECT:* prefix-wrap preserves ORDER BY semantics safely:
  `SELECT TOP (n) * FROM (<query>) subq` — valid T-SQL; unlike naive `TOP`
  injection it does not need to find the SELECT keyword position.
- *UNION / CTE / parenthesized:* identical wrap works:
  `SELECT TOP (n) * FROM (<cleaned>) subq`.

So SQL Server collapses to one form:

```go
case limitTop:
    return fmt.Sprintf("SELECT TOP (%d) * FROM (%s) subq", limitValue, cleaned)
```

Note T-SQL requires the derived-table alias (`subq`) — already present.
Also verify nested `ORDER BY` legality: SQL Server forbids `ORDER BY`
inside derived tables *unless* `TOP`/`OFFSET` is present in the subquery.
Since the inner query here is arbitrary, an inner `ORDER BY` without TOP
would error. Mitigation: if the masked query contains `\bORDER BY\b` for the
sqlserver branch, strip a single trailing top-level `ORDER BY …` clause
before wrapping (the outer `TOP (n)` without outer ORDER BY gives
unspecified-but-bounded ordering — acceptable for a safety row cap, and we
must not silently change result semantics). Document this behavior in the
tool/prompt notes so the LLM knows final ordering may not be guaranteed on
SQL Server when limits are auto-applied.

**Native queriers (BigQuery, Sheets):** BigQuery supports trailing `LIMIT`;
Sheets path builds its own query text. Verify both keep working via the
existing dispatch order (syntax chosen by `conn.Type` covers them through
`limitSuffix` default). Add explicit cases only if testing shows otherwise.

### Step 3 — Keep validation downstream

`executeSQLWithMode` validates *after* rewriting (rewritten SQL is what
executes). Preserve that ordering exactly — F-8/F-4 tests depend on it.

## Implementation Plan

1. Add `limitSyntaxFor` + masked-detection rewrite in
   `applyDefaultLimit`; delete the unreachable second
   `if threshold == 0 { return sqlQuery }` (see F-19).
2. Golden test table (goes into F-4 Tier-1 suite):
   | Input | Driver | Expected |
   |---|---|---|
   | long plain SELECT | mysql/pg/sqlite/mariadb/redshift | suffix ` LIMIT n` |
   | long plain SELECT | sqlserver | `SELECT TOP (n) * FROM (…) subq` |
   | long UNION | sqlserver | wrapped TOP |
   | long CTE | sqlserver | wrapped TOP |
   | `… WHERE note='no limit here'` (long) | mysql | limit appended (masking fixed bypass) |
   | short query | any | unchanged |
   | existing `LIMIT 5` | any | unchanged |
3. Manual verification per charter §5.3 against real MySQL + Postgres +
   SQLite + one NativeQuerier; SQL Server ideally via a free-tier Azure SQL
   or docker `mcr.microsoft.com/mssql/server`.
4. Update `documentation/AGENT_LOOP_DETAILS.md` if it describes limit
   injection syntax per driver.

## Risk Assessment

- **Failure mode:** wrong T-SQL emitted → same as today's bug; the golden
  table plus a live smoke test mitigates. Wrap-based approach avoids fragile
  keyword-position surgery, keeping blast radius small.
- **Behavioral change:** SQL Server users' previously-failing queries now
  succeed — strictly positive; previously-succeeding short queries untouched.
- **Ordering-by-limit caveat** documented above; flag in RISK_ANALYSIS_LOG.md
  per charter §4.4 since it touches prompt-visible behavior.
