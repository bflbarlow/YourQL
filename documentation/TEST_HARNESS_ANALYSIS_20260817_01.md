# Test Harness Analysis — Technical Recommendations

**Date:** 2026-08-17
**Source:** 180 conversation logs (6 models × 15 scenarios × 2 runs) via `yourql-test-harness`
**Reference:** `CONVERSATION_ANALYSIS_2026-08-16.md`

---

## Finding 1 — CRITICAL: Sales-rep scorecard context overflow (100% failure, 0 tokens)

### What the test found

The question "Build a complete performance scorecard for each sales rep…" (seven
distinct metrics, multi-table joins, window functions) produced **0 tokens and no
tool calls** from every model across both providers. This is a systemic context-window
exhaustion, not a per-model quality issue.

### Root cause

The combined prompt — system persona, 8-table schema with column-level detail,
Database Connection metadata, 6 instructions, Exploration Safety Rules block
(now including the expanded `safety.footer_rounds` and `instructions.4` we added),
Charts guidance block, tool definitions for 3–5 tools — plus the user's ~100-word
multi-part question — exceeds the practical response headroom for the LLM. The
provider returns empty content (some providers silently truncate; others return an
empty completion). The YourQL loop receives `response.Content == ""`,
`len(response.ToolCalls) == 0`, and falls through to the empty-response path in
`agentic_loop.go:1185`:

```go
// Empty response — truncated or malformed
if len(response.ToolCalls) == 0 {
    logRound(roundKindOther)
    if pendingFinal != nil {
        // render whatever we have
        ...
    }
    return handleClarification(query, LLMResponse{
        Action:                "clarification",
        ClarificationQuestion: cfg.ResponseEmptyTruncated,
    }, conversation.ID)
}
```

The user sees `"I received an incomplete response. Could you try rephrasing your
question?"` with no indication that the failure was context exhaustion vs. a
malformed response vs. a server timeout.

### Implementation approach

**Phase 1 — Detect and label (low effort, immediate signal)**

In the empty-response handler, check for the specific signature of context
exhaustion: `response.Content == "" && response.FinishReason != "stop"` (or
`response.PromptTokens` approaching the model's context limit). Distinguish:

| Condition | Likely cause | User message |
|---|---|---|
| `PromptTokens` near model max + 0 completion tokens | Context window overflow | "This question is too complex for the current context window. Try breaking it into smaller parts." |
| `PromptTokens` within range but 0 completion | Provider timeout / silent truncation | "The model produced no output. This may be a temporary issue — try again." |
| `response.Content != ""` but unparseable | Model output couldn't be parsed | "I received a response I couldn't interpret. You can view the raw output under tech details." |

In `agentic_loop.go:1185`, replace the single `handleClarification` call with a
three-way dispatch based on what we know about the response. The `handleClarification`
signature already accepts a message string; we just need to pass different strings
based on the failure mode.

**Files:** `agentic_loop.go` (empty-response handler), `agent_loop_config.go`
(three new response strings to replace/alongside `response.empty_truncated`).

**Phase 2 — Re-prompt with simplification (medium effort, recovers the use case)**

When context overflow is detected:

1. Send a follow-up system message that instructs the model to decompose the
   original question into sub-questions and answer the first one:
   *"The previous question was too complex for the context window. Answer just the
   FIRST part independently, then indicate you're ready for the next part."*
2. The model produces a partial answer (e.g., "total revenue per rep").
3. The user sees a helpful error + the partial result, and the conversation can
   continue with the user explicitly asking for the remaining metrics.

This requires a new code path in the loop: after the empty-response detection,
construct a simplified re-prompt message, make one more LLM call, and render the
result. Careful: this adds one extra LLM round-trip and must be guarded against
infinite loops (the `totalRoundCap` already handles this; a single retry is safe
within the existing loop bound).

**Files:** `agentic_loop.go` (new re-prompt logic after empty-response detection).

### Verification

- Run the scorecard question through headless mode with a known context-limited
  model. Confirm the error message changes from the generic "incomplete response"
  to the context-overflow variant.
- Run a normal short question to confirm the detection doesn't false-positive.

---

## Finding 2 — HIGH: Fallback protocol cannot trigger charts

### What the test found

The `local` provider (LM Studio, used by both Ollama and Local clients) produced
0 chart detections across 4 visualization-question runs, while the `openai` provider
(Ollama) produced 6/8. The fallback protocol path has no mechanism to generate
`chart_config`.

### Root cause

The fallback protocol in `llm_ollama.go:284` (shared with `llm_local.go`) defines
exactly two actions:

```go
const fallbackInstructions = `
...
For database queries:
{"action":"sql","sql":"SELECT ... LIMIT 10"}

For responses, explanations, or clarifying questions:
{"action":"respond"}
[your full response using markdown]
...
`

switch fallback.Action {
case "sql":
    // returns ChatMessage with ToolCall: query_database
case "respond":
    // returns ChatMessage with Content: restPart
default:
    // returns ChatMessage with Content: text
}
```

There is no `"action":"chart"` branch, no `render_chart` tool call synthesized,
and no code path from fallback to `chart_config`. The `parseFallbackResponse`
function simply doesn't produce `render_chart` tool calls.

Meanwhile, the model is told (in `fallbackInstructions`) that charts are
"automatically generated from your query results when visualization is enabled."
This instruction is actively misleading — it creates an expectation the system
never fulfills.

### Implementation approach

**Option A — Remove the misleading text (trivial, immediate)**

Delete the chart-related sentence from `fallbackInstructions` in
`llm_ollama.go:287`. One line removed. The model stops expecting charts and
won't waste tokens producing prose about a chart that never appears. Downside: no
charts for local models — the gap remains but the model no longer misbehaves about it.

**Option B — Add a `"action":"chart"` path (medium effort, closes the gap)**

1. Add a third action to `fallbackInstructions`:
   ```
   After a successful SQL query, if you want to attach a chart:
   {"action":"chart","type":"bar","labels":["$column_name"],"values":["$column_name"]}
   ```
2. Add a `case "chart"` branch to `parseFallbackResponse` that constructs a
   `render_chart` tool call:
   ```go
   case "chart":
       chartConfig := buildFallbackChartConfig(fallback)
       return &ChatMessage{
           Role: "assistant",
           ToolCalls: []ToolCall{
               {ID: "fb_chart_0", Function: ToolCallFunction{
                   Name: "render_chart",
                   Arguments: chartConfig,
               }},
           },
       }
   ```
3. Add `buildFallbackChartConfig` — maps the simpler fallback chart schema
   (`type`, `labels`, `values`) to the full `chart_config` JSON the
   `render_chart` tool expects. This is straightforward because the fallback
   model doesn't need the full Chart.js config surface; a bar chart with
   labels and values covers the most common visualization request.

**Recommendation:** Do Option A immediately (fix the lying text). Schedule Option B
as follow-up if local-model chart support is a priority. The two can ship independently.

### Verification

For Option A: grep `fallbackInstructions` to confirm no chart-related text
remains. For Option B: run a headless visualization scenario (`order-status-totals`
or `product-line-performance`) against the `local` provider and confirm
`chart_config` appears in the message metadata.

---

## Finding 3 — HIGH: "Incomplete response" collapses 4+ failure modes

### What the test found

17% of all conversations (30/180) ended with the generic message:

> *"I received an incomplete response. Could you try rephrasing your question?"*

This message is produced identically for:
- Server timeout / crash (0 tokens, no error)
- Context overflow (0 tokens, no error)
- Model produced unparseable output (tokens present, no tool calls)
- SQL execution failure after query was parsed
- Loop exhaustion (max rounds hit)

The user and any automated grader see the same string for all of them.

### Root cause

The failure messages are defined as single defaults in `agent_loop_config.go`:

```go
"response.empty_truncated":  "I received an incomplete response. Could you try rephrasing your question?",
"response.loop_exhausted":   "I wasn't able to complete this request due to repeated invalid responses. Could you try rephrasing your question?",
```

And consumed at exactly two call sites in `agentic_loop.go`:

- **Line 1193:** Empty/truncated response → `cfg.ResponseEmptyTruncated`
- **Line 1584:** Loop exhausted → `cfg.ResponseLoopExhausted`

There is no conditional logic between "the model returned nothing" and "the model
returned something we couldn't parse" — both hit the same `len(ToolCalls) == 0`
check. The raw output is stored in the tech-detail payload (`td.Response.RawOutput`)
but never surfaced in the user-visible message.

Additionally, the `headless.go` path directly returns the assistant message content
to the testing harness. The harness grader sees whatever the AssistantMessage
`Content` field holds — which for clarification responses is the generic string,
not any diagnostic detail.

### Implementation approach

**Step 1 — Differentiate the empty-response path (see Finding 1 above)**

Add a three-way dispatch in `agentic_loop.go:1185` based on prompt tokens,
completion tokens, and response content. This covers context overflow, provider
timeout, and unparseable output. Each gets a distinct user-facing message
(three new config strings in `agent_loop_config.go`).

**Step 2 — Add a diagnostic block to clarification messages**

When `handleClarification` is called, append a diagnostics section to the
assistant message that is hidden behind the tech-details toggle but visible to
the test harness. The clarification message already stores metadata; adding a
`diagnostics` JSON field with `prompt_tokens`, `completion_tokens`,
`finish_reason`, and the raw output prefix gives the grader and developer
immediate signal without changing the user-facing text.

In `handleClarification` (`discussion_engine.go:402`):

```go
message := resp.ClarificationQuestion
// Append diagnostics block for test harness / tech-details consumption.
// This is NOT rendered as user-visible prose — it lives in metadata.
diag := map[string]interface{}{
    "failure_mode":   resp.Action,
    "prompt_tokens":  query.PromptTokens,
    "completion_tokens": query.CompletionTokens,
}
```

This is stored in the message metadata and surfaced through the existing
`buildErrorMetadata` / tech-details path — the headless harness can read it
from the message response.

**Step 3 — Distinguish loop exhaustion from empty response**

The loop-exhaustion path (line 1584) already uses a different config string
(`ResponseLoopExhausted`). Verify this is actually distinct from
`ResponseEmptyTruncated` in the config. Currently both are similar ("could
you try rephrasing…") — make them clearly distinct so the grader can tell
which failure mode occurred:

| Config key | Current | Proposed |
|---|---|---|
| `response.empty_truncated` | "I received an incomplete response…" | "The model returned no response. This may be due to a timeout or the question being too complex for the current context window." |
| `response.loop_exhausted` | "I wasn't able to complete…" | "I tried several approaches but couldn't produce a reliable answer. Could you rephrase or simplify your question?" |

### Verification

Run the scorecard question and confirm the error message is the context-overflow
variant, not the generic "incomplete response." Run a deliberately invalid prompt
against a local model to trigger unparseable output and confirm that variant fires.
The headless harness should be able to parse `failure_mode` from the message
metadata.

---

## Finding 4 — MEDIUM: Summarization signal is a heuristic, not a direct measurement

### What the test found

The grader can't determine whether `summarizeResults()` actually ran vs. the
model just produced a data-backed prose response. The column conflates
"summarization feature fired" with "content ≥ 200 chars + data-backed."

### Root cause

Summarization runs inside `pendingFinalResult.render()` in
`agentic_loop.go:180`:

```go
if conversation.Summarize && p.result != nil {
    s, err := summarizeResults(p.ctx, p.client, p.userMessage, p.sql, p.result,
                               p.skillsContent, conversation.ID)
    if err != nil {
        log.Printf("[AgenticLoop] Summarization failed: %v", err)
        fallback := "⚠️ Summary unavailable…"
        summary = &fallback
    } else if s != "" {
        summary = &s
    }
}
```

The resulting summary is embedded in the assistant response HTML (via
`renderToolQueryResults` → `AssistantResponse.ToHTML()`) but there is no
dedicated metadata flag indicating "this message has a summary separate from
the respond text." The test harness reads message content as a blob and uses
a length heuristic.

### Implementation approach

Add a `"summary_generated": true` flag to the assistant message metadata when
`summarizeResults()` succeeds and produces non-empty output. Also add a distinct
`"summary_failed": true` flag when it fails (currently the fallback text "⚠️
Summary unavailable" is embedded in prose, which is fragile to parse).

In `pendingFinalResult.render()` in `agentic_loop.go:180`, after the summary
call:

```go
// Build metadata with explicit summarization signal.
meta := map[string]interface{}{
    "content_type":       "html",
    "summary_generated":  summary != nil && *summary != "" && !strings.HasPrefix(*summary, "⚠️"),
    "summary_failed":     err != nil,
}
metadataJSON, _ := json.Marshal(meta)
```

The headless harness can then read `summary_generated`/`summary_failed` from
the message metadata instead of relying on the length heuristic, giving an
accurate per-message signal for whether the LLM summarization call fired and
succeeded.

### Verification

Run a summarization-enabled scenario through headless mode and confirm the
response message's metadata includes `summary_generated: true` when the text
was produced by `summarizeResults()` vs. `summary_generated: false` when the
model produced data-backed prose without a separate summarization call.

---

## Finding 5 — MEDIUM: `unpaid-orders` ambiguity remains the hardest question

### What the test found

9 of 12 runs (75%) were low quality on this scenario. The question asks "total
dollar amount of orders not yet paid for" but the schema has no `paid` column
and payments are tracked per-customer (not per-order). Models that explain the
ambiguity to the user are rewarded; models that guess a single answer produce
"incomplete response" or a wrong answer.

### Relevance to our recent changes

Our `instructions.4` update (from earlier in this session) explicitly tells the
model:

> *"If you are not confident you understand what the user is asking — especially
> when the question depends on a concept the schema does not unambiguously define
> (e.g., 'paid', 'unpaid') — ask a clarifying question…"*

This directly targets the unpaid-orders scenario and should bias models toward
explaining ambiguity. The 25% of runs that did handle it well may already be
benefiting from this instruction. The 75% that didn't likely still explore first
then guess — our `safety.footer_rounds` says "if you exhaust the budget without
a confident answer, ask a guiding question" but the model often exhausts the
budget and then produces a final query anyway (guessing).

### Implementation approach

**No code change required for this specific finding** — the prompt changes are
already in place. What's needed is evaluation: re-run the unpaid-orders scenario
and compare pass rates against baselines with and without the new instructions.

If the pass rate doesn't improve, the next lever is the exploration budget
timing — currently the model tends to explore first, then clarify only when
forced. A more aggressive instruction like "If the question can't be answered
unambiguously from the schema alone, ask the user to clarify BEFORE running any
exploration queries" would front-load the clarification. This trades exploration
depth for faster resolution on inherently ambiguous questions.

---

## Finding 6 — LOW: Read-only enforcement holds (CTE-DML gap untriggered)

### What the test found

Zero read-only violations across 180 runs. The enforcement works for normal
use. However, the known gap between `executeSQLWithMode` (final queries:
SELECT/WITH prefix check only) and `validateExplorationQuery` (exploration
queries: prefix check + DML/DDL blocklist + comment stripping) was not tested
by any of the 15 benign scenarios.

### Root cause — reminder

From `sql_execution.go`:

- **Exploration path** (`validateExplorationQuery`, line ~756): checks
  SELECT/WITH prefix AND strips comments AND blocklists INSERT/UPDATE/DELETE/
  DROP/ALTER/TRUNCATE/CREATE/REPLACE/GRANT/REVOKE/LOAD_FILE/INTO OUTFILE/
  BENCHMARK/SLEEP/EXEC/xp_/sp_.

- **Final query path** (`executeSQLWithMode`, line ~210): checks SELECT/WITH
  prefix ONLY. If `database/sql` driver allows multi-statement execution, a
  `SELECT …; DROP TABLE …;` could slip through. Most Go SQL drivers disable
  multi-statement by default, but this is not guaranteed across all 11
  registered drivers.

### Implementation approach

Add the same keyword blocklist from `validateExplorationQuery` to
`executeSQLWithMode` as a second pass after the SELECT/WITH prefix check. The
code already exists in `validateExplorationQuery` — it would be a ~5-line
delegation:

```go
// In executeSQLWithMode, after the SELECT/WITH prefix check:
if err := validateExplorationQuery(sqlQuery, ExplorationRelaxed); err != nil {
    // Block DML/DDL even in final queries — same safety net as exploration
    return nil, err
}
```

Or extract the blocklist into a shared helper (`containsDangerousKeywords`),
call it from both `validateExplorationQuery` and `executeSQLWithMode`, and
keep the mode-specific complexity rules only in the exploration path.

### Verification

Write an adversarial headless scenario that attempts a CTE with an embedded
write — something like:

```sql
WITH cte AS (SELECT * FROM orders) DELETE FROM payments WHERE customerNumber = 1
```

This should be rejected by both the exploration AND final-query paths after
the fix. Currently it would be rejected only by the exploration path. Run
against the `openai` provider with relaxed safety mode.

---

## Cross-cutting observation

Findings 1 and 3 are fundamentally the **same root cause** (context exhaustion)
presenting through different symptoms — one as a 0-token response, one as a
generic error message. Fixing the diagnostic messaging (Finding 3, Step 1)
automatically improves the scorecard scenario (Finding 1) by giving the user
and grader a meaningful error instead of the generic string. The follow-up
re-prompt logic (Finding 1, Phase 2) would then recover the use case.

Findings 2 (fallback charts) and 4 (summarization signal) are independent
cleanup items that make the system more honest and measurable.

Finding 5 (unpaid-orders) is already addressed by our prompt changes and needs
re-evaluation, not new code.

Finding 6 (CTE-DML gap) remains low-priority but is a one-line safety
improvement worth shipping alongside any other `sql_execution.go` change.

---

*Technical analysis completed against the YourQL codebase at
`/Users/bflbarlow/Wails/YourQL` as of 2026-08-17. Referenced files:
`agentic_loop.go`, `agent_loop_config.go`, `discussion_engine.go`,
`sql_execution.go`, `llm_ollama.go`, `llm_local.go`, `headless.go`,
`headless_handlers.go`.*