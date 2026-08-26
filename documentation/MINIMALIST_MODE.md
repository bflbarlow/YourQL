# Minimalist Mode

> **Goal:** Give users a distraction-free interface that shows only the question and the answer — nothing else.

---

## 0. The Fundamental Goal

**YourQL exists to serve the answer to a user's question from their database.**

Minimalist mode is a *viewing mode*, not a feature addition. It does not add functionality — it removes UI chrome so the user sees more of the answer and less of the app. Everything in this document must be evaluated against: *Does this removal make the answer more visible and the experience calmer?*

### What Minimalist Mode Is (and Is Not)

| Is | Is Not |
|---|---|
| A single toggle that hides UI chrome | A new app or separate mode |
| A cleaner view of the existing conversation | A simplified LLM loop or fewer tools |
| Persisted per-user via `app_settings` | A different set of capabilities |
| Optional and reversible | A different theme or color palette |

---

## 1. Design Goals

### 1.0 Universal Scope — This Is a Global UI Change

**Minimalist mode applies to every screen, page, popup, and panel in the application.** It is not limited to the conversation view. This is a universal UI transformation — the entire app's chrome is simplified, not just one view.

What this means:

- **All Svelte components** that render UI chrome (sidebars, toolbars, buttons, panels, dialogs, popups, settings views, about page, update notifications, empty states) must respect the minimalist toggle.
- **CSS is the mechanism.** A single global `[data-minimalist="true"]` attribute on `<html>` drives the entire transformation — matching the existing `[data-theme]` and `[data-ui-scale]` pattern in `variables.css`. No component-by-component logic — just CSS variable overrides and conditional hiding via attribute selectors.
- **No component receives a prop.** The toggle state flows through a Svelte store and the global `data-minimalist` attribute. Components respond to the CSS attribute selector, not to props.
- **SettingsView, About page, update dialogs, confirmation popups** — all of these get the same treatment. Everything becomes quieter, tighter, and less busy.

This is intentional. If the user turns on minimalist mode, they want the *entire app* to feel minimal, not just the conversation area with the rest of the chrome still visible in the background.

### 1.1 Visual

- **Maximize answer real estate.** The conversation area should occupy the full window.
- **Hide everything that isn't the question or the answer.** Sidebar, settings panels, toolbars, status bars — all gone when minimalist mode is on.
- **Reduce visual density.** Tighter line-height, smaller typography, less padding, thinner borders.
- **Maintain theme parity.** Light and dark modes must both look clean in minimalist mode. No new colors — reuse the existing CSS variable system.

### 1.2 Behavioral

- **Toggle on/off with a single action.** A keyboard shortcut (`Cmd+Shift+M` on macOS, `Ctrl+Shift+M` on Windows/Linux), a settings toggle, and — critically — a persistent quick switch visible on every screen in both modes.
- **Persist across sessions.** Stored in `app_settings` as `minimalist_mode: "true"` / `"false"`.
- **No jarring transitions.** Smooth CSS transitions on layout changes.
- **Graceful degradation.** If a component fails to render in minimalist mode, the answer still reaches the user.

### 1.3 Trust

- **No hidden functionality.** Features aren't removed — they're hidden. The user can always restore them.
- **Tech details toggle still works.** The user can still see the actual SQL that was run.
- **Streaming still streams.** No change to the agentic loop or output pipeline.

---

## 2. What Gets Hidden (The "Chrome")

> **This list is not exhaustive by design — it's a representative sample.** Because minimalist mode is a global CSS transformation, *anything* that is chrome (not the answer itself) gets hidden or simplified automatically. The CSS rules define the behavior; any new UI element added in the future will inherit the same treatment unless explicitly excluded.

| UI Element | Current Location | Minimalist Behavior |
|---|---|---|
| **Sidebar (conversation list)** | Left panel in `App.svelte` | Hidden; the conversation area fills the full window width |
| **Top toolbar/status bar** | `App.svelte` header | Hidden |
| **Settings panel** | `SettingsView.svelte` | Simplified — fewer sections visible, tighter spacing, no illustrations |
| **About page** | (About view) | Simplified — less chrome, tighter layout |
| **Processing phase indicators** | `ConversationView.svelte` ("Exploring schema…", "Generating query…") | Hidden (the answer is what matters; intermediate phases are noise) |
| **Query count / round indicators** | `ConversationView.svelte` | Hidden |
| **Extra buttons (export, copy, regenerate)** | `ConversationView.svelte` message actions | Hidden; keep only the tech details toggle |
| **Chart visualization** | `VizChart.svelte` | Hidden; the data table is sufficient |
| **Footer / bottom bar** | `App.svelte` or `ConversationView.svelte` | Hidden |
| **Conversation metadata (provider, data source)** | `ConversationView.svelte` | Hidden |
| **Empty state illustrations** | `ConversationView.svelte` | Simplified — just a prompt, no illustration |
| **Confirmation dialogs / popups** | Anywhere | Tighter spacing, no illustrations, fewer visible fields |
| **Update notification banners** | `App.svelte` | Hidden or simplified |
| **Any new future UI element** | N/A | Inherits the same treatment — if it's chrome, it hides; if it's the answer, it stays |

### What Stays Visible (Across All Screens)

| UI Element | Rationale |
|---|---|
| **User question input** | The primary interaction point |
| **Conversation messages (questions + answers)** | The answer is the product |
| **Tech details toggle** | Trust signal — shows the actual SQL |
| **Streaming cursor / indicator** | Shows the app is working |
| **Cancel button** | Safety — the user must be able to stop processing |
| **Quick switch (persistent, both modes)** | A small, always-visible toggle or icon rendered at the `App.svelte` root level — visible in full mode and minimalist mode alike. In full mode it switches minimalist *on*; in minimalist mode it switches it *off*. The user never has to hunt for it. |
| **Any required confirmation (delete, clear)** | Safety — never hide destructive-action confirmations |
| **Any new future UI element that is part of the answer** | Inherits the same treatment — if it's part of the answer, it stays; if it's chrome, it hides |

---

## 3. Technical Project Plan

### Phase 1: Infrastructure (app-level)

**Files to modify:** None new; existing files get small additions.

1. **Use existing `app_settings` infrastructure (no new functions needed)**
   - `app.go` already exposes `GetAppSetting(key)` and `SetAppSetting(key, value)` as Wails bindings (lines 919–925), which delegate to `services.GetAppSetting` / `services.SetAppSetting`. The frontend can call these directly with key `"minimalist_mode"` — no new Go functions or bindings required.
   - Optionally add convenience wrappers in `pkg/services/app_settings.go` if the bool conversion feels unwieldy, but they are strictly optional — the existing functions are sufficient:
     ```go
     func GetMinimalistMode() (bool, error) {
         v, err := GetAppSetting("minimalist_mode")
         return v == "true", err
     }
     func SetMinimalistMode(enabled bool) error {
         v := "false"
         if enabled { v = "true" }
         return SetAppSetting("minimalist_mode", v)
     }
     ```
     If added, expose them as Wails bindings on `*App` in `app.go` (same pattern as `GetAppSetting` / `SetAppSetting`).

2. **Add keyboard shortcut (Svelte-side `keydown` listener)**
   - Wails v2 has no `runtime.BindKey` or global keybinding API. The existing pattern in this codebase is DOM-level event listeners on the Svelte side.
   - In `App.svelte`, add a `<svelte:window>` `on:keydown` handler that checks for `Meta+Shift+M` (macOS) or `Ctrl+Shift+M` (Windows/Linux).
   - On activation: toggle the store, call `SetAppSetting("minimalist_mode", newValue)`, and update the `data-minimalist` attribute on `<html>`.
   - Also toggle the persistent quick-switch UI element (see Phase 2 item 5). The quick switch is always rendered — in both modes — so the keyboard shortcut and the switch stay in sync.

3. **Startup initialization (frontend fetches state on mount)**
   - On mount in `App.svelte`, call `GetAppSetting("minimalist_mode")` and set the initial store value + `data-minimalist` attribute.
   - This covers the case where the user restarts the app — the frontend learns the persisted state immediately, without waiting for an event that may never fire.

### Phase 2: Frontend Changes (Svelte 5)

**Files to modify:** `frontend/src/App.svelte`, `frontend/src/ConversationView.svelte`, `frontend/src/variables.css`

1. **CSS variable additions** (in `variables.css`)
   - Follow the existing `html[data-theme]` / `html[data-ui-scale]` pattern — use a data attribute, not a class:
   ```css
   html[data-minimalist="true"] {
     --sidebar-width: 0;
     --toolbar-height: 0;
     --footer-height: 0;
     --message-padding: 0.5rem;
     --border-width: 0;
     --font-size-base: 0.875rem;
     --font-size-lg: 1rem;
     --line-height: 1.4;
     --transition-speed: 0.2s;
   }
   ```

2. **`App.svelte` changes**
   - On mount, call `GetAppSetting("minimalist_mode")` to read the persisted state and initialize the store.
   - Add a Svelte 5 `$state` variable (`let minimalist = $state(false)`).
   - **Set `document.documentElement.setAttribute('data-minimalist', minimalist ? 'true' : 'false')`** whenever the value changes — this is how every component in the app inherits the CSS overrides.
   - Add a `<svelte:window on:keydown={handleKeydown}>` listener that checks for `Meta+Shift+M` (macOS) or `Ctrl+Shift+M` (Windows/Linux) to toggle.
   - **Hide the sidebar with `display: none`** when minimalist mode is active — setting `--sidebar-width: 0` alone may still show borders or scrollbars. Use a conditional class or `style` binding:
     ```svelte
     <aside style:display={minimalist ? 'none' : ''}>
       <!-- sidebar -->
     </aside>
     ```
   - The main content area fills the full window width (no split-pane layout).
   - Use `transition: width 0.2s ease` on the sidebar container (for the collapse toggle) and `transition: opacity 0.15s ease` on toolbar elements.
   - **Render the persistent quick switch at the `App.svelte` root level** (outside any conditional view). It is always visible — in full mode and minimalist mode alike — so the user can toggle in either direction from any screen. See Phase 2 item 5 for full details.

3. **`ConversationView.svelte` changes**
   - Hide processing phase indicators, round counters, metadata, and extra action buttons.
   - Keep only: messages, tech details toggle, cancel button, input area.
   - Tighter message styling (smaller padding, thinner borders).
   - **Note:** Most of this is handled automatically by the global `[data-minimalist="true"]` CSS rules. If a component respects those rules, no component-specific changes are needed — just verify.

4. **Empty state simplification**
   - Replace the illustration with a simple centered prompt: "Ask a question about your data."
   - **Apply to all empty states across the app** — SettingsView, About page, etc. Every empty state gets the same treatment: no illustration, just a brief prompt.

5. **Persistent quick switch (both modes, every screen)**
   - **Goal:** No matter which screen the user is on — conversation view, settings, about page, empty state — and no matter which mode they're in — full or minimalist — there must be a visible, clickable element that toggles between the two modes.
   - **In full mode:** The switch reads as an invitation to simplify — e.g., a small toggle icon or a subtle "Minimal" pill. It fits cleanly into the existing chrome without adding visual weight.
   - **In minimalist mode:** The same element becomes the exit path — e.g., the same icon in a different state, or a thin "Exit minimalist" pill. It must remain visible and clickable but still respect minimalist density (low opacity, no border, small footprint).
   - **Implementation:** Rendered at the `App.svelte` root level, outside any conditional view. Always rendered — never hidden, never unmounted. Only its appearance changes (full-mode styling vs. minimalist-mode styling).
   - **Styling (full mode):** Unobtrusive — a small icon or text link, muted color, sits naturally in the existing layout (e.g., bottom-right corner or top bar).
   - **Styling (minimalist mode):** Even more restrained — lower opacity, smaller text, no background, no border. On hover, opacity increases slightly to signal it's interactive.
   - **Behavior:** Single click toggles the mode immediately — no confirmation, no delay. Works in both directions.

### Phase 3: Polish

1. **Smooth transitions**
   - CSS `transition` on toolbar opacity, message padding, and font sizes.
   - **Note:** The sidebar uses `display: none` to fully hide (avoiding borders/scrollbars that `width: 0` can leave behind). `display` does not animate — the sidebar reappears immediately when toggled off. This is acceptable because the toggle is a conscious user action, not a hover or gradual reveal.
   - No layout shift — the content area smoothly expands to fill the space.
   - **Global scope.** Transitions must feel smooth regardless of which screen the user is on — the CSS attribute applies everywhere.

2. **Dark mode verification**
   - Test both light and dark in minimalist mode.
   - Ensure contrast ratios are still WCAG-compliant.
   - **Test on every screen** — not just the conversation view. SettingsView, About page, any popup/dialog must look clean in both themes.

3. **Window resize behavior**
   - Minimalist mode should look good at any window size (the user can resize the Wails window).
   - No horizontal scroll in the conversation area.
   - **Test on every screen** — the layout must hold up at all window sizes across all views.

4. **Keyboard shortcut feedback**
   - Brief visual indicator (e.g., a toast or status flash) when the toggle activates — but keep it minimal.
   - **Must work on every screen** — the shortcut is global, so the feedback must also be global.
   - Since the shortcut is handled entirely on the Svelte side, the feedback (toast, flash, etc.) is also handled in Svelte — no Go-side event emission needed.

---

## 4. Files to Create

| File | Purpose |
|---|---|
| *(none)* | No new files needed. This is a view mode, not a new feature. |

---

## 5. Files to Modify

| File | Change |
|---|---|
| `pkg/services/app_settings.go` | Optionally add `GetMinimalistMode()` / `SetMinimalistMode()` convenience wrappers (existing `GetAppSetting`/`SetAppSetting` are sufficient) |
| `app.go` | Optionally expose convenience wrappers as Wails bindings (existing `GetAppSetting`/`SetAppSetting` are already bound and sufficient) |
| `frontend/src/App.svelte` | On-mount init from `GetAppSetting`, `$state` store, `data-minimalist` attribute, `svelte:window` keydown listener, `display: none` for sidebar, persistent quick switch (always rendered, both modes) |
| `frontend/src/SettingsView.svelte` | Add the minimalist mode toggle in the General tab; simplify sections in minimalist mode |
| `frontend/src/ConversationView.svelte` | Hide phase indicators, metadata, extra buttons; tighten message styling (verify against global CSS) |
| `frontend/src/variables.css` | Add `html[data-minimalist="true"]` CSS variable overrides |
| `documentation/MINIMALIST_MODE.md` | This file — update if scope changes |

---

## 6. Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| **User gets lost — can't find the sidebar back** | Low | Keyboard shortcut (`Cmd+Shift+M` / `Ctrl+Shift+M`) always toggles it back; the persistent quick switch is on every screen in both modes; the toggle is also always visible at the top of the Settings General tab |
| **Layout breaks at certain window sizes** | Medium | Test at minimum Wails window size (1024×1152) and larger; use flexbox, not fixed widths |
| **CSS transition conflicts with theme variables** | Low | Test light + dark separately; use `transition` only on width/opacity, not on color variables |
| **Settings panel still shows minimalist toggle** | Low | The settings panel is a separate concern; the toggle there is the source of truth |
| **No regression on the existing non-minimalist path** | High | Minimalist mode is purely CSS + conditional rendering — no logic changes. Existing paths unchanged. |

**Risk/reward analysis:** This change is **low risk, high reward**. It adds no new logic — only CSS and conditional rendering. The existing non-minimalist path is untouched. The primary risk is visual regression (layout breaks), which is easily caught by testing both themes at the window size.

---

## 7. Testing Checklist

- [ ] Toggle via keyboard shortcut (`Cmd+Shift+M` on macOS, `Ctrl+Shift+M` on Windows/Linux)
- [ ] Toggle via Settings panel (General tab — toggle is always visible, even in minimalist mode)
- [ ] Persisted across app restarts
- [ ] Light theme in minimalist mode
- [ ] Dark theme in minimalist mode
- [ ] Sidebar reappears when toggled off
- [ ] Toolbar reappears when toggled off
- [ ] Smooth transitions (no jarring layout shift)
- [ ] Messages are readable at all window sizes
- [ ] Tech details toggle still works
- [ ] Cancel button still works
- [ ] Input area still functional
- [ ] Streaming still works
- [ ] Empty state looks clean
- [ ] No horizontal scroll in conversation area
- [ ] Settings toggle is always visible and reachable (never hidden by minimalist mode itself)
- [ ] Persistent quick switch visible on every screen in both modes (conversation, settings, about, empty state)
- [ ] Quick switch is clickable and works immediately (no confirmation, no delay)
- [ ] No regression on the existing (non-minimalist) path

---

## 8. Remember

**The user asked a question. Minimalist mode is just a cleaner way to show them the answer.**

Don't over-engineer this. The goal is *less*, not *more*. Every pixel removed is a win. Every pixel added is a cost. If a change makes the minimalist view look busier, it's a step backward.

*Keep this document current. If your change alters anything described here, update the relevant section in the same change.*
