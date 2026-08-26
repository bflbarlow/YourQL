# Minimalist Mode V2

> **Concept:** The application becomes transparent. A window that shows the
> user their questions and answers, and nothing else. The technology fades
> away — no chrome, no navigation, no configuration noise. Just a clean
> surface where a question goes in and an answer comes out.

---

## 0. What We Got Wrong in V1

The V1 implementation treated minimalist mode as "hide the sidebar and some
buttons." That's not a mode — that's a toggle with less chrome. A real
minimalist mode changes the *feeling* of the application. It should feel like
a different product, not a trimmed version of the same product.

| V1 Thought | V2 Thought |
|---|---|
| "Hide the sidebar" | "There is no sidebar — the app has one surface" |
| "Remove buttons from the conversation header" | "The conversation header doesn't exist — the message area is all there is" |
| "Add a toggle in Settings" | "The user never visits Settings — the app handles it" |
| "Quick switch to exit" | "The switch is so subtle you forget it's there until you need it" |

**The fundamental mistake in V1:** we started by listing what to remove. V2
starts by imagining what should *remain* — and then strips everything that
isn't that. The result isn't a reduced version of the full app. It's a
distillation.

---

## 1. The Core Experience

### 1.1 What It Feels Like

The user opens YourQL. There is nothing to configure, nothing to navigate.
They type a question. They get an answer.

That's it.

Behind the scenes, the app is doing exactly what it always does — connecting
to a data source, sending prompts to an LLM, running safe SQL, rendering
results. But the user doesn't see any of that machinery. They see a clean
surface, a question, and an answer.

**The golden rule for every design decision in this mode:**

> Would a person who has never used YourQL before understand what to do
> within 3 seconds of seeing this screen?

If the answer is no, the element doesn't belong in minimalist mode.

### 1.2 What the User Sees on Launch

When the app opens in minimalist mode, the user sees:

- A single, centered prompt: *"Ask a question about your data."*
- An input box below it.
- Nothing else.

No conversations list. No settings gear. No sidebar. No provider names. No
"New Discussion" button. No toolbar. The app is a void with a prompt in the
middle.

When they type a question and press Enter:

- A brief, invisible moment passes (the app sends their question through the pipeline)
- The answer appears — data table, summary text, optional chart — flowing up from the input area
- The input moves to the bottom of the screen, and the answer fills the space above it
- The user can scroll up through their conversation, ask a follow-up, or start fresh

### 1.3 The Conversation Surface

The conversation area is the *entire application*. It occupies 100% of the
window with generous padding — centered, calm, and readable.

- **Messages are the only UI.** User questions on the right (or highlighted
  inline), assistant answers on the left.
- **No message metadata.** No timestamps, no token counts, no model names,
  no data source tags — unless the user explicitly taps/right-clicks a
  message to reveal them.
- **Tables and charts render inline** with no wrapping cards, no "Results"
  labels, no export buttons — just the data, styled cleanly.
- **Streaming** animates the answer into existence character by character,
  with the cursor blinking at the end of the last line.
- **Follow-up questions** are typed into the same input at the bottom. The
  thread scrolls up naturally, like any chat app.

### 1.4 The User's Model of the App

In minimalist mode, the user doesn't think about "conversations," "data
sources," or "LLM providers." The app manages those choices automatically:

- **It remembers the last used data source and LLM provider** and uses them
  by default. If the user has never configured one, the app picks the first
  available.
- **It remembers the user's conversation thread** and resumes it on next
  launch, the same way a messaging app shows your last chat when you open it.
- **If the user wants to start fresh**, they type `/new` or click a subtle
  "New" affordance — the previous thread is preserved but hidden.
- **If the user wants to switch data sources**, they type `/source` and a
  minimal inline selector appears — no panel, no page navigation.

The mental model is: *"I'm talking to my data. The app is just the
messenger."*

### 1.5 Switching Discussions

Sometimes the user needs to switch to a different conversation — to revisit
a previous topic or start fresh. In minimalist mode, this must feel as
seamless as everything else.

**Triggered by `/history` or a subtle afforance** (a small "◷" icon near
the top of the conversation surface). Either method opens the discussions
list as a clean, centered overlay or inline panel.

**The discussions list in minimalist mode:**
- A simple, centered list of thread titles — no dates, no tags, no provider
  names, no data source labels, no pin indicators, no archive badges.
- Threads are ordered by last activity, most recent first.
- A single "+ New discussion" option at the top of the list.
- Tapping a title immediately loads that conversation and closes the list.
- No filters, no search bars, no checkboxes, no multi-select. This is a
  picker, not a management panel.
- If the list is long, it scrolls. No pagination.

**The flow:**
1. User types `/history` or taps the ◷ icon.
2. The current conversation slides away (or dims).
3. The discussions list appears — clean titles, nothing else.
4. User taps a title → that conversation loads, list disappears.
5. User taps "+ New" → a fresh conversation starts with the same LLM and
   data source defaults.

This is how platforms like ChatGPT handle conversation switching — a simple
sidebar or overlay with titles only. It's proven, it's fast, and it keeps
the user focused on their data, not on app navigation.

### 1.6 What Gets Minimalist Treatment (and What Doesn't)

Minimalist mode applies to almost every screen in the application, but with
different levels of priority:

| Screen | Priority | Treatment |
|---|---|---|
| **Individual discussion (conversation view)** | **Primary** | Full minimalist redesign — see §1.3 and Phase 2. This is where the user spends 95% of their time. |
| **Discussions list** | **High** | Gets its own minimalist view — clean, centered titles with no metadata. The user enters minimalist mode while on this page, they see a minimalist discussions list, not a jumped-to conversation. |
| **About page** | **Low** | Gets its own minimalist view — centered app name and description only. No update section, no feature list. |
| **Settings page** | **Light touch** | Slightly simplified — tighter spacing, no illustrations, but the same sections and controls. Settings is where the user goes to *configure things*, not to ask questions. Should feel familiar, not reinvented. The minimalist mode toggle itself lives here (General tab). |

**The priority order is intentional.** Every hour spent polishing the
conversation surface is an hour well spent. Every hour spent redesigning
Settings in minimalist mode is an hour that could have been spent making
the answer experience better. Keep Settings as-is and redirect that energy
back to the discussion surface.

---

## 2. Visual Language

### 2.1 The App Is a Frame

Think of the app window as a glass pane. The content — questions and answers
— is the only thing that has visual weight. Everything else is translucent or
absent.

- **Background:** The app's background is the system background (or a very
  light/dark neutral). No borders, no shadows, no panels.
- **Input box:** A single-line text area with no border, no background fill,
  just a subtle underline or a thin pill shape. It looks like an extension
  of the answer area, not a separate control.
- **Messages:** Clean typography with ample whitespace. Assistant messages
  in a readable serif or sans-serif. User messages slightly differentiated
  by color or indentation, not a full bubble.
- **Scrollbar:** Hidden by default. Reveals on hover or scroll.

### 2.2 Typography as Structure

With no chrome to provide structure, typography does the work:

- **Question text:** Slightly bolder, aligned to one side or inset
- **Answer text:** Normal weight, full width, comfortable line-height
- **SQL (when revealed):** Monospace, smaller, in a subtly shaded block
- **Table headers:** Small caps, muted color
- **Table data:** Tabular numbers, aligned for scanning

### 2.3 The Invisible Layer

Some things exist but are invisible until summoned:

| Action | Trigger |
|---|---|
| Navigate to Discussions | Hamburger menu (top-left, §4.4) → Discussions |
| Navigate to Settings | Hamburger menu → Settings |
| Navigate to About | Hamburger menu → About |
| Show SQL | Tap/hover a result → inline toggle appears |
| Switch data source | Type `/source` in the input |
| Switch LLM provider | Type `/model` in the input |
| See conversation history | Type `/history` or swipe from a hidden edge |
| Export results as CSV | Long-press a table or right-click |
| Exit minimalist mode | Type `/exit` or press `Cmd+Shift+M` |

The goal: everything the user can do in the full app is also possible in
minimalist mode — but none of it is visible until they ask for it.

---

## 3. Smart Defaults (The App Handles It)

Minimalist mode works because the app makes good decisions for the user. It
never asks the user to configure something they don't care about.

### 3.1 Auto-Select LLM and Data Source

On launch:
1. Use the last-used provider and data source (stored per-session).
2. If none, use the user's defaults (`discussion_defaults` table).
3. If no defaults, use the first available provider and data source.
4. If still nothing, show a one-time setup prompt: *"Connect a data source
   to get started."* — but only once.

### 3.2 Auto-Name Conversations

In the full app, conversations have titles. In minimalist mode, the user
never sees titles. The app auto-names threads based on the first question
asked (e.g., "Revenue by region — Aug 19"). This exists only so the
conversation can be resumed later.

### 3.3 Resume on Relaunch

When the user quits and reopens YourQL, minimalist mode resumes exactly
where they left off: same conversation, same messages, same input position.
The app feels like it never closed.

### 3.4 Safety Remains Absolute

None of this changes the safety guarantees. Every query is `SELECT`-only.
Every data source is read-only. The `AGENT_READ_FIRST.md` invariants are
fully preserved — the app just doesn't show the safety mechanisms unless
they're breached (e.g., a query error).

---

## 4. The Toggle Between Modes

### 4.1 Entering Minimalist Mode

The user can enter minimalist mode by:
- Clicking the persistent toggle — a prominent button in the full-app
  sidebar (replaces the "+ New Discussion" footer area when minimalist
  mode is available). See §4.3.
- Toggling in Settings → General
- Pressing `Cmd+Shift+M` (macOS) / `Ctrl+Shift+M` (Windows/Linux)

When entering minimalist mode from the full app:
- **Every page gets its own minimalist version.** No page is skipped.
- If the user is in a conversation, that conversation becomes the
  minimalist discussion surface.
- If the user is on the Discussions list page, the list becomes the
  minimalist discussions view — a clean, centered list of titles with
  no metadata (see §1.5).
- If the user is on the About page, it becomes the minimalist About
  page — centered app name and description only.
- If the user is in Settings, the Settings page gets a slightly
  simplified treatment (tighter spacing, no illustrations) but remains
  largely recognizable — Settings is where configuration happens and
  shouldn't be reinvented.

### 4.2 Exiting Minimalist Mode

The user can exit by:
- Clicking the persistent toggle — the same element, now in its minimalist
  hover-reveal state (see §4.3)
- Pressing `Cmd+Shift+M` / `Ctrl+Shift+M`
- Typing `/exit` in the input
- Tapping the hamburger menu in the top-left corner of the minimalist
  view (see §4.4) and selecting the "Exit minimal" option

When exiting, the full app reappears with the same page selected.

### 4.3 The Persistent Toggle

A button that lives in two very different visual forms depending on mode,
but is always findable and always bound to the same action.

**Full mode:**
- Lives in the sidebar footer — exactly where the "+ New Discussion"
  button currently lives in the full app. It *replaces* that button area
  when minimalist mode is supported (the New Discussion function is still
  available in the discussions list header).
- Styled as a visible, intentional button: "Minimal Mode" with a small
  icon, using `var(--color-accent)` text on a subtle
  `var(--color-accent-light)` background.
- It is not hidden, not low-opacity, not subtle. It is a real affordance
  that a user will discover naturally.
- On hover: background intensifies slightly, cursor pointer.

**Minimalist mode:**
- Becomes a small, restrained element anchored to the top-right corner of
  the viewport — a thin pill that reads "Exit" or shows a ◐ icon. Fixed
  position, z-index above content but never overlapping the input area.
- Rest state: `opacity: 0.2` — barely visible, nearly transparent. It
  does not compete with the answer for attention.
- Hover state: `opacity: 0.7` — blows up to a readable label. The user
  knows it's there when they need it.
- Never overlaps the submit/send button because it is anchored to the
  top-right, not the bottom.

**Why it must exist:** The keyboard shortcut is not discoverable. The
Settings toggle requires navigating to Settings. The persistent toggle is
the **always-visible, always-available** affordance — the one thing a
user can always find, regardless of how they arrived at the current
screen. It is the primary entry and exit point. Everything else is
secondary.

### 4.4 Light Navigation in Minimalist Mode

Minimalist mode does not ban navigation — it just strips it down to
only what's necessary.

**Hamburger menu (top-left):**
- A small, circular button (24px icon) anchored to the top-left corner
  of the viewport. `opacity: 0.25` at rest, `0.7` on hover.
- On click: reveals a compact dropdown with three items:
  - Discussions
  - Settings
  - About
  - (divider)
  - Exit minimal mode
- Selecting Discussions switches to the minimalist discussions list.
- Selecting Settings or About switches to their minimalist versions.
- Selecting "Exit minimal" exits minimalist mode.
- The dropdown dismisses on selection, on outside click, or on Escape.

**No other navigation chrome is present.** No persistent sidebar, no
tab bar, no breadcrumbs. The hamburger menu is the single navigation
point — present on every minimalist screen, always in the same position.

---

## 5. Technical Project Plan

This is a significant rework of the frontend. The Go backend and agentic
loop require **zero changes** — the pipeline, safety, and LLM integration
are unchanged. This is purely a frontend transformation.

### Phase 1: Foundation — A New Root View

**Create a new `MinimalistView.svelte` component** that is the sole rendered
view when minimalist mode is active. This is not a modification of
`App.svelte` — it's a parallel rendering path.

**The persistent toggle** lives in two locations depending on mode:
- Full mode: inside the sidebar footer (replaces the "+ New Discussion"
  button area). It is part of the `{:else}` full-app layout, styled
  prominently.
- Minimalist mode: rendered inside `MinimalistView.svelte` (not at the
  `App.svelte` root) as a thin pill anchored to the top-right corner,
  hover-revealed. See §4.3.

**The hamburger menu** also lives inside `MinimalistView.svelte`,
anchored top-left. See §4.4.

**MinimalistView handles all minimalist pages via an internal router:**

```
MinimalistView.svelte internal routing:

  on mount: read initialView from prop (set by App.svelte when entering)

  {#if currentMinimalistView === 'discussion'}
    <!-- the full discussion surface: thread + input + streaming -->
  {:else if currentMinimalistView === 'discussions'}
    <!-- minimalist discussions list -->
  {:else if currentMinimalistView === 'settings'}
    <!-- simplified SettingsView (or the existing one, slightly styled) -->
  {:else if currentMinimalistView === 'about'}
    <!-- minimalist About page -->
  {/if}
```

This keeps the existing code completely untouched and makes the minimalist
experience independently testable.

### Phase 2: `MinimalistView.svelte` — The Core

**Files to create:**
- `frontend/src/MinimalistView.svelte`

**Props it receives:**
- `initialView` — which page to show first (`'discussion'`, `'discussions'`,
  `'settings'`, `'about'`). Set by `App.svelte` based on which full-app
  page the user was on when they entered minimalist mode.
- `llmProviders`, `dataSources` — for auto-selection
- `conversations` — full list of conversations (for discussions list)
- `activeConversation`, `conversationMessages` — current thread
- `processingActive` — whether a pipeline run is in progress
- `onSendMessage`, `onCancelProcessing` — pipeline hooks
- `onExit` — exit minimalist mode
- `onConversationChange` — switch to a different conversation
- `onMessageSent` — callback after a message send completes

**What it renders (depends on the current conversation, not on minimalist mode itself):**

`MinimalistView` is a single Svelte component that renders the minimalist
experience for the currently active discussion. It reads the `minimalist`
boolean from its parent (`App.svelte`) and uses it to determine *how* to
render, not *what* to render. The same boolean also drives the discussions
list and about page — each page is responsible for its own minimalist
presentation.

Within the discussion view, what appears depends on where the user is in
their conversation:

| Conversation context | User sees |
|---|---|
| No messages yet | Centered prompt: *"Ask a question about your data."* + input box |
| Question sent, awaiting | Subtle pulsing cursor in the answer area (no "Processing…" text) |
| Answer streaming in | Characters appear one by one; tool-call cards shown minimally inline |
| Answer complete | Full result: table/chart/summary + optional "Show SQL" toggle |
| Follow-up question | Thread scrolls up, new answer appears below |
| Error | Brief error message + retry affordance |

**Discussions list (triggered by `/history` or ◷ icon):**

A centered overlay or inline panel with a simple list of conversation titles (most recent first):
- Each title is a clickable/tappable row — tap to load that conversation
- "+ New discussion" row at the top
- The list dismisses immediately on selection
- No dates, tags, provider names, data source names, pin indicators, or metadata of any kind
- No search bar, no filters, no archive toggle — this is a picker, not a management tool

**What it does NOT render (in any state):**
- No sidebar
- No conversation selector
- No settings panel
- No gear popover
- No export dropdown
- No message timestamps
- No provider/data-source labels on messages

### Phase 3: Contextual Commands (`/` shortcuts)

The input box supports a set of slash commands that are invisible until
typed:

| Command | Behavior |
|---|---|
| `/new` | Start a fresh conversation (archives current, creates new) |
| `/source` | Show inline data source picker (compact dropdown) |
| `/model` | Show inline LLM provider picker |
| `/history` | Show recent conversation list as a clean, compact overlay — titles only, tap to switch (see §1.5) |
| `/exit` | Exit minimalist mode |
| `/help` | Show available commands |

These are rendered inline, not in a separate panel. They appear as a small
popover anchored to the input, and disappear when a selection is made or the
user presses Escape.

### Phase 4: Auto-Defaults and Resume

**In `App.svelte` script (not markup):**
- On startup, if minimalist is enabled, auto-select the last-used
  conversation (or create one if none exists) with sensible defaults for LLM
  and data source.
- Load the last conversation's messages so the minimalist view has content
  immediately.

**Persistence:**
- `last_minimalist_conversation_id` in `app_settings` — updated each time a
  message is sent
- `last_minimalist_llm_provider_id` / `last_minimalist_data_source_id` —
  updated when the user switches via `/source` or `/model`

### Phase 5: Visual Polish

- **Transitions:** Crossfade between full mode and minimalist mode — a brief
  (300ms) dissolve, not a slide or layout shift.
- **Typography:** Use the application's defined font (Nunito) at a
  comfortable size. Consider a slightly larger base size for readability.
- **Input styling:** A thin underline that thickens on focus. No background
  fill. No border box.
- **Message spacing:** Generous whitespace between messages. No bubbles.
  Only color/thickness differentiates user from assistant.
- **Table rendering:** Clean, borderless tables with subtle zebra striping.
  No "Results" label wrapping them. The data speaks for itself.
- **Persistent toggle:** Full mode — a prominent "Minimal Mode" button in
  the sidebar footer, replacing the "+ New Discussion" area. Accent-colored
  text on accent-light background. Minimalist mode — thin pill anchored
  top-right, opacity 0.2 at rest, 0.7 on hover. Never overlaps the send
  button.
- **Full design spec and copy-paste CSS:** See §11. Nothing in this phase
  should require inventing new colors, spacing, or type sizes — §11.7
  provides the complete, ready-to-use CSS.

### Phase 6: Edge Cases

- **No data sources configured:** Show a one-time setup prompt (inline, not
  a modal) with a link to Settings.
- **No LLM providers configured:** Same — one-time prompt.
- **Query fails:** Show the error inline, with a "Retry" affordance. The
  user never sees raw stack traces.
- **No internet:** Graceful offline state — "Cannot reach your LLM provider.
  Check your connection."
- **Long queries (>5 seconds):** Show a subtle progress indicator (thin bar
  or pulsing line, not a spinner or text).
- **Empty results:** "No results found. Try rephrasing your question."

---

## 6. What DOES NOT Change

| Concern | Status |
|---|---|
| **Pipeline** (`discussion_engine.go`, `agentic_loop.go`) | Unchanged |
| **Safety** (`safety.go`, `sql_execution.go`) | Unchanged |
| **LLM providers** | Unchanged |
| **Database drivers** | Unchanged |
| **Schema introspection** | Unchanged |
| **Wails bindings** (`app.go`) | Minimal additions (a few new bindings for auto-defaults) |
| **`App.svelte` routing** | One new `if` branch; existing code untouched |
| **Settings page** | Largely unchanged — no minimalist overhaul. Only the toggle checkbox is added (General tab). |
| **Full conversation experience** | Completely unchanged |
| **Read-only invariant** | Unchanged and absolute |

---

## 7. Files to Create

| File | Purpose |
|---|---|
| `frontend/src/MinimalistView.svelte` | The entire minimalist surface — all states, commands, rendering |
| `frontend/src/minimalist.css` | Shared, global minimalist component classes (see §11.7) — imported once in `main.js`, used by `MinimalistView.svelte`, the discussions overlay, and the About page's minimalist treatment |
| `documentation/MINIMALIST_MODE_V2.md` | This document |

## 8. Files to Modify

| File | Change |
|---|---|
| `frontend/src/App.svelte` | Add `if (minimalist)` branch that renders `<MinimalistView>`, passing `initialView` from current `activeView`; add startup auto-default logic; add keyboard shortcut; add prominent "Minimal Mode" button in sidebar footer (full mode only) |
| `frontend/src/main.js` | Add `setMinimalistMode` global function (parallel to `setThemeSelection`); init `data-minimalist` attribute on startup |
| `frontend/src/variables.css` | Add `html[data-minimalist="true"]` design tokens (see §11.7a) — additive only, never overrides existing `--space-*`/`--font-*`/`--color-*` tokens |
| `frontend/src/main.js` (CSS import) | Add `import './minimalist.css'` alongside the existing `import './variables.css'` |
| `frontend/src/SettingsView.svelte` | Add minimalist mode toggle in General tab |
| `pkg/services/app_settings.go` | Optionally add `GetMinimalistMode()` / `SetMinimalistMode()` wrappers |
| `app.go` | Optionally expose convenience wrappers; add `GetLastMinimalistConversation()` etc. for auto-resume |

---

## 9. Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| **User can't find settings / data source controls** | Medium | Slash commands (`/source`, `/model`) provide inline access; persistent toggle + keyboard shortcut exit minimalist mode |
| **Auto-defaults pick the wrong LLM or DB** | Medium | Auto-select follows user's defaults from `discussion_defaults`; user can change inline at any time |
| **New MinimalistView component diverges from ConversationView** | Medium | They share the same Wails bindings and message model but render independently — test both paths |
| **Long conversations feel cluttered without metadata** | Low | Metadata is available on hover/right-click; the user decides when they need it |
| **No regression on existing full app** | High | `MinimalistView` is a completely separate component; the existing layout path is untouched. Only one new `if` branch in `App.svelte` |
| **User quits during streaming, resumes to broken state** | Low | The pipeline handles cancellation and error recovery; the app resumes to the last complete state |

---

## 10. Testing Checklist

- [ ] Enter minimalist mode from a conversation → minimalist discussion surface
- [ ] Enter minimalist mode from the Discussions list page → minimalist discussions list (does NOT jump to a conversation)
- [ ] Enter minimalist mode from the About page → minimalist About page
- [ ] Enter minimalist mode from Settings → slightly simplified Settings (same sections, tighter spacing)
- [ ] Keyboard shortcut (`Cmd+Shift+M` / `Ctrl+Shift+M`) toggles both ways
- [ ] Persistent toggle in full mode: visible "Minimal Mode" button in sidebar footer (replaces +New Discussion area)
- [ ] Persistent toggle in minimalist mode: thin pill top-right, opacity 0.2 → 0.7 on hover
- [ ] Toggle in minimalist mode never overlaps the send button or input area
- [ ] Clicking the persistent toggle enters minimalist mode
- [ ] Clicking the persistent toggle exits minimalist mode
- [ ] Toggle hover behavior in both modes (opacity increase, label reveal)
- [ ] Exit via keyboard shortcut, `/exit` command, persistent toggle, and hamburger menu → Exit minimal option
- [ ] Hamburger menu visible top-left in minimalist mode (opacity 0.25 → 0.7 on hover)
- [ ] Hamburger dropdown shows Discussions, Settings, About, divider, Exit minimal
- [ ] Selecting Discussions from hamburger shows minimalist discussions list
- [ ] Selecting Settings from hamburger shows simplified Settings
- [ ] Selecting About from hamburger shows minimalist About page
- [ ] Empty state renders centered prompt + input
- [ ] Send a question → answer streams in character by character
- [ ] Results render inline (tables, text, charts) with no chrome wrapping
- [ ] `/new` starts a fresh conversation
- [ ] `/source` shows inline data source picker
- [ ] `/model` shows inline LLM provider picker
- [ ] `/history` shows recent conversations — titles only, no metadata
- [ ] Discussions list has "+ New discussion" at top
- [ ] Tapping a discussion title loads it immediately, list dismisses
- [ ] Tapping "+ New" starts a fresh conversation with current defaults
- [ ] Discussions list has no dates, tags, provider names, or filters
- [ ] About page simplified in minimalist mode (clean typography, no update section)
- [ ] Settings page largely unchanged — only the minimalist toggle is added
- [ ] Auto-resume: close and reopen → same conversation, same messages
- [ ] Auto-defaults: no provider or source configured → works with available ones
- [ ] One-time setup prompt when nothing is configured
- [ ] Error state: inline error message + retry
- [ ] Light theme in minimalist mode (discussion, discussions list, about, settings)
- [ ] Dark theme in minimalist mode (discussion, discussions list, about, settings)
- [ ] Exit restores full app with same page selected (not always conversation)
- [ ] No regression on existing full app path
- [ ] All minimalist screens respect the user's chosen theme (light/dark) automatically, with zero minimalist-specific color overrides
- [ ] All minimalist screens respect the user's chosen accent color automatically
- [ ] All minimalist screens respect the user's chosen UI scale (small/medium/large) automatically
- [ ] `prefers-reduced-motion` disables crossfades, cursor blink, and pulsing indicators

---

## 11. UI Design Specification (For Front-End Implementation)

> This section is the design handoff. It exists so a front-end developer can
> implement Phases 1–6 without making a single visual judgment call. Every
> measurement, color, and transition is specified. Where possible, the CSS
> is copy-paste ready.

### 11.1 Design Principles (Recap, for the CSS author)

1. **No new colors.** Every color in this spec is one of the existing
   `variables.css` tokens (`--bg-*`, `--text-*`, `--border-*`,
   `--color-accent*`, `--color-danger`, etc.). This means minimalist mode
   automatically inherits the user's **theme** (light/dark) and **accent
   color** with zero additional logic — the same variable resolves to a
   different value depending on `[data-theme]`, and minimalist CSS never
   needs to know which.
2. **No new absolute font sizes.** Every type size is expressed in `rem`,
   which is relative to the root `font-size` set by `html[data-ui-scale]`
   (13.6px / 16px / 19.2px). Small/Medium/Large scale works automatically —
   nothing in minimalist mode needs scale-specific overrides.
3. **Generous, not dense.** The app's existing `--space-*` scale (2px–40px)
   was tuned for a dense, settings-heavy UI. Minimalist mode needs more air.
   A new, additive `--mm-*` spacing scale is introduced for this purpose —
   it does not replace or conflict with `--space-*`.
4. **Motion is quiet.** Short, low-amplitude transitions only. No bounces,
   no slides, no attention-grabbing entrances. `prefers-reduced-motion`
   disables all non-essential motion.

### 11.2 Layout & Composition

- **Root container:** Full viewport height and width. A single centered
  column, `max-width: var(--mm-content-width)` (40rem / ~640px at medium
  scale), horizontally centered with fluid side padding
  (`clamp(1.5rem, 6vw, 4rem)`).
- **Wide content (tables):** When a result table is wider than the reading
  column, it may expand up to `var(--mm-content-width-wide)` (48rem) before
  it scrolls horizontally within its own row — the reading column for text
  never widens.
- **Vertical structure (top to bottom):**
  1. Scrollable thread area (flex: 1, `overflow-y: auto`, scrollbar hidden
     until hover/scroll)
  2. Fixed input bar pinned to the bottom, same max-width as the column
- **Minimum window size:** Must remain legible down to the app's documented
  minimum Wails window size (1024×1152 per `MINIMALIST_MODE.md` §6). Below
  the content max-width, side padding shrinks via the `clamp()` above
  rather than the column truncating awkwardly.
- **No horizontal scrolling** of the page itself, ever — only individual
  wide tables may scroll within their own bounds.

### 11.3 Typography

| Element | Size | Weight | Line-height | Color token |
|---|---|---|---|---|
| Empty-state headline ("Ask a question…") | `var(--mm-font-question)` (1.0625rem) | 500 | 1.4 | `var(--text-secondary)` |
| User question (in thread) | `var(--mm-font-question)` | 600 | `var(--mm-line-height)` | `var(--text-primary)` |
| Assistant answer body | `var(--mm-font-answer)` (1rem) | 400 | `var(--mm-line-height)` | `var(--text-primary)` |
| Assistant summary / prose | `var(--mm-font-answer)` | 400 | `var(--mm-line-height)` | `var(--text-primary)` |
| SQL block (revealed) | `var(--font-sm)` (existing token, monospace) | 400 | 1.5 | `var(--text-secondary)` |
| Table header | `var(--mm-font-meta)` (0.75rem), uppercase, letter-spacing 0.04em | 600 | 1.3 | `var(--text-tertiary)` |
| Table cell | `var(--mm-font-answer)` | 400 | 1.4 | `var(--text-primary)` |
| Metadata (hover-revealed) | `var(--mm-font-meta)` | 400 | 1.3 | `var(--text-tertiary)` |
| Discussions list row title | `var(--mm-font-question)` | 500 | 1.4 | `var(--text-primary)` |
| Persistent toggle label | `var(--font-2xs)` (existing token) | 500 | 1 | `var(--text-tertiary)` |
| Hamburger / nav items | `var(--mm-font-meta)` | 500 | 1.4 | `var(--text-primary)` |

**Font family:** Reuse the app's existing font stack (Nunito, then system
sans). No new font is introduced — consistency with the full app matters
more than a bespoke minimalist typeface.

### 11.4 Color Usage Map

Every color below is an *existing* token. This table exists purely so a
front-end developer never has to pick a color — only map an element to a
token.

| Design element | Token | Behavior across themes |
|---|---|---|
| Page background | `var(--bg-primary)` | Automatically light/dark via `[data-theme]` |
| User question text | `var(--text-primary)` | Automatic |
| Assistant answer text | `var(--text-primary)` | Automatic |
| Muted / meta text | `var(--text-tertiary)` | Automatic |
| Input bar underline (rest) | `var(--border-secondary)` | Automatic |
| Input bar underline (focus) | `var(--color-accent)` | Automatic — follows user's chosen accent |
| Streaming cursor | `var(--color-accent)` | Automatic |
| Table header text | `var(--text-tertiary)` | Automatic |
| Table row divider | `var(--border-secondary)` | Automatic |
| Table zebra stripe | `var(--bg-tertiary)` at 50% via `color-mix` fallback to `var(--bg-secondary)` | Automatic |
| Link / interactive text | `var(--color-accent)` | Automatic |
| Error text | `var(--color-danger)` | Automatic |
| Persistent toggle (minimalist, rest) | `var(--text-tertiary)` at opacity 0.2 | Automatic |
| Persistent toggle (minimalist, hover) | `var(--text-secondary)` at opacity 0.7 | Automatic |
| Full-mode toggle button | `var(--color-accent)` text on `var(--color-accent-light)` bg | Automatic |
| Hamburger icon | `var(--text-tertiary)` at opacity 0.25 | Automatic |
| Nav dropdown bg | `var(--bg-primary)` | Automatic |
| Nav row hover | `var(--color-accent-light)` | Automatic |
| SQL block background | `var(--bg-secondary)` | Automatic |
| SQL block text | `var(--text-secondary)` | Automatic |

### 11.5 Component Specs

**Empty / ready state**
- Vertically and horizontally centered within the thread area.
- Headline: *"Ask a question about your data."* per §11.3.
- Input bar directly below, `var(--mm-gap-lg)` (2rem) gap.

**Input bar**
- Single-line, auto-growing textarea, no visible box — a 1px underline
  (`var(--border-secondary)`) that thickens to 2px and changes to
  `var(--color-accent)` on focus.
- No background fill, no border-radius box, no placeholder icon.
- Placeholder text: *"Ask a question…"* in `var(--text-tertiary)`.
- Send affordance: pressing Enter submits (Shift+Enter for newline) —
  matches existing `ConversationView` behavior. No visible send button
  unless the input has focus and content, in which case a minimal arrow
  glyph fades in at `var(--mm-opacity-hover)`.

**Message thread**
- User and assistant messages are visually differentiated by **weight and
  alignment only** — never by a colored bubble or background fill.
- User message: right-aligned or indented, font-weight 600.
- Assistant message: left-aligned, full width of the reading column,
  font-weight 400.
- Gap between messages: `var(--mm-gap-xl)` (3rem) — generous enough that
  each exchange feels like its own moment.
- Metadata (timestamp, provider, data source) is `display: none` by
  default and revealed only via a hover-triggered fade
  (`opacity: 0 → var(--mm-opacity-hover)`, `var(--mm-transition-fast)`) on
  the message row.

**Streaming cursor**
- A thin vertical bar (`0.5em` tall, 2px wide, `var(--color-accent)`)
  blinking at `1s` interval (`opacity: 1 → 0.2`), positioned immediately
  after the last streamed character.
- Disabled under `prefers-reduced-motion` — shows as a static, non-blinking
  bar instead.

**Result tables**
- No card wrapper, no "Results" label, no border box around the whole
  table.
- Header row: uppercase, small, muted, bottom border `1px solid
  var(--border-secondary)`.
- Body rows: no vertical borders; a `1px solid var(--border-secondary)`
  bottom rule per row. Optional zebra striping on alternating rows using
  `var(--bg-tertiary)`.
- Numeric columns right-aligned with `font-variant-numeric: tabular-nums`.
- No visible "export" or "expand" controls by default — revealed on
  right-click or long-press per §2.3.

**SQL toggle (revealed state)**
- Collapsed by default: a small, muted "Show SQL" text link
  (`var(--mm-font-meta)`, `var(--text-tertiary)`) beneath the answer.
- Expanded: a `var(--bg-secondary)` block, monospace, `var(--radius-sm)`
  corners, `var(--mm-gap-sm)` padding.

**Persistent toggle**
- **Full mode:** Lives in the sidebar footer — replaces the "+ New
  Discussion" button. Styled as a visible, intentional button: text
  "Minimal Mode" + icon, `var(--color-accent)` on
  `var(--color-accent-light)` background, `var(--radius-md)` rounded,
  full width of the sidebar (minus padding). This is not subtle — it is
  meant to be discovered.
- **Minimalist mode:** Thin pill anchored `position: fixed` to the
  top-right corner, `var(--mm-gap-md)` from the top and right edges.
  Rest: `opacity: 0.2`, `var(--text-tertiary)`, no background, no
  border. Hover: `opacity: 0.7`, `var(--text-secondary)`. Transition:
  `var(--mm-transition-fast)` on opacity only.
- **Click behavior:** single click toggles the mode immediately. No
  confirmation.

**Hamburger menu (navigation)**
- `position: fixed`, top-left corner, `var(--mm-gap-md)` from edges.
- Circular button, 24px, `opacity: 0.25` at rest, `0.7` on hover.
  Icon: a simple ≡ or ☰ glyph.
- On click: reveals a compact dropdown (similar to the slash-command
  popover but anchored top-left). Items: Discussions, Settings, About,
  divider, Exit minimal. Each row styled as the slash-command rows.
  Dismisses on selection, outside click, or Escape.

**Discussions overlay**
- Full-bleed overlay, `var(--bg-primary)` at 96% opacity (a light scrim,
  not a modal box), centered column matching `var(--mm-content-width)`.
- Each row: title only, `var(--mm-gap-sm)` vertical padding, bottom rule
  `var(--border-secondary)`.
- "+ New discussion" row: same styling as other rows but prefixed with a
  `+` glyph in `var(--color-accent)`.
- Dismiss: click outside the column, press Escape, or select a row.

**About page (minimalist)**
- Single centered column, `var(--mm-content-width)`.
- App name + one-sentence description only. No update-checker UI, no
  feature list, no technology list, no license section.
- A single small link/affordance to "View full details" that exits
  minimalist mode and opens the full About page.

**Slash-command popover**
- Anchored directly above the input bar, `var(--mm-gap-xs)` gap.
- `var(--bg-primary)` background, `var(--border-secondary)` 1px border,
  `var(--mm-radius)` corners, subtle `var(--shadow)` drop shadow.
- Each command row: `var(--mm-font-meta)` command name in
  `var(--color-accent)`, description in `var(--text-tertiary)`.

**One-time setup prompt**
- Same visual weight as the empty state, but with a single sentence and a
  minimal accent-colored link to Settings — no modal, no dismiss button
  (it disappears permanently once a data source exists).

**Error state**
- Inline, in place of where the answer would have appeared.
- `var(--color-danger)` text, `var(--mm-font-answer)` size, one sentence.
- A muted "Retry" text link (`var(--text-tertiary)`, underline on hover)
  immediately below.

**Long-query progress indicator**
- A 2px-tall horizontal line at the very top of the thread area,
  `var(--color-accent)` at 40% width, animating left-to-right on a 1.2s
  loop. Not a spinner, not a percentage, not text.

### 11.6 Motion & Transitions

| Interaction | Duration | Easing | Property |
|---|---|---|---|
| Mode crossfade (full ↔ minimalist) | 300ms | ease | opacity |
| Message entrance | 180ms | ease-out | opacity, translateY(4px→0) |
| Input focus underline | 120ms | ease | border-color, border-width |
| Toggle hover | 120ms | ease | opacity |
| Streaming cursor blink | 1000ms | linear, infinite | opacity |
| Discussions overlay enter/exit | 220ms | ease | opacity |
| Progress indicator sweep | 1200ms | linear, infinite | transform: translateX |

All durations are defined as `--mm-transition-*` tokens (§11.7a) so they
can be tuned in one place.

**Reduced motion:** Wrap all non-essential motion (crossfades, cursor
blink, progress sweep, message entrance) in
`@media (prefers-reduced-motion: no-preference)`. Under
`prefers-reduced-motion: reduce`, all of the above render in their final
state immediately, with no animation.

### 11.7 The CSS

#### 11.7a — Design tokens (add to `frontend/src/variables.css`)

These are **additive**. They never override the existing `--space-*`,
`--font-*`, or `--color-*` tokens — they introduce a parallel, more generous
scale for minimalist mode specifically, and every value is expressed so
that theme, accent, and UI-scale changes apply automatically with zero
extra logic.

```css
/* ===== Minimalist Mode — Design Tokens ===== */
/* Additive only. Never redefines --space-*, --font-*, or --color-*.
   Dark mode and accent color are inherited automatically because every
   component rule in minimalist.css references existing color tokens,
   never a hard-coded value. UI scale is inherited automatically because
   every size below is in rem, relative to the root font-size set by
   html[data-ui-scale]. */
html[data-minimalist="true"] {
  /* Layout */
  --mm-content-width:      40rem;   /* ~640px @ medium scale */
  --mm-content-width-wide: 48rem;   /* wide tables only */
  --mm-viewport-padding:   clamp(1.5rem, 6vw, 4rem);

  /* Generous vertical rhythm (do not reuse --space-* here) */
  --mm-gap-xs:  0.5rem;
  --mm-gap-sm:  0.75rem;
  --mm-gap-md:  1.25rem;
  --mm-gap-lg:  2rem;
  --mm-gap-xl:  3rem;
  --mm-gap-2xl: 4.5rem;

  /* Typography */
  --mm-font-question: 1.0625rem;
  --mm-font-answer:   1rem;
  --mm-font-meta:     0.75rem;
  --mm-line-height:   1.65;

  /* Motion */
  --mm-transition-fast: 120ms ease;
  --mm-transition-med:  220ms ease;
  --mm-transition-slow: 320ms ease;
  --mm-crossfade:       300ms ease;

  /* Opacity levels for "invisible until summoned" elements */
  --mm-opacity-rest:   0.38;
  --mm-opacity-hover:  0.85;
  --mm-opacity-active: 1;

  /* Radii */
  --mm-radius: 0.5rem;

  /* Legacy full-app chrome, zeroed defensively in case any full-app
     component is still mounted underneath (belt-and-suspenders — the
     routing change in App.svelte should make this unreachable) */
  --sidebar-width: 0;
}
```

#### 11.7b — Component classes (new file: `frontend/src/minimalist.css`)

Imported once, globally, in `main.js`:

```js
import './variables.css'
import './minimalist.css'
```

Using a standalone global stylesheet (rather than per-component `<style>`
blocks) means `MinimalistView.svelte`, the discussions overlay, and the
About page's minimalist branch can all use the same class names without
fighting Svelte's style scoping.

```css
/* ============================================================
   Minimalist Mode — Shared Component Styles
   Imported globally. Active only within html[data-minimalist="true"].
   Every color below is an existing variables.css token — see
   MINIMALIST_MODE_V2.md §11.4 for the full color usage map.
   ============================================================ */

/* ── Shell ─────────────────────────────────────────────────── */
.mm-shell {
  display: flex;
  flex-direction: column;
  height: 100vh;
  width: 100%;
  background: var(--bg-primary);
}

.mm-scroll {
  flex: 1;
  overflow-y: auto;
  scrollbar-width: none; /* Firefox: hidden until hover via JS toggle if desired */
}
.mm-scroll::-webkit-scrollbar { width: 0; height: 0; }
.mm-scroll:hover::-webkit-scrollbar { width: 6px; }
.mm-scroll::-webkit-scrollbar-thumb {
  background: var(--border-primary);
  border-radius: var(--mm-radius);
}

.mm-column {
  max-width: var(--mm-content-width);
  margin: 0 auto;
  padding: 0 var(--mm-viewport-padding);
  width: 100%;
  box-sizing: border-box;
}

.mm-column--wide { max-width: var(--mm-content-width-wide); }

/* ── Empty / ready state ──────────────────────────────────── */
.mm-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--mm-gap-lg);
  text-align: center;
  padding: var(--mm-viewport-padding);
}
.mm-empty h1 {
  margin: 0;
  font-size: var(--mm-font-question);
  font-weight: 500;
  color: var(--text-secondary);
}

/* ── Input bar ─────────────────────────────────────────────── */
.mm-inputbar {
  padding: var(--mm-gap-md) var(--mm-viewport-padding) var(--mm-gap-lg);
  display: flex;
  justify-content: center;
}
.mm-input-row {
  width: 100%;
  max-width: var(--mm-content-width);
  display: flex;
  align-items: flex-end;
  gap: var(--mm-gap-sm);
  border-bottom: 1px solid var(--border-secondary);
  transition: border-color var(--mm-transition-fast), border-width var(--mm-transition-fast);
}
.mm-input-row:focus-within {
  border-bottom: 2px solid var(--color-accent);
}
.mm-input {
  flex: 1;
  border: none;
  background: transparent;
  resize: none;
  outline: none;
  font: inherit;
  font-size: var(--mm-font-answer);
  color: var(--text-primary);
  line-height: var(--mm-line-height);
  padding: var(--mm-gap-sm) 0;
  max-height: 40vh;
}
.mm-input::placeholder { color: var(--text-tertiary); }
.mm-send {
  opacity: 0;
  color: var(--color-accent);
  background: none;
  border: none;
  cursor: pointer;
  padding: var(--mm-gap-xs);
  transition: opacity var(--mm-transition-fast);
}
.mm-input-row:focus-within .mm-send,
.mm-send--visible { opacity: var(--mm-opacity-hover); }
.mm-send:hover { opacity: var(--mm-opacity-active); }

/* ── Message thread ───────────────────────────────────────── */
.mm-thread {
  display: flex;
  flex-direction: column;
  gap: var(--mm-gap-xl);
  padding: var(--mm-gap-2xl) 0 var(--mm-gap-lg);
}
.mm-msg {
  display: flex;
  flex-direction: column;
  gap: var(--mm-gap-xs);
  animation: mm-enter var(--mm-transition-med, 220ms) ease-out;
}
.mm-msg--user {
  align-self: flex-end;
  align-items: flex-end;
  max-width: 85%;
}
.mm-msg--user .mm-msg__body {
  font-weight: 600;
  font-size: var(--mm-font-question);
  color: var(--text-primary);
  text-align: right;
}
.mm-msg--assistant .mm-msg__body {
  font-weight: 400;
  font-size: var(--mm-font-answer);
  line-height: var(--mm-line-height);
  color: var(--text-primary);
}
.mm-msg__meta {
  font-size: var(--mm-font-meta);
  color: var(--text-tertiary);
  opacity: 0;
  transition: opacity var(--mm-transition-fast);
}
.mm-msg:hover .mm-msg__meta,
.mm-msg:focus-within .mm-msg__meta { opacity: var(--mm-opacity-hover); }

/* ── Streaming cursor ──────────────────────────────────────── */
.mm-cursor {
  display: inline-block;
  width: 2px;
  height: 0.9em;
  background: var(--color-accent);
  vertical-align: text-bottom;
  margin-left: 2px;
  animation: mm-blink 1s step-end infinite;
}
@media (prefers-reduced-motion: reduce) {
  .mm-cursor { animation: none; opacity: 1; }
}

/* ── Tables ────────────────────────────────────────────────── */
.mm-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--mm-font-answer);
  margin: var(--mm-gap-sm) 0;
}
.mm-table th {
  text-align: left;
  font-size: var(--mm-font-meta);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-weight: 600;
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border-secondary);
  padding: var(--mm-gap-xs) var(--mm-gap-sm);
}
.mm-table td {
  padding: var(--mm-gap-xs) var(--mm-gap-sm);
  border-bottom: 1px solid var(--border-secondary);
  color: var(--text-primary);
}
.mm-table tr:nth-child(even) td { background: var(--bg-tertiary); }
.mm-table td.mm-num { text-align: right; font-variant-numeric: tabular-nums; }

/* ── SQL toggle ────────────────────────────────────────────── */
.mm-sql-toggle {
  font-size: var(--mm-font-meta);
  color: var(--text-tertiary);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  text-decoration: underline;
  text-decoration-color: transparent;
  transition: text-decoration-color var(--mm-transition-fast), color var(--mm-transition-fast);
}
.mm-sql-toggle:hover {
  color: var(--text-secondary);
  text-decoration-color: var(--text-secondary);
}
.mm-sql-block {
  background: var(--bg-secondary);
  border-radius: var(--radius-sm);
  padding: var(--mm-gap-sm);
  margin-top: var(--mm-gap-xs);
  font-family: 'Courier New', monospace;
  font-size: var(--font-sm);
  color: var(--text-secondary);
  overflow-x: auto;
  white-space: pre-wrap;
}

/* ── Persistent toggle (minimalist mode — full-mode toggle lives in sidebar) */
.mm-toggle {
  position: fixed;
  top: var(--mm-gap-md);
  right: var(--mm-gap-md);
  z-index: 999;
  padding: var(--mm-gap-xs) var(--mm-gap-sm);
  border-radius: var(--mm-radius);
  background: transparent;
  border: none;
  color: var(--text-tertiary);
  font-size: var(--font-2xs);
  cursor: pointer;
  opacity: 0.2;
  transition: opacity var(--mm-transition-fast, 120ms ease), color var(--mm-transition-fast, 120ms ease);
}
.mm-toggle:hover { opacity: 0.7; color: var(--text-secondary); }

/* ── Hamburger menu (navigation) ──────────────────────────── */
.mm-hamburger {
  position: fixed;
  top: var(--mm-gap-md);
  left: var(--mm-gap-md);
  z-index: 998;
  width: 24px;
  height: 24px;
  background: transparent;
  border: none;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0.25;
  color: var(--text-tertiary);
  font-size: var(--font-lg);
  transition: opacity var(--mm-transition-fast, 120ms ease), color var(--mm-transition-fast, 120ms ease);
}
.mm-hamburger:hover { opacity: 0.7; color: var(--text-secondary); }

/* ── Navigation dropdown (hamburger popover) ──────────────── */
.mm-nav-popover {
  position: fixed;
  top: calc(var(--mm-gap-md) + 32px);
  left: var(--mm-gap-md);
  background: var(--bg-primary);
  border: 1px solid var(--border-secondary);
  border-radius: var(--mm-radius);
  box-shadow: 0 4px 16px var(--shadow);
  padding: var(--mm-gap-xs);
  min-width: 11rem;
  z-index: 999;
  animation: mm-fade 120ms ease;
}
.mm-nav-row {
  display: flex;
  gap: var(--mm-gap-sm);
  padding: var(--mm-gap-xs) var(--mm-gap-sm);
  border-radius: var(--radius-sm);
  font-size: var(--mm-font-meta);
  color: var(--text-primary);
  cursor: pointer;
  background: none;
  border: none;
  width: 100%;
  text-align: left;
}
.mm-nav-row:hover { background: var(--color-accent-light); }
.mm-nav-row--danger { color: var(--color-danger); }
.mm-nav-divider {
  height: 1px;
  background: var(--border-secondary);
  margin: var(--mm-gap-xs) 0;
}

/* ── Discussions overlay ───────────────────────────────────── */
.mm-discussions-overlay {
  position: fixed;
  inset: 0;
  background: color-mix(in srgb, var(--bg-primary) 96%, transparent);
  z-index: 900;
  display: flex;
  justify-content: center;
  overflow-y: auto;
  animation: mm-fade var(--mm-transition-med) ease;
}
.mm-discussions-list {
  width: 100%;
  max-width: var(--mm-content-width);
  padding: var(--mm-gap-2xl) var(--mm-viewport-padding);
}
.mm-discussion-row {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  padding: var(--mm-gap-sm) 0;
  border-bottom: 1px solid var(--border-secondary);
  font-size: var(--mm-font-question);
  font-weight: 500;
  color: var(--text-primary);
  cursor: pointer;
  transition: color var(--mm-transition-fast);
}
.mm-discussion-row:hover { color: var(--color-accent); }
.mm-discussion-new { color: var(--color-accent); font-weight: 600; }

/* ── About page (minimalist) ──────────────────────────────── */
.mm-about {
  max-width: var(--mm-content-width);
  margin: 0 auto;
  padding: var(--mm-gap-2xl) var(--mm-viewport-padding);
  text-align: center;
}
.mm-about p { color: var(--text-secondary); line-height: var(--mm-line-height); }
.mm-about a { color: var(--color-accent); font-size: var(--mm-font-meta); }

/* ── Slash-command popover ────────────────────────────────── */
.mm-command-popover {
  position: absolute;
  bottom: 100%;
  left: 0;
  right: 0;
  margin-bottom: var(--mm-gap-xs);
  background: var(--bg-primary);
  border: 1px solid var(--border-secondary);
  border-radius: var(--mm-radius);
  box-shadow: 0 4px 16px var(--shadow);
  padding: var(--mm-gap-xs);
  animation: mm-fade var(--mm-transition-fast) ease;
}
.mm-command-item {
  display: flex;
  gap: var(--mm-gap-sm);
  padding: var(--mm-gap-xs) var(--mm-gap-sm);
  border-radius: var(--radius-sm);
  cursor: pointer;
}
.mm-command-item:hover { background: var(--color-accent-light); }
.mm-command-item__name { color: var(--color-accent); font-size: var(--mm-font-meta); font-weight: 600; }
.mm-command-item__desc { color: var(--text-tertiary); font-size: var(--mm-font-meta); }

/* ── Setup prompt & errors ────────────────────────────────── */
.mm-setup-prompt, .mm-error {
  text-align: center;
  padding: var(--mm-gap-lg) var(--mm-viewport-padding);
  font-size: var(--mm-font-answer);
}
.mm-setup-prompt a { color: var(--color-accent); }
.mm-error { color: var(--color-danger); }
.mm-error__retry {
  display: block;
  margin-top: var(--mm-gap-xs);
  color: var(--text-tertiary);
  font-size: var(--mm-font-meta);
  text-decoration: underline;
  cursor: pointer;
}

/* ── Long-query progress indicator ────────────────────────── */
.mm-progress {
  position: absolute;
  top: 0;
  left: 0;
  height: 2px;
  width: 40%;
  background: var(--color-accent);
  animation: mm-sweep 1.2s linear infinite;
}

/* ── Animations ────────────────────────────────────────────── */
@keyframes mm-blink { 0%, 50% { opacity: 1; } 50.01%, 100% { opacity: 0.2; } }
@keyframes mm-fade { from { opacity: 0; } to { opacity: 1; } }
@keyframes mm-enter { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: translateY(0); } }
@keyframes mm-sweep { from { transform: translateX(-100%); } to { transform: translateX(250%); } }

@media (prefers-reduced-motion: reduce) {
  .mm-msg, .mm-discussions-overlay, .mm-command-popover { animation: none; }
  .mm-progress { animation: none; opacity: 0.5; }
}
```

### 11.8 Handoff Checklist for the Front-End Developer

- [ ] Import `minimalist.css` once, globally, in `main.js`
- [ ] Add the `--mm-*` tokens to `variables.css` under `html[data-minimalist="true"]`
- [ ] Never introduce a hard-coded color, font-size in `px`, or spacing value outside the `--mm-*` tokens
- [ ] Verify every component looks correct in light, dark, and all three UI scales without touching the component CSS
- [ ] Verify every component respects a custom accent color (test with at least 2 non-default presets from Settings → General → Accent Color)
- [ ] Verify `prefers-reduced-motion: reduce` disables all animation listed in §11.6

---

## 12. Remember

**The user didn't ask for a simpler version of the current app. They asked
for an app that gets out of their way.**

Minimalist mode V2 is not about removing things. It's about designing an
experience where the only thing the user notices is their question and the
answer. Every element, every transition, every default should be evaluated
against: *Does this make the app feel more transparent, or does it pull the
user's attention to the tool instead of the task?*

The full app is for power users — people who want to configure, tune, and
manage. Minimalist mode is for people who just want an answer. Both are
valid. Both are the same codebase. But they should feel like two different
products.

*Keep this document current. If your change alters anything described here,
update the relevant section in the same change.*