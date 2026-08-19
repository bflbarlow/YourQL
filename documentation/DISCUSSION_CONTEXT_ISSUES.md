# DISCUSSION_CONTEXT_ISSUES.md — Conversation History Context Exhaustion

> Companion to `AGENT_READ_FIRST.md`. This document captures the conversation
> history context exhaustion problem — why it happens, what it looks like to
> users, the solution options considered, and the selected fix with
> implementation details. Decisions are logged to `RISK_ANALYSIS_LOG.md`.
>
> **Note on line numbers:** Sections above "Implementation Details (As
> Shipped)" describe the pre-fix state and cite line numbers from *before*
> the fix landed (e.g., `agentic_loop.go:947`, `:224-225`). Those numbers are
> now stale — the file has grown since. The "Implementation Details (As
> Shipped)" section and everything after it reflects the current, post-fix
> line numbers as of last review.

---

## The Problem

When a user asks a question that requires multiple exploration queries, the
agentic loop accumulates large tool result messages in the conversation
history. Each tool result contains a **fully formatted markdown table** (up
to 50 rows via `formatToolResult`). There is a second, independent
contributor: each tool **call** (not just its result) is replayed with its
original `reasoning` argument intact — the model's own chain-of-thought
justification for that query, often 400–700+ characters per call, verbatim
in `TranscriptAction.Arguments` → `ToolCallFunction.Arguments` →
replayed by `buildToolMessages` on every future turn.

In the diagnosed conversation, a turn with 9 exploration/final queries
produced roughly **12–13KB of tool result tables** plus **~3–4KB of
replayed reasoning text** in the tool calls themselves, on top of the
**~5KB system prompt** (schema + instructions + active skill persona) that
is rebuilt fresh for every LLM call regardless of history. Combined, a
single bloated turn can push total context per request into the **20KB+**
range — and that entire turn still only counts as "1" against the 15-turn
history cap.

On the **next** user message — even a trivial one like "How many orders are
in the database?" — the conversation history still contains all those
bloated tool results from the prior turn. The model's context window is
exhausted before it can process the new question, producing an empty
response. The system falls through to `ResponseEmptyTruncated`:

> *"I received an incomplete response. Could you try rephrasing your
> question?"*

The user rephrases → same exhausted context → same empty response. The
conversation is effectively bricked until the user starts a new thread.

## Current Mitigations (Insufficient)

1. **`maxContextMessages`** (default 5, hard cap 15, enforced in
   `discussion_engine.go` via `history = history[len(history)-limit:]`)
   caps the number of **persisted conversation turns** sent to the model —
   not the number of `ChatMessage`s actually transmitted, and not their size.
   This is the crux of why the cap doesn't help here: `buildToolLlmMessages`
   (`agentic_loop.go`) expands each turn's `ToolTranscript` via
   `buildToolMessages` into one `ChatMessage` per tool call/result. A single
   turn with 9 exploration queries — which counts as **1** against the
   15-turn cap — expands into **10 `ChatMessage`s** (1 assistant message
   carrying all 9 tool calls + 9 separate tool result messages, per
   `buildToolMessages`). The cap operates at the wrong granularity to
   prevent this failure mode — it limits how many turns are remembered,
   not how much text each turn is allowed to cost.

2. **Summarization digest** (`formatResultsDigestForSummarization`,
   `discussion_engine.go`) produces compact subsets for the summarization LLM
   call, but this is not used for the general conversation history replay —
   `buildToolMessages` uses `TranscriptAction.ResultPreview` verbatim, which
   is the full `formatToolResult` output.

3. **Tool result preview** (`formatToolResult`, `agentic_loop.go:224-225`,
   `toolResultMaxRows = 50`, `toolResultMaxCellChars = 200`) caps at 50 rows
   — better than unbounded, but still far more than the model needs to
   understand "a query happened and returned data." This is also the exact
   same string persisted as `TranscriptAction.ResultPreview` and replayed
   verbatim on every subsequent turn for the lifetime of the conversation
   (until the turn ages out of the 15-turn window).

## Observed Failure Pattern

```
Turn N:   9 exploration queries -> 9 tool results (~12-13KB tables)
                                 + ~3-4KB replayed reasoning in tool calls
                                 + ~5KB system prompt (rebuilt every call anyway)
                                 = ~20KB+ for this one turn, counted as "1" of 15
Turn N+1: User: "Summarize that" -> context exhausted -> empty response
Turn N+2: User: "How many orders?" -> same exhausted context -> empty response
Turn N+3: User: "What's 2+2?" -> same exhausted context -> empty response
```

The conversation is dead. The user must start a new thread.

---

## Possible Solutions

*(Option 1 was selected and implemented. Options 2–5 are retained as
historical record of the alternatives considered, but none were chosen.)*

### Option 1 — Digest Tool Results in History Replay

Replace `formatToolResult`'s full 50-row table with a compact digest when
replaying tool results in the conversation history.

**Correction/clarification:** `formatToolResult` currently has a single call
site (`agentic_loop.go:947`), and its output feeds **both** (a) the live
tool-result message within the current turn and (b) `TranscriptAction.ResultPreview`,
which is replayed verbatim on all future turns via `buildToolMessages`. It is
**not** used by the final rendered results table shown to the user — that's
a separate function, `formatResultsHTML` (`sql_execution.go`), so the actual
data grid the user sees is unaffected either way.

However, `TranscriptAction.ResultPreview` **is** what appears in the
tech-details "📥 Request messages" panel, since that panel dumps the literal
`messages` array sent to the LLM (`TechDetail.Request.RawMessages`). Digesting
this content **will change what tech-details shows** for historical turns —
arguably for the better (a digest is more legible than a 50-row table), but
this is a real, visible side effect, not a no-op for observability. Any
implementation should either (a) accept this as an intentional, positive
change to tech-details, or (b) introduce a separate un-digested field
specifically for tech-details if full fidelity there is considered important.

**What it looks like:**
```
Query returned 98 row(s). Columns: customerNumber, num_orders, total_revenue.
Sample (3 of 98): 103|3|22314.36, 112|3|80180.98, 114|5|180585.07
Column stats: num_orders avg=2.8, total_revenue avg=45123.15
```

**Pros:**
- Dramatic context reduction (~12–13KB of tool result tables → ~2KB digest
  for a 9-query turn)
- Model still gets statistical awareness (row count, column names, sample rows)
- No breaking change to the final rendered results table — only the replayed
  history and (as noted above) tech-details' "Request messages" view change

**Cons:**
- Model loses exact row-level data from prior explorations
- Follow-up "what was the third row in the second query?" would require a re-query
- Digest format needs to be carefully designed to not lose critical info

**Risk:** Low. The August 5th summarization digest already proved this
approach works for the summarization path. Extending it to history replay is
a natural extension.

---

### Option 2 — Drop Tool Results Entirely, Rely on Schema + Transcript

Keep only the schema and the tool transcript (actions + result previews) in
the conversation history. Strip raw tool result content entirely.

**Pros:**
- Smallest possible context footprint
- Model can always re-query if it needs specific data

**Cons:**
- Model has NO awareness of prior results — every follow-up requires a re-query
- "What was the trend in that last chart?" becomes impossible without re-running
- Too aggressive for complex multi-turn analysis

**Risk:** Medium. May degrade answer quality for follow-up questions.

---

### Option 3 — Sliding Window on Row Count

Cap tool results at N rows (e.g., 10 instead of 50). First N rows + trailing
summary.

**Pros:**
- Simplest implementation — change one constant
- Model still sees real data rows
- Still have statistical summary via column stats

**Cons:**
- 10 rows × 9 queries is still ~5KB — better but not solved
- N is arbitrary — some queries need more context than others

**Risk:** Low but incomplete fix.

---

### Option 4 — Per-Turn Context Budget

Assign a token budget per turn. Before sending messages to the LLM, trim the
oldest turn's tool results to fit. Greedy: keep as many full results as
possible until budget is hit, then digest the rest.

**Pros:**
- Most intelligent — adapts to available context
- Preserves recent results in full, digests older ones
- Fits naturally within existing `maxContextMessages` framework

**Cons:**
- Complex to implement (token counting, trim logic)
- Token counting is provider-dependent (Anthropic vs OpenAI tokenizers differ)
- Adds latency to message assembly

**Risk:** Medium complexity, low operational risk.

---

### Option 5 — Auto-Create New Context on Empty Response

When the model returns an empty response (truncated), detect that it was
likely context exhaustion (not a model error) and automatically retry with
a stripped context — only the system prompt + the latest user message, no
prior tool results.

**Implementation note:** the current empty-response path is a single
terminal branch in `runAgenticLoop` (`agentic_loop.go:~885`) that calls
`handleClarification` with `cfg.ResponseEmptyTruncated` and returns
immediately — there is no existing retry behavior to build on. This option
would replace that terminal `return` with a one-shot retry using a rebuilt,
minimal `messages` slice before falling back to the clarification message.

**Pros:**
- Zero implementation complexity for the happy path
- Only activates on failure — no overhead for normal operation
- Self-healing — user doesn't need to start a new thread

**Cons:**
- Reactive, not proactive — user still sees one "incomplete response" message
  before the retry
- May retry for non-exhaustion causes (model error, network issue)
- Falls back to having the model re-query if it needs prior data

**Risk:** Low. Additive fallback path. Could be combined with any of Options 1-4.

---

## Decision

**2026-08-10: Option 1 (Digest Tool Results) selected.** The model will
receive a compact digest for prior tool results instead of full 50-row
markdown tables. If the model needs exact row-level data from a prior turn,
it can re-query the database — the agentic loop already supports this. Two
complementary changes ship alongside:

- Tool result tables are replaced with a digest (row count, column names,
  sample rows, column statistics).
- Replayed `reasoning` arguments are stripped from historical tool calls in
  `buildToolMessages` — the model doesn't need its own prior chain-of-thought
  verbatim to understand what happened.

Estimated context reduction for a worst-case 9-query turn: ~20KB+ → ~7-8KB.

---

## Implementation Details (As Shipped)

Both changes below are implemented and merged (see the Decisions table).
Line references reflect the current state of `pkg/services/agentic_loop.go`
as of this document's last review — re-verify with `grep -n` before relying
on them, since the file continues to change.

### Change 1 — `formatToolResult` produces a digest instead of a table

**File:** `pkg/services/agentic_loop.go`
**Function:** `formatToolResult(result *QueryResult) string` (~line 237)
**Call site:** `toolContent = formatToolResult(result)` (~line 1098)

The function now emits a compact digest instead of a markdown table:

```
Query returned N row(s).
Columns: col1_name, col2_name, ..., colN_name
Sample (3 of N):
  val1|val2|...
  val1|val2|...
  val1|val2|...
Column stats:
  col1_name: avg=X.XX min=Y.YY max=Z.ZZ
  col2_name: avg=X.XX min=Y.YY max=Z.ZZ
```

As actually implemented:
- **Row count** — always included first. If 0 rows, the function returns
  immediately after the row-count line (no Columns/Sample/Stats sections).
- **Column names** — comma-separated, via `humanizeColumnName`.
- **Sample rows** — fixed at `toolResultSampleRows = 3` (not "up to 3" driven
  by a shared constant with truncation — it's its own constant). Rows are
  pipe-delimited (`|`). If 0 rows, this section doesn't apply (row count is 0
  already returned above).
- **Column stats** — computed only for columns where every non-NULL value in
  the result set parses as a number, via `detectNumericColumns()` (checks
  `float64`/`int`/`[]byte`/`string` values with `strconv.ParseFloat`) and
  `computeToolResultColStats()`. Shows avg/min/max to 2 decimal places. If no
  numeric columns exist, the "Column stats" section is omitted entirely.
- **Cell truncation** — `toolResultMaxCellChars = 200` per cell in samples.

**Correction from the original plan:** the old `toolResultMaxRows = 50`
constant no longer exists — it was fully replaced by `toolResultSampleRows = 3`.
There is no retained "10-row cap" or similar; the table-formatting loop was
removed entirely, not capped.

**Side effect on tech-details:** `TranscriptAction.ResultPreview` (and thus
the tech-details "📥 Request messages" panel, via `TechDetail.Request.RawMessages`)
now shows the digest instead of the full markdown table for historical
turns. This was an intentional, accepted trade-off — the digest is more
legible for human debugging too. The live in-turn tool result (what the
model sees immediately after an exploration query, within the same turn) is
also a digest, which was also intentional.

### Change 2 — `stripReasoningFromArgs` trims replayed reasoning

**File:** `pkg/services/agentic_loop.go`
**Function:** `stripReasoningFromArgs(argsJSON string) string` (~line 603)
**Used in:** `buildToolMessages()` (~line 541), at the call site
`Function: ToolCallFunction{Name: action.Tool, Arguments: stripReasoningFromArgs(action.Arguments)}`
(~line 574)

Before replaying a historical tool call's `Arguments` JSON:

1. `json.Unmarshal` into a `map[string]interface{}`
2. `delete(m, "reasoning")`
3. `json.Marshal` back to a string
4. If unmarshal/marshal fails at any step, return the original `argsJSON`
   unchanged (fail-safe fallback)

**Important:** This trimming applies only inside `buildToolMessages`, i.e.,
only to **historical** replay. The live in-turn transcript recording (around
~line 1104–1108, `TranscriptAction{Arguments: tc.Function.Arguments, ...}`)
stores the raw, untrimmed arguments — stripping only happens later, at
replay time, when `buildToolMessages` reads that stored transcript back out
for a subsequent turn. The model still sees its own full `reasoning` during
the current turn's live processing; the reasoning field is only dead weight
on replay for *future* turns.

---

## Proposed Context Control Knobs

Even with digests and reasoning trimming, the remaining contributors to
context exhaustion — notably the system prompt (~5KB, dominated by full
schema + skill persona) and the number of tool calls per turn — vary widely
across use cases. A local model might need tight guardrails while a
frontier-model user wants maximum flexibility. The following knobs are
proposed to let users tune for their model and workload.

### Knob A — Tools Per Round Cap

**What:** A per-data-source setting (see correction under "Where" below)
that limits the number of `query_database` calls the model can batch in a
single turn. If the model issues more than N tool calls in one response,
the extra calls are rejected with a tool feedback message explaining the
limit.

**Default:** Unbounded (current behavior) for existing users. Suggested
default for new conversations: 5.

**Where:** **Correction:** `maxExplorationRounds` (Knob B) is not a
`conversations` column — it lives in `DataSourceConfig`
(`pkg/models/db_connection.go`), a JSON blob stored on `data_sources`, since
it's a property of the connection's safety profile, not a per-conversation
UI toggle (unlike `context_details`/`summarize`, which genuinely are
conversation-level settings). For consistency, `max_tools_per_round` should
live in the same place: a new `MaxToolsPerRound int` field on
`DataSourceConfig`, defaulting to 0 (unbounded) when absent from existing
configs. Enforced in `runAgenticLoop` during the `query_database` tool-call
processing loop, alongside the existing `maxExplorationRounds` check.

**Rationale:** In the diagnosed NPV turn, the model issued 3 identical
exploratory queries 3 times each (9 tool calls in one round). A cap of 5
would have blocked the 4 duplicates, and a cap of 3 would have cut straight
to the initial batch. Combined with `maxExplorationRounds`, this provides
two-dimensional control: width (tools per round) and depth (rounds).

**Trade-off:** Setting this too low penalizes legitimate batching — a model
that wants to query payments, orders, and customers in parallel is being
reasonable. A cap of 2 would force a 3-query exploration into two separate
rounds, adding an LLM round-trip and making the tool-result digests from
round 1 pollute the context for round 2.

### Knob B — Exploration Round Cap (Already Exists)

**What:** `maxExplorationRounds` — already configurable per data source.
The user confirmed they set this to 5 and it still capped context.

**Note:** The remaining issue after the digest fix is not tool-result bloat
but **system prompt size** — and, as identified later in this document's
"Rethinking Rounds" section, **live in-turn reasoning accumulation** (see
"A Second, Related Finding"). A 5-exploration turn with digests produces
~1.2KB of history, while the system prompt alone is ~5KB. The system prompt
is the larger of the two remaining contributors, but the in-turn reasoning
issue explains why setting this knob to a low value doesn't always prevent
exhaustion symptoms — even with a tight cap, unstripped reasoning from prior
rounds within the same turn accumulates in the live context.

### Knob C — Compact System Prompt Mode

**What:** A per-conversation or per-provider toggle that replaces the full
~5KB system prompt with a ~1KB compact version. The compact version drops:
- Full schema with row counts and per-column type annotations
- Skill/persona text (Janitor, Friendly Assistant, etc.)
- Chart guidance
- Verbose safety/instructions paragraphs

**Compact prompt example:**
```
MySQL — classicmodels
Tables: customers(122), employees(23), offices(7), orderdetails(2996),
  orders(326), payments(273), productlines(7), products(110)
Dialect: backtick quoting, LIMIT 1000, INFORMATION_SCHEMA
You have: query_database (SELECT only, is_exploration flag) and respond_to_user.
```

**Where:** This could be a boolean on `data_sources` ("compact prompts"
for all conversations using this data source) or on `conversations`
(per-conversation override). Data-source-level is probably better — if
you're connecting a local model to your MySQL server, you want compact
prompts for every conversation on that connection.

**Rationale:** The system prompt is the single largest remaining contributor
after the digest + reasoning trim fixes. For a 5-round turn: digest history
~1.2KB + system prompt ~5KB = 6.2KB total. The system prompt alone is ~80%
of that. Cutting it to ~1KB reduces total context to ~2.2KB — small enough
for a local model to breathe.

**Trade-off:** The model loses the full schema with column-level detail
(outside the system prompt — it still sees column names in tool result
digests), persona framing (skills won't apply), and chart instructions.
Users who want the Janitor personality or chart suggestions would keep
this off.

### Knob D — On-Demand Schema Tools (Threshold-Based)

**What:** Instead of always dumping the full schema for every table into the
system prompt, give the model two lightweight introspection tools:

- **`list_tables`** — returns table names + row counts only (no columns).
  A schema-size-independent, tiny payload (~20-30 bytes per table).
- **`describe_table(table_name)`** — returns full column detail (types,
  nullability, PK/FK) for exactly one named table, on demand.

The model calls `list_tables` first, then `describe_table` only for the
tables actually relevant to the user's question, instead of receiving all
table schemas whether relevant or not. This is the same pattern LangChain's
`SQLDatabaseToolkit` and Vanna AI's schema-retrieval step use, adapted to
our existing tool-calling loop rather than requiring embeddings/vector
search.

**Where:** New field on `DataSourceConfig`
(`pkg/models/db_connection.go`), alongside `MaxExplorationRounds`:

```go
SchemaToolThreshold int `json:"schema_tool_threshold,omitempty"` // default 10
```

**Behavior (threshold-gated, not a simple on/off toggle):**

| Table count in schema | Behavior |
|---|---|
| `< SchemaToolThreshold` (default 10) | **Full schema dump** in system prompt, exactly as today. No `list_tables`/`describe_table` tools registered — nothing changes for small schemas. |
| `>= SchemaToolThreshold` | System prompt omits the full schema. `list_tables` and `describe_table` are registered as tools; the model must call them to discover what it needs. |

The threshold is evaluated once per data source, from `len(schema.Tables)`
against `dbConnection.Config.SchemaToolThreshold` (falling back to the
default of 10 if unset/zero), at the same point `buildToolSystemPrompt` and
`buildTools` currently assemble the schema section and tool list.

**Rationale for the threshold default (10):** Below ~10 tables, a full
schema dump is already small (a few KB at most) and costs **one** prompt
inclusion. Forcing on-demand discovery below this size would typically cost
**more** tokens overall — `list_tables` (1 round) + `describe_table` × N
relevant tables (N rounds) is more LLM round-trips than just reading the
full dump once, especially for a small/local model that's already prone to
stumbling on multi-step planning (per the duplicate-query and empty-response
failures already diagnosed in this document). Above ~10 tables, the full
dump grows large enough (and increasingly full of irrelevant tables for any
given question) that on-demand discovery starts winning even with the extra
round-trip cost. `classicmodels` (8 tables) stays on full-dump by default
under this threshold — it would see no behavior change.

**Pros:**
- For large schemas (20+, 50+, 100+ tables — the regime where this pattern
  is proven in tools like Vanna AI and LangChain), the win is large: a
  question touching 2 tables out of 50 sends ~2 table descriptions instead
  of 50.
- No embeddings/vector store required — reuses the existing tool-calling
  loop and `DataSchema`/`GetDataSchema` introspection already in place.
- Zero behavior change for schemas below the threshold — existing small
  schema users (like the diagnosed `classicmodels` conversations) are
  unaffected by default.
- Read-only introspection, no different in kind from `query_database
  (is_exploration: true)` — no read-only invariant risk (§0).

**Cons / Risks:**
- **More round-trips for complex, multi-table questions.** A question
  touching 4 tables costs 1 (`list_tables`) + 4 (`describe_table`) = 5 tool
  calls before any real work starts, versus 0 today. Each round-trip is an
  additional chance for a weak/local model to stumble, given the same class
  of model is already the one hitting context exhaustion and empty
  responses in the first place.
- **The threshold is a heuristic, not a precise cost model.** Table *count*
  is a proxy for schema *size* — a data source with 9 very wide tables
  (50+ columns each) could exceed a 10-table-threshold schema in raw prompt
  bytes. A future refinement could threshold on estimated schema prompt
  size (bytes) instead of table count, but table count is simpler to
  reason about and configure for now.
- **Requires new tool-registration logic** conditional on schema size —
  more branching in `buildTools`/`buildToolSystemPrompt` than the other
  knobs, which are simpler on/off or numeric caps.

**Risk:** Medium. Bigger implementation lift than Knobs A/B/C (new tools,
conditional registration, introspection call wiring), but well-precedented
in the field and gated by a sane default so it only activates where it's
likely to help.

### Knob Combination Summary

| Knob | What It Controls | Existing? | Suggested Default |
|---|---|---|---|
| A — Tools per round | How many tools the model can batch in one turn | No | 5 (0 = unbounded) |
| B — Exploration tool calls | Total exploration `query_database` calls before forced final query | Yes (currently labeled "exploration rounds" in UI) | 5 |
| C — Compact prompts | System prompt verbosity (full vs. compact) | No | Off (full prompts) |
| D — On-demand schema tools | Full schema dump vs. `list_tables`/`describe_table` on demand | No | Threshold = 10 tables |

These four knobs together give the user defense-in-depth against context
exhaustion: tool result size (digest, already implemented), tool call
volume (A + B), per-request overhead (C), and schema payload size at scale
(D).

**Notable gap — duplicate-query detection (deferred):** None of the above
knobs prevent the model from re-issuing identical exploration queries
multiple times within the same turn, as observed in the diagnosed NPV
conversation (the same 3 SQL queries were run 3 times each, 9 tool calls
where 3 would have sufficed). A deduplication check (hash the SQL string
before executing an exploration query; if identical to a prior query in the
same turn, skip execution and feed back "This query was already run earlier
in this turn — it returned N rows") would close this gap. **Deferred** —
Knob A (tools-per-round) limits the damage from duplicate queries, and the
underlying model behavior (redundant exploration) is better addressed
through prompt engineering and better model selection than through a code
enforcement path that may confuse already-struggling local models. Worth
revisiting if redundant exploration remains a top contributor to exhaustion
after Knobs A/B/C/D are in place and the rounds/tool-calls rename is
shipped.

Using the same 5-round example as the Knob C rationale above: a
local-model user running B=5 with C=On brings total turn context to
roughly **~2.2KB** (down from ~6.2KB with full prompts, and far below the
pre-digest ~20KB+ worst case). Adding a low A value (e.g., 3) further
bounds the *width* of any single round, preventing the kind of
3x-duplicated-query blowup seen in the diagnosed conversation, though it
does not by itself change the ~2.2KB estimate above — A and B interact
multiplicatively (rounds × tools-per-round), so the actual worst case
depends on both values together, not their sum. Knob D is orthogonal to
A/B/C — it doesn't help a small schema like `classicmodels` at its default
threshold, but for a user connecting a 50-table production warehouse, it's
the knob that matters most, potentially cutting the system-prompt
contribution by 90%+ for any single-topic question. A frontier-model user
with a small schema keeps all four at their defaults
(unbounded/unbounded/full/10).

---

## Rethinking "Rounds" — A Leftover Convention From the Pre-Tool-Call Protocol

### The Finding

"Round" is a leftover concept from the old, pre-tool-calling discussion
engine, where one LLM call produced exactly one query. The tool-calling
protocol broke that 1:1 mapping (a single LLM response can batch multiple
tool calls), but the "round" label and the exploration-budget mechanism
never fully caught up. Tracing the actual code in `runAgenticLoop`
(`pkg/services/agentic_loop.go`) shows **"round" is currently two different
things wearing the same name**:

1. **The outer LLM-round-trip index** — the `for round := 0; round <
   totalRoundCap; round++` loop (~line 911). One iteration = one
   `ChatCompletionWithTools`/`...Streaming` call. This is used for:
   - `TechDetail.Round` (~line 937) — drives the `[Round N — kind]` label
     shown in tech-details and the UI's exploration trace grouping.
   - `ExplorationResult.Round: round + 1` (~line 1171) — drives the
     human-readable "Round N" numbering in the collapsible "Show N
     intermediate query(ies)" trace shown to end users.
   - `totalRoundCap` (~line 909) — the hard safety ceiling on LLM
     round-trips regardless of budget state.

2. **The exploration tool-call budget** — `explorationRoundsUsed` (~line
   907), incremented **once per exploration `query_database` tool call**
   (~lines 1085, 1118, 1164), not once per outer-loop iteration. This is
   checked against `maxExplorationRounds` (~line 1071) to decide when to
   force a final query. Despite the variable name ("RoundsUsed") and the
   user-facing setting name ("exploration rounds"), **this is already a
   tool-call counter, not a round counter.** A single LLM response batching
   3 tool calls increments it by 3 in one round-trip.

**Consequence:** the user-reported symptom — setting `maxExplorationRounds`
to 5 and still hitting context exhaustion — is explained by this
mismatch. If a model batches 3 duplicate tool calls per round-trip (as
observed in the diagnosed NPV conversation), the budget of "5" is consumed
in 2 round-trips, not 5, while the UI and mental model both imply "5 LLM
turns of exploration." The setting's *name* promises round-level control;
its *implementation* delivers tool-call-level control. This is precisely
the naming/semantics drift you'd expect from a convention inherited from a
protocol where the two concepts were indistinguishable.

### A Second, Related Finding: Live In-Turn Reasoning Is Not Trimmed

The `stripReasoningFromArgs` fix (Change 2, above) only trims replayed
`reasoning` arguments in `buildToolMessages` — i.e., only for **historical**
turns replayed on *future* user messages. It does **not** touch `messages`
within the **live, in-progress** turn. As `runAgenticLoop`'s outer loop
appends each round's response via `messages = append(messages, *response)`
(~line 992), every prior round's full, unstripped tool-call reasoning
(400–700+ chars per call, per the original diagnosis) accumulates in
context for the remainder of that same turn — because the live loop needs
that state to keep the model's train of thought coherent across rounds.

This is very likely the actual mechanism behind "5 rounds still exhausted
context": not round *count*, but reasoning-field accumulation **within a
single live turn**, compounding round over round regardless of what unit
the cap is measured in. Renaming/refactoring "rounds" to "tool calls" does
**not** fix this by itself — it's a separate, additive problem that would
need its own fix (e.g., trim reasoning from `messages` immediately after a
round's tool result is recorded, once that round's own reasoning is no
longer needed for the model's next decision — this needs care, since some
models may reference their own prior reasoning when deciding a follow-up
query, so trimming here carries more behavior risk than trimming at
historical-replay time).

### Should We Remove "Rounds" and Cap Only Tool Calls?

**Recommendation: partially.** Split the two things "round" currently
conflates, rather than eliminating rounds outright:

- **Keep** the outer LLM-round-trip index as a **display/audit concept**
  (tech-details grouping, exploration-trace numbering, latency/cost
  accounting per round-trip, and the `totalRoundCap` hard safety ceiling).
  This usage is legitimate, low-risk, and orthogonal to the budget problem
  — humans genuinely benefit from seeing "this happened during LLM call
  #2," and nothing about that requires renaming.
- **Rename/reframe** the *budget* mechanism to match what it already is:
  a tool-call counter. `explorationRoundsUsed` → something like
  `explorationToolCallsUsed`; `maxExplorationRounds` →
  `maxExplorationToolCalls` (config field, UI label, and default carried
  over unchanged — this is a rename for honesty, not a behavior change).
  This also unifies naturally with **Knob A** (tools-per-round cap,
  proposed above) — both become dials on the same underlying concept (tool
  call volume), just at different granularities (per-round-batch vs.
  per-turn-total), instead of one confusingly-named "rounds" setting
  secretly counting a different unit than its name implies.

### Risk Analysis

**Scope of "round" in the codebase** (grep-verified, `pkg/services/agentic_loop.go`):
approximately 25 references to `round`/`Round` across variable names, the
`roundKind` type (5 enum values: exploration/error_retry/chart/final/other),
the outer loop, `TechDetail.Round`, `ExplorationResult.Round`, and 15+
call sites of `logRound(...)`. This sounds large, but tracing every
reference (done above) shows they cluster into exactly the two categories
described — there is no third, hidden meaning of "round" lurking elsewhere.

| Risk Category | Finding | Severity |
|---|---|---|
| **Frontend coupling** | `ConversationView.svelte` reads `message.payload.kind`, `.duration_ms`, `.request.message_count`, `.response.tool_calls`, `.response.finish_reason`, `.sql`, `.stream.chunk_count` from parsed `TechDetail` JSON. **It never reads the numeric `.round` field directly in rendering logic** — the round number only appears pre-baked into the `message.content` string (`[Round N — kind]`), built server-side. | **Low.** The frontend has no structural dependency on round semantics; changing what the number *means* (round-trip index, unchanged) or renaming the *budget* field (`explorationRoundsUsed` → tool-call-count naming) touches no frontend code. |
| **Historical data compatibility** | `TechDetail` JSON (versioned, `Version: 1`) is persisted per-message in `conversation_messages.metadata` and replayed indefinitely (bounded only by `maxContextMessages`/history retention). Existing stored `TechDetail` blobs have `"round": N` with the *current* dual meaning baked in — old messages don't retroactively become inconsistent, since we're not proposing to remove the outer round-trip index, only to stop conflating it with the tool-call budget going forward. | **Low.** No schema migration needed if the outer round-trip index is kept as-is. If the budget field were renamed in the JSON schema (e.g., adding a new `tool_call_index` field), old `TechDetail` blobs simply lack the new field — additive, not breaking, consistent with the spirit of `AGENT_READ_FIRST.md` §3.2's "only add, never rename or drop" discipline (applied here to the JSON-blob analog of a table column). |
| **`roundKind` enum semantics** | The 5 `roundKind` values (`exploration`, `error_retry`, `chart`, `final`, `other`) classify *what a round-trip did*, not how many tool calls it contained. These are orthogonal to the rename — a round-trip can still be classified as "exploration" whether it contained 1 or 5 tool calls. | **None.** `roundKind` requires no change under this proposal. |
| **`totalRoundCap` hard ceiling** | `totalRoundCap := maxExplorationRounds + maxErrorRetries + 4` (~line 909) sizes the *outer loop's* iteration cap using the (renamed) budget values as inputs. This remains a safety net independent of the rename — it bounds LLM round-trips, which is a distinct and still-necessary safeguard (infinite-loop protection) even after the budget itself is correctly understood as tool-call-based. | **Low.** Arithmetic is unaffected by a rename; only variable/field names change, not the formula. |
| **User-facing setting rename** | The data-source config setting is currently labeled "exploration rounds" in the UI (`SettingsView.svelte` / data source config forms) and in `DataSourceConfig.MaxExplorationRounds`. Renaming to "exploration tool calls" or similar changes user-visible copy and the JSON key in `DataSourceConfig`. | **Medium.** This is the one genuinely breaking-adjacent change: existing saved data-source configs have `"max_exploration_rounds"` as a JSON key inside the config blob. A rename requires either (a) a migration step that copies the old key to a new key name at read time, or (b) keeping the JSON key stable while only changing the Go field name and UI label (decoupling wire format from display name) — option (b) avoids any migration risk entirely and is the safer default. |
| **Mid-conversation behavior change** | If the budget semantics are clarified without also addressing the live in-turn reasoning accumulation (the second finding above), some conversations may still exhaust context at the same rate as today, and a user could reasonably conclude "the fix didn't work." | **Medium (expectation-setting risk, not a code risk).** This isn't a risk of the rename itself, but a risk of shipping it *without* also documenting/fixing the in-turn reasoning issue — the rename alone does not resolve the originally reported symptom, only makes the existing mechanism's behavior legible and correctly named. |

### Mitigation Plan

1. **Decouple wire format from display name.** Keep `DataSourceConfig`'s
   JSON key as `max_exploration_rounds` (no migration needed) while
   renaming only the Go field's *documentation comment* and the UI label
   to reflect tool-call semantics (e.g., label: "Max exploration tool
   calls" with a tooltip explaining one LLM turn may include several).
   This eliminates the Medium-severity config migration risk entirely by
   never touching the persisted key.
2. **Rename internal variables only** (`explorationRoundsUsed` and similar
   in `agentic_loop.go`) — these are not serialized anywhere (confirmed:
   not part of `TechDetail`, not part of `DataSourceConfig`), so renaming
   them is a pure, zero-risk internal refactor.
3. **Leave `TechDetail.Round`, `roundKind`, `ExplorationResult.Round`, and
   `totalRoundCap` untouched.** These represent the legitimate
   round-trip-index and safety-ceiling concepts and require no change
   under this proposal — only the *budget* semantics are being clarified.
4. **Ship the live in-turn reasoning fix as a separate, explicitly-scoped
   change**, not bundled with the rename, so its distinct risk (potential
   behavior change to models that reference their own prior-round
   reasoning) can be evaluated and tested independently. Document it with
   its own risk/reward entry in `RISK_ANALYSIS_LOG.md` when implemented.
5. **Update this document and any UI tooltips together** so the
   user-facing mental model ("how many tool calls can the model make
   during exploration") matches the enforced behavior, closing the gap
   that caused the original confusing symptom report.

### Decision Status

**Direction agreed, not yet implemented.** The user has confirmed this is
the right direction. Recommended sequencing: (1) the wire-format-safe
rename (Mitigation #1–#3, low risk, clarifies existing behavior with zero
migration cost) should ship first; (2) the live in-turn reasoning trim
(Mitigation #4) should follow as an independently-reviewed change, since it
carries the higher behavioral risk of the two and directly targets the
symptom the user actually reported.

---

## Decisions

| Date | Decision | Rationale |
|---|---|---|
| 2026-08-10 | Problem documented. | Initial observation of context exhaustion after a 9-query NPV analysis turn. |
| 2026-08-10 | **Option 1 implemented.** Digest format replaces markdown table in `formatToolResult`; `stripReasoningFromArgs` trims replayed reasoning in `buildToolMessages`. | Changes in `agentic_loop.go`. Verified build. |
| 2026-08-10 | **Knobs A/B/C proposed, not yet implemented.** Tools-per-round cap and compact system prompt mode identified as the next highest-value controls; exploration round cap already exists. | Documented in "Proposed Context Control Knobs" above. Awaiting decision on whether to implement. |
| 2026-08-10 | **Knob D proposed** — on-demand schema tools (`list_tables`/`describe_table`), gated by a `SchemaToolThreshold` data-source setting defaulting to 10 tables. Below threshold: full schema dump (no change). At/above threshold: schema tools replace the dump. | Inspired by Vanna AI / LangChain `SQLDatabaseToolkit` schema-linking pattern. Targets large schemas where the full dump is the dominant context cost; explicitly does not change behavior for small schemas like `classicmodels`. Awaiting decision on whether to implement. |
| 2026-08-10 | **"Rounds" rethink: direction agreed, not yet implemented.** Confirmed via code trace that "round" conflates two concepts (LLM round-trip index vs. tool-call budget). Plan: rename the budget internally + in UI labels to reflect tool-call semantics, while keeping the JSON config key (`max_exploration_rounds`) stable to avoid migration risk; leave `TechDetail.Round`/`roundKind`/`totalRoundCap` unchanged as legitimate round-trip concepts. Separately identified live in-turn reasoning accumulation as the likely real cause of "5 rounds still exhausted context" — to be fixed independently. | See "Rethinking 'Rounds'" section above for full risk analysis and mitigation plan. |

---

*This document was created on 2026-08-10 after observing a user's
conversation bricked by context exhaustion following a 9-query NPV analysis
turn. Updated the same day with a review pass correcting stale line
numbers/constants against the shipped implementation, and with a proposed
knobs section for further discussion.*
