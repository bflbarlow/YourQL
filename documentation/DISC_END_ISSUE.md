# DISC_END_ISSUE.md — Conversation Ends After One Question

> Companion to `AGENT_READ_FIRST.md`.
> Definitive diagnosis of a bug where conversations become unusable after
> the first successful turn when using models with strict chat templates
> (Qwen3, and potentially others).
>
> **The underlying issue:** `buildToolLlmMessages` converts exploration
> tech-detail messages (`role: "exploration"`) into system messages and
> injects them into the LLM conversation context. Qwen3's Jinja chat
> template enforces that system messages may only appear at the very first
> position of the message list. Multiple consecutive system messages — or
> any system message past position 1 — triggers a Jinja exception. LM
> Studio returns this as an SSE `event: error` line, which the OpenAI
> streaming parser silently ignores, producing an empty `ChatMessage` that
> surfaces as "I received an incomplete response."
>
> **Status:** ✅ Root cause identified (2026-08-11) · 🔧 Fix pending

---

## 0. Quick Status Overview

| # | Item | Status | Effort | Impact |
|---|---|---|---|---|
| 1 | Skip exploration messages in `buildToolLlmMessages` | 🔧 Pending | Trivial | **Critical** — root cause |
| 2 | Surface SSE `event: error` in OpenAI streaming parser | 🔧 Pending | Small | High — prevents silent failures for all models |
| 3 | Preserve assistant `content` text in ToolTranscript | ✅ Shipped | Small | High — real bug, correctly fixed but masked by #1 |
| 4 | Fix clarification messages replaying raw JSON | 🧊 Proposed | Small | Medium |

---

## 1. Symptom

**Reproduction (reliable):** Ask a complex question that triggers a
multi-round agentic loop. The first turn succeeds. Then ask any
follow-up. The response is:

> I received an incomplete response. Could you try rephrasing your question?

Every subsequent question fails identically. New conversations work fine.

**Environment:** Qwen3.6-35b-a3b (reasoning model with ` think`/` think`
tags) served via LM Studio. Provider: OpenAI. Streaming enabled. 120K
context window.

---

## 2. Root Cause

### 2.1 The SSE Error (Definitive Evidence)

Captured via `YOURQL_DEBUG_STREAMS=1` from the second turn's debug stream
(860 bytes):

```
event: error
data: {"error":{"message":"Engine protocol predict request returned 500:
{\"error\":{\"code\":500,\"message\":\"Jinja Exception: System message
must be at the beginning.\",\"type\":\"server_error\"}}"}}
```

LM Studio returns an **SSE `event: error`** (not an HTTP 500, not a
`data:` line). The error is unambiguous:

> **Jinja Exception: System message must be at the beginning.**

Qwen3's chat template raises this exception when it encounters a system
message anywhere other than the very first position in the message list.

### 2.2 How the Error Reaches the Model

`buildToolLlmMessages` (`agentic_loop.go:957`) processes the conversation
history and produces the message list sent to the LLM. For the second
turn, it produces:

```
Position 1:  system     — Full schema + instructions
Position 2:  system     — "[Round 0 — exploration]"
Position 3:  system     — "[Round 0 — exploration]"    ← duplicate
Position 4:  system     — "[Round 1 — exploration]"
Position 5:  system     — "[Round 2 — final]"
Position 6:  assistant  — (tool calls from transcript replay)
Position 7:  tool       — hist_0 result digest
Position 8:  tool       — hist_1 result digest
Position 9:  tool       — hist_2 result digest
Position 10: tool       — hist_3 result digest
Position 11: user       — "Can you summarize that for me?"
```

Positions 2-5 are the four system messages injected by this code at line
~983-985:

```go
if role == "exploration" {
    role = "system"   // ← converts exploration debug messages to system messages
}
messages = append(messages, ChatMessage{Role: role, Content: content})
```

These exploration messages are tech-detail debug labels created by
`storeTechDetail()` in the agentic loop. Their content is labels like
`"[Round 0 — exploration]"` — meaningless to an LLM. They exist solely
for the tech-details UI panel.

Qwen3's chat template sees position 2 is a system message and raises
`Jinja Exception: System message must be at the beginning.`

### 2.3 Why the First Turn Succeeds

In the **first turn**, the conversation history is empty. `buildToolLlmMessages`
sends exactly 2 messages:

```
Position 1: system (full prompt)
Position 2: user   ("Calculate NPV...")
```

Only one system message at position 1 — valid.

The first turn's multi-round loop appends assistant and tool messages
incrementally, keeping the single system message at position 1. No
additional system messages are injected because exploration messages
are only replayed from history on subsequent turns.

### 2.4 Why the Error Is Silently Swallowed

The OpenAI streaming parser in `llm_openai.go` processes SSE lines:

```go
for scanner.Scan() {
    line := scanner.Text()
    if !strings.HasPrefix(line, "data: ") {
        continue   // ← skips "event: error" lines entirely
    }
    data := strings.TrimPrefix(line, "data: ")
    if data == "[DONE]" {
        continue
    }
    // ... parse chunk ...
}
```

LM Studio's error comes as `event: error` followed by `data: {...}`. The
`event: error` line is skipped because it doesn't start with `data: `.
The `data: {"error":...}` line would be parsed, but the chunk structure
doesn't match the expected `choices[0].delta` format, so it's silently
dropped. The scanner then reaches EOF. The assembled `ChatMessage` has
empty content and zero tool calls. The agentic loop treats it as an empty
response → "I received an incomplete response."

The actual error is never surfaced to the user or to the logs. If the
error had been surfaced, this bug would have been found immediately
instead of requiring an hours-long investigation.

---

## 3. Why Prior Fixes Didn't Resolve the Issue

### 3.1 Fix 1: Content text preservation (✅ Correct but masked)

The fix to preserve assistant `content` text in the `ToolTranscript` was
a real and necessary fix. Without it, reconstructed history showed tool
calls with no explanation text. However, this fix couldn't resolve the
issue because the messages never even reached the model — Qwen3's
template rejected the request before any content was processed.

The content text is now correctly preserved in transcripts. This fix
should be kept.

### 3.2 The real issue was invisible

The 1-chunk "response" with finish_reason `stop` and no content made the
failure look like the model chose to say nothing. In reality, the model
never processed the request — LM Studio's template layer rejected it
before inference even began. The SSE error was silently discarded.

---

## 4. Broken Functionality to Review

### 4.1 Agentic Loop Changes

**File:** `pkg/services/agentic_loop.go`

**Change made:** Added 13 lines after `pendingFinal` creation to capture
`response.Content` as a `respond_to_user` transcript action (§3.1 above).

**Verdict:** ✅ **KEEP.** This is a correct fix for a real bug. The
content text was not being preserved in transcripts for history replay.
This change is additive, follows the existing pattern (matching the
plain-text respond_to_user path at line ~1120), and compiles cleanly. No
regression risk.

### 4.2 No other changes were made

The only code change during this investigation was Fix 1 in
`agentic_loop.go`. The debug stream capture (`YOURQL_DEBUG_STREAMS=1`)
was pre-existing infrastructure. No other files were modified.

---

## 5. Fix Plan

### 5.1 Fix: Skip exploration messages in buildToolLlmMessages (primary)

**File:** `pkg/services/agentic_loop.go`, function `buildToolLlmMessages`

**Change:** Skip messages with `role == "exploration"` instead of
converting them to system messages. Exploration messages are debug
data for the tech-details UI panel; they have no value as LLM context.

**Before (line ~983-985):**
```go
if role == "exploration" {
    role = "system"
}
messages = append(messages, ChatMessage{Role: role, Content: content})
```

**After:**
```go
if role == "exploration" {
    continue  // skip — debug data, not model context
}
messages = append(messages, ChatMessage{Role: role, Content: content})
```

**Alternative approach:** Filter at the role check above:
```go
if role == "user" || role == "assistant" {
    // process...
}
// exploration messages are silently skipped (no branch)
```

Either approach is equivalent. The first is more explicit.

**Acceptance criteria:**
- Second turn's message list has exactly 1 system message (the prompt) at position 1
- No `"[Round N — ...]"` labels appear in LLM messages
- "Can you summarize that for me?" returns a text response
- "How many orders are in the database?" returns a count
- Tech details UI still shows exploration debug data (stored separately, not in LLM messages)

**Risk:** None. Exploration messages are debug labels like `"[Round 0 — exploration]"`.
They were never intended as model context. The tool transcript replay
(`buildToolMessages`) already provides all the actual conversation history
the model needs. Removing these from the message list does not lose any
meaningful information.

### 5.2 Fix: Surface SSE errors in OpenAI streaming parser (secondary)

**File:** `pkg/services/llm_openai.go`, function `ChatCompletionWithToolsStreaming`

**Change:** Detect and surface `event: error` SSE lines so that LM Studio
template errors (and similar backend errors) are not silently swallowed.

**Approach:** Track the current SSE event type. When an `event: error`
line is seen, capture the following `data:` line as an error message and
return it as a Go error.

```go
var currentEvent string
for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "event: ") {
        currentEvent = strings.TrimPrefix(line, "event: ")
        continue
    }
    if !strings.HasPrefix(line, "data: ") {
        continue
    }
    data := strings.TrimPrefix(line, "data: ")
    if currentEvent == "error" {
        return nil, "", "", fmt.Errorf("SSE error from backend: %s", data)
    }
    // ... rest of parsing ...
}
```

**Acceptance criteria:**
- Template errors produce a loggable Go error instead of an empty response
- The error message appears in the tech details or logs
- Non-error SSE events are unaffected

**Risk:** Low. Additive — adds error detection without changing existing
parsing paths. Only fires on `event: error` SSE lines.

### 5.3 Fix: Clarification messages replaying raw JSON (deferred)

See prior analysis in §4.2 of the original document. This is a separate
bug that causes pollution on turns after a clarification response but is
not the blocking issue addressed here.

---

## 6. Raw Debug Data

### 6.1 Stream Size Comparison (conversation 82)

```
First turn, Round 0:   76,442 bytes   (messages=2,  successful — 2 tool calls)
First turn, Round 1:    9,583 bytes   (messages=6,  successful — 1 exploration)
First turn, Round 2:   11,398 bytes   (messages=8,  successful — 1 exploration)
First turn, Round 3:   99,118 bytes   (messages=10, successful — final query)
Second turn, Round 0:     860 bytes   (messages=11, FAILED — template error)
Third turn, Round 0:      860 bytes   (messages=11, FAILED — template error)
```

### 6.2 LM Studio Error (verbatim, from debug stream)

```
event: error
data: {"error":{"message":"Engine protocol predict request returned 500: {\"error\":{\"code\":500,\"message\":\"\\n------------\\nWhile executing CallExpression at line 85, column 32 in source:\\n...first %}↵            {{- raise_exception('System message must be at the beginnin...\\n                                           ^\\nError: Jinja Exception: System message must be at the beginning.\",\"type\":\"server_error\"}}"},"message":"Engine protocol predict request returned 500: {\"error\":{\"code\":500,\"message\":\"\\n------------\\nWhile executing CallExpression at line 85, column 32 in source:\\n...first %}↵            {{- raise_exception('System message must be at the beginnin...\\n                                           ^\\nError: Jinja Exception: System message must be at the beginning.\",\"type\":\"server_error\"}}"}}
```

### 6.3 Message List Sent to LM Studio (conversation 82, second turn)

```
1.  system     — Full schema + instructions + exploration rules + tools
2.  system     — "[Round 0 — exploration]"     ← template rejects here
3.  system     — "[Round 0 — exploration]"     ← duplicate
4.  system     — "[Round 1 — exploration]"
5.  system     — "[Round 2 — final]"
6.  assistant  — content: "Now I have the data needed..." + 4 tool_calls
7.  tool       — hist_0 result (payments exploration, 20 rows)
8.  tool       — hist_1 result (orders exploration, 15 rows)
9.  tool       — hist_2 result (revenue exploration, 67 rows)
10. tool       — hist_3 result (final NPV, 50 rows)
11. user       — "Can you summarize that for me?"
```

### 6.4 Live Turn Message Lists (for comparison)

**Round 0:** messages=2
```
1. system — Full prompt
2. user   — "Calculate NPV..."
```

**Round 1:** messages=6
```
1. system    — Full prompt
2. user      — "..."
3. assistant — content + 2 tool_calls
4. tool      — result 1
5. tool      — result 2
6. assistant — (current round response)
```

All rounds in the first turn have exactly one system message at position
1. No exploration messages are present because they are only injected
when replaying history on subsequent turns.

---

## 7. Testing Checklist

- [ ] First turn: complex multi-round query — verify answer correctness
- [ ] Second turn: "Can you summarize that for me?" — verify non-empty response
- [ ] Third turn: "How many orders are in the database?" — verify answer
- [ ] Test with Qwen3 via LM Studio (the original failing configuration)
- [ ] Test with other strict-template models if available
- [ ] Test with OpenAI (GPT-4o) — verify no regression
- [ ] Test with Anthropic (Claude) — verify no regression
- [ ] Test with Ollama (fallback protocol) — verify no regression
- [ ] Verify tech details toggle still shows exploration debug data
- [ ] Verify tool transcript replay still works correctly
- [ ] Verify streaming errors are surfaced rather than silently swallowed

---

## 8. Lessons Learned

1. **SSE `event:` lines must be handled.** The OpenAI streaming parser
   only processes `data:` lines. Backend errors in `event: error` format
   are silently discarded. All streaming parsers should detect and
   surface error events.

2. **Exploration messages are debug data, not model context.** The
   `[Round N — ...]` labels exist for the tech-details UI. Injecting
   them as system messages into LLM context is harmful — they provide
   no value to the model and break models with strict chat templates.

3. **Chat template compatibility matters.** Not all OpenAI-compatible
   endpoints are equal. Models served via LM Studio apply their native
   Jinja chat templates, which may enforce constraints that the standard
   OpenAI API does not. System message placement is the most common
   constraint (Qwen3, Llama 3, Mistral all enforce this to varying
   degrees).

4. **`YOURQL_DEBUG_STREAMS=1` is the single most valuable diagnostic
   tool.** It provided the definitive error message that identified the
   root cause. Without it, the investigation would have continued
   guessing about model behavior based on indirect evidence.

---

## 9. Related Documents

- `AGENT_READ_FIRST.md` — Project charter and risk framework
- `DISCUSSION_CONTEXT_ISSUES.md` — Prior context exhaustion diagnosis
- `DISC_CONTEXT_FIX.md` — Execution plan for context exhaustion fixes
- `RISK_ANALYSIS_LOG.md` — Risk/reward analysis log
- `FINAL_MESSAGE.md` — One-shot finality rule design