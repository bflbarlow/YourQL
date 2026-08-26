# Final Message Design — Single Bubble, Single Surface

> Per `AGENT_READ_FIRST.md` §0: *"YourQL exists to serve the answer to a
> user's question from their database."* A single clear answer bubble
> reinforces trust. Multiple table-only bubbles before the answer erode it.

**Status: Design specification. Implementation pending.**

---

## 1. Problem Statement

The user asks one question. They should get one answer bubble — analysis text
above a data table, or a text-only response if no query was needed.

Today they often get three or four. The LLM runs multiple `query_database` calls
marked `is_exploration: false`, and each one produces a standalone table bubble
before the final analysis arrives. This happens because:

1. **Exploration safety mode is too restrictive by default.** Strict mode
   blocks JOINs, subqueries, GROUP BY, and ORDER BY — leaving exploration
   unable to do real data analysis. The LLM is forced to mark analytical
   queries as `is_exploration: false`, which surfaces them to the user.

2. **No enforcement of one-shot finality.** After the first `is_exploration:
   false` query succeeds, the loop allows more queries in subsequent rounds.
   Each one becomes a visible message.

3. **Conflated semantics.** `is_exploration` serves two unrelated purposes —
   safety gate (is this query allowed?) and visibility control (should the
   user see it?). These must be separated.

---

## 2. Design Goals

1. **One message bubble per user question.** Always.
2. **Exploration queries are behind the scenes.** They accumulate data for
   the LLM's internal reasoning. The user never sees them unless they expand
   the tech details toggle.
3. **Safety is a secondary constraint on exploration, not its primary
   purpose.** The primary purpose of `is_exploration: true` is "don't surface
   this to the user." The safety mode defines *how complex* behind-the-scenes
   queries may be, not whether they can exist at all.
4. **One surfaced query per answer.** Once the model delivers a
   user-visible query result, the database session closes. No more queries.
5. **Honest degradation.** If the model can't complete its analysis within
   the exploration safety constraints, it tells the user so in the response —
   it does not silently switch to surfacing intermediate results.

---

## 3. Three Changes

### 3.1 Default Safety Mode — Strict → Relaxed

**Current:** `ExplorationStrict` blocks JOINs, subqueries, GROUP BY, and
ORDER BY. The LLM can run bare `SELECT col FROM table LIMIT 5` and nothing
else. This makes exploration unusable for the data analysis it was designed
to support.

**Proposed:** Default to `ExplorationRelaxed`. This allows JOINs, subqueries,
GROUP BY, ORDER BY, UNION — every analytical pattern the LLM needs. DML/DDL
remains unconditionally blocked regardless of mode, per the Data Source
Read-Only Invariant (§0).

Users who connect to sensitive production databases can set the mode to
moderate or strict explicitly. That's a conscious choice, not the default.

**Files:**

- `pkg/services/discussion_engine.go:134` — change default from
  `ExplorationStrict` to `ExplorationRelaxed`.

### 3.2 `is_exploration` — Visibility First, Safety Second

**Current description of the `is_exploration` parameter:**

> *"Set to true if this is an exploratory query to understand the data before
> writing the final query. Set to false for the final answer query."*

This conflates purpose with visibility. It says "use this to explore" but
doesn't say "the user won't see it." The LLM interprets it as a workflow
hint, not a visibility contract.

**Proposed description:**

> *"Set to true for behind-the-scenes queries the user should NOT see.
> These are your investigative tools — use them to understand the data,
> test assumptions, and gather context before committing to a final answer.
> Set to false ONLY for the single final query whose results should be
> shown to the user. Exploration queries are subject to safety mode
> restrictions; if a query is rejected, adapt your approach or tell the
> user what you couldn't do."*

**Safety mode feedback:** When an exploration query is rejected by the safety
gate, the tool response already tells the LLM why. The model should relay
this to the user in its final response — not silently switch to
`is_exploration: false` to bypass it.

**Files:**

- `pkg/services/agentic_loop.go:32-36` — update `is_exploration` description
  in `query_database` tool definition.
- `pkg/services/agentic_loop.go:615-630` — update exploration safety rules
  section in `buildToolSystemPrompt` to emphasize that rejections should be
  communicated to the user, not routed around.

### 3.3 One-Shot Finality

**Rule:** As soon as the model surfaces a visible result to the user, the
database session closes for this request. No more `query_database` calls.

**Concrete behavior:**

| Phase | Allowed | Blocked |
|---|---|---|
| Exploration (`is_exploration: true`) | Unlimited within budget | — |
| Final query (`is_exploration: false`) | **One** successful query | Additional queries after success |
| Error retries on final query | Allowed within retry budget | — |
| After final query succeeds | `respond_to_user`, `render_chart` | `query_database` of any kind |
| After `respond_to_user` | Nothing — message delivered | Everything |

If the model attempts `query_database` after the final result is delivered,
the tool response says: *"Final result already delivered. Use
respond_to_user if you need to explain limitations or next steps."* The
model then has one round to compose a `respond_to_user` response.

**Handling incomplete analysis:** If the model runs out of exploration
rounds or hits the safety wall before it's ready to commit to a final
query, it should call `respond_to_user` explaining what it was able to
determine and what it couldn't. This is honest and useful — better than
surfacing a partial data table and hoping the user fills in the gaps.

**Interaction with batching:** When the model sends `query_database
(is_exploration: false)` + `respond_to_user` in the same round, both are
processed. The query executes, `pendingFinal` is set, and the
`respond_to_user` text is merged into one message (text above table). No
additional rounds are offered — the message is delivered immediately.

**Files:**

- `pkg/services/agentic_loop.go:runAgenticLoop` — after a successful
  `is_exploration: false` query sets `pendingFinal`, block subsequent
  `query_database` calls with a clear tool error message. Allow only
  `respond_to_user` and `render_chart` in subsequent rounds.
- `pkg/services/agentic_loop.go:case "query_database"` — when
  `pendingFinal` already exists, instead of rendering the previous result
  and allowing a new query, reject the new query. Exception: exploration
  queries (which don't surface) may still be allowed within budget if the
  previous `pendingFinal` was also an exploration.

---

## 4. Transcript and History Replay

All queries — exploration and final — continue to be recorded in the tool
transcript for LLM history replay. The difference is only in what the user
sees:

- **Exploration queries:** Recorded in transcript, stored as tech detail
  messages (role: `exploration`, hidden unless tech details expanded), never
  rendered as visible chat bubbles.
- **Final query:** Recorded in transcript, stored on the assistant message,
  rendered as the data table in the visible chat bubble.
- **`respond_to_user` text:** Recorded in transcript, rendered as the
  analysis/prose in the visible chat bubble.

No data is lost. The LLM has full context on replay. Only the user-facing
surface changes.

---

## 5. Risk Assessment

Per `AGENT_READ_FIRST.md` §4.

### 5.1 Impact on Fundamental Goal

| Priority | Impact |
|---|---|
| **#1 Correctness** | **Improved.** The LLM runs more exploration queries (fewer restrictions) leading to more informed final answers. |
| **#2 Safety** | **Unchanged.** DML/DDL remains blocked in all modes. Read-only invariant preserved. |
| **#3 Reliability** | **Improved.** One-shot finality eliminates the "run more queries, see what else I can find" loop that causes latency and confusion. |
| **#4 Appeal / Comfort** | **Improved.** One clean answer bubble instead of a cascade of table chunks. |

### 5.2 Risk Categories

| Category | Assessment |
|---|---|
| **Answer accuracy** | **Low risk, high benefit.** Relaxed exploration defaults allow the LLM to gather more context. The one-shot constraint forces it to commit, which could reduce accuracy if exploration is insufficient — mitigated by the exploration budget and the model's ability to explain limitations in `respond_to_user`. |
| **Answer delivery** | **Low risk.** Rendering path unchanged. The merge (text + table) is already implemented and working. |
| **Data safety** | **No risk.** DML/DDL blocked in all modes. Read-only invariant unchanged. |
| **User trust** | **Improved.** One bubble feels deliberate and polished. The model explaining its own limitations ("I couldn't run subqueries to answer this fully") builds more trust than silently surfacing partial data. |
| **Regression** | **Low.** Safety mode default change is isolated to one constant. One-shot enforcement is additive — doesn't change existing message rendering. |
| **UX/Comfort** | **High benefit.** Eliminates the most common user complaint about the chat interface. |

### 5.3 Failure Modes

1. **LLM can't finish analysis within exploration budget.** Mitigation: model
   explains what it learned and what it couldn't in `respond_to_user`. User
   can ask a follow-up.

2. **Exploration queries time out or exhaust resources with relaxed defaults.**
   Mitigation: LIMIT is still applied to all queries. Exploration budget (max
   rounds) is still enforced. User can configure stricter modes if needed.

3. **One-shot enforcement blocks a legitimate second query.** Mitigation:
   error retries on the final query are still allowed. The model gets one
   "successful" surfaced query — not one "attempt."

4. **Model marks everything as exploration and never surfaces a result.**
   Mitigation: the loop already handles this — if all queries are exploration,
   the loop eventually exhausts rounds and forces a `respond_to_user`. The
   model is prompted to eventually produce a final answer.

### 5.4 Decision

**Proceed.** Risk is low across all categories. Reward is significant UX
improvement. The exploration safety mode default change is conservative
(relaxed still blocks all writes) and configurable. One-shot finality
aligns with the fundamental goal — serve one answer, cleanly.

---

## 6. Charter Compliance

Per `AGENT_READ_FIRST.md` §6:

- [ ] **Is this change aligned with the fundamental goal?** Yes. Improves
      answer delivery quality and user trust.
- [ ] **Is the risk/reward analysis documented?** This document (§5).
- [ ] **Files identified.** `discussion_engine.go:134`,
      `agentic_loop.go:32-36`, `agentic_loop.go:runAgenticLoop`,
      `agentic_loop.go:buildToolSystemPrompt`.

- [ ] **Read-only invariant preserved?** Yes. Exploration mode change
      does not affect DML/DDL blocking.
- [ ] **SQL execution safety preserved?** Yes.
- [ ] **Existing conversations backward compatible?** Yes. Safety mode is
      per-data-source config; existing strict configurations preserved.
- [ ] **UI responsive?** N/A — rendering path unchanged.
- [ ] **Dark/light mode?** N/A — no new UI elements.
- [ ] **API keys/passwords not logged?** N/A — no logging changes.
- [ ] **Documentation updated?** This document is the update.

---

## 7. Implementation Summary

| File | Change |
|---|---|
| `pkg/services/discussion_engine.go` | Line 134: `ExplorationStrict` → `ExplorationRelaxed` |
| `pkg/services/agentic_loop.go` | Lines 32-36: Rewrite `is_exploration` parameter description to emphasize visibility-first semantics |
| `pkg/services/agentic_loop.go` | Lines 615-630: Update exploration safety rules to instruct model to communicate rejections to user, not bypass them |
| `pkg/services/agentic_loop.go` | `runAgenticLoop`: After `pendingFinal` is set by a surfaced query, reject subsequent `query_database` calls; allow only `respond_to_user` and `render_chart` |
| `pkg/services/agentic_loop.go` | `case "query_database"`: When `pendingFinal` exists and new query is not exploration, emit tool error instead of rendering + overwriting |