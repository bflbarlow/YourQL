# YourQL — Functionality Silo Map

> Companion to `AGENT_READ_FIRST.md`. That document defines *why* and *what
> matters*. This document defines *what the pieces are*, *how they connect*,
> and *what it would take to isolate each one into a black box* — independently
> testable, independently configurable, independently replaceable.

---

## Overview

YourQL is a Wails desktop application (Go backend + Svelte 5 frontend) that
answers user questions from their databases using an LLM-driven agentic loop.
The backend is organized as:

- **`main.go`** — entry point, Wails lifecycle, embedded assets, `appVersion`
- **`app.go`** — Wails bindings (~1,300 lines, 50+ public methods). The single
  bridge between Go and Svelte.
- **`pkg/models/`** — data structures, DB schema, migrations, the global
  `models.DB` (*sql.DB pointing at `~/.yourql/yourql.db`)
- **`pkg/services/`** — all business logic in a **single Go package** (54
  `.go` files, no sub-packages)
- **`frontend/src/`** — 4 Svelte 5 components (~7,200 lines total)

The services package is the heart of the monolith. Every `.go` file in it
shares the same namespace. There are no internal package boundaries — any
function can call any other function. This document maps the logical
boundaries that *should* exist and describes how to create them.

---

## Logical Sections

### 1. Data Models & Persistence (`pkg/models/`)

| | |
|---|---|
| **Files** | `database.go` (schema, migrations, `models.DB`, DB path resolution), `conversation.go`, `conversation_message.go`, `query.go`, `db_connection.go`, `llm_provider.go`, `skill.go`, `agent_loop_config.go`, `app_setting.go`, `discussion_default.go`, `database_migration.go` |
| **Lines** | ~1,200 across all files |
| **What it does** | Defines all data structures (structs with JSON/db tags), manages the SQLite schema via auto-migrations, and exposes the global `models.DB` connection. All application state — conversations, messages, provider configs, data source configs, skills, settings, query logs — lives in `~/.yourql/yourql.db`. |
| **Key types** | `Conversation`, `ConversationMessage`, `Query`, `DataSource`, `DataSourceConfig`, `LLMProvider`, `Skill`, `AgentLoopConfig`, `AppSetting`, `DiscussionDefault` |
| **Global singleton** | `var DB *sql.DB` — initialized once in `startup()`, closed in `shutdown()` |

**Inbound callers:**
- `app.go` — references model types for every Wails binding (returned as JSON)
- `headless_handlers.go` — same models via the headless HTTP API
- Every file in `pkg/services/` — reads/writes through `models.DB` (~50 direct
  `models.DB.Exec`/`.Query`/`.QueryRow` calls across 12 service files)
- `frontend/wailsjs/go/models.ts` — auto-generated TypeScript mirror of the
  model structs (595 lines)

**Outbound dependencies:**
- `database/sql` + `modernc.org/sqlite` (pure-Go SQLite driver)
- Filesystem (`os`, `path/filepath`) for `~/.yourql/yourql.db` location and
  the active-DB pointer file

**Current coupling level: HIGH**
The global `models.DB` is a naked `*sql.DB` — no interface, no abstraction.
Every service file writes raw SQL strings directly. Model structs are shared
across the entire app with no separation between "engine-internal" vs.
"wire-protocol" types. The migration system (`ensureColumn`, `runMigration`)
is called from `models.ConnectDatabase()` and cannot be invoked independently.

**To black-box:**
- Replace `models.DB` with an interface: `type AppDB interface { Query(...); Exec(...); QueryRow(...) }`. This is straightforward — `*sql.DB` already satisfies the `database/sql` patterns.
- Add a `Migrate()` interface method or keep migrations as an internal
  implementation detail called at startup.
- Separate "wire types" (models returned to the frontend) from "persistence
  types" (internal DB rows) — or at minimum, document which model fields are
  stable API vs. implementation details.
- Create a `type ModelStore interface` grouping all CRUD operations (see §8).

**Difficulty: Low.** The SQLite dependency is already isolated behind
`database/sql`. The main work is defining interfaces and updating ~50 call
sites — mechanical, not architectural.

---

### 2. Conversation & Message CRUD (`conversation.go`)

| | |
|---|---|
| **File** | `pkg/services/conversation.go` (475 lines) |
| **What it does** | Full CRUD for conversations and messages: create, get-by-ID, list, update settings (LLM provider, data source, max messages, context window, streaming, summarize, viz enabled, pinned, show tech details, show context details), duplicate, clear messages, archive, restore, delete. Also conversation-to-skill linking (`SetConversationSkill`, `GetConversationSkillIDs`). |
| **Key functions** | `CreateConversation`, `GetConversationByID`, `ListConversations`, `CreateConversationMessage`, `GetConversationMessages`, `UpdateConversationSettings`, `DeleteConversation`, `ClearConversationMessages`, `ArchiveConversation`, `RestoreConversation`, `DuplicateConversation`, `SetConversationSkill`, `GetConversationSkillIDs`, `CreateConversationWithDefaults` |
| **Inbound** | `discussion_engine.go` (reads conversation, history; creates user/assistant messages), `agentic_loop.go` (reads messages for context), `app.go` (all 50+ bindings), `headless_handlers.go` (HTTP API) |
| **Outbound** | `models.DB` (22 direct SQL calls), `models.Conversation`, `models.ConversationMessage` |

**Current coupling: HIGH.** Raw SQL strings against `models.DB`. No interface.
The engine calls these functions directly. The frontend calls them through Wails
bindings that wrap the same functions.

**To black-box:**
- Define a `ConversationStore` interface:
  ```go
  type ConversationStore interface {
      GetByID(id uint) (*models.Conversation, error)
      GetMessages(conversationID uint) ([]*models.ConversationMessage, error)
      CreateMessage(conversationID uint, role, content string, ...) (*models.ConversationMessage, error)
      Create(title string, llmID, dsID *uint) (*models.Conversation, error)
      // ... update, delete, archive, settings mutations
  }
  ```
- The existing `conversation.go` becomes one implementation of this interface
  (backed by SQLite).
- For testing the engine, a mock `ConversationStore` (in-memory map) is trivial.

**Difficulty: Low.** ~22 SQL calls to wrap, plus a handful of update methods.
Purely mechanical.

---

### 3. Provider & Data Source CRUD

| | |
|---|---|
| **Files** | `llm_provider.go` (352 lines), `db_connection.go` (324 lines) |
| **What they do** | Full CRUD for LLM provider configs and database data source configs. Each has create, get-by-ID, list, update, delete, set-default, plus type-specific helpers (model token detection for providers, config parsing for data sources). |
| **Key functions** | `GetLLMProviderByID`, `GetDefaultLLMProvider`, `ListLLMProviders`, `CreateLLMProvider`, `UpdateLLMProvider`, `DeleteLLMProvider`, `SetDefaultLLMProvider`, `DetectModelMaxTokens` / `GetDataSourceByID`, `GetDefaultDataSource`, `ListDataSources`, `CreateDataSource`, `UpdateDataSource`, `DeleteDataSource`, `SetDefaultDataSource`, `ParseConfig` (on DataSource), `GetSupportedDBTypes` |
| **Inbound** | `discussion_engine.go` (looks up provider + data source at start of pipeline), `app.go` (settings UI bindings), `headless_handlers.go` |
| **Outbound** | `models.DB` (~15 + ~12 direct SQL calls), model structs |

**Current coupling: HIGH.** Same pattern as conversation CRUD — raw SQL, no interfaces.

**To black-box:**
- Define `LLMProviderStore` and `DataSourceStore` interfaces (same pattern as
  `ConversationStore`).
- The discussion engine only needs `GetByID` + `GetDefault` from each — not
  the full CRUD surface. A focused "engine view" interface with just those
  two methods per store would suffice.

**Difficulty: Low.** The engine uses a thin slice (~2 methods each) of these
files' full surface.

---

### 4. Schema Introspection (`database_introspection.go`)

| | |
|---|---|
| **File** | `database_introspection.go` (58 lines) |
| **What it does** | Calls `driver.GetSchema(conn)` for a given data source and returns a `DataSchema` (tables → columns → indexes → foreign keys). Delegates to the per-driver `GetSchema()` implementation. |
| **Key function** | `GetDataSchema(conn *models.DataSource) (*DataSchema, error)` |
| **Key type** | `DataSchema` → `[]TableInfo` → `[]ColumnInfo` + `[]IndexInfo` + `[]ForeignKeyInfo` |
| **Inbound** | `discussion_engine.go` (step 7 of the pipeline), `app.go` (`GetSchemaPreview`) |
| **Outbound** | `db_registry.go` (driver lookup), each driver's `GetSchema()` method |

**Current coupling: LOW.** Already a single-function entry point. The
`DataSchema` type is a simple value type with no DB dependencies.
The driver dispatch happens through the `DBDriver` interface.

**To black-box:**
- Already nearly a black box. The only coupling is the `*models.DataSource`
  parameter. If the engine receives schema data rather than fetching it, the
  caller becomes responsible for introspection.
- Could define `type SchemaIntrospector interface { GetSchema(ds) (*DataSchema, error) }`
  — but the current function signature already serves as an implicit interface.

**Difficulty: Trivial.** This is already the cleanest boundary in the app.

---

### 5. Skills & Settings (`skill_service.go`, `app_settings.go`, `agent_loop_config.go`, `discussion_defaults.go`)

| | |
|---|---|
| **Files** | `skill_service.go` (152 lines), `app_settings.go` (73 lines), `agent_loop_config.go` (262 lines), `discussion_defaults.go` (90 lines) |
| **What they do** | Skills: CRUD for user-defined Markdown prompt fragments, toggle per conversation. Settings: key-value store for theme/accent/scale/timeouts. Agent loop config: overridable prompts and tool descriptions. Discussion defaults: per-conversation default settings. |
| **Key functions** | `GetEnabledSkillsContent`, `ListSkills`, `CreateSkill`, `UpdateSkill`, `DeleteSkill`, `SetSkillActive`, `GetConversationSkillIDs`, `SetConversationSkill` / `GetAppSetting`, `SetAppSetting`, `GetAllAppSettings`, `GetTimeoutSetting` / `GetAgentLoopConfig`, `ResetAgentLoopConfig`, `SetAgentLoopConfig` / `GetDiscussionDefaults`, `SetDiscussionDefault` |
| **Inbound** | `discussion_engine.go` (skills content injected into prompt, timeout setting), `agentic_loop.go` (agent loop config for tool definitions), `app.go` (settings + skills UI bindings), `headless_handlers.go` |
| **Outbound** | `models.DB` (~10 + ~7 + ~4 + ~2 direct SQL calls) |

**Current coupling: MEDIUM.** Scattered across 4 files but all follow the
same key-value/CRUD pattern. The engine only needs 3 things from this cluster:
`GetEnabledSkillsContent()`, `GetAgentLoopConfig()`, `GetTimeoutSetting()`.

**To black-box:**
- Define a `ConfigurationProvider` interface:
  ```go
  type ConfigurationProvider interface {
      GetEnabledSkillsContent(conversationID uint) (string, error)
      GetAgentLoopConfig() (*models.AgentLoopConfig, error)
      GetTimeoutSetting(key string, defaultSeconds int) int
  }
  ```
- Three methods cover everything the engine needs. The full CRUD for skills,
  settings, defaults, and config stays in the main app layer.

**Difficulty: Low.** The engine's surface is tiny — 3 methods.

---

### 6. LLM Client (`llm_client.go` + 4 providers)

| | |
|---|---|
| **Files** | `llm_client.go` (197 lines — interface + factory), `llm_openai.go` (576 lines), `llm_anthropic.go` (388 lines), `llm_ollama.go` (344 lines), `llm_local.go` (228 lines), `llm_tester.go` (connection testing) |
| **Lines** | ~1,800 across all files |
| **What it does** | Defines the `LLMClient` interface with 4 methods. Implements it for OpenAI, Anthropic, Ollama, and custom/local endpoints. Handles prompting, tool calling, streaming, response parsing, and provider-specific quirks (message format translation, token counting, API key handling). |
| **Key interface** | ```go
type LLMClient interface {
    ChatCompletion(ctx, messages) (string, error)
    ChatCompletionWithPayload(ctx, messages) (content, reqJSON, respJSON string, err error)
    ChatCompletionWithTools(ctx, messages, tools) (*ChatMessage, reqJSON, respJSON string, error)
    ChatCompletionWithToolsStreaming(ctx, messages, tools, onEvent) (*ChatMessage, reqJSON, respJSON string, error)
}
``` |
| **Key types** | `ChatMessage`, `Tool`, `ToolCall`, `FunctionDef`, `StreamEvent`, `StreamEventType` |
| **Factory** | `NewLLMClient(provider *models.LLMProvider) (LLMClient, error)` — switches on `provider.Provider` string. |
| **Inbound** | `discussion_engine.go` (creates client, passes to agentic loop), `agentic_loop.go` (calls `ChatCompletionWithTools` / `ChatCompletionWithToolsStreaming`), `app.go` (`TestLLMProviderConnection`), `llm_tester.go` |
| **Outbound** | `models.LLMProvider` (for config), HTTP clients to external APIs, `context.Context` for cancellation |

**Current coupling: LOW (already the best boundary in the app).**
The `LLMClient` interface is well-defined, implemented by 4 concrete types,
and the factory (`NewLLMClient`) cleanly separates construction from use.
The only coupling is `*models.LLMProvider` as the config carrier — but this
is a simple struct, not a DB dependency.

**To black-box:**
- This is already a black box from the engine's perspective. The engine sees
  only the `LLMClient` interface.
- To *fully* isolate for testing: replace `*models.LLMProvider` with a
  provider-agnostic config struct (`type LLMConfig struct { ... }`). The
  factory would remain in the app layer; the engine would receive an already-
  constructed `LLMClient`.
- A `MockLLMClient` implementing the interface with scripted tool-call
  responses is the single most valuable testing tool in the entire app.

**Difficulty: Trivial.** No architectural changes needed — the interface
already exists. Just don't pass `*models.LLMProvider` into the engine; pass
a pre-built `LLMClient` instead. The engine already does this (line ~147 of
`discussion_engine.go`).

---

### 7. SQL Execution & Safety (`sql_execution.go`)

| | |
|---|---|
| **File** | `sql_execution.go` (875 lines) |
| **What it does** | Executes SQL against user data sources. Enforces the Data Source Read-Only Invariant (`validateReadOnlySQL`). Applies exploration complexity limits (`validateExplorationQuery`). Manages row limits, automatic LIMIT insertion, error classification, result rendering to HTML (tables with sort/search), markdown rendering, JSON result formatting for LLM feedback. |
| **Key functions** | `executeSQL(conn, sqlQuery) (*QueryResult, error)`, `executeSQLWithMode(conn, sqlQuery, isExploration) (*QueryResult, error)`, `validateReadOnlySQL(sqlQuery) error`, `validateExplorationQuery(sqlQuery, mode) error`, `renderToHTML(columns, rows) string`, `formatSQLResultsForLLM(json) string`, `classifyErrorCategory(err) string` |
| **Key types** | `QueryResult` (Columns, Rows, RowCount), `ExplorationSafetyMode` (strict/moderate/relaxed) |
| **Inbound** | `agentic_loop.go` (calls `executeSQLWithMode` for every `query_database` tool call), `app.go` (manual `ExecuteQuery` binding) |
| **Outbound** | `db_registry.go` (driver lookup), `db_driver.go` (DBDriver / NativeQuerier interfaces), `models.DataSource` (connection config), raw `database/sql` and driver-specific packages |

**Current coupling: MEDIUM.** The `DBDriver` + `NativeQuerier` interfaces
already abstract the driver layer. The main coupling is to `*models.DataSource`
(for DSN building and config parsing). The result rendering functions
(`renderToHTML`, `formatSQLResultsForLLM`) are pure functions with no external
dependencies — already perfectly isolated. The HTML rendering is large (600+
lines of string building) but self-contained.

**To black-box:**
- Define a `QueryExecutor` interface:
  ```go
  type QueryExecutor interface {
      Execute(sql string, isExploration bool) (*QueryResult, error)
  }
  ```
- The current `executeSQLWithMode` already takes `conn *models.DataSource` —
  to fully decouple, the executor should own its connection internally
  (constructed with a driver + DSN at setup time, not looked up per-call).
- The safety functions (`validateReadOnlySQL`, `validateExplorationQuery`)
  are pure and should be testable independently (they already are — 5 tests
  in `sql_execution_test.go`).
- HTML rendering is also pure — no dependencies.
- The main extraction work: separating execution (needs driver+DSN) from
  validation (pure) from rendering (pure). Currently they're in one file.

**⚠️ Important nuance for extraction (do not lose this):** `validateReadOnlySQL`
and `validateExplorationQuery` are called from **two different call sites
today**, not one:
  - `validateReadOnlySQL` is called *inside* `executeSQLWithMode`
    (`sql_execution.go` line ~197) — it runs for every query, exploration or
    final, unconditionally. This is the read-only invariant's single choke
    point per `AGENT_READ_FIRST.md` §0.
  - `validateExplorationQuery` (which itself calls `validateReadOnlySQL` plus
    the complexity-mode check) is called *by the caller of* `executeSQLWithMode`
    — specifically `agentic_loop.go` line ~1282, **before** `executeSQLWithMode`
    is invoked, and only when `is_exploration: true`. It is NOT called from
    inside `sql_execution.go` at all.

  A `QueryExecutor` interface that only wraps `executeSQLWithMode` therefore
  captures the read-only check but **not** the exploration complexity-mode
  check — that check would need an explicit new call inside the
  `QueryExecutor` implementation (or `executeSQLWithMode` itself), sourced
  from the data source's configured `ExplorationSafetyMode`. See
  `FUNCTIONALITY_SILO_PLAN.md`'s Risk Register ("Exploration safety mode
  enforcement gap") for the concrete fix. This is a §1.4 safety control, not
  the §0 absolute rule — but it must not be silently dropped in extraction.

**Difficulty: Low-Medium.** The functional boundaries are already clear within
the file. The work is separating them into distinct types/packages, not
rewriting logic.

---

### 8. The Agentic Loop (`agentic_loop.go`)

| | |
|---|---|
| **File** | `agentic_loop.go` (1,622 lines — the largest single file) |
| **What it does** | The multi-round, tool-calling LLM loop. Receives conversation context + schema + LLM client. Builds tools (`query_database`, `respond_to_user`, `render_chart`). Runs rounds: sends messages to LLM, parses tool calls, executes queries via `executeSQLWithMode`, feeds results back, enforces one-shot finality (once `is_exploration: false` is used, further queries are rejected). Generates summaries, handles charts, writes conversation messages. |
| **Key functions** | `runAgenticLoop(ctx, query, client, messages, dbConnection, schema, conversation, maxRounds, maxTools, maxRetries, safetyMode, contextWindow, userMessage, skillsContent, onStream) error`, `buildTools()`, `buildToolLlmMessages()`, `buildToolSystemPrompt()`, `buildCompactSystemPrompt()`, `handleRespond()`, `renderToolQueryResults()`, `buildTranscript()`, `storeTechDetail()` |
| **Key types** | `Tool`, `ToolCallSummary`, `ToolTranscript`, `TranscriptAction`, `TechDetail`, `pendingFinalResult`, `roundKind` |
| **Inbound** | `discussion_engine.go` (calls `runAgenticLoop` with all params) |
| **Outbound** | `LLMClient` (interface — sends messages, receives responses), `sql_execution.go` (`executeSQLWithMode`), `conversation.go` (reads messages, writes assistant messages), `llm_provider.go` (model name for prom), `db_connection.go` (dialect hints for prompts), `skill_service.go` (skills content), `agent_loop_config.go` (tool definitions), `models.DB` (2 direct SQL calls for transcript storage), `discussion_engine.go` (circular: calls `handleRespond`, `renderSQLError`, `resolveChartConfig`) |

**Current coupling: HIGH — this is the core of the monolith.**
The loop directly calls 7+ service files, makes direct `models.DB` calls,
and has circular dependencies back into `discussion_engine.go`. Its signature
takes 14 parameters, 5 of which are `*models.*` structs. It's the most
complex and most coupled function in the app.

**Parameter breakdown of `runAgenticLoop`:**
| Parameter | Type | Source |
|---|---|---|
| `ctx` | `context.Context` | caller (cancellation support) |
| `query` | `*models.Query` | DB (`CreateQuery`) |
| `client` | `LLMClient` | factory (already interface!) |
| `messages` | `[]ChatMessage` | `buildToolLlmMessages` |
| `dbConnection` | `*models.DataSource` | DB |
| `schema` | `*DataSchema` | `GetDataSchema` |
| `conversation` | `*models.Conversation` | DB |
| `maxExplorationRounds` | `int` | data source config |
| `maxToolsPerRound` | `int` | data source config |
| `maxErrorRetries` | `int` | data source config |
| `safetyMode` | `ExplorationSafetyMode` | data source config |
| `contextWindow` | `*int` | provider config |
| `userMessage` | `string` | caller |
| `skillsContent` | `string` | `GetEnabledSkillsContent` |
| `onStream` | `func(StreamEvent)` | caller |

**To black-box:**
This is the highest-value extraction. The loop should become:

```go
type AgenticLoop struct {
    LLMClient       LLMClient        // how to talk to the model
    QueryExecutor   QueryExecutor    // how to run SQL
    OutputHandler   OutputHandler    // how to persist results (see below)
    Config          LoopConfig       // self-contained config (no models.*)
}
func (a *AgenticLoop) Run(ctx context.Context, input LoopInput) (*LoopOutput, error)
```

Where:
- `LoopInput` carries: user message, schema, conversation history (messages), skills content, streaming callback
- `LoopOutput` carries: the final assistant message (text + rendered HTML + SQL results + chart config + transcript)
- `OutputHandler` is a callback interface for persisting results (the 2 `models.DB` calls + message creation become `OutputHandler` methods)
- `LoopConfig` is a flat config struct (not `*models.AgentLoopConfig`) — max rounds, max tools, max retries, safety mode, context window, viz enabled

The circular calls to `discussion_engine.go` (`handleRespond`, `renderSQLError`,
`resolveChartConfig`) get absorbed into `OutputHandler` or moved into the
loop package as internal helpers.

**Why this is worth it:**
- The loop becomes testable with a `MockLLMClient` that returns scripted
  tool-call sequences. You can test: "given this schema + this user message,
  does the model explore correctly? does it respect one-shot finality? does
  the error retry path work?"
- You can swap tool definitions, safety limits, and system prompts without
  touching any other part of the app.
- The loop no longer knows about `models.DB`, `models.Query`, or
  `models.Conversation` — it works with generic input/output types.

**Difficulty: HIGH.** This is the biggest refactor. It requires:
1. Defining `OutputHandler` interface (replaces 3+ circular calls + 2 direct `models.DB` calls)
2. Defining `LoopConfig` struct (replaces 7 scattered parameters)
3. Defining `LoopInput` / `LoopOutput` types
4. Moving `handleRespond`, `renderToolQueryResults`, `renderSQLError`, `resolveChartConfig` into the loop package
5. Updating `discussion_engine.go` to construct and call the loop
6. Ensuring the streaming callback still works transparently

But the reward is enormous: the loop becomes the "highly configurable black
box" you described — prompt templates, tool definitions, safety limits, and
round behavior all configurable and testable in isolation.

---

### 9. Discussion Engine (`discussion_engine.go`)

| | |
|---|---|
| **File** | `discussion_engine.go` (886 lines) |
| **What it does** | The orchestrator. Glues everything together: loads conversation + provider + data source from DB, fetches schema, builds LLM messages, creates the LLM client, runs the agentic loop, handles errors, triggers summarization, formats final output. |
| **Key function** | `ProcessUserMessageWithContext(ctx, conversationID, userMessage, onPhase, onStream) error` |
| **Inbound** | `app.go` (`ProcessUserMessage` binding — line 123), `headless_handlers.go` |
| **Outbound** | Everything: conversation CRUD, provider CRUD, data source CRUD, schema introspection, skills, settings, agent loop config, LLM client factory, agentic loop |
| **Other functions in the file** | Summarization (`summarizeResults`), error rendering (`renderSQLError`), HTML stripping (`stripHTMLTags`), result formatting (`formatSQLResultsForLLM`, `formatRowsTable`), chart config resolution (`resolveChartConfig`), exploration result formatting (`formatResultsDigestForSummarization`, `computeColumnStats`) |

**Current coupling: HIGH — the orchestrator touches everything.**
It calls 22 distinct functions across 10+ service files. It directly accesses
`models.DB` (line 117). It's the bridge between the frontend and the engine.

**To black-box:**
Once all the other sections are behind interfaces, the discussion engine
becomes a thin orchestrator:

```go
func (d *DiscussionEngine) ProcessMessage(ctx context.Context, conversationID uint, message string, onStream func(StreamEvent)) error {
    conv, _     := d.Conversations.GetByID(conversationID)
    history, _  := d.Conversations.GetMessages(conversationID)
    provider, _ := d.Providers.GetByID(conv.LLMProviderID)
    ds, _       := d.DataSources.GetByID(conv.DataSourceID)
    schema, _   := d.Introspector.GetSchema(ds)
    skills, _   := d.Config.GetEnabledSkillsContent(conv.ID)
    loopConfig  := d.buildLoopConfig(conv, ds, provider)
    client, _   := llm.NewClient(provider) // already interface

    d.Conversations.CreateMessage(conversationID, "user", message, ...)

    input := LoopInput{...} // built from history + schema + skills + message
    output, err := d.Loop.Run(ctx, input)

    if output.FinalMessage != nil {
        d.Conversations.CreateMessage(conversationID, "assistant", output.FinalMessage.Content, ...)
    }
    return err
}
```

At ~50 lines, fully testable, and replaceable.

**Difficulty: MEDIUM (depends on other sections being extracted first).**
The engine itself is straightforward — the complexity is in everything it
calls. Once those dependencies are behind interfaces, the engine collapses
to a simple pipeline.

---

### 10. Result Rendering & Export

| | |
|---|---|
| **Files** | `discussion_engine.go` (rendering functions), `sql_execution.go` (HTML table rendering), `export.go` (707 lines), `total_export.go` (450 lines) |
| **What it does** | Converts query results into user-facing output: rendered HTML tables (sortable, searchable), plain-English summaries (via LLM), charts (via `render_chart` tool), and static file exports (HTML, Markdown, PDF). Also the "tech details" toggle (SQL, timing, token counts). |
| **Key functions** | `renderToHTML`, `summarizeResults`, `resolveChartConfig`, `renderSQLError`, `renderToolQueryResults`, `BuildConversationHTML`, `BuildConversationMarkdown`, `ExportConversationPDF` |
| **Inbound** | `agentic_loop.go` (result rendering), `discussion_engine.go` (summarization, chart config), `app.go` (export bindings) |
| **Outbound** | `LLMClient` (for summarization), `models.*` (for export), filesystem (for file exports) |

**Current coupling: LOW-MEDIUM.**
The rendering functions are already close to pure — they take data in, return
strings. The HTML table renderer in `sql_execution.go` is 600+ lines of string
building with zero external dependencies. Summarization uses `LLMClient` (which
is already an interface). Export uses `models.DB` directly (to dump all tables).

**To black-box:**
- Extract rendering into a `ResultRenderer` interface:
  ```go
  type ResultRenderer interface {
      RenderTable(columns []string, rows [][]interface{}) string
      RenderError(query string, err error, retryable bool) string
  }
  ```
- Extract export into an `Exporter` interface:
  ```go
  type Exporter interface {
      ExportHTML(conversation) string
      ExportMarkdown(conversation) string
      ExportPDF(conversation) []byte
  }
  ```
- Summarization is already clean — it takes `LLMClient` as a parameter.
- Chart resolution (`resolveChartConfig`) is a pure function of JSON config
  + column names + data — no external dependencies.

**Difficulty: Low.** The rendering functions are already self-contained
within their files. The work is naming them as a distinct concern.

---

### 11. Database Drivers (`db_driver.go` + 9 driver files + file drivers)

| | |
|---|---|
| **Files** | `db_driver.go` (35 lines — interfaces), `db_registry.go` (54 lines — registration), `db_mysql.go`, `db_postgres.go`, `db_sqlite.go`, `db_sqlserver.go`, `db_mariadb.go`, `db_snowflake.go`, `db_bigquery.go`, `db_redshift.go`, `db_google_sheets.go`, `data_file.go` (363 lines — CSV + Excel via SQLite import) |
| **Lines** | ~2,200 across all driver files |
| **What they do** | Each driver implements `DBDriver` (TypeKey, OpenDriver, BuildDSN, GetSchema, DisplayName, SQLDialectHint, DefaultPort). BigQuery and Google Sheets also implement `NativeQuerier` (bypasses `database/sql`). CSV/Excel files are imported into temporary SQLite databases and queried via the SQLite driver. |
| **Interfaces** | `DBDriver` (7 methods), `NativeQuerier` (3 methods) |
| **Inbound** | `sql_execution.go` (`executeSQLWithMode` → driver lookup → `BuildDSN` + `sql.Open`), `database_introspection.go` (`GetDataSchema` → driver lookup → `GetSchema`), `app.go` (connection testing) |
| **Outbound** | `database/sql` + driver-specific Go packages (`pgx`, `go-sql-driver/mysql`, `microsoft/go-mssqldb`, etc.), BigQuery/Sheets SDKs, `models.DataSource` |

**Current coupling: LOW (already well-abstracted).**
The `DBDriver` interface is clean. The registry pattern (`RegisterDriver` in
`init()`) is standard and effective. Each driver is in its own file. The
`NativeQuerier` extension interface is composable. This is the best-architected
part of the services package.

**To black-box:**
- Already a black box. The consumer (`sql_execution.go`) sees only the
  `DBDriver` / `NativeQuerier` interfaces.
- One minor improvement: `BuildDSN` takes `*models.DataSource` — replacing
  this with a flat config struct would decouple drivers from the models
  package entirely.

**Difficulty: Trivial.** No changes needed to the interface or pattern.
Drivers already work in isolation.

---

### 12. Frontend + Wails Bindings

| | |
|---|---|
| **Files** | `app.go` (1,318 lines), `main.go`, `frontend/src/App.svelte` (2,009 lines), `frontend/src/ConversationView.svelte` (1,310 lines), `frontend/src/SettingsView.svelte` (3,774 lines), `frontend/src/VizChart.svelte` (127 lines), `frontend/src/variables.css`, `frontend/wailsjs/go/main/App.js` + `App.d.ts` (auto-generated) |
| **Lines** | ~8,500 total |
| **What they do** | The UI layer. `app.go` exposes 50+ Wails-bound methods that the Svelte frontend calls via the auto-generated `App.js` bridge. Manages lifecycle (startup/shutdown), cancellation, file dialogs (PDF export), OAuth flow orchestration. The Svelte components render the chat interface, settings panels, and data visualizations. |
| **Key binding** | `ProcessUserMessage(conversationID uint, userMessage string) error` — called when the user sends a message. Thin wrapper: creates context with cancel, calls `services.ProcessUserMessageWithContext`, emits events. |
| **Inbound** | User interaction (Svelte → Wails → Go) |
| **Outbound** | Every service function, `models.*` types, `runtime.EventsEmit` for async events |

**Current coupling: MEDIUM (by design — Wails bridges are always coupled).**
`app.go` necessarily imports `services` and `models`. It has 107 `services.`
calls. This is normal for a Wails app. The coupling is in the *number* of
bindings (50+), not their complexity — each binding is a thin pass-through
to a service function.

**To black-box:**
- Not applicable in the same way. The frontend is the consumer of everything.
- The relevant question is: can the frontend be tested independently of the
  backend? Yes — the Wails binding layer (`wailsjs/go/main/App.js`) can be
  mocked at the JavaScript level. The Svelte components don't import Go code
  directly; they call `App.ProcessUserMessage(...)` via the bridge.
- The backend can be tested independently of the frontend via the headless
  HTTP API (`headless_handlers.go`).

**Difficulty: N/A.** This is the integration layer, not a silo candidate.

---

### 13. Headless HTTP API (`headless.go`, `headless_handlers.go`)

| | |
|---|---|
| **Files** | `headless.go` (308 lines), `headless_handlers.go` (346 lines) — **currently untracked in git** |
| **Lines** | 654 total |
| **What it does** | Exposes a REST API over HTTP: `POST /api/conversations`, `POST /api/conversations/{id}/messages`, `GET /api/conversations/{id}/messages` (SSE streaming), `GET /api/conversations/{id}/messages/poll`, `DELETE /api/conversations/{id}/messages`, `GET /api/conversations/{id}/export/html`, `GET /api/health`, `PUT /api/settings`, `GET /api/info`. Uses `ProcessUserMessageWithContext` as the engine, same as `app.go`. |
| **Inbound** | External HTTP clients (test harness, CI, automation) |
| **Outbound** | Same service functions as `app.go` — `CreateConversationWithDefaults`, `ProcessUserMessageWithContext`, `GetConversationMessages`, `ExportConversationHTML`, etc. |

**Current coupling: MEDIUM (matches app.go).**
A thin adapter layer. Same dependency depth as `app.go`. The difference is
the transport (HTTP JSON/SSE vs. Wails IPC) and the authentication model
(API keys vs. desktop app trust).

**To black-box:**
- Already a thin adapter. If the underlying services are extracted behind
  interfaces, this handler layer doesn't change — it calls the same
  interfaces.
- The key concern is security: this is a network surface. Auth, rate limiting,
  and input validation should be reviewed before committing.

**Difficulty: Low.** This is already a thin adapter. No extraction needed —
  it's the consumer, not the dependency.

---

### 14. Auto-Updater (`updater.go` + 3 OS-specific files)

| | |
|---|---|
| **Files** | `updater.go` (254 lines), `updater_darwin.go`, `updater_windows.go`, `updater_linux.go` |
| **Lines** | ~500 across all files |
| **What it does** | Checks for new releases on GitHub, downloads the platform-specific binary, verifies SHA256 checksum, and (on user confirmation) replaces the running binary and restarts the app. |
| **Inbound** | `app.go` (`CheckForUpdate`, `DownloadUpdate`, `PerformUpgradeRestart` bindings) |
| **Outbound** | HTTP client (GitHub API + release artifacts), filesystem, `os/exec` (detached scripts) |

**Current coupling: LOW.** Self-contained in its own files. Depends only on
`appVersion` (from `main.go`) and `models.DB` (for persisting "update dismissed"
state). No coupling to the discussion engine or agentic loop.

**To black-box:**
- Already a black box. Three public functions, no internal dependencies on
  the rest of the services package beyond `models.DB` for one bool flag.

**Difficulty: Trivial.** Already isolated.

---

### 15. SQLite DB Switcher (`db_switcher.go`)

| | |
|---|---|
| **File** | `db_switcher.go` (141 lines) — **currently untracked in git** |
| **What it does** | Allows swapping the app's own SQLite database (`~/.yourql/yourql.db`) to a different file. Writes a pointer file, validates the target, provides `GetActiveDatabaseInfo` for the settings UI. |
| **Inbound** | `app.go` (settings binding), `headless_handlers.go` |
| **Outbound** | `models.DB`, `models.ActiveDBPointer`, `models.DefaultDBPath`, filesystem |

**Current coupling: LOW.** Self-contained. Only touches model-level path
functions and the filesystem.

**To black-box:**
- Already isolated. Three functions, no engine dependencies.

**Difficulty: Trivial.**

---

## Dependency Map (Current)

```
                          ┌──────────────────────────┐
                          │  main.go (entry point)   │
                          └─────────┬────────────────┘
                                    │
                ┌───────────────────┼───────────────────┐
                ▼                   ▼                   ▼
    ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────┐
    │  app.go         │  │ headless.go +    │  │ models.          │
    │  (50+ Wails     │  │ handlers         │  │ ConnectDatabase  │
    │   bindings)     │  │ (HTTP API)       │  │ (startup)        │
    └───────┬─────────┘  └────────┬─────────┘  └────────┬────────┘
            │                     │                      │
            └──────────┬──────────┘                      │
                       │                                 │
                       ▼                                 ▼
    ┌──────────────────────────────────────────────────────────────┐
    │  pkg/services/  (single Go package — 54 files)              │
    │                                                              │
    │  ┌─────────────────────┐   ┌──────────────────────────────┐ │
    │  │  discussion_engine  │──▶│  conversation.go             │ │
    │  │  .go (orchestrator) │   │  llm_provider.go             │ │
    │  └─────────┬───────────┘   │  db_connection.go            │ │
    │            │               │  query.go                    │ │
    │            │               │  skill_service.go            │ │
    │            ▼               │  app_settings.go             │ │
    │  ┌─────────────────────┐   │  agent_loop_config.go        │ │
    │  │  agentic_loop.go    │   │  discussion_defaults.go      │ │
    │  │  (core loop)        │   └──────────────┬───────────────┘ │
    │  └─────────┬───────────┘                   │                 │
    │            │                               │                 │
    │            ▼                               ▼                 │
    │  ┌─────────────────────┐   ┌──────────────────────────────┐ │
    │  │  sql_execution.go   │   │  models.DB  (global *sql.DB) │ │
    │  │  (safety + exec)    │   │  ~/.yourql/yourql.db         │ │
    │  └─────────┬───────────┘   └──────────────────────────────┘ │
    │            │                                                 │
    │            ▼                                                 │
    │  ┌─────────────────────┐                                     │
    │  │  db_driver.go (iface)│◀── 9 registered drivers            │
    │  │  llm_client.go(iface)│◀── 4 LLM providers                 │
    │  └─────────────────────┘                                     │
    │                                                              │
    │  ┌─────────────────────┐   ┌──────────────────────────────┐ │
    │  │  updater.go         │   │  export.go / total_export.go │ │
    │  │  db_switcher.go     │   │  data_file.go                │ │
    │  │  (isolated)         │   │  google_auth.go              │ │
    │  └─────────────────────┘   └──────────────────────────────┘ │
    └──────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
    ┌──────────────────────────────────────────────────────────────┐
    │  frontend/src/  (Svelte 5)                                   │
    │  App.svelte, ConversationView.svelte, SettingsView.svelte,   │
    │  VizChart.svelte                                             │
    │  ↔  frontend/wailsjs/go/main/App.js  (auto-generated bridge) │
    └──────────────────────────────────────────────────────────────┘
```

---

## Target Architecture (After Isolation)

```
   app.go / headless ──┬──▶ DiscussionEngine (orchestrator)
                       │         │
                       │         ├──▶ ConversationStore (interface)
                       │         ├──▶ LLMProviderStore (interface)
                       │         ├──▶ DataSourceStore (interface)
                       │         ├──▶ SchemaIntrospector (interface)
                       │         ├──▶ ConfigurationProvider (interface)
                       │         ├──▶ LLMClient (interface — already exists)
                       │         └──▶ AgenticLoop (black box)
                       │                    │
                       │                    ├──▶ LLMClient (interface)
                       │                    ├──▶ QueryExecutor (interface)
                       │                    ├──▶ OutputHandler (interface)
                       │                    └──▶ LoopConfig (value struct)
                       │
                       ├──▶ SQLExecutor (interface — for manual queries)
                       ├──▶ Exporter (interface)
                       └──▶ Updater + DBSwitcher (already isolated)

   models/     ── stores implement the interfaces above (SQLite-backed)
   drivers/    ── implement DBDriver + NativeQuerier (already interfaces)
   llm/        ── implement LLMClient (already interface)
   frontend/   ── unchanged (already isolated via Wails bridge + headless API)
```

---

## Extraction Order (Recommended)

Each step builds on the previous one. Steps 1–4 are low-risk and can ship
incrementally. Steps 5–6 are the high-value core extraction.

| Step | What | Difficulty | Value |
|---|---|---|---|
| **1. Define interfaces** | Create `ConversationStore`, `LLMProviderStore`, `DataSourceStore`, `ConfigurationProvider`, `SchemaIntrospector` interfaces in a new `pkg/engine/ports.go` file. These are just type definitions — no implementation changes. | Trivial | Sets the contract |
| **2. Implement adapters** | Write thin adapter structs in `pkg/services/` that wrap the existing CRUD functions to satisfy the new interfaces. The existing code doesn't change — the adapters just delegate. | Low | Proves the interfaces work |
| **3. Extract loop config** | Create `LoopConfig` (flat value struct) and `LoopInput`/`LoopOutput` types. Update `discussion_engine.go` to build a `LoopConfig` from the scattered `*models.*` params and pass it to the loop. | Low | Reduces parameter count from 14 to 4 |
| **4. Define OutputHandler** | Create `OutputHandler` interface with methods for persisting assistant messages, storing transcripts, and updating query records. Move the `handleRespond`/`renderToolQueryResults` logic behind it. Break the circular `discussion_engine.go` ↔ `agentic_loop.go` dependency. | Medium | Clean separation of loop logic from persistence |
| **5. Extract AgenticLoop** | Move `agentic_loop.go` + tool definitions + result rendering + chart resolution into `pkg/engine/loop/`. It depends only on interfaces: `LLMClient`, `QueryExecutor`, `OutputHandler`. No `models.*` imports. | High | The crown jewel — testable loop |
| **6. Extract DiscussionEngine** | With all dependencies behind interfaces, the orchestrator collapses to ~50 lines. Move to `pkg/engine/orchestrator.go`. | Medium | Final decoupling |
| **7. Add tests** | With the loop isolated: table-driven tests with `MockLLMClient` (scripted tool calls), `MockQueryExecutor` (predefined results), `MockOutputHandler` (records what was persisted). Test exploration strategies, error retry, one-shot finality, chart rendering, streaming. | Ongoing | Catches regressions before they ship |

---

## Configurability Upside

Once the loop is isolated, "heavily configurable" becomes straightforward:

**Per-data-source loop behavior:**
- `LoopConfig.MaxExplorationRounds` — already exists (data source config)
- `LoopConfig.MaxToolsPerRound` — already exists
- `LoopConfig.MaxErrorRetries` — already exists
- `LoopConfig.SafetyMode` — already exists (strict/moderate/relaxed)

**Per-provider tuning:**
- `LoopConfig.ContextWindow` — already passed from provider config
- `LoopConfig.Temperature`, `TopP`, `MaxTokens` — could be added to provider config
- `LoopConfig.ThinkingBudget` — for Claude/OpenAI reasoning models

**Tool definitions (already configurable via `agent_loop_config.go`):**
- Custom `query_database` description (guide the model's exploration strategy)
- Custom `respond_to_user` description (guide answer style)
- Custom `render_chart` description (guide visualization choices)
- Potentially: allow adding/removing tools entirely (e.g., disable charts,
  add a `search_web` tool, etc.)

**Prompt templates:**
- System prompt — already overridable per deployment via `agent_loop_config`
- Compact prompt (used when context is tight) — currently hardcoded in
  `buildCompactSystemPrompt`
- Exploration hints, error recovery strategies, summarization instructions —
  all could become template strings with variable substitution

**This is the vision:** a `LoopConfig` that can be serialized as JSON, stored
in `agent_loop_config` or `data_sources.config`, and hot-reloaded without
restarting the app. The loop becomes a pure function of its config + inputs
+ interfaces — no global state, no `models.DB`, no file I/O.

---

## Summary Table

| Section | Files | Lines | Coupling | Black-Box Difficulty |
|---|---|---|---|---|
| Data Models & Persistence | `pkg/models/*.go` | ~1,200 | HIGH | Low |
| Conversation CRUD | `conversation.go` | 475 | HIGH | Low |
| Provider & Data Source CRUD | `llm_provider.go`, `db_connection.go` | 676 | HIGH | Low |
| Schema Introspection | `database_introspection.go` | 58 | LOW | Trivial |
| Skills & Settings | 4 files | 577 | MEDIUM | Low |
| LLM Client | `llm_client.go` + 4 providers | ~1,800 | LOW | Trivial |
| SQL Execution & Safety | `sql_execution.go` | 875 | MEDIUM | Low-Medium |
| **Agentic Loop** | **`agentic_loop.go`** | **1,622** | **HIGH** | **HIGH** |
| Discussion Engine | `discussion_engine.go` | 886 | HIGH | Medium |
| Result Rendering & Export | 4 files | ~1,700 | LOW-MEDIUM | Low |
| Database Drivers | 12 files | ~2,200 | LOW | Trivial |
| Frontend + Bindings | `app.go` + 4 Svelte files | ~8,500 | MEDIUM | N/A |
| Headless API | `headless.go` + handlers | 654 | MEDIUM | Low |
| Auto-Updater | `updater*.go` | ~500 | LOW | Trivial |
| DB Switcher | `db_switcher.go` | 141 | LOW | Trivial |

---

## Key Takeaway

The project has **two clean interfaces** (LLMClient, DBDriver) and **zero
interfaces for the data access layer**. Creating those interfaces is
mechanical — the real work is the **agentic loop extraction**, which
requires breaking circular dependencies and defining the `OutputHandler`
contract. That extraction is the single highest-leverage change: it would
let you iterate on prompt engineering, tool definitions, safety parameters,
and loop behavior in complete isolation, with fast mocked tests, no real
database, and no LLM API key.

The existing LLM provider isolation (4 implementations behind one interface)
is proof that this pattern works. The agentic loop should get the same
treatment.