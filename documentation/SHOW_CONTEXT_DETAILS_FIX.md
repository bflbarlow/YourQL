# SHOW CONTEXT & TOKEN DETAILS — Fix Plan

> The "Show Context & Token Details" checkbox in the conversation gear
> popover currently does nothing. This is a data gap introduced by the
> migration from JSON-response architecture to tool/function-calling
> architecture.

**Status: Diagnosis complete. Awaiting decision on fix approach.**

---

## 1. How It's Supposed to Work

When `showContextDetails` is enabled in `ConversationView.svelte`, two
additional meta-tags should appear next to the conversation title in the
chat header:

```
🟢 Classic Models   🟢 Qwen3.7   1.2K↑ 900↓ tokens   5 msgs
```

The frontend computes these from message metadata via the `tokenSummary`
derived value (`ConversationView.svelte:223`):

```javascript
for (const m of conversationMessages) {
  const payload = parsePayload(m.metadata) // JSON.parse
  if (!payload || !payload.response_json) continue
  const resp = JSON.parse(payload.response_json)
  const usage = resp.usage || resp.usage_info  // OpenAI or Anthropic format
  promptTotal += usage.prompt_tokens || usage.input_tokens
  completionTotal += usage.completion_tokens || usage.output_tokens
}
```

It requires `payload.response_json` in each assistant message's metadata,
containing the full LLM API response with a `usage` (OpenAI) or
`usage_info` (Anthropic) block.

The visible message count (`5 msgs`) is computed separately from
`conversationMessages.filter(m => m.role === 'user' || m.role === 'assistant')`.

The conditional guard is `showContextDetails && tokenSummary.msgCount > 0`
(`line 364`). Since `msgCount` is always 0 in the current code, nothing
renders.

---

## 2. Why It's Broken

The tool-calling path (`agentic_loop.go`) never stores the LLM response
JSON in assistant message metadata. Evidence at `agentic_loop.go:334`:

```go
llmContent := "" // no raw LLM response JSON in the tool-calling path
```

The `renderToolQueryResults` function builds metadata with only:

```go
metadataMap := map[string]interface{}{"content_type": "html"}
// optionally chart_config
```

No `response_json`. No token `usage`. The old JSON-response protocol in
`discussion_engine.go` used to store this, but that code path is now dead.

Each turn of the agentic loop *does* store a `TechDetail` record as an
*exploration* message (via `storeTechDetail`), and the `TechDetail`
struct has `response.raw_output` (the full raw LLM response). But:

- The raw output is stored as a plain string, not parsed into a `usage`
  block.
- `TechDetail` has no `response_json` or `usage` fields.
- Exploration messages are filtered out when `showTechDetails` is off,
  so they can't be the sole source.

---

## 3. Data Flow — What Exists per Turn

During each round of the agentic loop:

| Data | Where | Contains usage? |
|---|---|---|
| `response *ChatMessage` | In-memory only, the parsed LLM response | ✅ Yes — `response.Usage` (Go struct) |
| `rawResponse string` | In-memory only, the raw JSON from the LLM API | ✅ Yes — as JSON |
| `TechDetail` (stored as exploration message metadata) | DB, visible when `showTechDetails` is on | ❌ No — struct has no usage fields |
| Assistant message metadata (`renderToolQueryResults`) | DB, always visible | ❌ No — only `content_type: "html"` |

The token usage data exists in-memory during the call but is discarded
when the function returns. It is never persisted.

---

## 4. Fix Options

### Option A: Store `response_json` in assistant message metadata (recommended)

**What:** After each assistant message is created, marshal the full LLM
response (with `usage` block) into the metadata's `response_json` field.

**Changes needed (Go):**

1. In `agentic_loop.go:renderToolQueryResults`, add `response_json` to
   `metadataMap`. The raw LLM response JSON is already available as
   `rawResponse` in the calling scope — pass it through or re-marshal
   `response.Usage`.

2. In `agentic_loop.go:handleRespond`, do the same for `respond_to_user`
   messages.

3. Cumulative token totals would need to be tracked across rounds
   (since one user message may produce 4-5 LLM calls). The
   `tokenSummary.msgCount` counts per-assistant-message, so storing
   per-message usage is the simplest approach.

**Frontend changes needed:** None. The existing `tokenSummary` code
already scans `payload.response_json` — it just needs the data to be
there.

**Pros:**
- Minimal change — 3-4 lines of Go per message creation site.
- Frontend works as-is.
- Data is self-contained per message (no cross-message aggregation).

**Cons:**
- Storing the full LLM response JSON in metadata bloats the DB slightly
  (but it's a local SQLite DB — negligible).
- `rawResponse` needs to be threaded through to the render functions.
  Currently `handleRespond` doesn't have access to it.

---

### Option B: Add `usage` fields to `TechDetail` and count from exploration messages

**What:** Add `prompt_tokens` and `completion_tokens` to the
`TechDetail.Response` struct. Populate them from the in-memory
`response.Usage`. Frontend reads token counts from exploration messages
instead of assistant message metadata.

**Changes needed (Go):**

1. Add fields to `TechDetail.Response`:
   ```go
   PromptTokens     int `json:"prompt_tokens,omitempty"`
   CompletionTokens int `json:"completion_tokens,omitempty"`
   ```

2. Populate in `runAgenticLoop` where `logRound` is called.

**Frontend changes needed:**

1. Replace the `tokenSummary` derived to sum tokens from
   `m.payload.response.prompt_tokens` + `.completion_tokens` instead of
   `m.payload.response_json.usage...`.

**Pros:**
- Data is already stored per-round — just adding missing fields.
- Clean separation: tech detail stays in TechDetail.

**Cons:**
- Frontend needs rework. The existing code scans *assistant* messages;
  exploration messages are a different role and are filtered out when
  `showTechDetails` is off.
- The `showContextDetails` toggle would become dependent on
  `showTechDetails` (or would need its own filter logic).
- Confusing UX: "Show Context & Token Details" would show data from
  exploration messages that are gated behind a different toggle ("Show
  Technical Details").

---

### Option C: Track cumulative tokens in-memory and write at the end

**What:** Accumulate token usage across all rounds of a single user
message, then store the total in the final assistant message's metadata.

**Pros:**
- Cleanest per-message: each assistant message has its total turn cost.

**Cons:**
- Doesn't account for multi-message responses (respond_to_user separate
  from query_database).
- Token data isn't stored per-round, losing granularity for debugging.
- More complex — needs a token accumulator passed through the loop.

---

## 5. Recommendation: Option A

Option A is the simplest, touches the fewest files, requires no frontend
changes, and is consistent with the original design (which expected
`response_json` in metadata). The only work is threading `rawResponse`
(or a usage-only struct) through the two render functions.

The cumulative token display in the header would show the sum across all
assistant messages visible to the user — matching the existing
`tokenSummary` design.

## 6. Scope of Changes (Option A)

### 6.1 `agentic_loop.go`

**`handleRespond` (line ~263):** Currently creates a message with
`metadata := {"content_type": "html"}`. Needs to also include
`response_json` with token usage. Requires threading the LLM response
usage through.

**`renderToolQueryResults` (line ~304):** Same — add `response_json` to
`metadataMap`. The LLM response is available in the calling scope via
`rawResponse` or `response`.

**Calling sites:** The functions that call `handleRespond` and
`renderToolQueryResults` need to pass the usage data. This means:

- In `handleRespond` (called at lines 796 and 988): the LLM response
  `ChatMessage` has a `Usage` field — pass it through.
- In `pendingFinalResult.render()` (line 121): the `pendingFinalResult`
  struct would need to carry the usage data from the final round.

### 6.2 Files touched

| File | Lines changed | Type |
|---|---|---|
| `pkg/services/agentic_loop.go` | ~15 lines | Add `response_json` to metadata in 3 locations |
| `pkg/services/agentic_loop.go` | ~5 lines | Thread usage through render functions |
| Frontend | 0 lines | No changes needed |

### 6.3 Testing

- Enable "Show Context & Token Details" in a conversation
- Send a message and verify token counts appear in the header
- Verify counts accumulate across multiple messages
- Verify with different LLM providers (OpenAI `usage.prompt_tokens`,
  Anthropic `usage_info.input_tokens`)
