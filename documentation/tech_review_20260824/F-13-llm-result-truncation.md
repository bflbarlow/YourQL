# F-13 · Bound tool-response size sent back to the LLM from large result sets

**Severity:** Medium · **Effort:** Medium · **Risk categories:** answer accuracy, reliability, cost/latency

## Problem Statement

Row limits cap *rows* (default 1000 final / 100 exploration), but the tool
response handed back to the model renders **every** row as a markdown table
(`formatResults` builds a padded grid of all rows). For wide schemas this
multiplies: 1000 rows × 15 columns × ~15 chars ≈ 200KB+ of text injected
into the conversation history, then re-sent on every subsequent round of
the agentic loop (messages accumulate across rounds).

Consequences against the fundamental goal:

1. **Context exhaustion** → mid-loop provider errors ("context length
   exceeded") that surface as fatal loop failures after the user has waited.
2. **Accuracy degradation** — long-context degradation is well documented;
   the model's next-round reasoning (chart selection, summary generation)
   gets worse as noise grows.
3. **Cost/latency tax** — tokens billed on every subsequent round; streaming
   feels slower exactly when answers are biggest.
4. Provider-dependent cliff behavior: some local models (Ollama) silently
   truncate, meaning the model "sees" partial tables and may answer from
   truncated data — a wrong-answer risk (priority #1).

Note the asymmetry: the *user* sees the full interactive table in the UI
regardless; only the model needs fewer rows.

## Solution Design

Separate the **model-facing rendering budget** from the **user-facing
result**, mirroring the existing philosophy (exploration results already
differ from final presentation).

### Config surface

Extend `AgentLoopConfig` (canonical in `pkg/engine/types.go`, aliased per
§1.6):

```go
type AgentLoopConfig struct {
    // ... existing fields ...
    ToolResultMaxRows    int `json:"tool_result_max_rows,omitempty"`    // default 50
    ToolResultMaxChars   int `json:"tool_result_max_chars,omitempty"`   // default 20000
}
```

Defaults applied in the loop's config-normalization block alongside
`totalRoundCap`.

### Rendering change

Where exploration/final tool responses embed `formatResults(result)`:

```go
func formatResultsForModel(result *QueryResult, maxRows, maxChars int) string {
    shown := result.Rows
    truncated := false
    if maxRows > 0 && len(shown) > maxRows {
        shown = shown[:maxRows]
        truncated = true
    }
    out := renderTable(shown /* existing padding logic */)
    if maxChars > 0 && len(out) > maxChars {   // very wide rows
        out = out[:maxChars] + "\n…[truncated]"
        truncated = true
    }
    if truncated {
        out += fmt.Sprintf("\n(%d of %d rows shown — full result delivered to the user)", len(shown), result.RowCount)
    }
    return out
}
```

The trailing note matters: it tells the model authoritatively that the user
received complete data, preventing the known failure mode where the model
apologizes for "only showing part of your data" or re-queries unnecessarily.

Column-level guard too: if `len(columns)` exceeds e.g. 40, show first N
columns + column-count note (wide-row truncation alone mangles markdown).

### What does NOT change

- `QueryResult` delivered to the UI/output handler: untouched full fidelity.
- Final-answer HTML/table rendering: untouched.
- Chart generation input: charts should keep using full `QueryResult`
  (aggregations need all rows). Verify chart resolution reads from the
  result struct, not the markdown text — if any path parses rendered text,
  fix that path instead.

### Prompt documentation

Add one sentence to `buildToolSystemPrompt` describing the preview behavior
so the model doesn't miscount rows when writing summaries ("Result previews
shown to you may be truncated; the user sees the complete result set.").

## Implementation Plan

1. Add config fields + defaults; thread through loop where tool responses
   are composed.
2. Implement `formatResultsForModel` (pure function → belongs in
   `pkg/engine/rendering.go`; keeps silo boundary).
3. Tests (engine pkg): row-truncation math, char-truncation, wide-column
   case, note text present, full-fidelity result still reaches
   OutputHandler mock.
4. Manual multi-provider check per §5.2 with a deliberately huge query
   (generate 10k-row SQLite CTE): confirm loop completes on smallest-context
   local model.
5. Settings UI (advanced section, alongside existing AgentLoopConfig
   controls): expose both knobs with defaults labeled.
6. RISK_ANALYSIS_LOG entry per §4.2 (touches loop + prompt construction).

## Risk Assessment

- **Model confusion about truncation** → mitigated by explicit trailing
  note + prompt sentence; monitor ANSWER_CLARIFICATION_ISSUE-type feedback.
- **Summary/chart quality drop from seeing less data** → summaries operate
  on full result server-side today (verify in discussion_engine summary
  path); if any summarizer consumes the model-facing text, switch it to the
  full result first — this is a prerequisite, checked in step 3 tests.
