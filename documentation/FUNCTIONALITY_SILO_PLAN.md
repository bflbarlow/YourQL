# YourQL — Functionality Silo Technical Plan

> **This is the execution plan.** It describes every file, every change, and
> every verification step to get from the current monolith to the target
> architecture in `FUNCTIONALITY_SILO_TARGET.md`. Each phase is a single
> commit. Each phase produces a compilable, testable state.
>
> **Prime directive:** zero logic changes. This is a pure mechanical
> extraction — move code, wrap with interfaces, update callers. If a
> function moves, it moves character-for-character.

---

## Table of Contents

1. [Phase 0: Pre-flight Checklist](#phase-0-pre-flight-checklist)
2. [Phase 1: Engine Types and Interfaces](#phase-1-engine-types-and-interfaces)
3. [Phase 2: Move Pure Functions](#phase-2-move-pure-functions)
4. [Phase 3: Create Adapters](#phase-3-create-adapters)
5. [Phase 4: Move Prompts and Tools](#phase-4-move-prompts-and-tools)
6. [Phase 5: New AgenticLoop.Run](#phase-5-new-agenticlooprun)
7. [Phase 6: New Orchestrator.ProcessMessage](#phase-6-new-orchestratorprocessmessage)
8. [Phase 7: Swap Callers](#phase-7-swap-callers)
9. [Phase 8: Cleanup](#phase-8-cleanup)
10. [Phase 9: Engine Tests](#phase-9-engine-tests)
11. [Risk Register](#risk-register)
12. [Rollback Strategy](#rollback-strategy)

---

## Phase 0: Pre-flight Checklist

**Goal:** Ensure a clean starting state. Create a checkpoint.

### Steps

1. **Commit or stash all uncommitted work.**
   ```bash
   cd /Users/bflbarlow/Wails/YourQL
   git status
   # Ensure only intentional untracked files remain.
   # Stash or commit modified files.
   ```

2. **Create a checkpoint branch.**
   ```bash
   git checkout -b refactor/engine-extraction
   # Or: git checkout -b refactor/engine-extraction-pre
   #    git checkout -b refactor/engine-extraction  (if you want pre/post branches)
   ```

3. **Verify baseline.**
   ```bash
   go build ./...
   go vet ./...
   go test ./...
   ```
   All must pass with exit code 0.

4. **Record baseline line counts** (for Phase 8 cleanup verification).
   ```bash
   wc -l pkg/services/discussion_engine.go pkg/services/agentic_loop.go pkg/services/sql_execution.go pkg/services/llm_client.go
   ```

5. **Record a baseline conversation** for parity testing in Phase 7.
   - In the running app, ask: "What tables are in the database?" against a known test DB.
   - Save the conversation ID and the rendered assistant message HTML.
   - This will be the "does it still work" smoke test.

### Verification

```bash
git log --oneline -1              # "checkpoint: pre-extraction baseline"
go build ./... && echo "PASS"     # Must say PASS
go vet ./... && echo "PASS"       # Must say PASS
```

---

## Phase 1: Engine Types and Interfaces

**Goal:** Create the `pkg/engine/` package with pure type definitions. Zero
logic. Nothing imports this package yet — it's a leaf.

**Risk: NONE.** These are type definitions only. Nothing links to them.

### Type Alias Strategy for LLMClient

The `LLMClient` interface, `ChatMessage`, `Tool`, `StreamEvent` and related
types currently live in `pkg/services/llm_client.go`. They need to move to
`pkg/engine/` so the engine can declare its ports without importing services.

**Approach:** Define the canonical types in `engine/types.go` and
`engine/ports.go`. In `services/llm_client.go`, replace the definitions with
type aliases pointing to `engine`. This is a standard Go refactoring pattern.
All existing code in `services/` continues to use `services.ChatMessage` etc.
which are now aliases for `engine.ChatMessage` — zero call-site changes.

```go
// In services/llm_client.go AFTER the move:
type LLMClient = engine.LLMClient
type ChatMessage = engine.ChatMessage
type ToolCall = engine.ToolCall
type ToolCallFunction = engine.ToolCallFunction
type Tool = engine.Tool
type FunctionDef = engine.FunctionDef
type StreamEvent = engine.StreamEvent
type StreamEventType = engine.StreamEventType
```

### Files Created

#### `pkg/engine/types.go`

All shared types. Copy-paste from their current locations, prefix types with
package name in comments only (no logic changes).

```go
package engine

import (
    "time"
)

// ── Copied from pkg/services/llm_client.go ──────────────────────────

type StreamEventType string

const (
    StreamContentDelta   StreamEventType = "content_delta"
    StreamToolCallDelta  StreamEventType = "tool_call_delta"
    StreamReasoningStart StreamEventType = "reasoning_start"
    StreamReasoningEnd   StreamEventType = "reasoning_end"
    StreamToolCallStart  StreamEventType = "tool_call_start"
    StreamToolCallEnd    StreamEventType = "tool_call_end"
    StreamDone           StreamEventType = "done"
)

type StreamEvent struct {
    Type            StreamEventType `json:"type"`
    Content         string          `json:"content,omitempty"`
    ToolCallID      string          `json:"tool_call_id,omitempty"`
    ToolName        string          `json:"tool_name,omitempty"`
    Arguments       string          `json:"arguments,omitempty"`
    ReasoningStart  bool            `json:"reasoning_start,omitempty"`
    ReasoningEnd    bool            `json:"reasoning_end,omitempty"`
    ToolCallStart   bool            `json:"tool_call_start,omitempty"`
    ToolCallEnd     bool            `json:"tool_call_end,omitempty"`
    FinishReason    string          `json:"finish_reason,omitempty"`
}

type ChatMessage struct {
    Role             string     `json:"role"`
    Content          string     `json:"content,omitempty"`
    ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID       string     `json:"tool_call_id,omitempty"`
    Name             string     `json:"name,omitempty"`
    FinishReason     string     `json:"finish_reason,omitempty"`
    PromptTokens     int        `json:"prompt_tokens,omitempty"`
    CompletionTokens int        `json:"completion_tokens,omitempty"`
}

type ToolCall struct {
    ID       string           `json:"id"`
    Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
}

type Tool struct {
    Type     string      `json:"type"`
    Function FunctionDef `json:"function"`
}

type FunctionDef struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`
}

// ── Copied from pkg/services/sql_execution.go ───────────────────────

type QueryResult struct {
    Columns  []string        `json:"columns"`
    Rows     [][]interface{} `json:"rows"`
    RowCount int             `json:"row_count"`
}

// ── Copied from pkg/services/database_introspection.go ──────────────

type DataSchema struct {
    Tables []TableInfo `json:"tables"`
}

type TableInfo struct {
    Name        string           `json:"name"`
    Columns     []ColumnInfo     `json:"columns"`
    RowCount    int64            `json:"row_count,omitempty"`
    Description string           `json:"description,omitempty"`
    Indexes     []IndexInfo      `json:"indexes,omitempty"`
    ForeignKeys []ForeignKeyInfo `json:"foreign_keys,omitempty"`
}

type ColumnInfo struct {
    Name         string `json:"name"`
    DataType     string `json:"data_type"`
    IsNullable   bool   `json:"is_nullable"`
    IsPrimaryKey bool   `json:"is_primary_key"`
    DefaultValue string `json:"default_value,omitempty"`
    Description  string `json:"description,omitempty"`
}

type IndexInfo struct {
    Name     string   `json:"name"`
    IsUnique bool     `json:"is_unique"`
    Columns  []string `json:"columns"`
}

type ForeignKeyInfo struct {
    Name           string   `json:"name"`
    Columns        []string `json:"columns"`
    ReferencedTable string  `json:"referenced_table"`
    ReferencedColumns []string `json:"referenced_columns"`
}

// ── Copied from pkg/services/discussion_engine.go ───────────────────

type ExplorationResult struct {
    SQL       string       `json:"sql"`
    Result    *QueryResult `json:"result,omitempty"`
    Round     int          `json:"round"`
    Explained string       `json:"explained,omitempty"`
}

// ── Copied from pkg/services/agentic_loop.go ────────────────────────

type ToolCallSummary struct {
    ID         string `json:"id"`
    Tool       string `json:"tool"`
    Args       string `json:"args,omitempty"`
    Success    bool   `json:"success"`
    Result     string `json:"result,omitempty"`
    DurationMs int    `json:"duration_ms"`
}

type TranscriptAction struct {
    Round       int              `json:"round"`
    Tool        string           `json:"tool"`
    SQL         string           `json:"sql,omitempty"`
    Exploration bool             `json:"exploration,omitempty"`
    ResultCount int              `json:"result_count,omitempty"`
    Error       string           `json:"error,omitempty"`
    DurationMs  int              `json:"duration_ms"`
    Finish      string           `json:"finish_reason,omitempty"`
    ToolCalls   []ToolCallSummary `json:"tool_calls,omitempty"`
}

type ToolTranscript struct {
    Rounds  []TranscriptAction `json:"rounds"`
    TotalMs int                `json:"total_ms"`
}

type TechDetail struct {
    Version  int               `json:"version"`
    Round    int               `json:"round"`
    Kind     string            `json:"kind,omitempty"`
    Request  TechDetailRequest `json:"request"`
    Response TechDetailResponse `json:"response"`
}

type TechDetailRequest struct {
    MessageCount int    `json:"message_count"`
    LastUserMsg  string `json:"last_user_msg,omitempty"`
    RawMessages  string `json:"raw_messages,omitempty"`
}

type TechDetailResponse struct {
    FinishReason string          `json:"finish_reason,omitempty"`
    TextContent  string          `json:"text_content,omitempty"`
    ToolCalls    []ToolCallSummary `json:"tool_calls,omitempty"`
}

// ── New: Black-box types ────────────────────────────────────────────

// LoopInput is everything the agentic loop needs to start.
type LoopInput struct {
    UserMessage    string
    ConversationID uint
    QueryID        uint
    Conversation   ConversationMeta
    History        []*ConversationMessageMeta
    Schema         *DataSchema
    DBConnection   DataSourceMeta
    SkillsContent  string
    OnStream       func(StreamEvent)
}

// LoopOutput is everything the agentic loop produces.
type LoopOutput struct {
    FinalResponse *FinalResponse
    Clarification *Clarification
    FatalError    error
}

type FinalResponse struct {
    MarkdownText     string
    SQL              string
    QueryResult      *QueryResult
    ChartConfigJSON  string
    Summary          *string
    ExplorationTrace []ExplorationResult
    ToolTranscript   *ToolTranscript
}

type Clarification struct {
    Category string
    Message  string
}

type LoopConfig struct {
    MaxExplorationRounds int
    MaxToolsPerRound     int
    MaxErrorRetries      int
    TotalRoundCap        int
    SafetyMode           ExplorationSafetyMode
    ContextWindow        *int
    ModelName            string
    VizEnabled           bool
    Summarize            bool
    StreamingEnabled     bool
    AgentConfig          *AgentLoopConfig
}

// ── Meta types (lightweight views, no models.* dependency) ──────────

type ConversationMeta struct {
    ID                 uint
    LLMProviderID      *uint
    DataSourceID       *uint
    MaxContextMessages int
    VizEnabled         bool
    StreamingEnabled   bool
    Summarize          bool
}

type ConversationMessageMeta struct {
    ID             uint
    Role           string
    Content        string
    HTMLContent    *string
    SQLResultJSON  *string
    MetadataJSON   *string
    ToolTranscript *string
}

type NewMessage struct {
    Role           string
    Content        string
    HTMLContent    *string
    SQLResultJSON  *string
    MetadataJSON   *string
    ToolTranscript *string
}

type LLMProviderMeta struct {
    ID            uint
    Provider      string
    Model         string
    BaseURL       string
    APIKey        string
    ContextWindow *int
    MaxTokens     int
}

type DataSourceMeta struct {
    ID                  uint
    Type                string
    Host                string
    Port                int
    Database            string
    Username            string
    Password            string
    SSLMode             string
    Extra               string
    FilePath            string
    FileType            string
    ConfigJSON          string
    MaxExplorationRounds int
    ExplorationSafety   string
    MaxFinalRetries     int
    MaxToolsPerRound    int
}
```

#### `pkg/engine/ports.go`

```go
package engine

import "context"

// LLMClient communicates with an LLM provider.
type LLMClient interface {
    ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
    ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
    ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (*ChatMessage, string, string, error)
    ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onStream func(StreamEvent)) (*ChatMessage, string, string, error)
}

// QueryExecutor runs SQL against a user's data source.
type QueryExecutor interface {
    Execute(sql string, isExploration bool) (*QueryResult, error)
}

// OutputHandler receives and persists the loop's results.
type OutputHandler interface {
    CreateQueryRecord(conversationID uint, userMessage string, providerID *uint) (queryID uint, err error)
    EmitFinalResponse(conversationID uint, queryID uint, resp FinalResponse) error
    EmitSQLWarning(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error
    EmitClarification(conversationID uint, queryID uint, category string, message string) error
}

// ConversationStore provides read/write access to conversations.
type ConversationStore interface {
    GetByID(id uint) (*ConversationMeta, error)
    GetMessages(conversationID uint) ([]*ConversationMessageMeta, error)
    CreateMessage(conversationID uint, msg NewMessage) (*ConversationMessageMeta, error)
}

// LLMProviderStore provides read access to LLM provider configs.
type LLMProviderStore interface {
    GetByID(id uint) (*LLMProviderMeta, error)
    GetDefault() (*LLMProviderMeta, error)
}

// DataSourceStore provides read access to data source configs.
type DataSourceStore interface {
    GetByID(id uint) (*DataSourceMeta, error)
    GetDefault() (*DataSourceMeta, error)
}

// SchemaIntrospector gathers database metadata.
type SchemaIntrospector interface {
    GetSchema(ds DataSourceMeta) (*DataSchema, error)
}

// ConfigurationProvider supplies runtime configuration.
type ConfigurationProvider interface {
    GetEnabledSkillsContent(conversationID uint) (string, error)
    GetAgentLoopConfig() (*AgentLoopConfig, error)
    GetTimeoutSeconds(key string, defaultSeconds int) int
}
```

#### `pkg/engine/safety.go`

```go
package engine

// ExplorationSafetyMode controls query complexity limits for exploration.
// Values match the existing ExplorationSafetyMode in sql_execution.go.
type ExplorationSafetyMode int

const (
    ExplorationRelaxed  ExplorationSafetyMode = 0
    ExplorationModerate ExplorationSafetyMode = 1
    ExplorationStrict   ExplorationSafetyMode = 2
)
```

### Files Modified

#### `pkg/services/llm_client.go`

Replace the existing type definitions (lines 17–96, the `LLMClient` interface,
`StreamEvent`, `StreamEventType`, `ChatMessage`, `ToolCall`, `ToolCallFunction`,
`Tool`, `FunctionDef`) with type aliases:

```go
package services

import "YourQL/pkg/engine"

// Type aliases — canonical definitions are in pkg/engine/
type LLMClient = engine.LLMClient
type StreamEvent = engine.StreamEvent
type StreamEventType = engine.StreamEventType
type ChatMessage = engine.ChatMessage
type ToolCall = engine.ToolCall
type ToolCallFunction = engine.ToolCallFunction
type Tool = engine.Tool
type FunctionDef = engine.FunctionDef
```

Delete the `const` block for `StreamContentDelta` etc. — they also move to
`engine/types.go`. Add a replacement alias block or import them from `engine`:

```go
// Re-export stream event constants from engine
const (
    StreamContentDelta   = engine.StreamContentDelta
    StreamToolCallDelta  = engine.StreamToolCallDelta
    StreamReasoningStart = engine.StreamReasoningStart
    StreamReasoningEnd   = engine.StreamReasoningEnd
    StreamToolCallStart  = engine.StreamToolCallStart
    StreamToolCallEnd    = engine.StreamToolCallEnd
    StreamDone           = engine.StreamDone
)
```

**IMPORTANT:** Keep `NewLLMClient` factory function in `services/llm_client.go`
exactly as-is. It returns `LLMClient` — which is now `engine.LLMClient`. Zero
changes to the factory or any provider file.

#### `pkg/services/sql_execution.go`

Replace `type QueryResult struct` (lines 163–167) with:
```go
type QueryResult = engine.QueryResult
```

Replace `type ExplorationSafetyMode int` and its constants (lines 584–593) with:
```go
type ExplorationSafetyMode = engine.ExplorationSafetyMode
const (
    ExplorationRelaxed  = engine.ExplorationRelaxed
    ExplorationModerate = engine.ExplorationModerate
    ExplorationStrict   = engine.ExplorationStrict
)
```

#### `pkg/services/database_introspection.go`

Replace `type DataSchema`, `TableInfo`, `ColumnInfo`, `IndexInfo`, `ForeignKeyInfo`
with type aliases:

```go
type DataSchema = engine.DataSchema
type TableInfo = engine.TableInfo
type ColumnInfo = engine.ColumnInfo
type IndexInfo = engine.IndexInfo
type ForeignKeyInfo = engine.ForeignKeyInfo
```

#### `pkg/services/discussion_engine.go`

Replace `type ExplorationResult struct` with:
```go
type ExplorationResult = engine.ExplorationResult
```

#### `pkg/services/agentic_loop.go`

Replace `type ToolCallSummary`, `TranscriptAction`, `ToolTranscript`, `TechDetail`
with type aliases. Also replace `Tool`, `ToolCall`, `FunctionDef` aliases (these
are now in `llm_client.go` aliases pointing to engine — ensure they're consistent).

### Verification

```bash
go build ./...    # Must succeed. engine/ is a leaf; services/ aliases point to it.
go vet ./...      # Must pass.
go test ./...     # Must pass (existing sql_execution tests use type aliases).
```

**Commit message:** `phase 1: create pkg/engine types and port interfaces`

---

## Phase 2: Move Pure Functions

**Goal:** Move all pure functions (no external dependencies beyond stdlib) to
`pkg/engine/`. These are the safety validators, renderers, and chart resolvers.

**Risk: LOW.** All moved functions are pure. All call sites are updated to use
`engine.FunctionName()` instead of the naked `functionName()`. The logic moves
character-for-character.

### Principles for This Phase

1. **Copy the function body exactly.** Do not rename variables. Do not reorder
   statements. Do not "clean up." The diff should show the function body
   unchanged, only the package declaration and exported-ness change.
2. **Unexported → exported.** Internal functions (`validateReadOnlySQL`) become
   `ValidateReadOnlySQL`. Their callers in `services/` use the new exported name.
3. **Keep helpers together.** If a function calls an unexported helper, move
   the helper too.
4. **One file at a time.** Move all functions from one source file before
   starting the next, to keep the commit manageable.

### 2a. Safety Functions (`sql_execution.go` → `engine/safety.go`)

**Move these functions:**

| Function | Becomes | Dependencies |
|---|---|---|
| `validateReadOnlySQL` | `ValidateReadOnlySQL` | `stripSQLComments`, `containsSelectIntoTable` |
| `validateExplorationQuery` | `ValidateExplorationQuery` | `ValidateReadOnlySQL` |
| `stripSQLComments` | `StripSQLComments` | (none) |
| `containsSelectIntoTable` | (unexported, stays with `ValidateReadOnlySQL`) | `insideSingleQuotedString` |
| `insideSingleQuotedString` | (unexported, stays with `containsSelectIntoTable`) | (none) |
| `applyDefaultLimit` | `ApplyDefaultLimit` | (none) |

**Callers to update in `sql_execution.go`:**
- `executeSQLWithMode` line 197: `validateReadOnlySQL(sqlQuery)` → `engine.ValidateReadOnlySQL(sqlQuery)`
- `executeSQLWithMode` line ~200: `applyDefaultLimit(...)` → `engine.ApplyDefaultLimit(...)`
- `agentic_loop.go` line 1282: `validateExplorationQuery(...)` → `engine.ValidateExplorationQuery(...)`

**After this step:** `engine/safety.go` is complete. `sql_execution.go` keeps
`executeSQL`, `executeSQLWithMode`, `executeNativeQuery`, `renderToHTML`,
`renderMarkdown`, `classifyErrorCategory`, and helper functions.

### 2b. Rendering Functions (`sql_execution.go` + `discussion_engine.go` → `engine/rendering.go`)

**Move from `sql_execution.go`:**

| Function | Becomes |
|---|---|
| `renderToHTML` | `RenderResultTable` |
| `renderMarkdown` | `RenderMarkdown` |
| `fencedCodeRe` (package var) | (unexported, moves with `RenderMarkdown`) |

**Move from `discussion_engine.go`:**

| Function | Becomes |
|---|---|
| `formatSQLResultsForLLM` | `FormatSQLResultsForLLM` |
| `formatSQLResultsForLLMFromQueryResult` | `FormatSQLResultsForLLMFromResult` |
| `formatRowsTable` | `FormatRowsTable` |
| `humanizeColumnName` | (unexported, moves with `FormatRowsTable`) |
| `formatResultsDigestForSummarization` | `FormatResultsDigestForSummarization` |
| `computeColumnStats` | (unexported, stays with `FormatResultsDigest`) |
| `columnStats` struct | (unexported, stays) |
| `parseFloat`, `formatFloat` | (unexported, stays) |

Also move from `agentic_loop.go`:

| Function | Becomes |
|---|---|
| `formatToolResult` | `FormatToolResult` |
| `detectNumericColumns` | `DetectNumericColumns` |
| `computeToolResultColStats` | (unexported, stays) |
| `toFloat64ForStats` | (unexported, stays) |

**Callers to update:**
- `sql_execution.go`: `renderToHTML(...)` → `engine.RenderResultTable(...)`; `renderMarkdown(...)` → `engine.RenderMarkdown(...)`
- `discussion_engine.go`: all format/render calls → `engine.Format*`
- `agentic_loop.go`: `formatToolResult` → `engine.FormatToolResult`; `detectNumericColumns` → `engine.DetectNumericColumns`

### 2c. Chart Resolution (`discussion_engine.go` → `engine/charts.go`)

**Move these functions:**

| Function | Becomes |
|---|---|
| `resolveChartConfig` | `ResolveChartConfig` |
| `zipScatterPoints` | (unexported, stays with `ResolveChartConfig`) |
| `resolveRefs` | (unexported, stays) |
| `normalizeColRef` | (unexported, stays) |

**Callers to update:**
- `agentic_loop.go`: `resolveChartConfig(...)` → `engine.ResolveChartConfig(...)`

### Verification (After Each Sub-phase)

```bash
go build ./...    # Must succeed
go vet ./...      # Must pass
go test ./...     # Must pass (test expectations unchanged)
```

**Commit messages:**
- `phase 2a: move safety functions to engine/safety.go`
- `phase 2b: move rendering functions to engine/rendering.go`
- `phase 2c: move chart functions to engine/charts.go`

---

## Phase 3: Create Adapters

**Goal:** Create `pkg/services/adapters/` with implementations of every engine
port. Each adapter delegates to an existing `pkg/services/` function. The
adapters are **conversion layers only** — they translate between
`engine.MetaType` and `models.Type`, then call existing code.

**Risk: LOW.** New files only. Nothing calls the adapters yet. The existing
code paths are unchanged.

### Adapter Design Pattern

Every adapter follows this template:

```go
package adapters

// SqliteConversationStore implements engine.ConversationStore
// backed by the app's SQLite database via models.DB.
type SqliteConversationStore struct{}

func (s *SqliteConversationStore) GetByID(id uint) (*engine.ConversationMeta, error) {
    conv, err := services.GetConversationByID(id)
    if err != nil {
        return nil, err
    }
    return &engine.ConversationMeta{
        ID:                 conv.ID,
        LLMProviderID:      conv.LLMProviderID,
        DataSourceID:       conv.DataSourceID,
        MaxContextMessages: conv.MaxContextMessages,
        VizEnabled:         conv.VizEnabled,
        StreamingEnabled:   conv.StreamingEnabled,
        Summarize:          conv.Summarize,
    }, nil
}
```

Key rules:
1. Each adapter method calls exactly ONE existing service function.
2. The adapter's job is TYPE CONVERSION only. No logic, no defaults, no
   fallbacks, no error mapping.
3. If the existing function returns nil + nil for "not found," the adapter
   returns nil + error (e.g., `fmt.Errorf("conversation %d not found", id)`).
4. Field names in the Meta types are a flat subset of the models struct fields.
   No field is renamed — this reduces mapping errors.

### Files Created

#### `pkg/services/adapters/conversation_store.go`

Implements `engine.ConversationStore`.

```go
package adapters

type SqliteConversationStore struct{}

func (s *SqliteConversationStore) GetByID(id uint) (*engine.ConversationMeta, error)
    // delegates to services.GetConversationByID

func (s *SqliteConversationStore) GetMessages(conversationID uint) ([]*engine.ConversationMessageMeta, error)
    // delegates to services.GetConversationMessages

func (s *SqliteConversationStore) CreateMessage(conversationID uint, msg engine.NewMessage) (*engine.ConversationMessageMeta, error)
    // delegates to services.CreateConversationMessage
```

#### `pkg/services/adapters/provider_store.go`

Implements `engine.LLMProviderStore`.

```go
package adapters

type SqliteLLMProviderStore struct{}

func (s *SqliteLLMProviderStore) GetByID(id uint) (*engine.LLMProviderMeta, error)
    // delegates to services.GetLLMProviderByID

func (s *SqliteLLMProviderStore) GetDefault() (*engine.LLMProviderMeta, error)
    // delegates to services.GetDefaultLLMProvider
```

#### `pkg/services/adapters/datasource_store.go`

Implements `engine.DataSourceStore`.

```go
package adapters

type SqliteDataSourceStore struct{}

func (s *SqliteDataSourceStore) GetByID(id uint) (*engine.DataSourceMeta, error)
    // delegates to services.GetDataSourceByID

func (s *SqliteDataSourceStore) GetDefault() (*engine.DataSourceMeta, error)
    // delegates to services.GetDefaultDataSource
```

#### `pkg/services/adapters/config_provider.go`

Implements `engine.ConfigurationProvider`.

```go
package adapters

type SqliteConfigProvider struct{}

func (s *SqliteConfigProvider) GetEnabledSkillsContent(conversationID uint) (string, error)
    // delegates to services.GetEnabledSkillsContent

func (s *SqliteConfigProvider) GetAgentLoopConfig() (*engine.AgentLoopConfig, error)
    // delegates to services.GetAgentLoopConfig
    // NOTE: services.GetAgentLoopConfig returns *models.AgentLoopConfig.
    // The adapter converts it to *engine.AgentLoopConfig.
    // IF AgentLoopConfig hasn't moved to engine/ yet (Phase 5), this
    // adapter will need to import models and convert manually.
    // Phase 5 resolves this by moving AgentLoopConfig to engine types.

func (s *SqliteConfigProvider) GetTimeoutSeconds(key string, defaultSeconds int) int
    // delegates to services.GetTimeoutSetting
```

**NOTE ON AgentLoopConfig:** The `AgentLoopConfig` struct currently lives in
`pkg/models/agent_loop_config.go`. In Phase 5, it moves to `engine/types.go`.
Until then, the `GetAgentLoopConfig` adapter method performs a field-by-field
copy from `*models.AgentLoopConfig` to `*engine.AgentLoopConfig`. Both structs
are identical (same fields, same types) — this is a purely mechanical copy.

#### `pkg/services/adapters/query_executor.go`

Implements `engine.QueryExecutor`.

```go
package adapters

// QueryExecutorAdapter wraps a *models.DataSource and delegates to
// executeSQLWithMode. It is constructed per-pipeline-run because the
// data source is conversation-specific.
//
// SAFETY-CRITICAL (do not simplify away): SafetyMode must be set from the
// same DataSourceConfig.ExplorationSafety value the OLD agentic_loop.go used
// (via ParseExplorationSafety). Execute() must call
// engine.ValidateExplorationQuery(sql, q.SafetyMode) whenever isExploration
// is true, IN ADDITION to whatever executeSQLWithMode already does
// internally (engine.ValidateReadOnlySQL, unconditionally). Today the
// complexity-mode check (validateExplorationQuery) is called by
// agentic_loop.go BEFORE executeSQLWithMode is ever reached — it is NOT
// inside executeSQLWithMode. If this adapter only calls
// services.executeSQLWithMode() and nothing else, the complexity-mode gate
// silently disappears when agentic_loop.go is deleted in Phase 8. See the
// Risk Register entry "Exploration safety mode enforcement gap" below.
type QueryExecutorAdapter struct {
    DataSource *models.DataSource
    SafetyMode engine.ExplorationSafetyMode
}

func (q *QueryExecutorAdapter) Execute(sqlQuery string, isExploration bool) (*engine.QueryResult, error) {
    if isExploration {
        if err := engine.ValidateExplorationQuery(sqlQuery, q.SafetyMode); err != nil {
            return nil, err
        }
    }
    // executeSQLWithMode still independently calls engine.ValidateReadOnlySQL
    // for every query (exploration and final alike) — that check is NOT
    // removed or duplicated here, it stays the single choke point per
    // AGENT_READ_FIRST.md §0.
    return services.ExecuteSQLWithMode(q.DataSource, sqlQuery, isExploration)
}
    // NOTE: services.executeSQLWithMode must be exported (capitalized) or
    // wrapped, since it's called from the adapters package now instead of
    // from within pkg/services. See "Exported Surface Changes" note in
    // Phase 3 verification below.
```

#### `pkg/services/adapters/output_handler.go`

Implements `engine.OutputHandler`.

```go
package adapters

type SqliteOutputHandler struct{}

func (o *SqliteOutputHandler) CreateQueryRecord(conversationID uint, userMessage string, providerID *uint) (uint, error)
    // delegates to services.CreateQuery

func (o *SqliteOutputHandler) EmitFinalResponse(conversationID uint, queryID uint, resp engine.FinalResponse) error
    // This is a NEW function that encapsulates what handleRespond and
    // renderToolQueryResults currently do. It:
    //   1. Renders the HTML (using engine.RenderMarkdown, engine.RenderResultTable)
    //   2. Resolves chart config (if resp.ChartConfigJSON != "")
    //   3. Formats exploration HTML
    //   4. Calls services.CreateConversationMessage + services.UpdateQueryStatus
    //   5. Persists tool transcript
    //
    // NOTE: This function IS new logic — it combines the behavior of
    // handleRespond + renderToolQueryResults. See Phase 5 for how the
    // loop calls it.

func (o *SqliteOutputHandler) EmitSQLWarning(conversationID uint, queryID uint, sql string, err error, isRetryable bool) error
    // delegates to services.renderSQLError-like behavior

func (o *SqliteOutputHandler) EmitClarification(conversationID uint, queryID uint, category string, message string) error
    // delegates to services.handleClarification-like behavior
```

**NOTE ON OutputHandler:** This is the only adapter that contains "new" logic
— it combines `handleRespond` and `renderToolQueryResults` into a single
`EmitFinalResponse` method. The existing `handleRespond` and
`renderToolQueryResults` stay in `agentic_loop.go` until Phase 7 when they're
deleted. The `OutputHandler` adapter duplicates their logic with engine types.
**The duplication is intentional** — it proves that the output handler works
before we delete the old code path in Phase 8.

#### `pkg/services/adapters/introspector.go`

Implements `engine.SchemaIntrospector`.

```go
package adapters

type SchemaIntrospectorAdapter struct{}

func (s *SchemaIntrospectorAdapter) GetSchema(ds engine.DataSourceMeta) (*engine.DataSchema, error)
    // 1. Construct a *models.DataSource from engine.DataSourceMeta
    // 2. Call services.GetDataSchema(modelsDS)
    // 3. Return the result (DataSchema is now a type alias for engine.DataSchema)
```

### Credential Safety (AGENT_READ_FIRST.md §3.5 — apply to every file in this phase)

Every adapter in this phase touches `LLMProviderMeta` (`APIKey` field) and/or
`DataSourceMeta` (`Password` field) as plain strings. This phase is entirely
new code, which is exactly the situation where a debug `log.Printf("%+v",
meta)` or `log.Printf("provider: %v", providerMeta)` gets added during
development and accidentally ships — §3.5 is explicit: "Never log API keys
or passwords."

**Required before this phase's commit:**
- [ ] `grep -rn "log\.\|slog\." pkg/services/adapters/` — review every match
- [ ] No log statement takes a full `LLMProviderMeta` or `DataSourceMeta`
      struct (or `models.LLMProvider` / `models.DataSource`) as a `%v`/`%+v`
      argument. Log individual non-secret fields (`ID`, `Type`, `Provider`)
      by name if logging is needed for debugging.
- [ ] If an adapter method's error path wraps an error with `fmt.Errorf`,
      confirm the wrapped error text does not itself already contain a
      credential (check what `services.GetLLMProviderByID` /
      `services.GetDataSourceByID` put in their own error strings today —
      this phase must not introduce a *new* leak, but should also not
      assume the existing code is leak-free without checking).

### Verification

```bash
go build ./...    # Must succeed
go vet ./...      # Must pass
go test ./...     # Must pass
```

**Commit message:** `phase 3: create SQLite-backed adapters for engine ports`

---

## Phase 4: Move Prompts and Tools

**Goal:** Move the system prompt construction and tool definition functions to
`pkg/engine/`. These functions are pure string builders that depend on types
already in `engine/`.

**Risk: LOW.** All moved functions are string builders. They take engine types
as input and produce engine types as output. No DB access, no side effects.

### Files Created

#### `pkg/engine/prompts.go`

Move from `agentic_loop.go`:

| Function | Notes |
|---|---|
| `buildToolSystemPrompt` | Takes `*DataSchema`, `DataSourceMeta`, `bool`, `string`. Uses `AgentLoopConfig`. |
| `buildCompactSystemPrompt` | Takes `*DataSchema`, `DataSourceMeta`. No config. |
| `buildToolLlmMessages` | Takes many params. Builds the full message list. |

Also move from `discussion_engine.go`:

| Function | Notes |
|---|---|
| `formatSkillsContext` | Simple string wrapper around skills content. |

**IMPORTANT:** These functions currently reference `*models.DataSource` for
dialect hints and `*models.AgentLoopConfig` for tool descriptions. They must
be refactored to use `DataSourceMeta` and `*engine.AgentLoopConfig` instead.
The `AgentLoopConfig` struct moves from `pkg/models/` to `engine/types.go` in
this phase.

**AgentLoopConfig move:**

1. Copy `AgentLoopConfig` struct to `engine/types.go` (or a new `engine/agent_loop_config.go`)
2. In `pkg/models/agent_loop_config.go`, replace the struct definition with a type alias:
   ```go
   type AgentLoopConfig = engine.AgentLoopConfig
   ```
3. Update `pkg/services/agent_loop_config.go` to use the engine type (through the alias, this is transparent)

#### `pkg/engine/tools.go`

Move from `agentic_loop.go`:

| Function | Notes |
|---|---|
| `buildTools` | Builds the 3 Tool definitions. Takes `*engine.AgentLoopConfig`, `bool`, `*DataSchema`, `DataSourceMeta`. |
| `buildToolMessages` | Converts `ToolTranscript` → `[]ChatMessage`. |
| `buildTranscript` | Constructs `*ToolTranscript` from `[]TranscriptAction`. |
| `stripReasoningFromArgs` | Pure string manipulation helper. |

### Callers to Update

- `agentic_loop.go`: `buildTools(...)` → `engine.BuildTools(...)`, etc.
- `discussion_engine.go`: `buildToolLlmMessages(...)` → `engine.BuildToolLlmMessages(...)`

### Verification

```bash
go build ./...
go vet ./...
# Manual: diff the output of buildToolSystemPrompt before/after
# (should be character-for-character identical)
```

**Commit message:** `phase 4: move prompts, tools, and AgentLoopConfig to engine/`

---

## Phase 5: New AgenticLoop.Run

**Goal:** Create `engine/loop.go` with `AgenticLoop` struct and `Run()` method.
This is the new implementation of `runAgenticLoop` using engine types and
injected interfaces. It exists **alongside** the old `runAgenticLoop` — nothing
calls the new one yet.

**Risk: HIGH.** This is the most complex function in the app. Every line of
the old `runAgenticLoop` must be reproduced exactly in the new `Run()`.

**Charter callout (AGENT_READ_FIRST.md §1.3 — read before starting this
phase):** "Be conservative when modifying the loop's tool-response parsing
and fallback text-response handling... It has been tuned across providers
with different tool-calling formats (OpenAI, Anthropic, Ollama, local/custom).
A change that breaks parsing for one provider will silently fail for all
users of that provider." This phase moves exactly that code. Two specific
sub-areas need extra scrutiny during the copy, not just a general diff:
  1. **The plain-text-as-`respond_to_user` fallback path** — what happens
     when a provider (notably Ollama/local) returns prose instead of a
     well-formed tool call. Verify the fallback trigger condition is copied
     exactly, not "cleaned up."
  2. **Tool-call argument JSON parsing per provider** — malformed or
     partial JSON handling, reasoning-content stripping
     (`stripReasoningFromArgs`), and empty-response detection. These differ
     subtly across the 4 providers' response shapes.

Do not treat Phase 5 as done until Phase 7's multi-provider verification gate
(below) has been run against all 4 providers, not just the one used during
development.

### Strategy: Character-for-Character Copy with Targeted Replacements

1. Copy the body of `runAgenticLoop` (lines 1033–1622) into `engine/loop.go`
   as `func (a *AgenticLoop) Run(ctx context.Context, input LoopInput, config LoopConfig) (*LoopOutput, error)`.
2. Apply the following **targeted replacements** (each one verified before
   moving to the next):

| Old Reference | New Reference | Context |
|---|---|---|
| `query` (first param) | `input.QueryID` | Query ID for tracking |
| `client` (second param) | `a.LLMClient` | LLM client from struct |
| `messages` (third param) | parameter no longer needed — build inside the loop from `input` | Chat message list |
| `dbConnection` (fourth param) | `input.DBConnection` | Data source config |
| `schema` (fifth param) | `input.Schema` | Database schema |
| `conversation` (sixth param) | `input.Conversation` | Conversation settings |
| `maxExplorationRounds` | `config.MaxExplorationRounds` | |
| `maxToolsPerRound` | `config.MaxToolsPerRound` | |
| `maxErrorRetries` | `config.MaxErrorRetries` | |
| `safetyMode` | `config.SafetyMode` | |
| `contextWindow` | `config.ContextWindow` | |
| `userMessage` | `input.UserMessage` | |
| `skillsContent` | `input.SkillsContent` | |
| `onStream` | `input.OnStream` | |
| `explorationResults` (local) | keep as local var, unchanged | |
| `pendingFinalResult.respondText` | keep on struct, unchanged | |
| `transcriptActions` (local) | keep as local var, unchanged | |

3. **Replace direct DB/model calls:**

| Old Call | New Call |
|---|---|
| `GetAgentLoopConfig()` | (use `config.AgentConfig` — already loaded by orchestrator) |
| `buildTools(cfg, conversation.VizEnabled, schema, dbConnection)` | `BuildTools(config.AgentConfig, input.Conversation.VizEnabled, input.Schema, input.DBConnection)` |
| `executeSQLWithMode(dbConnection, args.SQL, args.IsExploration)` | `a.QueryExecutor.Execute(args.SQL, args.IsExploration)` |
| `validateExplorationQuery(args.SQL, safetyMode)` | RELOCATED, not removed — moves inside `QueryExecutorAdapter.Execute` (see Phase 3). The loop no longer calls it directly, but it MUST still be called, now by the executor, before `isExploration:true` queries reach a driver. Verified by `TestQueryExecutorAdapter_ExplorationSafetyMode` in Phase 9. |
| `handleRespond(query, text, conversationID, explorationResults, transcript)` | `a.OutputHandler.EmitFinalResponse(...)` — build FinalResponse and call |
| `renderToolQueryResults(query, sql, results, chartConfig, dbConnection, conversation, explorationResults, transcript, summary, respondText)` | `a.OutputHandler.EmitFinalResponse(...)` — same path, with all fields |
| `storeTechDetail(conversationID, td)` | (absorbed into OutputHandler — the adapter writes tech details) |
| `models.DB.Exec("UPDATE conversation_messages SET tool_transcript = ? ...")` | (REMOVED — OutputHandler handles this) |
| `UpdateQueryStatus(query.ID, ...)` | (REMOVED — OutputHandler handles this) |
| `CreateConversationMessage(...)` | (REMOVED — OutputHandler handles this) |

4. **Build LoopInput internally:**
   The loop needs conversation history. At the start of `Run()`, read it:
   ```go
   history, err := a.ConversationStore.GetMessages(input.ConversationID)
   // (if orchestrator didn't pre-load it)
   ```
   **Decision:** The orchestrator pre-loads history and puts it in `input.History`.
   The loop does NOT call `ConversationStore` directly — only the orchestrator does.

5. **Error handling replacement:**
   The old loop calls `handleClarification` and `renderSQLError` directly. The
   new loop calls `a.OutputHandler.EmitClarification(...)` and
   `a.OutputHandler.EmitSQLWarning(...)`.

### Files Created

#### `pkg/engine/loop.go`

~600 lines. Contains:
- `AgenticLoop` struct
- `Run()` method (the copied + adapted `runAgenticLoop`)
- Unexported helpers: `pendingFinalResult` struct, `roundKind` type, and any
  other unexported types currently in `agentic_loop.go` that are only used by
  the loop.

#### `pkg/engine/response.go`

~150 lines. Contains:
- `buildFinalResponse` — constructs `FinalResponse` from the loop's internal state
- Any helpers needed by the OutputHandler path that were previously in
  `handleRespond` / `renderToolQueryResults`.

### Files NOT Created (Stay in services/ for now)

The orchestrator (`engine/orchestrator.go`) is built in Phase 6. The old
`runAgenticLoop` in `agentic_loop.go` is untouched — it still works and is
still called by `discussion_engine.go`.

### Verification

```bash
go build ./...    # Must succeed. new loop compiles but is unused.
go vet ./...      # Must pass.
```

**Commit message:** `phase 5: create AgenticLoop.Run with injected dependencies`

---

## Phase 6: New Orchestrator.ProcessMessage

**Goal:** Create `engine/orchestrator.go` with `Orchestrator` struct and
`ProcessMessage()` method. This is a thin (~50 line) pipeline that wires all
the interfaces together. It exists alongside the old `ProcessUserMessageWithContext`.

**Risk: MEDIUM.** The orchestrator is the integration point. If any adapter
method is wrong, the pipeline fails. But since we're not wiring it to callers
yet (Phase 7), we can test it in isolation.

### Files Created

#### `pkg/engine/orchestrator.go`

```go
package engine

import (
    "context"
    "fmt"
    "log"
    "time"
)

// Orchestrator coordinates the full pipeline.
type Orchestrator struct {
    Conversations  ConversationStore
    Providers      LLMProviderStore
    DataSources    DataSourceStore
    Introspector   SchemaIntrospector
    Config         ConfigurationProvider
    Loop           *AgenticLoop

    // NewLLMClient is a factory function that creates an LLMClient from
    // provider config. The orchestrator doesn't know how to create clients
    // — the caller injects this. This keeps the engine package free of
    // provider-specific knowledge.
    NewLLMClient    func(LLMProviderMeta) (LLMClient, error)
}

// ProcessMessage is the entry point called by app.go and headless_handlers.go.
func (o *Orchestrator) ProcessMessage(
    ctx context.Context,
    conversationID uint,
    userMessage string,
    onPhase func(string),
    onStream func(StreamEvent),
) (*LoopOutput, error) {
    // ── Step 1: Load conversation ──
    conv, err := o.Conversations.GetByID(conversationID)
    if err != nil {
        return nil, fmt.Errorf("failed to get conversation: %w", err)
    }

    // ── Step 2: Load history ──
    history, err := o.Conversations.GetMessages(conversationID)
    if err != nil {
        return nil, fmt.Errorf("failed to get messages: %w", err)
    }

    // ── Step 3: Save user message ──
    if _, err := o.Conversations.CreateMessage(conversationID, NewMessage{
        Role:    "user",
        Content: userMessage,
    }); err != nil {
        return nil, fmt.Errorf("failed to save user message: %w", err)
    }

    // ── Step 4: Load provider ──
    var providerMeta LLMProviderMeta
    if conv.LLMProviderID != nil {
        pm, err := o.Providers.GetByID(*conv.LLMProviderID)
        if err != nil {
            return nil, fmt.Errorf("failed to get LLM provider: %w", err)
        }
        providerMeta = *pm
    } else {
        pm, err := o.Providers.GetDefault()
        if err != nil {
            return nil, fmt.Errorf("failed to get default LLM provider: %w", err)
        }
        providerMeta = *pm
    }

    // ── Step 5: Load data source ──
    var dsMeta DataSourceMeta
    if conv.DataSourceID != nil {
        dm, err := o.DataSources.GetByID(*conv.DataSourceID)
        if err != nil {
            return nil, fmt.Errorf("failed to get data source: %w", err)
        }
        dsMeta = *dm
    } else {
        dm, err := o.DataSources.GetDefault()
        if err != nil {
            return nil, fmt.Errorf("failed to get default data source: %w", err)
        }
        dsMeta = *dm
    }

    // ── Step 6: Fetch schema ──
    var schema *DataSchema
    schema, err = o.Introspector.GetSchema(dsMeta)
    if err != nil {
        log.Printf("[Orchestrator] Failed to fetch schema: %v", err)
        return nil, fmt.Errorf("failed to fetch database schema: %w", err)
    }

    // ── Step 7: Load config ──
    skillsContent, _ := o.Config.GetEnabledSkillsContent(conversationID)
    agentConfig, _ := o.Config.GetAgentLoopConfig()

    // ── Step 8: Build LoopConfig ──
    loopConfig := LoopConfig{
        MaxExplorationRounds: dsMeta.MaxExplorationRounds,
        MaxToolsPerRound:     dsMeta.MaxToolsPerRound,
        MaxErrorRetries:      dsMeta.MaxFinalRetries,
        TotalRoundCap:        dsMeta.MaxExplorationRounds + dsMeta.MaxFinalRetries + 4,
        SafetyMode:           ParseExplorationSafety(dsMeta.ExplorationSafety),
        ContextWindow:        providerMeta.ContextWindow,
        ModelName:            providerMeta.Model,
        VizEnabled:           conv.VizEnabled,
        Summarize:            conv.Summarize,
        StreamingEnabled:     conv.StreamingEnabled,
        AgentConfig:          agentConfig,
    }
    if loopConfig.MaxExplorationRounds <= 0 {
        loopConfig.MaxExplorationRounds = 2
    }
    if loopConfig.MaxErrorRetries <= 0 {
        loopConfig.MaxErrorRetries = 2
    }

    // ── Step 9: Apply context limit ──
    const maxContextHardCap = 15
    limit := conv.MaxContextMessages
    if limit <= 0 || limit > maxContextHardCap {
        limit = maxContextHardCap
    }
    if len(history) > limit {
        history = history[len(history)-limit:]
    }

    // ── Step 10: Build LLM messages ──
    messages := BuildToolLlmMessages(
        userMessage, history, schema, dsMeta,
        conv.VizEnabled, skillsContent,
    )

    // ── Step 11: Create LLM client ──
    client, err := o.NewLLMClient(providerMeta)
    if err != nil {
        return nil, fmt.Errorf("failed to create LLM client: %w", err)
    }
    // Inject into the loop (or pass as parameter)
    o.Loop.LLMClient = client

    // ── Step 12: Set timeout ──
    timeoutSecs := o.Config.GetTimeoutSeconds("pipeline_timeout_seconds", 180)
    ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
    defer cancel()

    if onPhase != nil {
        onPhase("Thinking with LLM...")
    }

    // ── Step 13: Build LoopInput and run ──
    input := LoopInput{
        UserMessage:    userMessage,
        ConversationID: conversationID,
        QueryID:        0, // the loop creates its own query record via OutputHandler
        Conversation:   *conv,
        History:        history,
        Schema:         schema,
        DBConnection:   dsMeta,
        SkillsContent:  skillsContent,
        OnStream:       onStream,
    }

    output, err := o.Loop.Run(ctx, input, loopConfig)
    if err != nil {
        return nil, err
    }
    return output, nil
}
```

**NOTE:** `QueryID` is obtained from `o.Loop.OutputHandler.CreateQueryRecord()`
in Step 13 before calling Run. This replaces the old `services.CreateQuery()`
call that was in `ProcessUserMessageWithContext` line ~110–117. The orchestrator
no longer accesses `models.DB` directly — query tracking is the OutputHandler's
responsibility.

### Verification

```bash
go build ./...    # Must succeed
go vet ./...      # Must pass
```

**Commit message:** `phase 6: create Orchestrator.ProcessMessage`

---

## Phase 7: Swap Callers

**Goal:** Wire `app.go` and `headless_handlers.go` to use the new
`Orchestrator.ProcessMessage()` instead of `services.ProcessUserMessageWithContext()`.
This is the cutover.

**Risk: HIGH.** This is the moment when the new code path becomes the only
code path. If anything is wrong, the app won't produce answers.

### Pre-Swap Checklist

Before touching app.go or headless_handlers.go:

1. **Write a smoke test function** in a new file `pkg/engine/smoke_test.go`
   (build tag: `//go:build ignore` so it doesn't run in CI):
   - Instantiate `Orchestrator` with mock stores
   - Call `ProcessMessage()` with a test message
   - Assert `LoopOutput` is non-nil
   - This proves the wiring works

2. **Re-read every adapter** for field mapping errors:
   - For each Meta type, verify every field is mapped
   - For each adapter method, verify it calls exactly one service function
   - Verify nil handling (if service returns nil + nil, adapter returns error)

3. **Diff the old and new orchestrator**:
   - Compare old `ProcessUserMessageWithContext` with new `Orchestrator.ProcessMessage`
   - Verify step 1–13 are present in both in the same order
   - Verify context limit logic, timeout logic, error handling

### Changes to `app.go`

**Old (line 123–155):**
```go
func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
    ctx, cancel := context.WithCancel(context.Background())
    a.registerCancel(conversationID, cancel)
    defer a.unregisterCancel(conversationID)

    onStream := func(ev services.StreamEvent) { ... }
    err := services.ProcessUserMessageWithContext(ctx, conversationID, userMessage,
        func(phase string) { ... }, onStream)
    // ... cancellation handling ...
}
```

**New:**
```go
func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
    ctx, cancel := context.WithCancel(context.Background())
    a.registerCancel(conversationID, cancel)
    defer a.unregisterCancel(conversationID)

    onStream := func(ev services.StreamEvent) {
        if a.ctx != nil {
            runtime.EventsEmit(a.ctx, "llm:stream", map[string]interface{}{
                "conversation_id": conversationID,
                "event":           ev,
            })
        }
    }
    
    // Build orchestrator (one-time setup — could be stored on App struct)
    orchestrator := &engine.Orchestrator{
        Conversations:  &adapters.SqliteConversationStore{},
        Providers:      &adapters.SqliteLLMProviderStore{},
        DataSources:    &adapters.SqliteDataSourceStore{},
        Introspector:   &adapters.SchemaIntrospectorAdapter{},
        Config:         &adapters.SqliteConfigProvider{},
        NewLLMClient: func(meta engine.LLMProviderMeta) (engine.LLMClient, error) {
            // Convert engine.LLMProviderMeta → models.LLMProvider
            // Call services.NewLLMClient
            // Return the client (which satisfies engine.LLMClient)
        },
        NewQueryExecutor: func(meta engine.DataSourceMeta, safetyMode engine.ExplorationSafetyMode) (engine.QueryExecutor, error) {
            // Convert engine.DataSourceMeta → models.DataSource
            // Return &adapters.QueryExecutorAdapter{DataSource: modelsDS, SafetyMode: safetyMode}
        },
        Loop: &engine.AgenticLoop{
            OutputHandler: &adapters.SqliteOutputHandler{},
            // QueryExecutor is set per-pipeline-run by the orchestrator via
            // NewQueryExecutor — see Step 5b below. It is NEVER shared across
            // conversations or requests, and it is NEVER constructed without
            // an explicit SafetyMode, so the exploration complexity gate
            // cannot be silently omitted by a caller that forgets to set it.
        },
    }
    
    _, err := orchestrator.ProcessMessage(ctx, conversationID, userMessage,
        func(phase string) {
            if a.ctx != nil {
                runtime.EventsEmit(a.ctx, "processingPhase", phase)
            }
        },
        onStream,
    )
    
    if a.ctx != nil {
        if errors.Is(err, context.Canceled) {
            runtime.EventsEmit(a.ctx, "processingCancelled", map[string]interface{}{
                "conversation_id": conversationID,
            })
            return nil
        } else {
            runtime.EventsEmit(a.ctx, "processingComplete")
        }
    }
    return err
}
```

**QueryExecutor construction note (SAFETY-CRITICAL):** `QueryExecutor` is
constructed fresh for every pipeline run via the `NewQueryExecutor` factory
on `Orchestrator`, mirroring `NewLLMClient`. This is not just a style choice
— it is the mechanism that guarantees `SafetyMode` can never be left at its
Go zero value (`ExplorationRelaxed == 0`) by accident. The factory signature
requires the caller to pass `safetyMode` explicitly:

```go
type Orchestrator struct {
    // ...
    NewQueryExecutor func(meta DataSourceMeta, safetyMode ExplorationSafetyMode) (QueryExecutor, error)
}
```

Add to the orchestrator's `ProcessMessage()`, immediately after Step 8
(building `LoopConfig`, which already resolves `SafetyMode` from
`dsMeta.ExplorationSafety` via `ParseExplorationSafety`):

```go
// Step 8b: Build a fresh QueryExecutor for this pipeline run, bound to
// this data source AND this data source's configured safety mode. Never
// reuse a QueryExecutor across conversations/data sources/requests.
queryExec, err := o.NewQueryExecutor(dsMeta, loopConfig.SafetyMode)
if err != nil {
    return nil, fmt.Errorf("failed to build query executor: %w", err)
}
o.Loop.QueryExecutor = queryExec
```

Phase 9 must include a test (`TestQueryExecutorAdapter_ExplorationSafetyMode`)
that directly instantiates `adapters.QueryExecutorAdapter` with each of the
three `SafetyMode` values and asserts complexity-violating queries (a JOIN
under strict mode, a subquery under moderate mode) are rejected — this is
the regression test for the gap identified in code review before this plan
was finalized.

### Changes to `headless_handlers.go`

Same pattern as `app.go`. The `processMessage` method in `headless.go` calls
`services.ProcessUserMessageWithContext` — replace with `orchestrator.ProcessMessage()`.

**Recommendation:** For headless, construct the Orchestrator once in
`newHeadlessServer()` and store it on the `headlessServer` struct. Then
`processMessage` just calls `s.orchestrator.ProcessMessage(...)`.

### Verification

```bash
go build ./...                 # Must succeed
wails build                    # Must produce a working binary

# Manual smoke test:
# 1. Launch the built binary
# 2. Create a conversation
# 3. Ask: "What tables are in the database?"
# 4. Verify answer is produced
# 5. Compare with baseline conversation from Phase 0
```

**Commit message:** `phase 7: swap app.go and headless to use Orchestrator`

---

## Phase 8: Cleanup

**Goal:** Delete the old code paths. Remove `agentic_loop.go` and
`discussion_engine.go`. Remove moved functions from `sql_execution.go`.

**Risk: LOW.** The old code is unreachable — nothing imports or calls it
(since Phase 7 swapped all callers). Deleting it is safe, but the compiler
will catch any remaining references.

### Files Deleted

1. **`pkg/services/agentic_loop.go`** — all logic moved to `engine/loop.go`,
   `engine/tools.go`, `engine/prompts.go`, `engine/response.go`.
2. **`pkg/services/discussion_engine.go`** — orchestration moved to
   `engine/orchestrator.go`; rendering moved to `engine/rendering.go`; chart
   resolution moved to `engine/charts.go`; `ExplorationResult` type alias
   stays in services if still used by export.go or other files (keep as alias).

### Functions Removed from `sql_execution.go`

These functions were moved to `engine/safety.go` and `engine/rendering.go` in
Phase 2. Their bodies in `sql_execution.go` should now be empty or deleted:

- `validateReadOnlySQL` → DELETE (replaced by `engine.ValidateReadOnlySQL`)
- `validateExplorationQuery` → DELETE (replaced by `engine.ValidateExplorationQuery`)
- `stripSQLComments` → DELETE (replaced by `engine.StripSQLComments`)
- `containsSelectIntoTable` → DELETE
- `insideSingleQuotedString` → DELETE
- `renderToHTML` → DELETE (replaced by `engine.RenderResultTable`)
- `renderMarkdown` → DELETE (replaced by `engine.RenderMarkdown`)
- `fencedCodeRe` → DELETE

Keep in `sql_execution.go`:
- `executeSQL`, `executeSQLWithMode`, `executeNativeQuery` — the actual execution
- `classifyErrorCategory` — used by error rendering
- Type aliases for `QueryResult`, `ExplorationSafetyMode`
- Any helper functions not moved

### Type Alias Cleanup in `services/`

Remove any type aliases that are no longer needed because the types are only
used from engine/. But keep aliases for types still referenced in services/:

- `QueryResult` — keep (used by `executeSQLWithMode` return type)
- `ExplorationSafetyMode` — keep (used by `executeSQLWithMode`)
- `DataSchema` etc. — keep (used by `GetDataSchema`, drivers)
- `LLMClient`, `ChatMessage`, `Tool`, `StreamEvent` — keep (used by providers, factory)
- `ToolCallSummary`, `TranscriptAction`, `ToolTranscript`, `TechDetail` — MAY DELETE
  (only used by agentic_loop.go which is deleted; if export.go or other files
  reference them, keep as aliases)

### Imports Cleanup

Remove unused imports from every modified file. `go vet` will catch these.

### Verification

```bash
go build ./...    # Must succeed — compiler verifies no dangling references
go vet ./...      # Must pass
go test ./...     # Must pass

# Verify line count reduction:
wc -l pkg/services/discussion_engine.go pkg/services/agentic_loop.go
# Both should be deleted (0 lines or file not found)

wc -l pkg/services/sql_execution.go
# Should be ~450 lines (down from 875 — safety + rendering moved)
```

### Documentation Housekeeping (AGENT_READ_FIRST.md §1.2/§1.3/§4.2/§7 — required, not optional)

`AGENT_READ_FIRST.md` names `discussion_engine.go` and `agentic_loop.go` by
path in multiple places, and this phase deletes both files. The charter's own
closing instruction is explicit: "Keep this document current... A stale
charter is worse than no charter." This phase must update, in the same
commit:

- [ ] **§1.2 Key Components table** — "Discussion Engine" row's file path
      (`pkg/services/discussion_engine.go`) → `pkg/engine/orchestrator.go`.
      Update the description if orchestration responsibilities changed.
- [ ] **§1.3 heading and body** — every reference to
      `pkg/services/agentic_loop.go` / `runAgenticLoop` → `pkg/engine/loop.go`
      / `AgenticLoop.Run`. The tool table, one-shot finality description, and
      the "be conservative when modifying..." paragraph's file reference all
      need the path update; the *content* of that warning is unchanged and
      now applies to `pkg/engine/loop.go`.
- [ ] **§1.4 SQL Execution Safety** — clarify that `ValidateReadOnlySQL` and
      `ValidateExplorationQuery` now live in `pkg/engine/safety.go`, called
      from `pkg/services/adapters/query_executor.go`'s `QueryExecutorAdapter`
      — not directly inside `sql_execution.go` anymore.
- [ ] **§4.2 "When to Pause" trigger list** — add `pkg/engine/loop.go`,
      `pkg/engine/orchestrator.go`, and `pkg/engine/safety.go` to (or in
      place of) the `discussion_engine.go`/`agentic_loop.go`/`sql_execution.go`
      trigger list, since these are now the files that carry the same risk
      profile the original list was protecting.
- [ ] **§7 Quick Reference table** — update "Core query flow (orchestration)"
      and "Agentic tool-calling loop" rows to the new file paths.
- [ ] Cross-check `TESTING_SUITE.md`, `TECH_REVIEW.md`, and any other
      `documentation/*.md` file with a hardcoded reference to
      `discussion_engine.go` or `agentic_loop.go` (`grep -rln
      "discussion_engine.go\|agentic_loop.go" documentation/`) and update or
      annotate as stale.

**Commit message:** `phase 8: delete old agentic_loop.go and discussion_engine.go; update AGENT_READ_FIRST.md references`

---

## Phase 9: Engine Tests

**Goal:** Add table-driven tests for the engine package, using mock
implementations of every port. This proves the isolation is real.

**Risk: NONE.** New files only. No production code changes.

### Files Created

#### `pkg/engine/mock_test.go`

Mock implementations of every interface:

```go
package engine

// MockLLMClient records calls and returns scripted responses.
type MockLLMClient struct {
    ChatCompletionCalls             []MockChatCall
    ChatCompletionWithPayloadCalls  []MockChatCall
    ChatCompletionWithToolsCalls    []MockToolsCall
    ChatCompletionWithToolsStreamingCalls []MockToolsStreamingCall
}

// Each method appends to the call log and returns the next scripted response.

// MockQueryExecutor returns predefined results or errors.
type MockQueryExecutor struct {
    ExecuteCalls []MockExecuteCall
}

// MockOutputHandler records what was emitted.
type MockOutputHandler struct {
    FinalResponses []FinalResponse
    SQLWarnings    []MockSQLWarning
    Clarifications []Clarification
}

// MockConversationStore returns predefined conversations and messages.
// MockLLMProviderStore, MockDataSourceStore, MockSchemaIntrospector,
// MockConfigurationProvider — same pattern.
```

#### `pkg/engine/loop_test.go`

Test cases:

| Test | What it exercises |
|---|---|
| `TestRespondNoQuery` | Model calls `respond_to_user` with text only. No queries. |
| `TestSingleExplorationThenFinal` | Model explores once, then runs final query, then responds. |
| `TestMultipleExplorationsThenFinal` | Model explores 3 times, then final query + respond. |
| `TestFinalQueryWithChart` | Model runs final query + `render_chart`. |
| `TestErrorRetrySuccess` | First query fails, LLM retries, second succeeds. |
| `TestErrorRetryExhausted` | All retries fail → `EmitSQLWarning` called. |
| `TestOneShotFinality` | Model tries `query_database` after final query → rejected. |
| `TestRoundCapExhaustion` | Loop hits `TotalRoundCap` → `EmitClarification("loop_exhausted")`. |
| `TestContextOverflow` | Prompt exceeds 90% of context window → `EmitClarification("context_overflow")`. |
| `TestEmptyLLMResponse` | LLM returns no content, no tool calls → `EmitClarification("empty_truncated")`. |
| `TestStreamingVsBlocking` | Identical input, one with OnStream set, one nil → identical LoopOutput. |
| `TestCancelMidLoop` | Context cancelled during loop → returns context.Canceled. |
| `TestSafetyRejection` | `QueryExecutor` returns read-only violation → error fed back to LLM. |
| `TestSummarizationEnabled` | `Summarize: true` in config → summary present in FinalResponse. |
| `TestExplorationTrace` | Multiple explorations → all appear in FinalResponse.ExplorationTrace. |

#### `pkg/engine/safety_test.go`

Move the 5 existing tests from `pkg/services/sql_execution_test.go` and add
more:

| Test | What it exercises |
|---|---|
| `TestValidateReadOnlySQL_Select` | Valid SELECT passes. |
| `TestValidateReadOnlySQL_Insert` | INSERT blocked. |
| `TestValidateReadOnlySQL_Update` | UPDATE blocked. |
| `TestValidateReadOnlySQL_Delete` | DELETE blocked. |
| `TestValidateReadOnlySQL_Drop` | DROP blocked. |
| `TestValidateReadOnlySQL_Create` | CREATE blocked. |
| `TestValidateReadOnlySQL_Alter` | ALTER blocked. |
| `TestValidateReadOnlySQL_Truncate` | TRUNCATE blocked. |
| `TestValidateReadOnlySQL_Merge` | MERGE blocked. |
| `TestValidateReadOnlySQL_CTE` | WITH ... SELECT passes. |
| `TestValidateReadOnlySQL_ParenthesizedSelect` | `(SELECT ...) UNION` passes. |
| `TestValidateReadOnlySQL_SelectInto` | `SELECT ... INTO table` blocked. |
| `TestValidateReadOnlySQL_SelectIntoVariable` | `SELECT ... INTO @var` passes. |
| `TestValidateReadOnlySQL_CommentStripping` | `/* DELETE */ SELECT` passes. |
| `TestValidateExplorationQuery_Strict` | Joins/subqueries blocked in strict mode. |
| `TestValidateExplorationQuery_Moderate` | Subqueries blocked, simple joins allowed. |
| `TestValidateExplorationQuery_Relaxed` | Everything passes (read-only enforced). |

#### `pkg/engine/rendering_test.go`

| Test | What it exercises |
|---|---|
| `TestRenderResultTable_Basic` | Simple table HTML output. |
| `TestRenderResultTable_Empty` | Empty columns/rows produces empty placeholder. |
| `TestRenderResultTable_SpecialChars` | HTML escaping in cell values. |
| `TestRenderMarkdown_Basic` | Bold, italic, code blocks. |
| `TestRenderMarkdown_CodeFence` | Fenced code block extraction. |
| `TestRenderMarkdown_Nil` | Nil input → empty string. |

### Verification

```bash
go test ./pkg/engine/... -v    # All tests pass
go test ./...                   # All existing tests still pass
```

**Commit message:** `phase 9: add engine tests with mock implementations`

---

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| **Field mapping error in Meta types** | Medium | High — silent data loss or wrong behavior | Phase 3 adapter audit checklist; every field mapped explicitly by name |
| **StreamEvent type incompatibility** | Low | High — streaming breaks | Type alias (`services.StreamEvent = engine.StreamEvent`) ensures identical types |
| **Read-only invariant (§0) lost** | Low | Critical — data source write | `QueryExecutorAdapter.Execute` delegates to `executeSQLWithMode`, which still calls `engine.ValidateReadOnlySQL` unconditionally for every query. This is unchanged from today's call chain — low risk. |
| **Exploration safety mode (§1.4) enforcement gap** | **High — flagged in code review, fix mandatory** | High — named safety control silently regresses (strict/moderate complexity limits stop applying; read-only itself is unaffected) | Today, `validateExplorationQuery` is called by `agentic_loop.go` BEFORE `executeSQLWithMode`, not inside it. If the extraction only wires `QueryExecutorAdapter.Execute` to call `executeSQLWithMode`, the complexity-mode gate has no caller once `agentic_loop.go` is deleted in Phase 8. **Mandatory fix:** `QueryExecutorAdapter` carries an explicit `SafetyMode` field (never a zero-value default) and calls `engine.ValidateExplorationQuery(sql, mode)` itself when `isExploration` is true, in addition to (not instead of) the read-only check inside `executeSQLWithMode`. See Phase 3's `QueryExecutorAdapter` definition and Phase 6's `NewQueryExecutor` factory. Regression-tested by `TestQueryExecutorAdapter_ExplorationSafetyMode` in Phase 9. This entry exists because an earlier draft of this plan marked the old call site as simply "REMOVED" without specifying where enforcement moved to — do not repeat that mistake in implementation. |
| **OutputHandler skips transcript persistence** | Medium | Medium — tech details toggle breaks | `EmitFinalResponse` explicitly includes transcript in conversation message metadata. Verified in Phase 9 test. |
| **AgentLoopConfig field mismatch** | Low | Medium — wrong prompt behavior | `AgentLoopConfig` is a flat struct of strings. Aliased or copied field-by-field in Phase 4. |
| **Orchestrator drops conversation settings** | Low | High — wrong provider/DS used | Step-by-step verification against old `ProcessUserMessageWithContext`. Diff the two functions. |
| **Race condition on QueryExecutor adapter** | Low | High — concurrent conversations collide | `QueryExecutorAdapter` is created per-pipeline-run (not shared). Verified by construction. |
| **Import cycle** | Medium | Medium — won't compile | Phase 1 structural check: engine/ imports nothing from services/ or models/. Services/ imports engine/ (one-way). |
| **`wails build` fails due to type changes** | Low | High — can't ship | Phase 7 includes `wails build` verification before committing. |
| **Frontend expects old message format** | Low | High — UI breaks | `EmitFinalResponse` produces the same HTML + metadata format as `handleRespond`/`renderToolQueryResults`. Phase 7 manual smoke test validates UI. |

---

## Rollback Strategy

### Per-Phase Rollback

Each phase is a single commit. Rollback is `git revert <commit>`. The app
returns to the previous working state with zero manual cleanup.

### Full Extraction Rollback (Post-Phase 7)

If Phase 7 is committed and the app doesn't work:

1. **Immediate:** `git revert` the Phase 7 commit. `app.go` returns to
   calling `services.ProcessUserMessageWithContext`. The old code is still
   present (Phases 1–6 only added files, never deleted them).
2. **Investigation:** The old code path is intact. Debug the issue.
3. **Re-attempt:** Fix the issue, re-commit Phase 7.

### Post-Phase 8 Rollback

Phase 8 deletes `agentic_loop.go` and `discussion_engine.go`. If a rollback
is needed after this:

1. `git revert` the Phase 8 commit (restores deleted files).
2. `git revert` the Phase 7 commit (restores old caller paths).
3. The intermediate commits (1–6) are additive and can stay — they don't
   affect the old code path.

### Binary Artifact Rollback

Before each phase, save a known-good binary:
```bash
wails build -o build/yourql-phase-N-backup
```

If the new binary breaks, the old binary is immediately available.

---

## Time Estimate

| Phase | Effort | Cumulative |
|---|---|---|
| 0: Pre-flight | 15 min | 15 min |
| 1: Types & interfaces | 1–2 hours | 2 hours |
| 2: Move pure functions | 2–3 hours | 5 hours |
| 3: Create adapters | 2–3 hours | 8 hours |
| 4: Move prompts & tools | 1–2 hours | 10 hours |
| 5: New AgenticLoop.Run | 3–4 hours | 14 hours |
| 6: New Orchestrator | 1–2 hours | 16 hours |
| 7: Swap callers | 30 min + testing | 17 hours |
| 8: Cleanup | 30 min | 18 hours |
| 9: Engine tests | 3–4 hours | 22 hours |

**Total: ~22 hours** (2–3 days full-time, or 1 week with testing).

---

## Decision Gates

After each phase, confirm before proceeding:

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes
- [ ] No files contain `// TODO: fix after extraction` or similar
- [ ] All moved code is character-for-character identical (diff verified for pure-function phases)
- [ ] Commit message follows `phase N: description` convention

After Phase 7, additionally:
- [ ] `wails build` succeeds
- [ ] Manual smoke test: create conversation, ask question, get answer
- [ ] Answer matches baseline from Phase 0 (content parity)
- [ ] Tech details toggle works (SQL visible)
- [ ] Chart renders (if applicable)
- [ ] Streaming works (if enabled)
- [ ] Cancellation works (stop button)
- [ ] Headless API smoke test: `curl POST /api/conversations/1/messages`
- [ ] Dark mode / light mode both render correctly
- [ ] **Multi-provider verification (AGENT_READ_FIRST.md §5.2 — mandatory,
      not optional, because Phase 5 moved the per-provider parsing logic):**
  - [ ] OpenAI: create conversation, ask a question requiring exploration + final query, verify tool calls parse and answer is produced
  - [ ] Anthropic: same test — different tool-calling message format
  - [ ] Ollama: same test — no API key, exercises the plain-text fallback path most heavily
  - [ ] Custom/local endpoint: same test — base URL variation
- [ ] **Multi-database verification (AGENT_READ_FIRST.md §5.3 — mandatory):**
  - [ ] SQLite: exploration + final query + verify read-only rejection of a deliberately-attempted write prompt
  - [ ] PostgreSQL or MySQL: same test
  - [ ] One `NativeQuerier` driver (BigQuery or Google Sheets): same test
- [ ] **Exploration safety mode regression check (the gap fixed in this
      review — verify the fix landed, don't just trust the docs):**
  - [ ] Set a data source's `ExplorationSafety` to `strict`; ask a question that would require a JOIN during exploration; verify the model receives a rejection (not a silently-executed JOIN)
  - [ ] Repeat for `moderate` with a subquery
  - [ ] Confirm via tech-details toggle that the rejection message reached the model, proving the check executed before Phase 8 deletes the old call site