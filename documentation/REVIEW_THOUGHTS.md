# YourQL — Review Thoughts

**Date:** 2026-08-19  
**Reviewer:** Agent review of the YourQL codebase  
**Scope:** Full architecture, code quality, security, frontend, testing, and specific code-level findings

---

## Executive Summary

YourQL is a well-architected, mature desktop application for querying databases via natural language. Built with Wails v2 (Go backend + Svelte 5 frontend), it features a sophisticated agentic tool-calling loop. The codebase demonstrates strong engineering discipline — particularly around safety, isolation of the core loop, and documentation. This is a high-quality project.

---

## Architecture Assessment

### Strengths

**1. Excellent Architecture Siloing (`pkg/engine/`)**

The agentic loop is properly isolated as a black box (`pkg/engine/loop.go`) with zero dependencies on `pkg/models`, `pkg/services`, `database/sql`, or the filesystem. The 8 interface contracts in `ports.go` cleanly separate concerns. The adapter pattern in `engine_adapters.go` correctly limits conversion logic to shims. This makes the loop independently testable — and `loop_test.go` proves it with real mock-based tests.

**2. Multi-Layered Safety (The Read-Only Invariant)**

The read-only guarantee is enforced at multiple levels:

- `ValidateReadOnlySQL` in `safety.go` — blocks DML/DDL patterns before any driver touch
- `ValidateExplorationQuery` — complexity-mode gate for exploration queries
- `QueryExecutorAdapter.Execute` — defense-in-depth layer
- Native querier paths also go through validation
- `sanitizeSQLError` strips credentials from driver error messages

The "Data Source Read-Only Invariant" in AGENT_READ_FIRST.md §0 is the right north star, and the code enforces it.

**3. Multi-Provider LLM Support**

Clean abstraction over OpenAI, Anthropic, Ollama, and custom endpoints via the `LLMClient` interface. Both streaming and non-streaming paths are implemented. The `effectiveMaxTokens` function correctly clamps to context window while respecting user config.

**4. Comprehensive Driver Support**

11 drivers registered (MySQL, PostgreSQL, SQLite, SQL Server, Snowflake, BigQuery, Redshift, MariaDB, plus CSV/Excel via file path). Each implements the `DBDriver` interface with DSN building, schema introspection, and dialect hints.

**5. Documentation Quality**

AGENT_READ_FIRST.md is exceptional — it reads like a project charter, not just docs. The risk/reward framework, priority order, and "absolute rules" make it genuinely useful. The documentation directory is comprehensive (enhancements, architecture, issues, operations, testing).

**6. Auto-Updater Safety**

`updater.go` correctly verifies SHA256 checksums before installing. The `appVersion` defaults to "dev" to prevent dev builds from self-updating. The OS-specific scripts (`updater_darwin.go`, etc.) handle code-signature preservation properly.

**7. Svelte 5 Reactivity**

Clean use of `$state`, `$effect`, and `$props`. The streaming event handler (`llm:stream`) properly maps SSE events to UI state changes.

---

## Code Quality Observations

### `app.go` — God-Object (~800+ lines)

TECH_REVIEW.md §1.1 correctly identifies this. Every public method on `App` is a Wails binding, and the file contains:

- 40+ delegate methods
- Type definitions (`LLMProviderSetting`, `DataSourceSetting`, `SchemaPreview`, etc.) that belong in domain packages
- `QueryResult` duplicated between `app.go` and `pkg/services/sql_execution.go`

*Recommendation:* Split into domain-specific binding files (conversations, datasources, llm_providers, skills, google_sheets, general) as suggested in TECH_REVIEW §1.1.

### `discussion_engine.go` — Too Many Responsibilities (~1,200 lines)

TECH_REVIEW §1.2 correctly identifies too many responsibilities packed in: prompt construction, LLM response parsing, exploration loop, query execution, HTML rendering, chart resolution, CSV export, summarization. The agentic loop itself has been extracted to `pkg/engine/loop.go`, but `discussion_engine.go` still does too much orchestration work.

*Recommendation:* Split after the DI refactor (TECH_REVIEW §1.3/1.4) to avoid merge conflicts.

### `agentic_loop.go` — Too Large (~1,600+ lines)

This file handles tool definitions, prompt construction, system messages, and `buildToolLlmMessages`. It's the largest file in the services layer. The tool definitions and prompt construction logic are tightly coupled.

### `formatUserError` and `classifyErrorCategory` — String Matching Fragility

Both functions in `discussion_engine.go` use `strings.Contains(lower, ...)` pattern matching. This is fragile — it works today but will miss edge cases and produce false positives (e.g., a column named "timeout" in a user table).

*Recommendation:* Consider a more structured error classification approach, perhaps using regex patterns or error-type enums.

### `sqlQueryHash` — Collision Risk

The simple hash in `sql_query_hash` can produce negative values and collisions, leading to duplicate HTML element IDs. The frontend uses these for `onclick` handlers and `id` attributes.

*Recommendation:* Use a proper hash (e.g., `fmt.Sprintf("sql-%x", hash)`) or UUID-based IDs.

### `ensureColumn` in Migrations — Silent Failures

The `dropColumnIfExists` migration silently skips if SQLite is too old. While this is safe (no data loss), it means users on old SQLite versions may have stale columns they don't know about.

### `QueryResult` Duplication

`app.go` defines its own `QueryResult` struct for the `ExecuteQuery` Wails binding, while `pkg/engine/types.go` has the canonical version. These are structurally identical but technically different types. The adapter layer should use the canonical one everywhere.

### Global `models.DB` State

The global `*sql.DB` in `models.DB` is a known anti-pattern. TECH_REVIEW §1.3/1.4 recommends introducing service interfaces and dependency-injecting it. This is a large refactor (~50 call sites) but would improve testability.

---

## Security Assessment

### Good Practices

- **Read-only invariant** is the architectural foundation, not an afterthought
- **Credential sanitization** in `sanitizeSQLError` strips DSN passwords from error messages
- **No telemetry/analytic/phoning home** — confirmed
- **SHA256 verification** before update installation
- **Dev-build guard** (`appVersion = "dev"`) prevents dev builds from self-updating
- **API keys never serialized to the frontend** (stored in `llm_providers.api_key` column, never exposed to bindings)
- **Streaming debug capture** is gated behind `YOURQL_DEBUG_STREAMS=1` env var

### Security Considerations

1. **Plaintext credentials at rest** — TECH_REVIEW §2.1 lists encrypting via OS keychain as a deferred item. This is a real gap for users on shared machines.
2. **SQL injection via `fmt.Sprintf` in migrations** — `ensureColumn` uses `fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", ...)` but is guarded by `safeSQLIdentifier`. This is correct today but fragile if new callers appear.
3. **`os.OpenFile` with user-controllable paths** in `ExportLog` and `ExportDatabase` — the file path comes from a native dialog, so this is safe, but worth noting.

---

## Frontend Assessment

### Good

- **Svelte 5 runes** used correctly (`$state`, `$effect`, `$props`)
- **Streaming state management** is clean — maps SSE events to local state
- **CSS variables** in `variables.css` support light/dark themes
- **VizChart.svelte** handles Chart.js integration properly

### Frontend Issues

1. **`{@html}` rendering** — TECH_REVIEW §2.3 lists this as a deferred item. Using `{@html}` for rendering assistant content is a potential XSS vector if the content source is ever compromised. The recommendation to replace with structured data rendering is valid.
2. **Inline `onclick` in generated HTML** — `formatResultsHTML` generates HTML with inline `onclick="toggleSQLSection(this, ...)"` handlers. These rely on global JS functions. Consider moving to Svelte event delegation.
3. **Large components** — `ConversationView.svelte` (1,300+ lines) and `App.svelte` (2,500+ lines) are both large. TECH_REVIEW §4.3 recommends splitting `SettingsView.svelte` into sub-components.

---

## Testing Assessment

### Good

- **Mock-based tests** in `loop_test.go` — the engine loop is tested with mock LLM, query executor, and output handler, proving the loop runs correctly without live dependencies
- **`sql_execution_test.go`** exists for SQL execution logic
- Testing checklist in AGENT_READ_FIRST.md §5 is comprehensive

### Gaps

1. **No integration tests** against real databases — the driver implementations are tested only indirectly
2. **No frontend tests** — Svelte components have no test coverage
3. **No LLM provider integration tests** — the streaming/non-streaming paths are untested against real providers
4. **Migration tests** — the migration system is exercised by `ConnectDatabase` but not independently tested

---

## Specific Code-Level Findings

### `pkg/engine/safety.go` — Read-Only Validation

- **Good:** `ValidateReadOnlySQL` handles SELECT, CTE, parenthesized SELECT, and blocks a comprehensive list of dangerous patterns
- **Good:** `containsSelectIntoTable` correctly distinguishes `SELECT ... INTO table` (write) from `SELECT ... INTO @variable` (harmless)
- **Concern:** `insideSingleQuotedString` is a naive scanner — it doesn't handle backtick/double-quote/dollar-quote strings. This is fine for a blocking (not allowing) check, but could miss edge cases with `""""`-style escaping.
- **Concern:** The `dangerousPatterns` list uses `strings.Contains` — `"EXEC "` would match inside a string literal like `"EXECUTE PROCEDURE"`. The comment-aware stripping helps but isn't perfect.

### `pkg/engine/loop.go` — Agentic Loop

- **Good:** One-shot finality is correctly implemented — after a successful `is_exploration: false` query, further `query_database` calls are rejected
- **Good:** Error retries are bounded (`maxErrorRetries`), and non-retryable errors (auth failures) are short-circuited
- **Good:** Context window overflow detection compares prompt tokens against context limit
- **Good:** Streaming is properly wired through the loop with `OnStream` callback
- **Concern:** The loop uses `log.Printf` instead of `slog` — inconsistent with the rest of the codebase (which uses `slog` per TECH_REVIEW §3.2)
- **Concern:** `logRound` is called multiple times per round in some paths (e.g., the `query_database` case calls it before and after the tool execution). The `storedThisRound` guard prevents double-stores, but it's fragile.

### `pkg/services/agentic_loop.go` — Prompt Construction

- **Good:** Tool definitions are configurable via `AgentLoopConfig`
- **Good:** System prompt includes schema metadata, skills, and business rules
- **Concern:** The file is ~1,600 lines and handles tool definitions, prompt construction, and message building — too many responsibilities

### `pkg/services/sql_execution.go` — SQL Execution

- **Good:** `applyDefaultLimit` correctly handles CTEs (appending after closing paren) and UNION queries (wrapping in outer SELECT)
- **Good:** `strippedTrailingComments` handles both `--` and `/* */` comments
- **Good:** `sanitizeSQLError` strips URI-style credentials from error messages
- **Concern:** `isRetryableError` defaults to `false` for unknown errors — TECH_REVIEW §3.5 recommends flipping the default to `true` with a transition period

### `pkg/models/database.go` — Migrations

- **Good:** Migration system uses `runMigration` for at-most-once execution
- **Good:** `ensureColumn` only adds columns, never modifies existing ones
- **Good:** `safeSQLIdentifier` guards against injection in ALTER TABLE statements
- **Good:** `dropColumnIfExists` skips gracefully if SQLite version is too old

---

## Risk/Reward Summary

| Risk | Reward | Verdict |
|------|--------|---------|
| `app.go` god-object (TECH_REVIEW §1.1) | Maintainability, testability | **Medium priority** — split into domain binding files |
| `discussion_engine.go` size (TECH_REVIEW §1.2) | Maintainability | **Medium priority** — split after DI refactor |
| Plaintext credential storage (TECH_REVIEW §2.1) | User security on shared machines | **High priority** — implement OS keychain encryption |
| `{@html}` rendering (TECH_REVIEW §2.3) | XSS prevention | **Medium priority** — replace with structured rendering |
| `sqlQueryHash` collisions | HTML element ID uniqueness | **Low priority** — use proper hash |
| Global `models.DB` (TECH_REVIEW §1.3) | Testability | **Low priority** — large refactor, low immediate risk |
| No frontend tests | Bug prevention | **Low priority** — add critical path tests first |

---

## Overall Verdict

This is a well-designed, well-documented project. The architecture silos are correctly implemented, the safety foundations are solid, and the documentation (especially AGENT_READ_FIRST.md) sets a high bar for project governance. The main areas for improvement are:

1. **Reduce file sizes** — Split `app.go`, `discussion_engine.go`, and `agentic_loop.go` into focused files
2. **Encrypt credentials at rest** — Implement OS keychain integration
3. **Replace `{@html}` rendering** — Eliminate XSS surface
4. **Add integration tests** — Test against real databases and LLM providers
5. **Consider dependency injection** — Replace global `models.DB` for testability

The project is **production-ready** in its current state. The remaining items are quality-of-life improvements and security hardening, not blockers.
