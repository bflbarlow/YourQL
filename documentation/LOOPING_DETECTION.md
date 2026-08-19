# Looping Detection & Timeout-Aware Prompting

**Date:** 2026-08-11
**Project:** YourQL — Detect and mitigate reasoning-model loops, add timeout
awareness to the system prompt.

---

## 1. Problem Statement

### Observed Failure

A user asked a complex analytical question (NPV calculation across customers)
using the reasoning model `qwen/qwen3.6-35b-a3b`. The agentic loop failed
with:

```
Error: LLM call failed: SSE stream error: context deadline exceeded
```

The conversation HTML dump showed the model's reasoning text — a ~3KB block
of planning — was **emitted repeatedly, identically,** across multiple rounds
without ever producing a tool call. The 180-second total loop timeout
(`discussion_engine.go:176`) expired mid-stream, killing the connection.

### Root Causes

1. **Total timeout, not per-round.** The 180s `context.WithTimeout` in
   `discussion_engine.go` covers the *entire* agentic loop. Every exploration
   round + the final answer round all consume from the same budget. A
   reasoning model that spends 40s thinking in round 1, 50s in round 2, etc.,
   runs out of time before delivering results.

2. **No timeout signaled to the LLM.** The system prompt gives the model zero
   information about its time budget. Reasoning models (Qwen3, DeepSeek-R1,
   o1, Claude with extended thinking) have no intrinsic reason to be concise.
   They will think as long as their training allows, producing verbose
   internal monologue that costs tokens, latency, and budget.

3. **No detection of reasoning loops.** When a model gets stuck re-deriving
   the same plan over and over (as the Qwen model did — the identical ~3KB
   reasoning block appeared many times), there is no mechanism to detect the
   repetition and either signal the model or abort the round early.

### Scope of Affected Models

Any model with extended reasoning / chain-of-thought output:

| Provider | Models | Manifestation |
|---|---|---|
| OpenAI-compatible (qwen) | qwen3-35b-a3b, qwen3-235b-a22b | Streaming `reasoning_content` deltas |
| OpenAI | o1, o3-mini, o4-mini | Internal reasoning (not streamed, but slow) |
| Anthropic | Claude with extended thinking | `thinking` blocks in streaming |
| Ollama (local) | deepseek-r1, qwen3 variants | `think` / `reasoning` content |

### Relation to the Fundamental Goal

This is an **answer delivery** failure (AGENT_READ_FIRST.md §4.1). The
model's answer quality is not the bottleneck — the answer **never arrives**
because the loop times out. Users see an error instead of results. Fixing
this directly serves the fundamental goal of "getting the user the right
answer from their data."

---

## 2. Solution Overview

Two complementary changes, either of which helps independently but together
provide defense in depth:

| # | Component | Where | What |
|---|---|---|---|
| **A** | Timeout-aware system prompt | `agentic_loop.go` — `buildToolSystemPrompt` | Inject a concise note about the time budget into the system prompt so the model knows to be efficient. |
| **B** | Stream-level looping detection | `llm_openai.go` — `ChatCompletionWithToolsStreaming` | Monitor text/reasoning deltas during streaming; if the same content repeats beyond a threshold, signal the model or abort early. |

### A. Timeout-Aware System Prompt

**Rationale:** Reasoning models produce verbose thinking because nothing tells
them to stop. A 3-minute budget is generous for a single SQL query — the
model should spend most of its time on the *query*, not on pre-query
monologue. Giving the model a soft time constraint encourages it to:

- Collapse multi-paragraph planning into a few bullet points
- Avoid re-deriving the same plan in subsequent rounds
- Prioritize calling `query_database` early rather than over-thinking

**Where to inject:** At the end of the system prompt, after all schema,
dialect rules, and tool definitions. The message adds a single sentence:
"You have a limited time budget per round — be concise in your reasoning
and prioritize calling tools over extended planning."

**Config surface:** Via `agent_loop_config` (`pkg/models/agent_loop_config.go`
and `pkg/services/agent_loop_config.go`) under a new key:

- `prompt.timeout_hint`: the text injected into the system prompt (empty
  by default for backward compatibility; populated to a sensible default
  for all users regardless of config).

**Implementation:**
- Add a constant `defaultTimeoutHint` in `agentic_loop.go`
- Append it at the end of `buildToolSystemPrompt` (and `buildCompactSystemPrompt`
  for consistency)
- Respect a user override from `AgentLoopConfig.PromptTimeoutHint` if set
- The hint should reference the actual timeout value from
  `discussion_engine.go` (currently 180s), or a qualitative description
  ("a few minutes")

**Safety:** This is purely additive. The hint text does not change tool
behavior, schema interpretation, or safety constraints. It is a soft nudge,
not a hard constraint — the model can still take as long as it wants (up to
the hard context deadline).

### B. Stream-Level Looping Detection

**Rationale:** Even with a timeout hint, some models will loop. Detecting
the loop at the stream level lets us abort early — before the hard deadline
is hit — and either retry with a stronger "stop looping" instruction or
surface a clear error to the user immediately.

**Detection algorithm:**

```
maintain a sliding window of the last N deltas (text + reasoning)
maintain a hash (SHA256 or FNV-64a) of the concatenated window contents

on each new delta:
  1. hash new delta
  2. append to window; drop oldest if window > N
  3. concatenate window contents, hash again
  4. if this window hash matches the previous window hash K consecutive times:
     → loop detected
  5. if total reasoning tokens exceed a threshold without any tool call:
     → excessive reasoning detected (different from pure looping)
```

**Parameters (tunable):**

| Parameter | Default | Meaning |
|---|---|---|
| `windowSize` | 8 | Number of deltas in the sliding window |
| `loopThreshold` | 3 | Consecutive identical windows to trigger |
| `maxReasoningTokens` | 4096 | Max reasoning tokens before intervention (no tool call yet) |
| `action` | `"abort_round"` | `"abort_round"` (skip this round, add corrective hint) or `"abort_loop"` (fail the entire request) |

**Where to plug in:** The `ChatCompletionWithToolsStreaming` method in each
LLM provider file. Specifically:

- **OpenAI path** (`llm_openai.go`): Inside the `for scanner.Scan()` loop,
  after parsing `delta.Content` and before calling `onEvent`. The loop
  detector wraps the delta stream transparently.
- **Anthropic path** (`llm_anthropic.go`): Same pattern, hooking into the
  SSE event loop.
- **Ollama path** (`llm_ollama.go`): Same pattern.
- **Local/custom path** (`llm_local.go`): Same pattern.

To avoid duplicating logic across 4 files, implement the detector as a
shared helper in a new file: `pkg/services/stream_loop_detector.go`.

**Failure mode — false positive:** If a model legitimately repeats a short
delta (e.g., "yes, yes, yes" or repeated emoji), the sliding window might
collide. Mitigations:
- Window size of 8 deltas means 8 consecutive *different* chunks would need
  to be identical — unlikely for normal streaming.
- Only trigger when the loop has been repeated at least `loopThreshold`
  times (1-2 repeats is normal for model hesitation; 3+ is a loop).
- Provide a config kill-switch: `loop_detection.enabled = false` in
  `agent_loop_config`.

**On loop detection — what happens:**

1. The scanner loop returns early with a sentinel error
   (`ErrReasoningLoopDetected`).
2. The agentic loop's `runAgenticLoop` catches this sentinel and:
   - **On first detection:** Appends a system message: "You appear to be
     repeating the same reasoning. Please proceed to the next step or call
     a tool." Then restarts the round with the same messages + the hint.
   - **On second detection in the same request:** Aborts the entire loop and
     returns a user-facing error: "The model is having trouble with this
     question. Try rephrasing your request or switching to a different AI
     model."
3. The error is classified as `"model_loop"` (new category) in
   `classifyErrorCategory` so the UI can show a specific message.

---

## 3. Files Changed

| File | Change | Lines (est.) |
|---|---|---|
| `pkg/services/stream_loop_detector.go` | **New file.** Shared loop detector with `SlidingWindow`, `DeltaHasher`, `Detect(previousHash, window) bool`. | ~80 |
| `pkg/services/stream_loop_detector_test.go` | **New file.** Unit tests for detector: normal stream, exact repeats, near-repeats (should not trigger), empty deltas, single-delta streams. | ~100 |
| `pkg/services/llm_openai.go` | Instantiate detector before scanner loop; feed deltas; check `detector.LoopDetected()` after each delta; return `ErrReasoningLoopDetected` if triggered. | ~20 |
| `pkg/services/llm_anthropic.go` | Same pattern as OpenAI (different stream format but same detector interface). | ~15 |
| `pkg/services/llm_ollama.go` | Same pattern. | ~15 |
| `pkg/services/llm_local.go` | Same pattern. | ~15 |
| `pkg/services/agentic_loop.go` | In `runAgenticLoop`: catch `ErrReasoningLoopDetected` from LLM call; inject corrective hint and retry once; abort on second detection. In `buildToolSystemPrompt` and `buildCompactSystemPrompt`: append timeout hint. | ~25 |
| `pkg/services/agent_loop_config.go` | New config keys: `prompt.timeout_hint`, `loop_detection.enabled`, `loop_detection.window_size`, `loop_detection.threshold`. | ~20 |
| `pkg/models/agent_loop_config.go` | New fields: `PromptTimeoutHint string`, `LoopDetectionEnabled bool`, `LoopDetectionWindowSize int`, `LoopDetectionThreshold int`. | ~8 |
| `pkg/services/discussion_engine.go` | In `classifyErrorCategory`: add case for `ErrReasoningLoopDetected` → `"model_loop"`. In `formatUserError`: add user-friendly message. | ~8 |
| `pkg/services/llm_client.go` | Add `ErrReasoningLoopDetected` sentinel error. | ~3 |
| `pkg/models/database.go` | Migration: add new `agent_loop_config` rows for the 4 new keys. | ~8 |
| **Total** | | **~315** |

---

## 4. Detailed Implementation Plan

### 4.1 Shared Loop Detector (`stream_loop_detector.go`)

```go
package services

import (
    "crypto/sha256"
    "encoding/hex"
)

// ErrReasoningLoopDetected is returned when the stream detector identifies
// a repeating pattern indicating the model is stuck in a reasoning loop.
var ErrReasoningLoopDetected = errors.New("reasoning loop detected — model is repeating the same output")

// StreamLoopDetector watches a stream of text deltas for repetitive patterns.
type StreamLoopDetector struct {
    window        []string
    windowSize    int
    loopThreshold int
    previousHash  string
    sameHashCount int

    // Stats
    TotalDeltas     int
    TotalReasoning  int // approx token count
}

// NewStreamLoopDetector creates a detector with the given window size and
// threshold. windowSize controls how many recent deltas are considered;
// loopThreshold is how many consecutive identical window hashes trigger
// detection.
func NewStreamLoopDetector(windowSize, loopThreshold int) *StreamLoopDetector {
    return &StreamLoopDetector{
        window:        make([]string, 0, windowSize),
        windowSize:    windowSize,
        loopThreshold: loopThreshold,
    }
}

// Feed adds a new delta to the detector. Returns true if a loop was detected.
// isReasoning should be true if this delta is a reasoning/thinking token
// rather than visible content.
func (d *StreamLoopDetector) Feed(delta string, isReasoning bool) bool {
    d.TotalDeltas++
    if isReasoning {
        d.TotalReasoning += len(delta) / 4 // rough token estimate
    }

    d.window = append(d.window, delta)
    if len(d.window) > d.windowSize {
        d.window = d.window[1:]
    }

    // Only check for loops once the window is full.
    if len(d.window) < d.windowSize {
        return false
    }

    hash := d.hashWindow()
    if hash == d.previousHash && d.previousHash != "" {
        d.sameHashCount++
        return d.sameHashCount >= d.loopThreshold
    }

    d.previousHash = hash
    d.sameHashCount = 0
    return false
}

// ExcessiveReasoning returns true if reasoning tokens exceed the limit
// without tool calls being made. The caller resets this by calling
// Feed with isReasoning=false (for tool call deltas).
func (d *StreamLoopDetector) ExcessiveReasoning(limit int) bool {
    return d.TotalReasoning > limit
}

func (d *StreamLoopDetector) hashWindow() string {
    h := sha256.New()
    for _, s := range d.window {
        h.Write([]byte(s))
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

### 4.2 Integration in OpenAI Stream (`llm_openai.go`)

Inside `ChatCompletionWithToolsStreaming`, after the delta parsing block
(~line 470) and before `onEvent`:

```go
// --- before the scanner loop ---
cfg, _ := GetAgentLoopConfig()
detector := NewStreamLoopDetector(
    cfg.LoopDetectionWindowSize,  // default 8
    cfg.LoopDetectionThreshold,   // default 3
)

// --- inside the for scanner.Scan() loop, after parsing delta ---
isReasoning := delta.ReasoningContent != "" // for qwen/openrouter reasoning
content := delta.Content
if content == "" && isReasoning {
    content = delta.ReasoningContent
}

if content != "" {
    if detector.Feed(content, isReasoning) {
        return nil, "", "", fmt.Errorf("%w: model output is repeating", ErrReasoningLoopDetected)
    }
    if detector.ExcessiveReasoning(cfg.MaxReasoningTokens) {
        return nil, "", "", fmt.Errorf("%w: excessive reasoning without progress", ErrReasoningLoopDetected)
    }
}
```

### 4.3 Agentic Loop Handling (`agentic_loop.go`)

In `runAgenticLoop`, after the LLM call:

```go
if llmErr != nil {
    if errors.Is(llmErr, ErrReasoningLoopDetected) {
        loopDetections++
        if loopDetections < 3 { // first: hint, second: hint + abort
            messages = append(messages, ChatMessage{
                Role:    "system",
                Content: "You are repeating yourself. Please make a decision — call a tool or respond to the user now. Do not re-explain the same plan.",
            })
            continue // retry the round
        }
    }
    return fmt.Errorf("LLM call failed: %w", llmErr)
}
loopDetections = 0 // reset on successful round
```

### 4.4 System Prompt Timeout Hint

In `buildToolSystemPrompt`, at the end (before the final return), and
in `buildCompactSystemPrompt`:

```go
// Timeout hint — encourages reasoning models to be concise.
// Overridable via agent_loop_config prompt.timeout_hint.
timeoutHint := cfg.PromptTimeoutHint
if timeoutHint == "" {
    timeoutHint = "You have a limited time budget per round. Be concise in your reasoning — prioritize calling tools over extended planning. If a query fails, fix it quickly rather than re-planning from scratch."
}
sb.WriteString("\n## Time Constraint\n")
sb.WriteString(timeoutHint)
sb.WriteString("\n")
```

---

## 5. Risk/Reward Analysis

### 5.1 Change

Add two complementary defenses against reasoning-model loops: a timeout hint
in the system prompt, and stream-level detection of repetitive output patterns.

### 5.2 Fundamental Goal Impact

**Improves answer delivery.** The primary goal is serving the answer. A
model that times out delivers *no* answer. Both changes reduce the
probability of timeout — the hint by encouraging conciseness; the detector
by aborting loops early and retrying with corrective context.

No impact on **answer correctness** — the hint does not change how the model
interprets schemas, SQL syntax, or the read-only constraint. The detector
only aborts on *exact repetition*, which by definition means no new progress
is being made.

### 5.3 Risk Categories

| Category | Assessment |
|---|---|
| **Answer accuracy** | Nil. Hint is advisory. Detector only triggers on no-progress states. |
| **Answer delivery** | **Positive.** Fewer timeouts, faster error feedback when loops occur. |
| **Data safety** | Nil. No change to SQL execution or connection handling. |
| **Data integrity** | Nil. No schema or migration changes touching user data. |
| **User trust** | **Positive.** "The model is having trouble, try rephrasing" is better than a cryptic "context deadline exceeded." |
| **Regression** | Low. Both changes are additive. Detector is a stream observer — it does not modify deltas or change the tool-calling protocol. |
| **UX/Comfort** | **Positive.** Clear error messages; faster failure when a model cannot answer. |

### 5.4 Failure Modes

1. **False positive loop detection.** A model legitimately repeats a
   short pattern (e.g., "done" or "wait") multiple times. Mitigated by
   requiring a full *window* of 8 deltas to hash identically 3+ times.
   A single repeated token ("yes, yes, yes") is 3 tokens — far smaller
   than the 8-token window. The SHA256 hash of 8 different strings
   repeating identically is astronomically unlikely to collide
   accidentally.

2. **Detector kills a slow-but-valid round.** A model that thinks for 5,000
   reasoning tokens *without looping* is genuinely slow, not broken.
   Mitigated by separating "loop detection" (exact repetition) from
   "excessive reasoning" (token threshold). The excessive reasoning check
   uses a high default (4096 tokens ≈ 10-15 seconds of streaming) and
   can be disabled independently.

3. **Timeout hint makes models *too* brief.** A model that is told "be
   concise" might skip legitimate exploration steps (e.g., not running a
   `SELECT COUNT(*)` to estimate query size). Mitigated by keeping the
   hint qualitative ("be concise in your reasoning") rather than
   quantitative ("respond in under 30 seconds"). Models interpret "be
   concise" as "don't over-explain," not "skip steps."

4. **Config proliferation.** Adding 4 new `agent_loop_config` keys adds
   complexity. Mitigated by sensible defaults (window=8, threshold=3,
   enabled=true, timeout_hint with a good default) so no user action is
   required. The config keys exist mainly as escape hatches.

### 5.5 Decision

**Proceed.** The changes are low-risk, additive, and directly address a
proven failure mode. The timeout hint is a one-line prompt change with
zero code risk. The loop detector is a pure observer with a high bar for
triggering. Both can be deployed independently — if the detector proves
noisy, it can be disabled via config without affecting the hint.

---

## 6. Testing Checklist

### 6.1 Stream Detector Unit Tests (`stream_loop_detector_test.go`)

- [ ] **Normal stream:** 100 unique deltas → no detection
- [ ] **Exact repeat:** 8 identical deltas repeated 3 times → detection on 24th delta
- [ ] **Near repeat (should not trigger):** 8 deltas where 1 character differs per window → no detection
- [ ] **Empty deltas:** Empty strings in window → still hashes, still works
- [ ] **Single delta repeated:** Feed the same single delta 24 times → window fills with same value → should trigger
- [ ] **Short deltas:** 1-2 character deltas repeated → trigger when window is uniform
- [ ] **Reset behavior:** Detection triggered → new unique delta → resets counter → no longer triggered

### 6.2 Integration Testing

- [ ] **OpenAI reasoning model (qwen3-35b-a3b):** Same NPV question that caused the original timeout → verify either answer arrives or clear "model is having trouble" error within 90s (vs. 180s timeout)
- [ ] **Anthropic Claude with extended thinking:** Complex multi-join query → verify no false positive
- [ ] **OpenAI gpt-4o (non-reasoning):** Normal query → verify no regression in streaming or tool calling
- [ ] **Ollama local model (deepseek-r1):** NPV-style question → verify loop detection works with local SSE format
- [ ] **Timeout hint alone:** Disable loop detector (`loop_detection.enabled=false`), keep hint → verify hint doesn't degrade answer quality on simple queries
- [ ] **Config overrides:** Set custom window_size=4, threshold=2 → verify detection triggers faster
- [ ] **Excessive reasoning:** Set `max_reasoning_tokens=100` → run a reasoning model → verify early abort
- [ ] **Dark mode:** Hint text is in system prompt only (not rendered) → no visual impact

### 6.3 Regression Checks

- [ ] Existing conversations with reasoning models still process correctly
- [ ] Non-reasoning models (gpt-4o, claude-3.5-sonnet) show zero behavior change
- [ ] Compact prompt mode includes the timeout hint
- [ ] `agent_loop_config` migration adds new keys without affecting existing overrides
- [ ] Error message classification still correctly identifies auth, rate-limit, and connection errors
- [ ] Read-only invariant unchanged

---

## 7. Future Considerations

### 7.1 Per-Round Timeout (Not in This Plan)

A more aggressive fix would be to give each *round* its own 60-90s timeout
instead of a single 180s total. This prevents a slow round 1 from starving
rounds 2-3. However, this requires restructuring the context passing in
`runAgenticLoop` and `discussion_engine.go`. Deferred to a separate
enhancement to keep this plan focused.

### 7.2 Reasoning Token Budget in Prompt

Some reasoning models (Anthropic's extended thinking) support a
`thinking.budget_tokens` parameter that caps reasoning directly. If/when
the Anthropic client exposes this, it would complement the loop detector.

### 7.3 Model-Specific Hints

Different models have different reasoning verbosity. A future enhancement
could tailor the timeout hint per model family (e.g., "Qwen3: be concise"
vs. "Claude: limit thinking to essential analysis"). This would require a
model-to-hint mapping, deferred for now.

---

*This document should be updated if the implementation diverges from this
plan, and the final risk/reward analysis should be appended to
`RISK_ANALYSIS_LOG.md` when the change ships.*