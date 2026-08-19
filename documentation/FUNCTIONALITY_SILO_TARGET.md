# YourQL — Functionality Silo Target Architecture

> This document describes the end state: what the codebase looks like after
> every logical section in `FUNCTIONALITY_SILO_DEFINITIONS.md` has been
> isolated behind an interface. It defines contracts, package boundaries,
> and the wiring diagram. For the step-by-step plan to get here, see
> `FUNCTIONALITY_SILO_PLAN.md`.

---

## Goal

The agentic loop becomes a black-box `AgenticLoop` struct that depends only on
three injected interfaces. It has zero knowledge of `models.DB`, Wails, Svelte,
HTTP handlers, or the filesystem. It can be instantiated, configured, and tested
in complete isolation with mock implementations.

The discussion engine becomes a thin orchestrator (~50 lines) that wires the
loop to the app's persistence layer through interface adapters. The engine is
likewise testable with mocked stores.

Every remaining `pkg/services/` file either implements one of these interfaces
or is a self-contained service (updater, export, OAuth).

---

## Package Layout (End State)

```
YourQL/
├── main.go                          # unchanged: Wails lifecycle, appVersion
├── app.go                           # thin: ~50% fewer service.* calls; uses
│                                    #   orchestrator + adapters instead
├── headless.go / headless_handlers.go # thin: same pattern as app.go
│
├── pkg/
│   ├── models/                      # unchanged: structs, DB, migrations
│   │   ├── database.go              #   models.DB, ConnectDatabase, migrations
│   │   ├── conversation.go          #   Conversation struct
│   │   ├── conversation_message.go  #   ConversationMessage struct
│   │   ├── query.go                 #   Query struct
│   │   ├── db_connection.go         #   DataSource, DataSourceConfig structs
│   │   ├── llm_provider.go          #   LLMProvider struct
│   │   ├── skill.go                 #   Skill struct
│   │   ├── agent_loop_config.go     #   AgentLoopConfig struct
│   │   ├── app_setting.go           #   AppSetting struct
│   │   └── discussion_default.go    #   DiscussionDefault struct
│   │
│   ├── engine/                      # NEW: the black-boxed engine
│   │   ├── ports.go                 #   all interfaces (see §Ports below)
│   │   ├── types.go                 #   LoopInput, LoopOutput, LoopConfig,
│   │   │                            #   QueryResult, DataSchema, etc.
│   │   ├── safety.go                #   validateReadOnlySQL,
│   │   │                            #   validateExplorationQuery,
│   │   │                            #   ExplorationSafetyMode (moved from
│   │   │                            #   sql_execution.go — pure functions)
│   │   ├── rendering.go             #   renderToHTML, renderMarkdown,
│   │   │                            #   renderSQLErrorHTML,
│   │   │                            #   formatSQLResultsForLLM,
│   │   │                            #   formatExplorationHTML (moved from
│   │   │                            #   sql_execution.go + discussion_engine.go
│   │   │                            #   — pure functions)
│   │   ├── tools.go                 #   buildTools, Tool, FunctionDef,
│   │   │                            #   ToolCallSummary, ToolTranscript,
│   │   │                            #   TranscriptAction, TechDetail
│   │   │                            #   (moved from agentic_loop.go)
│   │   ├── prompts.go               #   buildToolSystemPrompt,
│   │   │                            #   buildCompactSystemPrompt,
│   │   │                            #   buildToolLlmMessages
│   │   │                            #   (moved from agentic_loop.go)
│   │   ├── loop.go                  #   AgenticLoop struct + Run method
│   │   │                            #   (moved from agentic_loop.go)
│   │   ├── response.go              #   handleRespond, renderToolQueryResults
│   │   │                            #   (moved from agentic_loop.go)
│   │   └── charts.go                #   resolveChartConfig, zipScatterPoints,
│   │                                #   resolveRefs, normalizeColRef
│   │                                #   (moved from discussion_engine.go —
│   │                                #   pure functions)
│   │
│   ├── services/                    # SHRUNK: adapters + isolated services
│   │   ├── adapters/                #   NEW: SQLite-backed impls of engine
│   │   │   │                       #     ports
│   │   │   ├── conversation_store.go #   implements
│   │   │   │                       #   engine.ConversationStore
│   │   │   ├── provider_store.go    #   implements engine.LLMProviderStore
│   │   │   ├── datasource_store.go  #   implements
│   │   │   │                       #   engine.DataSourceStore
│   │   │   ├── query_recorder.go    #   implements engine.QueryRecorder
│   │   │   ├── config_provider.go   #   implements
│   │   │   │                       #   engine.ConfigurationProvider
│   │   │   ├── output_handler.go    #   implements engine.OutputHandler
│   │   │   └── query_executor.go    #   implements engine.QueryExecutor
│   │   │
│   │   ├── conversation.go          #   unchanged: full CRUD (app.go uses
│   │   │                            #   this; adapters delegate to it)
│   │   ├── llm_provider.go          #   unchanged: full CRUD
│   │   ├── db_connection.go         #   unchanged: full CRUD
│   │   ├── query.go                 #   unchanged: tracking CRUD
│   │   ├── skill_service.go         #   unchanged: skills CRUD
│   │   ├── app_settings.go          #   unchanged: settings CRUD
│   │   ├── agent_loop_config.go     #   unchanged: loop config CRUD
│   │   ├── discussion_defaults.go   #   unchanged: defaults CRUD
│   │   ├── database_introspection.go#   unchanged (already thin)
│   │   ├── sql_execution.go         #   shrunk: executeSQLWithMode stays;
│   │   │                            #   rendering + validation moved to
│   │   │                            #   engine/
│   │   ├── llm_client.go            #   unchanged: LLMClient interface +
│   │   │                            #   NewLLMClient factory
│   │   ├── llm_openai.go            #   unchanged
│   │   ├── llm_anthropic.go         #   unchanged
│   │   ├── llm_ollama.go            #   unchanged
│   │   ├── llm_local.go             #   unchanged
│   │   ├── llm_tester.go            #   unchanged
│   │   ├── db_driver.go             #   unchanged: DBDriver + NativeQuerier
│   │   ├── db_registry.go           #   unchanged
│   │   ├── db_*.go                  #   unchanged: 9+ driver files
│   │   ├── data_file.go             #   unchanged
│   │   ├── google_auth.go           #   unchanged
│   │   ├── export.go                #   unchanged
│   │   ├── total_export.go          #   unchanged
│   │   ├── updater.go               #   unchanged
│   │   ├── updater_*.go             #   unchanged
│   │   ├── db_switcher.go           #   unchanged
│   │   ├── discussion_engine.go     #   DELETED (moved to
│   │   │                            #   engine/orchestrator.go)
│   │   └── agentic_loop.go          #   DELETED (moved to engine/loop.go
│   │                                #   + tools.go + prompts.go +
│   │                                #   response.go)
│   │
│   └── ...                          # (no other packages affected)

frontend/                            # UNCHANGED: all Svelte components,
                                     #   Wails bridge, CSS
```

---

## Ports (Interface Contracts)

All interfaces live in `pkg/engine/ports.go`. Every interface name ends in
`-er` or `-or` to distinguish engine ports from implementation structs.

### LLMClient (already exists, re-exported)

```go
package engine

import "context"

// LLMClient is the interface for communicating with an LLM provider.
// This is re-exported from pkg/services/llm_client.go. The engine package
// does NOT import services — services imports engine and satisfies the
// interface. To break the import cycle, the interface definition moves to
// engine/ports.go and services/types get a type alias.
type LLMClient interface {
    ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
    ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
    ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error)
    ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onStream func(StreamEvent)) (*ChatMessage, string, string, error)
}

// NOTE: ChatMessage, Tool, ToolCall, FunctionDef, StreamEvent, StreamEventType
// also move to engine/types.go. services/ gets type aliases pointing to engine.
```

### QueryExecutor

```go
// QueryExecutor runs SQL against a user's data source. It encapsulates
// connection management (DSN, pooling, driver selection) and enforces
// read-only safety internally. The engine only sees this interface.
type QueryExecutor interface {
    // Execute runs a query against the configured data source.
    // isExploration controls whether exploration complexity limits apply.
    // Returns QueryResult on success. Errors include safety rejections
    // (read-only violation, complexity limits) and driver errors.
    //
    // SAFETY CONTRACT (AGENT_READ_FIRST.md §0, §1.4 — non-negotiable):
    // every call to Execute, regardless of isExploration, MUST pass through
    // ValidateReadOnlySQL (engine/safety.go) before reaching a driver. In
    // addition, when isExploration is true, Execute MUST also pass through
    // ValidateExplorationQuery(sql, mode) — the complexity-mode gate
    // (strict/moderate/relaxed) — using the SafetyMode configured for the
    // data source. Today these are two separate checks at two separate
    // call sites (validateReadOnlySQL inside executeSQLWithMode;
    // validateExplorationQuery inside agentic_loop.go, called by the loop
    // BEFORE executeSQLWithMode). The QueryExecutor implementation is
    // responsible for preserving BOTH checks — it must not silently drop
    // the complexity-mode gate just because the loop no longer calls it
    // directly. See FUNCTIONALITY_SILO_PLAN.md Phase 3/5 for the exact
    // wiring, and the Risk Register entry "Exploration safety mode
    // enforcement gap" for why this is called out explicitly.
    Execute(sql string, isExploration bool) (*QueryResult, error)
}
```

**Implementation requirement:** `QueryExecutorAdapter` (the SQLite-app-DB-era
implementation in `services/adapters/`) must carry both the data source
connection info AND the configured `ExplorationSafetyMode` for that data
source, and must invoke `engine.ValidateExplorationQuery` itself when
`isExploration == true` — not rely on the caller (the loop) to have already
done so. This makes the read-only + complexity-mode invariants properties of
the interface contract, not properties of "whoever happens to call it
correctly today."

### OutputHandler

```go
// OutputHandler receives the loop's output and persists it. The loop calls
// these methods as it progresses through rounds. The implementation
// (in services/adapters/) writes conversation messages, updates query
// records, and stores tool transcripts in the app database.
type OutputHandler interface {
    // CreateQueryRecord creates a query-tracking record before the loop runs.
    // Returns the new query ID, which the loop passes to the other methods
    // so they can update the tracking record as the pipeline progresses.
    CreateQueryRecord(conversationID uint, userMessage string, providerID *uint) (queryID uint, err error)

    // EmitFinalResponse is called exactly once per successful loop execution
    // when the model produces a final answer (with or without a query result).
    // The implementation creates an assistant conversation message with the
    // rendered HTML, SQL results, chart config, exploration trace, and
    // tool transcript.
    EmitFinalResponse(conversationID uint, queryID uint, resp FinalResponse) error

    // EmitSQLWarning is called when all error retries are exhausted. The
    // implementation creates a user-facing error message and updates the
    // query record.
    EmitSQLWarning(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error

    // EmitClarification is called when the loop cannot produce a final answer
    // (e.g., context overflow, loop exhausted, empty response). The
    // implementation creates a system-style assistant message with the
    // clarification text.
    EmitClarification(conversationID uint, queryID uint, category string, message string) error
}
```

### ConversationStore

```go
// ConversationStore provides read access to conversations and messages.
// The engine only needs the subset of the full CRUD surface required by
// the pipeline.
type ConversationStore interface {
    // GetByID returns a conversation by ID. Returns an error if not found.
    GetByID(id uint) (*ConversationMeta, error)

    // GetMessages returns all messages for a conversation, ordered by ID.
    GetMessages(conversationID uint) ([]*ConversationMessageMeta, error)

    // CreateMessage persists a new message in the conversation.
    // Returns the created message.
    CreateMessage(conversationID uint, msg NewMessage) (*ConversationMessageMeta, error)
}

// ConversationMeta is a lightweight view of a conversation.
// It does NOT embed models.Conversation — that type stays in pkg/models/.
type ConversationMeta struct {
    ID                 uint
    LLMProviderID      *uint
    DataSourceID       *uint
    MaxContextMessages int
    VizEnabled         bool
    StreamingEnabled   bool
    Summarize          bool
}

// ConversationMessageMeta is a lightweight view of a message.
type ConversationMessageMeta struct {
    ID             uint
    Role           string
    Content        string
    HTMLContent    *string
    SQLResultJSON  *string
    MetadataJSON   *string
    ToolTranscript *string
}

// NewMessage represents a message to be persisted.
type NewMessage struct {
    Role           string
    Content        string
    HTMLContent    *string
    SQLResultJSON  *string
    MetadataJSON   *string
    ToolTranscript *string
}
```

### Provider & DataSource Stores

```go
// LLMProviderStore provides read access to LLM provider configurations.
type LLMProviderStore interface {
    GetByID(id uint) (*LLMProviderMeta, error)
    GetDefault() (*LLMProviderMeta, error)
}

type LLMProviderMeta struct {
    ID            uint
    Provider      string   // "openai", "anthropic", "ollama", "local"
    Model         string
    BaseURL       string
    APIKey        string
    ContextWindow *int
    MaxTokens     int
}

// DataSourceStore provides read access to data source configurations.
type DataSourceStore interface {
    GetByID(id uint) (*DataSourceMeta, error)
    GetDefault() (*DataSourceMeta, error)
}

type DataSourceMeta struct {
    ID                uint
    Type              string
    ConfigJSON        string    // raw JSON — the engine doesn't parse it
    MaxExplorationRounds int
    ExplorationSafety string
    MaxFinalRetries   int
    MaxToolsPerRound  int
}
```

### SchemaIntrospector

```go
// SchemaIntrospector gathers database metadata for a data source.
type SchemaIntrospector interface {
    GetSchema(ds DataSourceMeta) (*DataSchema, error)
}
```

### ConfigurationProvider

```go
// ConfigurationProvider supplies configuration values the engine needs.
type ConfigurationProvider interface {
    GetEnabledSkillsContent(conversationID uint) (string, error)
    GetAgentLoopConfig() (*AgentLoopConfig, error)
    GetTimeoutSeconds(key string, defaultSeconds int) int
}
```

### Summary of Ports

| Interface | Methods | Used By |
|---|---|---|
| `LLMClient` | 4 | `AgenticLoop` (chat), `Orchestrator` (summarization) |
| `QueryExecutor` | 1 | `AgenticLoop` (every tool call) |
| `OutputHandler` | 4 | `AgenticLoop` (query tracking, final response, errors, clarifications) |
| `ConversationStore` | 3 | `Orchestrator` (load + persist), `AgenticLoop` (history) |
| `LLMProviderStore` | 2 | `Orchestrator` (create LLM client) |
| `DataSourceStore` | 2 | `Orchestrator` (config for loop) |
| `SchemaIntrospector` | 1 | `Orchestrator` (schema for prompt) |
| `ConfigurationProvider` | 3 | `Orchestrator` (skills, loop config, timeouts) |

Total: **8 interfaces, 21 methods.** Every method maps directly to an existing
service function — no new behavior, only new contracts.

---

## Core Types (engine/types.go)

```go
package engine

// LoopInput is everything the agentic loop needs to start processing.
type LoopInput struct {
    UserMessage      string
    ConversationID   uint
    QueryID          uint
    ConversationMeta ConversationMeta
    History          []*ConversationMessageMeta
    Schema           *DataSchema
    DBConnection     DataSourceMeta
    SkillsContent    string
    OnStream         func(StreamEvent)  // nil if streaming disabled
}

// LoopOutput is everything the agentic loop produces.
type LoopOutput struct {
    // FinalResponse is populated when the loop successfully produces an answer.
    // Nil if the loop ended with a clarification or fatal error.
    FinalResponse *FinalResponse

    // Clarification is populated when the loop cannot produce an answer
    // (context overflow, exhaustion, empty response). Nil on success.
    Clarification *Clarification

    // FatalError is populated when the loop cannot recover (LLM call failure).
    FatalError error
}

// FinalResponse is the completed assistant response.
type FinalResponse struct {
    MarkdownText     string           // the model's text answer
    SQL              string           // the final query, if any
    QueryResult      *QueryResult     // the query results, if any
    ChartConfigJSON  string           // resolved Chart.js config, if any
    Summary          *string          // LLM-generated summary, if enabled
    ExplorationTrace []ExplorationResult // exploration rounds
    ToolTranscript   *ToolTranscript  // full tool call transcript
}

// Clarification is a non-answer response.
type Clarification struct {
    Category string // "context_overflow", "loop_exhausted", "empty_truncated"
    Message  string // user-facing text
}

// LoopConfig is all configuration needed by the agentic loop.
// It is built by the orchestrator from provider + data source + app config.
// It has no models.* dependencies.
type LoopConfig struct {
    // Limits
    MaxExplorationRounds int
    MaxToolsPerRound     int
    MaxErrorRetries      int
    TotalRoundCap        int

    // Safety
    SafetyMode       ExplorationSafetyMode

    // LLM
    ContextWindow    *int   // nil = unknown
    ModelName        string

    // Features
    VizEnabled       bool
    Summarize        bool
    StreamingEnabled bool

    // Prompt overrides (from AgentLoopConfig)
    AgentConfig      *AgentLoopConfig
}
```

---

## AgenticLoop (engine/loop.go)

```go
package engine

type AgenticLoop struct {
    LLMClient     LLMClient
    QueryExecutor QueryExecutor
    OutputHandler OutputHandler
}

// Run executes the tool-calling loop.
//
// It is a pure function of its inputs + injected dependencies. It does
// NOT access models.DB, the filesystem, or any global state. It creates
// no goroutines that outlive the function call.
//
// The loop enforces one-shot finality: after the first successful
// is_exploration:false query, further query_database calls are rejected.
//
// On success, the OutputHandler's EmitFinalResponse is called exactly once.
// On recoverable failure, EmitClarification is called. On unrecoverable
// failure (LLM API error), FatalError is returned and no OutputHandler
// method is called — the caller handles cleanup.
func (a *AgenticLoop) Run(ctx context.Context, input LoopInput, config LoopConfig) (*LoopOutput, error)
```

**Key behavior guarantees (testable):**

1. Streaming: if `input.OnStream != nil`, the streaming code path is used. If nil, the blocking path is used. Both produce identical `LoopOutput`.
2. One-shot finality: after a `query_database(is_exploration: false)` succeeds, subsequent `query_database` calls return an error and the loop continues with text-only.
3. Safety: every query passes through `QueryExecutor.Execute(sql, isExploration)`, which internally enforces BOTH the read-only invariant (§0, always) AND the exploration complexity-mode gate (§1.4, when `isExploration` is true). The loop does NOT re-validate — it relies entirely on the `QueryExecutor` contract above. This is a deliberate change of *where* the complexity-mode check lives (today it's a pre-check in `agentic_loop.go`; after extraction it must live inside the executor), not a removal of the check itself. Phase 9 must include a regression test proving strict/moderate mode rejections still occur after extraction.
4. Error retry: when `QueryExecutor.Execute` fails with a retryable error, the error is fed back to the LLM and the loop retries up to `config.MaxErrorRetries` times.
5. Round cap: the loop never exceeds `config.TotalRoundCap` iterations. On exhaustion, `EmitClarification("loop_exhausted", ...)` is called.
6. Context overflow: if `config.ContextWindow` is set and the prompt exceeds 90% of it, the loop calls `EmitClarification("context_overflow", ...)`.
7. Empty response: if the LLM returns no tool calls and no text content, the loop calls `EmitClarification("empty_truncated", ...)`.
8. Tool transcript: every tool call and result is recorded in `LoopOutput.FinalResponse.ToolTranscript`.

---

## Orchestrator (engine/orchestrator.go)

```go
package engine

// Orchestrator coordinates the full pipeline: loads conversation state,
// creates the LLM client, runs the agentic loop, and handles error cleanup.
type Orchestrator struct {
    Conversations   ConversationStore
    Providers       LLMProviderStore
    DataSources     DataSourceStore
    Introspector    SchemaIntrospector
    Config          ConfigurationProvider
    Loop            *AgenticLoop
}

// ProcessMessage processes a user message through the full pipeline.
// This is the entry point called by app.go and headless_handlers.go.
//
// The function:
//   1. Loads conversation + provider + data source from stores
//   2. Fetches schema via Introspector
//   3. Loads skills + loop config from ConfigurationProvider
//   4. Saves the user message via ConversationStore
//   5. Builds LoopInput + LoopConfig
//   6. Creates an LLMClient (factory function injected or called internally)
//   7. Calls AgenticLoop.Run()
//   8. Returns the LoopOutput or error
//
// It does NOT access models.DB or any global state. All persistence goes
// through the injected stores.
func (o *Orchestrator) ProcessMessage(
    ctx context.Context,
    conversationID uint,
    userMessage string,
    onPhase func(string),
    onStream func(StreamEvent),
    createLLMClient func(LLMProviderMeta) (LLMClient, error),
) (*LoopOutput, error)
```

---

## Safety Functions (engine/safety.go)

Moved character-for-character from `pkg/services/sql_execution.go`. These are
pure functions — no dependencies beyond the standard library.

```go
package engine

type ExplorationSafetyMode int
const (
    ExplorationRelaxed  ExplorationSafetyMode = 0
    ExplorationModerate ExplorationSafetyMode = 1
    ExplorationStrict   ExplorationSafetyMode = 2
)

func ValidateReadOnlySQL(sql string) error
func ValidateExplorationQuery(sql string, mode ExplorationSafetyMode) error
func StripSQLComments(sql string) string
func ApplyDefaultLimit(sql string, ds DataSourceMeta, isExploration bool) string
```

Note: `ValidateReadOnlySQL` and `ValidateExplorationQuery` are exported with
capital letters (they were unexported in `sql_execution.go`). The `QueryExecutor`
implementation in `services/adapters/` calls them from the `engine` package.

`ApplyDefaultLimit` is new — it was previously called inside `executeSQLWithMode`
before validation. Extracting it as a pure function makes the executor cleaner.

---

## Rendering Functions (engine/rendering.go)

Moved character-for-character from `pkg/services/sql_execution.go` and
`pkg/services/discussion_engine.go`. All pure functions.

```go
package engine

func RenderResultTable(columns []string, rows [][]interface{}) string       // was renderToHTML
func RenderMarkdown(text string) string                                     // was renderMarkdown
func RenderSQLErrorHTML(sql string, err error, retryable bool) string    // was renderSQLErrorHTML
func FormatSQLResultsForLLM(results *QueryResult) string                    // was formatSQLResultsForLLM
func FormatExplorationHTML(results []ExplorationResult) string              // was formatExplorationHTML
```

The `OutputHandler` implementation calls these to produce the HTML content
stored in conversation messages.

---

## Wiring Diagram (End State)

```
  main.go ──▶ models.ConnectDatabase() ──▶ models.DB (global)
       │
       ├──▶ app.go ──▶ engine.Orchestrator.ProcessMessage()
       │                    │
       │                    ├──▶ ConversationStore ──▶ adapters ──▶ models.DB
       │                    ├──▶ LLMProviderStore  ──▶ adapters ──▶ models.DB
       │                    ├──▶ DataSourceStore   ──▶ adapters ──▶ models.DB
       │                    ├──▶ SchemaIntrospector──▶ adapters ──▶ DBDriver
       │                    ├──▶ ConfigProvider    ──▶ adapters ──▶ models.DB
       │                    └──▶ AgenticLoop.Run()
       │                              │
       │                              ├──▶ LLMClient (interface)
       │                              ├──▶ QueryExecutor   ──▶ adapters ──▶ DBDriver
       │                              └──▶ OutputHandler   ──▶ adapters ──▶ models.DB
       │
       └──▶ headless ──▶ engine.Orchestrator.ProcessMessage()  (same wiring)

  Every arrow crossing "──▶" is an interface call.
  Every "adapters ──▶ models.DB" is a single SQLite query.
  Every "adapters ──▶ DBDriver" is a single external DB operation.

  The engine package has ZERO imports of:
    - pkg/models (except for AgentLoopConfig, which moves to engine/types.go)
    - pkg/services
    - database/sql
    - Wails runtime
    - filesystem (os, path/filepath)
```

---

## What Moves Where (Complete Inventory)

| Current Location | Function / Type | New Location |
|---|---|---|
| `sql_execution.go` | `validateReadOnlySQL` | `engine/safety.go` |
| `sql_execution.go` | `validateExplorationQuery` | `engine/safety.go` |
| `sql_execution.go` | `stripSQLComments` | `engine/safety.go` |
| `sql_execution.go` | `containsSelectIntoTable` | `engine/safety.go` |
| `sql_execution.go` | `insideSingleQuotedString` | `engine/safety.go` |
| `sql_execution.go` | `applyDefaultLimit` | `engine/safety.go` |
| `sql_execution.go` | `ExplorationSafetyMode` | `engine/safety.go` |
| `sql_execution.go` | `QueryResult` | `engine/types.go` |
| `sql_execution.go` | `renderToHTML` | `engine/rendering.go` |
| `sql_execution.go` | `renderMarkdown` | `engine/rendering.go` |
| `sql_execution.go` | `fencedCodeRe` (regex) | `engine/rendering.go` |
| `sql_execution.go` | `executeSQL` | stays in `services/sql_execution.go` |
| `sql_execution.go` | `executeSQLWithMode` | stays in `services/sql_execution.go` |
| `sql_execution.go` | `executeNativeQuery` | stays in `services/sql_execution.go` |
| `sql_execution.go` | `classifyErrorCategory` | stays (used by `renderSQLError`) |
| `discussion_engine.go` | `renderSQLError` | `engine/response.go` |
| `discussion_engine.go` | `handleClarification` | `engine/response.go` |
| `discussion_engine.go` | `formatUserError` | `engine/response.go` |
| `discussion_engine.go` | `buildErrorMetadata` | `engine/response.go` |
| `discussion_engine.go` | `isErrorMessage` | (deleted — not needed after extraction) |
| `discussion_engine.go` | `formatSQLResultsForLLM` | `engine/rendering.go` |
| `discussion_engine.go` | `formatSQLResultsForLLMFromQueryResult` | `engine/rendering.go` |
| `discussion_engine.go` | `formatRowsTable` | `engine/rendering.go` |
| `discussion_engine.go` | `humanizeColumnName` | `engine/rendering.go` |
| `discussion_engine.go` | `formatResultsDigestForSummarization` | `engine/rendering.go` |
| `discussion_engine.go` | `computeColumnStats` | `engine/rendering.go` |
| `discussion_engine.go` | `summarizeResults` | stays (moved to `engine/summarize.go`) |
| `discussion_engine.go` | `resolveChartConfig` | `engine/charts.go` |
| `discussion_engine.go` | `zipScatterPoints` | `engine/charts.go` |
| `discussion_engine.go` | `resolveRefs` | `engine/charts.go` |
| `discussion_engine.go` | `normalizeColRef` | `engine/charts.go` |
| `discussion_engine.go` | `ExplorationResult` | `engine/types.go` |
| `discussion_engine.go` | `ProcessUserMessageWithContext` | `engine/orchestrator.go` |
| `discussion_engine.go` | `formatSkillsContext` | `engine/prompts.go` |
| `discussion_engine.go` | `formatUserError` | `engine/response.go` |
| `agentic_loop.go` | `runAgenticLoop` | `engine/loop.go` (as `AgenticLoop.Run`) |
| `agentic_loop.go` | `buildTools` | `engine/tools.go` |
| `agentic_loop.go` | `buildToolSystemPrompt` | `engine/prompts.go` |
| `agentic_loop.go` | `buildCompactSystemPrompt` | `engine/prompts.go` |
| `agentic_loop.go` | `buildToolLlmMessages` | `engine/prompts.go` |
| `agentic_loop.go` | `handleRespond` | `engine/response.go` |
| `agentic_loop.go` | `renderToolQueryResults` | `engine/response.go` |
| `agentic_loop.go` | `buildTranscript` | `engine/tools.go` |
| `agentic_loop.go` | `storeTechDetail` | (absorbed by `OutputHandler`) |
| `agentic_loop.go` | `buildToolMessages` | `engine/tools.go` |
| `agentic_loop.go` | `formatToolResult` | `engine/rendering.go` |
| `agentic_loop.go` | `detectNumericColumns` | `engine/rendering.go` |
| `agentic_loop.go` | `computeToolResultColStats` | `engine/rendering.go` |
| `agentic_loop.go` | `stripReasoningFromArgs` | `engine/tools.go` |
| `agentic_loop.go` | `roundKind`, `pendingFinalResult` | `engine/loop.go` (unexported) |
| `agentic_loop.go` | `Tool`, `ToolCall`, `FunctionDef` | `engine/tools.go` |
| `agentic_loop.go` | `ToolCallSummary`, `ToolTranscript`, etc. | `engine/types.go` |
| `agentic_loop.go` | `TechDetail` | `engine/types.go` |
| `llm_client.go` | `LLMClient` interface | `engine/ports.go` |
| `llm_client.go` | `ChatMessage`, `Tool`, `StreamEvent`, etc. | `engine/types.go` |
| `llm_client.go` | `NewLLMClient` factory | stays in `services/llm_client.go` |

---

## Configurability (What the Black Box Enables)

Once the loop is behind `AgenticLoop.Run(input, config)`, every config value
becomes a simple field on a struct. The loop no longer knows *where* the
config came from — the orchestrator builds the config, the loop consumes it.

### What's Already Configurable

| Config Field | Source Today | Source After |
|---|---|---|
| `MaxExplorationRounds` | `DataSourceConfig.MaxExplorationRounds` | `LoopConfig.MaxExplorationRounds` |
| `MaxToolsPerRound` | `DataSourceConfig.MaxToolsPerRound` | `LoopConfig.MaxToolsPerRound` |
| `MaxErrorRetries` | `DataSourceConfig.MaxFinalQueryRetries` | `LoopConfig.MaxErrorRetries` |
| `SafetyMode` | `DataSourceConfig.ExplorationSafety` | `LoopConfig.SafetyMode` |
| `ContextWindow` | `LLMProvider.ContextWindow` | `LoopConfig.ContextWindow` |
| Tool descriptions | `AgentLoopConfig.ToolQueryDatabaseDesc` etc. | `LoopConfig.AgentConfig.`… |
| System prompt instructions | `AgentLoopConfig.Instruction1` etc. | `LoopConfig.AgentConfig.`… |

### What Becomes Trivial to Add

| New Config Field | Effect |
|---|---|
| `LoopConfig.Temperature` | Override LLM temperature for this data source |
| `LoopConfig.ThinkingBudget` | Claude/OpenAI extended thinking token budget |
| `LoopConfig.ExplorationPersona` | Custom exploration strategy prompt per data source |
| `LoopConfig.ResponseStyle` | "concise" / "verbose" / "technical" answer style |
| `LoopConfig.MaxFinalRows` | Per-data-source row limit for final queries |
| `LoopConfig.ToolsEnabled` | Bitmask to disable `render_chart` or `query_database` |
| `LoopConfig.RequireConfirmation` | Show "Run query?" confirm before executing |

All can be serialized as JSON, stored in `data_sources.config` or a provider
config field, and loaded at pipeline start. The loop is a pure function of
its config — hot-reloadable without restart.

---

## What Does NOT Change

- **`pkg/models/`** — unchanged. Structs, DB, migrations, global `models.DB`.
  The engine does not import `pkg/models` (except for `AgentLoopConfig` which
  moves to `engine/types.go` — and even then, the engine uses it as a value
  struct, not a DB-backed model).
- **Frontend** — zero changes. All 4 Svelte components remain untouched.
  `app.go`'s Wails bindings keep the same signatures. The bridge is opaque.
- **Database drivers** — zero changes. `DBDriver` and `NativeQuerier` stay
  in `pkg/services/`. The `QueryExecutor` adapter wraps them.
- **LLM providers** — zero changes to the 4 provider implementations.
  `LLMClient` interface definition moves to `engine/ports.go` but with type
  aliases in `services/`, the provider files don't need to change imports.
- **Export, updater, DB switcher, OAuth** — zero changes. These are already
  self-contained and don't interact with the engine.
- **`agent_loop_config.go` CRUD** — unchanged. The `ConfigurationProvider`
  adapter wraps the existing `GetAgentLoopConfig()` function.
- **Wails `wails.json`** — unchanged. No new build dependencies.

---

## Non-Goals (Explicitly Out of Scope)

These are NOT part of this extraction:

1. **Changing any behavior.** The extraction is mechanical — move functions,
   wrap with interfaces, update callers. Zero logic changes.
2. **Replacing models.DB with an interface.** The global `*sql.DB` stays.
   The adapters in `services/adapters/` talk to `models.DB` directly. Only
   the engine package is isolated from it.
3. **Extracting the frontend.** The Svelte + Wails layer is unchanged.
4. **Adding new features.** No new tools, no new config options, no new LLM
   providers. Pure refactor.
5. **Extracting the drivers.** They're already behind `DBDriver`. No change
   needed.
6. **Changing the database schema.** No migrations. No new tables. No new
   columns. The same `~/.yourql/yourql.db` format.
7. **Breaking the headless API.** `headless_handlers.go` gets the same thin
   update as `app.go` — call `orchestrator.ProcessMessage()` instead of
   `services.ProcessUserMessageWithContext()`.
8. **Go module changes.** No new external dependencies. No version bumps.

---

## Validation Criteria (Is the Target Reached?)

1. **`pkg/engine/` compiles without importing `pkg/models`** (except for
   `AgentLoopConfig`, which is a dependency-free struct of strings).
2. **`pkg/engine/` compiles without importing `pkg/services`** — zero
   reverse dependencies.
3. **`pkg/engine/` compiles without importing `database/sql`** — no raw
   SQL access.
4. **`go vet ./pkg/engine/...` passes** with zero warnings.
5. **The existing test suite (`sql_execution_test.go`) still passes.**
6. **A new test in `pkg/engine/loop_test.go` can instantiate `AgenticLoop`
   with mock implementations, call `Run()`, and assert `LoopOutput` — all
   without a running database, a real LLM, or any Wails dependency.**
7. **`app.go` and `headless_handlers.go` compile with their existing
   method signatures unchanged.**
8. **`wails build` succeeds.**
9. **The app launches and a conversation produces the same answer as before
   the extraction (visual + content parity).**