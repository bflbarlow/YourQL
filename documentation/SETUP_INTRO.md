# SETUP_INTRO.md — First-Time Walkthrough

> **Date:** 2026-08-10
> **Project:** YourQL — Guided first-launch setup and UI orientation
> **Status:** Design specification. Implementation pending.

---

## 1. Motivation

A new user launching YourQL for the first time sees this:

- An **empty discussion list** with "No discussions found" and a "Create a new
  discussion to start querying your data" hint under it.
- A sidebar with three icons: Discussions, Settings, About.
- No LLM provider configured. No data source connected. No defaults set.

They have to:
1. Guess that Settings exists and open it.
2. Figure out which tab to use (Models / DBs).
3. Fill out a connection form they've never seen before.
4. Go back to Discussions.
5. Click "New Discussion" — and now they're faced with an empty chat box and no
   guidance on what to say.

This is a **cold-start problem**. The app is capable but inscrutable to someone
who hasn't read the README. A guided walkthrough that appears on first launch
(and can be re-triggered later) solves this by:

- Showing the user **what the app does** before asking them to configure anything.
- Guiding them through **the two prerequisites** (provider + data source) in
  order, with forms inline in the walkthrough — no tab-hopping.
- Letting them configure **defaults** so every new discussion starts with the
  right model and database.
- Dropping them into their **first discussion** with a friendly prompt and a
  subtle cue about what to type.

---

## 2. Design Overview

### 2.1 Trigger

The walkthrough appears automatically when **all three of these are true**:

1. `conversations.length === 0` — no discussions have ever been created.
2. `llmProviders.length === 0` — no LLM provider has been configured.
3. The `setup_complete` flag in `app_settings` is **not** set to `"true"`.

After the user completes the walkthrough (or clicks "Skip for now" on any step),
`setup_complete` is set to `"true"` and the walkthrough never auto-shows again.

The user can re-launch the walkthrough manually from the About page via a
**"Re-run Setup"** link — useful if they skipped it and want it later, or if
they're helping a colleague get started.

### 2.2 Format

A **multi-step modal** with 5 steps. Each step is a card inside the modal with:

- A step indicator ("Step 2 of 5")
- A title and short description
- An illustration or icon (conceptual — use existing Lucide icons)
- The action for that step (a form, a button, or a confirmation)
- Navigation: "Back" (where applicable), "Skip for now" (always available),
  and the primary action button

The modal is wide enough to host inline forms without feeling cramped, and uses
the app's existing modal styling pattern (`modal-overlay` + `modal` classes
from `App.svelte`).

### 2.3 Steps

| Step | Title | What happens |
|---|---|---|
| 1 | **Welcome** | Explains what YourQL does in 2–3 sentences. Shows the three-step journey: configure → connect → ask. "Get Started" button. |
| 2 | **Set Up an LLM** | Inline form: provider name, type (OpenAI/Anthropic/Ollama), model, API key. Same fields as the Settings → Models form, but simplified. Test Connection button. |
| 3 | **Connect a Database** | Inline form: name, type (dropdown of all registered drivers), host/port/database/username/password. Test Connection button. |
| 4 | **Set Defaults** | Pre-filled with the provider + data source just created. Additional toggles: summarize, viz, tech details. "Save & Continue" or "Skip — I'll configure later." |
| 5 | **Start Asking** | Confirmation: "You're all set!" Summary of what's configured. A "Go to Your First Discussion" button that creates the first discussion with the defaults from Step 4 and opens it. |

### 2.4 "Skip for now"

Available on every step. Sets `setup_complete = "true"` and dismisses the
walkthrough. The user can configure things later through Settings and create
discussions manually — the walkthrough is a convenience, not a gate.

On the Welcome step, "Skip for now" is prominent (secondary button next to
"Get Started"). On later steps, it's a subtle text link below the primary
button — the user has committed to the walkthrough at that point, so skipping
is still possible but not encouraged.

---

## 3. Detailed Step Design

### 3.1 Step 1 — Welcome

```
┌──────────────────────────────────────────────────────────────┐
│  ✨ Welcome to YourQL                      Step 1 of 5       │
│                                                              │
│  YourQL lets you talk to your databases in plain English.    │
│  Ask questions the way you'd ask a colleague — and YourQL    │
│  translates them into accurate, safe SQL queries, giving     │
│  you answers in tables, charts, and summaries.               │
│                                                              │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐                  │
│  │ 1. Add  │ →  │ 2. Add  │ →  │ 3. Ask  │                  │
│  │  a model│    │  a DB   │    │ away    │                  │
│  └─────────┘    └─────────┘    └─────────┘                  │
│                                                              │
│  Your data stays on your machine. No telemetry. No cloud.    │
│  Your database is read-only — YourQL never writes to it.     │
│                                                              │
│            [ Skip for now ]    [ Get Started → ]             │
└──────────────────────────────────────────────────────────────┘
```

**States:**
- "Get Started" advances to Step 2.
- "Skip for now" sets `setup_complete = "true"` and closes the modal.

### 3.2 Step 2 — Set Up an LLM

The inline form mirrors the Settings → Models form but is simplified for a
first-time user: no advanced fields (max tokens, context window, base URL)
unless the user clicks "Show advanced."

```
┌──────────────────────────────────────────────────────────────┐
│  🤖 Set Up an LLM Provider                    Step 2 of 5    │
│                                                              │
│  YourQL uses an AI model to translate your questions into    │
│  SQL and explain the results. Add your first provider below. │
│                                                              │
│  Name:  [ My OpenAI Account                        ]         │
│  Type:  [ OpenAI ▼ ]                                        │
│  Model: [ gpt-4o                 ]                           │
│  API:   [ sk-...                                ]  [••]     │
│                                                              │
│  [Show advanced ▸]                                          │
│                                                              │
│  [ Test Connection ]   ✓ Connected successfully              │
│                                                              │
│         [ ← Back ]          [ Skip ]    [ Continue → ]       │
└──────────────────────────────────────────────────────────────┘
```

**Validation:**
- Name is required.
- Type must be one of the supported providers (OpenAI, Anthropic, Ollama).
- Model is required.
- API key is required (hidden in the UI behind a show/hide toggle, matching
  the Settings pattern).
- "Continue" is disabled until "Test Connection" returns success. If the test
  fails, show the error inline — don't make the user guess what went wrong.

**On Continue:** The provider is saved to `llm_providers` via
`CreateLLMProvider`. If it succeeds, advance to Step 3.

**"Show advanced"** reveals optional fields (base URL for custom endpoints,
max tokens, context window) — useful for Ollama / local setups. Collapsed by
default to avoid overwhelming new users.

### 3.3 Step 3 — Connect a Database

Same pattern as Step 2 — simplified inline form for the data source.

```
┌──────────────────────────────────────────────────────────────┐
│  🗄️ Connect a Database                        Step 3 of 5   │
│                                                              │
│  Connect YourQL to a database you want to ask questions      │
│  about. YourQL will only run read-only queries — your data   │
│  is never modified.                                          │
│                                                              │
│  Name: [ My Production DB                         ]          │
│  Type: [ PostgreSQL ▼ ]                                     │
│                                                              │
│  Host: [ db.example.com        ]  Port: [ 5432 ]             │
│  DB:   [ analytics             ]                             │
│  User: [ readonly              ]                             │
│  Pass: [ ••••••••              ]  [••]                       │
│                                                              │
│  [ Test Connection ]   ✓ Connected — found 12 tables         │
│                                                              │
│         [ ← Back ]          [ Skip ]    [ Continue → ]       │
└──────────────────────────────────────────────────────────────┘
```

**Validation:**
- Name and Type are required.
- For `mysql` / `postgres` / `sqlserver` / `mariadb` / `redshift` / `snowflake`:
  host + database + username are required, port defaults to the driver's
  default.
- For `sqlite`: only the file path is needed (use a file picker).
- For `csv` / `xlsx`: only the file path is needed.
- For `bigquery` / `sheets`: show a note that OAuth setup will be handled in
  Settings after the walkthrough — provide a "Skip" path directly from the
  type selector for these types.
- "Continue" is disabled until "Test Connection" returns success.

**On Continue:** The data source is saved via `CreateDataSource`. If it
succeeds, advance to Step 4.

### 3.4 Step 4 — Set Defaults

Pre-fills with the provider and data source just created. The user can also
set other discussion defaults here.

```
┌──────────────────────────────────────────────────────────────┐
│  ⚙️ Set Your Defaults                         Step 4 of 5   │
│                                                              │
│  These defaults apply to every new discussion. You can       │
│  change them later in Settings → Defaults, or override them  │
│  per discussion from the gear menu.                          │
│                                                              │
│  Default LLM:       [ My OpenAI Account (gpt-4o) ▼]         │
│  Default Database:  [ My Production DB (PostgreSQL) ▼]       │
│                                                              │
│  ── Toggles ──                                               │
│  [✓] Summarize results into plain English                    │
│  [✓] Generate charts when useful                             │
│  [ ] Show the SQL that was run (tech details)                │
│                                                              │
│         [ ← Back ]          [ Skip ]    [ Save & Continue → ]│
└──────────────────────────────────────────────────────────────┘
```

**States:**
- Provider and data source dropdowns default to the items created in Steps 2–3.
  If the user skipped either step (somehow), show "(none)" and they can skip
  this step.
- Summarize defaults to `true` for new users (it's the friendlier experience).
- Viz defaults to `true`.
- Tech details defaults to `false` (power-user feature).

**On Continue:** Calls `UpdateDiscussionDefaults(...)` to persist. Advances
to Step 5.

### 3.5 Step 5 — Start Asking

```
┌──────────────────────────────────────────────────────────────┐
│  🚀 You're All Set!                           Step 5 of 5   │
│                                                              │
│  Here's what you configured:                                 │
│                                                              │
│    🤖  LLM:      My OpenAI Account (gpt-4o)                  │
│    🗄️  Database: My Production DB (PostgreSQL)               │
│    ⚙️  Defaults:  Summaries on, Charts on                    │
│                                                              │
│  ──────────────────────────────────────────                  │
│                                                              │
│  Ready to ask your first question? Your new discussion       │
│  opens with a prompt to get you started. Try something       │
│  like "How many rows are in the largest table?" or ask       │
│  a question about your own data.                             │
│                                                              │
│  [ ← Back ]    [ Skip ]    [ Go to My First Discussion → ]   │
└──────────────────────────────────────────────────────────────┘
```

**On Continue:**
1. Creates the first discussion via `CreateConversation("Getting Started",
   providerID, dataSourceID)`, which auto-applies the defaults from Step 4.
2. Closes the walkthrough modal.
3. Sets `setup_complete = "true"`.
4. Navigates to the new discussion (same as the existing
   `handleCreateDiscussion` flow).
5. The chat view opens with a **welcome message** pre-populated in the
   conversation (a system message in the `conversation_messages` table,
   role `"system"`, inserted during discussion creation when it's the
   setup-created conversation):

   > Welcome to YourQL! 👋
   >
   > I'm connected to **My Production DB** (PostgreSQL) and using **gpt-4o**.
   >
   > Try asking:
   > * "What tables are available?"
   > * "Show me the first 10 rows of `<a table>`"
   > * "How many rows are in each table?"
   > * Or any question about your data — I'll figure out the SQL.
   >
   > Your database is read-only — I can't change anything.

---

## 4. Implementation

### 4.1 Backend

#### New app_setting flag

No schema change needed — `app_settings` is already a key-value table. Add one
new key:

| Key | Value |
|---|---|
| `setup_complete` | `"true"` once the walkthrough is completed or skipped |

#### Service helpers

No new service file needed. The walkthrough uses existing functions:
- `CreateLLMProvider` / `TestLLMProviderConnection`
- `CreateDataSource` / `TestDataSource`
- `UpdateDiscussionDefaults`
- `CreateConversation` / `GetConversationMessages`
- `SetAppSetting("setup_complete", "true")`

One new helper in `conversation.go` for the welcome message:

```go
// CreateFirstConversation creates a discussion and inserts a welcome
// system message so the user sees guidance on their first open.
func CreateFirstConversation(title string, llmProviderID, dataSourceID *uint) (*models.Conversation, error) {
    conv, err := CreateConversationWithDefaults(title, llmProviderID, dataSourceID)
    if err != nil {
        return nil, err
    }
    // Insert a welcome system message.
    welcomeMsg := buildWelcomeMessage(llmProviderID, dataSourceID)
    _, err = models.DB.Exec(
        "INSERT INTO conversation_messages (conversation_id, role, content, created_at) VALUES (?, 'system', ?, CURRENT_TIMESTAMP)",
        conv.ID, welcomeMsg,
    )
    if err != nil {
        return conv, nil // non-fatal — the conversation is still usable
    }
    return conv, nil
}
```

`buildWelcomeMessage` resolves the provider and data source display names
(reusing the existing `resolveExportNames` pattern from `app.go`) and builds
the welcome text shown in §3.5.

#### Wails bindings

One new binding, or reuse existing — the walkthrough calls the same public
methods the Settings page already calls. No new bindings strictly required if
the frontend orchestrates the flow:

- Check `setup_complete` flag: existing `GetAppSetting("setup_complete")`.
- Create provider: existing `CreateLLMProvider(...)`.
- Test provider: existing `TestLLMProviderConnection(...)`.
- Create data source: existing `CreateDataSource(...)`.
- Test connection: existing `TestDataSource(...)`.
- Save defaults: existing `UpdateDiscussionDefaults(...)`.
- Create first discussion: existing `CreateConversation(...)` — or the new
  `CreateFirstConversation(...)` wrapper if the welcome message is desired.

If the welcome message is wanted (which it should be — it's the payoff for
the entire walkthrough), expose the new helper:

```go
func (a *App) CreateFirstConversation(title string, llmProviderID, dataSourceID *uint) (*models.Conversation, error)
```

If this feels like too many round-trips (CreateLLMProvider → TestLLMProvider →
CreateDataSource → TestDataSource → UpdateDiscussionDefaults → CreateConversation),
a single composite endpoint is acceptable but not required — the walkthrough
is linear and each step has its own user interaction, so network latency per
step is natural.

### 4.2 Frontend

#### New component: `SetupWalkthrough.svelte`

A new Svelte component that renders the multi-step modal. It manages:

- `currentStep` (1–5)
- Step-specific state (provider form fields, data source form fields, test
  results, defaults form)
- Navigation logic (back, skip, advance)
- The `setup_complete` flag

The component is imported in `App.svelte` and rendered as a sibling to the
existing `showNewDiscussion` modal:

```svelte
{#if showSetupWalkthrough}
  <SetupWalkthrough
    llmProviders={llmProviders}
    dataSources={dataSources}
    onComplete={handleSetupComplete}
    onSkip={handleSetupSkip}
  />
{/if}
```

#### Trigger logic in `App.svelte`

In `onMount` (or after `loadData()` completes), check:

```js
async function checkSetupWalkthrough() {
  if (conversations.length > 0) return
  if (llmProviders.length > 0) return
  try {
    const val = await GetAppSetting('setup_complete')
    if (val === 'true') return
  } catch { /* key doesn't exist — proceed */ }
  showSetupWalkthrough = true
}
```

This is called once after both `loadData()` and `ListLLMProviders()` have
resolved — it needs both arrays loaded to make the decision.

#### Re-run from About page

Add a link in the About page, below the auto-update section:

```svelte
<a href="#" onclick={() => showSetupWalkthrough = true}>Re-run Setup Walkthrough</a>
```

This bypasses the `setup_complete` check — it's an explicit user action.

### 4.3 Files to create or modify

| File | Action |
|---|---|
| `frontend/src/SetupWalkthrough.svelte` | **New** — the walkthrough component |
| `frontend/src/App.svelte` | **Modify** — add `showSetupWalkthrough` state, trigger check, render the component, add "Re-run Setup" link to About page |
| `pkg/services/conversation.go` | **Modify** — add `CreateFirstConversation` helper |
| `app.go` | **Modify** — add `CreateFirstConversation` binding |
| `documentation/SETUP_INTRO.md` | **New** — this document |

### 4.4 Dependencies

None. All actions use existing Wails bindings (`CreateLLMProvider`,
`CreateDataSource`, `UpdateDiscussionDefaults`, `CreateConversation`,
`GetAppSetting`, `SetAppSetting`). No new Go libraries.

---

## 5. UX Design Notes

### 5.1 Tone

The walkthrough should feel **friendly and confident**, not apologetic or
overly technical. The user just downloaded a desktop app that claims to let
them "talk to their database" — the walkthrough should make good on that
promise within 2 minutes.

### 5.2 When the user has prerequisites already configured

If the user launches YourQL and already has a provider and data source
configured (e.g., they restored a `yourql.db` from another machine, or they
set things up in a previous version before this walkthrough existed), the
walkthrough does **not** auto-show. The trigger conditions require *both*
`conversations.length === 0` AND `llmProviders.length === 0`. A user with
providers but no conversations can still create a discussion manually — they
don't need the walkthrough because they already know the app.

If they want the walkthrough anyway, "Re-run Setup" on the About page works
regardless.

### 5.3 Edge cases

| Scenario | Behavior |
|---|---|
| User refreshes mid-walkthrough | Walkthrough state is not persisted across sessions — it starts from Step 1 again. This is acceptable because the steps are idempotent (creating a provider with the same name would fail — show the error inline). |
| Provider test fails | Show the error inline. "Continue" stays disabled. User can fix the form or skip. |
| Data source test fails | Same as provider. For connection-timeout errors, show a hint ("Check your host and port — make sure the database is accessible from this machine"). |
| User picks BigQuery / Google Sheets | These require OAuth setup that can't be completed inline. Show a note: "BigQuery requires OAuth setup — you can configure this in Settings after the walkthrough. Skip this step for now?" Provide a "Skip" button that advances without a data source. |
| User closes the modal via X or overlay click | Treat the same as "Skip for now" — set `setup_complete = "true"` and close. |
| All steps completed but CreateConversation fails | Show the error inline on Step 5 and let the user retry. Don't dismiss the walkthrough on failure. |

---

## 6. Implementation Plan

| Phase | What | Effort |
|---|---|---|
| 1 | **Backend:** `CreateFirstConversation` + welcome message helper in `conversation.go`, binding in `app.go` | Tiny |
| 2 | **Frontend:** `SetupWalkthrough.svelte` component — step framework, navigation, state management | Medium |
| 3 | **Frontend:** Step 1 (Welcome) — static content, icon | Tiny |
| 4 | **Frontend:** Step 2 (LLM provider) — inline form with test, save via existing bindings | Small |
| 5 | **Frontend:** Step 3 (Data source) — inline form with test, type-dependent fields, save | Small |
| 6 | **Frontend:** Step 4 (Defaults) — pre-filled form, save via existing binding | Tiny |
| 7 | **Frontend:** Step 5 (Confirmation + first discussion) — summary display, create + navigate | Tiny |
| 8 | **Frontend:** Trigger logic in `App.svelte` — `checkSetupWalkthrough`, `showSetupWalkthrough` state | Tiny |
| 9 | **Frontend:** "Re-run Setup" link on About page | Tiny |
| 10 | **Testing:** Full walkthrough flow, skip at each step, reopen, test failure states, BigQuery skip path | Small |

Total effort: **2–3 work sessions.** The component is the bulk (Phases 2–7).
All backend work is reuse of existing endpoints plus one tiny helper.

---

## 7. Risk Assessment

Per `AGENT_READ_FIRST.md` §4.

| Priority | Impact |
|---|---|
| **#1 Correctness** | **None.** Walkthrough is a UI-only flow — no LLM, SQL, or answer pipeline is involved. The welcome message inserted into the first conversation is a static system message. |
| **#2 Safety** | **None.** All actions go through existing, tested Wails bindings (`CreateLLMProvider`, `CreateDataSource`, `TestDataSource`, etc.). No new data paths. Credentials are entered through the same form pattern the Settings page already uses (API keys hidden behind show/hide toggle, passwords not logged). |
| **#3 Reliability** | **Low risk.** Each step is independent — if one fails, the user can retry or skip. The walkthrough doesn't gate access to the app (Skip is always available). |
| **#4 Appeal / Comfort** | **Significant improvement.** This directly addresses the biggest barrier to adoption — the cold-start problem where a new user opens a blank screen with no guidance. |

### Failure modes

1. **Provider creation succeeds but the frontend state doesn't update.**
   Mitigation: after `CreateLLMProvider`, re-fetch `llmProviders` via
   `ListLLMProviders()` before advancing. The `llmProviders` prop passed to
   `SetupWalkthrough` is updated in the parent via a callback.

2. **"Test Connection" hangs on a slow/unreachable host.** Mitigation: the
   existing `TestDataSource` binding already has a timeout (via Go's
   `context.WithTimeout`). If the test times out, show "Connection timed out —
   check that the host is reachable."

3. **Walkthrough state lost on accidental close.** Mitigation: the skip action
   explicitly sets `setup_complete = "true"`, so the user won't see the
   walkthrough again on next launch. They can "Re-run Setup" from About if
   they want to try again. Acceptable trade-off vs. persisting walkthrough
   state across sessions.

### Decision

**Proceed.** Purely additive. No risk to the fundamental goal. Existing
bindings are reused, not modified. The walkthrough is a convenience, not a
gate — Skip is always one click away. The implementation is self-contained in
one new Svelte component and one tiny backend helper.

---

## 8. Charter Compliance

Per `AGENT_READ_FIRST.md`:

- [ ] **Is this change aligned with the fundamental goal?** Indirectly — a
      guided setup helps users reach their first answer faster. The walkthrough
      itself does not affect answer quality or delivery.
- [ ] **Read-only invariant preserved?** Yes — no data source interaction
      beyond the user-initiated "Test Connection" (which runs a read-only
      schema introspection, not a query).
- [ ] **SQL execution safety preserved?** Yes — no SQL execution path is
      added or modified.
- [ ] **Migration safety preserved?** Yes — no schema changes. Only a new
      `app_settings` key (`setup_complete`).
- [ ] **API keys/passwords not logged?** Yes — the walkthrough uses the same
      `CreateLLMProvider` / `CreateDataSource` bindings that already don't log
      credentials.
- [ ] **Existing conversations backward compatible?** Yes — the walkthrough
      only fires when there are zero conversations and zero providers.
- [ ] **UI responsive?** The walkthrough is a modal with local state — no
      network calls except the ones the user explicitly triggers (Test
      Connection, Save).
- [ ] **Dark/light mode?** The modal inherits the app's existing modal CSS
      pattern and uses CSS variables. Test in both themes.
- [ ] **Documentation updated?** This document is the spec.

---

## 9. Future Considerations

- **Animated transitions between steps.** A subtle slide or fade between walkthrough
  steps would improve the perceived polish. Out of scope for v1 — use instant
  swaps and add transitions later.
- **Skip-to-step.** If the user already has a provider (but no data source), they
  shouldn't re-do Step 2. Detect pre-existing state and jump to the first
  incomplete step. Out of scope for v1 — the current trigger only fires when
  *both* `conversations.length === 0` AND `llmProviders.length === 0`, so the
  walkthrough always starts from Step 1 for the target audience.
- **Contextual tooltips after the walkthrough.** Once the user is in their first
  discussion, subtle tooltips could highlight the gear menu, tech details toggle,
  and export button. Out of scope for v1 — the welcome system message in the
  first discussion provides the minimum orientation.
