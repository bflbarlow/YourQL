> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Tool Call Enhancement

**Status:** Approved design. Ready for phased implementation (§9).

**Summary:** Replace YourQL's JSON-response LLM protocol with native
function/tool calling. The model calls `query_database`, `respond_to_user`,
and (conditionally) `render_chart` instead of returning a JSON object with an
`action` field and prose embedded in string values. This removes the
parsing/escaping pipeline that caused every bug in
[`ANSWER_CLARIFICATION_ISSUE.md`](../issues/ANSWER_CLARIFICATION_ISSUE.md) except one,
and gives the model a materially better position to generate correct SQL and
well-chosen charts.

This document is the specification for that change. It assumes familiarity
with [`AGENT_READ_FIRST.md`](../AGENT_READ_FIRST.md), whose goal priority order
and risk framework govern every design decision below.

---

## 1. Motivation

### 1.1 The problem with the current protocol

YourQL's discussion engine asks the LLM to return a single JSON object:

```json
{"action":"answer","answer":"**Yes.**\n\nThe query has two issues:\n\n```sql\nCROSS JOIN...\n```"}
```

The `answer` (and formerly `explanation`) fields carry full markdown prose —
backticks, quotes, newlines, fenced code blocks — as JSON string values. That
string has to survive four independent transformation stages before it
reaches the user:

1. **Generation** — the model must correctly escape all markdown special
   characters inside a JSON string.
2. **Transport** — the LLM API wraps the model's JSON inside its own response
   envelope, double-encoding every escape sequence (`\n` → `\\n` → `\\\\n` in
   the raw wire format).
3. **Extraction** — application code must isolate the model's JSON from
   markdown code fences, "thinking" markers, and other formatting the model
   adds around it.
4. **Parsing** — application code must unmarshal the JSON and reverse the
   double-encoding to recover real newlines.

Five distinct bugs were found and fixed in this pipeline
(`ANSWER_CLARIFICATION_ISSUE.md`): an internal `explanation` field leaking to
users, a `sql_query` field silently dropped on `answer` actions, a markdown
renderer setting that corrupted SQL comments, double-encoded newlines
rendering as literal `\n`, and a code-fence extractor that truncated answers
containing their own fenced code blocks. Four of the five are direct,
structural consequences of asking a model to embed prose inside a JSON
string and then parsing that string back out. They are not implementation
mistakes in the ordinary sense — each fix added a compensating mechanism
(a state machine, a regex preprocessor, a lenient JSON parser, extraction
guards) to work around a fragility that is inherent to the protocol itself.
New models and new response shapes will keep finding new edges of this same
class of bug for as long as the protocol exists.

### 1.2 The fix: native tool calling

Every provider YourQL supports (OpenAI, Anthropic, Ollama, and any
OpenAI-compatible local endpoint) has first-class support for function/tool
calling: the model returns a structured call to a named function with typed
arguments, and the API client library — not application code — is
responsible for parsing it out of the response envelope.

```json
{
  "tool_calls": [{
    "id": "call_abc123",
    "function": {
      "name": "query_database",
      "arguments": "{\"sql\":\"SELECT * FROM customers\",\"is_exploration\":false}"
    }
  }]
}
```

The `arguments` string is still JSON, but it is flat and small — a SQL
string, a boolean, maybe a short reasoning note. There is no multi-paragraph
markdown with embedded code fences living inside a JSON string value, and
therefore nothing for a fragile extraction/unescaping pipeline to get wrong.
`sql`, `is_exploration`, and `text` (for prose answers) go straight through
`encoding/json` the same way the outer response envelope already does,
successfully, today.

### 1.3 What this buys, concretely

| Bug (from `ANSWER_CLARIFICATION_ISSUE.md`) | Why it cannot recur |
|---|---|
| SQL query dropped on answer actions | `query_database` and `respond_to_user` are separate tools; there is no shared field that can be requested and then ignored |
| Smartypants/markdown renderer corrupted SQL | SQL never passes through the markdown renderer — it is a structured argument, executed directly |
| Newlines rendered as literal `\n` | No double-JSON-encoding of prose — tool arguments are small and flat, and the outer envelope's own encoding is handled entirely by the API client library |
| Answer truncated by code-fence extraction | There is no "extract the model's JSON out of raw text" step at all — the API client returns typed `ToolCall` structs |
| Internal `explanation` field leaking to users | Chain-of-thought moves to an optional `reasoning` tool argument that is never read by any rendering code path (§2.1.3) |

Beyond removing bugs, tool calling also unlocks something the JSON protocol
made structurally awkward: the model can look at real query results before
deciding whether and how to chart them (§3.6), and the same execution loop
that resolves ordinary exploration also resolves SQL syntax-error correction,
unifying two code paths that exist separately today.

---

## 2. Goals and Non-Goals

**Goals:**

- Eliminate the JSON-response parsing pipeline and every bug class it causes.
- Preserve every safety guarantee in `AGENT_READ_FIRST.md` §0 and §1.4 without
  exception — the read-only invariant, exploration complexity gating, and
  row limits are not up for renegotiation.
- Preserve every user-facing capability that exists today: charts, plain-
  English summaries, the exploration-trace transparency toggle, and SQL
  error retry.
- Support all four LLM providers YourQL ships today, with an honest fallback
  for models that don't support tools natively.
- Ship incrementally, with a feature flag, so existing conversations are
  never disrupted mid-migration.

**Non-goals:**

- This is not a redesign of the prompt's schema/skills/business-rule
  injection, SQL execution safety layer, or result-rendering/summarization
  pipeline. Those are reused as-is.
- This is not a UI redesign. The frontend's rendering contract
  (`{@html message.content}`, the `content_type: "html"` metadata field) is
  unchanged.
- This does not attempt to unify every LLM provider's tool-calling wire
  format into one code path inside the discussion engine. Each provider
  client is responsible for translating the shared `Tool`/`ToolCall`
  vocabulary (§4) into its own request/response shape, exactly as each
  already translates `ChatMessage` today.

---

## 3. Tool Design

Three tools replace the current four JSON `action` types
(`sql_query`, `sql_exploration`, `answer`, `clarification`):

### 3.1 `query_database`

Replaces `sql_query` and `sql_exploration`. A single tool with a boolean flag
distinguishes the two, because the application-level handling they require
(read-only enforcement, complexity gating, row limits) is identical — only
the *routing* differs (exploration results go back to the model; final
results go to the user).

```go
{
    Type: "function",
    Function: FunctionDef{
        Name:        "query_database",
        Description: "Execute a read-only SELECT query against the connected database. Use this when the user's question requires querying data.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "sql": map[string]any{
                    "type":        "string",
                    "description": "A valid SELECT SQL query. Must be read-only. Always include a LIMIT clause.",
                },
                "is_exploration": map[string]any{
                    "type":        "boolean",
                    "description": "Set to true if this is an exploratory query to understand the data before writing the final query. Set to false for the final answer query.",
                },
                "reasoning": map[string]any{
                    "type":        "string",
                    "description": "Optional. Brief internal reasoning about why this query answers the question. This is NOT shown to the user — it exists only to improve query quality via chain-of-thought. Keep it short. Omit if not needed.",
                },
            },
            "required": []string{"sql"},
        },
    },
}
```

### 3.2 `respond_to_user`

Replaces `answer` and `clarification`. There is no separate "ask a
clarifying question" tool — a clarifying question is just prose that happens
to be a question, and folding it into `respond_to_user` removes an entire
action type (and its associated parsing branch) with no loss of capability.

```go
{
    Type: "function",
    Function: FunctionDef{
        Name:        "respond_to_user",
        Description: "Provide a direct response to the user. Use this when the question does not require querying the database — e.g., follow-up questions about previously returned data, explanations of how a query works, general knowledge questions, or when you need to ask the user a clarifying question.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "text": map[string]any{
                    "type":        "string",
                    "description": "The response text. Use markdown formatting for structure.",
                },
            },
            "required": []string{"text"},
        },
    },
}
```

### 3.3 `render_chart` (conditional)

Replaces the current `viz_config` JSON field. Offered to the model only when
`conversation.VizEnabled` is true — a model with charting disabled for its
conversation is never given this tool definition, so it cannot call it. This
tool is deliberately **not** a parameter on `query_database`: see §5.4 for
why chart selection is deferred until after the model has seen real results,
and how the loop implements that deferral.

```go
{
    Type: "function",
    Function: FunctionDef{
        Name:        "render_chart",
        Description: "Attach a chart visualization to the results of the most recent final query_database call. Only call this AFTER seeing the query result in a 'tool' message — use the actual returned columns, value ranges, and row count to choose an appropriate chart type. Only available when charting is enabled for this conversation. Do not call this for exploration queries.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "chart_config": map[string]any{
                    "type":        "string",
                    "description": "A JSON string describing a Chart.js configuration using $column references, in the same shape currently produced by the 'viz_config' field.",
                },
            },
            "required": []string{"chart_config"},
        },
    },
}
```

### 3.4 Why chain-of-thought moves to `reasoning`, not away entirely

The current prompt keeps an `explanation` field specifically because
chain-of-thought before producing SQL measurably improves generation
accuracy for models without a native reasoning trace. That benefit is real
and worth preserving — but the field must never become user-visible text,
which is exactly how it leaked in the current protocol (multiple independent
rendering paths each found their own way to surface it).

The `reasoning` parameter on `query_database` is the replacement: an optional
argument no rendering code path ever reads. It is stored (for tech-details
inspection) but never displayed. `respond_to_user` has no equivalent
parameter — a model producing prose can reason in its own turn before
calling the tool, and reasoning-capable models already do this via their
provider-level `reasoning`/`thinking` response fields, which are orthogonal
to tool arguments entirely.

---

## 4. LLM Client Layer

### 4.1 Shared vocabulary

These types are added to `pkg/services/llm_client.go` and are shared by every
provider implementation:

```go
type ChatMessage struct {
    Role       string     `json:"role"`
    Content    string     `json:"content,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    // Name identifies which function produced the result on a "tool" role
    // message. Required by some providers (OpenAI); harmless to include for
    // providers that key off ToolCallID alone.
    Name string `json:"name,omitempty"`
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
    Type     string      `json:"type"` // "function"
    Function FunctionDef `json:"function"`
}

type FunctionDef struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`
}
```

### 4.2 Interface: additive, not mutated

`LLMClient`'s two existing methods are untouched:

```go
type LLMClient interface {
    ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
    ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)

    // ChatCompletionWithTools is the tool-aware entry point used exclusively
    // by the discussion engine's agentic loop (§5). Every provider must
    // implement this method (Go requires all interface methods to be
    // implemented), but a provider without native tool support may implement
    // it as a passthrough that ignores `tools` and delegates to
    // ChatCompletionWithPayload, returning a ChatMessage with only Content
    // set. This keeps the package compiling and every provider usable from
    // day one, independent of when each provider's real tool support lands.
    ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (msg *ChatMessage, requestJSON, responseJSON string, err error)
}
```

Interface methods cannot be overloaded in Go — a signature change to the two
existing methods would force every implementer (`OpenAIClient`,
`AnthropicClient`, `OllamaClient`, `LocalClient`) to change in lockstep just
to keep the package compiling, regardless of whether that provider's tool
support is actually ready. Adding a new method instead means each provider's
real implementation can land independently, and every caller that has no
need for tools — `summarizeResults`, most obviously — never has to change at
all.

The discussion engine's agentic loop (§5) calls `ChatCompletionWithTools`
exclusively. It is the only caller that needs to.

---

## 5. The Agentic Loop

`runAgenticLoop` replaces the current exploration loop
(`sql_exploration` → `sql_query`, up to `maxRounds` iterations) inside
`discussion_engine.go`. It is the core of this change: a single function
that resolves exploration, final-query execution, SQL error retry, optional
charting, and the final prose response, all as one round-trip conversation
with the model, using two tool calls to feed results back and one to
conclude.

### 5.1 Round accounting

The loop is governed by two independently-configured budgets, matching the
two independently-tunable limits that already exist in
`models.DataSource` config today (`MaxExplorationRounds` for exploration;
what is currently `maxFinalRetries` for SQL error correction):

- **`maxExplorationRounds`** — how many `query_database` calls with
  `is_exploration: true` the model may make before it must produce a final
  query or answer directly.
- **`maxErrorRetries`** — how many times a *final* (`is_exploration: false`)
  query may fail execution and be retried with a corrected statement.

These are tracked as two separate running counters
(`explorationRoundsUsed`, `errorRetriesUsed`) so that exhausting one budget
can never silently consume the other. An exploration attempt — whether it
succeeds, is rejected for safety, or fails execution — always charges
`explorationRoundsUsed`. A failure on a *final* query always charges
`errorRetriesUsed`.

Three kinds of round are deliberately charged against **neither** budget,
because they are not retries of anything — they are the successful
conclusion of a turn:

1. A **successful final query** (the round that produces a pending result,
   §5.4).
2. A **`render_chart`** follow-up, if the model takes it.
3. The **`respond_to_user`** call that ends the turn.

This mirrors how `conversation.Summarize`'s second LLM call is not charged
against any retry counter today — reaching a successful conclusion is not a
retry, regardless of how many calls it took to get there.

The loop's outer iteration count is bounded by an unconditional safety
ceiling, `totalRoundCap := maxExplorationRounds + maxErrorRetries + 4`. The
`+4` is headroom for exactly the three uncounted round kinds above plus one
tolerance round for a single malformed/unrecognized tool call, so a
transient parse failure doesn't immediately exhaust the whole budget. It is
an internal implementation constant, not a user- or config-facing setting,
and is not meant to absorb a model that repeatedly sends malformed calls —
that failure mode is supposed to exhaust `totalRoundCap` and fall through to
a distinct "repeated invalid responses" clarification, so it is never
confused with an ordinary, well-understood budget exhaustion during support
or debugging.

Every round is tagged with a `roundKind`
(`exploration` / `error_retry` / `chart` / `final` / `other`) and logged via
`storePayload` with a label like `round-3-error_retry`, so tech-details
inspection can distinguish round types directly instead of showing an
undifferentiated counter.

### 5.2 Exploration safety is re-derived, not trusted

`is_exploration` is a flag the model self-reports on `query_database` calls.
It is not sufficient on its own to relax any safety control. Two checks run
on every exploration-flagged call, independent of what the model claims:

- **Read-only enforcement** (`executeSQLWithMode`) is unconditional
  regardless of `is_exploration` — this is the absolute, non-negotiable rule
  from `AGENT_READ_FIRST.md` §0 and applies identically to every query the
  tool ever executes.
- **Complexity gating** (`validateExplorationQuery` against the
  conversation's `ExplorationSafetyMode` — strict/moderate/relaxed) runs
  explicitly, exactly as it does today before any exploration query
  executes. Collapsing `sql_exploration` and `sql_query` into one tool with a
  boolean must not be allowed to quietly drop this check; it was doing
  double duty as a safety gate, not merely a routing hint, and the loop
  re-derives it on every exploration-flagged call rather than trusting the
  model's own claim about what kind of query it's running.

### 5.3 SQL execution failures resolve through the same loop

A final query that fails to execute (syntax error, missing column, etc.) is
not a separate code path — it is handled by feeding the error back to the
model as a `tool` role message and letting it call `query_database` again
with a corrected statement, exactly as it would respond to any other tool
result. The classification logic that exists today
(`isRetryableError`, `looksLikeSQLError`) still runs, and still matters:

- A **non-retryable** failure (permission-denied and similar entries in the
  `permanentlyFatal` list) is never fed back to the model for a retry it
  cannot productively act on — it surfaces immediately via `renderSQLError`.
- A **retryable** failure is fed back and charges `errorRetriesUsed`. If the
  budget is exhausted, the loop stops looping and surfaces the failure via
  `renderSQLError` rather than making one more attempt anyway.
- **Exponential backoff** for transient connection-level errors has no
  natural home inside a tight tool-call loop — a multi-second sleep between
  rounds is a real UX cost. When backoff is needed, the existing
  `processingPhase` event should be updated with a message like "Retrying
  after connection issue…" during the wait, per the standing UX pattern for
  any multi-step operation (`AGENT_READ_FIRST.md` §3.8).

### 5.4 Chart selection is deferred until real data exists

`render_chart` is a separate, optional follow-up call rather than a
same-call parameter on `query_database`, because a same-call parameter would
require the model to choose axes, chart type, and grouping *before* seeing a
single row of the result — no better informed than today's `viz_config`
field, which suffers the identical blindness. Deferring the decision to a
follow-up call lets the model choose based on the actual returned columns,
value ranges, and row count.

This requires one real change to the loop's control flow: a successful final
`query_database` call (`is_exploration: false`, no execution error) is no
longer automatically terminal. Its result is held as a `pendingFinalResult`
— the SQL and the `QueryResult`, nothing more, kept only in the loop's local
scope — and the loop runs one additional round so the model can look at the
result preview and decide whether to call `render_chart`.

```go
type pendingFinalResult struct {
    sql    string
    result *QueryResult
}

// render is the single choke point every exit path in the loop calls
// through, so "render a pending result" always means exactly one thing,
// done exactly once. chartConfig is "" for every chartless/degraded path.
func (p *pendingFinalResult) render(query *models.Query, dbConnection *models.DataSource, conversation *models.Conversation, chartConfig string) error {
    return renderToolQueryResults(query, p.sql, p.result, chartConfig, dbConnection, conversation)
}
```

If charting is disabled for the conversation, `render_chart` was never
offered to the model, there is nothing to wait for, and the result renders
immediately — identical to today's behavior for chart-disabled
conversations, with zero added latency. If charting is enabled, the extra
round costs one additional `ChatCompletionWithTools` call on every final
answer, whether or not a chart is ultimately produced; that cost is the
direct, accepted trade for chart selection informed by real data instead of
a guess.

**A pending result must never be silently lost.** Per the Graceful
Degradation Principle (`AGENT_READ_FIRST.md` §0), an optional enhancement can
never block or hide the primary result. Every exit point of the loop —
a plain-text reply instead of a tool call, an empty/truncated response,
another `query_database` call, the `respond_to_user` call, and the
max-rounds exhaustion fallback — checks for a non-nil pending result and
renders it (chartless) before doing anything else. A model that ignores the
chart offer and moves on can never cause the answer itself to disappear.
This is a hard acceptance criterion, not a best-effort guideline: it must be
verified for each of those flush points individually, not just the happy
path where `render_chart` is actually called.

Because a pending result is always resolved — rendered, with or without a
chart — before the turn's final `ConversationMessage` row is written, this
design needs no message-patching primitive. (No such primitive exists in the
codebase today; `pkg/services/conversation.go` has only
`CreateConversationMessage`, not update-in-place, and this design
deliberately avoids needing one.)

### 5.5 Three ways a turn can end, and why summarization is a different thing

After any number of exploration rounds, a turn concludes in exactly one of
three ways:

1. **A final `query_database` call**, producing a results table
   (`renderToolQueryResults`), optionally charted per §5.4.
2. **A `respond_to_user` call that references or summarizes a table the
   model already explored**, without ever running a final query — the model
   judges that describing the shape of what it found in prose serves the
   user better than dumping a raw table.
3. **A `respond_to_user` call with a short answer synthesized purely from
   exploration** — e.g. "Do we have any customers in Japan?" resolved by one
   `SELECT COUNT(*)...` exploration call and the answer "Yes, 3," with no
   table ever shown.

Outcomes 2 and 3 are not new capabilities; they are the direct equivalent of
today's `sql_exploration` → `answer` sequence. They matter here because it
would be easy to design the loop around the assumption that a database
question always ends in a table, and it does not — `respond_to_user` is a
fully valid final action regardless of how much exploration preceded it.

This is a distinct mechanism from `conversation.Summarize`, despite
superficially similar output (prose instead of a raw table).
`conversation.Summarize` is a conversation-level setting evaluated *after* a
results table already exists, triggering a second LLM call that writes
prose to sit *above* the table — the table remains present, just collapsed.
Outcomes 2 and 3 above are a per-turn model judgment made *instead of*
producing a table at all, with no setting and no second call — the same turn
that explored is the turn that answers. The two mechanisms are unrelated and
both remain fully functional under this design without modification to
either.

### 5.6 Exploration transparency is preserved

Today, exploration rounds are hidden by default but not invisible — they
persist as `ConversationMessage{Role: "exploration"}` rows and render as a
collapsible "Show N intermediate query(ies)" block
(`formatExplorationHTML`), satisfying the standing UX principle of
preferring transparency over magic (`AGENT_READ_FIRST.md` §3.8). This must
carry forward exactly: `runAgenticLoop` accumulates every `query_database`
call and its result for the current turn into a structure equivalent to
today's `[]ExplorationResult`, and both `handleRespond` (for outcomes 2 and
3 above) and `renderToolQueryResults` (for outcome 1) render that trace via
`formatExplorationHTML` or its tool-calling equivalent — exactly as
`renderSQLResults` already does via its `explorationResults` parameter
today. A question resolved entirely through exploration (outcome 3) must
remain just as auditable as one that ends in a table.

### 5.7 Multiple tool calls in one response

Some providers allow a single response to contain more than one tool call.
The loop processes each call in sequence within one round; per-round
tech-details logging reflects only the last call processed if a round
contains more than one, which is an accepted simplification for the debug
label only — it does not affect round-budget correctness, since each
counter is incremented individually and inline as each call is processed,
never derived from the label after the fact.

### 5.8 Full pseudocode

```go
type roundKind string

const (
    roundKindExploration roundKind = "exploration"
    roundKindErrorRetry  roundKind = "error_retry"
    roundKindChart       roundKind = "chart"  // metered against neither budget
    roundKindFinal       roundKind = "final"  // metered against neither budget
    roundKindOther       roundKind = "other"  // respond_to_user, malformed calls, unknown tool names
)

func (d *DiscussionEngine) runAgenticLoop(
    ctx context.Context,
    query *models.Query,
    client LLMClient,
    messages []ChatMessage,
    dbConnection *models.DataSource,
    conversation *models.Conversation,
    maxExplorationRounds int,
    maxErrorRetries int,
    safetyMode ExplorationSafetyMode,
) error {
    tools := []Tool{queryDatabaseTool, respondToUserTool}
    if conversation.VizEnabled {
        tools = append(tools, renderChartTool)
    }

    var pendingFinal *pendingFinalResult
    var explorationRoundsUsed, errorRetriesUsed int
    totalRoundCap := maxExplorationRounds + maxErrorRetries + 4

    for round := 0; round < totalRoundCap; round++ {
        response, reqJSON, respJSON, err := client.ChatCompletionWithTools(ctx, messages, tools)
        if err != nil {
            return fmt.Errorf("LLM call failed: %w", err)
        }

        // logRound tags and stores this round's tech-details payload,
        // distinguishing "exploration round 2" from "error-retry round 1"
        // etc. Called exactly once per iteration, explicitly, at every exit
        // point below — never via defer, which fires at function return in
        // Go, not end-of-iteration, and would log every round with the
        // FINAL iteration's values instead of its own.
        kind := roundKindOther
        logRound := func(k roundKind) {
            storePayload(conversation.ID, round, fmt.Sprintf("round-%d-%s", round, k), reqJSON, respJSON, messages)
        }

        // The assistant turn (with its ToolCalls, if any) must be appended
        // before any "tool" role replies — every provider requires an
        // assistant message with tool_calls to be immediately followed by
        // one matching tool message per call, or the request is rejected.
        messages = append(messages, *response)

        if response.Content != "" && len(response.ToolCalls) == 0 {
            // Plain text instead of a tool call. Treat as respond_to_user.
            logRound(roundKindOther)
            if pendingFinal != nil {
                if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                    return rerr
                }
            }
            return handleRespond(ctx, query, response.Content, conversation.ID)
        }

        if len(response.ToolCalls) == 0 {
            // Empty response — can happen if max_tokens truncated the
            // response mid-tool-call. A pending chart decision must never
            // block delivery of results the user already has a right to see.
            logRound(roundKindOther)
            if pendingFinal != nil {
                if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                    return rerr
                }
                return nil
            }
            return handleClarification(query, LLMResponse{
                Action:                "clarification",
                ClarificationQuestion: "I received an incomplete response. Could you try rephrasing your question?",
            }, conversation.ID)
        }

        for _, tc := range response.ToolCalls {
            switch tc.Function.Name {
            case "query_database":
                if pendingFinal != nil {
                    // The model already has a final result awaiting a chart
                    // decision and chose to run another query instead.
                    // Treat as an implicit decline; render what's pending
                    // (chartless) before processing the new call.
                    if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                        return rerr
                    }
                    pendingFinal = nil
                }

                var args struct {
                    SQL           string `json:"sql"`
                    IsExploration bool   `json:"is_exploration"`
                    Reasoning     string `json:"reasoning,omitempty"` // never rendered
                }
                if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
                    kind = roundKindOther
                    messages = append(messages, ChatMessage{
                        Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                        Content: fmt.Sprintf("Error parsing arguments: %v. Please retry with valid, complete JSON arguments.", err),
                    })
                    continue
                }

                if args.IsExploration && explorationRoundsUsed >= maxExplorationRounds {
                    kind = roundKindExploration
                    messages = append(messages, ChatMessage{
                        Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                        Content: "Exploration budget exhausted. You must now produce a final query_database call (is_exploration: false) or call respond_to_user with what you've learned so far.",
                    })
                    continue
                }

                if args.IsExploration {
                    if verr := validateExplorationQuery(args.SQL, safetyMode); verr != nil {
                        kind = roundKindExploration
                        explorationRoundsUsed++ // a rejected attempt still counts — not a free retry
                        messages = append(messages, ChatMessage{
                            Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                            Content: fmt.Sprintf("Exploration query rejected: %s. Please revise it to comply with safety constraints.", verr.Error()),
                        })
                        continue
                    }
                }

                result, execErr := executeSQLWithMode(dbConnection, args.SQL, args.IsExploration)

                if execErr != nil {
                    if args.IsExploration {
                        kind = roundKindExploration
                        explorationRoundsUsed++
                    } else {
                        kind = roundKindErrorRetry
                        if errorRetriesUsed >= maxErrorRetries {
                            return renderSQLError(query, LLMResponse{SQLQuery: args.SQL}, dbConnection, conversation.ID, nil, execErr)
                        }
                        errorRetriesUsed++
                        if !isRetryableError(execErr) {
                            return renderSQLError(query, LLMResponse{SQLQuery: args.SQL}, dbConnection, conversation.ID, nil, execErr)
                        }
                    }
                    messages = append(messages, ChatMessage{
                        Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                        Content: fmt.Sprintf("Query failed: %v", execErr),
                    })
                    continue
                }

                toolContent := formatToolResult(result) // truncated preview, §6.3
                messages = append(messages, ChatMessage{
                    Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: toolContent,
                })

                if !args.IsExploration {
                    kind = roundKindFinal
                    pendingFinal = &pendingFinalResult{sql: args.SQL, result: result}
                    if !conversation.VizEnabled {
                        return pendingFinal.render(query, dbConnection, conversation, "")
                    }
                    continue // give the model a round to optionally call render_chart
                }

                kind = roundKindExploration
                explorationRoundsUsed++

            case "render_chart":
                kind = roundKindChart
                var args struct {
                    ChartConfig string `json:"chart_config"`
                }
                if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || pendingFinal == nil {
                    reason := "no pending query result to attach a chart to"
                    if err != nil {
                        reason = fmt.Sprintf("error parsing arguments: %v", err)
                    }
                    messages = append(messages, ChatMessage{
                        Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                        Content: fmt.Sprintf("render_chart failed: %s.", reason),
                    })
                    continue
                }
                logRound(kind)
                result := pendingFinal.render(query, dbConnection, conversation, args.ChartConfig)
                pendingFinal = nil
                return result

            case "respond_to_user":
                kind = roundKindOther
                var args struct {
                    Text string `json:"text"`
                }
                if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
                    messages = append(messages, ChatMessage{
                        Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                        Content: fmt.Sprintf("Error parsing arguments: %v. Please retry with valid, complete JSON arguments.", err),
                    })
                    continue
                }
                logRound(kind)
                if pendingFinal != nil {
                    if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                        return rerr
                    }
                }
                return handleRespond(ctx, query, args.Text, conversation.ID)

            default:
                kind = roundKindOther
                messages = append(messages, ChatMessage{
                    Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
                    Content: fmt.Sprintf("Unknown tool: %s", tc.Function.Name),
                })
            }
        }

        logRound(kind) // covers every `continue` path above; every `return` path already logged itself
    }

    if pendingFinal != nil {
        return pendingFinal.render(query, dbConnection, conversation, "")
    }
    return handleClarification(query, LLMResponse{
        Action:                "clarification",
        ClarificationQuestion: "I wasn't able to complete this request due to repeated invalid responses. Could you try rephrasing your question?",
    }, conversation.ID)
}
```

---

## 6. Conversation History

### 6.1 The requirement

Every provider requires an exact, structurally valid sequence: an assistant
message with `tool_calls` must be immediately followed by one `tool` role
message per call, each carrying a matching `tool_call_id`, or the request is
rejected (or, for lenient providers, silently degraded). `runAgenticLoop`
builds this correctly within a single `ProcessUserMessage` call. The
question this section answers is what happens on the *next* user turn, when
the conversation is reloaded from storage and the prior turn's tool
structure has to be reconstructed.

### 6.2 Design: canonical transcript, translated fresh at read time

The key fact that makes full-fidelity replay practical: **tool-call IDs are
per-request correlation tokens, not durable identity.** Chat completions
APIs are stateless per request — the full conversation is resent from
scratch every time. An assistant message's `tool_calls[].id` only has to
match the `tool_call_id` on the following `tool` message(s) *within the
single payload being sent*; no provider checks that ID against anything
from a previous, separate HTTP request. IDs can therefore be freshly minted
every time history is replayed, with no cross-request or cross-provider
compatibility requirement at all — including across a mid-conversation
provider switch, which YourQL already supports today.

This means the persisted record of a turn's tool activity doesn't need to be
provider-shaped. A new nullable column on `conversation_messages` stores a
small, versioned, provider-neutral JSON record of what happened during an
assistant turn:

```go
ensureColumn("conversation_messages", "tool_transcript", "TEXT")
```

```go
// ToolTranscript is the canonical, provider-neutral record of one
// assistant turn's tool activity. Never contains provider-specific IDs or
// wire-format shape — those are synthesized fresh by buildToolMessages
// every time history is replayed.
type ToolTranscript struct {
    Version int                `json:"version"`
    Actions []TranscriptAction `json:"actions"`
}

type TranscriptAction struct {
    Tool          string `json:"tool"`      // "query_database", "render_chart", "respond_to_user"
    Arguments     string `json:"arguments"` // the exact JSON arguments the model produced
    ResultPreview string `json:"result_preview,omitempty"`
}
```

`runAgenticLoop` accumulates a `[]TranscriptAction` as it runs — the same
bookkeeping already required for exploration-trace transparency (§5.6),
serialized to a storable shape instead of discarded at the end of the turn.
`handleRespond` and `renderToolQueryResults` both persist the accumulated
transcript via `CreateConversationMessage`.

At request-build time, any history row with a populated `ToolTranscript` is
routed through a translator instead of the plain-content path:

```go
// buildToolMessages converts a canonical ToolTranscript into the message
// sequence the CURRENTLY CONFIGURED provider needs, with freshly minted IDs.
func buildToolMessages(t ToolTranscript) []ChatMessage {
    var out []ChatMessage
    var calls []ToolCall
    for i, action := range t.Actions {
        if action.Tool == "respond_to_user" {
            continue // rides on the assistant message's Content, not a tool call
        }
        calls = append(calls, ToolCall{
            ID:       fmt.Sprintf("hist_%d", i), // uniqueness within this request is the only requirement
            Function: ToolCallFunction{Name: action.Tool, Arguments: action.Arguments},
        })
    }
    assistantMsg := ChatMessage{Role: "assistant", ToolCalls: calls}
    if final := lastRespondToUser(t.Actions); final != "" {
        assistantMsg.Content = final
    }
    out = append(out, assistantMsg)
    for i, action := range t.Actions {
        if action.Tool == "respond_to_user" {
            continue
        }
        out = append(out, ChatMessage{
            Role: "tool", Content: action.ResultPreview,
            ToolCallID: fmt.Sprintf("hist_%d", i), Name: action.Tool,
        })
    }
    return out
}
```

`buildToolMessages` is provider-agnostic by construction — it produces the
same generic `ChatMessage`/`ToolCall` shapes defined in §4.1, and each
provider client is responsible for translating those into its own wire
format exactly as it already must for the live turn's tool calls. No new
per-provider history logic is required.

The alternative — collapsing a completed turn into a plain-text summary and
leaving `buildLlmMessages` untouched — was considered and rejected. It is
lower effort, but it degrades information on every turn: a follow-up
question referencing something specific about a prior query ("now filter
that to the West region") forces the model to reason from a paraphrase of
its own past SQL instead of the exact statement. Given that the per-request
ID fact above removes the only real objection to full fidelity (cross-
provider ID compatibility), full fidelity is the better design and is what
this document specifies.

`ToolTranscript.Version` exists because this shape will eventually need to
change. `buildToolMessages` must switch on `Version` and degrade a
transcript it doesn't recognize to a plain-text summary rather than erroring
the whole conversation load — the rejected alternative above becomes the
defined fallback for unrecognized versions, not the primary design.

Existing conversations, and any conversation that opts out of tool calling,
simply never populate `tool_transcript`; `buildLlmMessages`'s current
plain-content replay path is untouched for those rows.

---

## 7. Provider Support

### 7.1 OpenRouter / OpenAI

Full native support via the Chat Completions API's `tools` parameter.
`openAIChatRequest` gains a `Tools` field; `openAIChatMessage` gains
`ToolCalls` and `Name`; the response parser extracts `tool_calls` from
`Choices[].Message`.

### 7.2 Anthropic

Full native support via the Messages API's `tools` parameter. Anthropic's
wire format differs structurally from OpenAI's — tool calls and tool results
are content blocks nested inside a single message's `content` list, not
sibling messages with a flat `tool_calls` array — so `llm_anthropic.go`'s
translation of the shared `ChatMessage`/`ToolCall` vocabulary into that
nested shape needs a worked example during implementation, not just "map to
`tool_use` blocks."

### 7.3 Ollama and local/custom endpoints

Partial and variable. Ollama has supported tool calling since v0.3, but
quality varies by model; support for a given local or custom
OpenAI-compatible endpoint depends entirely on the backend. Both providers
implement native tools where the underlying model supports them, and fall
back to a minimal JSON protocol (§7.4) where it doesn't. Which path is in
use is a client-layer detail — the discussion engine only ever sees
`ChatMessage.ToolCalls`, regardless of how they were produced.

### 7.4 Fallback protocol for non-tool-capable models

A model without native tool support still needs *some* structured signal to
route on. The fallback is deliberately narrow, because its primary audience
— small or local models — is exactly the population most likely to fail at
embedding prose safely inside JSON (this is precisely what happened with
gemma-4-e4b-it in the incident that produced `ANSWER_CLARIFICATION_ISSUE.md`:
valid action JSON followed by unstructured prose outside the JSON object
entirely). A fallback that reintroduces prose-inside-JSON for its own target
audience would defeat the purpose of this whole change.

The fallback schema carries only `action` (`"sql"` or `"respond"`) and, when
`action == "sql"`, a `sql` field — never a `text`/prose field. When the model
is responding rather than querying, the prompt instructs it to emit
`{"action":"respond"}` on its own, followed by its full answer as plain
markdown text outside the JSON entirely. The fallback parser reads the small
JSON prefix to get `action`, then treats everything after the JSON object's
closing brace as the literal, unescaped answer — zero unescaping, zero
newline recovery, zero fence-matching required, because the prose was never
inside a JSON string to begin with. This inverts the actual failure mode
observed in production instead of fighting it: rather than trying to stop a
small model from putting prose outside the JSON (which real models do
reliably, and no amount of prompting reliably prevented), the fallback
requires it, and only asks the model to produce a two-token JSON prefix — a
dramatically easier task than escaping multi-paragraph markdown inside a
string.

This parser is new, narrow, purpose-built code isolated entirely to
`llm_ollama.go` and `llm_local.go`. It does not reuse any of the general-
purpose JSON-extraction machinery being removed (§8) — it is small enough to
test exhaustively on its own terms.

---

## 8. What Is Removed, and What Is Not

The JSON-response parsing pipeline is dead code once every provider produces
`ChatMessage.ToolCalls` natively or via the fallback in §7.4:

| Function | Reason |
|---|---|
| `extractJSONFromResponse` | No raw JSON to extract from response text |
| `fixJSONFormatting` | No double-encoded JSON to repair |
| `parseLLMResponse` | No response JSON to parse |
| `parseLenientJSON` / `extractFieldValue` | No lenient fallback needed |
| `unescapeNewlines` / `unescapeFields` | No double-encoded newlines |
| `looksLikeSQL` | No raw-SQL fallback parsing needed |

`_parse_error` action handling is removed from every switch statement that
handles it — it exists specifically to represent a JSON-parse failure, which
cannot occur once responses arrive as structured tool calls.

**`LLMResponse` itself is not removed.** It is also used by
`classifyErrorCategory`, `buildErrorMetadata`, and `renderSQLError` — none of
which are about JSON-response parsing; they belong to SQL error handling
(§5.3), which this design reuses rather than replaces. `LLMResponse` and
those three functions stay exactly as they are; only the action-routing
switch statements that consumed it for the old protocol are removed.

The `explanation` prompt field for the old protocol is removed; the tool
schema's `reasoning` parameter (§3.4) is its intentional, non-leaking
replacement and is not itself subject to removal. `viz_config` is removed
from the old protocol's prompt once every reference to it is confirmed
replaced by the `render_chart`/`chart_config` path (§3.3, §5.4).

---

## 9. Implementation Plan

### Phase 1 — LLM client layer

Because `ChatCompletionWithTools` is added to the shared `LLMClient`
interface, all five provider files gain a method implementation in this
phase, even though only OpenAI's is functionally real. "Lowest risk" here
means no existing behavior changes for any provider — not that fewer files
are touched.

- Add the shared vocabulary (§4.1) to `llm_client.go`; add
  `ChatCompletionWithTools` to the `LLMClient` interface with the two
  existing methods left untouched.
- Implement `ChatCompletionWithTools` for real in `llm_openai.go`: add
  `Tools` to `openAIChatRequest`; add `ToolCalls`/`Name` to
  `openAIChatMessage`; parse `tool_calls` out of the response.
- Implement `ChatCompletionWithTools` as a passthrough stub in
  `llm_anthropic.go`, `llm_ollama.go`, `llm_local.go` — ignore `tools`,
  delegate to the existing `ChatCompletionWithPayload`, return a
  `Content`-only `*ChatMessage`. This is the minimum required for the
  package to compile; it is explicitly not those providers' real tool
  support, which is Phase 3/4 work.
- Verify `go build ./...` and the existing test suite pass with zero
  behavior change for every provider.

**Deliverable:** the interface compiles with all five implementers.
OpenAI/OpenRouter can send tool definitions and receive structured tool
calls. Every other provider is unchanged.

### Phase 2 — the agentic loop

- Implement `runAgenticLoop` per §5.8, including the exploration-complexity
  gate (§5.2), the error-retry classification and dual budget counters
  (§5.3, §5.1), the deferred-chart state machine (§5.4), and the
  exploration-trace accumulation (§5.6).
- Implement `handleRespond` (replaces `handleAnswer`), rendering the current
  turn's accumulated exploration trace alongside the response text.
- Implement `renderToolQueryResults` (replaces `renderSQLResults`), handling
  chart attachment via the deferred `render_chart` flow and the same
  exploration-trace rendering as `handleRespond`.
- Implement `formatToolResult` with the truncation policy in §6.3 below (row
  cap and cell-length cap, independent of any `LIMIT` the generated SQL may
  or may not include).
- Implement `pendingFinalResult` and its `render` method (§5.4).
- Implement `ToolTranscript`, `TranscriptAction`, the `tool_transcript`
  column, and `buildToolMessages` (§6); wire the accumulation into
  `runAgenticLoop` and persist it on every `handleRespond` /
  `renderToolQueryResults` call.
- Add the `use_tool_calling` feature flag (§10.2); gate the new path behind
  it, leaving the existing JSON-response path fully intact for conversations
  that don't opt in.

**Acceptance criteria for this phase:**

1. A final query result with charting enabled, where the model responds with
   plain text, another `query_database` call, or an unusable/truncated
   response instead of calling `render_chart`, still renders the results
   table to the user exactly once, chartless. Test each of those four flush
   points individually, not just the happy path where `render_chart` is
   called.
2. A multi-turn conversation's second turn correctly reconstructs the first
   turn's *exact* prior `query_database` call — not a paraphrase — via
   `buildToolMessages`, and the resulting request is accepted by the
   provider. Use a follow-up question that depends on the literal prior SQL
   (e.g., "now filter that to the West region"), since the fidelity this
   design targets only shows up when the follow-up genuinely needs it.
3. `buildToolMessages` degrades to a plain-text summary — not an error —
   when given a `ToolTranscript` with an unrecognized `Version`.
4. A question resolved entirely through exploration (§5.5, outcome 3) is
   still auditable: the user can expand a trace of every `query_database`
   call made during that turn, matching today's "Show N intermediate
   query(ies)" behavior.

**Deliverable:** new conversations can opt into tool calling and survive at
least two conversational turns without malformed API requests. Existing
conversations are unaffected.

### Phase 3 — Anthropic

- Replace the Phase 1 stub in `llm_anthropic.go` with a real implementation:
  map `ChatCall`/`ToolCall` to Anthropic's `tool_use` content blocks, and
  `tool` role messages to `tool_result` content blocks (worked example
  required before implementation — see §7.2).
- Test with Claude models, including Phase 2's multi-turn acceptance
  criterion.

### Phase 4 — fallback protocol

- Implement the two-field fallback from §7.4 for `llm_ollama.go` and
  `llm_local.go`.
- Regression-test explicitly against a small local model. The gemma-4-e4b-it
  failure mode documented in `ANSWER_CLARIFICATION_ISSUE.md` is the
  regression test for this phase: it must no longer produce a truncated or
  leaked response.

**Deliverable:** every provider path produces `ChatMessage.ToolCalls`,
natively or via the fallback, verified against at least one real small/local
model that previously failed under the old protocol.

### Phase 5 — cleanup and default flip

- Remove the dead parsing code enumerated in §8, and `_parse_error` handling
  from every switch statement.
- Audit remaining `LLMResponse.Explanation` call sites and trim only what is
  confirmed unused outside the retained SQL-error-handling path (§8) —
  `LLMResponse` itself is not removed.
- Remove the old protocol's `explanation` and `viz_config` prompt text once
  every reference is confirmed migrated to `reasoning` (§3.4) and
  `render_chart`/`chart_config` (§3.3) respectively.
- Flip the *application-level* default for newly created conversations'
  `use_tool_calling` to `true`. This is separate from the column's SQL
  default, which stays `0` forever (§10.2) so historical rows correctly
  backfill as `false`.
- Remove the feature flag entirely only after a full deprecation window with
  no reported regressions.

---

## 10. Risk, Safety, and Compliance

### 10.1 Absolute rules

Per `AGENT_READ_FIRST.md` §4.0, three rules are never subject to risk/reward
trade-off. All three are unaffected by this change:

| Rule | Status |
|---|---|
| Never execute a write statement against a data source | Unchanged. `executeSQLWithMode` is the sole execution entry point for every tool call, exactly as it is today; the `query_database` schema explicitly requires `SELECT`-only queries, and read-only enforcement is unconditional regardless of `is_exploration`. |
| Never log API keys or database passwords | Unchanged. No logging call sites are modified. |
| Never destructively alter an existing column | Unchanged. Every schema change in this document is an additive, nullable `ensureColumn` call (§10.2, §6.2). |

Exploration complexity gating (`validateExplorationQuery` /
`ExplorationSafetyMode`) is not one of the three absolute rules — it is a
configurable control, and is therefore subject to ordinary risk/reward
reasoning below rather than being non-negotiable. §5.2 specifies why it must
still run unconditionally under the new design: dropping it would be a real
regression in capability control, even though it isn't an absolute-rule
violation.

### 10.2 Feature flag

```go
ensureColumn("conversations", "use_tool_calling", "INTEGER DEFAULT 0")
```

The default is `0`, not `1`. A default of `1` would silently switch every
existing conversation onto the new loop the moment the migration runs, which
directly contradicts the backward-compatibility guarantee this whole rollout
depends on. New conversations may have their *application-level* initial
value set to `true` once Phases 2–4 are confirmed stable across all
providers (§9, Phase 5) — that is a distinct decision from the column's SQL
default, which stays `0` permanently so historical rows always correctly
backfill as opted-out.

### 10.3 Risk assessment

| Category | Assessment |
|---|---|
| Answer accuracy | Improved. Removes four classes of parsing bugs entirely; the model produces structured arguments instead of prose-with-embedded-JSON. |
| Answer delivery | Net positive. The agentic loop is a well-established pattern; tool arguments are simple flat JSON with no escaping surface. The one added cost — one extra round-trip per chartable final answer (§5.4) — is scoped to conversations with charting enabled and is an explicit, accepted trade for better chart selection. |
| Data safety | Unchanged. Single execution entry point, unconditional read-only enforcement, unchanged row limits. |
| Data integrity | Unchanged. No destructive schema changes; two new nullable, additive columns. |
| User trust | Improved. Fewer rendering failures translate directly to more reliable answers. |
| Regression | Medium, actively mitigated. The discussion engine's core loop is substantially rewritten, and the history-persistence translator (§6.2) is new, previously-unwritten code. Mitigated by: a feature flag defaulting to off (§10.2), the old JSON-response path remaining fully intact and untouched for any conversation that doesn't opt in, and `ToolTranscript` versioning (§6.2) so the translator degrades gracefully instead of failing hard if its own schema changes later. |
| UX/Comfort | Improved. Same rendering pipeline downstream of tool resolution; fewer failure modes upstream of it. |

### 10.4 Failure modes and mitigations

1. **A model doesn't support tools at all.** Handled by the fallback
   protocol (§7.4), scoped specifically to avoid reintroducing
   prose-inside-JSON for the small/local models most likely to need it.
2. **A model calls the wrong tool or produces malformed arguments.**
   `json.Unmarshal` failures on tool arguments are treated as an expected,
   recoverable case — the loop appends a `tool` role error message and
   continues rather than aborting the turn (§5.8).
3. **A model skips exploration and its final query fails.** Handled by the
   same error-retry mechanism described in §5.3 — the failure is fed back
   and the model gets a bounded number of corrected attempts.
4. **The loop never terminates.** Bounded by two independent, meaningful
   budgets (§5.1) plus an unconditional `totalRoundCap` safety ceiling that
   also bounds every uncounted round kind. Either meaningful budget's
   exhaustion produces an explicit, budget-specific message immediately,
   rather than silently falling through to the ceiling.
5. **`max_tokens` truncates a response mid-tool-call.** The same class of
   risk a truncated prose answer carries under the old protocol — tool
   calling makes this easier to detect, not impossible. A response with
   unparseable arguments or with neither content nor tool calls is treated
   as a recoverable, expected case (§5.8), never a hard failure of the
   conversation turn.

### 10.5 Reward

- Eliminates the bug class documented in `ANSWER_CLARIFICATION_ISSUE.md` —
  four of its five entries cannot recur under this design.
- Removes roughly 300 lines of fragile, compensating parsing code (§8).
- Improves answer quality for smaller models specifically, for whom
  producing a valid tool call is a substantially easier task than escaping
  multi-paragraph markdown inside a JSON string.
- Enables chart selection informed by real data (§5.4) and unifies SQL
  error-retry with the ordinary exploration loop (§5.3), which are both
  capability improvements beyond bug elimination.
- Follows the same tool-calling pattern used by every major coding
  assistant and agentic LLM application in production today.

### 10.6 Charter compliance

- **Fundamental goal** (`AGENT_READ_FIRST.md` §0): answer delivery reliability
  improves; answer accuracy is unaffected in the negative direction and
  improves for smaller models specifically.
- **Safety**: unchanged — see §10.1.
- **Graceful degradation**: the fallback protocol (§7.4) and the mandatory
  pending-result flush guarantee (§5.4) are both direct implementations of
  this principle, not incidental side effects of the design.
- **Risk/reward**: documented in full above; proceed per §10.3's medium-risk,
  actively-mitigated assessment.

---

## 11. Files Affected

| File | Change |
|---|---|
| `pkg/services/llm_client.go` | Add the shared `Tool`/`ToolCall`/`ToolCallFunction`/`FunctionDef` types (§4.1). Add `ChatCompletionWithTools` to `LLMClient` (§4.2) — existing methods unchanged. Add `ToolCalls`/`ToolCallID`/`Name` to `ChatMessage`. |
| `pkg/services/llm_openai.go` | Add `Tools` to the request struct and `ToolCalls`/`Name` to message structs. Real `ChatCompletionWithTools` implementation (Phase 1). |
| `pkg/services/llm_anthropic.go` | Phase 1: passthrough stub. Phase 3: real `tool_use`/`tool_result` content-block mapping (§7.2). |
| `pkg/services/llm_ollama.go` | Phase 1: passthrough stub. Phase 4: two-field JSON fallback (§7.4). |
| `pkg/services/llm_local.go` | Phase 1: passthrough stub. Phase 4: two-field JSON fallback (§7.4). |
| `pkg/services/discussion_engine.go` | Add `runAgenticLoop` (§5), `handleRespond`, `renderToolQueryResults`, `formatToolResult`, `pendingFinalResult`, `ToolTranscript`/`TranscriptAction`/`buildToolMessages` (§6). Gate behind the feature flag. `LLMResponse` and its SQL-error-handling consumers are retained, not removed (§8). |
| `pkg/models/conversation.go` | Add `UseToolCalling bool` (`json:"use_tool_calling"`) alongside existing `VizEnabled`/`Summarize`. |
| `pkg/models/conversation_message.go` | Add `ToolTranscript *string` (`json:"tool_transcript,omitempty"`) alongside existing `LLMContent`/`SQLResults`/`Metadata`. |
| `pkg/models/database.go` | `ensureColumn("conversations", "use_tool_calling", "INTEGER DEFAULT 0")` and `ensureColumn("conversation_messages", "tool_transcript", "TEXT")` — both additive and nullable, per §3.2 of `AGENT_READ_FIRST.md`. |
| `app.go` | Optional: expose the `UseToolCalling` toggle in conversation settings, following the existing `VizEnabled`/`Summarize` binding pattern. |

---

## 12. References

- [`ANSWER_CLARIFICATION_ISSUE.md`](../issues/ANSWER_CLARIFICATION_ISSUE.md) — the five
  bugs this design exists to make structurally impossible.
- [`AGENT_READ_FIRST.md`](../AGENT_READ_FIRST.md) — project charter, risk
  framework, and goal priority order governing every decision above.
- [`RISK_ANALYSIS_LOG.md`](../RISK_ANALYSIS_LOG.md) — append this document's
  §10 risk assessment before implementation begins.
- [OpenAI Function Calling Guide](https://platform.openai.com/docs/guides/function-calling)
- [Anthropic Tool Use Guide](https://docs.anthropic.com/en/docs/build-with-claude/tool-use)
- [OpenRouter Tool Calling](https://openrouter.ai/docs/features/tool-calling)
