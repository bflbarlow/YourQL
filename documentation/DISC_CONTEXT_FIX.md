# DISC_CONTEXT_FIX.md — Actionable Plan: Fixing Context Exhaustion

> Companion to `AGENT_READ_FIRST.md` and `DISCUSSION_CONTEXT_ISSUES.md`.
> That document is the **discussion record** (why the problem happens, what
> options were weighed, the reasoning trail). **This document is the
> execution plan** — concrete, ordered, actionable tasks with file
> references, acceptance criteria, and test steps. If you're implementing
> rather than discussing, start here.
>
> **The underlying issue in one sentence:** models answering a single
> complex question can generate so much context (tool result tables, tool
> call reasoning, system prompt) that little to no room is left for any
> further question in the same conversation — the conversation becomes
> unusable ("bricked") without the user realizing why.
>
> **Status legend:** ✅ Shipped · 🔜 Next up · 🧊 Proposed, not scheduled

---

## 0. Quick Status Overview

| # | Item | Status | Effort | Impact |
|---|---|---|---|---|
| 1 | Digest tool results instead of full tables | ✅ Shipped | — | High |
| 2 | Strip replayed reasoning (historical turns) | ✅ Shipped | — | Medium |
| 3 | Rename exploration budget (rounds → tool calls) | ✅ Shipped | — | Low (clarity only) |
| 4 | Strip reasoning from **live in-turn** messages | ↩️ Reverted | Small | **Regressive** — caused model confusion on multi-round reasoning |
| 5 | Tools-per-round cap (Knob A) | ✅ Shipped | Small | Medium |
| 6 | Compact system prompt mode (Knob C) | ✅ Shipped | Medium | High (dominant remaining cost) |
| 7 | On-demand schema tools (Knob D) | ✅ Shipped | Large | High, but only for 10+ table schemas |
| 8 | Auto-retry on empty response (safety net) | ↩️ Reverted | Small | **Regressive** — stripped context on non-exhaustion empty responses, causing model to claim no data existed |
| 9 | Duplicate-query detection | 🧊 Deferred | Small | Medium |
| 10 | Fix forced extra round-trip after final query (VizEnabled) | ✅ Shipped | Small | **High** — eliminates wasted LLM call on every final answer |

**Recommended build order:** 5 → 6 → 7 → 9. 10 was shipped as a bugfix independent of these.
each section, summarized: #5 and #6 are additive, default-off settings
that compound safely; #7 requires schema-sizing awareness but is gated by
a global default threshold; #9 is explicitly deferred per prior
discussion.

**Reverted items (#4, #8):** Both were silent behavior changes that
activated on every conversation without user opt-in. #4 stripped the
model's own prior-round reasoning text from the live context, which broke
coherence across rounds for models that chain reasoning. #8 confused
transient model flakiness (occasional empty responses on a 120K-context
model with no real context pressure) for genuine context exhaustion, and
the stripped-on-retry context made the model claim prior data didn't
exist. Both are documented here and could be reconsidered as
**opt-in settings** rather than silent defaults.

## 0.1 ✅ Shipped: Fix Forced Extra Round-Trip After Final Query

**Discovered:** 2026-08-10 during follow-up empty-response diagnosis.
**Root cause:** the `VizEnabled` continuation at `agentic_loop.go:~1305`
always forced a second LLM round-trip after every successful final query
when charts were enabled, regardless of whether the model had any chart
intent. The model, having already delivered a complete answer, reliably
produced near-empty responses (9 chars) to this forced round. These empty
responses were silently swallowed by the `pendingFinal` fallback, so they
didn't break the answer — but they wasted one full LLM call on every
single finalized question, and conditioned the model to produce empty
responses in this conversational pattern. On subsequent **real** follow-up
questions (where `pendingFinal` was nil and no fallback existed), the same
empty-response pattern recurred and surfaced directly to the user as the
"I received an incomplete response" clarification.

**Fix:** Instead of unconditionally `continue`-ing for an extra round when
`VizEnabled` is true, scan the remaining tool calls in the **same** batched
response for a `render_chart` call. If one exists, `continue` to process it
inline (the existing tool-call loop handles it on the next iteration). If
no chart call is present in the batch, `return` immediately — no wasted
round-trip. This preserves chart support for batched `render_chart` +
`query_database` responses while eliminating the forced empty round for the
common case (no chart requested).

**Files changed:** `pkg/services/agentic_loop.go` — one conditional block.
**Risk:** Low — purely eliminating a wasted code path. The chart round is
still available if the model explicitly requests it alongside its final
query. No behavior change for non-VizEnabled conversations.

---

## 1. ✅ Already Shipped (Reference Only)

These are done. Listed here so the rest of this document can build on top
of them without re-explaining. Full narrative and risk analysis is in
`DISCUSSION_CONTEXT_ISSUES.md`.

### 1.1 Tool result digest (`formatToolResult`)

**File:** `pkg/services/agentic_loop.go:237`

Produces, instead of a markdown table:
```
Query returned N row(s).
Columns: col1, col2, ...
Sample (3 of N):
  val1|val2|...
Column stats:
  col1: avg=X.XX min=Y.YY max=Z.ZZ
```
Constants: `toolResultSampleRows = 3` (`agentic_loop.go:233`),
`toolResultMaxCellChars = 200` (`agentic_loop.go:234`). Helper functions:
`detectNumericColumns` (`:300`), `computeToolResultColStats` (`:346`).

### 1.2 Reasoning strip on historical replay (`stripReasoningFromArgs`)

**File:** `pkg/services/agentic_loop.go:603`, used in `buildToolMessages`
(`:541`, call site `:574`).

Removes the `"reasoning"` key from a tool call's JSON arguments before
replaying it in a **future** turn's message history. Does not touch the
live in-progress turn (see §2 below — this is the gap).

### 1.3 Rounds → tool-calls rename

**Files:** `pkg/models/db_connection.go:39-48` (doc comment added to
`MaxExplorationRounds`, JSON key `max_exploration_rounds` left unchanged),
`pkg/services/agentic_loop.go` (internal variable renamed
`explorationRoundsUsed` → `explorationToolCallsUsed`, 7 occurrences at
lines 907, 1003, 1071, 1085, 1118, 1164, 1230).

No behavior change. No migration. Purely a clarity fix so the code's
naming stops implying "N round-trips" when it actually enforces "N tool
calls, however they're batched across round-trips."

---

## 2. ↩️ REVERTED: Strip Reasoning From the Live In-Turn Loop

**Status: Reverted after shipping.** The implementation was removed because
it silently stripped the model's own prior-round reasoning from the live
context on every conversation, and this caused cross-round coherence
failures on a 120K-context model that chains its own reasoning. The
section below is kept as a record of the analysis and implementation
approach — it remains a legitimate optimization target, but should be
considered for an **opt-in setting** (e.g., per-data-source or
per-conversation) rather than a silent default, and must be tested against
models that demonstratively rely on their own reasoning-text chaining.

---

### Why This Is First (Historical Rationale From When It Was Live)

This is very likely the actual explanation for "I set exploration to 5 and
it still exhausted context." §1.2 only strips reasoning when a **past**
turn is replayed on a **future** message. Within a single live turn, as
`runAgenticLoop`'s outer loop appends each round's response via
`messages = append(messages, *response)` (`agentic_loop.go:992`), every
prior round's full, unstripped tool-call `reasoning` (400–700+ chars per
call, per the original diagnosis) accumulates in the **live** `messages`
slice for the rest of that same turn. Five exploration rounds with verbose
reasoning can add 2–3.5KB on top of everything else, entirely within one
turn, regardless of how the exploration budget is named or counted.

### The Constraint

Some models reference their own prior reasoning when deciding a follow-up
query within the same turn ("as I noted above, I should now check..."). Trim
too aggressively or too early and you risk degrading the model's ability to
reason coherently across rounds — this is a real behavior risk, not just a
context-size one. **Do not bundle this with §1's changes; it needs its own
test pass.**

### Implementation Plan

**File:** `pkg/services/agentic_loop.go`, inside `runAgenticLoop`'s main
loop (`for round := 0; round < totalRoundCap; round++`, starts `:911`).

1. **Add a helper, distinct from `stripReasoningFromArgs`**, that strips
   reasoning from a live `*ChatMessage`'s tool calls, for use *within* the
   loop rather than at replay time:

   ```go
   // stripReasoningFromLiveToolCalls removes the "reasoning" field from a
   // ChatMessage's tool calls after they've been recorded in the transcript
   // for this round, so the *next* round's LLM call doesn't re-pay the
   // token cost of this round's reasoning. Only affects the in-memory
   // messages slice for the remainder of the current turn — has no effect
   // on future turns (buildToolMessages/stripReasoningFromArgs already
   // handles those separately).
   func stripReasoningFromLiveToolCalls(msg *ChatMessage) {
       for i := range msg.ToolCalls {
           msg.ToolCalls[i].Function.Arguments = stripReasoningFromArgs(msg.ToolCalls[i].Function.Arguments)
       }
   }
   ```

   This reuses the existing `stripReasoningFromArgs` (already
   fail-safe: falls back to the original string on unmarshal/marshal
   failure), so no new failure mode is introduced.

2. **Critical ordering constraint, found during implementation review:**
   `logRound` (defined at `agentic_loop.go:940`) is called from every
   branch of the tool-call switch **after** `messages = append(messages,
   *response)` (line 982) has already run for that same round (verify via
   `grep -n "logRound(kind)"` — every call site is at or after line 1046,
   well after line 982). `logRound` captures `td.Request.RawMessages` via
   `json.Marshal(messages)` (`:951`) — i.e., it dumps the *live* `messages`
   slice, which by that point already includes the current round's own
   just-appended response. **If reasoning is stripped immediately after
   line 982 (in the same iteration), `logRound` for that same round would
   capture its own tool-call reasoning as already-stripped** — breaking
   the acceptance criterion that tech-details must still show full
   reasoning for the round that generated it.

   **Correct insertion point:** strip at the **top of the next loop
   iteration**, not immediately after this round's append. Concretely,
   right after the `for round := 0; round < totalRoundCap; round++ {`
   line (`:911`) and the existing cancellation check, before the LLM call
   is made for the new round, strip reasoning from the **previously**
   appended assistant message (the last one currently in `messages`, i.e.
   the one appended at the end of the *prior* iteration):

   ```go
   for round := 0; round < totalRoundCap; round++ {
       if err := ctx.Err(); err != nil {
           return err
       }
       // Strip reasoning from the previous round's tool calls now that
       // logRound has already captured it in full for that round's
       // tech-detail entry. Only touches the live in-progress turn.
       if n := len(messages); n > 0 && messages[n-1].Role == "assistant" {
           stripReasoningFromLiveToolCalls(&messages[n-1])
       }
       ...
   ```

   This guarantees: (a) `logRound` for round N always sees round N's own
   full reasoning, since the strip for round N happens at the *start* of
   round N+1, strictly after round N's `logRound` call; (b) by the time
   round N+1's LLM call is actually made, round N's reasoning is already
   gone from what gets sent.

3. **Do NOT strip reasoning from the round that produced the FINAL answer**
   (`is_exploration: false`) or from `respond_to_user`/`render_chart` calls
   — those terminate the loop via `return` before another iteration starts,
   so the top-of-loop strip point above never runs for them anyway (no
   extra guard needed — this falls out naturally from the insertion point
   chosen in step 2, since a `return` skips the next iteration entirely).

### Acceptance Criteria

- A multi-round exploration turn (4-5 rounds, each with verbose reasoning)
  should show a **measurably smaller** live `messages` payload in the tech
  details "📥 Request messages" panel for round 4/5 compared to before this
  change (compare `message_count` character length before/after; reasoning
  fields should be absent from tool calls belonging to rounds 1-3 by the
  time round 4/5's request is captured).
- Tech-details for the round that generated the (now-stripped) reasoning
  should still show it in full via `TechDetail.Response.ToolCalls[].Arguments`
  (captured by `logRound` before the strip happens) — verify the strip
  doesn't retroactively erase what was already logged for that round.
- Manually re-run the diagnosed NPV conversation (5 exploration rounds,
  verbose reasoning) end-to-end and confirm the model can still complete a
  coherent final answer — i.e., verify no regression in the model's ability
  to reason across rounds once its own prior reasoning is no longer visible
  starting from round 2 onward.

### Risk / Reward Entry Required

Per `AGENT_READ_FIRST.md` §4.2, this touches `discussion_engine.go`/
`agentic_loop.go` core query pipeline logic and modifies live model context
— log a risk/reward entry to `RISK_ANALYSIS_LOG.md` before merging, using
the template in §4.4. Key risk category: **Answer accuracy** (could degrade
multi-round reasoning coherence for models that lean on their own prior
reasoning text). Mitigation: only strip *completed* rounds' reasoning, never
the current round's own reasoning-in-progress; test with at least 2
providers per §5.2 of the charter.

---

## 3. 🔜 NEXT: Knob A — Tools-Per-Round Cap

### What

Limit how many `query_database` tool calls the model can batch into a
**single** LLM response. If exceeded, reject the excess calls with
feedback; don't execute them.

### Why Second

Directly addresses the diagnosed failure mode: the model issued the same 3
exploration queries 3 times each — 9 tool calls in what the UI grouped as
one "round." A tools-per-round cap bounds the *width* of any single
round-trip, independent of the *depth* (total exploration budget, already
covered by the existing/renamed setting from §1.3).

### Implementation Plan

1. **New field on `DataSourceConfig`** (`pkg/models/db_connection.go`,
   alongside `MaxExplorationRounds` at line ~48):

   ```go
   // MaxToolsPerRound limits how many query_database tool calls the model
   // may batch into a single LLM response. 0 means unbounded (default,
   // preserves current behavior for existing configs). Excess calls in a
   // response are rejected with tool feedback explaining the limit; the
   // model must spread them across additional rounds instead.
   MaxToolsPerRound int `json:"max_tools_per_round,omitempty"`
   ```

2. **Read the config value** in `discussion_engine.go`, alongside where
   `MaxExplorationRounds` is currently read into `maxRounds` (`:132-139`),
   and pass it as a new parameter to `runAgenticLoop`
   (signature at `agentic_loop.go:891`, alongside `maxExplorationRounds
   int`).

3. **Enforce in `runAgenticLoop`**, in the tool-call processing loop
   (`for _, tc := range response.ToolCalls` — locate exact current line via
   `grep -n "for _, tc := range response.ToolCalls"` since line numbers
   shift). Before processing the `query_database` case: count how many
   `query_database` tool calls are in `response.ToolCalls` for **this**
   round; if `maxToolsPerRound > 0` and the count exceeds it, for each
   excess call beyond the limit, append a tool message:

   ```go
   messages = append(messages, ChatMessage{
       Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name,
       Content: fmt.Sprintf(cfg.ResponseTooManyToolsPerRound, maxToolsPerRound),
   })
   ```

   and `continue` without executing the query. Only the first
   `maxToolsPerRound` `query_database` calls in the response actually run.

4. **New config message** in `pkg/services/agent_loop_config.go`
   (follow the existing pattern used for `ResponseSummarizationRequiresFinalQuery`,
   `ResponseQueryErrorRequiresRetry`, etc.):

   ```go
   "response.too_many_tools_per_round": "You issued more query_database calls in this single response than allowed (max %d per round). Only the first %d were executed. Spread additional queries across further rounds.",
   ```

   Add corresponding field to `models.AgentLoopConfig` and
   `AgentLoopConfigFields()` metadata, same pattern as prior additions.

5. **UI (`frontend/src/SettingsView.svelte`)** — the existing
   `max_exploration_rounds` field is threaded through **six** touchpoints
   in this file; the new `max_tools_per_round` field needs the same six,
   verified by grep against the current file:

   1. **Default object literal** (`:383`) — the form's initial
      `$state({...})` block. Add `max_tools_per_round: 0,` alongside the
      existing `max_exploration_rounds: 2,` line.
   2. **"New connection" reset literal** (`:567`) — a duplicate default
      object inside the handler that resets the form for a new data
      source. Add the same `max_tools_per_round: 0,` line here too (this
      duplication already exists for every other field — not introduced
      by this change).
   3. **`openSourceDetail`'s local default object** (`:595`) — a third
      duplicate of the same defaults, used when opening an existing
      connection's detail view before parsing its saved config over top.
      Add `max_tools_per_round: 0` here as well.
   4. **Parse-through line** (`:613`, pattern:
      `if (parsed.max_exploration_rounds) config.max_exploration_rounds = parsed.max_exploration_rounds`)
      — add `if (parsed.max_tools_per_round) config.max_tools_per_round = parsed.max_tools_per_round`
      immediately after the existing `max_exploration_rounds` line. Note:
      this pattern uses truthy-check (`if (parsed.X)`), which means a
      saved value of `0` will NOT override the default — harmless here
      since `0` IS the intended default (unbounded), but worth being aware
      of if this pattern is reused for a field where `0` is a meaningful,
      non-default saved value.
   5. **Save payload** (`:687`, inside the save handler's `const config = {...}`
      object) — add `max_tools_per_round: dbDetailConfig.max_tools_per_round,`
      alongside the existing `max_exploration_rounds: dbDetailConfig.max_exploration_rounds,`
      line, so the value actually gets persisted to `data_sources.config`.
   6. **Form markup** (`:1317`, inside the "Exploration Settings" `db-section`,
      immediately after the existing `<label>Max Exploration Rounds</label>`
      `<input type="number">` block) — add a matching field:
      ```svelte
      <div class="form-group">
        <label>Max Tools Per Round</label>
        <input type="number" bind:value={dbDetailConfig.max_tools_per_round} placeholder="0 = unbounded" />
        <p class="hint">Limits how many query_database calls the model can batch into a single response. 0 = unbounded (default).</p>
      </div>
      ```
      Placed inside the same `form-grid` as "Max Exploration Rounds" and
      "Safety Mode" so it reads as a related, adjacent control.

### Acceptance Criteria

- Set `MaxToolsPerRound = 2` on a test data source. Ask a question that
  historically triggers 3+ batched exploration calls. Confirm only 2
  execute; the 3rd+ receive the feedback message; the model's next round
  either retries the remaining query or proceeds with what it has.
- Set `MaxToolsPerRound = 0` (default). Confirm behavior is identical to
  today — unbounded batching still works, no regression for existing users.
- Existing data-source configs without this field set (absent from JSON)
  parse correctly and default to 0/unbounded — confirm via `ParseConfig()`.

### Risk / Reward Entry Required

Low risk per `AGENT_READ_FIRST.md` §4.3 (additive, no data source
interaction change, isolated and testable) — but still log a brief entry
to `RISK_ANALYSIS_LOG.md` per §4.2, since it does touch `agentic_loop.go`'s
core query pipeline.

---

## 4. 🔜 NEXT: Knob C — Compact System Prompt Mode

### What

A toggle that swaps the full system prompt (~5KB: complete schema with row
counts and column types, full skill/persona text, chart guidance, verbose
safety paragraphs) for a compact version (~1KB: table names + row counts
only, one-line dialect rule, terse tool descriptions, no persona/chart
text).

### Why Third

Per the arithmetic in `DISCUSSION_CONTEXT_ISSUES.md`, the system prompt is
the single largest remaining contributor to per-request context after §1
and §2's fixes land — roughly 80% of a typical 5-round turn's total. This
is the highest-leverage remaining lever that doesn't require new tools or
schema-size branching (unlike §6/Knob D).

### Implementation Plan

1. **New field on `DataSourceConfig`** (`pkg/models/db_connection.go`):

   ```go
   // CompactPrompts, when true, replaces the full system prompt (complete
   // schema with types/row-counts, full skill persona text, chart
   // guidance, verbose instructions) with a compact version (table names +
   // row counts only, terse tool/dialect notes, no persona/chart text).
   // Intended for small/local models prone to context exhaustion. Default
   // false preserves current behavior.
   CompactPrompts bool `json:"compact_prompts,omitempty"`
   ```

2. **Branch in `buildToolSystemPrompt`** (`agentic_loop.go:620`). At the
   top of the function, check `dbConnection.Config.CompactPrompts` (parse
   config as already done elsewhere in this function/its callers). If true,
   build and return a minimal prompt instead of proceeding through the
   existing full-assembly logic:

   ```go
   if cfg, err := dbConnection.ParseConfig(); err == nil && cfg.CompactPrompts {
       return buildCompactSystemPrompt(schema, dbConnection)
   }
   ```

3. **New function `buildCompactSystemPrompt`**, same file, producing
   something in the shape of:

   ```
   {Dialect} — {database name}
   Tables: {name}({row_count}), {name}({row_count}), ...
   Dialect: {one-line quoting/LIMIT/introspection rule}
   Tools: query_database (SELECT only, is_exploration flag), respond_to_user.
   {only if vizEnabled: one line mentioning render_chart}
   ```

   No skill/persona text, no chart guidance paragraphs, no verbose
   exploration-safety prose — keep only the single most safety-critical
   line (read-only enforcement is still handled by `validateExplorationQuery`/
   `executeSQLWithMode` in code, not by prompt text alone, per
   `AGENT_READ_FIRST.md` §1.4 — the compact prompt does not weaken this;
   it just says less about it).

4. **Also skip skills injection** when compact mode is on — check
   wherever `skillsContent` is currently appended to the full prompt (via
   `formatSkillsContext` per earlier work in this codebase) and don't call
   it in the compact path. This is intentional per the trade-off already
   documented: users who want persona/chart framing keep compact mode off.

5. **UI (`frontend/src/SettingsView.svelte`)** — same six-touchpoint
   pattern as Knob A's `max_tools_per_round` (§3, step 5), applied to a
   boolean field instead of a numeric one:

   1. **Default object literal** (`:383`) — add `compact_prompts: false,`.
   2. **"New connection" reset literal** (`:567`) — add `compact_prompts: false,`.
   3. **`openSourceDetail`'s local default object** (`:595`) — add
      `compact_prompts: false`.
   4. **Parse-through line** (`:613`) — add
      `if (typeof parsed.compact_prompts === 'boolean') config.compact_prompts = parsed.compact_prompts`
      (use the boolean-safe pattern already used for `exploration_allowed`
      at this same location, NOT the truthy-check pattern used for numeric
      fields — a truthy check on a boolean would incorrectly skip
      `false` values read from a saved config, since `false` is falsy).
   5. **Save payload** (`:687`) — add
      `compact_prompts: dbDetailConfig.compact_prompts,`.
   6. **Form markup** (`:1317` area, inside "Exploration Settings" or a new
      adjacent `db-section` — arguably deserves its own section since it
      affects the whole system prompt, not just exploration behavior):
      ```svelte
      <div class="form-group">
        <label>
          <input type="checkbox" bind:checked={dbDetailConfig.compact_prompts} />
          Compact System Prompt
        </label>
        <p class="hint">Reduces prompt size for local/small models prone to
          context exhaustion. Disables skill personas and chart suggestions
          for conversations on this connection.</p>
      </div>
      ```
      Follows the existing checkbox pattern used for "Allow Exploration
      Queries" in the same file (`:1310`-ish, `<input type="checkbox"
      bind:checked={dbDetailConfig.exploration_allowed} />`).

### Acceptance Criteria

- Toggle on: system prompt for `classicmodels` (8 tables) should be
  measurably smaller than today's, and should NOT include any of the
  active skill's persona text even if a skill is enabled on the
  conversation.
- Toggle off (default): byte-for-byte identical system prompt to current
  behavior — zero regression for existing users who never touch this
  setting.
- With compact mode on, confirm the model still enforces read-only query
  behavior (it does, structurally, via code-level validation — verify this
  is not accidentally weakened by anything removed from the prompt text).

### Risk / Reward Entry Required

Per `AGENT_READ_FIRST.md` §4.2 (touches system prompt construction) — a
full risk/reward entry is **required**, not optional, before this ships.
Key point to document: this must NOT be framed as a safety feature — the
read-only invariant (§0) is enforced in code (`validateExplorationQuery`,
`executeSQLWithMode`), not by prompt verbosity, so removing safety prose
from the prompt is a UX/context-size trade-off only, never a safety
regression. State this explicitly in the risk entry to preempt any future
reviewer wondering if compact mode is "less safe."

---

## 5. ↩️ REVERTED: Auto-Retry on Empty Response (Safety Net)

**Status: Reverted after shipping.** The implementation was removed because
it blindly retried all empty responses with a stripped context, regardless
of whether the model was actually suffering from context exhaustion. On a
120K-context model with plenty of headroom, occasional empty responses are
transient flakiness, not exhaustion, and the stripped retry context caused
the model to legitimately claim "I don't see any prior data" — a worse
outcome than the original clarification message. The section below is kept
as a record. If reconsidered, the retry should be gated: only fire when
`len(messages) > 20 || finish_reason == "length"` (genuine evidence of
context pressure) rather than on every empty response.

---

### What

When the model returns an empty response (the `ResponseEmptyTruncated`
path — `agentic_loop.go:~885`, verify exact line), instead of immediately
surfacing "I received an incomplete response," make **one** retry attempt
first with a stripped-down `messages` slice: system prompt + latest user
message only, no prior tool results/history from this turn.

### Why Include This Regardless of §2/§3/§4

Even after the above fixes land, some combination of factors (very long
user question, very verbose model reasoning even after trimming, unusually
large single query result before digesting) could still occasionally
exhaust context. This is a cheap, reactive safety net that turns "the
conversation is now bricked" into "one extra round-trip, then it works" —
it costs nothing when the primary fixes are sufficient (never triggers) and
saves the user from restarting the whole conversation when they aren't.

### Implementation Plan

1. **Locate the current terminal branch:**

   ```go
   // Empty response — truncated or malformed
   if len(response.ToolCalls) == 0 {
       ...
       return handleClarification(query, LLMResponse{
           Action:                "clarification",
           ClarificationQuestion: cfg.ResponseEmptyTruncated,
       }, conversation.ID)
   }
   ```

2. **Add a one-shot retry flag** (local var in `runAgenticLoop`, e.g.
   `emptyResponseRetried bool`, initialized false before the outer loop).

3. **On first empty response**, if `!emptyResponseRetried`, set it to
   true, rebuild a minimal `messages` slice (system prompt message +
   the original `userMessage` as a fresh user message, dropping
   everything else accumulated so far in this turn), and `continue` the
   outer loop instead of returning immediately.

4. **On a second consecutive empty response** (flag already true), fall
   through to the existing `handleClarification` path — don't retry
   indefinitely.

5. Ensure `totalRoundCap` still bounds this — the retry consumes one
   iteration of the existing outer loop, so no new infinite-loop risk is
   introduced.

### Acceptance Criteria

- Simulate/force an empty response mid-turn (e.g., via a test double or by
  reproducing the original bloated-context scenario before §2-§4 land).
  Confirm the system retries once with a minimal context and, if the retry
  succeeds, the user sees a real answer instead of the clarification
  message.
- Confirm a second consecutive empty response still surfaces
  `ResponseEmptyTruncated` as before — no infinite retry loop.
- Confirm normal (non-empty) responses are completely unaffected — this
  only activates on the existing empty-response branch.

### Risk / Reward Entry Required

Low risk, additive fallback (per `AGENT_READ_FIRST.md` §4.3 — proceed with
testing). Still log briefly to `RISK_ANALYSIS_LOG.md` since it touches
`agentic_loop.go`'s core loop control flow.

---

## 6. 🧊 PROPOSED (Larger Lift): Knob D — On-Demand Schema Tools

### What

For data sources with 10+ tables (configurable threshold), replace the
full schema dump in the system prompt with two lightweight tools —
`list_tables` (names + row counts only) and `describe_table(name)` (full
column detail for one table) — that the model calls on demand instead of
receiving all table schemas whether relevant or not.

### Why Last

Biggest implementation lift of everything in this document (new tools,
conditional tool registration based on schema size, introspection call
wiring) and its payoff is conditional — schemas below the threshold (like
the diagnosed `classicmodels`, 8 tables) see **zero benefit**, since the
existing full-dump prompt is already small for them. This is the right
knob for someone connecting a 50-table production warehouse, not for the
scenario that prompted this entire investigation. Build it after the
higher-certainty, broader-impact items above.

### Implementation Plan

1. **New field on `DataSourceConfig`:**

   ```go
   // SchemaToolThreshold: when the data source has this many tables or
   // more, the system prompt omits the full schema and registers
   // list_tables/describe_table tools instead, which the model calls on
   // demand. Below this threshold, the full schema dump is used as today
   // (unmodified). 0 or unset defaults to 10.
   SchemaToolThreshold int `json:"schema_tool_threshold,omitempty"`
   ```

2. **Threshold check** at the point `buildToolSystemPrompt` and `buildTools`
   currently assemble the schema section / tool list — both need to know
   `len(schema.Tables)` vs. the resolved threshold (default 10 if
   `SchemaToolThreshold <= 0`).

3. **Two new tool definitions** in `buildTools` (`agentic_loop.go:28`),
   registered only when the threshold is met:
   - `list_tables` — no parameters. Returns `{table_name}({row_count})`
     for every table, comma-separated, nothing else.
   - `describe_table` — one required string parameter `table_name`.
     Returns the same column-level detail (`humanizeColumnName`, types,
     nullability, PK/FK) that the full dump currently includes per table,
     but for exactly the one requested table.

4. **New tool-call handling** in `runAgenticLoop`'s tool-call switch,
   alongside the existing `query_database`/`respond_to_user`/`render_chart`
   cases — these are pure introspection, no SQL execution, no
   `executeSQLWithMode` call, so no read-only-invariant surface at all
   (confirmed safe per §0 — this is metadata about the schema already
   available via `GetDataSchema`/`DataSchema`, not a new data-access path).

5. **System prompt branch**: when the threshold is met, `buildToolSystemPrompt`
   omits the per-table column-level schema section entirely and instead
   includes one line instructing the model to call `list_tables` first.

6. **UI (`frontend/src/SettingsView.svelte`)** — this is the setting the
   user is asking about directly: a per-data-source control for "full
   schema dump vs. on-demand tool availability." Same six-touchpoint
   pattern as Knob A/C, numeric field like `max_tools_per_round`:

   1. **Default object literal** (`:383`) — add `schema_tool_threshold: 10,`.
   2. **"New connection" reset literal** (`:567`) — add
      `schema_tool_threshold: 10,`.
   3. **`openSourceDetail`'s local default object** (`:595`) — add
      `schema_tool_threshold: 10`.
   4. **Parse-through line** (`:613`) — add
      `if (parsed.schema_tool_threshold) config.schema_tool_threshold = parsed.schema_tool_threshold`.
   5. **Save payload** (`:687`) — add
      `schema_tool_threshold: dbDetailConfig.schema_tool_threshold,`.
   6. **Form markup** (`:1317` area, in "Exploration Settings" or a new
      "Schema Settings" `db-section`):
      ```svelte
      <div class="form-group">
        <label>Schema Tool Threshold</label>
        <input type="number" bind:value={dbDetailConfig.schema_tool_threshold} placeholder="10" />
        <p class="hint">Schemas with fewer tables than this get the full
          schema sent directly in every prompt (default, works well for
          small schemas). Schemas at or above this table count switch to
          on-demand list_tables/describe_table tools instead — the model
          looks up only the tables it needs, which keeps large schemas
          from bloating every request. Default: 10.</p>
      </div>
      ```

   **This is the single toggle point for the whole feature** — when checked,
schema tools are always used regardless of table count. When unchecked,
the global threshold (hardcoded at 10 tables) determines whether tools
kick in automatically. The threshold itself is a constant, not a
per-data-source field, because it's a system-wide behavior choice, not
something that should vary per connection. A per-connection force-toggle
+ a global threshold avoids the consistency problem of having both a
checkbox AND a threshold number on the same form that could contradict
each other.

### Acceptance Criteria

- Data source below threshold (e.g., `classicmodels`, 8 tables, default
  threshold 10): confirm byte-identical system prompt and tool list to
  current behavior — zero change.
- Data source at/above threshold: confirm the full schema section is
  absent from the system prompt, `list_tables`/`describe_table` appear in
  the tool list, and a multi-table question correctly results in the model
  calling `list_tables` then 1+ `describe_table` calls before writing SQL.
- Confirm `describe_table` with an invalid/unknown table name returns a
  clear error message the model can act on (e.g., "Table 'x' not found.
  Available tables: ...") rather than a crash or empty response.

### Risk / Reward Entry Required

Medium risk per the categorization already in `DISCUSSION_CONTEXT_ISSUES.md`
— full risk/reward entry required per `AGENT_READ_FIRST.md` §4.2 (adds new
tool-call paths, affects multiple drivers indirectly via schema
introspection). Explicitly note in the entry: more round-trips for
multi-table questions, and the threshold is a table-count heuristic, not a
precise byte-size cost model (a data source with a few very wide tables
could exceed a 10-table-threshold schema in raw prompt bytes despite being
under the numeric threshold).

---

## 7. 🧊 DEFERRED: Duplicate-Query Detection

**Explicitly deferred, not scheduled.** Documented here only so the reason
for deferral isn't lost.

**What it would do:** hash the SQL string before executing an exploration
query; if identical to a prior query already run in the same turn, skip
execution and feed back "This query was already run earlier in this turn —
it returned N rows" instead of re-executing.

**Why deferred:** §3 (tools-per-round cap) already limits the *damage* a
model can do by re-issuing duplicates, even without detecting the
duplication itself. The underlying model behavior (redundant exploration)
is judged better addressed through model selection and prompt engineering
than through a code-enforcement path that could itself confuse an
already-struggling local model with an unexpected "already ran this"
feedback message it doesn't know how to react to.

**Revisit if:** after §2–§6 ship, redundant/duplicate exploration remains a
measurable top contributor to context exhaustion in real usage. If
revisited, implement as: SHA-256 (or simple string equality — no need for
cryptographic hashing) of the normalized SQL string, checked against a
per-turn `map[string]bool` of already-executed queries, reset at the start
of each new user turn.

---

## 8. Build & Test Checklist (Apply to Every Item Above)

Per `AGENT_READ_FIRST.md` §5.1/§5.2/§5.3, before merging any item from this
document:

- [ ] Fundamental goal: does the change improve or preserve answer quality?
      (For §2/§3/§4: yes, directly, by preventing context-exhaustion
      non-answers. For §6: conditionally, only for large schemas.)
- [ ] SQL execution: test with at least one real database type — the
      diagnosed scenario used MySQL (`classicmodels`); also verify against
      SQLite (pure Go driver, per charter §5.3) since it has different
      digest/column-type behavior in `executeSQLWithMode`.
- [ ] LLM integration: test with at least one frontier model (OpenAI/
      Anthropic) AND one local/small model (Ollama) — this entire problem
      is disproportionately triggered by weaker models, so local-model
      testing is not optional for any item in this document.
- [ ] Read-only guarantee: confirm no code path in any new tool
      (`list_tables`, `describe_table`) or any new tool-call rejection path
      (§3's cap) allows INSERT/UPDATE/DELETE/DDL to reach a data source —
      trivially true for §2–§5 (no new data-access paths), must be
      explicitly re-verified for §6 (new tool-call types).
- [ ] Migration safety: confirm no data loss risk in any new
      `DataSourceConfig` fields — all are additive (`omitempty`, sensible
      zero-value defaults), consistent with §3.2's "only add" discipline.
- [ ] Existing conversations: confirm conversations that don't touch any
      new setting behave byte-identically to before these changes (default
      values must reproduce current behavior exactly).
- [ ] Wails bindings: if any new setting needs a new binding method in
      `app.go` to read/write it from the frontend, confirm it follows the
      ~50-line-method guidance in §2.1 and returns typed structs.

---

## 9. Cross-References

- **`DISCUSSION_CONTEXT_ISSUES.md`** — full discussion record: problem
  diagnosis, all options considered (including ones not chosen), risk
  analysis for the "Rounds" rename, and the reasoning trail behind every
  decision referenced in this document.
- **`RISK_ANALYSIS_LOG.md`** — where each item's formal risk/reward entry
  must be logged before/at implementation, per `AGENT_READ_FIRST.md` §4.4.
- **`SMALL_ISSUES.md`** — entry #3 (Conversation History Context
  Exhaustion) tracks the original bug this whole body of work resolves;
  update its status as items in this document ship.
- **`AGENT_READ_FIRST.md`** — the charter this plan is scoped against.
  Notably: §0 (read-only invariant — verified not at risk in any item
  above), §1.4 (SQL execution safety — code-enforced, not prompt-enforced,
  relevant to §4's compact-prompt trade-off), §4 (risk assessment
  framework — applied per-item above).

---

*This document was created on 2026-08-10 as the actionable counterpart to
`DISCUSSION_CONTEXT_ISSUES.md`, which had grown into a long discussion
record that was hard to act on directly. Items 1–3 in the status table
were already shipped before this document existed; items 4 onward are the
prioritized, sequenced remainder of the work.*
