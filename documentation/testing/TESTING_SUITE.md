> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Automated Testing Suite

**Date:** 2026-07-26  
**Project:** YourQL — Automated test suite design  
**Status:** Proposal — no code implemented. Revised 2026-08-13 to (a) align the discussion-engine tests with the current agentic tool-calling loop, (b) add a black-box configuration-matrix harness (§2.3) as the fastest path to automated bug-hunting, (c) close gaps against `AGENT_READ_FIRST.md` (final-query read-only validation, credential-logging safety, auto-updater safety, OAuth scope regression), and (d) create a companion execution framework in [`TESTING_EXECUTION.md`](TESTING_EXECUTION.md) covering the *how*: blank-slate DB setup, config seeding, message sending, and response analysis. Nothing in this document has been implemented in code yet.

> **ABSOLUTE RULE — FIND AND DOCUMENT ONLY, NEVER FIX.** The sole output of
> running this test suite (in any form — unit tests, the §2.3 harness, E2E) is
> a list of discovered bugs, written to a report/markdown file. Under no
> circumstances does the testing process itself modify application code, fix a
> bug it finds, refactor code "while you're in there," add a feature, or change
> anything other than a markdown document (e.g. `documentation/TEST_RESULTS.md`
> or this file). This applies to every phase and every agent that runs these
> tests: write test code and harness code as described below, run it, record
> findings — do not touch `pkg/`, `frontend/src/`, `app.go`, `main.go`, or any
> other non-test, non-markdown file to "resolve" something the tests surface.
> A discovered bug is a finding to document, never an invitation to patch.

---

## 1. Overview

This document describes a comprehensive automated test suite for YourQL, covering the Go backend services, data models, LLM integration, database drivers, and the Svelte 5 frontend. The suite is designed to be runnable in CI and on a developer's machine without requiring external services (databases, LLM APIs, or cloud credentials).

---

## 2. Testing Architecture

### 2.1 Layer Separation

| Layer | Technology | Test Framework | Requires |
|---|---|---|---|
| Models & Migrations | Go | `testing` + `testify` | In-memory SQLite |
| Services (business logic) | Go | `testing` + `testify` | Mocked `*sql.DB`, mock LLM clients |
| LLM Clients | Go | `testing` + `testify` + `httptest` | Mock HTTP server |
| Database Drivers | Go | `testing` + `testify` | Real DB container or SQLite fixture |
| Frontend Components | Svelte 5 + JS | Vitest + `@testing-library/svelte` | JSDOM |
| Wails Bindings | Go + JS | Wails dev mode | Running app instance |
| End-to-End | Go + JS | Playwright | Built app binary |

### 2.2 Dependency Injection Prerequisite

The unit tests in this section depend on the dependency-injection refactor described in `TECH_REVIEW.md` §1.3. Currently, all service functions reach directly into the package-level `models.DB` variable:

> **Note:** this refactor is **not** a prerequisite for the black-box
> configuration-matrix harness in §2.3 — that harness runs against the existing
> global `models.DB` via a temporary database. DI is required only for the
> isolated unit tests below.

```go
// Current state — untestable
func ListConversationsByUser() ([]*models.Conversation, error) {
    rows, err := models.DB.Query("SELECT ...")  // can't mock this
}
```

Before unit tests for services can be written, each service function must accept a `*sql.DB` parameter (or be a method on a struct that holds one). This is isolated from the Wails binding layer — the `App` struct can hold instantiated services, but the tests bypass `App` entirely and call services directly with a test database.

**Priority order for DI conversion:**
1. Conversation CRUD (`conversation.go`) — needed by almost every other test
2. LLM provider CRUD (`llm_provider.go`) 
3. Query tracking (`query.go`)
4. Skill CRUD (`skill_service.go`)
5. Data source CRUD (`database_connection.go`)
6. SQL execution (`sql_execution.go`) — depends on DBDriver interface, already mockable
7. Discussion engine (`discussion_engine.go`) — depends on all of the above

### 2.3 Black-Box Configuration-Matrix Harness (Recommended First Deliverable)

The unit-test plan below is the long-term quality net. The fastest way to the
project's actual goal — *exercise YourQL from blank-slate databases across many
configurations and summarize the results to find and document bugs* — is a
black-box harness that does **not** require the §2.2 DI refactor.

**[`TESTING_EXECUTION.md`](TESTING_EXECUTION.md)** is the companion document
that implements everything described here: concrete Go code for blank-slate DB
setup, config seeding via existing service CRUD functions, `FakeLLMClient`
implementation, message-sending via `ProcessUserMessage`, response analysis,
and the bug-report output format. Read that document for the *how*; this
document defines the *what*.

**How it works:**

1. **Blank slate per run.** Add a small additive helper
   `models.ConnectDatabaseAt(path string)` (opens the given SQLite file and runs
   `migrate()`) or, in a test, set `models.DB` directly against a temp file.
   `getDBPath()` currently derives `~/.yourql/yourql.db` from
   `user.Current().HomeDir`, which is fragile to override when cgo is on — so a
   path-taking constructor is preferred over mutating `$HOME`.
2. **Named configurations.** A config struct seeds a fresh DB via the existing
   service CRUD functions:
   - LLM provider (type/model/API key) + which one is default
   - data source (SQLite fixture path or a fake `DBDriver`) + `ExplorationSafety`
     mode + row limits
   - skills (0..n) + which are active on the discussion
   - `discussion_defaults` + `agent_loop_config` overrides
   - per-conversation flags: `vizEnabled`, `summarize`, `maxMessages`,
     `maxContextMessages`
3. **Deterministic fakes.** `LLMClient` and `DBDriver` are already interfaces:
   - `FakeLLMClient` implements all four `LLMClient` methods and returns a
     scripted tool-call sequence (`query_database` → `respond_to_user` →
     `render_chart`) or a scripted text/error response.
   - `FakeDBDriver` (or a real SQLite `DataSource` pointing at a seeded fixture)
     returns canned schema and rows, so no real LLM API or external DB is needed.
4. **Scenario list (wide functionality):**
   - conversation lifecycle: create → send message → assert assistant message,
     rendered result table/HTML, and `queries` log row
   - provider / data source / skill CRUD, default-handling, delete/archive/restore
   - read-only enforcement: exploration and **final** query reject DML/DDL
     (including `WITH … DELETE/UPDATE/INSERT` and parenthesized variants) —
     see the "Final-Query Safety Validation" tests in §5.3, which currently
     document a known gap on the final-query path rather than assume parity
     with the exploration path
   - one-shot finality, exploration budget exhaustion, error-retry, cancellation
   - export (HTML/Markdown/PDF), chart-config resolution, summarization
5. **Runner + summary.** Iterate `config × scenario`; record
   `PASS / FAIL / SKIP` plus captured error/stack and a one-line bug note;
   emit a committed Markdown/JSON report (e.g. `documentation/TEST_RESULTS.md`)
   so findings become durable bug documentation.

**Find-and-document only.** When a scenario fails, the harness's job ends at
recording it in the report — name the bug, the config it reproduces under, and
the evidence (error text, mismatched output, safety-gate bypass, etc.). Do not
follow a failure by editing `pkg/`, `frontend/src/`, or any other application
file to make the scenario pass. If a failure looks trivial to fix, document it
as trivial-to-fix in the report and move on — fixing is a separate, deliberate,
human-reviewed activity outside this process (see the banner at the top of
this document).

**Rationale:** this delivers config-matrix coverage and bug documentation
immediately, with no dependency on the DI refactor, real LLM APIs, or the
Playwright/Wails binary. The unit and E2E layers in the rest of this document
remain valuable follow-ups.

---

## 3. Unit Tests — Models & Migrations

### 3.1 Scope

Test the database schema, migration logic, and data access operations in `pkg/models/`.

### 3.2 Approach

Use an **in-memory SQLite database** (`:memory:` with `mode=memory&cache=shared`) for every test. Each test creates a fresh database, runs `migrate()`, and tears down. No persistent state between tests.

### 3.3 Test Cases

| Test | Description |
|---|---|
| `TestConnectDatabase` | In-memory DB opens and migrates without error |
| `TestConnectDatabase_MigrationIdempotent` | Running migrate() twice doesn't error |
| `TestEnsureColumn_AddsMissingColumn` | `ensureColumn` adds a column to an existing table |
| `TestEnsureColumn_SkipsExistingColumn` | `ensureColumn` is a no-op when column already exists |
| `TestSafeSQLIdentifier_Valid` | `safeSQLIdentifier` accepts `table_name`, `column123`, `_private` |
| `TestSafeSQLIdentifier_Invalid` | `safeSQLIdentifier` rejects `123abc`, `drop table`, empty string |
| `TestRunMigration_RecordsApplied` | `runMigration` records success in `schema_migrations` |
| `TestRunMigration_SkipsPreviouslyApplied` | Second call to same migration name is a no-op |
| `TestDropColumnIfExists_DropsColumn` | Column is removed when present. `dropColumnIfExists` is a real migration helper (`models/database.go`) used only for post-migration cleanup of known-legacy columns (e.g. `conversations.user_id`); it must never drop a live column that still holds un-migrated data |
| `TestDropColumnIfExists_Idempotent` | Second call with already-dropped column is a no-op |

### 3.4 Test Data

No fixtures needed — DDL operations are self-contained.

---

## 4. Unit Tests — Services (Conversation, LLM Provider, Query, Skill, Data Source)

### 4.1 Scope

Test CRUD operations and business logic in `pkg/services/conversation.go`, `llm_provider.go`, `query.go`, `skill_service.go`, and `database_connection.go`.

### 4.2 Approach

After DI conversion, each service function accepts an `*sql.DB`. Tests pass an in-memory SQLite database. A helper function `setupTestDB(t *testing.T) *sql.DB` creates the in-memory DB, runs migrations, and returns the handle.

For the `App`-level delegate methods in `app.go`, tests call the service functions directly — bypassing Wails entirely.

### 4.3 Test Cases — Conversation

| Test | Description |
|---|---|
| `TestCreateConversation` | Creates a conversation, verifies returned struct has ID and timestamps |
| `TestCreateConversation_WithProviderAndDataSource` | Creates with linked LLM provider and data source |
| `TestGetConversationByID` | Fetches a conversation by ID |
| `TestGetConversationByID_NotFound` | Returns error for non-existent ID |
| `TestListConversationsByUser_ExcludesDeleted` | Soft-deleted conversations are filtered out |
| `TestListConversationsByUser_PinnedFirst` | Pinned conversations sort to top, then by updated_at |
| `TestSoftDeleteConversation` | Sets status to `deleted` and populates `deleted_at` |
| `TestArchiveConversation` | Sets status to `archived` |
| `TestRestoreConversation` | Sets status back to `active` |
| `TestUpdateConversationTitle` | Title changes and updated_at refreshes |
| `TestDuplicateConversation_CopiesMessages` | Duplicate conversation has a copy of all messages |
| `TestCreateConversationMessage` | Message is created and conversation updated_at refreshes |
| `TestGetConversationMessages_Ordered` | Messages return in chronological order |
| `TestDeleteConversationMessages` | All messages for a conversation are removed |
| `TestUpdateConversationMaxMessages` | Setting updates and persists |

### 4.4 Test Cases — LLM Provider

| Test | Description |
|---|---|
| `TestCreateLLMProvider` | Creates provider, verifies defaults (is_active=true, is_default=false) |
| `TestSetDefaultLLMProvider` | Setting one as default unsets the previous default |
| `TestGetDefaultLLMProvider_None` | Returns nil when no default set |
| `TestGetDefaultLLMProvider_One` | Returns the provider marked is_default |
| `TestDeleteLLMProvider_CannotDeleteDefault` | Deleting the default provider returns an error |
| `TestDeleteLLMProvider_CannotDeleteWithActiveConversations` | Referenced by active conversations → error |
| `TestUpdateLLMProvider_OnlyChangedFields` | Partial update leaves other fields unchanged |
| `TestListLLMProviders_DefaultFirst` | Default provider appears first in list |

### 4.5 Test Cases — Data Source

| Test | Description |
|---|---|
| `TestCreateDataSource` | Creates data source with all fields |
| `TestParseConfig_Empty` | Empty config returns default DataSourceConfig |
| `TestParseConfig_ValidJSON` | Parses exploration settings, limits, business rules |
| `TestParseConfig_InvalidJSON` | Returns error for malformed config |
| `TestSetConfig_RoundTrip` | SetConfig → ParseConfig returns identical struct |
| `TestSetDefaultDataSource` | Setting one as default unsets the previous default |
| `TestDeleteDataSource` | Removes data source |
| `TestTestDataSource_InvalidConnection` | Returns error for unreachable host |

### 4.6 Test Cases — Skills

| Test | Description |
|---|---|
| `TestCreateSkill` | Creates skill with name and markdown content |
| `TestUpdateSkill` | Name and content update, updated_at refreshes |
| `TestDeleteSkill` | Skill is removed |
| `TestSetSkillActive` | Toggles is_active flag |
| `TestListSkills` | Returns all skills |
| `TestSetConversationSkill` | Enables/disables a skill for a conversation |
| `TestGetConversationSkillIDs` | Returns enabled skill IDs for a conversation |
| `TestGetEnabledSkillsContent` | Returns concatenated markdown for active skills |

### 4.7 Test Cases — Query

| Test | Description |
|---|---|
| `TestCreateQuery` | Creates query record with all fields |
| `TestUpdateQueryStatus` | Status updates, timestamps and optional fields store correctly |
| `TestGetQueryByConversation` | Returns queries filtered by conversation ID |

---

## 5. Unit Tests — SQL Execution Engine

### 5.1 Scope

Test `pkg/services/sql_execution.go` — query execution, safety validation, result formatting, markdown rendering, chart config resolution, and the `AssistantResponse.ToHTML()` method.

### 5.2 Approach

SQL execution tests use an in-memory SQLite database with pre-seeded test data. Validation tests are pure logic (no DB required). Rendering tests assert on the HTML output string.

### 5.3 Test Cases

#### Query Execution

| Test | Description |
|---|---|
| `TestExecuteSQL_BasicSelect` | Simple SELECT returns column names and rows |
| `TestExecuteSQL_WithLimit` | Query contains LIMIT or one is auto-appended |
| `TestExecuteSQL_NonSelectRejected` | INSERT/UPDATE/DELETE returns error |
| `TestExecuteSQL_InvalidSyntax_ReturnsError` | Malformed SQL returns query error |
| `TestExecuteSQL_CTEQuery` | WITH clause queries execute correctly |
| `TestExecuteSQL_UnionQuery_LimitApplied` | UNION gets wrapped in subquery with LIMIT |
| `TestApplyDefaultLimit_NoChange` | Query already has LIMIT → no modification |
| `TestApplyDefaultLimit_Added` | Long query without LIMIT → LIMIT appended |
| `TestApplyDefaultLimit_CTE` | CTE query gets LIMIT after closing paren |
| `TestApplyDefaultLimit_Union` | UNION wrapped in outer SELECT with LIMIT |

#### Exploration Safety Validation

| Test | Description |
|---|---|
| `TestValidateExploration_Strict_AllowsSimpleSelect` | Basic SELECT passes |
| `TestValidateExploration_Strict_RejectsJoin` | JOIN is blocked in strict mode |
| `TestValidateExploration_Strict_RejectsGroupBy` | GROUP BY is blocked in strict mode |
| `TestValidateExploration_Moderate_AllowsJoin` | Single JOIN passes in moderate mode |
| `TestValidateExploration_Moderate_RejectsSubquery` | Subquery blocked in moderate mode |
| `TestValidateExploration_Relaxed_AllowsSubquery` | Subquery passes in relaxed mode |
| `TestValidateExploration_AllModes_RejectsDDL` | INSERT/DROP/ALTER blocked in all modes |
| `TestValidateExploration_AllModes_RejectsDML` | UPDATE/DELETE blocked in all modes |
| `TestParseExplorationSafety_DefaultsToStrict` | Empty string → ExplorationStrict |
| `TestParseExplorationSafety_CaseInsensitive` | "MODERATE" → ExplorationModerate |

#### Final-Query Safety Validation (priority — closes an `AGENT_READ_FIRST.md` §0 gap)

> **Why this subsection exists.** `AGENT_READ_FIRST.md` §0 treats the read-only
> invariant as absolute for *both* exploration and final (`is_exploration:
> false`) queries, and states the app's own validation — not the LLM's good
> behavior, not the user's DB role — is the primary safety control. As of this
> revision, `executeSQLWithMode()` (the function the final-query path calls)
> only checks that a query *starts with* `SELECT` or `WITH`; it does **not**
> run the dangerous-pattern check that `validateExplorationQuery()` runs for
> exploration queries. A data-modifying CTE (`WITH x AS (SELECT 1) DELETE FROM
> users`) starts with `WITH` and is not currently rejected on the final-query
> path. These tests exist to **detect and document** that gap (and confirm it
> if/when it is closed) — per the rule at the top of this document, finding
> this is the deliverable; fixing the underlying code is explicitly out of
> scope for the testing process.

| Test | Description |
|---|---|
| `TestFinalQuery_RejectsDML` | `is_exploration: false` with `INSERT`/`UPDATE`/`DELETE` → document whether it is rejected before reaching a data source |
| `TestFinalQuery_RejectsDDL` | `is_exploration: false` with `DROP`/`ALTER`/`TRUNCATE`/`CREATE` → document whether it is rejected |
| `TestFinalQuery_RejectsDataModifyingCTE` | `WITH x AS (SELECT 1) DELETE FROM users` (and `UPDATE`/`INSERT`/`MERGE` variants) → document whether the `WITH`-prefix check alone lets this through |
| `TestFinalQuery_RejectsParenthesizedDML` | `(DELETE FROM users)`-style or comment-obfuscated variants → document handling |
| `TestFinalQuery_MatchesExplorationSafety` | Same dangerous-pattern inputs run through both the exploration gate and the final-query gate → document any behavioral difference between the two paths, since §0 requires them to be equally strict |
| `TestFinalQuery_NativeQuerierPathAlsoReadOnly` | For `NativeQuerier` drivers (BigQuery, Google Sheets), confirm the same SELECT-only / DML-rejection behavior applies — native paths bypass `database/sql` and must not bypass the safety gate too |

#### Retry Logic

| Test | Description |
|---|---|
| `TestIsRetryableError_FatalConnectionRefused` | Connection refused → not retryable |
| `TestIsRetryableError_FatalAuthentication` | Auth error → not retryable |
| `TestIsRetryableError_RetryableSyntaxError` | Syntax error → retryable |
| `TestIsRetryableError_RetryableUnknownColumn` | Unknown column → retryable |
| `TestIsRetryableError_UnknownError_DefaultsToRetryable` | Unmatched error → currently retryable (current behavior) |

#### Markdown Rendering

| Test | Description |
|---|---|
| `TestRenderMarkdown_PlainText` | Plain text passes through in `<p>` tags |
| `TestRenderMarkdown_Bold` | `**bold**` → `<strong>bold</strong>` |
| `TestRenderMarkdown_Italic` | `*italic*` → `<em>italic</em>` |
| `TestRenderMarkdown_BulletList` | Markdown list → `<ul><li>...</li></ul>` |
| `TestRenderMarkdown_NumberedList` | Numbered list → `<ol><li>...</li></ol>` |
| `TestRenderMarkdown_InlineCode` | `` `code` `` → `<code>code</code>` |
| `TestRenderMarkdown_CodeBlock` | Fenced code block → `<pre><code>...</code></pre>` |
| `TestRenderMarkdown_Link` | `[text](url)` → `<a href="url">text</a>` |
| `TestRenderMarkdown_EmptyString` | Empty input → empty output |
| `TestRenderMarkdown_NoRawHTML` | `<script>` in markdown → escaped, not executed |

#### AssistantResponse Rendering

| Test | Description |
|---|---|
| `TestToHTML_SummaryOnly` | Summary appears, no table when no Result |
| `TestToHTML_ExplanationOnly` | Explanation appears when no Summary and no Result |
| `TestToHTML_SummaryWithExploration` | Summary + exploration HTML both rendered |
| `TestToHTML_TableWithRows` | Result table renders with correct row count |
| `TestToHTML_TableNoRows` | "No rows returned" when RowCount == 0 |
| `TestToHTML_SummaryCollapsesTable` | Summary present → table wrapped in `<details>` |
| `TestToHTML_NoSummaryShowsTableDirect` | No summary → table rendered directly |
| `TestToHTML_SQLInToolbar` | SQL appears in popover div, not as separate block |
| `TestToHTML_ChartConfigInMetadata` | chart_config present → stored in metadata JSON |

#### Result Formatting

| Test | Description |
|---|---|
| `TestFormatResults_Empty` | Zero rows → "No rows returned" |
| `TestFormatResults_ColumnsAndRows` | Column headers + data rows in markdown table |
| `TestFormatResultsHTML_SortableTable` | Output includes `data-sort-rows` attribute |
| `TestFormatResultsHTML_RowCollapse` | >10 rows → collapse button present |
| `TestFormatResultsHTML_CSVButton` | CSV export button present |
| `TestFormatResultsHTML_SQLPopover` | SQL toggle button + hidden popover div |
| `TestFormatResultsHTML_NumberCells` | Numeric values get `num-cell` class |
| `TestFormatResultsHTML_DateCells` | Date values get `date-cell` class |

#### Chart Config Resolution

| Test | Description |
|---|---|
| `TestResolveChartConfig_BarChart` | `$column_name` → replaced with column data array |
| `TestResolveChartConfig_MultipleRefs` | Multiple `$col` refs all resolved |
| `TestResolveChartConfig_ScatterZip` | `{x: "$col1", y: "$col2"}` → zipped point objects |
| `TestResolveChartConfig_InvalidJSON_ReturnsError` | Malformed viz_config → error |
| `TestResolveChartConfig_UnknownColumn` | `$nonexistent` stays as literal string |

---

## 6. Unit Tests — Discussion Engine

### 6.1 Scope

Test the core orchestration logic in `pkg/services/discussion_engine.go` and `pkg/services/agentic_loop.go`: tool-call argument parsing, LLM response/fallback extraction, the agentic exploration-then-final-query loop flow (including one-shot finality), prompt building, and the `respond_to_user` / `query_database` / `render_chart` tool handlers. (The pre-agentic-loop "action parsing" and "three action handlers" model is legacy — see the architecture note in §6.3.)

### 6.2 Approach

The discussion engine depends on:
- A **mock LLM client** that returns pre-programmed responses
- A **mock database connection** (in-memory SQLite for schema setup)
- **Pre-seeded conversation/message data** in the test DB

A `MockLLMClient` implements the `LLMClient` interface and returns responses from a predefined list. Each test sets up the mock with the response sequence it expects.

> **Architecture note (revised 2026-08-13).** The "Response Parsing" and
> "JSON Extraction" tables below predate the current agentic tool-calling loop
> (`pkg/services/agentic_loop.go`, `runAgenticLoop`). The `action`-based parser
> (`sql_query` / `clarification` / `answer` / `sql_exploration`) is no longer the
> primary path — today the LLM emits tool calls. The authoritative tests for the
> current pipeline are:

#### Agentic Loop (current architecture)

| Test | Description |
|---|---|
| `TestParseToolCall_QueryDatabase` | `query_database` args (`sql`, `is_exploration`) parsed correctly |
| `TestParseToolCall_RespondToUser` | `respond_to_user` args (`message`) parsed correctly |
| `TestParseToolCall_RenderChart` | `render_chart` args (`chart_config`) parsed correctly |
| `TestParseToolCall_UnknownTool` | unknown tool name → safe round error, no panic |
| `TestParseToolCall_MalformedArguments` | malformed args JSON → safe round error, no panic |
| `TestParseToolCall_MissingRequiredFields` | `query_database` without `sql` → rejected, another round |
| `TestFallbackTextAsRespondToUser` | plain-text LLM response (no tool call) → treated as `respond_to_user` |
| `TestFallbackEmptyOrTruncated` | empty/truncated response → clarification, never a silent wrong answer |
| `TestLoop_ExploreThenFinalQuery` | exploration round(s) then `is_exploration: false` → final result surfaced |
| `TestLoop_OneShotFinality` | after final query, further `query_database` calls rejected; only `respond_to_user`/`render_chart` allowed |
| `TestLoop_ExplorationBudgetExhausted` | max rounds reached → forced final query or clarification |
| `TestLoop_RespondToUserAlone` | `respond_to_user` with no data → clarification, not a dangling promise |
| `TestLoop_RenderChartNoPending` | `render_chart` with no pending result → error fed back |
| `TestLoop_ErrorRetry` | failed final query → one correction attempt, then surfaced error |
| `TestLoop_Cancellation` | context cancellation → clean abort, no partial answer |
| `TestLoop_FinalQueryReadOnlyGate` | drive a `query_database(is_exploration:false)` tool call containing DML/DDL/data-modifying-CTE through the full loop — documents whether the loop-level flow relies on §5.3's final-query gate or has its own (currently, it has none) |

The two subsections below are retained as legacy reference only; do not treat
them as the current contract.

### 6.3 Test Cases

#### Response Parsing

| Test | Description |
|---|---|
| `TestParseLLMResponse_ValidSQLQuery` | `{"action":"sql_query","sql_query":"SELECT 1"}` → parsed correctly |
| `TestParseLLMResponse_ValidClarification` | `{"action":"clarification","clarification_question":"..."}` → parsed |
| `TestParseLLMResponse_ValidAnswer` | `{"action":"answer","answer":"Hello"}` → parsed correctly |
| `TestParseLLMResponse_ValidExploration` | `{"action":"sql_exploration","sql_query":"SELECT ..."}` → parsed |
| `TestParseLLMResponse_RawSQL_LooksLikeSQL` | `SELECT * FROM users` → treated as sql_query |
| `TestParseLLMResponse_InvalidJSON_Braced` | `{broken json}` → clarification with error message |
| `TestParseLLMResponse_PlainText` | `Hello, here is your answer` → treated as answer (not clarification) |

#### JSON Extraction

| Test | Description |
|---|---|
| `TestExtractJSON_MarkdownCodeBlock` | ```` ```json\n{...}\n``` ```` → `{...}` |
| `TestExtractJSON_EmbeddedInText` | `Some text {"action":"sql_query",...} more text` → `{...}` |
| `TestExtractJSON_BareJSON` | `{"action":"clarification",...}` → unchanged |
| `TestExtractJSON_ThinkTags` | `</think>\n{"action":"sql_query",...}` → strips think tags |
| `TestExtractJSON_NoJSON` | `Just some text` → unchanged |

#### System Prompt Building

| Test | Description |
|---|---|
| `TestBuildSystemPrompt_WithSchema` | Schema tables and columns appear in prompt |
| `TestBuildSystemPrompt_NoSchema` | No schema section when no DB |
| `TestBuildSystemPrompt_VizEnabled` | Visualization instructions included |
| `TestBuildSystemPrompt_VizDisabled` | Visualization instructions omitted |
| `TestBuildSystemPrompt_ExplorationStrict` | Strict mode constraints documented |
| `TestBuildSystemPrompt_ExplorationRelaxed` | Relaxed mode constraints differ |
| `TestBuildSystemPrompt_SystemPromptOverride` | Custom system prompt from DataSourceConfig appears |
| `TestBuildSystemPrompt_BusinessRules` | Business rules injected |
| `TestBuildSystemPrompt_SkillsContent` | Skills content appended |
| `TestBuildSystemPrompt_Truncation` | Prompt exceeding 16KB is truncated with note |
| `TestBuildSystemPrompt_TableDescriptions` | Custom table descriptions appear in schema |
| `TestBuildSystemPrompt_ColumnDescriptions` | Custom column descriptions appear in schema |

#### LLM Message Construction

| Test | Description |
|---|---|
| `TestBuildLlmMessages_HistoryIncluded` | Previous user + assistant messages appear |
| `TestBuildLlmMessages_ExplorationAsSystem` | Exploration messages re-role as "system" |
| `TestBuildLlmMessages_SQLResultsInContext` | Previous sql_results fed back as system message |
| `TestBuildLlmMessages_ConsecutiveUserMerged` | Two user messages in a row are merged |
| `TestBuildLlmMessages_LLMContentPreferred` | llm_content used over stripped HTML content |
| `TestBuildLlmMessages_AnswerActionInContext` | answer messages' llm_content included for LLM |

#### Full Pipeline with Mock LLM (agentic loop)

| Test | Description |
|---|---|
| `TestProcessUserMessage_FinalQuery` | mock returns `query_database(is_exploration:false)` → SQL executed → result rendered |
| `TestProcessUserMessage_Clarification` | mock returns `respond_to_user` clarification → assistant clarification message created |
| `TestProcessUserMessage_RespondToUser` | mock returns `respond_to_user` with text → assistant answer message created |
| `TestProcessUserMessage_ExploreThenFinal` | mock explores then final-queries → both surfaced/recorded correctly |
| `TestProcessUserMessage_UnknownToolRetry` | mock returns unknown tool → retried up to maxRounds |
| `TestProcessUserMessage_NoDatabase_FinalQueryError` | final query without DB → error handled gracefully |
| `TestProcessUserMessage_PanicRecovery` | a panic in processing → recovered, error message written |
| `TestProcessUserMessage_SQLExecutionError` | SQL fails → error sent back to LLM for correction |
| `TestProcessUserMessage_ExploreRejected_Adapts` | exploration rejected by safety mode → model adapts or clarifies |
| `TestProcessUserMessage_RoundsExhausted_ForcesFinal` | max rounds reached → forced final query/clarification |

#### SQL Context Formatting

| Test | Description |
|---|---|
| `TestFormatSQLResultsForLLM_WithRows` | Results formatted as markdown table for LLM |
| `TestFormatSQLResultsForLLM_Empty` | Empty results → empty string |
| `TestFormatSQLResultsForLLM_RowTruncation` | >200 rows → truncated to 200 with note |
| `TestFormatSQLResultsForLLM_CellTruncation` | >80 char cell → truncated with ... |

#### Exploration Results

| Test | Description |
|---|---|
| `TestExplorationResult_ToMessageContent` | Round, SQL, row count all included |
| `TestFormatExplorationHTML_Empty` | No results → empty string |
| `TestFormatExplorationHTML_WithResults` | Each round rendered with SQL and result summary |

---

## 7. Unit Tests — LLM Clients

### 7.1 Scope

Test the HTTP interface for each LLM provider: request formatting, response parsing, error handling, and payload capture.

### 7.2 Approach

Use `net/http/httptest` to create mock HTTP servers that return pre-defined responses. Each test configures the mock server to return a specific JSON response, then calls the LLM client and asserts the result.

### 7.3 Test Cases

#### OpenAI Client

| Test | Description |
|---|---|
| `TestOpenAI_ChatCompletion` | Valid response → content extracted from choices[0].message.content |
| `TestOpenAI_ChatCompletionWithPayload` | Returns content + request/response JSON strings |
| `TestOpenAI_ErrorStatus` | Non-200 → error with status code and body |
| `TestOpenAI_EmptyChoices` | Response with no choices → appropriate error |
| `TestOpenAI_APIErrorField` | Error field in response → message extracted |
| `TestOpenAI_NoAPIKeyForCloud` | Missing API key for api.openai.com → error |
| `TestOpenAI_LocalEndpointNoKeyOK` | Custom base URL without API key → succeeds |

#### Anthropic Client

| Test | Description |
|---|---|
| `TestAnthropic_ChatCompletion` | Valid response → content extracted |
| `TestAnthropic_ErrorMessage` | API error format → parsed correctly |

#### Ollama Client

| Test | Description |
|---|---|
| `TestOllama_ChatCompletion` | Valid response → content extracted |
| `TestOllama_ConnectionRefused` | Unreachable server → error |

#### Local Client

| Test | Description |
|---|---|
| `TestLocal_OpenAICompatible` | /v1/chat/completions endpoint → response parsed |
| `TestLocal_LegacyCompletionFallback` | OpenAI endpoint fails → falls back to /completion |
| `TestLocal_BothFail` | Both endpoints fail → error |

#### Credential Logging Safety (all providers — closes an `AGENT_READ_FIRST.md` §3.5 gap)

> `AGENT_READ_FIRST.md` §3.5 states plainly: "Never log API keys or passwords."
> Every LLM client logs request/error metadata (`log.Printf("[OpenAI] ERROR —
> HTTP %d ... %s", ..., string(body))` and similar in the Anthropic/Ollama/Local
> clients). The key itself is not interpolated into those log lines today, but
> there is no regression test guarding that, and error response *bodies* are
> logged verbatim — a provider that echoes request headers/config in an error
> body could leak a key into the log file (`~/.yourql/yourql.log`, see
> `main.go`'s `setupLogging`). These tests exist to catch a future regression,
> not to fix today's logging code.

| Test | Description |
|---|---|
| `TestOpenAI_LogsNeverContainAPIKey` | Configure a known API key, trigger success + error + malformed-response paths, assert the key string never appears in any `log.Printf`/`slog` output |
| `TestAnthropic_LogsNeverContainAPIKey` | Same, for the `x-api-key` header value |
| `TestOllama_LogsNeverContainCredentials` | Same, for any configured auth/basic-auth value |
| `TestLocal_LogsNeverContainCredentials` | Same, for local/custom endpoint credentials |
| `TestDataSource_LogsNeverContainPassword` | Trigger a `TestDataSource`/connection-failure path with a real password configured → assert the password never appears in logs or in the error string returned to the frontend |
| `TestDiagnosticLogFile_NeverContainsSecrets` | With `logging_enabled` on, run a representative session (provider + data source configured) → grep the resulting `~/.yourql/yourql.log` for the configured API key and DB password strings, assert zero matches |

---

## 8. Integration Tests — Database Drivers

### 8.1 Scope

Test each `DBDriver` implementation for DSN construction, connection, schema introspection, and query execution.

### 8.2 Approach

For drivers that support it (**SQLite**), use in-memory databases with pre-seeded test data. For external drivers (**MySQL, PostgreSQL, SQL Server, Snowflake, BigQuery**), these tests are skipped by default (guarded by a build tag or environment variable) and run only when a test database is available (e.g., in CI with Docker Compose). The in-memory SQLite driver serves as the primary driver test and validates the `DBDriver` interface contract.

### 8.3 Test Cases

#### Driver Registry

| Test | Description |
|---|---|
| `TestDriverRegistry_AllRegistered` | All 9 drivers appear in `GetSupportedDBTypes()` |
| `TestDriverRegistry_TypeKeyUnique` | No two drivers share the same TypeKey |
| `TestDriverRegistry_GetDriver_Valid` | Known type returns driver |
| `TestDriverRegistry_GetDriver_Unknown` | Unknown type returns error |

#### SQLite Driver (primary interface-contract test)

| Test | Description |
|---|---|
| `TestSQLite_BuildDSN` | DSN includes path + busy_timeout + WAL pragmas |
| `TestSQLite_BuildDSN_MissingPath` | Missing database path → error |
| `TestSQLite_GetSchema_Tables` | Returns all user tables (excludes sqlite_% internal) |
| `TestSQLite_GetSchema_Columns` | Columns include name, type, nullable, primary key |
| `TestSQLite_GetSchema_RowCounts` | Row counts are populated |
| `TestSQLite_GetSchema_Indexes_WhenEnabled` | Indexes returned when IncludeIndexes is true |
| `TestSQLite_GetSchema_ForeignKeys_WhenEnabled` | FKs returned when IncludeForeignKeys is true |
| `TestSQLite_TypeKey` | Returns "sqlite" |
| `TestSQLite_DisplayName` | Returns "SQLite" |
| `TestSQLite_DefaultPort` | Returns 0 |
| `TestSQLite_SQLDialectHint` | Returns non-empty hint string |

#### Per-Driver DSN Construction

| Test | Description |
|---|---|
| `TestMySQL_BuildDSN` | DSN format includes user:password@tcp(host:port)/db?params |
| `TestPostgres_BuildDSN` | URL format with sslmode and search_path |
| `TestSQLServer_BuildDSN` | DSN format with encrypt and instance options |
| `TestSnowflake_BuildDSN` | DSN format with account, warehouse, role |
| `TestBigQuery_BuildDSN` | DSN format with project and dataset |

#### OAuth / Native API Scope Regression (closes an `AGENT_READ_FIRST.md` §0 gap)

> §0 requires that "Native, non-`database/sql` integrations (BigQuery, Google
> Sheets) must request and use read-only API scopes only" and that this never
> be expanded. `google_auth.go`'s `googleSheetsScopes` is read-only today. This
> is exactly the kind of constraint that can silently regress in a future edit
> (e.g. someone adds a write feature and widens the scope to make it work) —
> a one-line test makes that an immediately visible, documented failure.

| Test | Description |
|---|---|
| `TestGoogleSheetsScopes_AreReadOnlyOnly` | Assert every entry in `googleSheetsScopes` is a known read-only scope (e.g. `.../auth/spreadsheets.readonly`, `.../auth/drive.readonly`) and fails loudly if any scope string changes to a non-`.readonly` variant |
| `TestBigQuery_NoWriteScopeOrPermission` | If/when BigQuery uses its own OAuth scope list (vs. service-account key), assert the same read-only-only constraint |

---

## 9. Frontend Tests — Svelte 5 Components

### 9.1 Scope

Test the Svelte 5 components: `App.svelte`, `ConversationView.svelte`, `SettingsView.svelte`, and `VizChart.svelte`.

### 9.2 Approach

Use **Vitest** (the test runner bundled with Vite) and **`@testing-library/svelte`** for component rendering and interaction. Mock the Wails runtime calls (`../wailsjs/go/main/App.js` and `../wailsjs/runtime/runtime.js`) so tests run without a Go backend.

### 9.3 Mock Setup

Create `frontend/src/__mocks__/wailsjs/` with stub implementations:

```js
// go/main/App.js
export const ListConversations = vi.fn()
export const CreateConversation = vi.fn()
export const GetConversationMessages = vi.fn()
export const ProcessUserMessage = vi.fn()
// ... all exported functions stubbed

// runtime/runtime.js
export const EventsOn = vi.fn()
export const EventsOff = vi.fn()
export const EventsEmit = vi.fn()
export const BrowserOpenURL = vi.fn()
```

Each test sets up the specific return values it needs via `vi.mocked(ListConversations).mockResolvedValue([...])`.

### 9.4 Test Cases

#### ConversationView

| Test | Description |
|---|---|
| `renders conversation title in header` | Title from activeConversation prop appears |
| `renders user messages in blue bubbles` | User-role messages have `.user-message` class |
| `renders assistant messages` | Assistant-role messages appear |
| `renders markdown in assistant messages` | `**bold**` renders as `<strong>` |
| `sends message on Enter key` | `handleSendMessage` is called |
| `does not send empty message` | Empty input → send button disabled |
| `shows processing indicator during LLM call` | Loading dots appear when processingMessage is set |
| `shows error message` | messageError prop → error banner displayed |
| `shows hidden messages banner when maxMessages set` | Banner appears with correct count |
| `shows all messages when Show All clicked` | `showHiddenMessages` toggle works |
| `shows Show last N when all messages visible` | Button text changes after toggle |
| `shows chart when chart_config in metadata` | VizChart component renders |
| `calls ExportConversationPDF on export button click` | Wails binding is invoked |
| `handles back button click` | onBack callback fires |

#### SettingsView

| Test | Description |
|---|---|
| `renders model configurations tab by default` | Tab content visible |
| `switches to data sources tab` | Data source list appears |
| `switches to skills tab` | Skills list appears |
| `switches to general tab` | General settings visible |
| `creates new LLM provider` | Form submission calls CreateLLMProvider |
| `shows test connection result` | dbStatus message appears |
| `adds new data source` | Form fields sent to CreateDataSource |
| `loads schema for existing connection` | Schema data renders in table |
| `toggles Set as Default button for LLM provider` | SetDefaultLLMProvider called |
| `toggles Set as Default button for data source` | SetDefaultDataSource called |

#### VizChart

| Test | Description |
|---|---|
| `renders canvas element` | Canvas is created in DOM |
| `destroys previous chart on config change` | Old Chart instance is destroyed |
| `shows error on invalid config` | Error text appears in placeholder |
| `does not render when config is null` | No canvas in DOM |

#### App

| Test | Description |
|---|---|
| `renders sidebar with navigation` | Discussions, Settings, About links present |
| `shows conversations list` | ListConversations result rendered |
| `shows empty state when no conversations` | "No discussions found" text |
| `opens new discussion modal` | Modal appears, form fields visible |
| `deletes conversation with confirmation` | Confirm dialog → DeleteConversation called |
| `shows About page content` | Version and feature list render |

---

## 10. End-to-End Tests

### 10.1 Scope

Test complete user workflows through the built application.

### 10.2 Approach

Use **Playwright** to drive the built YourQL application. Tests are run against a compiled build with a fresh test database. Playwright launches the app binary and interacts with it via the Chromium DevTools Protocol (Wails webview exposes a remote debugging port in dev mode).

### 10.3 Test Scenarios

| Scenario | Steps |
|---|---|
| **Create and query a discussion** | Add LLM provider → Add SQLite data source → Create discussion → Type question → Verify assistant response appears |
| **Configuration management** | Add provider → Edit provider → Test connection → Delete provider → Verify removed from list |
| **Exploration workflow** | Enable exploration → Ask complex question → Verify exploration messages appear → Verify final result |
| **PDF export** | Open discussion → Click export → Verify print dialog opens (or verify document title changes) |
| **Conversation management** | Create discussion → Pin it → Archive it → Restore it → Delete it |

### 10.4 CI Integration

E2E tests require a built app binary (platform-specific). They run as a separate CI job that:
1. Builds the app (`wails build`)
2. Launches a headless display server (Xvfb on Linux)
3. Runs Playwright tests against the process
4. Kills the app process on completion

### 10.5 Skipping E2E in PRs

E2E tests are expensive — they're run on `main` branch pushes and release tags, not on every PR. PRs run unit + integration tests only.

### 10.6 Auto-Updater Safety Tests (closes an `AGENT_READ_FIRST.md` §3.8 gap)

> `AGENT_READ_FIRST.md` §3.8 treats the auto-updater as a distinct, high-risk
> trust domain — it replaces the running application binary and can exit the
> process. It calls out three specific, testable guarantees: (1) never install
> without a matching SHA256 checksum, (2) `appVersion == "dev"` skips
> self-update entirely, (3) `PerformUpgradeRestart` only fires after explicit
> user confirmation. None of §5–§10 above previously covered this. These tests
> use `httptest` (mock GitHub release/checksum endpoints) plus a temp
> directory for staged files — no real network calls, no real installs, and
> critically **no modification of the actual installed application** even
> during the test run.

| Test | Description |
|---|---|
| `TestDownloadUpdate_RejectsEmptyChecksum` | `expectedSHA256 == ""` → `DownloadUpdate` refuses and stages nothing |
| `TestDownloadUpdate_RejectsMismatchedChecksum` | Downloaded bytes hash to a different value than `expectedSHA256` → rejected, no runnable/openable artifact staged |
| `TestDownloadUpdate_AcceptsMatchingChecksum` | Correct checksum → download succeeds and stages the artifact |
| `TestCheckForUpdate_DevVersionSkipsSelf` | `appVersion == "dev"` → `CheckForUpdate` makes no network call and returns no update offered |
| `TestCheckForUpdate_RealVersionChecksNetwork` | Non-`dev` `appVersion` → `CheckForUpdate` does query the mock release endpoint |
| `TestPerformUpgradeRestart_RequiresPriorDownload` | Calling the restart path without a prior verified download → refused/no-op (never installs an unverified artifact) |
| `TestPerformUpgradeRestart_NoAutoInvocation` | Static/code-path check (not a runtime assertion): confirm `PerformUpgradeRestart` is only reachable from the explicit "Restart Now" UI action, never from a background/automatic code path — document as a finding if any automatic call site is found |
| `TestReplacementScript_NoElevation` | Per-OS replacement script content/invocation does not request elevated/admin privileges and only touches paths the current user owns |

---

## 11. Test Data & Fixtures

### 11.1 SQLite Test Database Schema

A standard test fixture for schema-introspection and query-execution tests:

```sql
CREATE TABLE customers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER REFERENCES customers(id),
    total REAL,
    status TEXT DEFAULT 'pending',
    order_date TEXT
);

CREATE TABLE order_items (
    id INTEGER PRIMARY KEY,
    order_id INTEGER REFERENCES orders(id),
    product_name TEXT,
    quantity INTEGER,
    price REAL
);
```

Seeded with 5 customers, 10 orders, and 30 order_items.

### 11.2 Mock LLM Response Fixtures

Predefined JSON responses for each action type, stored as test fixtures:

```
testdata/
  llm_responses/
    sql_query_basic.json
    sql_query_with_explanation.json
    clarification.json
    answer_basic.json
    sql_exploration.json
    invalid_json.json
    plain_text.txt
    sql_query_with_viz.json
```

### 11.3 Mock HTTP Server Responses

Predefined HTTP responses for LLM provider tests, matching each provider's API format:

```
testdata/
  llm_api_responses/
    openai_success.json
    openai_error.json
    anthropic_success.json
    anthropic_error.json
    ollama_success.json
```

---

## 12. CI Configuration

### 12.1 GitHub Actions Workflow

```yaml
name: Test

on:
  pull_request:
  push:
    branches: [main]

jobs:
  go-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25.7' }
      - run: go test ./pkg/... -v -count=1 -short

  frontend-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - run: cd frontend && npm ci && npx vitest run

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25.7' }
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - run: cd frontend && npm ci && npm run build
      - run: go build ./...
```

The `-short` flag on `go test` skips integration tests that require external databases (tagged with `//go:build !short` or guarded by `testing.Short()`).

---

## 13. Test Coverage Targets

| Layer | Target Coverage | Notes |
|---|---|---|
| Models & Migrations | 90%+ | Small surface area, high value |
| Services (CRUD) | 85%+ | Straightforward assertions |
| SQL Execution | 80%+ | Safety validation and formatting are critical |
| Discussion Engine | 75%+ | Complex mock setup, prioritize happy paths and error recovery |
| LLM Clients | 80%+ | Mock HTTP makes this fast |
| Database Drivers | 60%+ | Most driver logic is DSN-string building (trivial); schema tests cover the important path |
| Frontend | 65%+ | Focus on interactive elements (forms, toggles, buttons) |

---

## 14. Implementation Priority

| Phase | What | Estimated Scope |
|---|---|---|
| **Phase 1** (prerequisite) | DI refactor — make `*sql.DB` injectable into all service functions | 1-2 days |
| **Phase 2** | Model tests + in-memory SQLite test helpers | 1 day |
| **Phase 3** | Service CRUD tests (conversation, LLM provider, data source, skills, query) | 2 days |
| **Phase 4** | SQL execution engine tests + discussion engine mock tests | 3 days |
| **Phase 5** | LLM client tests (httptest) + driver tests | 1 day |
| **Phase 6** | Frontend component tests (Vitest + testing-library) | 2 days |
| **Phase 7** | CI pipeline + E2E smoke tests | 1 day |

Total: approximately **11-12 days** of dedicated work, heavily front-loaded by the DI refactor which unblocks everything else.

> **Reminder for every phase above, and for the §2.3 harness:** the deliverable
> of this entire testing effort is test code, harness code, and a bug report
> (markdown). It is never a fix, a refactor, or a feature change to the
> application itself. If a phase's tests reveal a bug (including the known
> §5.3 final-query gate, or anything else), the correct action is to add it to
> the bug report with reproduction steps — not to open `pkg/services/*.go` and
> patch it. Treat this as a hard boundary, not a style preference.
