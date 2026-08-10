# Tool Call Enhancement — Technical Proposal

> **Status:** Proposed. Not yet implemented. Revised after technical review
> and four follow-up design discussions (see §9) — five blocking issues, six
> design gaps, and three deliberate design decisions (data-informed charting,
> full-fidelity history persistence, dual round-budget counters) have been
> identified and folded in against the live codebase and against real usage
> patterns (exploration-driven answers, chart timing, multi-turn follow-up
> questions, retry-budget isolation). All fourteen are addressed in the
> sections below; nothing in this document should be treated as ready to
> implement until the items marked **[REVIEW]** are
> resolved in code, not just in prose.
>
> **Aligns with fundamental goal (§0):** Eliminates an entire class of answer-delivery
> bugs by replacing hand-rolled JSON parsing with a first-class LLM API feature.

---

## 0. The Problem: JSON Parsing Is a Fragile Choke Point

The current LLM response protocol requires the model to produce a JSON object
with prose embedded inside string fields:

```json
{"action":"answer","answer":"**Yes.**\n\nThe query has two issues:\n\n```sql\nCROSS JOIN...\n```"}
```

This single string must survive **four transformation stages** before reaching
the user:

1. **LLM generation** — the model must embed markdown (with backticks, quotes,
   newlines, asterisks) inside a JSON string value, correctly escaping all
   special characters
2. **API transport** — the provider wraps this JSON inside another JSON response
   body, double-encoding all escape sequences (`\n` → `\\n` → `\\\\n` in raw)
3. **Application extraction** — `extractJSONFromResponse` must isolate the JSON
   from code fences, thinking markers, and other LLM artifacts
4. **Application parsing** — `parseLLMResponse` must unmarshal the JSON,
   recover double-encoded newlines, and route to the correct handler

**Five bugs have been caused by this pipeline** (see
[`ANSWER_CLARIFICATION_ISSUE.md`](ANSWER_CLARIFICATION_ISSUE.md)):

| Bug | Root Cause |
|---|---|
| Explanation leaked to user | `ToHTML()` rendered `Explanation` field |
| SQL query silently dropped | Prompt/contract mismatch for `answer` actions |
| Smartypants corrupted SQL | Markdown renderer flags destroyed `--` comments |
| Newlines became literal `\n` | Double JSON encoding + no unescaping |
| Answer truncated to code block | `extractJSONFromResponse` matched fences inside JSON |

**Four of these five bugs** (all except the explanation leak) are **direct
consequences of the JSON-response protocol.** The explanation leak is also
enabled by it. Each fix added complexity — a state machine
(`fixJSONFormatting`), a regex preprocessor (`normalizeMarkdown`), a lenient
JSON parser (`parseLenientJSON` / `extractFieldValue`), guard logic
(`extractJSONFromResponse` fence detection) — all to compensate for the
fundamental fragility of asking an LLM to produce valid JSON with prose
embedded inside it.

**This class of bug is inherent to the protocol, not to any specific
implementation mistake.** As long as YourQL asks the LLM to produce
prose-inside-JSON, new edge cases will emerge with new models, new response
formats, and new markdown content.

---

## 1. The Solution: Native Function / Tool Calling

Every major LLM provider (OpenAI, Anthropic, Google, OpenRouter) supports
**native function calling** — also called "tool use." The LLM does not produce
raw JSON for the application to parse. Instead, it produces a structured
**tool call** object that the API client library unmarshals directly into Go
structs:

```json
{
  "choices": [{
    "message": {
      "tool_calls": [{
        "id": "call_abc123",
        "function": {
          "name": "execute_sql",
          "arguments": "{\"sql\":\"SELECT * FROM customers\"}"
        }
      }]
    }
  }]
}
```

**No application-level JSON parsing is required.** The provider's API response
is unmarshalled by `encoding/json` into typed Go structs. The `arguments` field
is a JSON string representing the function's parameters — but this is a
simple flat structure with no embedded prose, no markdown, no special
characters to escape. It is parsed by the same `encoding/json` that already
works correctly for the API response envelope.

### 1.1 Why This Eliminates the Bug Class

Under the current protocol, the LLM must:
- Embed prose containing backticks, quotes, and newlines inside a JSON string
- Escape all special characters correctly
- Produce output that survives double-JSON-encoding through the API transport

Under tool calling, the LLM:
- Calls a named function with simple typed arguments (`sql`, `text`)
- The prose answer is a flat string argument — no markdown-inside-JSON nesting
- The API client handles all transport encoding

**All four JSON-parsing bugs from ANSWER_CLARIFICATION_ISSUE.md become
impossible under this architecture:**

| Bug | Why It Can't Happen |
|---|---|
| SQL query dropped | The `execute_sql` tool is only called for queries; `respond` is only called for prose |
| Smartypants corrupts SQL | SQL is extracted from a structured argument, never processed as markdown prose |
| Newlines as literal `\n` | No double-encoding — the API client handles transport |
| Answer truncated by fence extraction | No `extractJSONFromResponse` — response is never raw text |
| Explanation leak | `explanation` is not a function argument — it's never in the response |

---

## 2. Technical Design

### 2.1 Tool Definitions

Two core tools, plus one conditional tool (`render_chart`, §2.1.1/§2.3.4,
offered only when charting is enabled), replace the current four action types:

```go
type Tool struct {
    Type     string       `json:"type"`     // "function"
    Function FunctionDef  `json:"function"`
}

type FunctionDef struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`
}
```

**Tool 1: `query_database`** — replaces `sql_query` and `sql_exploration`

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

**Tool 2: `respond_to_user`** — replaces `answer` and `clarification`

```go
{
    Type: "function",
    Function: FunctionDef{
        Name:        "respond_to_user",
        Description: "Provide a direct response to the user. Use this when the question does not require querying the database — e.g., follow-up questions about previously returned data, explanations of how a query works, or general knowledge questions.",
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

**Tool 3: `render_chart`** — replaces the `viz_config` field (see §2.1.1 for
why this is a separate tool, called *after* the model has seen real data,
rather than a same-call parameter on `query_database`)

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

#### 2.1.1 [REVIEW — Blocking #2, REVISED] `viz_config` as a data-informed follow-up tool, not a same-call parameter

The current protocol's `resp.VizConfig` (an `LLMResponse` field,
`discussion_engine.go` line 23) is consumed at `renderSQLResults` (line
1444-1445) to render a chart when `conversation.VizEnabled` is true. This is
a real, shipped feature — an earlier draft of this document proposed removing
the field with no replacement (would have silently deleted charting), then a
follow-up revision added it back as a `chart_config` parameter on
`query_database` itself. **This subsection supersedes that revision.**

**Why a parameter on `query_database` is the wrong shape.** A same-call
`chart_config` parameter requires the model to specify chart type, axes, and
grouping *before* it has seen a single row of the result — it can only guess
from column names and the SQL it just wrote. This is no worse than today's
behavior (`resp.VizConfig` is also specified blind, at the same time as the
SQL), but it is also no *better*, and doesn't use one of the actual
advantages tool calling makes newly practical: the model can wait, look at
real returned data, and only then decide how to visualize it.

**Design: `render_chart` is a separate tool, called only after a `tool` role
message containing the final query's actual results has been appended to the
conversation.** This requires `runAgenticLoop` to no longer treat a
`query_database` call with `is_exploration: false` as an automatic, immediate
terminal state — which is a real change to the loop's control flow, not a
cosmetic one. See §2.3's revised pseudocode and §2.3.4 below for exactly how
this is implemented without reopening the SQL-execution/rendering pipeline to
double-execution or duplicate row limits.

Same JSON shape as before (`$column` references resolved by
`resolveChartConfig`), same gating (`conversation.VizEnabled` — `render_chart`
is simply omitted from the `tools` list sent to the model when charting is
disabled for the conversation, so a disabled model literally cannot call it).

#### 2.1.2 [REVIEW — Blocking #3] `explanation` is not simply deleted

`ANSWER_CLARIFICATION_ISSUE.md` §6.1 keeps the `explanation` field in the
prompt *by design*, specifically because chain-of-thought before producing
SQL measurably improves generation accuracy for models that don't have a
native reasoning trace (e.g., non-`o1`/non-extended-thinking models). The
original draft of this document removed the field outright in §5 Phase 5 with
the justification "no longer used" — true only for *rendering*, not for the
accuracy purpose it was added for. The `reasoning` parameter added to
`query_database` above is the direct replacement: it preserves the
chain-of-thought benefit as an optional tool argument that is *never*
rendered to the user (unlike the old field, which leaked through multiple
paths documented in `ANSWER_CLARIFICATION_ISSUE.md`). `respond_to_user` has no
equivalent `reasoning` parameter by design — prose answers don't need a
separate scratchpad field; the model can reason in its own turn before
calling the tool, and reasoning-capable models already do this via their
native `reasoning`/`thinking` response fields, which are provider-level and
orthogonal to tool arguments.

### 2.2 LLM Client Interface Changes

**Current interface:**

```go
type LLMClient interface {
    ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
    ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
}
```

#### 2.2.1 [REVIEW — Blocking #4] The interface must be extended, not mutated

The original draft of this document changed the *signatures* of
`ChatCompletion` and `ChatCompletionWithPayload` in place (adding a `tools
[]Tool` parameter and changing the return type to `*ChatMessage`). **This does
not work in Go.** Interface methods cannot be overloaded — the moment the
`LLMClient` interface's method signatures change, every type implementing
that interface must be updated in the same commit just to keep the package
compiling, regardless of whether that provider has been wired up for tool
calling yet. Concretely: `AnthropicClient`, `OllamaClient`, and `LocalClient`
all implement `LLMClient` today (`llm_anthropic.go`, `llm_ollama.go`,
`llm_local.go`), and `go build ./...` fails immediately if only
`OpenAIClient`'s methods are updated to a new signature. This contradicts the
Phase 1 goal of "lowest risk, OpenAI-only" (see §5 Phase 1, revised).

**Fix: add new methods; do not change existing ones.** The existing
`ChatCompletion` / `ChatCompletionWithPayload` methods and their signatures
are left completely untouched — they remain the fallback path for callers
that don't need tools (e.g., `summarizeResults`, which never needs tool
calling). A new method is added to the interface for tool-aware calls. Every
provider must implement it (Go requires all interface methods to be
implemented, so this still touches all five provider files — see the revised
file list in §7), but providers that don't yet support tools natively can
implement it as a **thin wrapper that ignores `tools` and delegates to the
existing `ChatCompletionWithPayload`**, returning a `*ChatMessage` with only
`Content` set and `ToolCalls` empty. This keeps every provider compiling and
functionally unchanged from day one of Phase 1, and lets each provider's real
tool support land independently in later phases without ever breaking the
build.

**Proposed interface (additive):**

```go
type ChatMessage struct {
    Role       string     `json:"role"`
    Content    string     `json:"content,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    // Name is required by some providers (OpenAI) on "tool" role messages to
    // echo which function produced the result being replied to. Optional for
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

// LLMClient is UNCHANGED — both existing methods keep their exact current
// signatures. This means every existing call site (including
// summarizeResults and any other caller that has no need for tools)
// continues to compile and behave identically with zero changes.
type LLMClient interface {
    ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
    ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)

    // ChatCompletionWithTools is the tool-aware entry point. Providers
    // without native tool support MUST still implement this method (Go
    // requires all interface methods to be implemented on any type used as
    // an LLMClient), but may implement it as a passthrough that ignores
    // `tools` and wraps ChatCompletionWithPayload, returning a ChatMessage
    // with only Content populated. This guarantees the package always
    // compiles and every provider is usable from day one, independent of
    // when its real tool support lands (see Phases 1/3/4 in §5).
    ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (msg *ChatMessage, requestJSON, responseJSON string, err error)
}
```

The discussion engine's agentic loop (§2.3) calls `ChatCompletionWithTools`
exclusively. The pre-existing `ChatCompletion` / `ChatCompletionWithPayload`
methods remain in place for every non-tool-calling caller in the codebase
(e.g. `summarizeResults`), untouched, forever — there is no reason to migrate
them.

### 2.3 Discussion Engine Changes

The current exploration loop (up to `maxRounds` iterations of
`sql_exploration` → `sql_query`) is replaced by an **agentic loop**. The
pseudocode below is revised from the original draft to close three gaps
identified in review: missing `validateExplorationQuery` enforcement
(Design Gap #6), missing SQL-error-retry handling (Blocking #5 / see new
§2.7), and no truncation strategy for tool results (Design Gap #7). It has
also been revised a second time to split the single `maxRounds` budget into
two independently-tracked counters (§2.7's recommendation, now implemented
rather than just described) and to make explicit that `render_chart` rounds
are metered against neither counter (§2.3.4).

```go
// roundKind distinguishes what a round of the agentic loop was "spent on,"
// for two purposes: (1) charging the round against the correct budget
// counter below, and (2) tagging storePayload's tech-details entry so a
// human debugging a conversation can tell "exploration round 2" apart from
// "error-retry round 1" instead of seeing an undifferentiated round number.
type roundKind string

const (
    roundKindExploration roundKind = "exploration"
    roundKindErrorRetry  roundKind = "error_retry"
    roundKindChart       roundKind = "chart"          // metered against neither budget
    roundKindFinal       roundKind = "final"          // the round that produced pendingFinal; metered against neither budget once IsExploration is false and execErr is nil
    roundKindOther       roundKind = "other"          // respond_to_user, malformed tool calls, unknown tool names
)

func (d *DiscussionEngine) runAgenticLoop(
    ctx context.Context,
    query *models.Query,
    client LLMClient,
    messages []ChatMessage,
    dbConnection *models.DataSource,
    conversation *models.Conversation,
    maxExplorationRounds int, // [REVIEW #14] replaces the single maxRounds — budget for is_exploration:true query_database calls
    maxErrorRetries int,      // [REVIEW #14] budget for query_database calls that follow a failed execution (§2.7)
    safetyMode ExplorationSafetyMode, // [REVIEW #6] carried through, same as today
) error {
    tools := []Tool{queryDatabaseTool, respondToUserTool}
    if conversation.VizEnabled {
        // [REVIEW B] render_chart is only offered to the model when charting
        // is enabled — an unauthorized model literally cannot call a tool it
        // was never told about.
        tools = append(tools, renderChartTool)
    }

    // [REVIEW B] pendingFinal holds a final (non-exploration) query_database
    // result that has been executed and returned to the model as a "tool"
    // message, but not yet rendered to the user — because we are giving the
    // model one more round to optionally call render_chart having now seen
    // the real data. See §2.3.4 for the full state machine this introduces.
    var pendingFinal *pendingFinalResult

    // [REVIEW #14] explorationRoundsUsed and errorRetriesUsed are the two
    // independent counters replacing the single flat `round`/`maxRounds`
    // loop variable. They are incremented ONLY when a round is actually
    // spent on that kind of work — a render_chart round, a final successful
    // query round, a respond_to_user round, or a malformed-tool-call round
    // increments neither counter (see the increment sites below, each
    // tagged with the roundKind it corresponds to). The loop itself is now
    // bounded by an unconditional safety ceiling (totalRoundCap) rather than
    // a single meaningful budget, since "how many rounds are allowed" is now
    // a question with two different, independently-configurable answers.
    var explorationRoundsUsed, errorRetriesUsed int
    totalRoundCap := maxExplorationRounds + maxErrorRetries + 4 // +4 headroom for final/chart/respond/malformed-call rounds, which are uncounted but still finite — see §2.3.5

    for round := 0; round < totalRoundCap; round++ {
        response, reqJSON, respJSON, err := client.ChatCompletionWithTools(ctx, messages, tools)
        if err != nil {
            return fmt.Errorf("LLM call failed: %w", err)
        }

        // [REVIEW #14] logRound tags and stores this round's payload for
        // tech-details, distinguishing "exploration round 2" from
        // "error-retry round 1" etc. instead of an undifferentiated round
        // number — the concrete implementation of §2.3's "distinguish for
        // debugging" requirement. It is called explicitly, exactly once, at
        // every exit point of this iteration below (every `return` and every
        // `continue` in the switch below is preceded by a `logRound(...)`
        // call with the roundKind that actually applies to that branch).
        // Deliberately NOT implemented via `defer`: in Go, `defer` inside a
        // `for` loop fires at function return, not end-of-iteration, and
        // would capture every closed-over variable's FINAL value across all
        // rounds instead of each round's own — every tech-details entry in a
        // multi-round conversation would silently log identical, wrong data.
        // (Caught in review — an earlier revision of this pseudocode made
        // exactly this mistake.)
        logRound := func(k roundKind) {
            storePayload(conversation.ID, round, fmt.Sprintf("round-%d-%s", round, k), reqJSON, respJSON, messages)
        }

        // The assistant turn (with its ToolCalls, if any) MUST be appended to
        // `messages` before any "tool" role replies are appended — this is a
        // hard API requirement for every provider (a "tool" message with no
        // preceding matching tool_calls entry is a 400 error, not a soft
        // failure). See §2.3.1 for how this sequence is persisted across turns.
        messages = append(messages, *response)

        if response.Content != "" && len(response.ToolCalls) == 0 {
            // Plain text response (no tool call) — some models may do this
            // despite tools being offered. Treat as respond_to_user.
            logRound(roundKindOther)
            // [REVIEW B] If a final query result is already pending a chart
            // decision and the model responded with plain text instead of
            // calling render_chart, the model has implicitly declined to
            // chart. Render the pending result now (chartless), THEN the
            // text response — do not let the pending result get silently
            // lost just because the model moved on. See §2.3.4.
            if pendingFinal != nil {
                if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                    return rerr
                }
            }
            return handleRespond(ctx, query, response.Content, conversation.ID)
        }

        if len(response.ToolCalls) == 0 {
            // [REVIEW #10] Empty response with no content and no tool calls
            // can happen if max_tokens truncated the response mid-tool-call.
            logRound(roundKindOther)
            // [REVIEW B] Per the Graceful Degradation Principle
            // (AGENT_READ_FIRST.md §0), a truncated chart decision must never
            // block delivery of results the user already has a right to see.
            // If a final result is pending, render it (chartless) before
            // surfacing the clarification — never let "the chart didn't work
            // out" become "the answer never arrived."
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

        // [REVIEW #14] NOTE: if a single response contains MULTIPLE tool
        // calls (some providers support this — §2.3.4 Variant A discusses
        // parallel calls), `continue` below only continues this inner `tc`
        // loop, not the outer round loop. `kind` and `logRound` at the
        // bottom of the outer loop reflect only the LAST tool call
        // processed in a multi-call response, not each one individually.
        // This is an accepted simplification for tech-details labeling
        // (worst case: a mixed-outcome round is tagged by its last call
        // instead of showing all outcomes) — it does NOT affect budget
        // correctness, since explorationRoundsUsed/errorRetriesUsed are
        // incremented individually, inline, at the moment each tool call is
        // processed, not derived from `kind` after the fact. If per-call
        // tech-details granularity is wanted later, logRound can be called
        // inside this inner loop instead of once at the bottom — deferred
        // as a refinement, not required for correctness.
        for _, tc := range response.ToolCalls {
            switch tc.Function.Name {
            case "query_database":
                if pendingFinal != nil {
                    // [REVIEW B] The model already got a final result this
                    // turn and chose to run ANOTHER query_database call
                    // instead of calling render_chart or respond_to_user.
                    // Treat this the same as an implicit decline to chart —
                    // render what's pending (chartless) before processing
                    // the new call, so the first result is never dropped.
                    if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                        return rerr
                    }
                    pendingFinal = nil
                }
                var args struct {
                    SQL          string `json:"sql"`
                    IsExploration bool  `json:"is_exploration"`
                    Reasoning    string `json:"reasoning,omitempty"`   // [REVIEW #3], never rendered
                }
                // [REVIEW #10] tc.Function.Arguments can be truncated JSON if
                // max_tokens cut the response off mid-argument. json.Unmarshal
                // returning an error is the expected, handled case for this —
                // do not treat it as a hard failure of the whole loop.
                if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
                    kind = roundKindOther
                    messages = append(messages, ChatMessage{
                        Role:       "tool",
                        Content:    fmt.Sprintf("Error parsing arguments: %v. Please retry with valid, complete JSON arguments.", err),
                        ToolCallID: tc.ID,
                        Name:       tc.Function.Name,
                    })
                    continue
                }

                // [REVIEW #14] Budget check happens BEFORE execution, not
                // after — a round that gets rejected here for being over
                // budget must not also spend a round of the OTHER counter or
                // silently execute anyway. Which counter applies is decided
                // by is_exploration alone at this point; whether a
                // *successful* exploration query still charges the
                // exploration counter (as opposed to being an error retry)
                // is resolved after execution, below.
                if args.IsExploration {
                    if explorationRoundsUsed >= maxExplorationRounds {
                        kind = roundKindExploration
                        messages = append(messages, ChatMessage{
                            Role:       "tool",
                            Content:    "Exploration budget exhausted. You must now produce a final query_database call (is_exploration: false) or call respond_to_user with what you've learned so far.",
                            ToolCallID: tc.ID,
                            Name:       tc.Function.Name,
                        })
                        continue
                    }
                }

                // [REVIEW #6] Exploration queries must still pass the same
                // complexity gate they do today. `is_exploration` is a
                // self-reported flag from the model — it is NOT sufficient by
                // itself to relax safety. executeSQLWithMode's read-only
                // enforcement is unconditional either way (§0 Data Source
                // Read-Only Invariant), but the complexity gating
                // (join/subquery/union limits per strict/moderate/relaxed)
                // must be explicitly re-derived here, exactly as
                // ProcessUserMessage does today before calling
                // executeSQLWithMode(..., true).
                if args.IsExploration {
                    if verr := validateExplorationQuery(args.SQL, safetyMode); verr != nil {
                        kind = roundKindExploration
                        explorationRoundsUsed++ // a rejected-for-safety attempt still counts against the budget, same as today's behavior — it is not a free retry
                        messages = append(messages, ChatMessage{
                            Role:       "tool",
                            Content:    fmt.Sprintf("Exploration query rejected: %s. Please revise it to comply with safety constraints.", verr.Error()),
                            ToolCallID: tc.ID,
                            Name:       tc.Function.Name,
                        })
                        continue
                    }
                }

                result, execErr := executeSQLWithMode(dbConnection, args.SQL, args.IsExploration)

                if execErr != nil {
                    // [REVIEW #5] This branch replaces today's SQL-error-retry
                    // loop (executeFinalQueryWithRetry's error path). See
                    // §2.7 for the full design.
                    //
                    // [REVIEW #14] An execution failure on an is_exploration:
                    // true call charges the EXPLORATION budget (it was an
                    // exploration attempt that happened to fail, not an
                    // error-correction round for a final query). An execution
                    // failure on an is_exploration: false call charges the
                    // ERROR-RETRY budget instead — this is the §2.7-mandated
                    // separation: exploration retries must never silently
                    // consume the error-retry budget, or vice versa.
                    if args.IsExploration {
                        kind = roundKindExploration
                        explorationRoundsUsed++
                    } else {
                        kind = roundKindErrorRetry
                        if errorRetriesUsed >= maxErrorRetries {
                            // [REVIEW #5] Budget exhausted — do not keep
                            // retrying indefinitely. Surface the failure via
                            // renderSQLError (or its tool-calling equivalent),
                            // exactly as executeFinalQueryWithRetry does
                            // today when maxFinalRetries is exhausted. Do NOT
                            // feed this back to the model for "one more try"
                            // — that is precisely the silent-budget-violation
                            // this counter split exists to prevent.
                            return renderSQLError(query, LLMResponse{SQLQuery: args.SQL}, dbConnection, conversation.ID, nil, execErr)
                        }
                        errorRetriesUsed++
                        // [REVIEW #5] isRetryableError/looksLikeSQLError must
                        // still run here, exactly as executeFinalQueryWithRetry
                        // does today: a non-retryable error (e.g.
                        // permission-denied, in the permanentlyFatal list in
                        // sql_execution.go) must not consume a retry attempt
                        // waiting for a correction the model cannot
                        // productively make. Call renderSQLError immediately
                        // instead of looping.
                        if !isRetryableError(execErr) {
                            return renderSQLError(query, LLMResponse{SQLQuery: args.SQL}, dbConnection, conversation.ID, nil, execErr)
                        }
                    }
                    messages = append(messages, ChatMessage{
                        Role:       "tool",
                        Content:    fmt.Sprintf("Query failed: %v", execErr),
                        ToolCallID: tc.ID,
                        Name:       tc.Function.Name,
                    })
                    continue
                }

                // [REVIEW #7] formatToolResult must cap the amount of data
                // sent back into context. Reuse the same truncation ceiling
                // as exploration results today (ExplorationResult.ToMessageContent
                // already bounds row count/width) — do NOT dump the full
                // 1000-row default limit as a markdown table into every
                // subsequent LLM call. See §2.3.2 for the exact policy.
                toolContent := formatToolResult(result)

                messages = append(messages, ChatMessage{
                    Role:       "tool",
                    Content:    toolContent,
                    ToolCallID: tc.ID,
                    Name:       tc.Function.Name,
                })

                if !args.IsExploration {
                    // [REVIEW #14] A SUCCESSFUL final query round is metered
                    // against neither budget — it is the answer, not a retry
                    // of anything. roundKindFinal exists purely for the
                    // tech-details label; it never increments a counter.
                    kind = roundKindFinal
                    // [REVIEW B] Do NOT render immediately. Defer: stash the
                    // result as pendingFinal and let the loop run one more
                    // round so the model can look at `toolContent` (the same
                    // preview text just appended to `messages`) and
                    // optionally call render_chart before anything is shown
                    // to the user. See §2.3.4 for the full rationale, the
                    // round-budget cost, and the mandatory degradation path
                    // if the model never follows up.
                    pendingFinal = &pendingFinalResult{
                        sql:    args.SQL,
                        result: result,
                    }
                    if !conversation.VizEnabled {
                        // Charting isn't offered to this model at all
                        // (render_chart was never in `tools`) — there is
                        // nothing to wait for. Render immediately, same as
                        // today's behavior for chart-disabled conversations.
                        return pendingFinal.render(query, dbConnection, conversation, "")
                    }
                    // Charting is enabled — loop continues so the model can
                    // decide. Every other branch in this loop already knows
                    // to flush a non-nil pendingFinal before doing anything
                    // else, so this is safe even if the model never calls
                    // render_chart. The upcoming render_chart round (if the
                    // model takes it) is ALSO uncounted — see roundKindChart
                    // below.
                    continue
                }
                // [REVIEW #14] Successful exploration query. This is the
                // "normal" exploration round the budget exists to bound.
                kind = roundKindExploration
                explorationRoundsUsed++
                // Exploration query — loop continues, LLM sees results next round.

            case "render_chart":
                // [REVIEW #14] render_chart rounds are metered against
                // NEITHER budget — same treatment as summarization's
                // second LLM call today, which also isn't charged against
                // any retry/exploration counter. This tool can only ever be
                // reached once per turn (pendingFinal is always nil'd or the
                // function returns immediately after), so there is no
                // unbounded-loop risk from leaving it uncounted.
                kind = roundKindChart
                var args struct {
                    ChartConfig string `json:"chart_config"`
                }
                if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || pendingFinal == nil {
                    // [REVIEW B] Either truncated/invalid arguments, or the
                    // model called render_chart with no pending final result
                    // (e.g. after an exploration-only round, or a second time
                    // for the same result). Both are model errors, not
                    // application errors — tell it so and let it recover.
                    reason := "no pending query result to attach a chart to"
                    if err != nil {
                        reason = fmt.Sprintf("error parsing arguments: %v", err)
                    }
                    messages = append(messages, ChatMessage{
                        Role:       "tool",
                        Content:    fmt.Sprintf("render_chart failed: %s.", reason),
                        ToolCallID: tc.ID,
                        Name:       tc.Function.Name,
                    })
                    continue
                }
                // Render now, with the chart attached, and exit the loop —
                // this IS the terminal state that the original single-tool
                // design reached immediately. render_chart is always the
                // last call of a turn; nothing legitimately follows it.
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
                    // [REVIEW #10] Same truncation concern as query_database.
                    // Do not hard-fail the whole conversation turn — retry.
                    messages = append(messages, ChatMessage{
                        Role:       "tool",
                        Content:    fmt.Sprintf("Error parsing arguments: %v. Please retry with valid, complete JSON arguments.", err),
                        ToolCallID: tc.ID,
                        Name:       tc.Function.Name,
                    })
                    continue
                }
                logRound(kind)
                // [REVIEW B] Same flush-before-proceeding rule as every other
                // exit path: a model that got a final result, was offered
                // the chance to chart it, and instead called respond_to_user
                // has implicitly declined. Render chartless, then respond.
                if pendingFinal != nil {
                    if rerr := pendingFinal.render(query, dbConnection, conversation, ""); rerr != nil {
                        return rerr
                    }
                }
                return handleRespond(ctx, query, args.Text, conversation.ID)

            default:
                kind = roundKindOther
                messages = append(messages, ChatMessage{
                    Role:       "tool",
                    Content:    fmt.Sprintf("Unknown tool: %s", tc.Function.Name),
                    ToolCallID: tc.ID,
                    Name:       tc.Function.Name,
                })
            }
        }

        // [REVIEW #14] Reached only by branches that `continue`d rather than
        // `return`ed — every `return` path above already called `logRound`
        // itself, at the point `kind` was known, before returning. This
        // final call covers every `continue` path (budget rejections, parse
        // errors, successful exploration/error-retry rounds).
        logRound(kind)
    }

    // Max rounds exhausted.
    // [REVIEW B] If a final result was pending a chart decision when the
    // round budget ran out, render it chartless rather than discarding a
    // result the user is entitled to see — same Graceful Degradation
    // rationale as every other flush point above.
    if pendingFinal != nil {
        return pendingFinal.render(query, dbConnection, conversation, "")
    }
    // [REVIEW #14] totalRoundCap (not either meaningful budget) is what
    // actually terminated the loop here. In practice this should be
    // unreachable via normal exhaustion of maxExplorationRounds or
    // maxErrorRetries, since both budgets return an explicit, more specific
    // clarification/error at the point they're exhausted (see the
    // explorationRoundsUsed and errorRetriesUsed checks above). Reaching
    // this line means the model spent an unexpectedly large number of
    // uncounted rounds (repeated malformed calls, repeated unknown tool
    // names, etc.) — worth its own distinct message so this doesn't get
    // confused with a normal budget exhaustion in support/debugging.
    return handleClarification(query, LLMResponse{
        Action:                "clarification",
        ClarificationQuestion: "I wasn't able to complete this request due to repeated invalid responses. Could you try rephrasing your question?",
    }, conversation.ID)
}
```

#### 2.3.1 [REVIEW — Blocking #1, REVISED] Conversation history persistence: canonical transcript + translate-at-boundary

**This is the highest-risk gap in the original draft.** `buildLlmMessages`
(`discussion_engine.go` line 836) reconstructs LLM context on *every* turn by
reading `ConversationMessage` rows from SQLite and flattening them into plain
`ChatMessage{Role, Content}` pairs — it collapses `"exploration"` rows into
`"system"` role messages (line 857) and injects either `LLMContent` (raw JSON)
or HTML-stripped text (lines 850-853) as ordinary content strings. An earlier
draft's §2.5 ("What Stays the Same") asserted this function is "unaffected"
by the tool-calling change. **That assertion is false.** Every major provider
requires an exact, structurally valid sequence — an `assistant` message with
`tool_calls` must be immediately followed by one `tool` role message per
call, each carrying a matching `tool_call_id` — or the API returns a 400, or
(worse, for lenient providers) silently drops the structure and the model
loses its own prior tool-call context. `runAgenticLoop` (§2.3) builds this
sequence correctly *within a single `ProcessUserMessage` invocation*. The
moment the conversation is reloaded for the **next** user turn, without a
deliberate design, `buildLlmMessages` would flatten everything back to plain
strings and the structure would be gone.

**This subsection supersedes an earlier revision that chose to collapse
completed turns into a plain-text summary** (reusing `llm_content` to store
something like *"Called query_database(sql=...), got 12 rows, then
responded: <answer text>"*, with zero changes to `buildLlmMessages`). That
approach works and is the lowest-effort option, but degrades information on
every turn: a follow-up question referencing something specific about a
prior query ("filter that same query to just the West region") forces the
model to reason from a paraphrase of its own past SQL rather than the exact
statement. On reflection, given the explicit goal of not conforming to
legacy shape for its own sake, that trade is not the best available design
— there is a better option once one specific fact about these APIs is taken
into account.

**The fact that makes full fidelity practical: tool-call IDs are per-request
correlation tokens, not durable identity.** OpenAI/Anthropic/OpenRouter chat
completions APIs are stateless per request — the entire conversation is
resent from scratch on every call, always. The requirement that an
`assistant` message's `tool_calls[].id` match the `tool_call_id` on the
following `tool` message(s) is validated *only within the single payload
being sent* — no provider checks that ID against anything from a previous,
separate HTTP request. This means IDs can be freshly minted every time
history is replayed, with zero compatibility requirement to match whatever
ID a prior physical API call originally used. This removes the specific
concern ("potentially mixing multiple providers' ID formats if the user
switches LLM providers mid-conversation") that motivated rejecting full
fidelity in the first place — the stored data was never going to be
provider-shaped IDs anyway, and doesn't need to be.

**Chosen design: persist a provider-neutral canonical transcript; translate
to whichever provider's wire format is needed, fresh, at request-build
time.**

A new nullable column on `conversation_messages` stores a small, versioned,
provider-agnostic JSON record of everything that happened during an
assistant turn — every `query_database` / `render_chart` call and its
result, and the final `respond_to_user` text:

```go
// ensureColumn per AGENT_READ_FIRST.md §3.2 — additive, nullable, no data
// loss risk for existing rows (old JSON-response-protocol turns simply
// never populate this column).
ensureColumn("conversation_messages", "tool_transcript", "TEXT")
```

```go
// ToolTranscript is the canonical, provider-neutral record of one
// assistant turn's tool activity. Stored as JSON in
// ConversationMessage.ToolTranscript. Never contains provider-specific
// IDs or wire-format shape — those are synthesized fresh by
// buildToolMessages (below) every time history is replayed.
type ToolTranscript struct {
    Version int                `json:"version"` // schema version, see below
    Actions []TranscriptAction `json:"actions"`
}

type TranscriptAction struct {
    Tool          string `json:"tool"`           // "query_database", "render_chart", "respond_to_user"
    Arguments     string `json:"arguments"`      // the exact JSON arguments the model produced
    ResultPreview string `json:"result_preview,omitempty"` // formatToolResult's output, §2.3.2 — same truncation policy applies here as in-flight
}
```

`runAgenticLoop` (§2.3) accumulates a `[]TranscriptAction` as it runs — this
is the same accumulation already required for the exploration-trace
transparency feature (§2.3.3), so it is not additional bookkeeping, it is the
same bookkeeping serialized to a stable, storable shape instead of being
discarded at the end of the turn. `handleRespond` and `renderToolQueryResults`
both serialize the accumulated actions (plus the final `respond_to_user` /
`query_database` call itself) into `ToolTranscript` and pass it to
`CreateConversationMessage` as the new `tool_transcript` argument.

**Translation at read time.** `buildLlmMessages` (or a new sibling function
called from it) gains one new step: for any history row with a non-empty
`ToolTranscript`, call a translator instead of falling through to today's
plain-content path for that row:

```go
// buildToolMessages converts a canonical ToolTranscript into the message
// sequence the CURRENTLY CONFIGURED provider needs. IDs are synthesized
// fresh on every call — see rationale above for why this is always safe,
// including across a provider switch mid-conversation.
func buildToolMessages(t ToolTranscript) []ChatMessage {
    var out []ChatMessage
    var calls []ToolCall
    for i, action := range t.Actions {
        if action.Tool == "respond_to_user" {
            continue // handled as the assistant's final Content, not a tool_call
        }
        id := fmt.Sprintf("hist_%d", i) // uniqueness within this one request is the only requirement
        calls = append(calls, ToolCall{
            ID: id,
            Function: ToolCallFunction{Name: action.Tool, Arguments: action.Arguments},
        })
    }
    assistantMsg := ChatMessage{Role: "assistant", ToolCalls: calls}
    if final := lastRespondToUser(t.Actions); final != "" {
        assistantMsg.Content = final // if the turn ended in prose, it rides on this same assistant message
    }
    out = append(out, assistantMsg)
    for i, action := range t.Actions {
        if action.Tool == "respond_to_user" {
            continue
        }
        out = append(out, ChatMessage{
            Role:       "tool",
            Content:    action.ResultPreview,
            ToolCallID: fmt.Sprintf("hist_%d", i),
            Name:       action.Tool,
        })
    }
    return out
}
```

This function is provider-agnostic by construction — it produces the
generic `ChatMessage`/`ToolCall` shapes already defined in §2.2.1, and each
provider's client (`llm_openai.go`, `llm_anthropic.go`, etc.) is responsible
for translating *those* into its own wire format exactly as it already must
do for the live turn's tool calls. No new per-provider history logic is
required beyond what each provider's `ChatCompletionWithTools` implementation
already needs for in-flight tool calls (§2.2.1) — replayed history and live
tool calls share the same generic-to-wire-format translation path inside
each provider client.

**Versioning discipline.** `ToolTranscript.Version` exists because this
shape will need to change eventually (a new tool is added, an argument shape
changes). `buildToolMessages` must switch on `Version` and either handle old
versions directly or degrade a transcript it doesn't recognize to a plain
text summary (the §2.6-superseded approach becomes the *fallback* for
unrecognized versions, not the primary design) rather than erroring the
whole conversation load. This is the one piece of ongoing maintenance cost
this design accepts in exchange for full fidelity — stated explicitly so it
isn't discovered as a surprise later.

**Comparison, for the record:**

| | Prose collapse (superseded) | Canonical + translate-at-boundary (chosen) |
|---|---|---|
| Model sees its own exact prior SQL/tool calls on a later turn | No — paraphrased | Yes |
| Works across a mid-conversation provider switch | Yes (trivially, it's just text) | Yes (translator targets whichever provider is currently configured) |
| Schema footprint | None | One new nullable `TEXT` column |
| New code required | None (`buildLlmMessages` untouched) | `buildToolMessages` translator + version handling, one function per provider client already required by Phase 1 (§2.2.1) |
| Follow-up questions referencing exact prior query details | Degraded | Full fidelity |

**Action required:** Phase 2 (§5) must implement `ToolTranscript`, the
`tool_transcript` column, `buildToolMessages`, and the accumulation logic in
`runAgenticLoop` — not as an afterthought, but as core Phase 2 scope, since
the exploration-trace accumulation this reuses (§2.3.3) is already mandatory
for that phase. The acceptance criterion below is unchanged in spirit from
the earlier draft, but now tests full-fidelity replay rather than confirming
a paraphrase round-trips: verify that a multi-turn conversation's second turn
correctly reconstructs the first turn's exact tool calls via
`buildToolMessages`, not just that the API accepts the resulting payload.

#### 2.3.2 [REVIEW — Design Gap #7] `formatToolResult` truncation policy

`formatToolResult` (referenced in §2.3, not yet defined) must not be a bare
alias for `formatResults` (the existing full-markdown-table formatter used
for final answer rendering). Every round-trip through the agentic loop
re-sends the *entire* accumulated `messages` slice, including every prior
tool result, on every subsequent call — this is a per-round cost multiplier
that the current exploration loop does not have in the same shape (today,
only `ExplorationResult.ToMessageContent()`, which is deliberately compact,
is replayed; final query results are rendered once and never re-sent to the
LLM). Policy: `formatToolResult` caps rows at a small constant (e.g. 20, well
under the `explorationDefaultLimit` of 100 used for exploration's DB-side
limit) and truncates any single cell value over ~200 characters with an
ellipsis, independent of the row/column limits already applied by
`executeSQLWithMode`. This must be a hard cap enforced in `formatToolResult`
itself, not a hope that `LIMIT` clauses in generated SQL will keep results
small — exploration queries in particular are LLM-generated and not
guaranteed to include a tight `LIMIT`.

#### 2.3.3 Three-Way Routing After Exploration, and Why This Is Not Summarization

The agentic loop (§2.3) is not just "explore, then run one final query." After
any number of `is_exploration: true` rounds, the model has exactly three ways
to conclude the turn, and all three are already implied by the two-tool
design — this subsection makes the third one explicit, since it is easy to
under-specify in the pseudocode as an afterthought rather than a designed
outcome:

1. **Call `query_database` again with `is_exploration: false`.** Final query,
   results table shown to the user (`renderToolQueryResults`).
2. **Call `respond_to_user` with prose that includes or references a table
   the model already saw.** E.g. the model explored, got a wide result set,
   and chooses to summarize the *shape* of it in prose rather than dump it
   raw — this is a model judgment call made per-turn, not a setting.
3. **Call `respond_to_user` with a short direct answer synthesized from
   exploration alone, with no results table ever shown.** E.g. "Do we have
   any customers in Japan?" → model explores with `SELECT COUNT(*)...`, sees
   `3`, and answers "Yes, you have 3 customers in Japan" — never calling
   `query_database` with `is_exploration: false` at all.

Outcome 3 is not a new capability this proposal introduces — it is the direct
equivalent of today's existing `sql_exploration` → `answer` action sequence
(`ExplorationResult.Explained` already captures the model's reasoning for
doing this under the current protocol). It is called out here because a
reader could reasonably assume `query_database` → table is the only possible
outcome of a database question, and design `runAgenticLoop` around that
assumption. It is not: `respond_to_user` is a fully valid final action
regardless of how many `query_database` calls preceded it, exploration or
otherwise.

**This is a distinct mechanism from summarization, despite superficially
similar output (prose instead of a raw table).** `conversation.Summarize` is
a *conversation-level setting*, evaluated *after* a final `sql_query`/
`query_database` call has already produced a results table — it triggers a
*second, separate* LLM call (`summarizeResults`) that writes prose to sit
*above* the table, which remains present (collapsed behind "View raw
results"). Outcome 3 above is a *per-turn model decision*, made *instead of*
producing a table at all, requiring no setting and no second LLM call — the
same turn that explored is the turn that answers. Summarization is orthogonal
to this proposal and needs no changes (§2.5 already covers this correctly:
result rendering consumes the same `QueryResult` struct regardless of
protocol). Outcome 3 is part of the core agentic loop design and belongs in
§2.3's pseudocode as a first-class, expected path — not a fallback.

**Open gap this creates — exploration transparency.** Today, exploration
rounds are hidden by default but not invisible: they persist as
`ConversationMessage{Role: "exploration"}` rows and render as a collapsible
"Show N intermediate query(ies)" block (`formatExplorationHTML`), satisfying
`AGENT_READ_FIRST.md` §3.8's "prefer transparency over magic." Under §2.3.1's
chosen history-persistence approach, `tool` role messages for exploration
rounds live only in the ephemeral `messages` slice for the duration of one
`ProcessUserMessage` call — nothing in the current pseudocode collects them
into a persisted, user-facing trace. If a user gets "Yes, 3 customers in
Japan" via outcome 3 above and later wants to audit *what query produced
that*, there is currently no equivalent of today's expandable trace. **This
must be closed before Phase 2 ships**, not deferred: `runAgenticLoop` needs to
accumulate every `query_database` tool call + result pair from the current
turn into a structure equivalent to today's `[]ExplorationResult`, and
`handleRespond` (for outcome 2 and 3) and `renderToolQueryResults` (for
outcome 1) both need to render that trace via `formatExplorationHTML` or its
tool-calling equivalent, exactly as `renderSQLResults` already does via the
`explorationResults` parameter today. This is an addition to Phase 2's
acceptance criteria (§5) alongside the multi-turn history check already
specified there.

#### 2.3.4 [REVIEW B] Deferred Final-Result Rendering for Data-Informed Charts

This subsection documents a deliberate design decision made after initial
review: `chart_config` was originally a same-call parameter on
`query_database` (blind to actual results, informationally no better than
today's `resp.VizConfig`). It is now the separate `render_chart` tool
described in §2.1 / §2.1.1, callable only after the model has seen the real
result set — so it can choose a chart type based on actual value ranges,
category counts, and row shape, not guesses from column names. This is a
real capability improvement, but it requires breaking the invariant every
other part of this document assumed: **a final (`is_exploration: false`)
`query_database` call is no longer automatically terminal.**

**The `pendingFinalResult` mechanism, precisely:**

```go
// pendingFinalResult holds a final query's results after execution but
// before rendering, while the model is given one extra round to optionally
// call render_chart. It is intentionally NOT persisted anywhere — it lives
// only in runAgenticLoop's local scope for the remainder of the current
// ProcessUserMessage call.
type pendingFinalResult struct {
    sql    string
    result *QueryResult
}

// render performs the actual, one-time rendering of a final result — with
// or without a chart. This is the single choke point every exit path in
// runAgenticLoop calls through, so "render a pending result" always means
// exactly one thing, done exactly once. chartConfig is "" for every
// chartless/degraded path.
func (p *pendingFinalResult) render(query *models.Query, dbConnection *models.DataSource, conversation *models.Conversation, chartConfig string) error {
    return renderToolQueryResults(query, p.sql, p.result, chartConfig, dbConnection, conversation)
}
```

**Cost: one additional LLM round-trip on every final query, whenever
charting is enabled** — not just when a chart is actually produced. Even if
the model ultimately declines to chart, it must be given the chance to
decide, which means one full extra `ChatCompletionWithTools` call (latency +
tokens) is spent on every chartable answer. This is the direct trade-off
accepted by choosing Variant B over the same-call-parameter design: `AGENT_READ_FIRST.md`
§0's secondary goal of feeling "fast" is measurably in tension with this
change, and it should be scoped **only** to conversations with
`conversation.VizEnabled == true` (already enforced above — `render_chart` is
omitted from `tools` entirely when charting is off, so chart-disabled
conversations pay zero extra latency, identical to today).

**Mandatory degradation guarantee.** Per the Graceful Degradation Principle
(`AGENT_READ_FIRST.md` §0: "secondary features... must fail independently
without ever blocking or hiding the primary result"), a `pendingFinal` that
is never resolved by an explicit `render_chart` call **must never result in
the user not seeing their answer.** The revised §2.3 pseudocode enforces this
at every single exit point of the loop — plain-text response, empty/truncated
response, another `query_database` call, a `respond_to_user` call, and the
max-rounds exhaustion fallback all check for a non-nil `pendingFinal` and
flush it (chartless) before doing anything else. There is no path through the
revised loop where a completed final query's results can be silently
discarded because the model moved on without calling `render_chart` — this
is the acceptance criterion for this subsection and must be explicitly
tested (call a final query with charting enabled, have the test/mock model
respond with plain text instead of calling `render_chart`, assert the results
table still renders).

**Why this does not need `UpdateConversationMessage`.** `pendingFinal` is
deliberately scoped to a single local variable inside `runAgenticLoop` — by
the time a turn completes, it has always been resolved (rendered, chart or
no chart) one way or another, so there is never a "pending" state that
survives past the end of a single `ProcessUserMessage` call, and never a
need to patch an already-created row. This holds regardless of which
history-persistence design §2.3.1 uses: by the time `CreateConversationMessage`
is called for the turn's final row, `render_chart` has either already run
(chart included in both the rendered HTML and the `ToolTranscript`
accumulated for §2.3.1) or been explicitly declined (chartless in both).
Worth stating explicitly: no `UpdateConversationMessage`-style function
exists in the codebase today — `pkg/services/conversation.go` only has
`CreateConversationMessage`, not update-in-place — and this design
deliberately avoids needing one, unlike a design that tried to render a
placeholder message first and patch a chart into it after the fact.

#### 2.3.5 [REVIEW #14] `totalRoundCap`: why it exists and what its headroom covers

§2.3's loop is no longer bounded by a single meaningful `maxRounds` — it is
bounded by `totalRoundCap := maxExplorationRounds + maxErrorRetries + 4`.
This subsection makes explicit what that constant `+4` covers and why it is
sized the way it is, rather than leaving it as an unexplained magic number in
the pseudocode.

**What consumes an uncounted round.** Per §2.7 and §2.3's `roundKind`
logic, exactly four kinds of round are deliberately metered against neither
budget:

1. The successful **final** `query_database` call itself (`roundKindFinal`)
2. The **`render_chart`** follow-up, if the model takes it (`roundKindChart`)
3. The **`respond_to_user`** call that ends the turn (`roundKindOther`)
4. **One** malformed-call or unknown-tool-name round, as a tolerance for a
   single transient parse failure (e.g. §2.2.1's stub providers, or a model
   that briefly hallucinates a tool name before self-correcting) without
   immediately exhausting the safety ceiling

A well-behaved turn spends at most one round on each of (1)–(3), and zero on
(4). `+4` headroom is exactly enough for that well-behaved case. It is
deliberately **not** enough headroom to absorb a model that repeatedly sends
malformed or unknown tool calls — that failure mode is supposed to exhaust
`totalRoundCap` and fall through to the "repeated invalid responses"
clarification at the bottom of §2.3, by design (see the comment at that
return site). If a future revision needs a larger tolerance for repeated
malformed calls specifically, that should be its own named, configurable
constant — not a larger, unexplained value folded into the same `+4`.

**Why this is not itself a third meaningful budget.** `maxExplorationRounds`
and `maxErrorRetries` are user/config-facing limits (`models.DataSource`
settings) that a person might reasonably want to tune. `totalRoundCap`'s `+4`
is not — it is an internal implementation constant sized to the number of
deliberately-uncounted round kinds enumerated above, and should move only if
a sixth kind of uncounted round is added to the loop, not as a general
"give it more headroom" dial. This distinction matters for Phase 2's
implementation: `totalRoundCap` should not be exposed as a conversation or
data-source setting alongside the two real budgets.

### 2.4 Message Flow

**Current flow (4 actions, JSON parsing):**

```
User → buildLlmMessages → LLM API → extractJSONFromResponse → fixJSONFormatting →
parseLLMResponse → switch(action) → executeSQL/handleAnswer/handleClarification
```

**Proposed flow (2 core tools + 1 conditional tool, no parsing):**

`query_database` and `respond_to_user` are always offered. `render_chart`
(§2.1, §2.3.4) is offered only when `conversation.VizEnabled` is true, and is
callable only as an optional follow-up to a final `query_database` call —
it never appears as the first tool call of a turn.

```
User → buildLlmMessages (with tools) → LLM API → switch(tool_call.name) →
execute_query / respond_to_user → append tool result → loop or return
```

### 2.5 What Stays the Same

The following components are **not affected** by this change:

- **SQL execution safety** — `executeSQLWithMode` still enforces read-only
  queries, row limits, and exploration safety modes (§1.4)
- **Exploration complexity gating** — `validateExplorationQuery` and
  `ExplorationSafetyMode` (strict/moderate/relaxed) still run before every
  exploration-flagged `query_database` call, exactly as before (§2.3 code,
  §2.5.1 below). **[REVIEW #6]** This was missing from the original draft's
  pseudocode and is called out explicitly here because "the tool call has an
  `is_exploration` flag" is not the same guarantee as "the application
  independently re-validates exploration complexity" — see §2.5.1.
- **Schema introspection** — `db_introspection.go` still gathers table/column
  metadata for prompt construction (§1.5)
- **Prompt construction** — `buildLlmMessages` still injects schema, skills,
  business rules, and conversation history for every row that predates or
  opts out of tool calling. **This is a partial exception, not a full
  "unchanged":** rows with a populated `ToolTranscript` are routed through
  the new `buildToolMessages` translator instead (§2.3.1, revised) — schema,
  skills, and business rule injection are untouched either way, since those
  are prepended once as the system prompt, not per-row.
- **Result rendering** — `formatResultsHTML`, chart attachment (via the
  deferred `render_chart` flow, §2.1.1/§2.3.4), summaries all consume the
  same `QueryResult` struct
- **Frontend** — `ConversationView.svelte` renders HTML message content the
  same way (`{@html message.content}`); the content type metadata
  (`"content_type": "html"`) is unchanged
- **Data source read-only invariant** — the absolute rule from §0 is
  enforced by `executeSQLWithMode`, which is called by both the current
  `executeFinalQueryWithRetry` and the proposed `query_database` handler

#### 2.5.1 [REVIEW — Design Gap #6] Exploration complexity gating is not optional

Today, `sql_exploration` is a distinct **action type**, and every exploration
query passes through `validateExplorationQuery(sql, safetyMode)` *before*
execution — an independent, application-controlled check beyond "read-only
only," gating join/subquery/UNION/GROUP BY complexity per
strict/moderate/relaxed mode. Collapsing `sql_query` and `sql_exploration`
into a single `query_database` tool with a self-reported `is_exploration`
boolean (§2.1) must not be allowed to weaken this: `is_exploration` is data
the model provides, not a decision the application independently verifies
unless it explicitly calls `validateExplorationQuery` when the flag is true —
which the revised pseudocode in §2.3 now does. This is listed here as well,
not just embedded in the pseudocode comment, because it is a regression
against `AGENT_READ_FIRST.md` §1.4 if it is ever refactored away without
noticing it was doing double duty as a safety gate, not just a routing hint.

### 2.6 What Is Removed

The following functions become dead code and can be cleaned up:

| Function | Reason |
|---|---|
| `extractJSONFromResponse` | No raw JSON to extract |
| `fixJSONFormatting` | No double-encoded JSON to repair |
| `parseLLMResponse` | No response JSON to parse (tool calls come from API client) |
| `parseLenientJSON` | No lenient fallback needed |
| `extractFieldValue` | No field extraction needed |
| `unescapeNewlines` | No double-encoded newlines |
| `unescapeFields` | No field-level unescaping needed |
| `looksLikeSQL` | No raw-SQL fallback parsing needed |

**[REVIEW — Blocking #5] `LLMResponse` is not safely removable as a single
unit.** The original draft stated flatly that "the `LLMResponse` struct...
is removed," as if it were an isolated type used only by the four action
handlers this document discusses. It is not: `LLMResponse` (and its
`Explanation` field specifically) is also referenced by
`classifyErrorCategory`, `buildErrorMetadata`, `renderSQLError` (line 1567),
the SQL-retry-on-error loop inside `executeFinalQueryWithRetry` (line 946),
and `storePayload`'s metadata — none of which are about JSON-response
parsing at all; they're about **retrying a failed SQL query against the
same LLM**, a code path this document's original draft never mentioned. That
path does not disappear under tool calling — see the new §2.7, which gives it
an explicit design instead of leaving it as an unstated assumption. Only
after §2.7's design is implemented and verified working can the retry-loop's
remaining `LLMResponse` usages (error classification helpers, metadata
builders) be safely trimmed down to whatever subset survives; `LLMResponse`
itself is retained (not removed) for as long as `renderSQLError` and
`classifyErrorCategory` exist in their current form, since both are
independent of the JSON-parsing bug class and out of scope for this proposal.

The `_parse_error` action handling is removed from all switch statements —
this part of the original claim holds, since `_parse_error` exists
specifically to represent a JSON-parse failure, which cannot occur once
responses arrive as structured tool calls.

### 2.7 [REVIEW — Blocking #5] SQL Error-Retry Loop Under Tool Calling

The original draft's agentic loop (§2.3) only described the **exploration**
loop (`sql_exploration` → `sql_query`). It never addressed the **separate**
retry-on-execution-error loop that exists today in
`executeFinalQueryWithRetry`: when a final query fails to execute (syntax
error, missing column, etc.), the current code classifies the error
(`isRetryableError`, `looksLikeSQLError` in `sql_execution.go`), applies
backoff for transient errors (`backoffDuration`), and re-prompts the LLM with
the failed SQL + error text, expecting a corrected `sql_query` action back —
up to a bounded retry count (`maxFinalRetries`). This is a frequently-hit,
first-class path (most non-trivial LLM-generated SQL fails at least once on
complex schemas) and cannot be left undesigned.

**This maps directly onto the revised §2.3 pseudocode, which now implements
the design below rather than just describing it (§2.3's `roundKind`/
`explorationRoundsUsed`/`errorRetriesUsed` machinery is the concrete
realization of every bullet in this subsection).** A query execution failure
is just another `tool` role reply with an error message — the model sees the
failure in its next turn and is expected to call `query_database` again with
a corrected `sql` argument, exactly as it would respond to any other tool
result. The retry *budget* and *classification* logic are carried over as
follows:

- **Retry budget — resolved as two independent counters, not a single flat
  `maxRounds`.** `runAgenticLoop` (§2.3) takes `maxExplorationRounds` and
  `maxErrorRetries` as separate parameters, and tracks
  `explorationRoundsUsed`/`errorRetriesUsed` separately as it runs. A
  `query_database` call with `is_exploration: true` — whether it succeeds,
  fails validation, or fails execution — always charges
  `explorationRoundsUsed`, never `errorRetriesUsed`. A `query_database` call
  with `is_exploration: false` that fails execution charges
  `errorRetriesUsed`. Neither counter conflates with the other, closing the
  exact risk this subsection originally flagged ("exploration retries don't
  silently consume the error-retry budget or vice versa"). This preserves
  the two independently-tuned limits already present in `models.DataSource`
  config (`MaxExplorationRounds` today) without merging them into one
  setting.
- **`render_chart` and a successful final `query_database` call are metered
  against neither counter** — by explicit decision, treated the same as
  today's summarization second-call, which also isn't charged against any
  retry/exploration budget (§2.3's `roundKindChart`/`roundKindFinal`, never
  incremented). This is intentional, not an oversight: a model that
  correctly explores, runs one final query, and optionally charts it has not
  "used a retry" in any sense the budgets exist to bound — it has succeeded.
  The loop remains bounded overall via `totalRoundCap` (§2.3), an
  unconditional safety ceiling distinct from either meaningful budget,
  guarding against a model that spins on malformed calls or unknown tool
  names indefinitely.
- **Error classification is still valuable and still runs:** `isRetryableError`
  / `looksLikeSQLError` currently decide whether to retry at all versus
  surface a hard failure to the user (e.g., permission-denied errors are
  never retried — see `permanentlyFatal` list in `sql_execution.go`). This
  logic runs **before** appending the `tool` role error message: if the
  error is classified as non-retryable, the loop does not continue — it
  calls `renderSQLError` directly instead of feeding the failure back to the
  model for a retry it cannot productively act on. §2.3's `execErr != nil`
  branch now shows this explicitly (`if !isRetryableError(execErr) { return
  renderSQLError(...) }`), alongside the `errorRetriesUsed >= maxErrorRetries`
  budget-exhaustion check that also calls `renderSQLError` rather than
  looping further.
- **Debuggability:** every round is tagged with a `roundKind`
  (`exploration`/`error_retry`/`chart`/`final`/`other`) and logged via
  `storePayload` with a label like `round-3-error_retry`, so a human
  inspecting a conversation's tech-details payloads can distinguish
  "exploration round 2" from "error-retry round 1" directly, rather than
  seeing an undifferentiated round counter. See §2.3's `logRound` helper.
- **Backoff for transient errors:** `backoffDuration` (exponential backoff for
  connection-level errors) has no natural home in a tight tool-call loop —
  inserting a multi-second sleep between tool-call rounds is a UX regression
  (§0 Secondary Goals: "the experience must feel fast") unless the
  `processingPhase` event is updated to say something like "Retrying after
  connection issue…" during the wait, per the existing UX pattern in
  `AGENT_READ_FIRST.md` §3.8 ("Always show progress during multi-step
  operations").

---

## 3. Provider Compatibility

### 3.1 OpenRouter / OpenAI

Fully supported. The Chat Completions API with `tools` parameter is the
standard path. The `openAIChatRequest` struct gains a `Tools` field,
and `openAIChatMessage` gains `ToolCalls` and `ToolCallID` fields.

### 3.2 Anthropic

Fully supported via the Messages API with `tools` parameter. The
`llm_anthropic.go` provider already translates `ChatMessage` into the
Anthropic format — it would additionally translate tool definitions and
tool-call responses.

### 3.3 Ollama

**Partial support.** Ollama supports tool calling as of v0.3+, but
implementation quality varies by model. The `llm_ollama.go` provider would
implement tools for models that support it, with a fallback to the current
JSON-response protocol for models that don't. This is handled transparently
by the `LLMClient` interface — the discussion engine doesn't know which
protocol is in use.

### 3.4 Local / Custom Endpoint

**Varies.** The `llm_local.go` provider wraps any OpenAI-compatible endpoint.
Tools support depends on the backend. The fallback described in §3.3 applies.

### 3.5 Fallback Protocol

For providers/models that don't support native tools, retain a simplified
JSON-response protocol as a **provider-level fallback** (not a
discussion-engine concern). The fallback:

1. Asks the LLM for JSON with only `action` and `sql`/`text` fields (no explanation, no viz_config)
2. Parses the response into the same `ChatMessage.ToolCalls` structure
3. Returns it to the discussion engine, which processes it identically to native tool calls

This keeps the fallback isolated to the LLM client layer. The discussion engine
only sees `ChatMessage.ToolCalls`, regardless of whether they came from native
tool calling or JSON parsing.

#### 3.5.1 [REVIEW — Design Gap #9] The fallback must not reintroduce prose-inside-JSON

As originally scoped ("JSON with only `action` and `sql`/`text` fields"), this
fallback still asks a model to embed prose — the exact same class of risk
(backtick fences, quotes, newlines, and provider-side double-encoding through
OpenRouter-style wrapping) that this entire document exists to eliminate. This
matters specifically because the fallback's target population — small/local
models via Ollama or a custom OpenAI-compatible endpoint — is precisely the
population that produced the live incident motivating this proposal
(gemma-4-e4b-it in `ANSWER_CLARIFICATION_ISSUE.md`'s final entry emitted valid
action JSON followed by unstructured prose *outside* the JSON object entirely).
A fallback that reintroduces the bug class for its primary audience is not an
acceptable fallback.

**Revised fallback design, scoped to minimize prose-inside-JSON risk:**

- The fallback JSON schema carries **only** `action` (`"sql"` or
  `"respond"`) and, when `action == "sql"`, a `sql` field. It never asks for
  a `text`/prose field inside the JSON.
- When `action == "respond"`, the fallback parser does **not** expect prose
  inside the JSON at all. Instead, following the same instruction pattern
  that already works for `extractJSONFromResponse`'s existing "first
balanced JSON object" heuristic (`discussion_engine.go` line 84), the prompt
  instructs the model: *"If you are responding directly rather than
  querying, reply with `{"action":"respond"}` on its own, followed by your
  full answer as plain markdown text — do not put your answer inside the
  JSON."* The fallback parser reads the small JSON prefix to get `action`,
  then treats **everything after** the JSON object's closing brace as the
  literal, unescaped answer text — zero unescaping, zero newline recovery,
  zero fence-matching, because it was never inside a JSON string to begin
  with. This is a deliberate inversion of the gemma failure mode: instead of
  trying to stop the model from putting prose outside the JSON (which failed
  repeatedly against real models), the fallback *requires* prose outside the
  JSON and only asks the model to produce a two-token JSON prefix, which is a
  dramatically easier task for small models than escaping multi-paragraph
  markdown inside a string.
- SQL, being a single flat string, remains the only content ever asked to
  live inside a JSON string value in the fallback protocol — and even then
  only for `action == "sql"`, where the string is a single-line-oriented SQL
  statement, not multi-paragraph markdown with code fences.
- This fallback parser is new code, isolated entirely to the LLM client layer
  (specifically `llm_ollama.go` and `llm_local.go`), and does not reuse or
  resurrect `parseLLMResponse` / `extractJSONFromResponse` / `fixJSONFormatting`
  (all removed per §2.6) — it is a narrower, purpose-built parser for exactly
  the two-field shape above, small enough to reason about and test
  exhaustively, unlike the general-purpose parser it replaces.

---

## 4. Risk Assessment (per AGENT_READ_FIRST.md §4)

### 4.0 Absolute Rules Check

| Rule | Impact |
|---|---|
| Never execute write against data source | **No change.** `executeSQLWithMode` still enforces read-only. The `query_database` tool definition explicitly requires `SELECT`-only queries. |
| Never log API keys or passwords | **No change.** Logging calls are not modified. |
| Never destructively alter columns | **No change.** No schema changes are required. |

#### 4.0.1 [REVIEW] Exploration complexity gating is safety-adjacent, not covered by the table above

The table above only checks the three absolute, non-negotiable rules verbatim
from `AGENT_READ_FIRST.md` §4.0. Worth stating explicitly: `validateExplorationQuery`
/ `ExplorationSafetyMode` (§1.4, reaffirmed in §2.5.1) is **not** one of those
three absolute rules — it is a configurable complexity control, distinct from
the read-only invariant. This means it *is* subject to the normal
risk/reward framework below (unlike the absolute rules), but §2.5.1 documents
why it must not be silently dropped as part of this refactor: doing so would
be a regression in capability-control fidelity, not a violation of an
absolute rule, but a regression nonetheless that should be caught in review
rather than shipped by omission.

### 4.1 Risk Categories

| Category | Assessment |
|---|---|
| **Answer accuracy** | **Improved.** Eliminates four classes of parsing bugs. LLM can focus on producing correct content, not valid JSON-with-prose-inside. |
| **Answer delivery** | **Low risk of regression, high reward.** The agentic loop is a well-established pattern (coding harnesses, Copilot, Claude Code). Tool-call arguments are simple flat JSON — no embedded prose, no escaping challenges. |
| **Data safety** | **No change.** `executeSQLWithMode` is the same entry point. Read-only enforcement is unchanged. |
| **Data integrity** | **No change.** No schema changes. No new data storage paths. |
| **User trust** | **Improved.** Fewer rendering failures → more reliable answers → higher trust. |
| **Regression** | **Medium risk.** The discussion engine's main loop is significantly refactored, and §2.3.1's canonical-transcript design adds a translator (`buildToolMessages`) as new, testable surface area beyond the loop itself. Existing conversations use the old message format and never populate `tool_transcript`, so they are structurally unaffected. Mitigation: implement as a new code path, keep old `ProcessUserMessage` for backward compatibility, migrate with a conversation-level feature flag (§4.1.1), and version `ToolTranscript` from day one (§2.3.1) so the translator has a defined fallback instead of a hard failure if its own schema needs to change later. |
| **UX/Comfort** | **Improved.** Same rendering pipeline, fewer failure modes. |

#### 4.1.1 [REVIEW — Design Gap #8] Feature flag migration must follow §3.2's rules exactly

"Add a feature flag" is not itself a migration spec, and `AGENT_READ_FIRST.md`
§3.2 requires every schema change to go through `ensureColumn()` with an
explicit type and default. The literal migration line, matching the existing
pattern (`ensureColumn("conversations", "viz_enabled", "INTEGER DEFAULT 1")`):

```go
ensureColumn("conversations", "use_tool_calling", "INTEGER DEFAULT 0")
```

**Default must be `0` (false), not `1`.** This is the load-bearing detail: a
default of `1` would silently switch every existing conversation onto the new
agentic loop the moment the migration runs, which contradicts §4.1's own
mitigation ("existing conversations use the old message format" /
"backward compatibility") and §2.3.1's history-persistence design, which has
not yet been exercised against real historical data at the time this
migration ships. New conversations can default the *application-level*
initial value to `true` at creation time (a separate decision from the
column's SQL default, which only matters for rows that predate the column's
existence) once Phase 2 is verified stable — see §5 Phase 5's migration step,
which already describes flipping the default for new conversations, but only
after Phases 2–4 are confirmed working across all four providers.

### 4.2 Failure Modes

1. **Model doesn't support tools.** Mitigation: provider-level fallback (§3.5,
   revised per §3.5.1 to avoid reintroducing prose-inside-JSON for its
   primary audience of small/local models).
2. **Model calls wrong tool or hallucinates arguments.** Mitigation: same retry
   logic as current action validation; tool arguments are validated before
   execution.
3. **Model calls `query_database` with `is_exploration: false` but should
   explore first.** Mitigation: exploration is a soft hint — the model decides.
   If it skips exploration and the query fails, the error is fed back as a
   tool result, and the model can retry.
4. **Agentic loop never terminates.** Mitigation: two independent, meaningful
   budgets — `maxExplorationRounds` and `maxErrorRetries`, tracked separately
   (§2.7) — plus an unconditional `totalRoundCap` safety ceiling (§2.3) that
   bounds even the uncounted round kinds (`render_chart`, `respond_to_user`,
   malformed/unknown tool calls). If either meaningful budget is exhausted,
   an explicit, budget-specific message is returned immediately (a
   clarification for exploration exhaustion, `renderSQLError` for error-retry
   exhaustion) rather than silently falling through to `totalRoundCap`, which
   exists only to catch the pathological uncounted-round case.
5. **[REVIEW #10] `max_tokens` truncates a response mid-tool-call, producing
   invalid/incomplete JSON in `tc.Function.Arguments`.** This is the same
   class of risk a truncated prose answer has today under the old protocol —
   tool calling does not make truncation impossible, only easier to detect.
   Mitigation: `json.Unmarshal` failure on `tc.Function.Arguments` is treated
   as an expected, recoverable case (§2.3's revised pseudocode appends a
   `tool` role error message and continues the loop rather than aborting the
   conversation turn), and a response with empty `Content` and zero
   `ToolCalls` is treated as an incomplete-response clarification rather than
   a hard error (also shown in §2.3). No streaming is used by any current
   provider client, so there is no path to recover a partial tool call
   mid-stream — the only lever available is prompting for shorter
   `respond_to_user` text when `effectiveMaxTokens` is low, which is outside
   this document's scope and unchanged from today's behavior.

### 4.3 Reward

- **Eliminates an entire class of bugs** documented in `ANSWER_CLARIFICATION_ISSUE.md`
- **Simplifies the parsing pipeline** — ~300 lines of fragile parsing code removed
- **Improves answer quality for smaller models** — tool calling is an easier
  task than producing valid JSON with prose inside
- **Enables future agentic features** — multi-step reasoning, data comparison
  across queries, iterative refinement
- **Aligns with industry standard** — tool calling is the recommended pattern
  for LLM-powered applications

### 4.4 Decision

**Proceed with phased implementation** (see §5), subject to:

1. Maintaining backward compatibility for existing conversations
2. Provider-level fallback for models without tool support
3. Full risk/reward documentation appended to `RISK_ANALYSIS_LOG.md`

---

## 5. Implementation Phases

### Phase 1: LLM Client Layer (Lowest Risk)

**[REVIEW — Blocking #4] Scope correction:** the original draft scoped this
phase as "OpenAI/OpenRouter only," claiming other providers were untouched.
That is not achievable in Go — see §2.2.1. Because `ChatCompletionWithTools`
is added to the `LLMClient` interface, **all five provider files must gain a
method implementation in this phase**, even though only OpenAI's is
functionally real. This phase is still "lowest risk" in the sense that no
existing behavior changes for any provider — it is not lowest *scope*.

- Add `Tool`, `ToolCall`, `ToolCallFunction`, `FunctionDef` types to `llm_client.go`
- Add `ChatCompletionWithTools` to the `LLMClient` interface (§2.2.1) —
  existing `ChatCompletion` / `ChatCompletionWithPayload` signatures are
  **not modified**
- Implement `ChatCompletionWithTools` for real in `llm_openai.go`: add
  `Tools` field to `openAIChatRequest`, add `ToolCalls`/`Name` to
  `openAIChatMessage`, parse `tool_calls` out of `openAIChatResponse.Choices[].Message`
- Implement `ChatCompletionWithTools` as a **passthrough stub** in
  `llm_anthropic.go`, `llm_ollama.go`, `llm_local.go` (ignore `tools`,
  delegate to existing `ChatCompletionWithPayload`, return `Content`-only
  `*ChatMessage`) — this is the minimum required for `go build ./...` to
  succeed and is explicitly *not* "Anthropic/Ollama/local tool support,"
  which remain Phase 3/4 work
- Verify `go build ./...` and existing test suite pass with zero behavior
  change for all four providers

**Deliverable:** `LLMClient` interface compiles with all five implementers.
OpenAI/OpenRouter can send tool definitions and receive structured tool
calls. Every other provider is unchanged (stub passthrough). No discussion
engine changes.

### Phase 2: Agentic Loop (Core Change)

- Implement `runAgenticLoop` in `discussion_engine.go` per the revised §2.3
  pseudocode, including: the `validateExplorationQuery` gate (§2.5.1), the
  `isRetryableError`/`renderSQLError` guard before retry (§2.7), and separate
  `maxExplorationRounds`/`maxErrorRetries` counters (§2.7) rather than a
  single flat `maxRounds`
- Implement `handleRespond` (replaces `handleAnswer` for prose). Per §2.3.3,
  `handleRespond` must accept and render the current turn's accumulated
  exploration trace (every `query_database` call made before the final
  `respond_to_user`), not just the response text — this covers routing
  outcomes 2 and 3 from §2.3.3
- Implement `renderToolQueryResults`, including chart attachment via the
  deferred `render_chart` flow (§2.1.1, §2.3.4) and the same exploration
  trace rendering as `handleRespond` — replaces `renderSQLResults`, which
  already does this today via its `explorationResults` parameter
- Implement `formatToolResult` with the row/cell truncation policy from §2.3.2
- Implement the `pendingFinalResult` type and its `render` method (§2.3.4);
  wire the `render_chart` tool into `tools` only when `conversation.VizEnabled`
  is true
- **Acceptance criterion [§2.3.4]:** verify that when charting is enabled and
  the model responds to a final query result with plain text, another
  `query_database` call, or nothing usable (simulated truncation) instead of
  calling `render_chart`, the query results are still rendered to the user
  — chartless — exactly once. This is the Graceful Degradation guarantee for
  this feature and must be tested for all four flush points identified in
  §2.3's pseudocode (plain-text reply, empty/truncated reply, a second
  `query_database` call, and max-rounds exhaustion), not just the happy path
  where `render_chart` is actually called.
- Implement `ToolTranscript`/`TranscriptAction` (§2.3.1), the
  `tool_transcript` column, and `buildToolMessages` — wire `runAgenticLoop`
  to accumulate `TranscriptAction`s as it runs (the same accumulation §2.3.3
  already requires for exploration-trace transparency, serialized instead of
  discarded) and persist them via `CreateConversationMessage` on every
  `handleRespond` / `renderToolQueryResults` call
- **Acceptance criterion [REVIEW — Blocking #1]:** verify a multi-turn
  tool-calling conversation's **second** turn correctly reconstructs the
  **first** turn's exact prior `query_database` call(s) via
  `buildToolMessages` — not a paraphrase, the literal `sql` argument and
  `result_preview` — and that the resulting request is accepted by the
  provider. Test with an actual multi-turn conversation (ask a question that
  triggers `query_database`, then ask a follow-up referencing something
  specific about the first query, e.g. "now filter that to the West region"),
  not just a single-turn smoke test — the bug this closes only manifests on
  turn 2, and the fidelity this design targets only shows up if the
  follow-up genuinely depends on the exact prior SQL, not just prior intent.
- **Acceptance criterion [§2.3.1]:** verify `buildToolMessages` degrades
  gracefully (falls back to a plain-text summary, does not error the whole
  conversation load) when given a `ToolTranscript` with an unrecognized
  `Version` — simulate this by hand-constructing a transcript with
  `Version: 999` in a test.
- **Acceptance criterion [§2.3.3]:** verify that a question answered via
  routing outcome 3 (explore, then `respond_to_user` with no final table) is
  still auditable — the user must be able to expand a trace of every
  `query_database` call made during that turn, matching today's "Show N
  intermediate query(ies)" behavior. Test explicitly with a question like
  "do we have any customers in Japan" that is expected to resolve via
  exploration alone.
- Add feature flag to conversation settings (`use_tool_calling`) per the
  exact migration spec in §4.1.1 (`INTEGER DEFAULT 0`, not `DEFAULT 1`)
- Gate the new path behind the flag; old path remains for existing conversations

**Deliverable:** New conversations can opt into tool calling and survive at
least two conversational turns (initial question + follow-up) without
malformed API requests. Existing conversations continue to use the
JSON-response protocol, unaffected.

### Phase 3: Anthropic Provider

- Extend `llm_anthropic.go`'s `ChatCompletionWithTools` (replacing the Phase 1
  stub) to support tools
- Map `ChatMessage.ToolCalls` to Anthropic's `tool_use` content blocks, and
  `tool` role messages to Anthropic's `tool_result` content blocks (note:
  Anthropic's message format differs structurally from OpenAI's flat
  `tool_calls` array — content blocks are nested inside a single message's
  `content` list, not sibling messages; this mapping needs its own worked
  example before implementation, not just "map to tool_use blocks")
- Test with Claude models, including a multi-turn conversation per Phase 2's
  acceptance criterion

### Phase 4: Provider-Level Fallback

- Implement the revised two-field JSON fallback from §3.5.1 (not the original
  three-field prose-inside-JSON version) for `llm_ollama.go` and `llm_local.go`
- Parses the LLM's minimal `{"action":...}` prefix plus trailing plain-text
  answer into `ChatMessage.ToolCalls` / `ChatMessage.Content`
- Isolated to the LLM client layer — discussion engine is unaware
- Test explicitly against a small local model (the gemma-4-e4b-it failure
  mode from `ANSWER_CLARIFICATION_ISSUE.md` is the regression test for this
  phase — it must no longer produce a truncated or leaked response)

**Deliverable:** Every provider path produces `ChatMessage.ToolCalls`, either
natively or via the revised fallback parsing, verified against at least one
real small/local model known to have previously failed under the old protocol.

### Phase 5: Cleanup and Migration

- Remove dead parsing code (`extractJSONFromResponse`, `fixJSONFormatting`,
  `parseLLMResponse`, `parseLenientJSON`, `extractFieldValue`, `unescapeNewlines`,
  `unescapeFields`, `looksLikeSQL`)
- Remove `_parse_error` action handling from all switch statements
- **Do not remove `LLMResponse` in this phase** (§2.6, Blocking #5) — it is
  still referenced by `classifyErrorCategory`, `buildErrorMetadata`, and
  `renderSQLError`, none of which are in scope for this proposal. Only trim
  unused fields (e.g. confirm nothing outside §2.7's retry path still reads
  `LLMResponse.Explanation`) after auditing all remaining call sites
  individually
- Remove the `explanation` prompt field from the old JSON-response prompt
  text specifically (the *tool* schema's `reasoning` parameter, §2.1.2, is
  the intentional replacement and is not removed)
- Confirm `viz_config` has no remaining references outside the `chart_config`
  tool parameter path (§2.1.1) before removing the old field name
- Flip the *default* `use_tool_calling` value for **newly created**
  conversations to `true` at creation time in application code — this is
  separate from, and must not be confused with, the column's SQL default
  (`INTEGER DEFAULT 0`, §4.1.1), which stays `0` forever so it correctly
  backfills existing rows as `false`
- Remove the feature flag entirely only after a full deprecation window with
  no regressions reported, per the existing project's general caution around
  `discussion_engine.go` changes (`AGENT_READ_FIRST.md` §4.2)

---

## 6. Charter Compliance

Per `AGENT_READ_FIRST.md` §0:

- **Fundamental goal:** Tool calling improves answer delivery by removing four
  classes of rendering bugs. Answer accuracy is unchanged (same SQL execution,
  same markdown rendering). Answer reliability is improved (fewer parsing
  failures).
- **Safety:** Unchanged. `executeSQLWithMode` is the single entry point for
  all queries. The read-only invariant (§0) is preserved.
- **Graceful degradation:** Provider-level fallback (§3.5) ensures that models
  without tool support still function. The fallback is isolated to the client
  layer — the discussion engine is unaware of which protocol is in use.
- **Risk/reward:** Documented in §4 of this document. The reward (eliminating a
  bug class, simplifying the codebase, enabling future features) outweighs the
  risk (medium regression risk mitigated by feature flag and backward
  compatibility).

---

## 7. Files Affected

| File | Change |
|---|---|
| `pkg/services/llm_client.go` | Add `Tool`, `ToolCall`, `ToolCallFunction`, `FunctionDef` types. Add `ChatCompletionWithTools` to `LLMClient` interface (existing methods unchanged — §2.2.1). Add `ToolCalls`/`ToolCallID`/`Name` fields to `ChatMessage`. |
| `pkg/services/llm_openai.go` | Add `Tools` to request struct, `ToolCalls`/`Name` to message structs. Implement `ChatCompletionWithTools` for real (§5 Phase 1). |
| `pkg/services/llm_anthropic.go` | Phase 1: stub passthrough implementation of `ChatCompletionWithTools` (required to compile — §2.2.1). Phase 3: real `tool_use`/`tool_result` content-block mapping. |
| `pkg/services/llm_ollama.go` | Phase 1: stub passthrough. Phase 4: revised two-field JSON fallback (§3.5.1), not the original three-field version. |
| `pkg/services/llm_local.go` | Phase 1: stub passthrough. Phase 4: revised two-field JSON fallback (§3.5.1). |
| `pkg/services/discussion_engine.go` | Add `runAgenticLoop` (with exploration-complexity gate §2.5.1, error-retry design §2.7, dual round counters, deferred-chart state machine §2.3.4), `handleRespond`, `renderToolQueryResults` (chart attached via `render_chart`'s deferred flow, not a same-call parameter — §2.1.1/§2.3.4), `formatToolResult` (with truncation policy §2.3.2), `pendingFinalResult` type + `render` method (§2.3.4), `ToolTranscript`/`TranscriptAction` types + `buildToolMessages` translator (§2.3.1). Gate behind feature flag. `LLMResponse` and `_parse_error` handling are trimmed, not removed wholesale (§2.6, Blocking #5) — `classifyErrorCategory`/`buildErrorMetadata`/`renderSQLError` keep using it. |
| `pkg/models/conversation.go` | Add `UseToolCalling bool` field (`json:"use_tool_calling"`) to `Conversation` struct, alongside existing `VizEnabled`/`Summarize`/etc. bools. |
| `pkg/models/conversation_message.go` | Add `ToolTranscript *string` field (`json:"tool_transcript,omitempty"`) to `ConversationMessage` struct, alongside existing `LLMContent`/`SQLResults`/`Metadata` nullable string fields (§2.3.1). |
| `pkg/models/database.go` | Migrations: `ensureColumn("conversations", "use_tool_calling", "INTEGER DEFAULT 0")` (§4.1.1) and `ensureColumn("conversation_messages", "tool_transcript", "TEXT")` (§2.3.1). Both nullable/defaulted, additive only, per §3.2. |
| `app.go` | Optional: expose `UseToolCalling` toggle in conversation settings, following the existing pattern for `VizEnabled`/`Summarize` bindings. |

---

## 8. References

- [`ANSWER_CLARIFICATION_ISSUE.md`](ANSWER_CLARIFICATION_ISSUE.md) — Five rendering/parsing bugs documented
- [`AGENT_READ_FIRST.md`](AGENT_READ_FIRST.md) — Project charter, risk framework, goal priority
- [`RISK_ANALYSIS_LOG.md`](RISK_ANALYSIS_LOG.md) — Append risk/reward analysis for this enhancement
- [OpenAI Function Calling Guide](https://platform.openai.com/docs/guides/function-calling)
- [Anthropic Tool Use Guide](https://docs.anthropic.com/en/docs/build-with-claude/tool-use)
- [OpenRouter Tool Calling](https://openrouter.ai/docs/features/tool-calling)

---

## 9. Technical Review Log

This document was reviewed against the live codebase after the initial draft.
The review identified five blocking issues (would prevent implementation from
working or compiling as originally scoped) and five design gaps (would ship
working but regressed or under-specified behavior). All ten are now resolved
inline above; this section is the index of what changed and why, kept for
future agents auditing this document's history.

| # | Severity | Finding | Resolved In |
|---|---|---|---|
| 1 | Blocking | Conversation history replay (`buildLlmMessages`) is incompatible with tool-call transcripts as originally claimed "unaffected" | §2.3.1 |
| 2 | Blocking | `viz_config` / charting silently dropped with no replacement | §2.1.1 |
| 3 | Blocking | `explanation`'s chain-of-thought accuracy benefit removed with no replacement, contradicting `ANSWER_CLARIFICATION_ISSUE.md` §6.1's rationale for keeping it | §2.1.2 |
| 4 | Blocking | `LLMClient` interface signature mutation is not valid Go and breaks the Phase 1 "OpenAI-only" scoping claim | §2.2.1, §5 Phase 1 |
| 5 | Blocking | SQL error-retry loop (`executeFinalQueryWithRetry`, `isRetryableError`, backoff) never addressed; `LLMResponse` removal claim ignored 8+ non-JSON-parsing call sites | §2.6, §2.7 |
| 6 | Design Gap | `is_exploration` self-reported flag silently dropped `validateExplorationQuery` complexity gating | §2.5.1, §2.3 pseudocode |
| 7 | Design Gap | No truncation policy for tool results accumulating in context across agentic loop rounds | §2.3.2 |
| 8 | Design Gap | Feature flag migration described as a bullet point, not a spec; default value unstated (and wrong default would silently migrate all existing conversations) | §4.1.1 |
| 9 | Design Gap | Fallback protocol (§3.5) reintroduced prose-inside-JSON for exactly the small/local-model population most likely to need it | §3.5.1 |
| 10 | Design Gap | No handling specified for `max_tokens` truncating a response mid-tool-call | §2.3 pseudocode, §4.2 failure mode 5 |
| 11 | Design Gap | Three-way routing after exploration (final query / prose-with-table / prose-only) never made explicit; exploration-trace transparency ("Show N intermediate queries") has no equivalent under §2.3.1's persistence model | §2.3.3 |
| 12 | Design Decision | `chart_config` moved from a same-call `query_database` parameter to a separate `render_chart` tool, callable only after the model has seen real query results — an intentional latency/quality trade-off, not a defect fix. Required breaking the "final query call is automatically terminal" invariant assumed throughout §2.3, and introduced a mandatory Graceful-Degradation flush rule at every loop exit point so a completed result can never be lost while awaiting an optional chart decision | §2.1, §2.1.1, §2.3.4 |
| 13 | Design Decision | Conversation history persistence (Blocking #1's resolution) revised from "collapse to prose summary" to a canonical, versioned, provider-neutral `ToolTranscript` translated at request-build time — chosen after recognizing tool-call IDs are per-request correlation tokens with no cross-request identity requirement, which removed the cross-provider-ID objection that originally motivated the prose-collapse choice. Trades one new nullable column and a translator function for full-fidelity replay of prior tool calls on later turns | §2.3.1 |
| 14 | Design Decision | §2.7's single-counter/dual-counter question resolved: `maxExplorationRounds` and `maxErrorRetries` implemented as two independently-tracked counters (not the single flat `maxRounds` still present in an earlier pseudocode revision, which contradicted §2.7's own recommendation). `render_chart` explicitly metered against neither counter, same treatment as summarization's second LLM call today. Per-round tech-details entries now tagged with a `roundKind` (exploration/error_retry/chart/final/other) for debugging. Introduced `totalRoundCap` as a separate, non-configurable safety ceiling distinct from either meaningful budget (§2.3.5), and caught/fixed a `defer`-in-a-loop bug in an intermediate draft of the `storePayload` tagging logic before it reached this revision | §2.3, §2.3.5, §2.7 |

**Outstanding before implementation begins:** none of the fourteen are left as
open questions in this revision — each has an explicit design decision
recorded at its resolution point above. If any resolution proves wrong during
implementation (most likely candidate: §2.3.1's `buildToolMessages`
translator — it is the newest, least-precedented piece of code in this
proposal, and the one most exposed to per-provider wire-format quirks that
won't be discovered until tested against a real API), update this log and
the relevant section together, per `AGENT_READ_FIRST.md`'s general rule that
documentation and code drift must be corrected in the same change.