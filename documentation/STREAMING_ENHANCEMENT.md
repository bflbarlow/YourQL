# Streaming Enhancement

**Status:** Design specification. Not yet implemented.

**Summary:** Add Server-Sent Events (SSE) streaming to every LLM provider's
`ChatCompletionWithTools` path, forward model output to the frontend in real
time via Wails events, and render a live-updating assistant bubble that
shows the model's thinking, tool calls, and response as they form — replacing
the current static spinner with visible progress.

This document assumes familiarity with the tool-calling architecture
([`TOOL_CALL_ENHANCEMENT.md`](TOOL_CALL_ENHANCEMENT.md)) and the project
charter ([`AGENT_READ_FIRST.md`](AGENT_READ_FIRST.md)), whose goal priority
order and risk framework govern every design decision below.

---

## 1. Motivation

### 1.1 Current state

Every LLM call in every provider is a blocking request-response:

| Provider | Method | Streaming? |
|---|---|---|
| OpenAI / OpenRouter | HTTP POST, `stream: false` (default) | No |
| Anthropic | HTTP POST, no streaming header | No |
| Ollama | `Stream: false` (explicit) | No |
| Local / custom | `stream: false` (explicit) | No |

`runAgenticLoop` calls `ChatCompletionWithTools` synchronously and blocks
until the full `*ChatMessage` returns — no incremental delivery, no
callback, no channel. The user sees a spinner with static phase labels
(`"Thinking with LLM..."`, `"Running query..."`) set by the `onPhase`
callback *before* each LLM call.

### 1.2 The user experience problem

With the old JSON-response protocol, LLM calls were fast — the model
produced a small JSON object (a few hundred tokens) and the user saw results
in 2–4 seconds. With tool calling and reasoning models (qwen/qwen3.7-plus,
o1, o3, claude-sonnet with extended thinking, deepseek-r1), a single call
can involve:

1. **Reasoning phase** — 1–30 seconds of internal chain-of-thought (model
   thinks before producing tool calls). The user sees nothing.
2. **Tool call generation** — fractional seconds (small JSON arguments).
3. **Multiple rounds** — exploration queries loop 2–3 times, each with its
   own reasoning phase.

In the worst case the user stares at a spinner for 30+ seconds before seeing
any output. This degrades the experience and undermines trust — the user
can't distinguish "the model is thinking productively" from "the app is
stuck."

### 1.3 What streaming buys

All four provider types support streaming via SSE (Server-Sent Events) or
equivalent mechanisms:

| Provider | Streaming protocol |
|---|---|
| OpenAI / OpenRouter | SSE: `data: {"choices":[{"delta":{"content":"...","tool_calls":[...]}}]}\n\n` |
| Anthropic | SSE: `data: {"type":"content_block_delta","delta":{"text":"..."}}\n\n` / `data: {"type":"content_block_start","content_block":{"type":"tool_use",...}}\n\n` |
| Ollama | NDJSON: `{"message":{"content":"..."}}\n` (one JSON object per line) |
| Local / custom | SSE (OpenAI-compatible) or NDJSON depending on backend |

With streaming:
- Reasoning text appears character-by-character in the chat bubble.
- Tool calls appear as structured cards that build up as arguments arrive.
- The response text appears incrementally after tool results come back.
- The user sees continuous, visible progress — no dead-air spinner.

This directly serves the **Appeal / Comfort** secondary goal
(`AGENT_READ_FIRST.md` §0): *"The interface should feel modern, clean, and
pleasant to use — clear feedback on what's happening."*

---

## 2. Goals and Non-Goals

**Goals:**
- Stream model output (text deltas, tool call formation, final response) to
  the frontend in real time for all four provider types.
- Preserve the existing blocking `ChatCompletionWithTools` path unchanged —
  streaming is additive, not a rewrite. Callers that don't need streaming
  (e.g., `summarizeResults`) continue to use the blocking path.
- Render a single streaming bubble that accumulates text and transitions
  into the final rendered message (results table, chart, markdown).
- Handle the reasoning phase specially — show it as a collapsible
  "Thinking…" section inside the streaming bubble, so the user can see
  progress without being overwhelmed by raw monologue.
- The agentic loop's core logic (tool call processing, SQL execution, budget
  management) remains identical. Streaming only changes *when* data arrives
  at the loop, not what the loop does with it.

**Non-goals:**
- This is not a UI redesign. The streaming bubble replaces the spinner
  during an active LLM call; the final rendered message (results table,
  charts, markdown) is identical to the non-streaming path.
- This is not a rewrite of the rendering pipeline. The frontend still
  receives `content_type: "html"` assistant messages via the conversation
  history API; streaming is a transient, in-flight mechanism that lives
  alongside the existing message persistence.
- This is not an attempt to stream tool execution results incrementally.
  `query_database` results arrive as a complete `*QueryResult` struct and
  are rendered atomically, exactly as today.

---

## 3. Provider-Level Streaming

### 3.1 Shared types

These types are added to `pkg/services/llm_client.go` alongside the existing
`Tool`/`ToolCall`/`ChatMessage` vocabulary:

```go
// StreamEvent is emitted for each chunk of a streaming LLM response.
type StreamEvent struct {
    Type StreamEventType `json:"type"`

    // ContentDelta: incremental text from the model (may be reasoning or
    // final response — the frontend decides how to render based on context).
    Content string `json:"content,omitempty"`

    // ToolCallDelta: a fragment of a tool call being constructed.
    // ToolName is sent on the first chunk for a given ToolCallID (the
    // frontend uses it to label the tool card). Arguments are the
    // incremental JSON string fragment — concatenated across chunks.
    ToolCallID string `json:"tool_call_id,omitempty"`
    ToolName   string `json:"tool_name,omitempty"`
    Arguments  string `json:"arguments,omitempty"`

    // Done: the stream has finished. FinishReason indicates why
    // ("stop", "tool_calls", "length", "error").
    FinishReason string `json:"finish_reason,omitempty"`

    // Error: a non-fatal error during streaming (e.g. a chunk parse
    // failure). The stream may still continue after this event.
    Error string `json:"error,omitempty"`

    // ReasoningStart / ReasoningEnd: brackets around model reasoning.
    // Some providers (OpenAI o-series, Anthropic extended thinking)
    // explicitly mark reasoning boundaries. Others don't — the frontend
    // can infer reasoning from the gap between the user message and the
    // first tool call or final response.
    ReasoningStart bool `json:"reasoning_start,omitempty"`
    ReasoningEnd   bool `json:"reasoning_end,omitempty"`

    // ToolCallStart: a new tool call is beginning. Sent when the model
    // starts emitting a tool call (before arguments arrive). The frontend
    // uses this to create a placeholder card.
    ToolCallStart bool `json:"tool_call_start,omitempty"`

    // ToolCallEnd: a tool call's arguments are complete.
    ToolCallEnd bool `json:"tool_call_end,omitempty"`
}

type StreamEventType string

const (
    StreamContentDelta   StreamEventType = "content_delta"
    StreamToolCallDelta  StreamEventType = "tool_call_delta"
    StreamReasoningStart StreamEventType = "reasoning_start"
    StreamReasoningEnd   StreamEventType = "reasoning_end"
    StreamToolCallStart  StreamEventType = "tool_call_start"
    StreamToolCallEnd    StreamEventType = "tool_call_end"
    StreamDone           StreamEventType = "done"
    StreamError          StreamEventType = "error"
)
```

### 3.2 Interface (additive, not mutated)

A new method is added to `LLMClient` — the two existing methods are
untouched:

```go
type LLMClient interface {
    ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error)
    ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error)
    ChatCompletionWithTools(ctx context.Context, messages []ChatMessage, tools []Tool) (msg *ChatMessage, requestJSON, responseJSON string, err error)

    // ChatCompletionWithToolsStreaming sends tools and streams the response
    // via onEvent. The final assembled *ChatMessage is returned when the
    // stream completes (identical shape to the blocking path — callers
    // process it identically). The onEvent callback is called synchronously
    // from the same goroutine; callers must not block the callback for long.
    ChatCompletionWithToolsStreaming(ctx context.Context, messages []ChatMessage, tools []Tool, onEvent func(StreamEvent)) (msg *ChatMessage, requestJSON, responseJSON string, err error)
}
```

The callback receives `StreamEvent` values as chunks arrive. The returned
`*ChatMessage` is the fully assembled result — `Content` contains all
concatenated text deltas, `ToolCalls` contains all fully-formed tool calls
with complete `Arguments` JSON strings. The agentic loop processes this
identically to the blocking path. The callback is purely a side channel for
the frontend.

### 3.3 OpenAI / OpenRouter implementation

**SSE format:**

Each chunk is a line starting with `data: ` followed by a JSON object.
Empty lines (just `\n`) separate chunks. The stream ends with
`data: [DONE]\n\n`.

Chunk structure:
```json
{
  "choices": [{
    "index": 0,
    "delta": {
      "content": "some text",        // text delta
      "reasoning_content": "...",    // o-series reasoning
      "tool_calls": [{
        "index": 0,
        "id": "call_abc",            // sent on first chunk only
        "function": {
          "name": "query_database",  // sent on first chunk only
          "arguments": "{\"sql\""    // fragment, concatenate across chunks
        }
      }]
    },
    "finish_reason": "stop"          // null until final chunk
  }]
}
```

**Implementation approach:**

1. Set `stream: true` in the request.
2. Read the response body line by line with a `bufio.Scanner`.
3. For each `data: ` line, unmarshal the JSON.
4. Extract `choices[0].delta`:
   - If `reasoning_content` is present and this is the first reasoning
     chunk: emit `StreamReasoningStart` then `StreamContentDelta`.
   - If `reasoning_content` is present: emit `StreamContentDelta`.
   - If `content` is present (and no reasoning is active): emit
     `StreamContentDelta`.
   - If `tool_calls` array is non-empty: for each entry, check the
     `index` field to identify the tool call slot. If `id` is present,
     emit `StreamToolCallStart`. Concatenate `function.arguments`
     fragments per slot. When fragments stop arriving for a slot and
     the next slot/event type arrives, emit `StreamToolCallEnd`.
   - If `finish_reason` is set: emit `StreamDone`.
5. Reconstruct the full `ChatMessage` from accumulated deltas.
6. Return the assembled message.

**Edge cases:**
- Tool calls with empty arguments: emit `StreamToolCallStart`/`End` with
  `Arguments: "{}"`.
- Multiple tool calls in one response: indexed by `tool_calls[].index`.
- Reasoning content from o-series models: explicitly bracketed with
  `StreamReasoningStart`/`End`.
- Connection drops mid-stream: return a partial message + error. The
  agentic loop's error handling treats this like any other LLM failure.

### 3.4 Anthropic implementation

**SSE format:**

Anthropic's streaming uses a different event structure. Events include:

```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_xxx","name":"query_database","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"sql"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null}}

event: message_stop
data: {"type":"message_stop"}
```

**Implementation approach:**

1. Set `stream: true` in the request.
2. Read the response line by line. Each event starts with `event: <type>`
   followed by `data: <json>`.
3. Accumulate state per content block index:
   - `content_block_start` with `type: "text"`: begin accumulating text.
   - `content_block_delta` with `text_delta`: emit `StreamContentDelta`.
   - `content_block_start` with `type: "tool_use"`: emit
     `StreamToolCallStart`.
   - `content_block_delta` with `input_json_delta`: emit
     `StreamToolCallDelta`.
   - When a new content block starts or the stream ends: emit
     `StreamToolCallEnd` for the previous tool_use block.
4. `message_delta` with `stop_reason`: emit `StreamDone`.
5. Reconstruct `ChatMessage` from accumulated content blocks.

**Edge cases:**
- Anthropic's thinking/reasoning (Claude extended thinking): appears as
  `content_block` with `type: "thinking"`. Map to
  `StreamReasoningStart`/`End` + `StreamContentDelta`.
- Multiple tool_use blocks in one turn: handled by the index-based
  accumulation above.

### 3.5 Ollama implementation

**NDJSON format:**

Ollama streams one JSON object per line (NDJSON), with `stream: true` in
the request:

```json
{"message":{"role":"assistant","content":"SELECT"}}
{"message":{"role":"assistant","content":" *"}}
{"message":{"role":"assistant","content":" FROM"}}
...
{"done":true,"total_duration":1234567890}
```

Note: Ollama does NOT natively stream structured tool calls. The model
returns raw text (the JSON routing prefix from the fallback protocol), not
`tool_calls` objects. Streaming here means streaming the raw text as it
forms, then parsing the fallback JSON prefix at the end exactly as the
non-streaming path does.

**Implementation approach:**

1. Set `stream: true` in the request.
2. Read the response line by line.
3. For each line, unmarshal the JSON.
4. If `message.content` is present: emit `StreamContentDelta`.
5. If `done` is true: concatenate all content deltas, parse with
   `parseFallbackResponse`, emit `StreamDone`, return the assembled
   `*ChatMessage`.

**Edge cases:**
- The model may interleave the JSON routing prefix with prose within a
  single streaming response. The parser only activates on the final
  assembled text — streaming deltas are raw passthrough.
- Since Ollama models don't have native tool calling, there are no
  `StreamToolCallStart`/`End` events to emit. The frontend simply sees
  text streaming in.

### 3.6 Local / custom endpoint implementation

Same as the OpenAI SSE implementation (§3.3) since most local backends
(lm-studio, llama-server, text-generation-webui) expose an
OpenAI-compatible SSE endpoint. The legacy `/completion` endpoint fallback
in `ChatCompletionWithPayload` is not used for streaming — only the
`/v1/chat/completions` path is supported.

For backends that do not support streaming at all, `ChatCompletionWithToolsStreaming`
falls back to the blocking `ChatCompletionWithTools` path internally and
emits exactly one `StreamContentDelta` containing the full response text
followed by `StreamDone`. The agentic loop is unaware of this fallback —
it receives the same event sequence, just collapsed into a single tick.

---

## 4. The Streaming Agentic Loop

### 4.1 Design principle: loop logic unchanged

The agentic loop's core structure — process tool calls, execute SQL, manage
budgets — is identical. The only change is replacing the blocking
`ChatCompletionWithTools` call with `ChatCompletionWithToolsStreaming`. The
callback (`onEvent`) is forwarded to the frontend via Wails events. When the
stream completes, the assembled `*ChatMessage` is processed exactly as
before.

```go
// Before (current, blocking):
response, reqJSON, respJSON, err := client.ChatCompletionWithTools(ctx, messages, tools)

// After (streaming) — onStream is a parameter threaded down from app.go,
// see §4.2 for how it's constructed. It is nil when streaming is disabled.
var response *ChatMessage
var reqJSON, respJSON string
var err error
if onStream != nil {
    response, reqJSON, respJSON, err = client.ChatCompletionWithToolsStreaming(ctx, messages, tools, onStream)
} else {
    response, reqJSON, respJSON, err = client.ChatCompletionWithTools(ctx, messages, tools)
}
```

### 4.2 Forwarding events to the frontend

This project uses Wails v2 (`github.com/wailsapp/wails/v2/pkg/runtime`).
`runtime.EventsEmit()` requires the Wails context — which is only available
inside Wails-bound `App` methods (i.e., `app.go`). Service-layer functions
like `runAgenticLoop` do not have access to this context.

The pattern mirrors how `onPhase func(string)` already works today: `app.go`
creates a closure with access to `a.ctx` and passes it as a parameter.

```go
// app.go — the Wails-bound method:
func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
    onStream := func(ev services.StreamEvent) {
        // Tag with conversation ID so the frontend only renders
        // streaming content for the active conversation.
        runtime.EventsEmit(a.ctx, "llm:stream", map[string]interface{}{
            "conversation_id": conversationID,
            "event":           ev,
        })
    }
    return services.ProcessUserMessage(conversationID, userMessage,
        func(phase string) {
            runtime.EventsEmit(a.ctx, "processingPhase", phase)
        },
        onStream,
    )
}
```

```go
// pkg/services/agentic_loop.go — the service layer:
// runAgenticLoop gains an onStream func(StreamEvent) parameter.
// When non-nil, the callback is invoked for each stream chunk.
// The callback MUST be fast — it blocks the SSE reader goroutine.
func runAgenticLoop(
    ctx context.Context,
    query *models.Query,
    client LLMClient,
    messages []ChatMessage,
    dbConnection *models.DataSource,
    conversation *models.Conversation,
    maxExplorationRounds int,
    maxErrorRetries int,
    safetyMode ExplorationSafetyMode,
    userMessage string,
    skillsContent string,
    onStream func(StreamEvent),   // nil when streaming is disabled
) error {
```

The frontend listens on `llm:stream` and updates the streaming bubble only
when `event.data.conversation_id` matches the currently-viewed conversation.

When `onStream` is `nil`, `runAgenticLoop` uses the blocking
`ChatCompletionWithTools` path (Phase 1–3 behavior, gated by the feature
flag). In Phase 4, `onStream` is always non-nil and the streaming path is
unconditional.

### 4.3 Streaming transcript for history replay

The `ToolTranscript` (§6.2 of the tool-calling spec) already captures each
tool call's final state. Streaming adds no new persistence requirements —
the stream is transient. If the frontend is closed mid-stream, the
conversation message is never created (the agentic loop only persists on
completion, as today), and the user's next load shows no partial message.

---

## 5. Frontend Streaming UI

### 5.1 Streaming state machine

The frontend maintains a per-conversation streaming state:

```
IDLE → STREAMING → (STREAMING_TOOL_WAIT) → STREAMING_RESPONSE → DONE
                                        ↓
                                   DONE (tool call only, no text)
```

| State | What the user sees |
|---|---|
| `IDLE` | Normal conversation history |
| `STREAMING` | A streaming bubble with text appearing character-by-character. If `reasoning_start` is received, the text is placed in a **collapsed** "💭 Thinking…" details block (see §5.3 for the postmortem constraint). |
| `STREAMING_TOOL_WAIT` | The streaming bubble shows a tool card ("Running query_database…") with a spinner. The model is executing a tool call and awaiting results. |
| `STREAMING_RESPONSE` | The streaming bubble shows the model's final response building up. The tool card remains visible above or below the text. |
| `DONE` | The streaming bubble is replaced by the final rendered message (from the persisted `conversation_messages` row). |

### 5.2 Event handling

Each Wails event payload has the shape established in §4.2:
`{ conversation_id: number, event: StreamEvent }`. The frontend first
filters on `conversation_id`, then dispatches on the nested event's `type`:

```
on llm:stream event:
  if event.data.conversation_id !== activeConversationId:
    return  // discard — not for the conversation being viewed

  switch event.data.event.type:
    case "content_delta":
      Append event.data.event.content to the streaming text buffer.
      Rerender the streaming bubble.

    case "reasoning_start":
      Enter reasoning mode — append text inside a
      <details class="streaming-reasoning">
        <summary>💭 Thinking…</summary>
        ...text...
      </details>
      The block starts collapsed — the user sees the summary line
      only, not the raw monologue (§5.3).

    case "reasoning_end":
      Close the reasoning block. Update summary to show duration.
      Subsequent text goes to the main bubble.

    case "tool_call_start":
      Insert a tool card:
      <div class="streaming-tool-card">
        <span class="tool-spinner" />
        <span>Running {event.data.event.tool_name}…</span>
      </div>

    case "tool_call_delta":
      Update the tool card's arguments display (show partial JSON
      accumulating from event.data.event.arguments).

    case "tool_call_end":
      Mark the tool card as complete. If the tool was query_database,
      the card transitions to a "waiting" state.

    case "done":
      Finalize the streaming bubble. Wait for the final assistant
      message to be persisted (it arrives via the normal conversation
      history refresh), then collapse the streaming bubble into the
      final rendered message.

    case "error":
      Show an error indicator in the streaming bubble.
```

### 5.3 Reasoning display

**Critical constraint from `ANSWER_CLARIFICATION_ISSUE.md`:** The postmortem's
central fix was removing LLM internal monologue from user-facing display
(§0: *"A wrong answer is worse than no answer — the user should not see
LLM chain-of-thought reasoning as part of their result."*). Showing the
model's raw thinking stream verbatim would undo that fix.

Reasoning content (from o-series models, Claude extended thinking, or any
model's internal monologue) is therefore rendered **collapsed by default**:

```html
<details class="streaming-reasoning">
  <summary>💭 Thinking…</summary>
  <div class="streaming-reasoning-content">
    <!-- monospace, smaller, muted color, distinct background -->
  </div>
</details>
```

The `<details>` element is **closed** initially — the user sees a single
"Thinking…" summary line that indicates the model is working, but the raw
monologue is hidden. The user can expand it if they want transparency, but
the default experience matches the postmortem: reasoning is never the
primary visible content.

When reasoning ends (`reasoning_end`), the block's summary updates to
show a checkmark or duration indicator ("Thought for 12s") and the block
remains collapsed. The model's final response text then appears as the
primary visible content in the streaming bubble.

### 5.4 Tool call display

Each tool call appears as a card in the streaming bubble:

```html
<div class="streaming-tool-card">
  <span class="tool-icon">🔧</span>
  <span class="tool-name">query_database</span>
  <code class="tool-args">{ "sql": "SELECT COU..." }</code>
  <span class="tool-status">⏳ executing…</span>
</div>
```

The card transitions from "forming" (arguments accumulating) → "executing"
(waiting for `query_database` result) → "complete" (result received, next
streaming round). For `respond_to_user` and `render_chart`, there is no
execution wait — the card immediately resolves.

### 5.5 Cleanup

When the streaming conversation changes (user switches conversations) or the
stream errors out:
- The streaming bubble is removed.
- The partial text/tool calls are discarded.
- If the LLM call was mid-flight, the context is cancelled and the stream
  is aborted (Go context propagation handles this).

---

## 6. Implementation Plan

### Phase 1 — OpenAI / OpenRouter SSE

- Add `ChatCompletionWithToolsStreaming` to `LLMClient` interface.
- Add a passthrough (blocking, one-chunk) implementation for Anthropic,
  Ollama, and Local so the package compiles.
- Implement real SSE streaming in `llm_openai.go`:
  - `stream: true` request parameter.
  - SSE line-by-line parser (`bufio.Scanner` + `data: ` prefix).
  - Delta accumulation: text, tool calls (by index), reasoning.
  - StreamEvent emission via callback.
  - Final `*ChatMessage` assembly.
- Wire into `runAgenticLoop` behind a conversation-level feature flag
  (`streaming_enabled`, default `0` — matching the rollout pattern from
  the tool-calling project).
- Add minimal frontend listener that logs events to console (no UI yet).

**Deliverable:** OpenAI streaming works end-to-end (events reach the
frontend via Wails runtime). Other providers compile as blocking
passthroughs. Existing non-streaming path entirely untouched.

### Phase 2 — Frontend streaming bubble

- Add streaming state machine to `ConversationView.svelte`.
- Render the streaming bubble component:
  - Reasoning block (`<details>` with "Thinking…" summary).
  - Tool call cards (forming → executing → complete).
  - Response text (character-by-character accumulation).
- Handle stream lifecycle:
  - Start: show streaming bubble below the latest user message.
  - During: update in-place on each event.
  - End: fetch the final persisted message, replace streaming bubble.
  - Error: show error indicator, keep partial content visible.
- Handle conversation switches (clean up streaming state for the old
  conversation).

**Deliverable:** user sends a message → sees the model thinking in real
time → sees tool calls forming → sees final response appear → streaming
bubble resolves into the normal rendered message. No regression on the
non-streaming path.

### Phase 3 — Anthropic SSE

- Replace the Phase 1 passthrough with real SSE streaming in
  `llm_anthropic.go`:
  - `stream: true` request parameter.
  - SSE event-type parser (`event: content_block_start`, etc.).
  - Content block accumulation by index.
  - `tool_use` block handling (input_json_delta fragments).
  - Thinking/reasoning block handling.
  - StreamEvent emission via callback.
  - Final `*ChatMessage` assembly (extract content blocks, marshal
    `tool_use.input` back to JSON string).

**Deliverable:** Claude models stream identically to OpenAI models from the
frontend's perspective.

### Phase 4 — Ollama / Local streaming + fallback

- For Ollama: NDJSON line-by-line streaming. Since Ollama models don't
  support native tool calls, the stream delivers raw text (the fallback
  JSON routing prefix). Events are `StreamContentDelta` only. After
  `done: true`, the full text is parsed by `parseFallbackResponse`.
- For Local: Same SSE logic as OpenAI (§3.6). Backends that don't support
  streaming fall back to a blocking call with one synthetic
  `StreamContentDelta` + `StreamDone` event.
- Remove the conversation-level feature flag and flip the default to
  streaming-always (matching the tool-calling project's default-flip
  pattern in Phase 5).

**Deliverable:** every provider path streams. The blocking fallback for
non-streaming backends is transparent to the frontend.

---

## 7. Risk, Safety, and Compliance

### 7.1 Absolute rules

Per `AGENT_READ_FIRST.md` §4.0, three rules are never subject to risk/reward
trade-off. All three are unaffected:

| Rule | Status |
|---|---|
| Never execute a write statement against a data source | Unchanged. Streaming only affects the LLM communication layer. |
| Never log API keys or database passwords | Unchanged. Streaming events carry model output text — never credentials. |
| Never destructively alter an existing column | The `streaming_enabled` column (if used in Phase 1–3) is an additive, nullable `ensureColumn` call with `DEFAULT 0`. Removed in Phase 4 along with the flag. |

### 7.2 Specific streaming risks

| Risk | Mitigation |
|---|---|
| **Reasoning leak** — the model's internal monologue reaches the user. | Reasoning is explicitly bracketed (`StreamReasoningStart`/`End`) and rendered in a collapsed `<details>` block by default (§5.3). The user sees only a "💭 Thinking…" summary line — the raw monologue requires an explicit click to expand. This matches the `ANSWER_CLARIFICATION_ISSUE.md` postmortem fix. Providers that don't mark reasoning boundaries (e.g., some local models) may have reasoning appear as normal text — the frontend cannot distinguish it without provider signals. |
| **Double rendering** — the streaming bubble and the final persisted message both appear. | The frontend state machine explicitly transitions: streaming bubble is removed when `done` fires, replaced by the next `conversation_messages` refresh that includes the persisted row. A debounce (100ms) on the history refresh prevents flicker. |
| **Partial tool call arguments** — arguments arrive as JSON fragments. If the stream disconnects mid-argument, the tool call is incomplete. | The agentic loop only processes `*ChatMessage` after the stream completes (`done` fires). If the stream errors, the partial tool calls are discarded and the error is surfaced via the normal error path (`handleClarification` or `formatUserError`). |
| **Callback blocking** — the `onEvent` callback is synchronous. If the frontend event emit blocks (e.g., the frontend is frozen), the stream stalls. | Wails `EventsEmit` is non-blocking on the Go side — it fires and returns immediately. The browser event loop handles delivery asynchronously. No back-pressure risk. |
| **High event rate** — a single response can produce hundreds of deltas (one per token). | The callback must be fast — no allocations, no JSON marshalling of large objects. `StreamEvent` is a small struct. The Svelte side uses `requestAnimationFrame` batching to avoid thrashing the DOM. |
| **Multi-conversation interference** — streaming events for conversation A arrive while viewing conversation B. | Every emitted event is wrapped with a top-level `conversation_id` (§4.2) — `StreamEvent` itself carries no conversation identity. The frontend only updates the streaming bubble when `event.data.conversation_id` matches the active conversation. Mismatched events are discarded silently. |

### 7.3 Payload logging for streamed calls

`storePayload` currently captures the full request and response JSON for
tech-details debugging. For a streamed call, the "response JSON" would be
the entire raw SSE stream — potentially thousands of lines per call.
Storing this would bloat the database. Instead:

- `ChatCompletionWithToolsStreaming` returns `requestJSON` (the marshalled
  request, identical to the blocking path) and `responseJSON` (the
  **assembled** `*ChatMessage` marshalled as JSON — not the raw SSE
  stream). This is what `storePayload` stores.
- The raw SSE stream is never persisted. If debugging a streaming issue
  requires the raw stream, that's a development-time concern (add a
  temporary `log.Printf` or environment-flagged dump), not a production
  persistence concern.
- During Phase 1–3 (when streaming is feature-flagged), `storePayload`
  continues to receive the assembled message for streamed calls, keeping
  the tech-details experience identical to the blocking path.

### 7.4 StreamEvent.Error sanitization

Per `AGENT_READ_FIRST.md` §3.8: *"Never expose raw stack traces or driver
error strings directly to the user without context."*

`StreamEvent.Error` carries error information from the provider layer
(chunk parse failures, connection issues). Before an error event reaches
the frontend, the Go-side event forwarder in `app.go` wraps the raw error
in a user-friendly message:

```go
onStream := func(ev services.StreamEvent) {
    if ev.Type == services.StreamError {
        ev.Error = "I'm having trouble receiving the response. Retrying…"
        // Raw error stored for tech-details inspection if needed.
    }
    runtime.EventsEmit(a.ctx, "llm:stream", ...)
}
```

This way the frontend can safely render `event.data.event.error` without
leaking provider internals. The raw error detail is surfaced separately
via the existing `buildErrorMetadata` / tech-details toggle path if the
stream fails entirely and falls through to the agentic loop's error
handling.

### 7.5 HTTP timeout alignment

The project sets per-provider HTTP client timeouts and a context deadline
in `ProcessUserMessage`:

| Scope | Duration |
|---|---|
| OpenAI HTTP client | 300s |
| Anthropic HTTP client | 120s |
| Ollama HTTP client | 180s |
| Local HTTP client | 180s |
| `ProcessUserMessage` context deadline | 180s |

Streaming keeps an HTTP connection open for the entire LLM generation.
Anthropic's 120s timeout could expire during a long reasoning phase
(e.g., Claude extended thinking on a complex schema) before the 180s
context deadline fires. The fix: bump the Anthropic HTTP client timeout
to 300s (matching OpenAI) during the Phase 3 implementation. This is a
one-line change in `NewAnthropicClient` and carries no risk — the
context deadline is still the ultimate governor, and the HTTP timeout
is just a safety net for hung connections.

### 7.6 Charter compliance

- **Fundamental goal** (`AGENT_READ_FIRST.md` §0): streaming is purely
  additive to answer delivery; it does not change what answer is produced
  or how it is executed.
- **Appeal / Comfort**: directly improved — the user sees continuous
  progress instead of a dead-air spinner.
- **Safety**: unchanged — no new data sources, no new credential surfaces,
  no new execution paths for SQL.
- **Graceful degradation**: providers without streaming support silently
  fall back to a single-chunk event (the full response text in one
  `StreamContentDelta` + `StreamDone`). The frontend handles this
  identically to a streamed response. No feature detection required.

### 7.7 Formal risk/reward analysis (per `AGENT_READ_FIRST.md` §4.4)

**Change:** Add SSE streaming to every LLM provider's `ChatCompletionWithTools`
path, forward output to the frontend via Wails events, and render a
live-updating assistant bubble that shows model progress in real time.

**Fundamental goal impact:** None on answer accuracy or delivery.
Streaming is a UX layer on top of the identical answer pipeline — the
model produces the same tool calls, the same SQL executes, the same
results render. The only difference is the user sees progress during
what would otherwise be a dead-air spinner.

**Risk category(ies):** Regression (new code path in the LLM communication
layer for every provider), User trust (reasoning display must not undo
the `ANSWER_CLARIFICATION_ISSUE.md` fix), UX/Comfort (streaming bubble
must not interfere with or duplicate the final rendered message).

**Failure mode(s):**
1. A provider's SSE parser misparses a chunk, causing a corrupted
   `*ChatMessage` assembly and a wrong or missing tool call.
2. The streaming bubble fails to transition to the final persisted
   message, leaving the user with a stuck "thinking" indicator.
3. Raw reasoning text reaches the user as primary visible content
   (undoing the postmortem fix).
4. The Anthropic HTTP client timeout (120s) fires before the context
   deadline (180s), killing the stream during a long reasoning phase.
5. Streaming events for one conversation render in another conversation's
   view due to a frontend state leak.

**Reward:** Replaces 1–30 second dead-air spinner gaps with visible,
continuous progress. Directly serves the Appeal and Comfort secondary
goals. Eliminates the user's uncertainty about whether the app is stuck
or thinking productively. Particularly impactful for reasoning models
(qwen/qwen3.7-plus, o-series, Claude extended thinking) where the gap
between "ask" and "see result" is longest.

**Mitigations:**
1. Each provider's SSE parser is implemented and tested independently.
   The blocking `ChatCompletionWithTools` path remains untouched and
   serves as a fallback (feature flag during Phases 1–3).
2. The frontend state machine has explicit transitions; the streaming
   bubble is removed when `done` fires, replaced by the persisted
   message on the next history refresh. A 100ms debounce prevents
   flicker.
3. Reasoning is rendered collapsed by default (§5.3), matching the
   postmortem fix. The user sees only a "💭 Thinking…" summary line;
   raw monologue requires an explicit click to expand.
4. Anthropic HTTP client timeout bumped to 300s (matching OpenAI) in
   Phase 3.
5. Each streaming event carries `conversation_id`. The frontend
   discards events for non-active conversations.

**Decision:** Proceed. The risk is medium (new code in the LLM
communication path, four providers), but actively mitigated by the
feature flag, the blocking-path fallback, the collapsed reasoning
default, and the per-event conversation routing. The reward is high —
a dead-air spinner for 30+ seconds on reasoning models is a real UX
failure that directly contradicts the Appeal and Comfort secondary
goal. The change does not touch the answer pipeline, SQL execution,
or data safety layers.

---

## 8. Files Affected

| File | Change |
|---|---|
| `pkg/services/llm_client.go` | Add `StreamEvent`, `StreamEventType`, and `ChatCompletionWithToolsStreaming` to `LLMClient` interface. Existing methods untouched. |
| `pkg/services/llm_openai.go` | Real SSE streaming implementation: `stream: true`, SSE line parser, delta accumulation by `tool_calls[].index`, reasoning detection. |
| `pkg/services/llm_anthropic.go` | Real SSE streaming: `stream: true`, event-type dispatch (`content_block_start` / `content_block_delta` / `message_delta` / `message_stop`), tool_use input accumulation, thinking block handling. |
| `pkg/services/llm_ollama.go` | NDJSON streaming: `stream: true`, line-by-line JSON parse, raw text accumulation, `parseFallbackResponse` at stream end. |
| `pkg/services/llm_local.go` | SSE streaming (OpenAI-compatible), same parser as `llm_openai.go`. Blocking single-chunk fallback for non-streaming backends. |
| `pkg/services/agentic_loop.go` | Call `ChatCompletionWithToolsStreaming` instead of `ChatCompletionWithTools` when streaming is enabled. Forward `StreamEvent` via Wails runtime. |
| `pkg/models/conversation.go` | Optional: add `StreamingEnabled bool` for the Phase 1–3 feature flag (removed in Phase 4). |
| `app.go` | Optional: expose `StreamingEnabled` toggle in conversation settings. |
| `frontend/src/ConversationView.svelte` | Streaming state machine, streaming bubble component (reasoning block, tool cards, response text), `llm:stream` event listener. |

---

## 9. References

- [`TOOL_CALL_ENHANCEMENT.md`](TOOL_CALL_ENHANCEMENT.md) — the tool-calling
  architecture this design builds on.
- [`AGENT_READ_FIRST.md`](AGENT_READ_FIRST.md) — project charter, risk
  framework, goal priority order.
- [`ANSWER_CLARIFICATION_ISSUE.md`](ANSWER_CLARIFICATION_ISSUE.md) —
  postmortem on the old JSON-response protocol's user-facing bugs.
- [OpenAI Streaming Guide](https://platform.openai.com/docs/api-reference/streaming)
- [Anthropic Streaming Guide](https://docs.anthropic.com/en/api/streaming)
- [Ollama API Docs](https://github.com/ollama/ollama/blob/main/docs/api.md)
- [Wails Runtime Events](https://wails.io/docs/reference/runtime/events/)
