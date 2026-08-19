# Tool / Round Consolidation

> **Status:** Design analysis — not yet implemented.
> **Context:** Models consistently issue more `query_database` calls per round than the configured limit, then express confusion ("the second query didn't execute"). The root cause is a dual-unit budget: the model is told "3 rounds, 2 per round" but enforcement counts *tool calls*, not rounds. This documents the problem and the recommended simplification.

---

## 1. Definitions

### Round

One iteration of the agentic loop in `pkg/services/agentic_loop.go` (`runAgenticLoop`):

```go
for round := 0; round < totalRoundCap; round++ {
    // 1. One LLM completion call (ChatCompletionWithTools / ...Streaming)
    // 2. Process the single response:
    //    - plain text   → respond_to_user
    //    - empty        → clarification
    //    - tool_calls[] → execute each tool call sequentially
    // 3. Append assistant + tool-result messages to conversation
}
```

A round is fundamentally one **LLM reasoning step** — the model receives a context window,
thinks, and produces one response. Results from tool calls are only visible at the *next*
LLM call, never mid-round.

### Tool Call

One entry in `response.ToolCalls` — a `{id, function: {name, arguments}}` object.
Available tools:

| Tool | Purpose |
|------|---------|
| `query_database` | Run a read-only query (exploration or final) |
| `respond_to_user` | Deliver text to the user |
| `render_chart` | Attach a chart to the most recent final result |
| `list_tables` | List all tables with row counts |
| `describe_table` | Describe columns and types for one table |

Tool calls are processed sequentially within the round:
`for toolIdx, tc := range response.ToolCalls { ... switch tc.Function.Name ... }`

### Relationship

**1 round = 1 LLM API call, containing 0..N tool calls.**

This relationship is not a design choice — it is dictated by the LLM tool-calling
protocol. The model emits tool calls; the system executes them and appends `tool`-role
result messages; the model must be invoked *again* to see those results. Rounds are
structurally unavoidable.

---

## 2. The Current Budgeting Model

Three numbers govern the loop, but they use different units:

| Variable / Config | Meaning | Unit | User-Facing? |
|---|---|---|---|
| `MaxExplorationRounds` | Total exploration `query_database` calls allowed | **tool calls** (misnamed "rounds") | Yes, in the prompt and data-source settings |
| `MaxToolsPerRound` | Max `query_database` calls per response | tool calls per round | Yes, in the prompt and data-source settings |
| `totalRoundCap` | Hard loop-safety bound | rounds (actual LLM iterations) | No (internal only: `maxExplorationRounds + maxErrorRetries + 4`) |

**The critical problem:** `MaxExplorationRounds` is named "rounds" but enforced **per
tool call** (`explorationToolCallsUsed >= maxExplorationRounds`). The code comment in
`pkg/models/db_connection.go` acknowledges this:

> "Despite the 'rounds' name (a legacy from the pre-tool-calling protocol where one
> LLM call produced exactly one query), the counter is incremented per tool call,
> not per LLM round-trip."

So the model is told "3 rounds, 2 per round" in the prompt, but the truth is "3
total queries, max 2 per response." **Two budgets, two units, one mislabeled.**

### What counts against the budget

`explorationToolCallsUsed` increments on:

- ✅ Executed exploration queries (success or error)
- ✅ Safety-rejected exploration queries (blocked by `validateExplorationQuery`)
- ❌ NOT on: too-many-tools-per-round rejection (the call never reached execution)
- ❌ NOT on: parse errors, budget-exhausted rejections, oneshot violations

---

## 3. What the Per-Round Cap Buys (and Costs)

### The one genuine purpose

A per-round cap of 1–2 forces the model to **pause and see results** before spending
more budget. Because tool results are only visible at the next LLM call, this enforces
the exploration rhythm:

```
Round 0: query → see results (start of round 1)
Round 1: query (informed by round 0 results) → see results (start of round 2)
```

Without a per-round cap, the model can dump its entire budget in one blind batch —
all queries are based on the same prior context, with no incremental learning.

### What it costs

- **Re-issuance**: the model batches 3–4 queries, the cap rejects the excess, and
  the model re-issues them in the next round — wasting an LLM round-trip.
- **Model confusion**: the model doesn't track which queries were rejected ("the
  second query didn't execute" — actually the third).
- **More round-trips, not fewer**: the cap *increases* LLM API calls because rejected
  queries must be retried in a new round.
- **Dual-unit planning**: the model must reason about two different constraints
  simultaneously, and reliably gets it wrong.

### The trade-off

| | Per-round cap ON (current) | Per-round cap OFF (proposed) |
|---|---|---|
| Exploration quality | Incremental (better for large budgets) | Blind batch (fine for small budgets) |
| LLM round-trips | More (re-issuance) | Fewer |
| Model comprehension | Confusing (dual units) | Simple (one number) |
| Re-issue loop | Frequent | Eliminated |

For a small total budget (e.g., 3), the incremental-pacing benefit is negligible — the
model's first 2–3 queries are going to be mostly-blind anyway, and getting all results
in one batch is strictly more efficient.

---

## 4. Recommendation

### Collapse to a single budget

Replace the dual-unit budget with one number:

```
MaxExplorationQueries  (total tool calls, no "rounds" label)
```

Drop `MaxToolsPerRound` as a first-class budget. The model may batch up to its
total budget in one response or self-pace across rounds — its choice. The total
budget is the only constraint.

### Keep rounds as internal loop guard

The loop still needs a hard cap to prevent infinite iteration (e.g., error retries,
clarification loops). Decouple it from the exploration budget:

```
totalRoundCap = maxExplorationQueries + maxErrorRetries + safetyMargin
```

This is internal, not user-facing, and not a number the model needs to know.

### If forced pacing is ever needed

`MaxToolsPerRound` can return as an **optional advanced throttle** (default off/∞)
with a clear label: "Max queries per round." But it should be a performance/cost
dial, not a core budget the model must plan around. When off (the default), the
model sees only a single "total exploration queries" number and never hits a
"too many tools per round" rejection.

### Model prompt

The prompt becomes simpler:

> `- **Exploration:** enabled (max %d exploration queries, %s mode)`

One number. No "round" ambiguity. The model doesn't need to juggle two constraints
or guess which queries were rejected.

---

## 5. Implementation Notes (for reference)

- `MaxExplorationRounds` in `db_connection.go` should be renamed to
  `MaxExplorationQueries` and the JSON key should change only if a migration path for
  existing data-source configs exists; otherwise update comments/documentation.
- Remove the `MaxToolsPerRound` enforcement from `agentic_loop.go` (the
  `toolsThisRound > maxToolsPerRound` check). Save the removal of the config field for
  a clean migration window.
- Remove `max_tools_per_round` from the prompt (already done conditionally: only
  injected when > 0).
- Adjust `safety.footer_rounds` in `agent_loop_config.go` to reflect the single-unit
  budget.
- The `totalRoundCap` safety net should be recalculated — a simple
  `maxExplorationQueries + maxErrorRetries + 4` remains adequate since each round
  can consume multiple queries.

---

## 6. Related Documents

- `AGENT_READ_FIRST.md` — §0 (Fundamental Goal), §1.3 (Agentic Tool-Calling Loop)
- `AGENT_LOOP_CONFIG.md` — prompt template fields
- `FINAL_MESSAGE.md` — one-shot finality rules
- `LOOPING_DETECTION.md` — loop guard rationale