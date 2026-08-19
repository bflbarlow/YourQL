# YourQL Testing — Execution Framework

**Date:** 2026-08-13  
**Project:** YourQL — How to run tests against blank-slate configurations  
**Companion to:** [`TESTING_SUITE.md`](TESTING_SUITE.md) (the *what* to test — scenarios, coverage, safety gates)  
**Status:** Proposal — no code implemented

> **ABSOLUTE RULE — FIND AND DOCUMENT ONLY, NEVER FIX.** This document
> describes how to run tests, capture results, and write bug reports. It
> never authorizes modifying `pkg/`, `frontend/src/`, `app.go`, `main.go`,
> or any other non-test, non-markdown file to "resolve" something the tests
> surface. A discovered bug is a finding to document — see
> `TESTING_SUITE.md`'s top banner for the full rule.

---

## 1. Overview

This document defines the *how* — the concrete execution framework for
automated testing of YourQL. It covers:

- The anatomy of a blank-slate application database (`~/.yourql/yourql.db`)
- How to seed configs (LLM providers, data sources, skills, discussion
  defaults, agent loop overrides) from scratch
- How to create conversations with specific settings and send messages
- How to extract and analyze the assistant's response for correctness
- The data structures, function calls, and SQL queries needed at every step

The goal is a repeatable, deterministic harness that exercises the full
pipeline from "blank DB" → "config" → "conversation" → "message" →
"analyze" without requiring a real LLM API or an external database server.

### 1.1 The Harness Architecture at a Glance

```
┌─────────────────────────────────────────────────────┐
│  Test runner (one config × scenario)                │
│                                                     │
│  1. Create temp directory & point models.DB at it   │
│  2. Run migrate() to get fresh schema               │
│  3. Seed config (provider, data source, etc.)       │
│  4. Create a conversation with specific settings    │
│  5. Send a user message ("How many customers?")     │
│  6. Capture the assistant response & query log      │
│  7. Analyze: was the answer correct/complete/safe?  │
│  8. Record PASS/FAIL/SKIP + evidence in report      │
│  9. Tear down (close DB, delete temp dir)           │
└─────────────────────────────────────────────────────┘
```

### 1.2 Where This Harness Should Live: In-Repo vs. a Separate Project

Before writing any of the code below, decide where it lives. This section
answers that question, and the recommendation below is what the rest of this
document assumes (`pkg/testing/harness.go` plus `_test.go` files alongside
the packages under test, all inside the YourQL repo).

#### Option A — Roll it into the YourQL repository (recommended)

| Pros | Cons |
|---|---|
| Direct access to unexported internals — `migrate()` (package `models`), and any other package-private helper — without adding an exported wrapper for every one of them | The global `models.DB` means test packages must run sequentially (already required by §7.3); true parallel test execution needs more isolation work regardless of repo location |
| The two additive hooks this framework needs (`ConnectDatabaseAt`, `SetTestLLMClient`/the `"test"` provider case) are small, one-line-per-hook additions that live and get reviewed alongside the code they test | Test-only code (harness types, `FakeLLMClient`) sits in the same repo as production code — must be kept in non-`_test.go`, clearly-named files (`pkg/testing/`) so it's obviously test infrastructure, not app functionality |
| A feature change and its test-scenario update ship in the same PR/commit — no cross-repo version drift | Slightly larger repo/module surface (a handful of new files, no new runtime dependency for the shipped binary since `_test.go` files and a `pkg/testing` package are never linked into `wails build` output) |
| Single `go test ./...` runs everything; no second clone, no `go mod replace` dance for contributors | |
| Enforcing “find and document only” (per `AGENT_READ_FIRST.md`, see the banner at the top of this document and of `TESTING_SUITE.md`) is a process/discipline matter, not a repo-boundary matter — a separate project would not make that rule any easier to enforce or any harder to violate | |

#### Option B — A separate project/module that imports YourQL

| Pros | Cons |
|---|---|
| Forces a clean, fully-exported API surface between the harness and the app | `migrate()` is still unexported — an external module would *still* need the same `ConnectDatabaseAt` addition inside this repo; the additive-hook cost is identical, just split across two repos instead of paid once here |
| Test dependencies (fakes, scenario code) never live in the main repo | They never touch the shipped binary either way — `_test.go` files and any `pkg/testing` package are excluded from `wails build` regardless of where they live |
| In principle, reusable by other projects | YourQL is a Wails desktop app built around a global `models.DB`, not a library designed for embedding. No realistic second consumer exists today — this benefit is speculative |
| | Cross-repo version drift: a YourQL code change that breaks the harness is only caught when someone remembers to update and re-test the external module, which weakens the “find bugs quickly” goal this whole framework exists for |
| | More contributor friction: two clones, two Go modules, `go mod replace` or a tagged-release dependency to test against local/in-progress YourQL changes |
| | Splits `AGENT_READ_FIRST.md` context across repos — an agent working in the external project would need to re-read YourQL's charter from a different checkout to stay aligned with §0's absolute rules, increasing the chance those rules get missed |

#### Recommendation

**Roll this into the YourQL repository.** Structure it as:

- `pkg/testing/harness.go` (and sibling files) — exported, reusable harness
  types: `TestConfig`, `Finding`, `MessageAnalysis`, `FakeLLMClient`,
  `setupBlankDB`, `seedConfig`, `sendAndCapture`, `recordFinding`,
  `writeReport`. Not a `_test.go` file, so it can be imported by test files
  in multiple packages.
- `pkg/models/database_test.go` — tests that need direct access to
  unexported `migrate()`.
- `pkg/services/*_test.go` — scenario runners that use `pkg/testing` to
  drive `ProcessUserMessage` end to end.

This keeps the two additive application changes (§8) as small, reviewable,
single-repo commits, keeps `AGENT_READ_FIRST.md` in scope for whoever writes
or runs the tests, and avoids inventing cross-repo infrastructure to solve a
reuse problem that doesn't currently exist. If YourQL is ever split into a
reusable library/CLI in the future, this harness can be extracted at that
point — but that is a speculative future need, not a reason to add
cross-repo complexity today.

---

## 2. Step-by-Step Execution

### 2.1 Creating a Blank-Slate Application Database

YourQL stores all app state in a single SQLite file at `~/.yourql/yourql.db`,
opened by `models.ConnectDatabase()`. The database path is derived from
`user.Current().HomeDir`, which makes it platform-dependent and fragile to
override. For testing, the harness must use a database it controls fully.

> **If the harness ends up as an independent, code-blind project** (per the
> "in-repo vs. separate project" discussion in §1.2), the options below
> (direct `models.DB` assignment, `ConnectDatabaseAt`) are **not available** —
> both require importing `YourQL/pkg/models`. An independent project instead
> needs the running YourQL application itself to expose a way to point at a
> blank database and restart. See
> [`SQLITE_DB_SWITCHER.md`](SQLITE_DB_SWITCHER.md) for the GUI-based
> create-and-switch mechanism, and
> [`HEADLESS_YOURQL.md`](HEADLESS_YOURQL.md) for the `-headless -db-path`
> launch mode and HTTP API that the harness actually calls. The two
> subsections below remain relevant only for the in-repo option
> (§1.2, Option A).

**Option A — Direct `models.DB` assignment (preferred, simplest, in-repo only).**

Since `models.DB` is a package-level variable, the harness can set it
directly:

```go
import (
    "database/sql"
    "os"
    "path/filepath"
    _ "modernc.org/sqlite"
)

// setupBlankDB creates a fresh SQLite database at tmpPath, runs migrations,
// and assigns the connection to models.DB. It returns the path and a
// teardown function. Caller is responsible for ensuring tmpPath does not
// already exist.
func setupBlankDB(t *testing.T) (dbPath string, teardown func()) {
    dir, _ := os.MkdirTemp("", "yourql-test-*")
    dbPath = filepath.Join(dir, "yourql.db")

    db, err := sql.Open("sqlite", dbPath+
        "?_busy_timeout=5000&_journal_mode=WAL")
    if err != nil {
        t.Fatalf("failed to open test db: %v", err)
    }
    db.SetMaxOpenConns(1)
    db.SetMaxIdleConns(1)

    models.DB = db

    // Run migrations. migrate() is unexported (package models), so this
    // block must live inside an _test.go file in package models, OR a small
    // exported helper like ConnectDatabaseAt(path) needs to be added.
    // See §Additive Helpers below.
    if err := migrate(); err != nil {
        t.Fatalf("failed to migrate test db: %v", err)
    }

    teardown = func() {
        db.Close()
        os.RemoveAll(dir)
    }
    return
}
```

**Option B — Additive helper `models.ConnectDatabaseAt(path)` (recommended, in-repo only).**

Add a single exported function to `pkg/models/database.go`:

```go
// ConnectDatabaseAt opens the given SQLite file, runs migrations, and stores
// the connection in the global DB var. Intended for test harnesses that need
// a blank-slate database at a specific path. This is additive — it does not
// change the behavior of ConnectDatabase() or the existing getDBPath().
func ConnectDatabaseAt(path string) error {
    var err error
    DB, err = sql.Open("sqlite", path+
        "?_busy_timeout=5000&_journal_mode=WAL")
    if err != nil {
        return fmt.Errorf("failed to open SQLite database: %w", err)
    }
    DB.SetMaxOpenConns(1)
    DB.SetMaxIdleConns(1)

    if err := DB.Ping(); err != nil {
        return fmt.Errorf("failed to ping SQLite database: %w", err)
    }

    if err := migrate(); err != nil {
        return fmt.Errorf("failed to run migrations: %w", err)
    }
    return nil
}
```

The harness then calls:

```go
func setupBlankDB(t *testing.T) (dbPath string, teardown func()) {
    dir, _ := os.MkdirTemp("", "yourql-test-*")
    dbPath = filepath.Join(dir, "yourql.db")
    if err := models.ConnectDatabaseAt(dbPath); err != nil {
        t.Fatalf("ConnectDatabaseAt: %v", err)
    }
    teardown = func() {
        models.DB.Close()
        models.DB = nil
        os.RemoveAll(dir)
    }
    return
}
```

Either way, each test run gets a completely blank slate — no providers, no
data sources, no conversations, no skills, no defaults.

### 2.2 Seeding Configuration (the Config Struct)

After the blank DB is ready, seed it with the configuration profile for
this test run. Every seed value goes through the existing service CRUD
functions, so the exact same code paths are exercised as in the real app.

```go
// TestConfig defines one configuration profile in the test matrix.
type TestConfig struct {
    Name string  // human-readable label, e.g. "sqlite+ollama strict"

    // LLM Provider
    LLMProvider struct {
        Name     string // e.g. "test-openai"
        Provider string // "openai" | "anthropic" | "ollama" | "local"
        Model    string // e.g. "gpt-4o-mini"
        BaseURL  string // optional custom endpoint
        APIKey   string // fake — never a real key
    }
    SetAsDefaultProvider bool

    // Data Source
    DataSource struct {
        Name     string
        Type     string // "sqlite"
        Database string // path to fixture file (seeded with known data)
        ConfigJSON string // JSON: ExplorationSafety, limits, business rules
    }
    SetAsDefaultSource bool

    // Skills — each skill is created then optionally enabled on the discussion
    Skills []struct {
        Name    string
        Content string // markdown
        Enabled bool   // enabled on the test discussion?
    }

    // Discussion Defaults (applied to every new discussion)
    DiscussionDefaults struct {
        MaxMessages        int
        MaxContextMessages int
        Summarize          bool
        VizEnabled         bool
        StreamingEnabled   bool
        TechDetails        bool
        ContextDetails     bool
    }

    // Agent Loop Config overrides (optional — nil/empty means use defaults)
    AgentLoopOverrides map[string]string  // key → value pairs

    // Per-discussion flags (applied after CreateConversation)
    Conversation struct {
        VizEnabled       bool
        Summarize        bool
        MaxMessages      int
        MaxContextMessages int
        StreamingEnabled bool
        TechDetails      bool
        ContextDetails   bool
    }
}
```

Seeding a config looks like:

```go
func seedConfig(t *testing.T, cfg *TestConfig) (providerID, dataSourceID uint) {
    // 1. LLM Provider
    provider, err := CreateLLMProvider(
        cfg.LLMProvider.Name,                         // name
        cfg.LLMProvider.Provider,                      // provider type
        cfg.LLMProvider.Model,                         // model
        cfg.LLMProvider.BaseURL,                       // base URL
        cfg.LLMProvider.APIKey,                        // API key
        cfg.SetAsDefaultProvider,                      // isDefault
        "",                                            // configJSON
        ptrTo(4096),                                   // maxTokens
    )
    if err != nil {
        t.Fatalf("CreateLLMProvider: %v", err)
    }
    providerID = provider.ID

    // 2. Data Source (SQLite fixture with known data)
    source, err := CreateDataSource(
        cfg.DataSource.Name,                            // name
        cfg.DataSource.Type,                            // type
        "", 0, cfg.DataSource.Database,                 // host, port, database
        "", "", "",                                     // username, password, sslMode
        cfg.DataSource.ConfigJSON,                      // configJSON
        "",                                             // extraJSON
        cfg.DataSource.Database,                        // filePath (for sqlite)
        "",                                             // fileType
    )
    if err != nil {
        t.Fatalf("CreateDataSource: %v", err)
    }
    dataSourceID = source.ID

    if cfg.SetAsDefaultSource {
        if err := SetDefaultDataSource(source.ID); err != nil {
            t.Fatalf("SetDefaultDataSource: %v", err)
        }
    }

    // 3. Skills
    for _, sk := range cfg.Skills {
        skill, err := CreateSkill(sk.Name, sk.Content)
        if err != nil {
            t.Fatalf("CreateSkill(%s): %v", sk.Name, err)
        }
        if sk.Enabled {
            // Skills are enabled per-conversation, done in §2.3 below
            _ = skill  // stored for later
        }
    }

    // 4. Discussion Defaults
    if defaults := &cfg.DiscussionDefaults; defaults != nil {
        setDefault("max_messages", fmt.Sprintf("%d", defaults.MaxMessages))
        setDefault("max_context_messages", fmt.Sprintf("%d", defaults.MaxContextMessages))
        setDefault("summarize", fmt.Sprintf("%t", defaults.Summarize))
        setDefault("viz_enabled", fmt.Sprintf("%t", defaults.VizEnabled))
        setDefault("streaming_enabled", fmt.Sprintf("%t", defaults.StreamingEnabled))
        setDefault("tech_details", fmt.Sprintf("%t", defaults.TechDetails))
        setDefault("context_details", fmt.Sprintf("%t", defaults.ContextDetails))
    }

    // 5. Agent Loop Config overrides
    for key, val := range cfg.AgentLoopOverrides {
        if err := SetAgentLoopConfigKey(key, val); err != nil {
            t.Fatalf("SetAgentLoopConfigKey(%s): %v", key, err)
        }
    }

    return
}

// setDefault writes a discussion_defaults row via the SetDiscussionDefault
// service function.
func setDefault(key, val string) {
    _ = SetDiscussionDefault(key, val)
}

func ptrTo[T any](v T) *T { return &v }
```

### 2.3 Creating a Conversation and Sending a Message

After seeding the config, create a conversation (which applies discussion
defaults) and then apply any per-discussion overrides.

```go
func createTestConversation(t *testing.T, cfg *TestConfig,
    providerID, dataSourceID uint) uint {

    // CreateConversationWithDefaults applies DiscussionDefaults from the DB
    conv, err := CreateConversationWithDefaults(
        "Test Discussion",
        &providerID,
        &dataSourceID,
    )
    if err != nil {
        t.Fatalf("CreateConversationWithDefaults: %v", err)
    }

    // Apply per-discussion overrides
    if cfg.Conversation.MaxMessages > 0 {
        _ = UpdateConversationMaxMessages(conv.ID, cfg.Conversation.MaxMessages)
    }
    if cfg.Conversation.MaxContextMessages > 0 {
        _ = UpdateConversationMaxContextMessages(conv.ID, cfg.Conversation.MaxContextMessages)
    }
    _ = UpdateConversationSummarize(conv.ID, cfg.Conversation.Summarize)
    _ = UpdateConversationVizEnabled(conv.ID, cfg.Conversation.VizEnabled)
    _ = UpdateConversationStreamingEnabled(conv.ID, cfg.Conversation.StreamingEnabled)
    _ = UpdateConversationTechDetails(conv.ID, cfg.Conversation.TechDetails)
    _ = UpdateConversationContextDetails(conv.ID, cfg.Conversation.ContextDetails)

    // Enable skills on the conversation
    skills, _ := ListSkills()
    for _, sk := range skills {
        for _, cfgSk := range cfg.Skills {
            if sk.Name == cfgSk.Name && cfgSk.Enabled {
                _ = SetConversationSkill(conv.ID, sk.ID, true)
            }
        }
    }

    return conv.ID
}
```

Sending a message uses the real `ProcessUserMessage` (with a background
context) or `ProcessUserMessageWithContext` (with a timed context):

```go
// sendAndCapture sends a user message and captures both the streaming events
// and the final state of the conversation after processing completes.
func sendAndCapture(t *testing.T, convID uint, message string) *MessageAnalysis {
    var streamEvents []StreamEvent

    // The streaming callback collects events. For a test, we often just need
    // to know the message was processed, but events can be useful for
    // verifying progress-phase emission.
    onStream := func(ev StreamEvent) {
        streamEvents = append(streamEvents, ev)
    }

    var phase string
    onPhase := func(p string) {
        phase = p
    }

    // ProcessUserMessage uses the global models.DB and the service layer.
    // For a deterministic test, the LLM client must be a FakeLLMClient.
    // See §3.
    err := ProcessUserMessage(convID, message, onPhase, onStream)
    if err != nil {
        return &MessageAnalysis{
            Error:      err.Error(),
            Phase:      phase,
            ConvID:     convID,
        }
    }

    // Fetch everything for analysis
    msgs, _ := GetConversationMessages(convID)
    queries, _ := GetQueriesByConversation(convID)

    return analyzeMessages(msgs, queries, streamEvents)
}
```

### 2.4 Analyzing Messages for Success

The core of the testing framework: given the messages and query log produced
by `ProcessUserMessage`, determine whether the answer was correct, safe, and
complete according to the test scenario's criteria.

```go
type MessageAnalysis struct {
    // Message fields
    AssistantMessageCount int
    LastAssistantContent  string  // raw Content of the last assistant message
    LLMContent            string  // raw LLM response (pre-HTML-rendering)
    SQLResults            string  // query results JSON/table
    Metadata              string  // metadata JSON from the message
    ToolTranscript        string  // tool call transcript

    // Query log fields
    Queries []models.Query
    QueryCount       int
    LastGeneratedSQL string
    LastQueryStatus  string   // "pending", "running", "success", "error", "cancelled"
    LastErrorMessage string
    LastTokenCount   int
    LastExecTimeMS   int

    // Streaming
    StreamEvents []StreamEvent
    Phase        string

    // Error
    Error   string
    ConvID  uint

    // Analysis flags (set by test-specific analysis functions)
    HasFinalAnswer        bool   // an assistant message was created
    HasResultData         bool   // SQLResults is non-empty and has rows
    AnswerContainsError   bool   // the assistant output indicates an error
    AnswerIsClarification bool   // the assistant asked a follow-up question
    ReadOnlyViolation     bool   // a write statement was executed
    CorrectnessScore      int    // 0=wrong, 1=partial, 2=correct (if known)
}

// analyzeMessages reads the conversation messages and query log and populates
// the analysis struct with whatever evidence it can extract.
func analyzeMessages(
    msgs []*models.ConversationMessage,
    queries []models.Query,
    streamEvents []StreamEvent,
) *MessageAnalysis {
    a := &MessageAnalysis{
        QueryCount: len(queries),
    }

    // Last assistant message
    for i := len(msgs) - 1; i >= 0; i-- {
        if msgs[i].Role == "assistant" {
            a.AssistantMessageCount++
            if a.LastAssistantContent == "" {
                a.LastAssistantContent = msgs[i].Content
                if msgs[i].LLMContent != nil {
                    a.LLMContent = *msgs[i].LLMContent
                }
                if msgs[i].SQLResults != nil {
                    a.SQLResults = *msgs[i].SQLResults
                }
                if msgs[i].Metadata != nil {
                    a.Metadata = *msgs[i].Metadata
                }
                if msgs[i].ToolTranscript != nil {
                    a.ToolTranscript = *msgs[i].ToolTranscript
                }
            }
        }
    }

    // Last query record
    if len(queries) > 0 {
        q := queries[len(queries)-1]
        a.LastQueryStatus = q.Status
        if q.GeneratedSQL != nil {
            a.LastGeneratedSQL = *q.GeneratedSQL
        }
        if q.ErrorMessage != nil {
            a.LastErrorMessage = *q.ErrorMessage
        }
        if q.TokensUsed != nil {
            a.LastTokenCount = *q.TokensUsed
        }
        if q.ExecutionTimeMS != nil {
            a.LastExecTimeMS = *q.ExecutionTimeMS
        }
    }

    // Heuristic: does the content contain a clarification or error?
    content := strings.ToLower(a.LastAssistantContent)
    if strings.Contains(content, "?") &&
        (strings.Contains(content, "could you") ||
         strings.Contains(content, "please") ||
         strings.Contains(content, "clarify") ||
         strings.Contains(content, "what do you mean")) {
        a.AnswerIsClarification = true
    }
    if strings.Contains(content, "error") ||
        strings.Contains(content, "sorry") ||
        strings.Contains(content, "i cannot") {
        a.AnswerContainsError = true
    }

    // Heuristic: does SQLResults have rows?
    if a.SQLResults != "" && !strings.Contains(a.SQLResults, "No rows") {
        a.HasResultData = true
    }

    // Heuristic: was a final answer actually delivered?
    if a.AssistantMessageCount > 0 && a.LastAssistantContent != "" {
        a.HasFinalAnswer = true
    }

    return a
}
```

### 2.5 Heuristic Correctness Checks

The harness cannot know the "right answer" in the general case (that would
require another LLM). Instead, it uses deterministic heuristics to flag
suspicious results as candidates for manual review:

| Heuristic | What It Detects | False Positive Risk |
|---|---|---|
| `HasResultData == false` | No data returned — maybe a bad query, empty DB, or a clarification | Low; empty results are usually suspicious |
| `HasFinalAnswer == false` | No assistant message — the pipeline failed silently | Very low; should always produce some output |
| `AnswerIsClarification` | LLM asked a clarifying question instead of answering | Medium; sometimes correct behavior |
| `AnswerContainsError` | LLM output contains "sorry"/"error"/"cannot" | Low; almost always a real failure |
| `LastQueryStatus == "error"` | SQL execution failed | Very low; the query log captures this reliably |
| `ReadOnlyViolation == true` | A write statement was executed against a data source | Very low; checked via `executeSQLWithMode` validation |
| Phase did not advance | The pipeline got stuck (no "Exploring schema…" → "Thinking with LLM…") | Low |
| `LastGeneratedSQL` is empty | The LLM never produced a SQL query when one was expected | Medium; some answers don't need SQL |

For known-answer tests (e.g. "How many customers are in the fixture?" with
a seeded table of 5 customers), a correctness assertion is straightforward:

```go
func assertRowCount(answer int, analysis *MessageAnalysis) bool {
    // Parse the "5" from text like "There are 5 customers." or from
    // the results table. A robust approach is to search for the number
    // in the content or in SQLResults.
    for _, word := range strings.Fields(analysis.LastAssistantContent) {
        if n, err := strconv.Atoi(word); err == nil && n == answer {
            return true
        }
    }
    return false
}
```

---

## 3. Deterministic Fakes (no real LLM or database needed)

The test harness uses fake implementations of the two key interfaces so that
tests are fully deterministic, fast, and require no network access.

### 3.1 FakeLLMClient

`LLMClient` is an interface with four methods. A fake returns a scripted
tool-call sequence or text response:

```go
type FakeLLMClient struct {
    // Script: each call to ChatCompletionWithTools returns the next entry.
    Script []ScriptedToolResponse

    // Fallback for ChatCompletion (non-tool path) — rarely used in the
    // current agentic loop, which always passes tools.
    TextFallback string

    callIndex int
}

type ScriptedToolResponse struct {
    // The *ChatMessage that ChatCompletionWithTools should return.
    Message *ChatMessage

    // Optional: if non-empty, cause ChatCompletionWithTools to return this
    // error instead of the Message.
    Error error

    // Optional: if true, signal that this call exhausts the script and the
    // harness should expect no more tool calls.
    IsFinal bool
}

func (f *FakeLLMClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
    return f.TextFallback, nil
}

func (f *FakeLLMClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
    resp := f.next()
    if resp.Error != nil {
        return "", "", "", resp.Error
    }
    if resp.Message != nil && len(resp.Message.Content) > 0 {
        return resp.Message.Content, "", "", nil
    }
    return "", "", "", nil
}

func (f *FakeLLMClient) ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (msg *ChatMessage, requestJSON, responseJSON string, err error) {
    resp := f.next()
    if resp.Error != nil {
        return nil, "", "", resp.Error
    }
    return resp.Message, "", "", nil
}

func (f *FakeLLMClient) ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (msg *ChatMessage, requestJSON, responseJSON string, err error) {
    // For simplicity, delegate to the blocking path and emit a Done event.
    msg, requestJSON, responseJSON, err = f.ChatCompletionWithTools(ctx, messages, tools)
    onEvent(StreamEvent{Type: StreamDone, FinishReason: "stop"})
    return
}

func (f *FakeLLMClient) next() ScriptedToolResponse {
    if f.callIndex >= len(f.Script) {
        return ScriptedToolResponse{
            Error: fmt.Errorf("FakeLLMClient: script exhausted at call %d", f.callIndex),
        }
    }
    resp := f.Script[f.callIndex]
    f.callIndex++
    return resp
}
```

A typical script for a "happy path" exploration-then-answer scenario:

```go
script := []ScriptedToolResponse{
    // Round 1: exploration query
    {
        Message: &ChatMessage{
            Role: "assistant",
            ToolCalls: []ToolCall{{
                ID:   "call_1",
                Function: ToolCallFunction{
                    Name: "query_database",
                    Arguments: `{"sql":"SELECT * FROM customers LIMIT 5","is_exploration":true}`,
                },
            }},
        },
    },
    // Round 2: final query
    {
        Message: &ChatMessage{
            Role: "assistant",
            ToolCalls: []ToolCall{{
                ID:   "call_2",
                Function: ToolCallFunction{
                    Name: "query_database",
                    Arguments: `{"sql":"SELECT COUNT(*) as cnt FROM customers","is_exploration":false}`,
                },
            }},
        },
    },
    // Round 3: respond to user (paired with the final query)
    {
        Message: &ChatMessage{
            Role: "assistant",
            ToolCalls: []ToolCall{{
                ID:   "call_3",
                Function: ToolCallFunction{
                    Name: "respond_to_user",
                    Arguments: `{"message":"There are 5 customers."}`,
                },
            }},
            FinishReason: "stop",
        },
    },
}
```

**Hooking the fake into the pipeline.** After seeding the config but before
sending a message, replace the real LLM provider with a fake one that the
test controls. The simplest approach: register a provider that points to a
`FakeLLMClient`. However, `NewLLMClient()` (in `llm_client.go`) uses a
`switch provider.Provider` that always returns a real client. The harness
must either:

- (a) Add a `"test"` case to the switch that returns a `FakeLLMClient` —
  additive, no behavior change for real providers.
- (b) Create the provider *and* override the client used in the agentic
  loop by adding an exported injection point
  (`SetLLMClientForTesting(client LLMClient)`).

Option (a) is simpler:

```go
// In llm_client.go, additive change:
case "test":
    return NewFakeLLMClient(provider)
```

Option (b) is cleaner and is the pattern used by many Go projects:

```go
// Package-level test-only setter.
var testLLMClient LLMClient

// SetTestLLMClient sets a test client that overrides NewLLMClient.
// Must only be called from test code.
func SetTestLLMClient(client LLMClient) {
    testLLMClient = client
}

// In NewLLMClient, first line:
if testLLMClient != nil {
    return testLLMClient, nil
}
```

### 3.2 FakeDBDriver / SQLite Fixtures

The `DBDriver` interface is implemented for each database type. For testing,
the safest approach is a real SQLite database at a known fixture file path:

```go
// createFixtureDB creates a temporary SQLite file with known data and
// returns the file path. The test seeds data exactly once, then the
// fixture is reused for any scenario that needs that data shape.
func createFixtureDB(t *testing.T) string {
    f, _ := os.CreateTemp("", "yourql-fixture-*.db")
    path := f.Name()
    f.Close()

    db, _ := sql.Open("sqlite", path)
    defer db.Close()

    db.Exec(`CREATE TABLE customers (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT,
        created_at TEXT DEFAULT (datetime('now'))
    )`)
    db.Exec(`INSERT INTO customers (name, email) VALUES
        ('Alice', 'alice@example.com'),
        ('Bob', 'bob@example.com'),
        ('Carol', 'carol@example.com'),
        ('Dave', 'dave@example.com'),
        ('Eve', 'eve@example.com')`)

    // Add orders, order_items, etc. as needed by the scenario.
    return path
}
```

The fixture path is then used as `DataSource.Database` (or `FilePath` for
SQLite) when seeding the test config.

---

## 4. Configuration Profiles (the Config Matrix)

Each configuration profile is a `TestConfig` struct literal. The test runner
iterates over a list of profiles and runs every scenario against each one.

```go
func TestConfigMatrix(t *testing.T) {
    profiles := []TestConfig{
        // Profile 1: Basic SQLite + Ollama, relaxed safety, no viz
        {
            Name: "basic-sqlite-relaxed",
            LLMProvider: struct{...}{
                Name: "test-provider",
                Provider: "test", // uses FakeLLMClient
                Model: "gpt-4o-mini",
            },
            DataSource: struct{...}{
                Name: "test-fixture",
                Type: "sqlite",
                Database: fixturePath,
                ConfigJSON: `{"exploration_safety":"relaxed"}`,
            },
            Conversation: struct{...}{
                VizEnabled: false,
                Summarize: false,
            },
        },
        // Profile 2: Strict safety, viz enabled, skills active
        {
            Name: "sqlite-strict-viz-skills",
            // ... different settings
        },
        // Profile 3: Summarize on, max context messages = 3
        // Profile 4: Agent loop config overridden
        // Profile 5: Discussion defaults applied, no explicit conversation overrides
        // etc.
    }

    for _, profile := range profiles {
        t.Run(profile.Name, func(t *testing.T) {
            dbPath, teardown := setupBlankDB(t)
            defer teardown()

            providerID, dataSourceID := seedConfig(t, &profile)
            convID := createTestConversation(t, &profile, providerID, dataSourceID)

            runScenarios(t, convID, &profile)
        })
    }
}
```

### 4.1 Minimal Configuration Matrix (Suggested First Pass)

These six profiles cover the widest behavioral variance with minimal setup:

| # | Name | Safety | Viz | Summ. | Max Msgs | Skills | Streaming |
|---|------|--------|-----|-------|----------|--------|-----------|
| 1 | `basic-relaxed` | relaxed | off | off | 10 | none | off |
| 2 | `strict-viz` | strict | on | off | 10 | none | off |
| 3 | `moderate-summarize` | moderate | off | on | 20 | none | off |
| 4 | `strict-viz-skills` | strict | on | off | 10 | 2 active | off |
| 5 | `relaxed-streaming` | relaxed | on | on | 10 | none | on |
| 6 | `strict-small-context` | strict | off | off | 3 | none | off |

Each profile also seeds a `"test"` provider (pointing at `FakeLLMClient`)
and a SQLite fixture with 5 customers, 10 orders, and 30 order items.

---

## 5. Scenarios

Each scenario is a function that takes a `convID`, a `*TestConfig`, and a
`*testing.T`, sends a specific message, analyzes the response, and calls
`t.Error`/`t.Fail` or records a finding.

### 5.1 Scenario: Direct Question, Known Answer

```go
func scenario_KnownAnswer(t *testing.T, convID uint, cfg *TestConfig) {
    analysis := sendAndCapture(t, convID, "How many customers are there?")

    if analysis.Error != "" {
        recordFinding(t, cfg.Name, "known-answer", "FAIL",
            fmt.Sprintf("pipeline error: %s", analysis.Error))
        return
    }
    if !analysis.HasFinalAnswer {
        recordFinding(t, cfg.Name, "known-answer", "FAIL",
            "no assistant message produced")
        return
    }
    if analysis.AnswerIsClarification {
        recordFinding(t, cfg.Name, "known-answer", "FAIL",
            "LLM asked a question instead of answering")
        return
    }
    if analysis.LastQueryStatus == "error" {
        recordFinding(t, cfg.Name, "known-answer", "FAIL",
            fmt.Sprintf("SQL error: %s", analysis.LastErrorMessage))
        return
    }
    recordFinding(t, cfg.Name, "known-answer", "PASS", "")
}
```

### 5.2 Scenario: Safety Gate — DML Rejected in Final Query

```go
func scenario_SafetyGateDMLFinal(t *testing.T, convID uint, cfg *TestConfig) {
    // The FakeLLMClient's script must produce a final query containing DML.
    // The test checks whether executeSQLWithMode rejects it.
    analysis := sendAndCapture(t, convID, "Delete all customers")

    if analysis.HasResultData == false &&
       (analysis.LastQueryStatus == "error" ||
        strings.Contains(analysis.LastAssistantContent, "only SELECT")) {
        recordFinding(t, cfg.Name, "safety-dml-final", "PASS",
            "DML rejected (correct)")
    } else if analysis.ReadOnlyViolation {
        recordFinding(t, cfg.Name, "safety-dml-final", "FAIL",
            "DML reached data source (read-only violation)")
    } else {
        recordFinding(t, cfg.Name, "safety-dml-final", "MANUAL_REVIEW",
            fmt.Sprintf("unclear path — status=%s, error=%s",
                analysis.LastQueryStatus, analysis.LastErrorMessage))
    }
}
```

### 5.3 Full Scenario Catalog

See `TESTING_SUITE.md` for the complete list. Each scenario from that
document maps to a function in this framework:

| TESTING_SUITE.md § | Scenario Function | What It Exercises |
|---|---|---|
| §5.3 Query Execution | `scenario_KnownAnswer` | Basic SELECT, LIMIT, column names |
| §5.3 Exploration Safety | `scenario_ExplorationRejection` | `validateExplorationQuery` gates |
| §5.3 Final-Query Safety | `scenario_SafetyGateDMLFinal` | `executeSQLWithMode` SELECT-only check |
| §5.3 Retry Logic | `scenario_SQLErrorRetry` | LLM gets a correction round |
| §6 Agentic Loop | `scenario_OneShotFinality` | No second `is_exploration:false` |
| §6 Agentic Loop | `scenario_ExploreThenFinal` | Full exploration → query flow |
| §4.3 Conversation CRUD | `scenario_ConversationLifecycle` | Create, archive, restore, delete |
| §4.4 LLM Provider | `scenario_ProviderDefaults` | Default provider logic |
| §6 Summarization | `scenario_SummarizeEnabled` | Summary + collapsed table |
| §6 Chart Resolution | `scenario_ChartConfig` | `render_chart` with scripted config |
| §10.6 Auto-Updater | `scenario_UpdateChecksum` | Updater SHA256 rejection |
| §7 Credential Logging | `scenario_SecretsNotLogged` | API key absent from log output |

Each scenario captures its outcome as a structured finding and returns
without halting the test runner (so all scenarios run even if one fails).

---

## 6. Finding Recording and the Bug Report

The output of every test run is a Markdown bug report written to
`documentation/TEST_RESULTS.md` (or a per-run timestamped variant).

### 6.1 Finding Struct

```go
type Finding struct {
    Config    string // profile name (e.g. "basic-relaxed")
    Scenario  string // scenario name (e.g. "known-answer")
    Status    string // "PASS", "FAIL", "SKIP", "MANUAL_REVIEW"
    Evidence  string // human-readable description of what happened
    Timestamp time.Time
    GitRef    string // optional: current git commit hash
}
```

### 6.2 Writing the Report

```go
var allFindings []Finding

func recordFinding(t *testing.T, config, scenario, status, evidence string) {
    f := Finding{
        Config:    config,
        Scenario:  scenario,
        Status:    status,
        Evidence:  evidence,
        Timestamp: time.Now(),
    }
    allFindings = append(allFindings, f)
    t.Logf("[%s/%s] %s — %s", config, scenario, status, evidence)
}

func writeReport(t *testing.T, outputPath string) {
    var buf strings.Builder
    buf.WriteString("# Test Results\n\n")
    buf.WriteString(fmt.Sprintf("**Date:** %s\n\n", time.Now().Format(time.RFC3339)))
    buf.WriteString("| Config | Scenario | Status | Evidence |\n")
    buf.WriteString("|--------|----------|--------|----------|\n")

    for _, f := range allFindings {
        buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
            f.Config, f.Scenario, f.Status, f.Evidence))
    }

    // Summary counts
    pass := countStatus("PASS")
    fail := countStatus("FAIL")
    skip := countStatus("SKIP")
    review := countStatus("MANUAL_REVIEW")

    buf.WriteString(fmt.Sprintf("\n## Summary\n\n- **PASS:** %d\n- **FAIL:** %d\n- **SKIP:** %d\n- **MANUAL_REVIEW:** %d\n- **Total:** %d\n",
        pass, fail, skip, review, len(allFindings)))

    os.WriteFile(outputPath, []byte(buf.String()), 0644)
}
```

### 6.3 Bug Documentation Format

When a FAIL or MANUAL_REVIEW finding represents a confirmed or suspected bug,
it is documented in the report with:

1. **Config it reproduces under** (the profile name)
2. **Scenario that triggered it** (which user message + settings)
3. **Evidence** — the exact error text, SQL that failed, assistant response,
   phase sequence, or tool transcript that proves the bug
4. **Severity** (matching `AGENT_READ_FIRST.md` §4.0):
   - `CRITICAL/SAFETY` — violates an absolute rule (read-only invariant,
     credential leak, data loss). Must be fixed before anything else.
   - `HIGH` — breaks answer correctness or delivery.
   - `MEDIUM` — degrades UX/comfort (wrong but non-blocking behavior).
   - `LOW` — visual polish or edge case.
5. **Whether it's a known pre-existing gap** (like the final-query DML
   bypass documented in `TESTING_SUITE.md` §5.3) or a newly discovered bug

---

## 7. Safety Guardrails for the Harness Itself

Because the test harness exercises the same code paths that connect to real
databases, it must be designed to never accidentally touch production data.

### 7.1 Never Point at a Real Data Source

- Every test data source is a `file:` SQLite fixture created with
  `os.CreateTemp` or an in-memory `:memory:` database. There is no code path
  that reads a connection string from a config file or environment variable.
- The `FakeLLMClient` never makes an HTTP request. The `"test"` provider
  type must be wired to skip `NewLLMClient`'s normal switch (see §3.1).
- The `DataSource` table is seeded with `CreateDataSource` — the fake
  fixture path is explicit, never loaded from `~/.yourql/yourql.db`.

### 7.2 No Real API Keys

- Every `APIKey` in a `TestConfig.LLMProvider` is the literal string
  `"fake-key-for-testing"`. The credential-logging safety scenario asserts
  this string never appears in the log output.
- No `.env` file is loaded during tests. The harness sets `os.Environ` or
  clears it entirely before starting.

### 7.3 Test Isolation

- Each config profile gets its own temp directory and its own `models.DB`.
  The teardown function closes the connection and removes the directory.
- Config profiles run sequentially (no parallelism) to avoid race conditions
  on the global `models.DB` variable.
- `models.DB` is set to `nil` after each teardown so a stale reference in a
  deferred goroutine panics immediately rather than corrupting a new test.

### 7.4 Find-and-Document Only

- When a scenario fails, the harness records the finding and moves to the
  next scenario. It never opens `pkg/` or `frontend/src/` to patch the
  failure.
- If a failure appears trivial to fix (e.g. a one-line safety check), the
  report documents it as "appears trivial to fix" and moves on. The fix is
  a separate, deliberate, human-reviewed activity.
- The only files the test runner writes to are the temp directory (auto-
  cleaned), `documentation/TEST_RESULTS.md` (the report), and optionally
  `documentation/TESTING_SUITE.md` (if a test reveals the scenario list
  needs updating). It never modifies any file in `pkg/`, `frontend/`,
  `app.go`, or `main.go`.

---

## 8. Additive Helpers the Harness Needs from the Application

All additions to the application code are purely additive — they add new
exported functions or switch cases but change nothing about existing
behavior. No existing function signature, behavior, or database schema is
modified.

| Addition | File | Purpose |
|---|---|---|
| `models.ConnectDatabaseAt(path string) error` | `pkg/models/database.go` | Opens a specific SQLite file and runs migrations. Used by the harness instead of `ConnectDatabase()`. |
| `services.SetTestLLMClient(client LLMClient)` | `pkg/services/llm_client.go` | Test-only override for `NewLLMClient`. When non-nil, `NewLLMClient` returns the test client instead of entering the provider switch. This is the cleanest injection point for `FakeLLMClient`. |
| `case "test":` in `NewLLMClient` switch | `pkg/services/llm_client.go` | Alternative to `SetTestLLMClient` — a `"test"` provider type that returns a `FakeLLMClient` constructed from the provider's stored config. |

Neither addition changes any existing behavior. Both are guarded: the
`"test"` provider type would never appear in a user's real provider config,
and `SetTestLLMClient` is a no-op when not called from test code.

---

## 9. Quick Reference

### Key Service Functions Used by the Harness

| Function | File | Purpose |
|---|---|---|
| `models.ConnectDatabaseAt(path)` | `pkg/models/database.go` | Open fresh DB + migrate |
| `CreateLLMProvider(...)` | `pkg/services/llm_provider.go` | Seed a provider |
| `CreateDataSource(...)` | `pkg/services/db_connection.go` | Seed a data source |
| `SetDefaultLLMProvider(id)` | `pkg/services/llm_provider.go` | Mark as default |
| `SetDefaultDataSource(id)` | `pkg/services/db_connection.go` | Mark as default |
| `CreateSkill(name, content)` | `pkg/services/skill_service.go` | Create a skill |
| `SetConversationSkill(convID, skillID, true)` | `pkg/services/skill_service.go` | Enable skill on discussion |
| `SetDiscussionDefault(key, val)` | `pkg/services/discussion_defaults.go` | Set a discussion default |
| `SetAgentLoopConfigKey(key, val)` | `pkg/services/agent_loop_config.go` | Override agent loop config |
| `CreateConversationWithDefaults(title, &providerID, &dataSourceID)` | `pkg/services/conversation.go` | Create discussion |
| `UpdateConversationSummarize(id, true)` | `pkg/services/conversation.go` | Set per-discussion flags |
| `ProcessUserMessage(convID, msg, onPhase, onStream)` | `pkg/services/discussion_engine.go` | Send message, process pipeline |
| `GetConversationMessages(convID)` | `pkg/services/conversation.go` | Fetch all messages |
| `GetQueriesByConversation(convID)` | `pkg/services/query.go` | Fetch query log |

### Key Data Models (for analysis)

| Model | Table | Relevant Fields |
|---|---|---|
| `ConversationMessage` | `conversation_messages` | `id`, `conversation_id`, `role`, `content`, `llm_content`, `sql_results`, `metadata`, `tool_transcript`, `created_at` |
| `Query` | `queries` | `id`, `conversation_id`, `question`, `generated_sql`, `status`, `result_summary`, `error_message`, `execution_time_ms`, `tokens_used`, `created_at` |
| `Conversation` | `conversations` | `id`, `llm_provider_id`, `data_source_id`, `status`, `max_messages`, `max_context_messages`, `summarize`, `viz_enabled`, `streaming_enabled` |

### Seed Fixture SQL

```sql
-- Standard test fixture. Create with createFixtureDB().
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
INSERT INTO customers (name, email) VALUES
    ('Alice', 'alice@example.com'),
    ('Bob', 'bob@example.com'),
    ('Carol', 'carol@example.com'),
    ('Dave', 'dave@example.com'),
    ('Eve', 'eve@example.com');
-- orders: 10 rows with various statuses and dates
-- order_items: 30 rows across the orders
```

### Finding Severity Mapping

| Charter § | Severity | Test Signal |
|---|---|---|
| §0 Read-Only Invariant | CRITICAL/SAFETY | DML/DDL reached a data source |
| §3.5 Credential Safety | CRITICAL/SAFETY | API key or password in log output |
| §3.2 Migration Safety | CRITICAL/SAFETY | Data loss from destructive migration |
| §3.8 Updater Safety | HIGH | SHA256 bypass, dev-version update offered |
| §1.3 One-Shot Finality | HIGH | Multiple final queries surfaced to user |
| §1.4 Safety Mode Bypass | HIGH | Complex query in strict mode |
| §3.9 UX Trust | MEDIUM | Raw stack trace shown to user |
| §3.9 Progress Phase | MEDIUM | Spinner without phase text for >2s |
| §6 Polishing | LOW | Dark mode rendering, animation glitch |