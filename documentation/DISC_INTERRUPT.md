# DISC_INTERRUPT.md — Interrupt & Cancel Plan

> **Status:** Implementing (core backend + Go build passing)  
> **Version target:** v0.4.0  
> **Created:** 2026-08-04

## 1. Overview

Currently, when a user sends a message in YourQL, they are locked into
waiting for the full pipeline to complete — LLM calls, exploration rounds,
SQL execution, summarization — with no way to stop it. If the LLM is
generating bad SQL, stuck in an exploration loop, producing a very long
response, or simply taking too long, the user is helpless.

This feature adds a **Cancel** button that appears during processing,
allowing the user to safely interrupt the pipeline at any point and return
the conversation to a clean, usable state.

### User Story

> As a YourQL user, I want to cancel a long-running or stuck LLM request so
> that I can rephrase my question or adjust settings without force-quitting
> the application or waiting minutes for a timeout.

---

## 2. Design Goals

1. **Safe** — Interrupting must never leave the conversation in an
   inconsistent state. The user's message must be saved. No partial AI
   responses must remain visible. No database connections must be leaked.
2. **Responsive** — The cancel button must feel immediate. When the user
   clicks it, the pipeline should stop within a reasonable timeframe
   (ideally under 1 second for LLM cancellation, under 3 seconds for
   in-flight SQL queries).
3. **Transparent** — The user must know what happened. A clear "Cancelled"
   message should appear in the chat, and the input field must be
   immediately re-enabled so they can try again.
4. **Read-only invariant preserved** — Cancellation must not leave a write
   statement mid-execution. Since all data source queries are SELECT-only,
   this is already guaranteed, but the cancel mechanism must not
   accidentally bypass safety checks.
5. **No goroutine leaks** — Every cancelled operation must release its
   resources: HTTP connections, database connections, goroutines.

---

## 2.5 Risk/Reward Analysis

Per `AGENT_READ_FIRST.md` §4.4.

- **Change:** Adding user-initiated cancellation via `context.Context` through the
  entire pipeline (LLM calls, SQL execution, exploration loop, summarization).

- **Fundamental goal impact:** Improves **reliability of delivery** (#3) — the
  user regains control over stuck or long-running requests instead of waiting
  for the 180-second timeout. No impact on answer correctness (#1) or data
  safety (#2) since cancellation only affects in-flight operations, not
  persisted state.

- **Risk category(ies):** Answer delivery, user trust, regression.

- **Failure mode(s):**
  1. **`context.Canceled` triggers the deferred error message in
     `ProcessUserMessage`.** If unhandled, the user sees both a "Cancelled"
     indicator AND a spurious error bubble. Mitigation: exclude
     `context.Canceled` from the error-message defer (see §7.1).
  2. **Partial assistant message visible after cancel.** If cancellation
     occurs between `CreateConversationMessage` for the assistant and the
     query status update, the conversation shows an orphaned message with no
     corresponding query record. Mitigation: save the "Cancelled" system
     message as the LAST step of cleanup, after all other state is consistent.
  3. **Concurrent map overwrite.** If two `ProcessUserMessage` calls run
     simultaneously for the same conversation (theoretical — the UI prevents
     this by disabling the send button during processing), `registerCancel`
     overwrites the first call's cancel function. Mitigation: log a warning on
     overwrite as defense-in-depth (see §8).

- **Reward:** Eliminates the primary frustration of long-running queries —
  users are no longer hostages to the pipeline. Enables faster iteration on
  query refinement.

- **Mitigations:** Deferred error exclusion, system message as final cleanup
  step, concurrent-overwrite warning log.

- **Decision:** Proceed with the deferred-error fix (required). Concurrent
  overwrite defense and query status update are recommended but not blocking.

---

## 3. Architecture

### 3.1 Cancellation Mechanism: Go `context.Context`

Go's standard `context.Context` with `WithCancel` is the natural mechanism:

```
App.svelte (Cancel button click)
  → Wails binding: CancelProcessing(conversationID)
    → looks up cancel func in map
      → calls cancel()
        → context cancelled
          → all pipeline stages check ctx.Err()
            → each stage returns context.Canceled
              → cleanup runs
                → "Cancelled" message saved
                  → event emitted to frontend
```

### 3.2 Per-Conversation Cancel Map

A thread-safe map in the `App` struct (or a dedicated service) maps
conversation IDs to their cancel functions:

```go
// In app.go
type App struct {
    ctx               context.Context
    activeCancels     map[uint]context.CancelFunc
    activeCancelsMu   sync.Mutex
}

func (a *App) registerCancel(conversationID uint, cancel context.CancelFunc) {
    a.activeCancelsMu.Lock()
    a.activeCancels[conversationID] = cancel
    a.activeCancelsMu.Unlock()
}

func (a *App) unregisterCancel(conversationID uint) {
    a.activeCancelsMu.Lock()
    delete(a.activeCancels, conversationID)
    a.activeCancelsMu.Unlock()
}

func (a *App) CancelProcessing(conversationID uint) error {
    a.activeCancelsMu.Lock()
    cancel, ok := a.activeCancels[conversationID]
    a.activeCancelsMu.Unlock()
    if !ok {
        return fmt.Errorf("no active processing for conversation %d", conversationID)
    }
    cancel()
    return nil
}
```

### 3.3 Context Propagation Through the Pipeline

The cancellable context must flow through the entire pipeline:

```
ProcessUserMessage (creates ctx)
  → runAgenticLoop (receives ctx)
    → LLM API call (passes ctx to http.Request)
    → SQL execution (passes ctx to db.QueryContext)
    → Summarization (passes ctx)
  → each stage checks ctx.Err() on entry and after blocking calls
```

---

## 4. Cancellation Points

The pipeline has four distinct stages where cancellation can occur. Each
requires different handling.

### Stage 1: Before LLM Call

**When:** User clicks cancel during schema fetch or prompt construction.

**Action:** Cancel immediately. No LLM response has been generated, no
messages have been saved. The user message was already saved (Step 2 of
`ProcessUserMessage`).

**Cleanup:**
- The user message is already persisted — no change needed.
- Emit `processingCancelled` event with `{ reason: "user_cancelled" }`.
- Frontend removes the optimistic user message (or keeps it — see §5).

**Cost:** Trivial. Context cancellation returns `context.Canceled`
immediately from `runAgenticLoop`.

### Stage 2: Mid-LLM Call (Streaming or Blocking)

**When:** User clicks cancel while the LLM is generating a response.

**Action:** Cancel the HTTP request. The `http.Client` will close the
underlying TCP connection when the context is cancelled, which terminates
the streaming or blocking response.

**Cleanup:**
- Any partial streaming content already emitted to the frontend must be
  cleared.
- No assistant message is saved (the response was incomplete).
- The LLM API call may have consumed tokens — those are lost, which is
  acceptable.

**Frontend handling:**
- Clear any partial streaming text from the chat view.
- If a streaming `assistant` message div was already created, remove it.
- Show "Cancelled" as a system message.

### Stage 3: Mid-SQL Execution

**When:** User clicks cancel while a data source query is running.

**Action:** Cancel the `database/sql` query via `QueryContext`. The
underlying driver receives the cancellation and terminates the query on
the database server.

**Cleanup:**
- Close the `*sql.Rows` result set.
- Close the data source connection (`defer db.Close()` already handles
  this).
- No `sql_results` message is saved.

**Risk assessment:** Terminating a long-running `SELECT` query on a
production database is safe — the database rolls back the query's
transaction (if any) and releases locks. Since YourQL only runs SELECT
queries, there is no risk of partial writes.

### Stage 4: Mid-Exploration Loop

**When:** User clicks cancel during exploration rounds (the LLM is
exploring the schema before writing the final query).

**Action:** Cancel the entire agentic loop. The `runAgenticLoop` function
checks `ctx.Err()` at the start of each round. If cancelled, it returns
`context.Canceled`.

**Cleanup:**
- Any exploration messages already saved (schema exploration results)
  remain in the conversation — they are valid, complete query results.
- The final answer is not generated.
- User message + exploration messages are visible; the conversation is in
  a valid state.

---

## 5. Conversation State After Interrupt

### 5.1 What Gets Saved

| Message type | Saved? | Rationale |
|---|---|---|
| User's original message | ✅ Always | The user sent it; it should persist |
| Exploration results (completed rounds) | ✅ Keep | They are valid, complete query results |
| Partial streamed assistant text | ❌ Discard | Incomplete, would confuse the user |
| Partial tool call results | ❌ Discard | Incomplete; may contain invalid state |
| "Cancelled" indicator | ✅ Save as system message | Informs the user what happened |

### 5.2 System Message Format

After cancellation, a system message is inserted into the conversation:

```
role: "system"
content: "⏹ Cancelled"
```

Frontend renders this as a subtle, non-intrusive indicator — not a full
chat bubble, but a small centered label between messages.

### 5.3 The User's Message

The user message is saved BEFORE the LLM pipeline begins (Step 2 in
`ProcessUserMessage`). This means even if the user cancels immediately
after sending, their message is persisted. When they return to the
conversation, they see:

```
User: "Show me all customers who..."
---- ⏹ Cancelled ----
```

The input field is active, ready for the next attempt.

### 5.4 Query Status on Cancel

A cancelled query must leave a trace in the `queries` table. During
cleanup, call:

```go
UpdateQueryStatus(query.ID, "cancelled", nil, nil,
    stringPtr("cancelled by user"), nil, nil, nil)
UpdateQueryErrorCategory(query.ID, "user_cancelled")
```

The `error_category` value `user_cancelled` distinguishes intentional
cancellation from system errors. The frontend can use this to suppress
retry prompts for cancelled queries.

Add `"user_cancelled"` to `classifyErrorCategory` to prevent it from
falling through to the `"internal_error"` default.

---

## 6. Frontend Design

### 6.1 Cancel Button

During processing, the send button in `ConversationView` transforms into a
cancel button:

```
Normal:     [▶ Send]
Processing: [■ Cancel]   ← Red/danger colored, pulsing (optional)
```

**Behavior:**
- Clicking Cancel calls `CancelProcessing(activeConversation.id)` via the
  Wails binding.
- The button immediately shows a brief "Cancelling..." state.
- On `processingCancelled` event, the button reverts to [▶ Send].

### 6.2 Event Flow

```
Frontend                          Backend
   |                                 |
   |── ProcessUserMessage(id, msg) ─→|
   |                                 |── save user message
   |   ←── processingPhase("...") ──|
   |                                 |── LLM call running...
   |                                 |
   |── CancelProcessing(id) ───────→|
   |                                 |── cancel()
   |                                 |── ctx cancelled
   |                                 |── cleanup
   |   ←── processingCancelled ─────|
   |                                 |
   |── remove streaming content      |
   |── show "Cancelled" label        |
   |── enable input                  |
```

### 6.3 Existing Events to Update

| Event | Current behavior | New behavior |
|---|---|---|
| `processingPhase` | Updates "Thinking..." label | Unchanged |
| `processingComplete` | Clears "Thinking..." | Unchanged |
| `processingCancelled` | Does not exist | **New** — Clears "Thinking...", removes partial content, shows cancelled indicator |

### 6.4 Processing State Changes

In `App.svelte`:

```js
EventsOn('processingCancelled', (data) => {
    processingMessage = ''
    processingConversationId = null
    // data.conversation_id tells us which conversation was cancelled
    if (data.conversation_id === activeConversation?.id) {
        // Refresh messages to show "Cancelled" system message
        loadConversationMessages(activeConversation.id)
    }
})
```

---

## 7. Implementation

### 7.1 Backend Changes

#### `app.go`

| Change | Description |
|---|---|
| Add `activeCancels` map + mutex | Thread-safe per-conversation cancel storage |
| Add `registerCancel` / `unregisterCancel` | Manage lifecycle |
| Add `CancelProcessing(conversationID uint) error` | **New Wails binding** — calls cancel function |
| Modify `ProcessUserMessage` | Create context, register cancel, defer unregister, pass context to service |

```go
func (a *App) ProcessUserMessage(conversationID uint, userMessage string) error {
    // Create cancellable context
    ctx, cancel := context.WithCancel(context.Background())
    a.registerCancel(conversationID, cancel)
    defer a.unregisterCancel(conversationID)

    onStream := func(ev services.StreamEvent) { /* ... */ }

    err := services.ProcessUserMessageWithContext(ctx, conversationID, userMessage,
        func(phase string) { /* ... */ }, onStream)

    if a.ctx != nil {
        if errors.Is(err, context.Canceled) {
            runtime.EventsEmit(a.ctx, "processingCancelled", map[string]interface{}{
                "conversation_id": conversationID,
            })
        } else {
            runtime.EventsEmit(a.ctx, "processingComplete")
        }
    }
    return err
}
```

#### `pkg/services/discussion_engine.go`

| Change | Description |
|---|---|
| **Add `ProcessUserMessageWithContext(ctx, ...)`** | New entry point accepting external context. Wraps existing logic. |
| **Modify deferred error handler** | Exclude `context.Canceled` from the error-message defer so user-initiated cancellations don't produce spurious error bubbles. |
| Modify `runAgenticLoop` | Check `ctx.Err()` at the start of each round. Return `context.Canceled` immediately without saving an error message. |
| Modify `runAgenticLoop` | Check `ctx.Err()` at the start of each round |
| Modify LLM API calls | Pass `ctx` to `client.ChatCompletion(ctx, ...)` |
| Modify SQL execution | Pass `ctx` to `db.QueryContext(ctx, ...)` / native querier |

The existing `ProcessUserMessage` can become a wrapper:

```go
func ProcessUserMessage(conversationID uint, userMessage string,
    onPhase func(string), onStream func(StreamEvent)) error {
    return ProcessUserMessageWithContext(context.Background(),
        conversationID, userMessage, onPhase, onStream)
}
```

#### `pkg/services/sql_execution.go`

| Change | Description |
|---|---|
| `ExecuteQuery` accepts `context.Context` | Already partially supported via `QueryContext` |
| `NativeQuerier` interface updated | Add `ctx` parameter to native query methods |

#### `pkg/services/llm_client.go`

| Change | Description |
|---|---|
| `ChatCompletion` already accepts `context.Context` | ✅ No change needed |
| `ChatCompletionStreaming` already accepts `context.Context` | ✅ No change needed |

### 7.2 Frontend Changes

| File | Change |
|---|---|
| `ConversationView.svelte` | Replace send button with cancel button during processing; call `CancelProcessing` |
| `App.svelte` | Add `EventsOn('processingCancelled', ...)` listener; cleanup streaming content |
| `main.js` | No changes needed |

### 7.3 Cancel Button UI

```svelte
<!-- In ConversationView.svelte, replace the send button: -->
{#if processingMessage}
  <button class="cancel-btn" onclick={handleCancel}>
    <svg><!-- stop icon --></svg>
  </button>
{:else}
  <button class="send-btn" onclick={sendAndReset} disabled={!localMessage.trim()}>
    <svg><!-- send icon --></svg>
  </button>
{/if}

<script>
  import { CancelProcessing } from '../wailsjs/go/main/App.js'

  function handleCancel() {
    CancelProcessing(activeConversation.id).catch(e =>
      console.error('Failed to cancel:', e))
  }
</script>
```

---

## 8. Edge Cases

| Scenario | Behavior |
|---|---|
| **User clicks Cancel before any LLM call** | User message saved. `processingCancelled` emitted. Input re-enabled. |
| **User clicks Cancel during streaming** | Streaming stops. Partial text discarded. No assistant message saved. |
| **User clicks Cancel, then immediately sends another message** | The new `ProcessUserMessage` call creates a new context + cancel function, replacing the old one. The old cancel is no-oped. |
| **Concurrent `ProcessUserMessage` calls for same conversation** | The UI prevents this (send button disabled during processing), but `registerCancel` includes a defense-in-depth warning log if an existing cancel is overwritten. |
| **Two rapid Cancel clicks** | Second click calls `CancelProcessing` again but the cancel func is already gone (unregistered on first click). Returns error silently. |
| **Cancel during SQL execution** | `QueryContext` returns `context.Canceled`. Retry logic in `runAgenticLoop` skips retry because the error is `context.Canceled`. |
| **Cancel during summarization** | Summary is optional (graceful degradation principle). Cancellation at this stage means the data table is shown without a summary. |
| **App quit during processing** | `defer unregisterCancel` runs during `ProcessUserMessage` cleanup. No goroutine leak. |
| **Cancel on conversation with streaming disabled** | Works identically — the LLM client's blocking `ChatCompletion` respects context cancellation. |

---

## 9. Safety Verification Checklist

- [x] `context.Canceled` is excluded from the deferred error handler and SQL error retry paths so cancellation never surfaces as a user-facing error — only the "Cancelled" system message appears.
- [ ] The send button is re-enabled after cancellation, even if `ProcessUserMessage` panics (deferred cleanup handles this).
- [ ] No goroutine leak — every `go func()` spawned during processing has a `select` on `ctx.Done()`.
- [ ] Data source connections are always closed — `defer db.Close()` and `defer rows.Close()` handle this regardless of cancellation.
- [ ] The "Thinking..." indicator is always cleared — `processingCancelled` event handler in `App.svelte` clears `processingMessage`.
- [ ] Read-only invariant preserved — cancellation doesn't create a code path that bypasses the SELECT-only check in `sql_execution.go`.
- [ ] Test with all four LLM providers (OpenAI, Anthropic, Ollama, Custom) — each handles HTTP cancellation differently.
- [ ] Test with streaming on and off.
- [ ] Test with all database driver types — especially `NativeQuerier` drivers (BigQuery, Google Sheets).

---

## 10. Implementation Phases

### Phase 1: Backend Context Plumbing (2 hours)

| Task |
|---|
| Add `activeCancels` map + `CancelProcessing` binding in `app.go` |
| Create `ProcessUserMessageWithContext` in `discussion_engine.go` |
| Plumb `ctx` through `runAgenticLoop`, LLM calls, SQL execution |
| Add `ctx.Err()` checks at loop boundaries |
| Handle `context.Canceled` in error paths (don't retry, don't show as error) |

### Phase 2: Frontend Cancel Button (1 hour)

| Task |
|---|
| Transform send button into cancel button during processing |
| Add `CancelProcessing` import and call |
| Add `processingCancelled` event listener |
| Clear partial streaming content on cancel |
| Show "Cancelled" system message indicator |

### Phase 3: System Message & Cleanup (30 min)

| Task |
|---|
| Save "Cancelled" system message to conversation |
| Ensure input is re-enabled after cancel |
| Handle edge case: cancel + immediate re-send |
| Dark mode styling for cancel button and cancelled indicator |

### Phase 4: Testing (1 hour)

| Task |
|---|
| Test with all LLM providers |
| Test with streaming on and off |
| Test with at least 3 database types |
| Test rapid cancel + re-send |
| Test cancel during exploration loop |

---

## 11. Future Enhancements (Out of Scope for v0.4.0)

- **Timeout configuration** — Let users set a per-conversation timeout instead
  of the hardcoded 180 seconds.
- **Pause/resume** — Pause an LLM generation and resume it later (requires
  checkpointing the conversation state).
- **Partial results** — If cancelled during SQL execution, show partial
  results if the database supports it (e.g., BigQuery dry runs).
- **Cancel reason prompt** — Ask the user why they cancelled (for
  diagnostics/improvement).