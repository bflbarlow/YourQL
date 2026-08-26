> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# CRITICAL ISSUE — Read-Only Invariant Gap on Final Queries

## Severity

Critical. Violates the Data Source Read-Only Invariant in
`AGENT_READ_FIRST.md` §0.

## Summary

`executeSQLWithMode` only checks that a query *starts with* `SELECT`,
`WITH`, or a parenthesized `SELECT`. It does not block DML/DDL or
write-adjacent patterns. The thorough check — `validateExplorationQuery` —
is only invoked for **exploration** queries (`is_exploration: true`).

The **final** surfaced query (`is_exploration: false`) bypasses
`validateExplorationQuery` entirely and goes straight to
`executeSQLWithMode`.

## Facts

### 1. Final queries skip the DML/DDL validation

- `agentic_loop.go` L1282 calls `validateExplorationQuery` only inside
  `if args.IsExploration { ... }`.
- `agentic_loop.go` L1294 then calls `executeSQLWithMode(...)` for **all**
  queries, including finals, without any additional read-only check.
- `executeSQLWithMode` (L190) only checks the statement **prefix**:

  ```go
  isSelect := strings.HasPrefix(upper, "SELECT")
  // ...
  if !isSelect && !isCTE {
      return nil, fmt.Errorf("only SELECT queries are allowed")
  }
  ```

### 2. The dangerous-pattern check exists but is never applied to finals

- `validateExplorationQuery` (L739) blocks:
  `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `TRUNCATE`, `CREATE`,
  `REPLACE`, `GRANT`, `REVOKE`, `LOAD_FILE`, `INTO OUTFILE`,
  `INTO DUMPFILE`, `BENCHMARK(`, `SLEEP(`, `EXEC`, `EXECUTE`, `xp_`, `sp_`.
- This check is **only** reachable via the exploration path.

### 3. Write operations that pass the prefix check

Because finals are only prefix-checked, the following reach the data source:

| Database | Query | Effect |
|---|---|---|
| SQL Server | `SELECT * INTO newtable FROM src` | Creates a table (write) |
| MySQL | `SELECT col INTO OUTFILE '/path'` | Writes a server file |
| MySQL | `SELECT col INTO DUMPFILE '/path'` | Writes a server file |
| Postgres / Snowflake | `WITH cte AS (...) INSERT INTO t SELECT ...` | Data-modifying CTE (write) |
| Postgres / Snowflake | `WITH cte AS (...) UPDATE t SET ...` | Data-modifying CTE (write) |
| Postgres / Snowflake | `WITH cte AS (...) DELETE FROM t ...` | Data-modifying CTE (write) |
| Many | `SELECT ... FOR UPDATE` | Row locks with side effects |

## Fix (verified against current `dangerousPatterns` list)

**Decision: call `validateExplorationQuery(sql, ExplorationRelaxed)` from
`executeSQLWithMode`, for both exploration and final queries, as a single
choke point.** This closes the gap in one place regardless of caller.
Relaxed mode is used so complexity limits (joins/subqueries/unions), which
are exploration-only per §1.4, are never applied to finals.

Before doing this, verify the existing `dangerousPatterns` list actually
covers every row in the table above — **it does not, as written today**:

| Row | Covered by current `dangerousPatterns`? |
|---|---|
| `WITH ... INSERT INTO ...` | Yes — matches `"INSERT "` |
| `WITH ... UPDATE ...` | Yes — matches `"UPDATE "` |
| `WITH ... DELETE ...` | Yes — matches `"DELETE "` |
| `SELECT ... INTO OUTFILE` | Yes — explicit `"INTO OUTFILE"` |
| `SELECT ... INTO DUMPFILE` | Yes — explicit `"INTO DUMPFILE"` |
| `SELECT * INTO newtable FROM src` (SQL Server) | **No** — no bare `INTO <table>` pattern |
| `SELECT ... FOR UPDATE` | **No** — no `"FOR UPDATE"` pattern |

**Required additions to `dangerousPatterns` in `sql_execution.go` (L757-762)**
before this fix is complete:

1. `"FOR UPDATE"` — add to the list directly.
2. `"MERGE "` — named as forbidden in `AGENT_READ_FIRST.md` §0/§4.0 but
   currently absent from the list entirely (pre-existing gap, independent of
   the exploration/final split).
3. Bare `SELECT ... INTO <table>` (SQL Server table creation) — cannot be a
   simple substring match, since `INTO` also appears legitimately in
   `INSERT INTO` (already blocked) and in `SELECT ... INTO @variable`
   (a harmless SQL Server variable assignment, not a write). Needs a
   dedicated check: flag `INTO` only when (a) the statement is not already
   blocked by another pattern, and (b) the token after `INTO` is not `@...`
   and not `OUTFILE`/`DUMPFILE` (already separately matched).

**Error message clarity:** the two `validateExplorationQuery` error strings
(`"exploration query blocked: contains '%s'"` and `"exploration queries must
be SELECT statements"`) are exploration-specific wording. If this function
is now the shared choke point for finals too, reword both to be caller-
agnostic (e.g. `"query blocked: contains '%s'"` / `"only SELECT statements
are allowed"`) so a rejected final query doesn't surface a confusing
"exploration" label to the model or, if ever shown raw, to the user.

**Acceptance criteria (must all pass before closing this issue):**

- [ ] `dangerousPatterns` includes `FOR UPDATE` and `MERGE `.
- [ ] A dedicated check rejects bare `SELECT ... INTO <table>` while still
      allowing `SELECT ... INTO @var` (SQL Server) and not double-flagging
      `INSERT INTO`, `INTO OUTFILE`, `INTO DUMPFILE`.
- [ ] `executeSQLWithMode` calls the shared validation for every query,
      exploration and final alike, before `sql.Open`/`db.Query` is reached.
- [ ] Error strings no longer say "exploration" when used for a final query.
- [ ] Manual/unit test matrix covers all 7 rows in the Facts table above,
      confirming each is rejected before reaching the driver, for at least
      one of MySQL/Postgres/SQL Server (per the dialect each row applies to).
- [ ] Existing legitimate exploration queries (SELECT with JOIN/subquery/
      UNION/GROUP BY/ORDER BY under relaxed mode, and simple SELECTs under
      strict/moderate) still pass.

**Note:** no `*_test.go` file currently exists for `sql_execution.go` or
`validateExplorationQuery` (checked via `pkg/services/*_test.go` — none
found). The acceptance criteria above assume new tests will be written as
part of this fix, not that a regression suite already exists to run against.
