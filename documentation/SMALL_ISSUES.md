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

---

<!-- Add new entries above this line -->