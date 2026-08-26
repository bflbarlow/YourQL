# F-17 · Frontend componentization, type-checking, and test tooling

**Severity:** Medium · **Effort:** Large (incremental) · **Risk categories:** appeal/comfort (secondary), regression, maintainability

## Problem Statement

Measured 2026-08-24 (`wc -l frontend/src/*.svelte`):

| File | Lines |
|---|---|
| `SettingsView.svelte` | **3,795** |
| `App.svelte` | 2,696 |
| `ConversationView.svelte` | 1,310 |
| `MinimalistView.svelte` | 476 |

Plus systemic gaps: no `svelte-check`, no ESLint/Prettier, no test runner
(`frontend/package.json` has only dev/build/preview scripts). Consequences:

1. Every settings tweak edits a near-4k-line file — high collision and
   regression risk for exactly the "comfort" features the charter makes
   first-class deliverables.
2. No static analysis means Svelte 5 runes misuse, a11y issues, and
   undefined-variable bugs surface only at runtime in `wails dev`.
3. Dark-mode regressions (§3.6 requires testing both themes) have no
   automated guard even at the lint level (e.g., hardcoded colors).
4. Nothing prevents JS-side secret mishandling symmetric to charter §3.5's
   Go rules.

## Solution Design

### Phase A — Tooling foundation (do first; zero behavior change)

```bash
npm i -D svelte-check typescript prettier eslint eslint-plugin-svelte \
        eslint-config-prettier vitest @testing-library/svelte jsdom
```

- `npm run check` → `svelte-check --tsconfig ./tsconfig.json` (start with
  `--fail-on-warnings false`; ratchet up as errors are fixed).
- Prettier config (single quotes per existing style); format-once commit.
- ESLint flat config with `eslint-plugin-svelte`; add rule banning literal
  hex/rgb colors outside `variables.css` (custom rule via
  `no-restricted-syntax` on CSS? simpler: stylelint with
  `declaration-property-value-disallowed-list`) → theme-drift guard.
- Vitest + Testing Library wired with `jsdom`; add `"test": "vitest run"`.
- Wire all three into CI's frontend job (F-3).

Note: codebase is `.js` Svelte (`vite-env.d.ts`, `main.js`). Adopting TS is
optional; recommend *gradual* — enable `checkJs: true` in jsconfig first
(type info without renames), migrate files opportunistically later. Do not
big-bang convert.

### Phase B — Component extraction (incremental, one section per PR)

Target structure:

```
frontend/src/lib/
  components/
    settings/
      ProviderCard.svelte        ProviderForm.svelte
      DataSourceCard.svelte      DataSourceForm.svelte
      SkillEditor.svelte         AgentLoopConfigPanel.svelte
      AppearanceSection.svelte   TimeoutSettings.svelte
    chat/
      MessageBubble.svelte       ExplorationTrace.svelte
      ResultsToolbar.svelte      ChartBlock.svelte (wrap VizChart)
      TechDetails.svelte
  stores/
    conversation.js              // extracted reactive state from App.svelte
```

Extraction protocol per component:
1. Identify a self-contained section + its exact props/callbacks.
2. Move markup+script verbatim; wire props/events.
3. Visual smoke both themes; CI green; PR.

Start with leaf sections of SettingsView (provider card, data-source form,
skill editor) — highest line-count yield, lowest entanglement. Extracting
Svelte 5 snippets where parents pass templated content avoids prop drilling.

### Phase C — First tests

Highest-value pure-logic tests once extracted:
- limit/threshold UI calculations mirroring backend defaults,
- markdown/HTML sanitization boundaries if any client rendering exists,
- store reducers (conversation list filter/sort/search logic currently
  inline in App.svelte),
- one smoke render test per major view (mounts without errors).

## Implementation Plan

1. Phase A in one day: tooling configs, format-once, fix top svelte-check
   errors, CI wiring.
2. Phase B: cadence of one component per session; SettingsView target
   <800 lines within ~10 extractions.
3. Phase C grows alongside B; every extracted component gets ≥1 test.

## Risk Assessment

- **Behavioral drift during moves** → verbatim-move protocol + visual smoke;
  Svelte compiler catches most wiring mistakes.
- **Tooling noise blocking work** → warnings-not-errors start; strictness
  ratchets after cleanup.
- Charter: purely improves #4/#5 with #1–#3 untouched; §4.3 case 3.
