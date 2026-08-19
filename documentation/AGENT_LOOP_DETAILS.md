# YourQL — Agentic Loop Details

> This document describes every path a model can take through the agentic
> loop, every tool it has access to, every limit it operates under, every
> safety gate that blocks it, and every prompt that shapes its behavior.
> Written from the source at `pkg/engine/loop.go`,
> `pkg/services/agentic_loop.go`, `pkg/services/agent_loop_config.go`, and
> the system prompt builder.

---

## 1. Overview

The agentic loop is a multi-round, tool-calling conversation between YourQL
and an LLM. The LLM is given tools to query a database, respond to the user,
and (optionally) render charts. It runs rounds until it converges on a final
answer or exhausts its budget.

**Flow:**
```
User Message → Build System Prompt + Schema + Tools → Send to LLM
  → Parse Tool Calls → Execute / Reject / Feedback → Send Results Back
  → Repeat until respond_to_user or exhaustion
```

**Core file:** `pkg/engine/loop.go` — `AgenticLoop.Run()` (~350 lines, pure
function of inputs + 3 injected interfaces).

---

## 2. Inputs

The loop takes two value types and has three injected dependencies:

### 2.1 `LoopInput` (provided by orchestrator)

| Field | Type | Source |
|---|---|---|
| `UserMessage` | `string` | The user's natural-language question |
| `ConversationID` | `uint` | Conversation being processed |
| `QueryID` | `uint` | Tracking record (from `OutputHandler.CreateQueryRecord`) |
| `Conversation` | `ConversationMeta` | Settings: VizEnabled, Summarize, StreamingEnabled, MaxContextMessages |
| `History` | `[]*ConversationMessageMeta` | Prior messages (context-window-limited) |
| `Schema` | `*DataSchema` | Introspected database schema (tables → columns → indexes → FKs) |
| `DBConnection` | `DataSourceMeta` | Data source identity (type, name, host, database) |
| `SkillsContent` | `string` | Concatenated Markdown from active skills |
| `OnStream` | `func(StreamEvent)` | Streaming callback (nil = blocking path) |
| `Messages` | `[]ChatMessage` | Already-built LLM message array (system prompt + history + user) |
| `Tools` | `[]Tool` | Already-built tool definitions (2–4 tools depending on schema size + viz) |

### 2.2 `LoopConfig` (provided by orchestrator)

| Field | Type | Default Source | Description |
|---|---|---|---|
| `MaxExplorationRounds` | `int` | Data source `max_exploration_rounds` (default 2) | Max exploration `query_database` calls |
| `MaxToolsPerRound` | `int` | Data source `max_tools_per_round` (0 = unlimited) | Max `query_database` calls per LLM response |
| `MaxErrorRetries` | `int` | Data source `max_final_query_retries` (default 2) | Max retries on failed final queries |
| `TotalRoundCap` | `int` | `MaxExplorationRounds + MaxErrorRetries + 4` | Absolute ceiling on LLM round-trips |
| `SafetyMode` | `ExplorationSafetyMode` | `strict` (0), `moderate` (1), `relaxed` (2) | Exploration query complexity limits |
| `ContextWindow` | `*int` | Provider `context_window` (nil = unknown) | Used for context-overflow detection |
| `ModelName` | `string` | Provider name | Logging/display only |
| `VizEnabled` | `bool` | Conversation setting | Whether `render_chart` tool is registered |
| `Summarize` | `bool` | Conversation setting | Whether to generate an LLM summary of results |
| `StreamingEnabled` | `bool` | Conversation setting | Streaming vs. blocking LLM calls |
| `SummarizationTimeoutSeconds` | `int` | App setting `summarization_timeout_seconds` (default 120) | Separate timeout for summary LLM call |
| `AgentConfig` | `*AgentLoopConfig` | DB-backed config with hardcoded defaults | All prompt strings, tool descriptions, response messages (see §12) |

### 2.3 Injected Dependencies

| Interface | Injected at | Method | Called When |
|---|---|---|---|
| `LLMClient` | Loop construction | `ChatCompletionWithTools(msg, tools)` | Every round (blocking path) |
| `LLMClient` | | `ChatCompletionWithToolsStreaming(msg, tools, callback)` | Every round (streaming path) |
| `LLMClient` | | `ChatCompletionWithPayload(msg)` | Summarization (separate LLM call) |
| `QueryExecutor` | Loop construction | `Execute(sql, isExploration)` | Every `query_database` tool call |
| `OutputHandler` | Loop construction | `CreateQueryRecord(...)` | Pre-loop (by orchestrator) |
| `OutputHandler` | | `EmitFinalResponse(...)` | On successful answer |
| `OutputHandler` | | `EmitSQLWarning(...)` | On exhausted error retries |
| `OutputHandler` | | `EmitClarification(...)` | On context overflow / loop exhaustion / empty response |
| `OutputHandler` | | `StoreTechDetail(detail)` | Every round (tech-details toggle data) |

---

## 3. The Round Loop

The loop iterates over LLM rounds. Each round:

```
for round := 0; round < TotalRoundCap; round++ {
    1. Check ctx.Err() — bail on cancellation
    2. Call LLM (streaming or blocking)
    3. If LLM call fails → return FatalError (loop terminates)
    4. Build TechDetail for this round
    5. Append assistant response to messages
    6. Route based on response:
       A. Plain text, no tool calls → §4.A
       B. Empty response, no tool calls, no text → §4.B
       C. One or more tool calls → §5–§8
}
```

**Key state tracked across rounds:**

| Variable | Purpose |
|---|---|
| `pendingFinal` | Set when a final query (`is_exploration: false`) succeeds. Holds SQL, results, exploration trace, transcript, and any batched respond text. |
| `explorationResults` | Accumulated `ExplorationResult` entries (SQL + result + round + reasoning). Shown in the UI's exploration trace. |
| `transcriptActions` | Accumulated `TranscriptAction` entries (every tool call + result preview). Persisted as a `ToolTranscript` JSON blob for history replay. |
| `explorationToolCallsUsed` | Running count of `is_exploration: true` calls consumed. |
| `errorRetriesUsed` | Running count of failed final query retries consumed. |
| `toolsThisRound` | `query_database` calls in the current LLM response (for tools-per-round cap). |
| `lastQueryHadError` | Set when most recent query failed. Gated: when Summarize is enabled, `respond_to_user` is blocked until the model retries successfully. |
| `renderChartNeedsRetry` | Set when `render_chart` fails because no `pendingFinal` exists. Gives the model one extra turn to produce a final query then call `render_chart`. |

---

## 4. No-Tool-Call Response Paths

### 4.A. Plain Text Response (No Tool Calls)

**Trigger:** The LLM returns `Content` text with zero `ToolCalls`.

**Path A1 — Pending final result exists:**
The text is treated as a batched `respond_to_user`. The response text becomes
`pendingFinal.respondText` (rendered above the results table). The loop
calls `renderFinal(pendingFinal, "")` which computes a summary (if enabled)
and emits a single combined message: text above table + results + exploration
trace.

**Path A2 — Summarize enabled + last query failed + no pending final:**
The model tried to respond instead of fixing a broken query. The loop injects
`cfg.ResponseQueryErrorRequiresRetry` ("Fix the SQL error and call
query_database again before responding...") and continues the loop.

**Path A3 — Standalone respond (no pending final, no failed query):**
The text becomes the answer. The loop builds a `ToolTranscript`, calls
`OutputHandler.EmitFinalResponse` with a respond-only `FinalResponse` (no
SQL, no results, only text + exploration trace + transcript), and terminates.

### 4.B. Empty Response (No Content, No Tool Calls)

**Trigger:** The LLM returns zero `ToolCalls` and `Content` is `""`.

**Path B1 — Pending final result exists:**
Renders the pending final result (text above table + results) and terminates
without surfacing the empty response to the user. The model's silence is
accepted as "done" when a final query already exists.

**Path B2 — No pending final — Context overflow:**
If `PromptTokens > 0`, `CompletionTokens == 0`, `ContextWindow` is known, and
`PromptTokens >= ContextWindow * 90%`, the loop classifies this as
`context_overflow` and calls `OutputHandler.EmitClarification(category,
detail, cfg.ResponseContextOverflow)`. The user sees: "This question exceeds
the model's context window. Try breaking it into smaller questions..."

**Path B3 — No pending final — Generic empty response:**
If the overflow conditions aren't met, classified as `empty_response`. The
loop calls `OutputHandler.EmitClarification` with
`cfg.ResponseEmptyTruncated`: "I received an incomplete response. Could you
try rephrasing your question?"

---

## 5. Tools Available

### 5.1 Always Registered

| Tool | Parameters | Purpose |
|---|---|---|
| `query_database` | `sql` (required string), `is_exploration` (optional bool), `reasoning` (optional string) | Execute a read-only SELECT query. Exploration queries are never shown to the user; final queries surface in the UI. |
| `respond_to_user` | `text` (required string) | Deliver a text answer to the user (markdown). Used standalone or batched with a final query. |

### 5.2 Conditionally Registered

| Tool | Condition | Parameters |
|---|---|---|
| `render_chart` | `VizEnabled == true` | `chart_config` (required string — a JSON Chart.js config with `$column_name` references) |
| `list_tables` | Schema ≥ 10 tables OR `ForceSchemaTools` enabled | None |
| `describe_table` | Same as `list_tables` | `table_name` (required string) |

**Tool registration logic:**
- `render_chart` is registered only when the conversation's VizEnabled flag
  is on (per-conversation setting, toggleable in Settings).
- `list_tables` / `describe_table` are registered when the data source has
  ≥ 10 tables OR the user has explicitly enabled `ForceSchemaTools` in the
  data source config. When registered, the full column-level schema is
  **removed** from the system prompt — the model must discover it via these
  tools instead. This saves context window space on large databases.

All tool descriptions, parameter descriptions, and required fields are
configurable via `AgentLoopConfig` (§12).

---

## 6. `query_database` — All Paths

This is the most complex tool. Every call is routed through a sequence of
gates.

### Gate 1: One-Shot Finality

```
IF pendingFinal != nil:
    → Reject: append cfg.ResponseOneshotViolation to messages, continue
```

Once a final query has succeeded, no more `query_database` calls of any kind
are allowed. The model may only call `respond_to_user` or `render_chart`.

### Gate 2: Argument Parsing

```
Unmarshal args into {sql, is_exploration, reasoning}
IF parse error:
    → Reject: append cfg.ResponseParseError to messages, continue
```

The `sql` field is required; `is_exploration` and `reasoning` are optional.

### Gate 3: Exploration Budget

```
IF is_exploration AND explorationToolCallsUsed >= MaxExplorationRounds:
    → Reject: append cfg.ResponseExplorationExhausted, continue
```

The exploration budget counts **tool calls**, not LLM rounds. A single round
with 3 batched `query_database` calls consumes 3 against the budget. The
rejection message tells the model: produce a final query if confident,
otherwise ask the user a targeted question.

### Gate 4: Exploration Safety (Complexity Mode)

```
IF is_exploration:
    err := ValidateExplorationQuery(sql, safetyMode)
    IF err:
        → Reject: increment exploration counter, append
          cfg.ResponseSafetyRejected(err), continue
```

The safety mode (`strict`/`moderate`/`relaxed`) controls what the exploration
query may contain:

| Mode | Allowed | Blocked |
|---|---|---|
| `strict` | Simple SELECT, COUNT, DISTINCT, SHOW/DESCRIBE, INFORMATION_SCHEMA | JOINs, subqueries, GROUP BY, ORDER BY |
| `moderate` | Strict + single-table JOIN, GROUP BY, ORDER BY | Subqueries, UNION, multi-table JOINs |
| `relaxed` | Everything in moderate + subqueries + UNION | INSERT/UPDATE/DELETE/DROP/ALTER/TRUNCATE/CREATE (read-only enforced separately) |

**Important:** A rejected exploration query counts against the exploration
budget AND the tools-per-round cap. The model learns nothing from it — it
wastes a turn.

### Gate 5: Execution

```
result, err := QueryExecutor.Execute(sql, is_exploration)
```

The `QueryExecutor` (adapter wrapping `executeSQLWithMode`) independently
enforces the read-only invariant (`ValidateReadOnlySQL`) for **every** query,
exploration or final — this is the single choke point per
`AGENT_READ_FIRST.md §0`. For exploration queries, the executor also calls
`ValidateExplorationQuery` itself as an additional safety layer.

### Path 6A: Execution Fails

```
IF err != nil:
    IF err == context.Canceled → return cancellation
    IF is_exploration:
        → increment exploration counter, mark lastQueryHadError,
          append result as tool message, continue loop
    ELSE (final query):
        IF errorRetriesUsed >= MaxErrorRetries:
            → emitSQLWarning(sql, err), terminate
        errorRetriesUsed++
        IF !IsRetryableError(err):
            → emitSQLWarning(sql, err), terminate
        → append result as tool message, continue loop
```

**Error retry logic for final queries:**
- Retryable: schema errors (unknown column/table), syntax errors, transient
  connection issues. The error is augmented with schema hints (e.g., "did you
  reference the right table/alias?") via `AugmentSQLError`.
- Non-retryable (immediate failure): authentication failures, permission
  denied, invalid credentials. The warning is emitted and the loop terminates.
- Each retry consumes one round in the loop.

### Path 6B: Execution Succeeds — Exploration

```
IF is_exploration:
    → increment exploration counter
    → append to explorationResults (for UI trace)
    → append result as tool message
    → continue loop
```

The result is formatted as a compact digest (row count, columns, up to 50
sample rows, numeric column stats) via `FormatToolResult`. The model sees
this as a `"tool"` role message.

### Path 6C: Execution Succeeds — Final Query

```
IF !is_exploration:
    → set pendingFinal = {sql, result, explorationTrace, transcript}
    → IF response.Content != "" (model paired text with query):
        → append respond_to_user TranscriptAction
        → set pendingFinal.respondText = response.Content
          (suppressed if renderChartNeedsRetry — that text is debug monologue)
    → IF render_chart is batched in this same response:
        → let the tool-call loop process it next
    → ELSE:
        → call renderFinal(pendingFinal, "") → emit one combined message
    → continue (or return)
```

**Batching behavior:** When the model sends `query_database(is_exploration:
false)` + `respond_to_user` in the same message, the respond text is stashed
as `pendingFinal.respondText` and rendered above the results table in one
combined message — one UI bubble, one round-trip.

When `render_chart` is also batched alongside the final query, the loop lets
the `render_chart` handler process it next on the same tool-call iteration.

**renderChartNeedsRetry special case:** If the model previously tried
`render_chart` without a pending result (got a "no pending" rejection), and
now produces a final query in response, the loop gives it one extra turn to
call `render_chart` again (instead of immediately rendering without a chart).

---

## 7. `list_tables` and `describe_table`

These are on-demand schema exploration tools — registered only when the
database has ≥ 10 tables or `ForceSchemaTools` is on. They do NOT count
against the exploration budget (they don't run SQL).

**`list_tables`:**
Returns all table names with row counts. Format: `"Tables: orders(1500),
products(200), customers(5000)"`.

**`describe_table(table_name)`:**
Returns the full column schema for a single table: name, type, nullability,
primary/foreign key markers, table description. If the table isn't found,
returns a list of available tables.

Both are classified as `roundKindExploration` for tech-details purposes but
consume zero exploration budget. They are free lookups.

---

## 8. `respond_to_user` — All Paths

### Path 8A: Argument Parse Error

```
IF args can't be unmarshaled:
    → append cfg.ResponseRespondParseError to messages, continue
```

### Path 8B: Pending Final Result Exists

The model is pairing a response with a previously-succeeded final query.

```
IF pendingFinal != nil:
    IF render_chart appears later in this same response:
        → stash respondText on pendingFinal, let render_chart process first
    ELSE:
        → set pendingFinal.respondText, transcript
        → renderFinal(pendingFinal, "") — combined message
        → terminate
```

### Path 8C: Summarize + Last Query Failed

```
IF Summarize && lastQueryHadError:
    → append cfg.ResponseQueryErrorRequiresRetry to messages, continue
```

When summarization is on and the previous query failed, the model must fix
the error before responding. This prevents the user from seeing a partial
answer without data.

### Path 8D: Standalone Respond (No Pending Final, No Error Gate)

```
→ build ToolTranscript from transcriptActions
→ OutputHandler.EmitFinalResponse(respond-only FinalResponse)
→ terminate
```

---

## 9. `render_chart` — All Paths

### Path 9A: Parse Error

```
IF chart_config can't be unmarshaled:
    → append cfg.ResponseRenderChartParseError(err), continue
```

### Path 9B: No Pending Final Result

```
IF pendingFinal == nil:
    → set renderChartNeedsRetry = true
    → append cfg.ResponseRenderChartNoPending
      ("render_chart failed: no pending query result. Call query_database
       (is_exploration: false) first, then call render_chart in the SAME
       response.")
    → continue
```

This triggers the `renderChartNeedsRetry` flag, which gives the model one
extra turn after the next final query to retry the chart.

### Path 9C: Pending Final Exists

```
→ append render_chart TranscriptAction
→ build transcript (including the chart call)
→ renderFinal(pendingFinal, args.ChartConfig)
    → if Summarize enabled, compute summary via separate LLM call
    → resolve chart config ($column_name → data arrays)
    → OutputHandler.EmitFinalResponse(FinalResponse{HasChart: true, ChartConfig: ...})
    → terminate
```

The chart config is a JSON string describing a Chart.js visualization using
`$column_name` references. The loop's `renderFinal` calls `ResolveChartConfig`
to replace those references with actual data arrays, then stores the resolved
config in the message's metadata as `chart_config`. The frontend's
`VizChart.svelte` renders it.

---

## 10. Unknown Tool

```
default case:
    → append cfg.ResponseUnknownTool(tc.Function.Name)
      ("Unknown tool: <name>"), continue
```

The loop continues — unknown tools don't terminate processing.

---

## 11. Limits and Budgets

### Round Cap

`TotalRoundCap = MaxExplorationRounds + MaxErrorRetries + 4`

This is the **absolute ceiling** on LLM round-trips. No configuration field
directly sets it — it's derived from the exploration + retry budgets with a
buffer of 4 rounds for text-only or chart follow-ups.

**On exhaustion:**
- If `pendingFinal` exists → it's rendered as the answer.
- Otherwise → `EmitClarification("loop_exhausted", cfg.ResponseLoopExhausted)`.

### Exploration Budget

`MaxExplorationRounds` (default: 2). Counts `is_exploration: true` calls to
`query_database`. Safety-rejected calls count against this budget. Once
exhausted, the model gets `cfg.ResponseExplorationExhausted` ("produce a final
query if confident, otherwise ask a targeted question").

### Tools-Per-Round Cap

`MaxToolsPerRound` (default: 0 = unlimited). When set, only the first N
`query_database` calls in a single LLM response are executed; the rest get
`cfg.ResponseTooManyToolsPerRound`.

### Error Retry Budget

`MaxErrorRetries` (default: 2). Failed final queries consume one retry each.
Non-retryable errors (auth, permissions) terminate immediately.

### Row Limits

| Row limit | Where enforced | Default |
|---|---|---|
| Final query | `applyDefaultLimit` in `executeSQLWithMode` | 1,000 |
| Exploration query | `applyDefaultLimit` in `executeSQLWithMode` | 100 |
| LIMIT auto-appended | Queries longer than `query_length_threshold` (200 chars) that lack an explicit LIMIT | auto |

### Context Window

The model's `ContextWindow` (from provider config) isn't enforced as a hard
limit but is used for **overflow detection:** when `PromptTokens >=
ContextWindow * 90%` and 0 completion tokens are returned, the loop classifies
the empty response as `context_overflow` instead of a generic empty response.

### Pipeline Timeout

`pipeline_timeout_seconds` (default: 180). The whole `AgenticLoop.Run` call
runs inside a `context.WithTimeout` set by the orchestrator. If the timeout
fires mid-loop, the `ctx.Err()` check at the top of each round returns
`context.DeadlineExceeded`.

### Summarization Timeout

`summarization_timeout_seconds` (default: 120). The summarization LLM call
runs on its OWN context and timeout, separate from the pipeline timeout. This
prevents a slow main loop from starving summarization.

---

## 12. Termination States

The loop terminates in exactly one of these ways:

| State | Trigger | User Sees |
|---|---|---|
| **Success — respond-only** | `respond_to_user` tool call (no query) | Markdown text ± exploration trace |
| **Success — query + respond** | Final query + (optional) respond text + (optional) chart | Text above results + results table + exploration trace + chart |
| **SQL error — warning** | Final query retries exhausted OR non-retryable error | Friendly error message (authentication / connection / schema) |
| **Clarification — context overflow** | Prompt ≥ 90% context window, 0 completion tokens | "This question exceeds the model's context window..." |
| **Clarification — empty response** | 0 tokens, 0 tool calls, no overflow detected | "I received an incomplete response..." |
| **Clarification — loop exhausted** | TotalRoundCap reached, no pending final | "I wasn't able to complete this request..." |
| **Cancellation** | `ctx.Err()` returns `context.Canceled` | "⏹ Cancelled" (orchestrator, not loop) |
| **Fatal LLM error** | `LLMClient` call fails | Deferred error message (orchestrator) |

---

## 13. The System Prompt

The prompt the LLM receives is built by `buildToolSystemPrompt` (or
`buildCompactSystemPrompt` for compact mode) in `pkg/services/agentic_loop.go`.
It's assembled from:

### 13.1 Persona / Custom System Prompt

1. If the data source has a custom `system_prompt` → that replaces the persona.
2. Otherwise → `cfg.PersonaFallback` ("You are a helpful data analyst
   assistant. Your task is to help users query a database using natural
   language.").
3. If the data source has `business_rules` → appended as a bullet list after
   the persona.

### 13.2 Database Schema

If a database is connected and schema is available:

- **Normal mode** (< 10 tables, no ForceSchemaTools): Full column-level schema
  with types, nullability, primary keys, foreign keys, indexes, table/column
  descriptions, and row counts.
- **On-demand mode** (≥ 10 tables OR ForceSchemaTools): Only table names and
  row counts. The model must use `list_tables` / `describe_table` tools to
  discover column details.

### 13.3 Database Connection

- Connection name, database type, dialect rules (quoting, LIMIT style)
- Row limit (default LIMIT if missing)
- Exploration config: enabled/disabled, max rounds, max tools per round,
  safety mode

### 13.4 Instructions (§1–§6)

Six numbered instructions (all overridable via `AgentLoopConfig`):

1. "Analyze the user's question and the database schema."
2. "You have three tools available" → bullets for query_database,
   respond_to_user, render_chart.
3. "Consider pairing respond_to_user with your final query_database..." —
   the most behaviorally impactful instruction. Tells the model when to
   combine text + data and when commentary is appropriate.
4. "Confidence rule — before your final answer, check that you are confident
   in every part of it." The model must be confident about meaning,
   methodology, interpretation, and budget before answering. If uncertain,
   ask a targeted question.
5. "All SQL queries must be read-only (SELECT only). Follow dialect rules."
6. "Always include a LIMIT clause matching the row limit."

### 13.5 Exploration Safety Rules

Only displayed when exploration is enabled on the data source. Includes:

- Preamble: "Exploration queries run in **<mode>** mode."
- Mode-specific allowed/blocked rules.
- Footer: "All modes: read-only only — no DML/DDL."
- Budget: "You may run at most N exploration queries."
- Rejection guidance: "If a query is rejected, adapt within constraints OR
  use respond_to_user."
- One-shot rule: "Once you deliver a final query, your database session
  closes immediately."

### 13.6 Chart Guidance

Only displayed when `VizEnabled` is on. Includes:

- When to use `render_chart` (only when explicitly asked).
- Chart.js config format (`type`, `data.labels`, `data.datasets`,
  `$column_name` references, scatter format, semi-transparent colors).
- Timing: batch with final query, call after seeing results, skip if unsure.

### 13.7 Skills

Active skills are appended as `"## Additional Context (from Skills)"`
followed by their Markdown content. Skills are user-defined prompt fragments
toggleable per conversation.

### 13.8 Prompt Truncation

If the assembled prompt exceeds **16,384 characters**, it's truncated to
16,000 with a note: "[Note: schema truncated due to context limits. The
user's question follows below.]"

### 13.9 Compact Prompt Mode

When the data source has `compact_prompts: true`, the system prompt is
replaced with `buildCompactSystemPrompt`: database type + name, table names
with row counts only (no column detail), one-line dialect hint, terse tool
list. No persona, no chart guidance, no verbose instructions, no skills.
Designed for small/local models where the full prompt alone exhausts context.

### 13.10 Message Construction

The `buildToolLlmMessages` function builds the full message array:

```
[system: systemPrompt]
[assistant: from tool_transcript replay]   ← for each history message
[tool: result_preview for each tool call]  ← from transcript
[system: SQL results digest]               ← for messages with SQLResults
[user: userMessage]
```

Error messages (flagged with `is_error: true` in metadata) and exploration
messages (tech-detail entries) are excluded from history replay.

Tool transcripts (persisted as JSON in `conversation_messages.tool_transcript`)
are replayed via `buildToolMessages`: each non-respond action becomes an
assistant tool-call + a tool-result message pair with freshly minted IDs
(`hist_0`, `hist_1`, ...). This allows the model to see what queries it ran
and what came back without needing the full conversation message content.

---

## 14. Summarization

When `Summarize` is enabled and a final query produces results:

1. The loop calls `a.summarize()` in a **separate goroutine** with its own
   context and timeout (configurable, default 120s).
2. A compact digest is built via `FormatResultsDigestForSummarization`:
   column-level statistics (min/max/mean for numeric, distinct counts),
   plus first 5 + last 5 rows.
3. The digest is sent to the LLM via `ChatCompletionWithPayload` with the
   summarization prompt.
4. If summarization fails → a fallback note is displayed ("⚠️ Summary
   unavailable").
5. If it succeeds → the summary markdown is rendered above the results table
   (unless a chart is present, in which case it moves inside the collapsed
   results block per `SUMMARIZE_DATA_VIZ_CONFLICT.md`).

---

## 15. Tech Details (Debug Toggle)

Every round produces a `TechDetail` record persisted via
`OutputHandler.StoreTechDetail`. Each record contains:

| Section | Fields |
|---|---|
| `Request` | Message count, last user message (truncated 200 chars), full raw messages JSON |
| `Response` | Finish reason, text content, raw output, tool call summaries, prompt/completion tokens |
| `SQL` (query rounds) | Query text, execution time, row count, error |
| `Stream` (streaming rounds) | Chunk count, byte count, debug file path |
| `Round` | 0-based round number, kind (`exploration`/`error_retry`/`chart`/`final`/`other`), duration |

`StoreTechDetail` writes these as `role: "exploration"` conversation messages.
The frontend's tech-details toggle renders them.

Duplicates are prevented by a per-round guard: `logRound` writes at most one
tech-detail message per round, even if a single LLM response contains multiple
tool calls.

---

## 16. Error Augmentation

When a query fails with a known error pattern, the error is augmented before
being fed back to the model:

**MySQL Error 1054 (Unknown column):**
```
Query failed: Error 1054: Unknown column 'bad_col' in 'field list'

[Hint: This is a schema error — a column in your query doesn't exist on the
table you referenced. Check:
1. Did you reference the correct table/alias for this column?
2. Is the column on a different table that needs a JOIN?
3. Check table names and column names against the schema above.
4. Fix the column reference and retry.]
```

Other SQL engine errors are returned as-is. This is the only augmentation
currently implemented.

---

## 17. Configuration (`AgentLoopConfig`)

All prompts, tool descriptions, instructions, safety rules, chart guidance,
and response messages are stored in `AgentLoopConfig` — a struct with 45
string fields. Every field has a hardcoded default in
`pkg/services/agent_loop_config.go`. Users can override any field at runtime
by inserting a row in the `agent_loop_config` SQLite table. The UI exposes
these in the Settings panel grouped by section.

Fields with `%s` / `%d` / `%v` placeholders are format strings — the loop
fills them with runtime data (safety mode name, quota numbers, error text)
before sending to the model.

Setting a field to its default value (or empty) deletes the override row,
reverting to the hardcoded default.

### Configurable Tool Descriptions

| Field | Controls |
|---|---|
| `ToolQueryDatabaseDesc` | When the model decides to query the database |
| `ToolQueryDatabaseSQLDesc` | What constitutes a valid SQL query |
| `ToolQueryDatabaseIsExplorationDesc` | Visibility: hidden (exploration) vs. surfaced (final) |
| `ToolQueryDatabaseReasoningDesc` | Optional chain-of-thought parameter |
| `ToolRespondToUserDesc` | When the model provides text responses |
| `ToolRespondToUserTextDesc` | The text parameter description |
| `ToolRenderChartDesc` | When the model generates charts |
| `ToolRenderChartConfigDesc` | The chart_config JSON format |

### Configurable Instructions

| Field | Content |
|---|---|
| `Instruction1` | "1. Analyze the user's question and the database schema." |
| `Instruction2` | "2. You have three tools available:" |
| `Instruction2a` | query_database bullet |
| `Instruction2b` | respond_to_user bullet |
| `Instruction2c` | render_chart bullet (viz only) |
| `Instruction3` | Pairing guidance (respond + query together) |
| `Instruction4` | Confidence rule (ask when uncertain) |
| `Instruction5` | Read-only SQL reminder |
| `Instruction6` | LIMIT clause reminder |

### Configurable Response Messages

| Field | When Sent | Default |
|---|---|---|
| `ResponseParseError` | Malformed tool-call arguments JSON | "Error parsing arguments: %v..." |
| `ResponseExplorationExhausted` | Exploration budget used up | "Exploration budget exhausted..." |
| `ResponseSafetyRejected` | Query violates complexity mode | "Exploration query rejected: %s..." |
| `ResponseOneshotViolation` | Query after final result | "Final result already delivered..." |
| `ResponseUnknownTool` | Unknown tool name | "Unknown tool: %s" |
| `ResponseRenderChartNoPending` | Chart with no pending result | "render_chart failed: no pending query result..." |
| `ResponseRenderChartParseError` | Chart config parse error | "render_chart failed: error parsing arguments: %v" |
| `ResponseRespondParseError` | Respond args parse error | "Error parsing arguments: %v..." |
| `ResponseLoopExhausted` | Round cap hit | "I wasn't able to complete this request..." |
| `ResponseEmptyTruncated` | Empty LLM response | "I received an incomplete response..." |
| `ResponseContextOverflow` | Prompt too large | "This question exceeds the model's context window..." |
| `ResponseQueryErrorRequiresRetry` | Respond after failed query (summarize on) | "Fix the SQL error and call query_database again..." |
| `ResponseTooManyToolsPerRound` | Tools-per-round cap exceeded | "You issued more query_database calls than allowed (max %d)..." |

---

## 18. Streaming

When `OnStream` is set (conversation has `StreamingEnabled`), the loop uses
`ChatCompletionWithToolsStreaming` instead of `ChatCompletionWithTools`. The
streaming path:

1. Receives chunks one at a time via the `onEvent` callback.
2. Stream chunks are emitted to the frontend via `runtime.EventsEmit` (Wails)
   for incremental display.
3. The final assembled `*ChatMessage` has the same shape as the blocking path.
4. The streaming metrics (chunk count, byte count) are recorded in `TechDetail`.

When `OnStream` is nil, the blocking path is used — identical `LoopOutput`
for the same LLM response.

---

## 19. One-Shot Finality (Full Detail)

The one-shot rule is enforced by the `pendingFinal` variable:

1. Before the loop starts, `pendingFinal = nil`.
2. When a final query succeeds (`is_exploration: false`), `pendingFinal` is
   set to a struct holding the SQL, results, exploration trace, transcript,
   and any batched respond text.
3. All subsequent `query_database` calls (Gate 1) are rejected with
   `ResponseOneshotViolation`.
4. The only remaining tools are `render_chart` and `respond_to_user`.
5. The pending result is rendered when:
   - A `render_chart` call consumes it.
   - A `respond_to_user` call (batched or standalone) consumes it.
   - The next LLM response returns plain text (Path A1).
   - The next LLM response is empty (Path B1).
   - The round cap is hit.

This guarantees **one answer bubble per user question** — the user never sees
two separate messages with different final queries for the same question. See
`FINAL_MESSAGE.md` for the full design rationale.

---

## 20. Plain-Text Fallback Path

When the LLM returns content text but no tool calls (Provider-API-level
behavior, not YourQL tool-call parsing):

- **With pendingFinal:** The text is treated as a batched `respond_to_user`
  merged with the final query result (combined message).
- **With summarize + failed query:** The response is rejected with
  `ResponseQueryErrorRequiresRetry`.
- **Otherwise:** The text is treated as a standalone `respond_to_user` and
  rendered as the final answer.

This path is the most provider-sensitive — it's triggered by models that
output prose instead of structured tool calls (notably certain Ollama/local
models), and the behavior differs from the explicit `respond_to_user` tool
path only in that no `TranscriptAction` is recorded for the respond call
(since there's no structured JSON to parse). The charter (`AGENT_READ_FIRST.md
§1.3`) explicitly warns to "be conservative when modifying this fallback
text-response handling."

---

## 21. Limits Summary Table

| Limit | Variable | Default | Enforced At |
|---|---|---|---|
| Absolute rounds | `TotalRoundCap` | `exploration + retries + 4` | Every round iteration |
| Exploration queries | `MaxExplorationRounds` | 2 (from data source) | Gate 3 (counts tool calls, not rounds) |
| Error retries | `MaxErrorRetries` | 2 (from data source) | Path 6A (only final queries) |
| Queries per round | `MaxToolsPerRound` | 0 = unlimited | Tools-per-round cap |
| Row limit (final) | `DefaultLimit` | 1,000 | `applyDefaultLimit` |
| Row limit (exploration) | `ExplorationDefaultLimit` | 100 | `applyDefaultLimit` |
| Pipeline timeout | `pipeline_timeout_seconds` | 180s | `context.WithTimeout` |
| Summarization timeout | `summarization_timeout_seconds` | 120s | Separate context |
| Prompt truncation | — | 16,384 chars | `buildToolSystemPrompt` |
| Context overflow threshold | — | 90% of `ContextWindow` | §4.B Path B2 |
| Context window (history) | `MaxContextMessages` | 5 (hard cap 15) | Orchestrator |