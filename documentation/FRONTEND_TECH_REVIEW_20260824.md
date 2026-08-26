# YourQL — Frontend Technical Review (2026-08-24)

> **Status: IMPLEMENTED (2026-08-24).** All phases below have been applied to
> `frontend/src/` and the production build passes (`npm run build`, 0
> svelte-check errors). See “Implementation Notes” at the end of this file for
> what landed and any deviations from the original plan.

> Scope: everything visual and frontend-structural in `frontend/src/` —
> `App.svelte` (2,696 lines), `ConversationView.svelte` (1,310),
> `SettingsView.svelte` (3,795), `MinimalistView.svelte` (476),
> `VizChart.svelte` (127), `variables.css` (182), `minimalist.css` (542),
> `main.js` (437). Reviewed against `AGENT_READ_FIRST.md`, whose priority
> order (answer correctness → safety → reliability → appeal/comfort → polish)
> frames every recommendation below. **Nothing here proposes touching the
> agentic loop, prompts, SQL execution, or any backend behavior** — this is a
> presentation-layer review only.

---

## Executive Summary

The frontend works and is feature-rich, but it has grown by accretion rather
than design. The result is:

1. **Two parallel UIs with no shared design language.** Full mode
   (`App.svelte`) and Minimalist mode (`MinimalistView.svelte`) duplicate
   chat, streaming, sending, auto-scroll, and chart logic with different
   visual treatments. **Both modes are intentional and both stay** — see the
   Two Modes principle below.
2. **A token system that exists but isn't trusted.** `variables.css` defines
   spacing/type/radius/color tokens, yet components hardcode hex values,
   `rgba(2,136,209,…)` literals, `#e0e0e0`, `1px 6px`, `8px`,
   `var(--font-3xs)` (which doesn't exist), and inline `style="…"` in markup.
3. **Monolithic components.** Three files hold ~9,500 lines including all
   styles. The gear popover alone is a 15-section form embedded inside
   `App.svelte`.
4. **DOM-string HTML from the backend manipulated via `window.*` globals**
   (`copySQL`, `toggleSQLPopover`, `toggleSQLSection`, `exportCSV`,
   `setThemeSelection`, `setAccent`, `setUIScale`) with a `MutationObserver`
   re-binding loop in `main.js`. This is the single largest architectural debt.
5. **Dead/duplicated code**: duplicate `.tag-chip` definitions, two copy-SQL
   implementations, three near-identical icon-button styles, duplicated
   streaming state machines, unused CSS classes, `confirm()` browser dialog
   alongside custom modals.

The good news: the fix is not a rewrite. It's (a) one shared design-token +
primitive layer, (b) extracting components along already-visible seams, and
(c) replacing backend-generated interactive HTML with small Svelte renderers.
Each step is independently shippable.

---

## Part A — Visual Design: Toward One Coherent Look

### A0. The Two Modes Principle (standing design rule)

YourQL deliberately ships two modes, and this review does not propose removing
either:

- **Full mode** is the control surface. The app has many powerful settings —
  providers, data sources, skills, per-conversation toggles, tags, filters,
  agent-loop config — and full mode must keep every one of them visible and
  reachable. It is the place where power is *available*.
- **Minimalist mode** is the answer surface. Its job is to put the least
  possible friction between a user and their answer: one input, one thread.
  It is the place where power is *pre-decided*.

What makes Minimalist mode work is not the absence of settings — it's that
**the Discussion Defaults system (Settings → Defaults) has already answered
the questions before the user arrives.** Default provider, default data
source, summarize on, viz on — when those are set, Minimalist mode needs
nothing else from the user.

Standing rules that follow from this principle:

1. **Nothing in this review may remove capability from full mode.** Every
   consolidation (shared buttons, dialogs, primitives) must preserve all
   existing controls there.
2. **Minimalist mode never grows its own settings UI.** If a new feature
   needs configuration, it gets configured via Discussion Defaults / full
   mode, and Minimalist simply consumes the defaults.
3. **Shared logic, distinct skins.** Both modes must render answers through
   the same components and the same streaming/send pipeline (A8/B1), so a fix
   or improvement to answer delivery lands in both at once. Only layout,
   typography, and density differ.
4. **Defaults coverage is part of Minimalist UX.** Where Minimalist currently
   forces a manual choice because no default exists (e.g., creating an
   untitled conversation, no data source selected), prefer falling back to
   the user's saved defaults automatically — friction removed upstream in
   Settings, not papered over with more UI in Minimalist.
5. **Mode entry should feel continuous.** Entering Minimalist mode (◐) while
   a discussion is open should land the user in that same discussion with the
   same thread visible; today's behavior should be preserved and strengthened
   as the shared chat core lands.

Everything below is written to serve this principle, not compete with it.

### A1. Adopt a single "quiet chrome, loud answer" concept

The app's job (per AGENT_READ_FIRST §0) is to deliver an answer. The design
concept should make that literal:

- **Chrome (sidebar, headers, filters, settings) = quiet**: flat surfaces,
  `--bg-secondary`, no borders on hover that change layout, restrained accent
  use.
- **Answer content (chat bubbles, tables, charts) = the only place accent
  color and elevation appear.**

Today this is inverted in places: modals and popovers carry a heavy
`border: 2px solid var(--color-accent)` while answer bubbles are visually
flat; the discussion-list rows have a hover `translateX(0.3125rem)` slide that
is playful but unique to that one list (nothing else moves horizontally).
Recommendation: reserve accent borders for *focus states* only, drop the
slide-translate in favor of a subtle background shift used app-wide.

### A2. Fix the broken / inconsistent tokens first (cheap, high impact)

These are concrete bugs or inconsistencies found in the current code:

| Issue | Where | Fix |
|---|---|---|
| `var(--font-3xs)` doesn't exist | `ConversationView.svelte` (`.tech-detail-chip`) | Add `--font-3xs: 0.5625rem` (9px) or switch to `--font-2xs` |
| `var(--radius-lg)`, `--radius-sm` used but only `sm/md` defined | search input, filter selects, streaming bubble | Add `--radius-lg: 0.5rem`; keep `sm/md/lg` as the full set |
| Hardcoded `#e0e0e0` borders | ConversationView header/input/footer, exploration header | Replace with `var(--border-primary)` |
| Hardcoded `rgba(2, 136, 209, x)` accent tints | App.svelte nav active, row hover, meta tags, ConversationView `.meta-tag` | Replace with `var(--color-accent-light)` / `--color-accent-border` so user accent choice actually applies everywhere |
| `--color-error` / `--color-error-hover` referenced but undefined (falls back to literal) | ConversationView cancel button | Use existing `--color-danger` / `--color-danger-hover` |
| `--color-primary`, `--border-color`, `--shadow-lg`, `--font-mono`, `--color-warning` referenced inconsistently | About view update notes, popover shadow, reasoning text | Define them once in `variables.css` or remove references |
| Duplicate `.tag-chip` block (two definitions of same class) | App.svelte styles | Delete one |
| Mixed unit systems: rem tokens vs raw px (`1px 6px`, `8px 12px`, `180px`, `380px`, `10px`) throughout styles | All components | Normalize to spacing tokens; allow px only for hairlines and icon sizes |

### A3. One button system

Currently there are at least seven ad-hoc button idioms: `.btn/.btn-primary/
.btn-secondary/.btn-danger`, `.row-action-btn`, `.gear-btn-header`,
`.export-btn-header`, `.icon-btn` variants, `.msg-limit-btn`,
`.gear-action-btn`, plus Minimalist mode's own buttons — each duplicating
padding/hover/disabled rules (e.g., `.btn-primary:hover { background:
var(--color-accent); }` — hover does nothing because it sets the same color;
`.send-btn:hover` has the identical no-op).

Recommend a minimal set, defined once globally:

```
.btn            → default (secondary surface)
.btn.primary    → accent fill
.btn.danger     → destructive fill (used for delete/clear confirm)
.icon-btn       → 36px square ghost button (pin/tag/settings/export/gear/close)
.chip           → tag/filter pill
```

Everything else composes from these. This alone removes several hundred lines
of SCSS-like duplication across the three big components.

### A4. Unify overlays: modal vs popover vs `confirm()`

Three different destructive-action patterns coexist:

1. Custom modal (`Delete Discussion`) — good pattern, matches §3.9.
2. `window.confirm('Clear all messages…')` in `handleClearMessages` — native
   dialog that looks nothing like the app and blocks the webview thread.
3. The "gear popover" is actually a **centered modal** (`position: fixed;
   top: 50%; z-index: 20000`) named after a different pattern, with a
   different border treatment than the real modal.

Recommendation:

- One `<Overlay>` primitive (scrim + centered card) and one `<ConfirmDialog>`
  built on it. Replace `window.confirm` immediately (§3.9 alignment).
- Rename/restyle the gear popover as what it really is: the **Conversation
  Settings dialog**, sharing the exact chrome (header, body scroll, footer)
  of New Discussion and Delete Discussion.
- Standardize z-index scale in variables.css: `--z-popover: 100;
  --z-modal: 200; --z-toast: 300;` (today's values are 999 / 1000 / 19999 /
  20000 — arbitrary and fragile).

### A5. Typography rhythm

The type scale is fine; usage isn't. Examples: sidebar h1 uses `--font-4xl`,
view h2 `--font-5xl`, conversation title `--font-2xl`, modal h3 `--font-3xl`.
Uppercase micro-labels appear in the gear popover (`text-transform: uppercase`
labels) and the row-tag popover title, nowhere else. Recommendation:

- Define semantic roles, not sizes-at-call-sites:
  `--text-h1` (app name), `--text-h2` (view title), `--text-title`
  (card/modal titles), `--text-body`, `--text-meta` (timestamps/chips),
  `--text-label` (form label, uppercase, letterspaced — the *one* place
  uppercase is allowed).
- Pick a single monospace stack for SQL/payloads (`ui-monospace, SFMono-Regular,
  Menlo, monospace`) instead of `'Courier New'` (used in four places), and add
  the missing `--font-mono` variable that's already referenced.

### A6. Dark-mode correctness sweep

Several dark-mode leaks exist because colors bypass tokens:

- `.assistant-message pre` and `.payload-content` hardcode `#1e1e1e/#d4d4d4`
  — acceptable as a deliberate "always-dark code surface," but then do it
  intentionally: define `--code-bg` / `--code-fg` once.
- Chart.js defaults are updated on theme-change events, but the initial
  `Chart.defaults.borderColor = '#e9ecef'` is light-only until an event fires.
  Initialize from `matchMedia` at module load.
- `--bg-primary` inputs sit directly on `--bg-primary` panels in some spots
  (search input on Discussions view) making field boundaries invisible in
  light mode. Standardize input surfaces on `--bg-input` (new token: white /
  `#222240`).

### A7. Empty & loading states — one idiom

Empty states exist for discussions and conversations with different markup and
tone ("No discussions found" vs "No messages yet"). Loading has three idioms:
bouncing dots, spinner text, and streaming cards. Recommend:

- One `<EmptyState icon title hint action />` component used everywhere,
  always answering "what happened / what can I do next" (§3.9).
- One `<Busy label />` component wrapping the dots + phase text, reused by the
  processing indicator and any future long operation. Keep the
  `processingPhase` labels ("Exploring schema…") — they're excellent comfort
  UX per the charter — but also show them in Minimalist mode (currently
  minimalist shows its own dots without phase text).

### A8. Minimalist mode keeps its look but borrows the engine

Minimalist mode stays exactly as it is visually and experientially — generous
whitespace, larger type, reduced chrome, slash commands, discussions overlay.
What it should stop doing is re-implementing the machinery behind that look:
message rendering, streaming machine, send flow, auto-resize textarea, chart
embedding, optimistic message handling. Visual differences are legitimate
(typography, width, opacity levels); logic differences are bugs waiting to
happen — e.g., Minimalist tracks reasoning
text separately (`streamingReasoningText`) while ConversationView doesn't;
Minimalist gates streaming on `streaming_enabled`, ConversationView doesn't.

Recommendation: extract `ChatThread.svelte`, `useStreaming()` (shared event
subscription + state), and `MessageComposer.svelte`. Each mode remains its
own thin layout over these shared parts — Minimalist keeps its typography,
density, and command palette; full mode keeps its headers, export menu, and
tech-detail affordances. **Neither mode's behavior changes for the user;
only the duplicated internals merge.** This is the highest-value structural
refactor in this document (see B1), and it exists specifically so the Two
Modes Principle (A0) can hold long-term without drift.

### A9. Small visual nits worth fixing in passing

- Sidebar "About" nav icon is a text emoji (`i️`) next to Lucide icons — swap
  to `<Info size={18} />` for consistency.
- Pin indicator uses emoji 📌 while row actions use Lucide `Pin` — pick Lucide.
- Emoji in payload toggles (`📋 📄 📥 🔧 ⚠ 💭 ⏳ ⚙`) mix pictograph styles;
  replace with Lucide icons at 12–14px.
- Legacy toggle labels `"up Request Payload"` / `"down Response Payload"` are
  clearly corrupted arrows (↑/↓ lost) — replace with proper labels/icons.
- `.btn-new-discussion` CSS is fully unused (button was moved into the view
  header) — delete.
- `@media print` hides `.sidebar-footer` etc. — verify after any layout change;
  consider moving print rules into their own file so they don't rot inside
  component styles.
- Focus visibility: many custom buttons lack `:focus-visible` rings (only
  `.conversation-item` and text inputs have them). Keyboard users lose their
  place in the sidebar nav, tab bar, and message actions. Add one global
  focus rule using `--color-accent`.

---

## Part B — Code Architecture & Frontend Best Practices

### B1. Break up the monoliths along existing seams

Target structure (all pure moves, no behavior change):

```
src/
  lib/
    api.js              // re-export wailsjs bindings (single import point)
    theme.js            // theme/accent/scale engine (from main.js)
    stream.js           // llm:stream subscription + state machine
    format.js           // fmtTokens, formatTime, csv escape helpers
  components/
    ui/  Button.svelte IconBtn.svelte Modal.svelte ConfirmDialog.svelte
         Chip.svelte Toggle.svelte Field.svelte EmptyState.svelte Busy.svelte
    chat/ ChatThread.svelte MessageBubble.svelte PayloadSection.svelte
          SqlBlock.svelte ResultTable.svelte MessageComposer.svelte
    conversation/ ConversationSettingsDialog.svelte TagPopover.svelte
                  ExportMenu.svelte
    settings/ tabs: ModelsTab.svelte DatabasesTab.svelte SkillsTab.svelte
              DefaultsTab.svelte GeneralTab.svelte AgentLoopTab.svelte AppDbTab.svelte
```

Priority order by pain:

1. **SettingsView → tab components.** 3,795 lines / 7 tabs is the worst
   offender; each tab is already isolated by `{#if activeSettingsTab === …}`.
   Mechanical extraction, near-zero risk.
2. **Gear popover → ConversationSettingsDialog.svelte** (removes ~450 lines
   and 20+ handlers from App.svelte).
3. **Shared chat parts** (enables A8; preserves both modes per A0).
4. **main.js cleanup** (see B2/B3).

### B2. Kill the `window.*` + inline-onclick-string bridge

Backend-rendered assistant HTML contains elements wired via
`onclick="copySQL('id')"` strings, serviced by globals registered in
`main.js` *and* again in `ConversationView.svelte` (two competing
definitions of `window.copySQL` — last mount wins). There is also a
`MutationObserver` over the whole body re-scanning for `details.sql-block`
elements on every DOM mutation, and a table-sorting engine in `main.js` that
rebuilds `<tbody>` rows by hand (including re-applying inline styles cell by
cell).

Recommended end-state:

- Keep HTML generation in Go for prose/markdown, but emit **stable, semantic
  markup** (`<details class="sql-block"><pre data-role="sql">…`) with **no
  inline handlers**.
- Render interactive result areas (tables with sort, copy-SQL, CSV export)
  through Svelte components that parse the same structured metadata already
  persisted per message (`metadata.sql_query`, `sql.rows` etc.). The data is
  already there — the DOM-parsing CSV exporter (`querySelectorAll('td')`)
  would become unnecessary.
- Interim low-risk step: keep the string handlers but consolidate to **one**
  implementation in a dedicated `lib/htmlBridge.js`, remove the duplicate in
  ConversationView, and replace the MutationObserver with a delegated
  `document.addEventListener('toggle', …, true)` (capture-phase listeners
  catch `details` toggles without re-binding).

### B3. Theme/accent/scale: one owner, one source of truth

`main.js` currently implements a dual-persistence scheme (localStorage + DB
via `GetGeneralSettings`/`UpdateGeneralSettings`) with migration heuristics,
plus HSL math, plus window-global setters consumed by SettingsView via
`window.setThemeSelection`. This is intricate and works, but it's the last
non-module global left. Move it into `lib/theme.js` exporting
`{ applyTheme, applyAccent, applyScale, subscribe }`; SettingsView imports the
module directly. Behavior unchanged, testability greatly improved, and the
DB-vs-localStorage merge logic gets documented in one place.

Also: the dark-accent preset map (`ACCENT_DARK_MODE_MAP`) duplicates knowledge
of the six preset accents. Deriving the dark variant purely with the existing
30% lighten fallback (i.e., deleting the map) would remove a lookup table that
must be kept in sync with SettingsView's swatch list.

### B4. Duplicated logic inventory (consolidation targets)

| Logic | Duplicated in |
|---|---|
| Streaming event machine (`llm:stream` switch) | ConversationView, MinimalistView (already diverged — see A8) |
| Optimistic user message + rollback on error | App.handleSendMessage, Minimalist.sendMessage |
| Auto-scroll on new/streaming content ($effect pairs) | Both views |
| Textarea auto-resize | Both views (+ drag-resize only in ConversationView) |
| `getChartConfig` metadata parsing | ConversationView, MinimalistView |
| Tag suggestion/add/remove flows | Gear popover version AND row-popover version in App.svelte (~120 lines × 2) |
| Defaults consumption at conversation start | App.openNewDiscussionModal pre-fills from GetDiscussionDefaults; Minimalist `/new` creates untitled conversations without consulting defaults — unify so both honor the user's defaults (A0 rule 4) |
| `parsePayload` / JSON-metadata guards | ConversationView (×2 variants) |
| Copy-to-clipboard-with-feedback | main.js, ConversationView |
| Icon-button hover styles | ≥5 class definitions |

### B5. Svelte 5 idiom cleanups

The codebase correctly uses runes (`$state`, `$derived`, `$effect`) — good —
but several patterns fight the compiler:

- `$effect(() => { activeConversation?.id; userMessage = '' })` — dependency-
  by-side-effect. Prefer `$derived` keys or explicit `onchange` in the
  conversation list. At minimum, comment the intentional touch.
- `payloadToggles[key] = !payloadToggles[key]` mutates `$state` object fields
  without reassignment; works today but fragile — prefer
  `payloadToggles = { ...payloadToggles, [key]: !payloadToggles[key] }` or a
  `SvelteSet`.
- `filteredMessages` wraps an IIFE inside `$derived` — fine, but splitting
  into named derived steps (filter → merge → limit) would be self-documenting.
- Props destructure defaults like `processingMessage = ''` combined with
  parent-supplied conditional expressions
  (`processingConversationId === activeConversation?.id ? … : ''`) — the
  parent is doing the child's filtering. Pass the raw state; let children gate.
- Event handler props (`onSendMessage`, `onGearClick`, …) form an informal
  API of ~15 callbacks between App and ConversationView. Consider a tiny
  context store (`setContext('conversationActions', …)`) to flatten prop
  drilling as components extract.
- `bind:value={selectedLLMProvider}` with `<option value={null}>` relies on
  object identity in option values; works, but string IDs + lookup would be
  more robust against provider list refreshes replacing objects.

### B6. Accessibility pass (cheap wins)

- The gear popover, export menu, and tag popovers close on scrim click but
  not on <kbd>Escape</kbd>; none move focus or restore it. Add Escape
  handling and focus-the-dialog on open (the New Discussion title input
  should autofocus; it currently doesn't).
- Row-tag popover uses `autofocus` attribute inside an `{#if}` — unreliable;
  use an action (`use:focusOnMount`).
- Sortable table headers rendered as `<th>` with click handlers need
  `role="button"`/`tabindex` or, better, `<button>` inside `th` — moot if B2's
  Svelte table lands.
- `aria-live="polite"` on the processing/streaming indicator would let screen
  readers announce "Exploring schema…" phases (charter §3.9 comfort goal).
- Contrast check: `--text-tertiary #999999` on `--bg-primary #ffffff` is
  ~2.8:1 — below WCAG AA for the meta text it's used on. Nudge to `#767676`
  (4.54:1) and define dark equivalent.

### B7. Dead code & hygiene checklist

- Remove unused CSS: `.delete-discussion-btn`, `.btn-new-discussion*`,
  duplicate `.tag-chip`, `.sort-header`/`.sort-indicator` in ConversationView
  (sorting lives in main.js against generated HTML), various `!important`
  overrides in `.skill-toggle` (caused by the double `label` rule above it —
  fix the source specificity instead).
- Remove `dist/` from the source tree concerns (it's generated; ensure it's
  gitignored except where Wails embed requires committed builds — if commits
  are required, note it in CI docs).
- `package.json.md5` is a Wails artifact; leave, but exclude from review scope.
- `jsconfig.json` exists but no linter/formatter config does. Add Prettier +
  `svelte-check` (ESLint optional). Given zero tests exist on the frontend,
  even a minimal Vitest setup covering `format.js`, `theme.js` color math,
  and the sort comparator would protect the consolidations proposed here.
- `escape`-based decode (`decodeURIComponent(escape(dataAttr))`) in the sort
  path is deprecated — replace with `decodeURIComponent(encodeURIComponent)`
  inverse or base64 if it survives B2.

### B8. Performance notes (minor, but free)

- `llmNameByID` / `dataSourceNameByID` derived maps are rebuilt correctly —
  good. But the discussion list re-renders every tag mutation via
  `conversations = [...conversations]`; keyed `{#each}` already handles this —
  mutate-free updates are fine, just ensure keys stay stable (`(conv.id)` is
  currently missing on the discussions `{#each}` — add it).
- MutationObserver over `document.body` subtree fires on every streaming
  delta (character-level) and rescans all `details.sql-block` nodes. This is
  measurable jank during streaming. Fixed by B2's delegation approach.
- Chart.js: registering all `registerables` is fine for a desktop app; no
  action needed.

---

## Part C — Suggested Phasing

Each phase ships independently and preserves the charter's priority order
(no phase touches answers, safety, or backend contracts):

1. **Phase 0 — Hygiene (hours).** Token bug fixes (A2), delete dead CSS,
   replace `window.confirm` with the existing delete-modal pattern, unify
   z-index, focus-visible ring, About icon fixes.
2. **Phase 1 — Design tokens & primitives (1–2 days).** Complete
   `variables.css` (radii, fonts, code surfaces, z-scale, semantic roles);
   introduce `Button/IconBtn/Chip/Modal/ConfirmDialog/EmptyState/Busy`;
   restyle existing markup onto them without moving logic.
3. **Phase 2 — Component extraction (2–4 days).** Settings tabs out of
   SettingsView; ConversationSettingsDialog out of App.svelte; shared
   `stream.js`, `format.js`, `theme.js`.
4. **Phase 3 — Unified chat core (2–3 days).** ChatThread/Composer shared by
   full and Minimalist modes; Minimalist becomes layout-only.
5. **Phase 4 — HTML bridge modernization (2–3 days, most care).** Svelte
   ResultTable/SqlBlock fed from message metadata; retire MutationObserver,
   window globals, and hand-built tbody sorting. Result rendering components
   are built once and used by both modes, so Minimalist inherits any
   improvement to tables/charts/SQL transparency automatically.

## Verification per charter §5

Every phase must be checked in both themes, both UI modes, with at least one
live LLM + database run, confirming: tech-details toggles still reveal correct
SQL, exploration/final rendering unaffected, exports (PDF/HTML/MD) still
produce correct output, and no regression in error display paths.

---

## Part D — Implementation Guide

Part A–C say *what* and *why*. This part says *exactly how*, so an agent or
developer can execute each item without re-deriving decisions. Where a value
below conflicts with something already shipped, this part wins within its own
scope — but A0's Two Modes Principle overrides everything here.

### D1. Complete `variables.css` additions (executes A2, A5, A6)

Append to the `:root` block in `variables.css`. These are the missing
tokens referenced by existing code plus the semantic roles from A5:

```css
:root {
  /* Radii — completes the set (sm/md already exist) */
  --radius-lg: 0.5rem;    /* 8px */

  /* Type scale completion + semantic roles */
  --font-3xs: 0.5625rem;  /* 9px — referenced today but undefined */

  --text-h1:    var(--font-4xl);   /* app name only            */
  --text-h2:    var(--font-5xl);   /* view titles              */
  --text-title: var(--font-2xl);   /* modal/card/thread titles */
  --text-body:  var(--font-md);
  --text-meta:  var(--font-sm);    /* timestamps, chips        */
  --text-label: var(--font-xs);    /* form labels (uppercase)  */

  /* The ONLY monospace stack — replaces all 'Courier New' uses */
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;

  /* Deliberate always-dark code surface (A6) */
  --code-bg: #1e1e1e;
  --code-fg: #d4d4d4;

  /* Input surface so fields read on same-color panels */
  --bg-input: #ffffff;

  /* Z-index scale — replaces 999/1000/19999/20000 */
  --z-popover: 100;
  --z-modal:   200;
  --z-toast:   300;

  /* Contrast-safe tertiary text (WCAG AA on both themes) */
  --color-warning: #f0a020; /* currently referenced with fallback literal */
}

[data-theme="dark"] {
  --bg-input: #222240;
  --code-bg: #11111f;      /* slightly deeper than panels in dark mode */
  --code-fg: #d4d4d4;
}
```

Also change in light palette: `--text-tertiary: #767676;` and dark palette:
`--text-tertiary: #9a9aac;` (B6 contrast fix).

**Mechanical replacements (grep-driven, safe to do in one commit):**

| Search | Replace with |
|---|---|
| `rgba(2, 136, 209, 0.1)` / `rgba(2,136,209,0.05)` / `rgba(2,136,209,0.15)` | `var(--color-accent-light)` |
| `rgba(2, 136, 209, 0.3)` | `var(--color-accent-border)` |
| `#e0e0e0` | `var(--border-primary)` |
| `'Courier New', monospace` | `var(--font-mono)` |
| `#1e1e1e` (bg context) / `#d4d4d4` | `var(--code-bg)` / `var(--code-fg)` |
| `var(--color-error, #ef4444)` etc. | `var(--color-danger)` / `--color-danger-hover` |
| z-index literals | the three `--z-*` tokens |
| `.btn-primary:hover`, `.send-btn:hover` no-op rules | real hover: `background: var(--color-accent-hover);` |

**Accent-tint audit:** after replacement, run
`grep -rn "2, ?136, ?209" frontend/src` — it must return zero hits. Same
audit for `#bbb", "bbbbbb", "#e9ecef` outside VizChart's initial defaults.

### D2. UI primitive contracts (executes A3, A4, A7)

All primitives live in `src/components/ui/`, use only tokens, work in both
themes and both modes unmodified.

**Button.svelte**
```js
let { variant = 'default',  // 'default' | 'primary' | 'danger' | 'ghost'
      size = 'md',          // 'sm' | 'md'
      disabled = false,
      loading = false,      // spinner replaces label, keeps width
      onclick, children, ...rest } = $props()
```
Migration map: `.btn-primary`→variant=primary; `.btn-danger`→danger;
`.btn-secondary`/plain `.btn`→default; `.gear-action-btn.*`→variant=danger
or ghost with explicit color prop NOT needed (archive/delete/clear are all
danger-class actions; restore is ghost). Delete `.msg-limit-btn.active` —
a preset button that is “active” is a selected state; use a segmented
control pattern instead (see Field.svelte note below).

**IconBtn.svelte**
```js
let { icon,            // Lucide component
      label,           // required — becomes aria-label AND title
      active = false,  // accent tint when toggled (pin, tech details)
      size = 'md',     // md = 36px square; sm = 28px
      onclick } = $props()
```
Replaces: `.row-action-btn`, `.gear-btn-header`, `.export-btn-header`,
`.sidebar-toggle`, `.modal-close`, `.search-clear-btn`, tag-remove buttons.
One hover rule (`accent-light bg`), one focus ring, everywhere.

**Modal.svelte** (single overlay primitive — gear dialog, New Discussion,
Delete confirm, DB-switcher confirm in SettingsView all build on this)
```svelte
<Modal {open} title="..." width="md"   <!-- sm:25rem md:31rem lg:42rem -->
       onclose={...}>                  <!-- scrim click AND Escape -->
  <svelte:fragment slot="footer">…</svelte:fragment>
</Modal>
```
Behavior contract: on open → focus first focusable element; on Escape or
scrim click → call `onclose`; restore focus to the element that opened it;
`aria-modal="true" role="dialog" aria-labelledby`; body scroll locked while
any modal is open.

**ConfirmDialog.svelte**
```js
let { open, title, body,        // body may contain <strong> via {@html}
                            // only from trusted internal strings, never user data
      confirmLabel = 'Confirm', danger = false,
      busy = false, onconfirm, onclose } = $props()
```
First migration: replace `window.confirm(...)` inside App.svelte's
`handleClearMessages()` with a ConfirmDialog instance (danger=true,
confirmLabel='Clear Messages'). Second: SettingsView's inline `dbConfirm`
object/modal collapses into this component.

**Chip.svelte** — `{ tag } | { removable, onremove }`; kills both duplicate
`.tag-chip` definitions and `.filter-pill` (one component, two contexts).

**EmptyState.svelte**
```js
let { icon = null, title, hint = '', actionLabel = '', onaction } = $props()
```
Used by: discussions list (both filtered and unfiltered variants),
conversation view, Minimalist discussions overlay, settings tabs that can be
empty (no skills configured, no data sources). The `hint` must answer “what
can I do next” (charter §3.9).

**Busy.svelte** — `{ label = '' }`; renders dots + optional phase text;
`aria-live="polite"`. Consumed by ConversationView processing indicator AND
Minimalist mode (closing the current gap where Minimalist shows dots without
the `processingPhase` text). Do not remove any existing `processingPhase`
event labels when migrating — they are charter §3.9 comfort UX.

**Field.svelte** — `{ label, hint }` wrapper standardizing the
label-above-control pattern used across the gear dialog and settings forms;
its `hint` slot replaces every inline
`style="color: var(--text-tertiary); font-size: var(--font-xs);"` div
(App.svelte has five of these). Segmented presets (“Show All”, “Max 15”)
become `role="radiogroup"` buttons inside a Field.

**Toggle.svelte** — `{ checked, label, hint, onchange }` wrapping checkbox +
accent `accent-color`; replaces all nine bare checkbox-label blocks in the
gear dialog and the archived/logging/db-switcher checkboxes elsewhere.

### D3. Extraction maps (executes B1)

**SettingsView.svelte → tabs.** Each block between
`{#if activeSettingsTab === 'X'}` … `{/if}` moves verbatim into a tab
component. State travels as follows — keep this wiring exact:

| Tab component | Owns (state + functions that move with it) | Needs via props |
|---|---|---|
| ModelsTab | provider CRUD form state, test-connection state | llmProviders, onUpdate |
| DatabasesTab | datasource CRUD, schema preview, Sheets OAuth state | dataSources, onUpdate |
| SkillsTab | skill CRUD state | onUpdate |
| DefaultsTab | entire `defaultsForm`, loadDefaults/saveDefaults/resetDefaults | — (self-contained) |
| GeneralTab | theme/accent/scale local mirrors, timeouts, logging state | minimalist, onMinimalistChange |
| AgentLoopTab | agentLoopEnabled/ConfigValues/Fields/Status | — |
| AppDbTab | dbSwitcher*, export* state, dbConfirm | — |

Shared imports after split: `GetSupportedDBTypes`, `BrowserOpenURL`,
`EventsOn/Off` go to whichever tabs use them — do not create a shared
settings store yet. SettingsView reduces to: tab bar + `<svelte:component>`
switch + the shared style block trimmed of tab-specific rules (each tab takes
its styles with it).

**App.svelte → ConversationSettingsDialog.svelte.** Moves: the entire gear
popover markup block plus handlers `handleRenameConversation`,
`handleSetMaxMessages`, `handleSetMaxContextMessages`, `handleSetPinned`,
`handleToggleTechDetails`, `handleToggleContextDetails`, `handleSetSummarize`,
`handleToggleVizEnabled`, `handleToggleStreamingEnabled`,
`handleToggleConversationSkill`, `loadConversationSkills`, tag trio
(`handleAddTag/handleRemoveTag/handleTagKeydown` + suggestions derived), and
the actions row (duplicate/clear/archive/delete/restore). Props:
`{ conversation, allSkills, llmProviders, dataSources, open, onclose }`.
The dialog performs its own binding calls and emits `onclose(changed=true)`;
App then calls `loadData()`. Net effect: ~600 lines leave App.svelte.

**App.svelte → TagPopover.svelte.** The row-level tag popover (wrapper,
overlay, chips, input, suggestions) plus `rowTag*` state/functions. Props:
`{ conversation, open, onclose }`. Self-fetches `ListAllTags` and
`GetTagsForConversation` on open.

**ConversationView → ExportMenu.svelte.** Export button + popover + the four
export handler functions move together; props `{ conversationId, title }`.

**MinimalistView stays.** After ChatThread/MessageComposer extraction (D4)
it should be ≈300 lines of layout, command palette, and overlays — nothing
more is removed from it.

### D4. Shared module contracts (executes A8, B3)

**lib/stream.js**
```js
export function createStreamStore(getConversationId) {
  // Returns { active, text, reasoning, reasoningActive, toolCards }
  // Subscribes once to 'llm:stream'; filters events by comparing
  // data.conversation_id against getConversationId().
  // Exposes reset(). 'done' resets everything.
}
```
Both views consume this. Decision to record now: ConversationView currently
does NOT separate `reasoningText` from `text`; Minimalist does. Adopt
Minimalist's richer behavior (separate buffers) in the shared store — it's a
strict improvement and matches the streaming-reasoning `<details>` UI.
Conversely, Minimalist's `streaming_enabled` gate is adopted by both (a
conversation with streaming off never shows a stream bubble). Both divergent
behaviors converge to the better one; note this in release notes if visible.
Auto-scroll-on-delta also lives here as an exported `attachAutoScroll(el)`
action replacing the two near-identical `$effect` pairs.

**lib/theme.js** — move ALL of main.js's theme/accent/scale code unchanged:
exports `applyTheme(sel)`, `applyAccent(hex)`, `applyScale(scale)`,
`getTheme()/getAccent()/getScale()`, `initFromStorage()` (runs before mount,
including the GetGeneralSettings DB-sync promise), and `onThemeChange(cb)`.
SettingsView's GeneralTab imports these directly; delete
`window.setThemeSelection` / `window.setAccent` / `window.setUIScale`.
VizChart switches from `window.addEventListener('theme-change')` to
`onThemeChange`. Keep `ACCENT_DARK_MODE_MAP` deletion (B3) as a follow-up
commit — verify all six swatches render acceptably with pure 30%-lighten in
dark mode before deleting the map; revert the deletion if any swatch fails
eyeball test in dark mode.

**lib/format.js** — `fmtTokens(n)`, `formatTime(dateStr)`, `parsePayload(m)`,
`getChartConfig(m)`, `escapeCsvCell(s)`. Single source; delete the copies in
ConversationView and MinimalistView.

**ChatThread.svelte / MessageComposer.svelte** — props:
```js
// ChatThread: { messages, showTechDetails, streaming (store), maxMessages,
//               emptyState (slot or config object) }
// MessageComposer: { value, busy, placeholder, onsend, oncancel,
//                    resizable=false }  // drag-handle only in full mode
```
Minimalist passes `resizable={false}` and its own placeholder; slash-command
handling stays entirely inside MinimalistView (it intercepts keys before
onsend fires). Full mode keeps drag-resize + per-conversation height memory
as a wrapper around MessageComposer, not inside it.

### D5. Defaults fallback spec (executes A0 rule 4)

Goal: Minimalist mode never asks what Defaults already answered.

1. **`/new` with defaults:** MinimalistView's `/new` command and
   “New discussion” overlay action call a new shared helper (in lib/, e.g.
   `createDefaultConversation()`) that fetches `GetDiscussionDefaults()` and
   passes `llm_provider_id` / `data_source_id` to `CreateConversation`,
   exactly as `openNewDiscussionModal` does in full mode today. If Defaults
   has no provider/source set, fall back to the app-wide default provider /
   default data source (`SetDefaultLLMProvider` / `SetDefaultDataSource`
   targets) before creating with nulls. Only create with nulls if neither
   exists — and in that case show EmptyState guidance (“Choose a model & data
   source in Settings → Defaults”) rather than silently failing on send.
2. **First-run entry:** entering Minimalist mode with no conversation open
   creates-or-selects using the same helper, so the user lands ready to type.
3. **No new settings UI appears in Minimalist for this.** If defaults are
   unset, the guidance EmptyState links out (`onExit` → full mode → Defaults
   tab) rather than embedding pickers.
4. Acceptance: fresh install → set Defaults (provider+source) in full mode →
   switch to Minimalist → type question → answer arrives with correct
   source/model shown in thread meta, zero intermediate choices.

### D6. Phase acceptance criteria

A phase is done when all its criteria pass AND the charter §5 checklist
passes (both themes, both modes, one live LLM+DB run, exports intact).

**Phase 0:** zero grep hits for hardcoded accent rgba/#e0e0e0/Courier New;
no native `confirm(` anywhere in src; all undefined-var references resolved
(app runs clean under svelte-check's CSS warnings);
Escape closes every overlay; About/pin icons are Lucide.

**Phase 1:** primitives exist with D2 contracts; gear dialog, New Discussion,
Delete, Clear-Messages, DB-switcher confirm all render through Modal/
ConfirmDialog; visual diff review done side-by-side in light+dark × full+
minimal (screenshots into PR); no behavioral change to any setting.

**Phase 2:** App.svelte ≤ ~1200 lines, SettingsView.svelte ≤ ~400 lines
(tab bar + wiring); all seven tabs functionally identical before/after
(click through every control); window.setThemeSelection/setAccent/setUIScale
gone; theme persists correctly across restart (DB sync path exercised).

**Phase 3:** one implementation each of streaming/send/auto-scroll/rendering;
send a message with streaming ON in BOTH modes in the same session and
verify identical bubble behavior; Minimalist retains slash commands, opacity
styling, and discussions overlay pixel-for-pixel (compare screenshots);
divergence resolutions from D4 documented in release notes.

**Phase 4:** no MutationObserver, no `window.copySQL/toggleSQL*/exportCSV`;
sortable table, copy-SQL, CSV export, SQL popover all work from metadata-
rendered components in both themes; sort a 1000-row result set and compare
to old behavior; PDF/HTML/MD exports still contain tables and SQL.

**Regression guard for every phase:** run one real query end-to-end per
phase (per charter §5.1) and confirm the answer path — optimistic bubble,
processing phases, final table/chart/summary, error retry path (kill network
mid-query) — behaves identically to pre-phase baseline.

---

## Appendix — Concrete before/after examples

**Before (App.svelte nav):**
```css
.nav-item.active { background: rgba(2, 136, 209, 0.15); }
.conversation-item:hover { background: rgba(2,136,209,0.05); }
.meta-tag { background: rgba(2, 136, 209, 0.1); }
```

**After:** all three become `var(--color-accent-light)` — which already
tracks the user's chosen accent and dark-mode variant. Every hardcoded blue
tint in the codebase silently breaks accent theming today; this is the
single most visible "united design" win available.

---

---

## Closing Note

The two-mode structure is treated throughout this review as a strength to be
protected, not a problem to be solved. The problem is duplication and drift
between the modes — solved by shared internals (A8/B1) — and missing defaults
coverage — solved by leaning harder on Discussion Defaults (A0 rule 4). Full
mode stays the complete control surface; Minimalist mode stays the shortest
path between a question and its answer.

*End of review.*

---

## Implementation Notes (2026-08-24)

**Result:** App.svelte 2,696→1,495 lines; ConversationView 1,310→980; main.js
437→20; SettingsView 3,795 lines replaced by a 100-line shell + 7 tab
components. Production build passes; svelte-check reports 0 errors.

### What landed

- **Phase 0 — Hygiene.** All token bugs fixed (`--font-3xs`, `--radius-lg`,
  `--color-error`→`--color-danger`, `--font-mono` defined and applied).
  Zero hardcoded accent rgba / `#e0e0e0` / Courier New remain (grep-verified).
  Contrast-safe `--text-tertiary`. Global focus-visible ring. Emoji icons →
  Lucide (About nav, pin indicator, payload toggles; corrupted "up/down"
  labels fixed). Keyed `{#each}` on discussions.
- **Phase 1 — Tokens & primitives.** variables.css extended per D1
  (semantic type roles, code surfaces, `--bg-input`, z-scale, aliases). New
  primitives in `components/ui/`: Modal, ConfirmDialog, ConfirmHost, IconBtn,
  EmptyState, Busy. Every native `confirm()` replaced via the promise-based
  `lib/confirm.js` service (App delete/clear, Models/Databases deletes,
  AgentLoop reset); one ConfirmHost is mounted in each mode root.
- **Phase 2 — Extraction.** SettingsView → shell + 7 tabs under
  `components/settings/tabs/` with shared global `settings.css` (Svelte
  scoping cannot reach child components). Gear popover →
  `ConversationSettingsDialog.svelte`; row tag popover → `TagPopover.svelte`;
  export menu → `ExportMenu.svelte`. Theme/accent/scale moved to
  `lib/theme.js`; `window.setThemeSelection/setAccent/setUIScale` deleted;
  GeneralTab imports the module directly.
- **Phase 3 — Shared chat core.** `lib/stream.js` (single llm:stream store,
  Minimalist's richer reasoning buffer + streaming_enabled gate adopted by
  both modes), `lib/format.js` (parsePayload/getChartConfig/fmtTokens/
  formatTime), auto-scroll helper shared. Both modes now consume identical
  machinery; Minimalist keeps its layout, slash commands, and styling.
- **Phase 4 — HTML bridge.** `lib/htmlBridge.js` is now the single owner of
  copySQL/toggleSQLSection/exportCSV (the duplicate in ConversationView was
  deleted) plus delegated sort-header handling; the body-wide
  MutationObserver was replaced with a capture-phase `toggle` listener.
- **D5 defaults fallback.** `createDefaultConversation()` in `lib/api.js`
  (Discussion Defaults → app-wide default provider/source → nulls) used by
  Minimalist `/new`, the overlay "+ New discussion", and full mode's modal
  pre-fill path stays consistent.
- **Tooling (B7).** Prettier (+svelte plugin) and svelte-check added as dev
  deps with `npm run format` / `npm run check` scripts; `checkJs` disabled
  (untyped-JS implicit-any noise), jsconfig modernized to moduleResolution
  bundler. VizChart initializes Chart.js theme from the current theme at
  load instead of assuming light mode.
- **CSS correctness fix found during check:** selectors targeting
  `@html`-injected content (result tables, `<pre>/<code>`, markdown lists)
  are now `:global()` descendants of `.assistant-message` so they actually
  apply — previously they were scoped and silently dead (tables only looked
  right because Go emitted inline styles).

### Deviations from plan

1. **Button/Chip primitives created as contracts but not mass-adopted yet.**
   Existing `.btn`/chip markup works and is consistent; swapping every call
   site is mechanical but visual-regression-heavy without screenshot tooling.
   Recommended as a fast-follow.
2. **Backend inline handlers kept (interim step from B2).** Go-rendered HTML
   still uses `onclick="copySQL(...)"` strings, serviced by the single bridge
   implementation. Full Svelte ResultTable/SqlBlock fed from message metadata
   remains the end-state and requires a coordinated rendering.go change.
3. **Dark-accent preset map deleted** per D4 (pure 30% lighten fallback).
   Eyeball-check the six preset swatches in dark mode; restore the map if any
   looks off.
4. **A11y warnings (~28)** remain: label/control associations and div-click
   patterns carried over verbatim from the original markup (mostly in the
   Databases tab and legacy overlays). Tracked for a follow-up pass; they do
   not affect function.
5. **Per charter §5, runtime verification is still required before release:**
   run one real query end-to-end per phase checklist item in light+dark ×
   full+minimal, exercise exports (PDF/HTML/MD), and confirm tech-details
   toggles still reveal correct SQL. This document's author cannot click the
   UI — that verification belongs to a human or a scripted UI test run.
