> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Timeout Resolution — Summarization Context Deadline

**Date:** 2026-08-17
**Status:** Diagnosed, not yet fixed

---

## Symptom

Every scorecard run produces:

> ⚠️ Summary unavailable — the model could not generate one for this result set.

With the log:

```
[AgenticLoop] Summarization failed: LLM call for summarization failed:
    failed to read response body: context deadline exceeded
```

100% reproducible. Not intermittent.

---

## Root cause

The entire pipeline — agentic loop + summarization — shares a single 180-second
context deadline set at `discussion_engine.go:178`:

```go
ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
defer cancel()
...
if err := runAgenticLoop(ctx, ...); err != nil { ... }
```

This `ctx` flows through `runAgenticLoop` → `pendingFinalResult.ctx` →
`summarizeResults(p.ctx, ...)`. Summarization inherits whatever time remains
after the main loop finishes.

When the main loop is slow — which it reliably is for complex questions via
OpenRouter (one round took 73 seconds in testing) — summarization gets a
fraction of the 180-second budget before the deadline fires.

### Log evidence — Conversation 113

| Event | Timestamp | Elapsed |
|---|---|---|
| Pipeline starts | 17:50:15 | 0s |
| Summarization call | 17:52:46 | 151s consumed by main loop |
| Summarization fails | 17:53:15 | 180s — deadline fires |

Summarization had exactly 29 seconds before the pipeline deadline hit.

### Log evidence — Conversation 114

| Event | Timestamp | Elapsed |
|---|---|---|
| Pipeline starts | 17:54:02 | 0s |
| Summarization call | 17:56:32 | 150s consumed by main loop |
| Summarization fails | 17:57:02 | 180s — deadline fires |

Same pattern: ~150 seconds of main-loop time, ~30 seconds left for
summarization, deadline fires mid-call.

### Why the main loop is slow

The scorecard question involves multiple LLM rounds with large prompts and
responses. OpenRouter adds latency (queuing, rate limiting, model inference
time). One round in Conv 113 took **73 seconds**. When 3–4 rounds each take
20–70 seconds, the pipeline budget is exhausted before summarization can
finish.

This is NOT an OpenRouter-imposed limit. The HTTP client timeout is 300
seconds. The deadline comes from YourQL's own pipeline context at
`discussion_engine.go:178`.

---

## Fix options

### Option A — Decouple summarization from the pipeline context (recommended)

In `agentic_loop.go`, `pendingFinalResult.render()`, give `summarizeResults`
its own context with an independent timeout:

```go
// Replace: s, err := summarizeResults(p.ctx, p.client, ...)
sCtx, sCancel := context.WithTimeout(context.Background(), 120*time.Second)
defer sCancel()
s, err := summarizeResults(sCtx, p.client, p.userMessage, p.sql, p.result,
                            p.skillsContent, conversation.ID)
```

This guarantees summarization gets a full 120 seconds regardless of how long
the main loop took. The main loop keeps its own 180s deadline.

**File:** `pkg/services/agentic_loop.go`, `pendingFinalResult.render()` (line ~180).

### Option B — Increase the pipeline timeout

Change `discussion_engine.go:178` from 180s to 300s or 600s:

```go
ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
```

Simpler but doesn't address the root cause: the main loop and summarization
still share a clock, and a slow main loop will always starve summarization.
Option A is preferred because it fixes the architecture, not just the number.

### Option C — Both (belt and suspenders)

Do A (decouple) AND increase the pipeline timeout to 300s. The pipeline
timeout handles runaway loops; the summarization timeout handles slow
summaries independently.

---

## Verification

1. Run the scorecard question with `YOURQL_DEBUG_STREAMS=1`.
2. Confirm the summarization call now succeeds and produces a natural-language
   summary instead of "Summary unavailable."
3. Check that a fast question (e.g., "How many orders?") still works — the
   new summarization context should not affect normal cases.

---

## Related

- `discussion_engine.go:178` — pipeline timeout
- `agentic_loop.go:180` — summarization call site
- `llm_openai.go:49` — HTTP client timeout (300s, not the issue)
- `TEST_HARNESS_ANALYSIS_20260817_01.md` — Finding 4 (summarization signal)