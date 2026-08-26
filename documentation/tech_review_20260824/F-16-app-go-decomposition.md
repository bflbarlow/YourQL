# F-16 · Decompose `app.go` into domain-scoped binding groups

**Severity:** Medium · **Effort:** Large (incremental) · **Risk categories:** regression, security reviewability, maintainability

## Problem Statement

`app.go` is 1,271 lines declaring **86 exported methods** on `*App`. Every
one is auto-bound into the frontend bridge. Consequences:

1. **Security auditability** — answering "what can the webview invoke?"
   requires reading one giant file with mixed concerns (conversations,
   credentials CRUD, updater triggers, exports, settings). A malicious-
   dependency incident or XSS-in-webview scenario is only as bounded as
   this surface, and nobody can review it at a glance.
2. **Charter drift** — §2.1 advises methods stay "focused and small… >50
   lines → delegate to a service." Aggregate size makes violations hard to
   spot; several methods embed logic that belongs in services.
3. **Merge friction** — every feature touches the same file.
4. **Testing** — methods on the monolith are unreachable without Wails
   runtime context; grouped services would be unit-testable (feeds F-4).

## Solution Design

Split by embedding sub-API structs — zero change to what Wails binds
(embedded exported structs' methods bind identically), zero change to JS
call signatures.

### Target layout

```
app.go                  — App struct assembly, startup/shutdown, shared ctx
app_conversations.go    — conversation/message bindings
app_datasources.go      — data-source CRUD, connection testing, switcher entry
app_providers.go        — LLM provider CRUD/testing
app_settings.go         — app settings, discussion defaults, agent-loop config
export_api.go           — HTML/Markdown/PDF/total export bindings
update_api.go           — CheckForUpdate / DownloadUpdate / PerformUpgradeRestart
skills_api.go           — skills CRUD
```

```go
type App struct {
    *conversationAPI
    *dataSourceAPI
    *providerAPI
    *settingsAPI
    ...
}
```

Each sub-API struct holds whatever shared state it needs (ctx, db path) via
constructor wiring in `startup`.

### Binding-surface audit (do during split)

While touching every method, tag each with one of:
- `keep` — legit frontend need
- `internal-only` — called by other Go code but bound unnecessarily →
  unexport it (shrinks attack surface for free)
- `move-to-service` — body >50 lines or contains business logic → extract
  per §2.1

Expected yield: a documented, minimal bridge inventory written into
`AGENT_READ_FIRST.md` §2.1 ("these N methods are the entire Go↔JS
surface").

### Sequencing (critical — no big-bang)

One domain per PR, mechanical move first (no signature/logic edits), then a
follow-up cleanup PR per domain if needed. Order by churn: settings →
providers → conversations → datasources → export → update → skills.

## Implementation Plan

1. Spike: move `settingsAPI` (~10 methods); run `wails dev`; click through
   settings UI; verify bindings JSON identical (`wails generate:module` diff
   empty except file layout).
2. Repeat per domain; after each, CI green + manual smoke of that UI area.
3. Produce the bridge inventory table; append to charter §2.1.
4. Unexport `internal-only` methods discovered (verify no frontend caller
   first: grep `window['go']\|Go\.main\.App\.` patterns in frontend/src).

## Risk Assessment

- **Binding breakage** — embedded promotion changes nothing about method
  names/receivers visible to Wails, but verify with generated-bindings diff
  each step (step 1's check makes regressions impossible to miss).
- **Effort risk:** large total; fully incremental and shippable per PR —
  value accrues from the first merged domain.
- Charter §4.2: touches app.go broadly → RISK_ANALYSIS_LOG entry once, at
  series start, referencing this doc.
