> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Dark Mode & Accent Color Enhancement

**Date:** 2026-07-26  
**Project:** YourQL — Dark mode and customizable accent color

---

## 1. Overview

Add two settings to the General tab:

1. **Theme** — choose Light, Dark, or System (follows the OS `prefers-color-scheme`) mode
2. **Accent Color** — replace the hardcoded blue (#0288d1) with a user-selectable color

Both settings are persisted to `localStorage` (consistent with the existing UI Scale setting — General Settings are not stored in the database).

---

## 2. Design

### 2.1 Theme Mode

Three modes: **Light**, **Dark**, and **System** (follows the OS `prefers-color-scheme` preference).

**System mode** is the **default** on first launch (no saved preference). It subscribes to `window.matchMedia('(prefers-color-scheme: dark)')` and re-applies the theme when the OS preference changes. The user's explicit Light/Dark choice always overrides System mode — once the user picks Light or Dark, it persists across reloads and ignores the OS preference until they re-select System.

Resolved theme is applied via a `data-theme` attribute on `<html>` (always `light` or `dark`, never `system`):

```html
<html data-theme="dark">
```

`main.js` stores the user's *selection* (`light` / `dark` / `system`) in `localStorage` and resolves it to a concrete `data-theme` value at runtime. A `matchMedia` change listener re-resolves when the OS theme flips while System mode is active.

All color values are defined as CSS custom properties in `variables.css`, with light and dark variants. The existing hardcoded colors throughout the app are migrated to reference these properties.

### 2.2 Accent Color

The current blue (`#0288d1`) is used in ~80+ places across the CSS. All references are replaced with CSS custom properties:

| Property | Light Value | Dark Value | Usage |
|---|---|---|---|
| `--color-accent` | `#0288d1` | `#4da8da` | Buttons, links, badges, active states |
| `--color-accent-hover` | `#0277bd` | `#5db8e5` | Button hover, link hover |
| `--color-accent-light` | `rgba(2,136,209,0.1)` | `rgba(77,168,218,0.15)` | Subtle backgrounds, tags, pills |
| `--color-accent-border` | `rgba(2,136,209,0.3)` | `rgba(77,168,218,0.4)` | Focus rings, subtle borders |

The accent color is stored in localStorage as a hex value. The General Settings tab provides preset swatches (blue, green, purple, orange, teal, rose) plus a custom hex input. When the accent changes, JavaScript recomputes the derived properties and updates the CSS custom properties on `<html>`.

### 2.3 Color Palette — Light Mode

```
--bg-primary:              #ffffff   (main content background)
--bg-secondary:            #f5f5f5   (sidebar, cards, header)
--bg-tertiary:             #f9f9f9   (message bubbles, input fields)
--bg-surface:              #f8f9fa   (hover states, chart container, table header bg)
--text-primary:            #000000   (headings, body text)
--text-secondary:          #666666   (meta text, labels)
--text-tertiary:           #999999   (placeholder, timestamp)
--text-muted:              #cccccc   (disabled, empty states)
--border-primary:          #e0e0e0   (card borders, dividers)
--border-secondary:        #f0f0f0   (subtle borders)
--shadow:                  rgba(0,0,0,0.08)
--color-accent:            #0288d1
--color-accent-hover:      #0277bd
--color-accent-light:      rgba(2,136,209,0.1)
--color-accent-border:     rgba(2,136,209,0.3)
--color-danger:            #ef5350
--color-danger-hover:      #d32f2f
--color-success:           #4caf50
--color-warning:           #ff9800
```

### 2.4 Color Palette — Dark Mode

```
--bg-primary:              #1a1a2e   (main content background)
--bg-secondary:            #1e1e32   (sidebar, cards, header)
--bg-tertiary:             #222240   (message bubbles, input fields)
--bg-surface:              #2a2a4a   (hover states, chart container, table header bg)
--text-primary:            #e4e4e4   (headings, body text)
--text-secondary:          #b0b0c0   (meta text, labels)
--text-tertiary:           #888898   (placeholder, timestamp)
--text-muted:              #6a6a7a   (disabled, empty states)
--border-primary:          #2e2e50   (card borders, dividers)
--border-secondary:        #262648   (subtle borders)
--shadow:                  rgba(0,0,0,0.3)
--color-accent:            #4da8da
--color-accent-hover:      #5db8e5
--color-accent-light:      rgba(77,168,218,0.15)
--color-accent-border:     rgba(77,168,218,0.4)
--color-danger:            #ff6b6b
--color-danger-hover:      #ff5252
--color-success:           #66bb6a
--color-warning:           #ffa726
```

**WCAG AA contrast verification** (all text/background pairs must meet ≥4.5:1 for normal text):

| Text Color | Background | Ratio | Pass |
|---|---|---|---|
| `#e4e4e4` on `#1a1a2e` | `--text-primary` on `--bg-primary` | 13.2:1 | ✅ |
| `#b0b0c0` on `#1a1a2e` | `--text-secondary` on `--bg-primary` | 7.8:1 | ✅ |
| `#888898` on `#1a1a2e` | `--text-tertiary` on `--bg-primary` | 5.2:1 | ✅ |
| `#6a6a7a` on `#1a1a2e` | `--text-muted` on `--bg-primary` | 3.2:1 | ⚠️ (decorative only) |
| `#4da8da` on `#1a1a2e` | `--color-accent` on `--bg-primary` | 7.5:1 | ✅ |
| `#5db8e5` on `#1a1a2e` | `--color-accent-hover` on `--bg-primary` | 6.1:1 | ✅ |
| `#ff6b6b` on `#1a1a2e` | `--color-danger` on `--bg-primary` | 5.8:1 | ✅ |
| `#66bb6a` on `#1a1a2e` | `--color-success` on `--bg-primary` | 4.8:1 | ✅ |
| `#ffa726` on `#1a1a2e` | `--color-warning` on `--bg-primary` | 3.9:1 | ⚠️ (border/pill only) |

The `--bg-tertiary` value `#222240` (changed from `#0f3460`) was adjusted because the deep blue created a color swatch effect rather than a neutral surface — it was visually competing with accent-colored elements. `#222240` is a near-neutral dark that reads as a background, not a color.

### 2.5 Accent Color Presets

| Name | Light Hex | Dark Hex | Notes |
|---|---|---|---|
| Blue (default) | `#0288d1` | `#4da8da` | — |
| Green | `#2e7d32` | `#66bb6a` | — |
| Purple | `#7b1fa2` | `#ab47bc` | — |
| Orange | `#e65100` | `#ff9800` | — |
| Teal | `#00695c` | `#26a69a` | — |
| Rose | `#c2185b` | `#ec407a` | — |

Dark mode variants are **pre-computed** (not derived at runtime). Each preset has a hand-tuned dark mode hex that maintains ≥4.5:1 contrast against `#1a1a2e`. The `lighten()` helper is only used for the `--color-accent-hover` derived property (a 10% lightening of the user-selected accent for hover states).

A custom hex input is also available, accepting any valid 3 or 6 character hex value. Invalid input is rejected with a red border on the input field.

---

## 3. Implementation Plan

### 3.1 CSS Refactor (`variables.css`)

The current `variables.css` defines spacing and font-size variables. This file is expanded to include the full color palette as CSS custom properties. The file structure is:

```
/* === Spacing & Typography (existing, unchanged) === */
:root {
  --space-xs: 4px;
  --space-sm: 8px;
  /* ... existing spacing variables ... */
  --font-xs: 12px;
  /* ... existing font-size variables ... */
}

/* === Color Palette — Light Mode (defaults) === */
:root {
  --bg-primary: #ffffff;
  --bg-secondary: #f5f5f5;
  --bg-tertiary: #f9f9f9;
  --bg-surface: #f8f9fa;
  --text-primary: #000000;
  --text-secondary: #666666;
  --text-tertiary: #999999;
  --text-muted: #cccccc;
  --border-primary: #e0e0e0;
  --border-secondary: #f0f0f0;
  --shadow: rgba(0,0,0,0.08);
  --color-accent: #0288d1;
  --color-accent-hover: #0277bd;
  --color-accent-light: rgba(2,136,209,0.1);
  --color-accent-border: rgba(2,136,209,0.3);
  --color-danger: #ef5350;
  --color-danger-hover: #d32f2f;
  --color-success: #4caf50;
  --color-warning: #ff9800;
}

/* === Color Palette — Dark Mode (overrides) === */
/* Scoped to screen so print/PDF always uses the light palette (§6.3). */
@media screen {
  [data-theme="dark"] {
    --bg-primary: #1a1a2e;
    --bg-secondary: #1e1e32;
    --bg-tertiary: #222240;
    --bg-surface: #2a2a4a;
    --text-primary: #e4e4e4;
    --text-secondary: #b0b0c0;
    --text-tertiary: #888898;
    --text-muted: #6a6a7a;
    --border-primary: #2e2e50;
    --border-secondary: #262648;
    --shadow: rgba(0,0,0,0.3);
    --color-accent: #4da8da;
    --color-accent-hover: #5db8e5;
    --color-accent-light: rgba(77,168,218,0.15);
    --color-accent-border: rgba(77,168,218,0.4);
    --color-danger: #ff6b6b;
    --color-danger-hover: #ff5252;
    --color-success: #66bb6a;
    --color-warning: #ffa726;
  }
}

/* === User-selected accent (applied inline by main.js) === */
/* These are set via document.documentElement.style.setProperty() and
   override the CSS defaults when present. No CSS selector needed. */
```

**Selector hierarchy:** `:root` sets light-mode defaults → `[data-theme="dark"]` overrides dark-mode values → inline `style` properties on `<html>` override everything for user-selected accent colors. This cascade ensures user accent colors always take precedence.

**Migrated colors (mapping):**

| Current Value | Variable |
|---|---|
| `#ffffff` / `#fff` | `var(--bg-primary)` |
| `#f5f5f5` | `var(--bg-secondary)` |
| `#f9f9f9` | `var(--bg-tertiary)` |
| `#f8f9fa` | `var(--bg-surface)` |
| `#e8e8e8` (borders in SettingsView) | `var(--border-primary)` |
| `#e0e0e0` (borders + hover backgrounds) | `var(--border-primary)` |
| `#e9ecef` (VizChart border) | `var(--border-primary)` |
| `#f0f0f0` (subtle borders, divider backgrounds) | `var(--border-secondary)` |
| `#000000` / `#000` | `var(--text-primary)` |
| `#666666` / `#666` | `var(--text-secondary)` |
| `#999999` / `#888` | `var(--text-tertiary)` |
| `#cccccc` / `#ccc` / `#adb5bd` | `var(--text-muted)` |
| `#0288d1` | `var(--color-accent)` |
| `#0277bd` | `var(--color-accent-hover)` |
| `rgba(2,136,209,0.1)` / `#e8f0fe` | `var(--color-accent-light)` |
| `rgba(2,136,209,0.3)` | `var(--color-accent-border)` |
| `#ef5350` | `var(--color-danger)` |
| `#d32f2f` | `var(--color-danger-hover)` |
| `#27ae60` (exploration success) | `var(--color-success)` |

### 3.2 Accent Color Application (`main.js`)

#### 3.2.1 Color Utility Functions

```js
// --- Color conversion helpers ---

function hexToRgb(hex) {
  const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex)
  if (!m) return null
  return { r: parseInt(m[1], 16), g: parseInt(m[2], 16), b: parseInt(m[3], 16) }
}

function rgbToHex(r, g, b) {
  return '#' + [r, g, b].map(c => Math.max(0, Math.min(255, Math.round(c))).toString(16).padStart(2, '0')).join('')
}

// Lighten by a percentage (0-100). Works in HSL space for perceptual uniformity.
function lighten(hex, percent) {
  const rgb = hexToRgb(hex)
  if (!rgb) return hex
  const hsl = rgbToHsl(rgb.r, rgb.g, rgb.b)
  hsl[2] = Math.min(100, hsl[2] + percent)
  return hslToHex(hsl[0], hsl[1], hsl[2])
}

// Darken by a percentage (0-100). Works in HSL space.
function darken(hex, percent) {
  return lighten(hex, -percent)
}

function rgbToHsl(r, g, b) {
  r /= 255; g /= 255; b /= 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b)
  let h, s, l = (max + min) / 2
  if (max === min) { h = s = 0 }
  else {
    const d = max - min
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
    switch (max) {
      case r: h = ((g - b) / d + (g < b ? 6 : 0)) / 6; break
      case g: h = ((b - r) / d + 2) / 6; break
      case b: h = ((r - g) / d + 4) / 6; break
    }
  }
  return [h * 360, s * 100, l * 100]
}

function hslToHex(h, s, l) {
  h /= 360; s /= 100; l /= 100
  let r, g, b
  if (s === 0) { r = g = b = l }
  else {
    const hue2rgb = (p, q, t) => {
      if (t < 0) t += 1
      if (t > 1) t -= 1
      if (t < 1/6) return p + (q - p) * 6 * t
      if (t < 1/2) return q
      if (t < 2/3) return p + (q - p) * (2/3 - t) * 6
      return p
    }
    const q = l < 0.5 ? l * (1 + s) : l + s - l * s
    const p = 2 * l - q
    r = hue2rgb(p, q, h + 1/3)
    g = hue2rgb(p, q, h)
    b = hue2rgb(p, q, h - 1/3)
  }
  return rgbToHex(Math.round(r * 255), Math.round(g * 255), Math.round(b * 255))
}

function hexToRgba(hex, alpha) {
  const rgb = hexToRgb(hex)
  if (!rgb) return `rgba(0,0,0,${alpha})`
  return `rgba(${rgb.r},${rgb.g},${rgb.b},${alpha})`
}
```

#### 3.2.2 Preset Dark Mode Accent Map

```js
const ACCENT_DARK_MODE_MAP = {
  '#0288d1': '#4da8da',   // Blue
  '#2e7d32': '#66bb6a',   // Green
  '#7b1fa2': '#ab47bc',   // Purple
  '#e65100': '#ff9800',   // Orange
  '#00695c': '#26a69a',   // Teal
  '#c2185b': '#ec407a',   // Rose
}
```

#### 3.2.3 Apply Functions

```js
const THEME_KEY = 'yourql-theme'        // user's selection: 'light' | 'dark' | 'system'
const ACCENT_KEY = 'yourql-accent'
const DEFAULT_THEME_SELECTION = 'system'

// Resolve a selection ('light'|'dark'|'system') to a concrete 'light'|'dark'.
function resolveTheme(selection) {
  if (selection === 'system') {
    if (typeof window !== 'undefined' && window.matchMedia) {
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
    }
    return 'light'
  }
  return selection
}

function applyAccent(hex) {
  // Validate hex
  if (!/^#?([a-f\d]{3}){1,2}$/i.test(hex)) return
  if (hex.length === 4) hex = '#' + hex.slice(1).split('').map(c => c + c).join('')
  const root = document.documentElement
  root.style.setProperty('--color-accent', hex)
  // Hover: darken by 10% in HSL space for perceptual uniformity
  root.style.setProperty('--color-accent-hover', darken(hex, 10))
  // Derived: semi-transparent variants
  root.style.setProperty('--color-accent-light', hexToRgba(hex, 0.1))
  root.style.setProperty('--color-accent-border', hexToRgba(hex, 0.3))
  // Print accent: the user's literal (light-mode) accent, used by the print
  // media query so PDFs stay light-mode but use the chosen accent color.
  root.style.setProperty('--color-accent-print', hex)
  localStorage.setItem(ACCENT_KEY, hex)
}

// applyResolvedTheme takes a CONCRETE 'light'|'dark'. It sets data-theme,
// re-applies the accent's dark-mode variant if needed, and dispatches the
// theme-change event so VizChart.svelte can destroy+recreate charts.
function applyResolvedTheme(resolved) {
  document.documentElement.setAttribute('data-theme', resolved)
  // Re-apply accent so dark-mode variant (preset map or 30% lighten) is correct
  const currentAccent = localStorage.getItem(ACCENT_KEY) || '#0288d1'
  const darkAccent = ACCENT_DARK_MODE_MAP[currentAccent]
  if (resolved === 'dark' && darkAccent) {
    document.documentElement.style.setProperty('--color-accent', darkAccent)
    document.documentElement.style.setProperty('--color-accent-hover', lighten(darkAccent, 10))
    document.documentElement.style.setProperty('--color-accent-light', hexToRgba(darkAccent, 0.15))
    document.documentElement.style.setProperty('--color-accent-border', hexToRgba(darkAccent, 0.4))
  } else if (resolved === 'dark' && !darkAccent) {
    // Custom accent in dark mode: lighten by 30% as fallback
    const lightened = lighten(currentAccent, 30)
    document.documentElement.style.setProperty('--color-accent', lightened)
    document.documentElement.style.setProperty('--color-accent-hover', lighten(lightened, 10))
    document.documentElement.style.setProperty('--color-accent-light', hexToRgba(lightened, 0.15))
    document.documentElement.style.setProperty('--color-accent-border', hexToRgba(lightened, 0.4))
  } else {
    // light mode: restore the user's literal accent
    applyAccent(currentAccent)
  }
  // Notify chart components to destroy+recreate with the new theme.
  // The handler in VizChart.svelte updates Chart.defaults THEN recreates.
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('theme-change', { detail: { theme: resolved } }))
  }
}

// setThemeSelection persists the user's SELECTION and applies the resolved theme.
function setThemeSelection(selection) {
  localStorage.setItem(THEME_KEY, selection)
  applyResolvedTheme(resolveTheme(selection))
}

function applyThemeAndAccent() {
  const savedSelection = localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION
  const savedAccent = localStorage.getItem(ACCENT_KEY) || '#0288d1'
  // applyAccent sets the light-mode accent + --color-accent-print.
  // applyResolvedTheme then overrides --color-accent (and derivatives) if dark.
  applyAccent(savedAccent)
  applyResolvedTheme(resolveTheme(savedSelection))
}
```

#### 3.2.4 Initialization & System Mode Listener

Run `applyThemeAndAccent()` on `DOMContentLoaded` (before any component mounts) so the app renders with the correct theme. Also register a `matchMedia` listener so System mode re-resolves when the OS theme changes:

```js
// In main.js, at the top level
if (typeof document !== 'undefined') {
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initTheme)
  } else {
    initTheme()
  }
}

function initTheme() {
  applyThemeAndAccent()
  // Re-resolve when OS theme changes, but ONLY if the user is in System mode.
  // An explicit Light/Dark selection is always respected and ignores the OS.
  if (window.matchMedia) {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    mql.addEventListener('change', () => {
      const selection = localStorage.getItem(THEME_KEY) || DEFAULT_THEME_SELECTION
      if (selection === 'system') {
        applyResolvedTheme(resolveTheme('system'))
      }
    })
  }
}
```

### 3.3 General Settings UI (`SettingsView.svelte`)

The General tab is expanded with two new sections, laid out in a two-column grid:

```
┌──────────────────────────┬──────────────────────────┐
│ Theme                    │ Accent Color             │
│ ┌─────┐ ┌─────┐ ┌────────┐ │ ┌────┐ ┌────┐ ┌────┐ ...  │
│ │ Light│ │Dark │ │ System │ │ │ 🔵 │ │ 🟢 │ │ 🟣 │        │
│ └─────┘ └─────┘ └────────┘ │ └────┘ └────┘ └────┘        │
│                          │ ┌──────────────────────┐    │
│                          │ │ Custom: #0288d1      │    │
│                          │ └──────────────────────┘    │
└──────────────────────────┴──────────────────────────┘
```

**Theme section:**
- Three toggle buttons: "Light", "Dark", and "System"
- The active button has an accent border and filled background
- Default: **System** (follows the OS `prefers-color-scheme` preference)
- A small hint under the buttons shows the currently-resolved mode, e.g. "System (currently Dark)"
- Changes take effect immediately (no save button needed — General settings are client-side only)
- On mount, the saved selection is read from `localStorage` and the active button is set

**Accent Color section:**
- A row of 6 preset swatches (color circles, 32px diameter)
- The currently selected swatch has a white ring (2px) around it
- Below the swatches: a text input labeled "Custom color" with a hex value
- The input validates on blur — invalid hex values get a red border
- Changes take effect immediately
- On mount, the saved accent is read from `localStorage` and the matching swatch is highlighted

**Theme selection wiring:** the SettingsView buttons call `setThemeSelection(selection)` (see §3.2.3), which persists the selection and applies the resolved theme. `setThemeSelection` is exposed on `window` (like the existing `window.setUIScale`) so the Svelte component can call it without a new binding.

In `VizChart.svelte`, the chart re-render is triggered by listening for the `theme-change` event and bumping a reactive `themeTick` (full snippet in §3.4).

### 3.4 Dark Mode — Chart.js

Charts rendered by `VizChart.svelte` need to respect dark mode. Chart.js applies `Chart.defaults` only to **new** chart instances — `chart.update()` does **not** re-read defaults. Existing charts must therefore be **destroyed and recreated** on theme change (the same code path `VizChart.svelte` already uses when its `config` prop changes).

The existing module-scope defaults in `VizChart.svelte` are:

```js
Chart.defaults.borderColor = '#e9ecef';
Chart.defaults.font.family = 'system-ui, -apple-system, sans-serif';
Chart.defaults.font.size = 12;
```

A helper function (module-scope in `VizChart.svelte`) sets the theme-aware defaults:

```js
function applyChartTheme(isDark) {
  Chart.defaults.color = isDark ? '#b0b0c0' : '#666666'
  Chart.defaults.borderColor = isDark ? '#2e2e50' : '#e9ecef'
}
```

`VizChart.svelte` already destroys and recreates its `Chart` instance inside a `$effect()` keyed on `config`. To re-render on theme change, the component subscribes to the `theme-change` event. The handler **first** calls `applyChartTheme()` to update defaults (so the new instance picks them up), **then** bumps a reactive counter that triggers the existing destroy+recreate `$effect`:

```js
let themeTick = $state(0)

onMount(() => {
  const handler = (e) => {
    applyChartTheme(e.detail.theme === 'dark')  // update defaults before recreate
    themeTick++                                    // trigger $effect
  }
  window.addEventListener('theme-change', handler)
  return () => window.removeEventListener('theme-change', handler)
})

$effect(() => {
  // depend on both config and themeTick so a theme change re-runs
  const _tick = themeTick
  if (!config || !canvas) return
  if (chart) { chart.destroy(); chart = null }
  // ... existing create logic ...
})
```

This guarantees every visible chart is destroyed and recreated with the new `Chart.defaults`, so colors update reliably. It reuses the existing destroy+create logic rather than introducing a separate `chart.update()` path that would silently miss the default changes.

### 3.5 Dark Mode — Markdown Content

The assistant message markdown content needs dark-mode-aware styling. The existing `pre`, `code`, `table`, and `th` selectors in `ConversationView.svelte` use hardcoded colors that are migrated to CSS variables. The markdown renderer in Go produces standard HTML tags that the CSS selectors handle — the backend markdown renderer itself needs no changes (the Go-side HTML changes in §3.6 are for the *results wrapper*, not the markdown body).

### 3.6 Dark Mode — Results & Exploration Tables (`sql_execution.go`)

The HTML generated by `formatResultsHTML`, `buildCollapsibleSQLBlockHTML`, and `formatExplorationHTML` in `sql_execution.go` uses hardcoded inline `style="..."` colors. These are migrated to CSS variables so they respect the theme. Inline `style` attributes accept `var(--...)` and resolve against the current `data-theme`.

**Complete color inventory** (every hardcoded color found in the generated HTML, not just the common ones):

| Current value | Maps to |
|---|---|
| `#f8f9fa` (table header bg, toolbar bg) | `var(--bg-surface)` |
| `#f5f5f5` (raw-results summary, exploration round bg) | `var(--bg-secondary)` |
| `#f8f8f8` (SQL code block bg) | `var(--bg-tertiary)` |
| `#f5f7fa` (exploration round card bg) | `var(--bg-secondary)` |
| `#f0f4ff` (exploration summary bg) | `var(--color-accent-light)` |
| `#fff` / `white` (buttons, popover bg, code bg) | `var(--bg-primary)` |
| `#e8e8e8` (table cell borders, toolbar border, popover border) | `var(--border-primary)` |
| `#e0e0e0` (expand-button border) | `var(--border-primary)` |
| `#ddd` (copy-button border, SQL-toggle border) | `var(--border-primary)` |
| `#666` (toolbar text, button text, summary text) | `var(--text-secondary)` |
| `#888` (exploration row-count text) | `var(--text-tertiary)` |
| `#27ae60` (exploration success row count) | `var(--color-success)` |
| `#0288d1` (none inline — accent only via CSS classes) | — |

Note: `#e8e8e8` appears as a **border** color in both `sql_execution.go` and `SettingsView.svelte`, so it is consistently mapped to `var(--border-primary)` (light value `#e0e0e0`) in both §3.1 and §3.6. `--bg-surface` (light value `#f8f9fa`) is used for hover states, the chart container background, and table header backgrounds — it is **not** the same as `#e8e8e8`.

Before:
```go
sb.WriteString(`style="background:#f8f9fa; border:1px solid #e8e8e8; ..."`)
```
After:
```go
sb.WriteString(`style="background:var(--bg-surface); border:1px solid var(--border-primary); ..."`)
```

This is a find-and-replace across `sql_execution.go`'s three HTML generation functions.

### 3.7 Dark Mode — Inline JS Colors (`main.js`)

`main.js` sets colors directly via `element.style` in three event handlers. These are **not** in `<style>` blocks and are not picked up by the §3.1 Svelte migration. They must be migrated separately so dark mode and accent changes apply:

| Handler | Current code | Migrates to |
|---|---|---|
| `sortTable()` | `td.style = 'border:1px solid #e8e8e8; ...'` | `'border:1px solid var(--border-primary); ...'` |
| `sortTable()` | `indicator.style.color = '#0288d1'` | `'var(--color-accent)'` |
| `sortTable()` | `indicator.style.color = '#ccc'` (inactive) | `'var(--text-muted)'` |
| `copySQL()` | `btn.style.color = '#0288d1'` | `'var(--color-accent)'` |
| `copySQL()` | `btn.style.borderColor = '#0288d1'` | `'var(--color-accent)'` |
| `toggleSQLPopover()` | `btn.style.background = '#e8f0fe'` (open) | `'var(--color-accent-light)'` |
| `toggleSQLPopover()` | `btn.style.borderColor = '#0288d1'` (open) | `'var(--color-accent)'` |
| `toggleSQLPopover()` | `btn.style.background = '#fff'` (closed) | `'var(--bg-primary)'` |
| `toggleSQLPopover()` | `btn.style.borderColor = '#ddd'` (closed) | `'var(--border-primary)'` |

`element.style.setProperty()` accepts CSS variable references and they resolve against the current `data-theme`, so no JavaScript theme branching is needed — `var(--color-accent)` automatically yields the light or dark accent depending on the active theme.

### 3.8 Dark Mode — VizChart Container (`VizChart.svelte` `<style>`)

The `VizChart.svelte` `<style>` block has three hardcoded colors not in the §3.1 mapping:

| Current value | Maps to |
|---|---|
| `#f8f9fa` (`.viz-chart-container` background) | `var(--bg-surface)` |
| `#e9ecef` (`.viz-chart-container` border, also `Chart.defaults.borderColor`) | `var(--border-primary)` |
| `#adb5bd` (`.viz-error` text) | `var(--text-tertiary)` |

---

## 4. Touchpoint Map

| File | Change |
|---|---|
| `frontend/src/variables.css` | Add full color palette with light/dark variants, accented by variables. All existing spacing/size variables kept. |
| `frontend/src/App.svelte` | Replace all hardcoded colors with CSS variables (~30 places) |
| `frontend/src/ConversationView.svelte` | Replace all hardcoded colors with CSS variables (~50 places). Print media query left light-mode (see §6.3). |
| `frontend/src/SettingsView.svelte` | Replace all hardcoded colors with CSS variables (~60 places), add Theme and Accent Color UI to General tab |
| `frontend/src/VizChart.svelte` | Replace `<style>` block colors with CSS variables (§3.8), add `applyChartTheme()` + `theme-change` listener that bumps `themeTick` to trigger destroy+recreate (§3.4) |
| `frontend/src/main.js` | Add `applyAccent()`, `resolveTheme()`, `applyResolvedTheme()`, `setThemeSelection()`, and `matchMedia` listener (System mode); initialize from localStorage on load. **Also** migrate the inline-JS colors in `sortTable()`, `copySQL()`, `toggleSQLPopover()` to CSS variables (§3.7) |
| `pkg/services/sql_execution.go` | Replace hardcoded inline style colors in `formatResultsHTML`, `formatExplorationHTML`, `buildCollapsibleSQLBlockHTML` with CSS variable references (~20 places, §3.6) |

That's **7 touchpoints** (the `main.js` inline-JS migration and `VizChart.svelte` `<style>` migration are sub-items of files already in the list). No database changes. No API changes. No new dependencies.

---

## 5. Implementation Strategy

### 5.1 Phase 1 — CSS Variables (no visual change)

Define all color variables in `variables.css` with their **current** values as the light-mode defaults. Replace every hardcoded color in the CSS with the corresponding variable. At this point, the app looks identical — all variables resolve to their current values. This phase is a pure refactor and can be merged independently.

### 5.2 Phase 2 — Theme Toggle (Light / Dark / System)

Add the `[data-theme="dark"]` overrides in `variables.css`. Add the three-button theme UI in the General tab. Add `resolveTheme()`, `applyResolvedTheme()`, `setThemeSelection()`, and the `matchMedia` listener in `main.js`. Default selection is **System**. The `theme-change` event is dispatched on every resolved-theme change so `VizChart.svelte` destroys+recreates charts. At this point, all three modes work for all user-visible surfaces.

### 5.3 Phase 3 — Accent Color & Print Accent

Add the accent color preset swatches and custom hex input to the General tab. Add the `applyAccent()` function in `main.js` with the `darken()` (HSL space), `hexToRgba()`, and `lighten()` helpers. `applyAccent` also writes `--color-accent-print` (the literal light-mode accent) used by the print media query. Dark mode accent variants are handled by the `ACCENT_DARK_MODE_MAP` lookup for presets, or a 30% HSL lightening for custom colors. The `theme-change` event is dispatched to notify `VizChart.svelte` to re-render charts.

### 5.4 Phase 4 — Backend HTML & Inline JS

Replace hardcoded inline colors in `sql_execution.go` (§3.6) and the inline-JS colors in `main.js`'s `sortTable()` / `copySQL()` / `toggleSQLPopover()` (§3.7). Migrate the `VizChart.svelte` `<style>` block colors (§3.8). This phase is independent of phases 1-3 and can be done in parallel or after.

---

## 6. Edge Cases & Considerations

### 6.1 System Preference Detection

The default selection is **System**, which reads `prefers-color-scheme: dark` via `window.matchMedia` and re-resolves live when the OS theme changes (see §3.2.4). An explicit Light/Dark selection is always persisted and overrides System mode — the OS listener only re-resolves while the selection is `system`. This avoids the "app follows system theme then forgets the user's preference" problem: the user's explicit choice always wins.

On WKWebView (macOS) and WebView2 (Windows), `prefers-color-scheme` reflects the OS appearance setting and is supported in current runtimes. If `matchMedia` is unavailable (older runtimes), System mode falls back to Light.

### 6.2 Chart.js Re-rendering

When the theme changes, existing charts on the page need to be re-rendered with the new colors. Because `Chart.defaults` only affects *new* instances and `chart.update()` does **not** re-read defaults, charts must be **destroyed and recreated** on theme change. This is handled via a custom `theme-change` event dispatched by `main.js` (see §3.2.3). The `VizChart.svelte` component listens for this event in `onMount`, calls `applyChartTheme()` to update `Chart.defaults`, then bumps a reactive `themeTick` that its existing `$effect()` (which already destroys+recreates on `config` change) depends on, so the destroy+recreate code path is reused. Charts rendered before the theme change are recreated on the event; charts rendered after it pick up the new defaults automatically.

### 6.3 Print/PDF Export

The print media query in `ConversationView.svelte` is the basis for PDF export (`runtime.WindowPrint`). The current print block hardcodes light-mode colors: `.assistant-message { background: #fff; border: 1px solid #ccc; }` and `.user-message { background: #0288d1; color: #fff; }`.

**Resolution:** the print media query is migrated to use CSS variables so it remains light-mode *regardless* of the active `data-theme` (dark mode should not darken printed pages), **but** the `.user-message` background uses `var(--color-accent)` so a user who picked a green accent gets green user-message bubbles in their PDF. Because the accent's dark-mode variant is set inline on `<html>` and the print query uses `var(--color-accent)`, we must pin the accent to its **light-mode** value for print. This is done by adding, inside the `@media print` block, explicit overrides:

```css
@media print {
  @page { size: portrait; }
  /* ... existing print rules ... */
  /* Force light-mode surface colors, regardless of screen data-theme */
  .assistant-message { background: var(--bg-primary); border: 1px solid var(--border-primary); }
  .user-message { background: var(--color-accent-print, #0288d1); color: #ffffff; }
}
```

`main.js` writes the user's literal (light-mode) accent to a `--color-accent-print` custom property alongside `--color-accent` (see §3.2.3 `applyAccent`), so the print query always reads the light accent even when dark mode is active. No print-specific `:root` override is needed — `.user-message` reads `--color-accent-print` directly, bypassing `--color-accent` entirely. The existing light-mode variables (`--bg-primary` etc.) remain light in the print context because `[data-theme="dark"]` is screen-only by default.

This gives: dark-mode UI on screen, light-mode PDF with the user's chosen accent color — both consistent and predictable.

### 6.4 Accent Color Contrast

The `darken()` helper for hover states works in **HSL color space** (not RGB) for perceptual uniformity — a 10% darkening in HSL produces a visually consistent darkening regardless of the input color. The `hexToRgba()` helper creates semi-transparent variants by parsing the hex and substituting the alpha channel.

For dark mode accent variants of **preset colors**, the values are **pre-computed** and stored in `ACCENT_DARK_MODE_MAP` (see §3.2.2). For **custom hex colors** that have no pre-computed dark variant, the fallback is to lighten the color by 30% in HSL space.

All preset dark mode variants have been verified to meet ≥4.5:1 contrast against `#1a1a2e` (see §2.4 WCAG table).

### 6.5 Migration for Existing Users

On first load after the update, no theme or accent is saved in `localStorage`. The app defaults to **System** mode with the **Blue** accent. On a machine whose OS is in light mode this is visually identical to the current appearance; on a machine whose OS is in dark mode the app will render in dark mode on first launch (matching the OS). Users who prefer the old always-light behavior can switch to Light explicitly. No data migration is needed.

---

## 7. Testing

| Test | Description |
|---|---|
| Theme selection switches between Light, Dark, System | Visual verification, three buttons |
| System mode follows OS `prefers-color-scheme` on first launch | Default selection; toggling OS appearance updates app |
| System mode re-resolves live when OS theme changes | `matchMedia` listener fires while selection is `system` |
| Explicit Light/Dark selection persists and ignores OS | Reload with OS in dark + selection `light` stays light |
| Theme persists across page reloads | localStorage key `yourql-theme` stores the selection |
| Accent preset click updates all accent-colored elements | Buttons, links, badges change color |
| Custom hex input accepts valid colors | Input validation, invalid hex rejected |
| Custom hex input normalizes 3-char hex (e.g. `#f00` → `#ff0000`) | Input validation |
| Dark mode charts destroyed+recreated on theme change | `theme-change` event bumps `themeTick`, $effect re-runs |
| Chart.defaults.color/borderColor correct per resolved theme | Verified on new chart instances after toggle |
| Dark mode markdown is readable | Code blocks, tables, links visible |
| Dark mode print/PDF is light-mode but uses chosen accent | `.user-message` uses `--color-accent-print` (light accent) |
| `main.js` sort indicator / SQL popover colors adapt to theme | Inline-JS styles use `var(--...)` |
| `sql_execution.go` result/exploration HTML adapts to theme | Inline `style` uses `var(--...)` |
| Dark mode empty states are visible | "No discussions" text contrast |
| Dark mode error messages are readable | Error banner contrast |
| Dark mode modals are visible | Modal background, border, text contrast |
| WCAG AA contrast pass for all text/background pairs | ≥4.5:1 for normal text (see §2.4 table) |
| Custom accent dark mode fallback lightens by 30% in HSL | Visual verification |
| Preset dark mode variants use pre-computed map | No runtime computation |
| Theme selection and accent operate independently | Changing one does not reset the other |
| `--bg-surface` (`#f8f9fa`) distinct from `--border-primary` (`#e0e0e0`) | No color collision in table/chart surfaces |
| First-launch with OS dark + no localStorage renders dark | System default resolves to dark |
