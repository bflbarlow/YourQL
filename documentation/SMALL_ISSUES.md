# SMALL_ISSUES.md — Known Bugs & Deferred Fixes

> Companion to `AGENT_READ_FIRST.md`. Documents non-critical issues discovered
> during development that are understood but deferred for a later pass. Each
> entry includes the root cause, impact, and a concrete fix strategy so any
> agent (or future self) can pick it up without re-investigating.

---

## 1. Table Sorting Reverts After Sort (Svelte 5 Reactivity Clash)

- **Discovered:** 2026-08-07
- **Severity:** Medium — user-facing feature silently broken, but data is still
  viewable and the CSV export works.
- **Affected files:**
  - `frontend/src/main.js:239–345` (sort logic)
  - `pkg/services/sql_execution.go:435–440` (table HTML generation)
  - `frontend/src/ConversationView.svelte:565` (message rendering)

### Root Cause

Table sorting is implemented outside of Svelte's reactivity system — plain
JavaScript in `main.js` manipulates the DOM directly via `tbody.innerHTML = ''`
and `appendChild`. The conversation view renders assistant messages with
`{@html message.content}`, which means Svelte treats the entire message block
as opaque HTML. When `sortTable()` rebuilds `tbody`, Svelte may re-render the
original `{@html}` expression on a subsequent reactivity tick (e.g., when a
streaming event, timer, or state change triggers a re-render in the parent
component), silently replacing the sorted DOM with the original server-rendered
HTML.

The symptom: sorting works on the first click (sort indicators update, rows
reorder), but the table reverts to its original order after a brief delay or on
the next interaction. This is a classic Svelte 5 dirty-check vs. direct DOM
mutation conflict.

### Why It's Not a Regression From Our Changes

Our agent-loop-config work touched:
- `pkg/services/agentic_loop.go` (tool descriptions, system prompt text)
- `pkg/services/agent_loop_config.go` (new file)
- `pkg/models/agent_loop_config.go` (new file)
- `pkg/models/database.go` (migration — new table, additive)
- `app.go` (new Wails bindings, no changes to message processing)
- `frontend/src/SettingsView.svelte` (settings UI only)

None of these files touch `main.js`, `ConversationView.svelte`, or
`sql_execution.go`. The sorting issue is pre-existing and was revealed by the
`wails dev` rebuild cycle, not introduced by our code.

### Data Still Sortable

The table HTML is correctly generated. `data-sort-rows` contains the full row
data as JSON. All headers have `sort-header` class and `data-col` attributes.
The `sortTable()` function in `main.js` parses and sorts correctly. The
JavaScript event delegation (`document.addEventListener('click')`) fires and
completes successfully. The problem is only that the sorted DOM is later
overwritten by Svelte.

### Fix Strategy

**Option A — Lift sort state into Svelte (preferred, more work):**
- Move the sort logic into `ConversationView.svelte` as reactive state (`$state`).
- Parse `data-sort-rows` on mount. Store sort column + direction as component state.
- Use a derived table render that respects sort state, rather than DOM mutation.
- This makes sorting survive re-renders and integrates cleanly with Svelte 5.

**Option B — Signal Svelte not to touch the sorted DOM (quick, fragile):**
- Wrap the table in a Svelte `{@html}` block that is keyed on a non-reactive value.
- Use `{@html message.content}` inside a `{#key}` block with a stable key so
  Svelte skips re-rendering it.
- Risk: fragile against future Svelte behavior changes.

**Option C — Restore sort after Svelte re-render (band-aid):**
- Use a `MutationObserver` on the results card to detect when Svelte replaces
  the `tbody`, and re-apply the active sort.
- This keeps the existing DOM-mutation approach but patches over the reactivity
  conflict.
- Risk: performance overhead from observer, potential flicker.

### Recommended Path

Option A is the right long-term fix. Until then, users can still view all data
and export to CSV, which provides a sortable experience in spreadsheet apps.

### Verification After Fix

- Click a column header → rows reorder, indicator shows ↑/↓.
- Click the same header again → direction toggles.
- Click a different header → new column sorts ascending.
- Send a follow-up message in the conversation → table preserves its sort
  order (does not revert).
- Use "Show all X rows" → sorting works across the full dataset.
- Test with numeric columns, string columns, and date columns.

## 5. `window.confirm()` / `window.alert()` Do Not Work in Wails WKWebView (macOS)

- **Discovered:** 2026-08-16
- **Severity:** Medium — affected features silently fail instead of showing
  a confirmation dialog. No data loss, but user clicks destructive/restart
  buttons and nothing happens with zero feedback.
- **Affected files:**
  - `frontend/src/SettingsView.svelte:431` (reset all Agent Loop settings)
  - `frontend/src/SettingsView.svelte:720` (delete LLM provider)
  - `frontend/src/SettingsView.svelte:1128` (delete data source connection)
  - `frontend/src/App.svelte:495` (clear all messages)
  - All five calls use bare `window.confirm(...)` or `window.alert(...)`.
  - The new Database Switching feature (`SQLITE_DB_SWITCHER.md`) was the
    sixth call and was fixed during its own implementation
    (2026-08-16) by switching to a custom Svelte modal — see below.

### Root Cause

Wails v2.13.0's macOS frontend (`WKWebView`) does not implement the
`WKUIDelegate` methods `runJavaScriptConfirmPanelWithMessage` and
`runJavaScriptAlertPanelWithMessage`. Without these delegates, the webview's
`window.confirm()` returns `undefined` (falsy) silently with no dialog, and
`window.alert()` does nothing. There is no error thrown, no console warning
visible to end users — the JS call simply produces a non-blocking no-op.

Confirmed by auditing the Wails v2.13.0 source:
`internal/frontend/desktop/darwin/WailsContext.m` implements `WKUIDelegate`
but contains zero `runJavaScript*` selectors.

### Impact

| Call Site | File:Line | What Happens |
|---|---|---|
| `confirm('This will reset all Agent Loop settings...')` | SettingsView:431 | Reset button does nothing — user sees no dialog, settings are unchanged |
| `confirm('Are you sure you want to delete this provider?')` | SettingsView:720 | Delete button does nothing |
| `confirm('Are you sure you want to delete this connection?')` | SettingsView:1128 | Delete button does nothing |
| `confirm('Clear all messages...')` | App.svelte:495 | Clear messages button does nothing |
| (DB switcher — already fixed) | SettingsView | Now uses custom `.skill-editor-overlay` modal instead |

All four broken sites exhibit the identical symptom: user clicks a button
expected to show a confirmation dialog, and nothing happens. The underlying
action is never executed because `confirm()` returns falsy and the guard
`if (!confirm(...)) return` exits early.

### Fix Strategy

The DB switcher fix (implemented 2026-08-16, reference implementation at
`SettingsView.svelte` function `confirmDbAction` and the `{#if dbConfirm}`
modal block) demonstrates the pattern:

1. **State-driven confirmation.** Add a reactive state variable (e.g.
   `let pendingDeleteProvider = $state(null)`) that stores the ID/nature
   of the action awaiting confirmation.
2. **Replace `confirm()` with setting that state.** Instead of
   `if (!confirm(...)) return`, set `pendingDeleteProvider = providerId`.
3. **Render a confirmation modal** using SettingsView's existing
   `skill-editor-overlay` / `skill-editor` CSS classes (the same
   classes the DB switcher modal reuses). The modal shows the message
   and "Confirm" / "Cancel" buttons.
4. **Confirm executes the original action.** Cancel clears the state.

This is a drop-in pattern with no new CSS, no new Go-side code, and no
configuration changes. Each call site can be migrated individually;
they share no state.

### Why This Is a Wails Bug, Not a YourQL One

Wails v2 does not implement `runJavaScriptConfirmPanelWithMessage` or
`runJavaScriptAlertPanelWithMessage` in its macOS WKUIDelegate. Without
those, `confirm()`/`alert()` are non-functional in all Wails v2 macOS apps.
This is a framework-level gap, not a YourQL-specific issue. Upstream fix
would add the two WKUIDelegate methods to `WailsContext.m` and return the
appropriate values.

Until then, all Wails v2 macOS apps should use custom Svelte modals (or
Wails's own `runtime.MessageDialog` Go-side API) instead of
`window.confirm()` / `window.alert()`.

---

<!-- Add new entries above this line -->

## 4. "Show Context & Token Details" Toggle Non-Functional for Agentic Loop — ✅ PARTIALLY RESOLVED (2026-08-10)

- **Discovered:** 2026-08-10
- **Severity:** Medium
- **Affected files:**
  - `frontend/src/ConversationView.svelte:242–268` (`tokenSummary` derived)
  - `pkg/services/agentic_loop.go:173–203` (`TechDetail` struct)
  - `pkg/services/llm_openai.go:91–95` and `llm_anthropic.go:79–83` (usage discarded)
  - `pkg/services/llm_client.go:70–77` (`ChatMessage` — no usage field)

### Root Cause

Two separate breaks prevent token counts from appearing:

1. **The frontend reads from a field the agentic loop never writes.**
   `tokenSummary` iterates `conversationMessages`, parses each message's
   `metadata` JSON, and reads `response_json.usage`. In the old protocol
   every assistant message carries the raw LLM API response with `usage`.
   In the agentic loop, metadata has no `response_json` key — only
   `content_type` and optionally `chart_config`.

2. **The Go layer discards usage from LLM API responses.** The usage data
   is present in `openAIChatResponse.Usage` and `anthropicChatResponse.Usage`
   but never stored in `ChatMessage` (which has no usage fields) or
   `TechDetail` (which also has none).

Result: the "Show context & token details" header shows `↑ 0 ↓ 0 tokens`
for every agentic-loop conversation regardless of actual LLM usage.

The message count (`14 msgs`) is unaffected — it reads from
`conversationMessages.length`, which works correctly.

### Fix Strategy (Two Phases)

**Phase 1 — Plumb usage through to `TechDetail`:**
- Add `PromptTokens` / `CompletionTokens` to `ChatMessage` struct
- Populate from OpenAI non-streaming (`response.Usage`) and streaming
  (add `stream_options: include_usage` to capture usage in final chunk)
- Populate from Anthropic non-streaming (`response.Usage`)
  **Caveat:** Anthropic streaming does not return usage
- Add `PromptTokens` / `CompletionTokens` to `TechDetail` and populate
  in `logRound`
- Ollama/Local: leave at zero (API doesn't provide usage)

**Phase 2 — Frontend reads from tech details:**
- Rewrite `tokenSummary` to sum `prompt_tokens` / `completion_tokens`
  from `exploration`-role messages' tech detail JSON
- For providers where usage is unavailable, optionally estimate via
  `4 chars ≈ 1 token` from raw request/response lengths

### Provider Coverage

| Provider | Non-Streaming | Streaming |
|---|---|---|
| OpenAI | ✅ From `usage` | ✅ With `include_usage` |
| Anthropic | ✅ From `usage` | ❌ Not supported by API |
| Ollama | ❌ | ❌ |
| Local (OpenAI-compat) | ⚠️ Depends on server | ⚠️ Depends on server |

**Implemented 2026-08-10 (Phase 1 only — Go backend):**
- `ChatMessage` now carries `PromptTokens` / `CompletionTokens`
- OpenAI non-streaming and streaming (with `include_usage`) populate them
- Anthropic non-streaming populates them (streaming: not supported by API)
- `TechDetail` carries them; `logRound` populates from `response`
- **Frontend Phase 2 not yet implemented** — the `tokenSummary` derived
  still reads from the old `response_json.usage` path, so the header will
  still show `0` / `0` until the frontend is updated to sum from tech details.

## 3. Conversation History Context Exhaustion — ✅ RESOLVED (2026-08-10)

- **Discovered:** 2026-08-10
- **Severity:** High — after a turn with many exploration queries, subsequent
  messages produce empty responses because tool result tables exhaust context.
- **Status:** ✅ Implemented 2026-08-10. `formatToolResult` replaced with
  digest format; `stripReasoningFromArgs` added to `buildToolMessages`.

## 2. ~~Model Abandons Error Retry with `respond_to_user`~~ ✅ RESOLVED (2026-08-10)

- **Discovered:** 2026-08-10
- **Severity:** High — user sees a non-answer claiming results that don't exist.
  Happens on summarization-enabled conversations with complex queries.
- **Affected files:**
  - `pkg/services/agentic_loop.go` (error-retry path, `respond_to_user` guard)
  - `pkg/services/sql_execution.go` (`executeSQLWithMode` error formatting)
  - `pkg/services/agent_loop_config.go` (new config message)
  - `pkg/models/agent_loop_config.go` (new config field)

### Root Cause

When `query_database` fails (e.g., MySQL schema error "Unknown column"), the
model receives the raw error as a tool message. Instead of fixing the SQL and
retrying, the model defaults to a confident-sounding `respond_to_user` that
claims results were retrieved — but no corrected query was ever executed. The
system accepts this as a valid answer, delivering a text-only non-answer to
the user.

The existing error-retry loop allows retries (`maxErrorRetries`) but doesn't
prevent the model from exiting early via `respond_to_user`. The model's escape
hatch is too easy.

### Fix Strategy

**Three-part fix implemented 2026-08-10:**

**Part A — Block early exit via `respond_to_user`:** Track
`lastQueryHadError` in the agentic loop. When summarization is on and the
last `query_database` returned an error, reject bare `respond_to_user` with
feedback telling the model to fix and retry the query. Continue the loop.

**Part B — Better error messages (schema hints):** When a MySQL error
matches "Unknown column" (Error 1054), read the available columns and tables
from the live `DataSchema` and augment the error message with schema
context — which table the column exists on, what columns are available on
the referenced table, and a suggestion for the correct JOIN.

**Part C — Configurable feedback:** New config field
`ResponseQueryErrorRequiresRetry` with a default message that guides the
model back to correction.

### Verification After Fix

- Ask a question that requires a 3-table JOIN → model makes a column error →
  receives augmented error message with schema hints → retries with
  corrected SQL → delivers actual results → summary renders correctly.
- Model should not be able to call `respond_to_user` claiming results after
  a query failure (with summarization on).
- Non-summarization users are unaffected — they can still get
  `respond_to_user` after errors (to tell user "that query can't work").