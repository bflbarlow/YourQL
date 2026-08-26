> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# DEFAULT_DISC_SETTINGS.md — Project Plan

> **Status:** Planned  
> **Version target:** v0.4.0  
> **Created:** 2026-08-04

## 1. Overview

Add a **"Defaults"** tab to the Settings view where users configure the default
values applied to every new discussion. Currently, creating a discussion requires
manually selecting an LLM provider and data source every time, and all other
settings start at their system defaults. This feature pre-fills those values so
users spend less time configuring and more time asking questions.

### User Story

> As a YourQL user, I want to set my preferred defaults once in Settings so that
> every new discussion starts with those defaults already applied, without me
> having to configure them each time.

---

## 2. Defaultable Settings

The following conversation-level settings will be defaultable:

| Setting | Type | Current system default | Rationale |
|---|---|---|---|
| Default LLM provider | Provider ID (nullable) | `null` (must pick each time) | Most users have one primary model |
| Default data source | Data source ID (nullable) | `null` (must pick each time) | Most users have one primary database |
| Max context messages | Integer | `10` | Users with large context windows may want more |
| Max messages | Integer | `0` (unlimited) | Control conversation history length |
| Summarize results | Boolean | `false` | Power users may always want summaries |
| Data visualization | Boolean | `true` | Some users always want charts |
| Show tech details | Boolean | `false` | Developers may want SQL visible by default |
| Show context details | Boolean | `false` | Debugging workflow |
| Streaming enabled | Boolean | `false` | Performance preference |

### Settings explicitly excluded

- **Pinned** — Not a meaningful default (every discussion would start pinned).
- **Title** — Already user-provided in the "New Discussion" modal.
- **Exploration safety** — Stored per data source, not per conversation.
  Future enhancement: add exploration safety to data source defaults.
- **Skills** — Skills are activated per-conversation from a library. A
  "default active skills" feature is a future enhancement.

---

## 3. Storage Design

### 3.1 Table: `discussion_defaults`

A simple key-value table, same pattern as `app_settings`:

```sql
CREATE TABLE IF NOT EXISTS discussion_defaults (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
)
```

This avoids schema changes to the `conversations` table. Each setting is stored
as a separate row with a well-known key.

### 3.2 Keys

| Key | Value format | Example |
|---|---|---|
| `llm_provider_id` | Integer as string, or empty | `"1"` or `""` |
| `data_source_id` | Integer as string, or empty | `"3"` or `""` |
| `max_context_messages` | Integer as string | `"20"` |
| `max_messages` | Integer as string | `"0"` |
| `summarize` | `"true"` or `"false"` | `"true"` |
| `viz_enabled` | `"true"` or `"false"` | `"true"` |
| `tech_details` | `"true"` or `"false"` | `"false"` |
| `context_details` | `"true"` or `"false"` | `"false"` |
| `streaming_enabled` | `"true"` or `"false"` | `"false"` |

### 3.3 Migration

```go
// In models/database.go — add after existing migrations:
_ = runMigration("create_discussion_defaults", func() error {
    _, err := DB.Exec(`CREATE TABLE IF NOT EXISTS discussion_defaults (
        key TEXT PRIMARY KEY,
        value TEXT NOT NULL
    )`)
    return err
})
```

- **Additive only** — new table, no changes to existing tables.
- **`runMigration`** ensures it runs exactly once and is tracked in
  `schema_migrations`.
- **No data migration needed** — empty table means "use system defaults"
  which matches existing behavior exactly. Users gradually populate defaults
  through the Settings UI.

---

## 4. Go Backend

### 4.1 Model: `pkg/models/discussion_default.go`

```go
package models

// DiscussionDefault represents a single default setting for new discussions.
type DiscussionDefault struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}

// DiscussionDefaults holds all defaultable discussion settings.
type DiscussionDefaults struct {
    LLMProviderID      *uint  `json:"llm_provider_id,omitempty"`
    DataSourceID       *uint  `json:"data_source_id,omitempty"`
    MaxContextMessages *int   `json:"max_context_messages,omitempty"`
    MaxMessages        *int   `json:"max_messages,omitempty"`
    Summarize          *bool  `json:"summarize,omitempty"`
    VizEnabled         *bool  `json:"viz_enabled,omitempty"`
    TechDetails        *bool  `json:"tech_details,omitempty"`
    ContextDetails     *bool  `json:"context_details,omitempty"`
    StreamingEnabled   *bool  `json:"streaming_enabled,omitempty"`
}
```

Using pointers so `nil` means "no default set — use system default."

### 4.2 Service: `pkg/services/discussion_defaults.go`

```go
package services

import (
    "fmt"
    "strconv"

    "YourQL/pkg/models"
)

// GetDiscussionDefaults returns the user's configured defaults, or
// nil pointers for any setting that hasn't been configured.
func GetDiscussionDefaults() (*models.DiscussionDefaults, error) {
    rows, err := models.DB.Query("SELECT key, value FROM discussion_defaults")
    if err != nil {
        return nil, fmt.Errorf("failed to query discussion defaults: %w", err)
    }
    defer rows.Close()

    d := &models.DiscussionDefaults{}
    raw := make(map[string]string)
    for rows.Next() {
        var k, v string
        if err := rows.Scan(&k, &v); err != nil {
            continue
        }
        raw[k] = v
    }

    if v, ok := raw["llm_provider_id"]; ok && v != "" {
        id, err := strconv.ParseUint(v, 10, 64)
        if err == nil {
            uid := uint(id)
            d.LLMProviderID = &uid
        }
    }
    if v, ok := raw["data_source_id"]; ok && v != "" {
        id, err := strconv.ParseUint(v, 10, 64)
        if err == nil {
            uid := uint(id)
            d.DataSourceID = &uid
        }
    }
    if v, ok := raw["max_context_messages"]; ok && v != "" {
        n, err := strconv.Atoi(v)
        if err == nil {
            d.MaxContextMessages = &n
        }
    }
    if v, ok := raw["max_messages"]; ok && v != "" {
        n, err := strconv.Atoi(v)
        if err == nil {
            d.MaxMessages = &n
        }
    }
    if v, ok := raw["summarize"]; ok {
        b := v == "true"
        d.Summarize = &b
    }
    if v, ok := raw["viz_enabled"]; ok {
        b := v == "true"
        d.VizEnabled = &b
    }
    if v, ok := raw["tech_details"]; ok {
        b := v == "true"
        d.TechDetails = &b
    }
    if v, ok := raw["context_details"]; ok {
        b := v == "true"
        d.ContextDetails = &b
    }
    if v, ok := raw["streaming_enabled"]; ok {
        b := v == "true"
        d.StreamingEnabled = &b
    }

    return d, nil
}

// SetDiscussionDefault upserts a single default setting. Setting an empty
// value removes the default (reverts to system default).
func SetDiscussionDefault(key, value string) error {
    if value == "" {
        _, err := models.DB.Exec("DELETE FROM discussion_defaults WHERE key = ?", key)
        return err
    }
    _, err := models.DB.Exec(
        "INSERT INTO discussion_defaults (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
        key, value,
    )
    if err != nil {
        return fmt.Errorf("failed to set discussion default %s: %w", key, err)
    }
    return nil
}
```

### 4.3 Wails Bindings: `app.go`

**Placement:** add these directly after the existing `GetGeneralSettings` /
`UpdateGeneralSettings` pair (`app.go` ~line 708–738), since they follow the
same persisted-settings pattern and JSON snake_case convention already
established there (verified: `GeneralSettings` uses `json:"app_name"` etc.).

Two new methods on `*App`:

```go
// GetDiscussionDefaults returns the user's configured defaults for new discussions.
func (a *App) GetDiscussionDefaults() (*models.DiscussionDefaults, error) {
    return services.GetDiscussionDefaults()
}

// UpdateDiscussionDefaults persists a batch of default settings.
// Only non-nil fields are updated. Pass nil for fields you don't want to change.
func (a *App) UpdateDiscussionDefaults(defaults models.DiscussionDefaults) error {
    if defaults.LLMProviderID != nil {
        if err := services.SetDiscussionDefault("llm_provider_id",
            fmt.Sprintf("%d", *defaults.LLMProviderID)); err != nil {
            return err
        }
    }
    if defaults.DataSourceID != nil {
        if err := services.SetDiscussionDefault("data_source_id",
            fmt.Sprintf("%d", *defaults.DataSourceID)); err != nil {
            return err
        }
    }
    if defaults.MaxContextMessages != nil {
        if err := services.SetDiscussionDefault("max_context_messages",
            fmt.Sprintf("%d", *defaults.MaxContextMessages)); err != nil {
            return err
        }
    }
    if defaults.MaxMessages != nil {
        if err := services.SetDiscussionDefault("max_messages",
            fmt.Sprintf("%d", *defaults.MaxMessages)); err != nil {
            return err
        }
    }
    if defaults.Summarize != nil {
        if err := services.SetDiscussionDefault("summarize",
            fmt.Sprintf("%t", *defaults.Summarize)); err != nil {
            return err
        }
    }
    if defaults.VizEnabled != nil {
        if err := services.SetDiscussionDefault("viz_enabled",
            fmt.Sprintf("%t", *defaults.VizEnabled)); err != nil {
            return err
        }
    }
    if defaults.TechDetails != nil {
        if err := services.SetDiscussionDefault("tech_details",
            fmt.Sprintf("%t", *defaults.TechDetails)); err != nil {
            return err
        }
    }
    if defaults.ContextDetails != nil {
        if err := services.SetDiscussionDefault("context_details",
            fmt.Sprintf("%t", *defaults.ContextDetails)); err != nil {
            return err
        }
    }
    if defaults.StreamingEnabled != nil {
        if err := services.SetDiscussionDefault("streaming_enabled",
            fmt.Sprintf("%t", *defaults.StreamingEnabled)); err != nil {
            return err
        }
    }
    return nil
}
```

---

## 5. Applying Defaults to New Conversations

### 5.1 Go Backend — Apply Defaults in `app.go` (No Service Signature Changes)

**Recommended (verified against current code):** Keep `services.CreateConversation`'s
signature **unchanged** — it is only called from one place
(`app.go:65`, `func (a *App) CreateConversation(title string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error)`),
so there is no risk in leaving it as-is. Apply the remaining defaults with a
create-then-update sequence in `app.go`, reusing the **existing** per-field
update functions already in `pkg/services/conversation.go` — no new service
functions are needed for this step:

| Field | Existing service function (verified present) |
|---|---|
| `max_messages` | `UpdateConversationMaxMessages(id uint, maxMessages int) error` |
| `max_context_messages` | `UpdateConversationMaxContextMessages(id uint, maxContextMessages int) error` |
| `summarize` | `UpdateConversationSummarize(id uint, summarize bool) error` |
| `viz_enabled` | `UpdateConversationVizEnabled(id uint, vizEnabled bool) error` |
| `tech_details` | `UpdateConversationTechDetails(id uint, showTechDetails bool) error` |
| `context_details` | `UpdateConversationContextDetails(id uint, showContextDetails bool) error` |
| `streaming_enabled` | `UpdateConversationStreamingEnabled(id uint, enabled bool) error` |

```go
// app.go — stays a thin delegator, per AGENT_READ_FIRST.md §2.1
// ("Keep methods focused and small ... delegate to a service function")
func (a *App) CreateConversation(title string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error) {
    return services.CreateConversationWithDefaults(title, llmProviderID, dbConnectionID)
}
```

```go
// pkg/services/conversation.go — new function; the branching logic for
// resolving + applying defaults lives here, not in app.go.
func CreateConversationWithDefaults(title string, llmProviderID, dataSourceID *uint) (*models.Conversation, error) {
    defaults, _ := GetDiscussionDefaults() // nil-safe: err ignored, defaults may be nil

    if llmProviderID == nil && defaults != nil {
        llmProviderID = defaults.LLMProviderID
    }
    if dataSourceID == nil && defaults != nil {
        dataSourceID = defaults.DataSourceID
    }

    conv, err := CreateConversation(title, llmProviderID, dataSourceID)
    if err != nil {
        return nil, err
    }

    if defaults != nil {
        if defaults.MaxMessages != nil {
            _ = UpdateConversationMaxMessages(conv.ID, *defaults.MaxMessages)
        }
        if defaults.MaxContextMessages != nil {
            _ = UpdateConversationMaxContextMessages(conv.ID, *defaults.MaxContextMessages)
        }
        if defaults.Summarize != nil {
            _ = UpdateConversationSummarize(conv.ID, *defaults.Summarize)
        }
        if defaults.VizEnabled != nil {
            _ = UpdateConversationVizEnabled(conv.ID, *defaults.VizEnabled)
        }
        if defaults.TechDetails != nil {
            _ = UpdateConversationTechDetails(conv.ID, *defaults.TechDetails)
        }
        if defaults.ContextDetails != nil {
            _ = UpdateConversationContextDetails(conv.ID, *defaults.ContextDetails)
        }
        if defaults.StreamingEnabled != nil {
            _ = UpdateConversationStreamingEnabled(conv.ID, *defaults.StreamingEnabled)
        }
    }

    // Re-fetch so the returned struct reflects all applied defaults
    return GetConversationByID(conv.ID)
}
```

**Conformance note (AGENT_READ_FIRST.md §2.1):** the original draft of this
plan put the defaults-resolution branching directly in the `app.go` binding.
That violates the stated best practice — every existing binding in `app.go`
is a 1–3 line delegator; this logic belongs in the service layer. Moved to
`CreateConversationWithDefaults` in `pkg/services/conversation.go` instead.

**Why create-then-update instead of a single INSERT:** `services.CreateConversation`
is a small, single-purpose function used elsewhere in tests/other call sites
as well as here. Extending its signature to take 7 extra parameters would be
a wider, riskier change for no real benefit — the per-field update functions
already exist, are already tested by the conversation-settings UI, and running
them once at creation time costs a few extra fast local SQLite writes. This
keeps the diff small and avoids touching a function signature other callers
might rely on.

**Error handling note:** the `_ = services.UpdateConversation...` calls above
intentionally ignore errors (consistent with "defaults are a convenience, not
a correctness requirement" — see §8). If every one of these fails the
conversation still exists with system defaults, which is safe degradation, not
a broken state. This should be called out explicitly during implementation so
it isn't mistaken for an oversight.

---

## 6. Frontend

### 6.1 SettingsView — New "Defaults" Tab

**Verified current tabs** (`SettingsView.svelte`, `activeSettingsTab` values):
`'models'`, `'databases'`, `'skills'`, `'general'` (the last one currently holds
Theme/Accent/UI Scale — there is no separate "Appearance" tab today). Add a
new tab value, e.g. `'defaults'`, inserted before `'general'`:

```
Settings
├── Model Configurations   (activeSettingsTab = 'models')
├── Data Sources           (activeSettingsTab = 'databases')
├── Skills                 (activeSettingsTab = 'skills')
├── Defaults               (activeSettingsTab = 'defaults')  ← NEW
└── General (theme/accent/scale) (activeSettingsTab = 'general')
```

The **Defaults** tab contains:

```
┌─────────────────────────────────────────────────┐
│ Defaults for New Discussions                     │
├─────────────────────────────────────────────────┤
│                                                   │
│ Default LLM Provider                              │
│ [Dropdown: all providers + "None (ask each time)"]│
│                                                   │
│ Default Data Source                               │
│ [Dropdown: all data sources + "None (ask each time)"]│
│                                                   │
│ ── Conversation Behavior ──                       │
│                                                   │
│ Messages in Context    [  20  ]                   │
│ Total Messages         [   0  ] (0 = unlimited)   │
│                                                   │
│ ── Toggles ──                                     │
│                                                   │
│ [✓] Summarize results by default                  │
│ [✓] Enable charts by default                      │
│ [ ] Show SQL/tech details by default              │
│ [ ] Show prompt context by default                │
│ [ ] Enable streaming by default                   │
│                                                   │
│ [Reset to system defaults]                        │
└─────────────────────────────────────────────────┘
```

### 6.2 Frontend State & Wires

```js
// In SettingsView.svelte
let defaultsForm = $state({
    llm_provider_id: null,
    data_source_id: null,
    max_context_messages: 10,
    max_messages: 0,
    summarize: false,
    viz_enabled: true,
    tech_details: false,
    context_details: false,
    streaming_enabled: false,
})

// Load defaults on tab mount
async function loadDefaults() {
    const d = await GetDiscussionDefaults()
    if (d) {
        defaultsForm.llm_provider_id = d.llm_provider_id ?? null
        defaultsForm.data_source_id = d.data_source_id ?? null
        defaultsForm.max_context_messages = d.max_context_messages ?? 10
        defaultsForm.max_messages = d.max_messages ?? 0
        defaultsForm.summarize = d.summarize ?? false
        defaultsForm.viz_enabled = d.viz_enabled ?? true
        defaultsForm.tech_details = d.tech_details ?? false
        defaultsForm.context_details = d.context_details ?? false
        defaultsForm.streaming_enabled = d.streaming_enabled ?? false
    }
}

// Save on any change (debounced)
async function saveDefaults() {
    await UpdateDiscussionDefaults(defaultsForm)
}
```

### 6.3 "New Discussion" Modal — Updated

When the user opens the "New Discussion" modal, the LLM Provider and Data
Source dropdowns are pre-selected with the user's defaults (if set). The
modal label changes to reflect this:

```
LLM Provider
[OpenAI (default)]  ← pre-selected from defaults

Data Source
[None selected]     ← user hasn't set a default
```

If both defaults are set, the user can click "Create" immediately without
touching the dropdowns. The modal should also indicate which selections
came from defaults (subtle tag or label).

---

## 7. Implementation Phases

### Phase 1: Backend (1–2 hours)

| Task | File(s) |
|---|---|
| Add `discussion_defaults` table migration | `pkg/models/database.go` |
| Create `DiscussionDefaults` model | `pkg/models/discussion_default.go` |
| Create `discussion_defaults.go` service | `pkg/services/discussion_defaults.go` |
| Add Wails bindings `GetDiscussionDefaults` / `UpdateDiscussionDefaults` | `app.go` |
| Add `CreateConversationWithDefaults` service function; update `app.go`'s `CreateConversation` binding to a 1-line delegator | `pkg/services/conversation.go` + `app.go` |
| Regenerate Wails bindings | `wails generate module` |

### Phase 2: Frontend — Defaults Tab (1–2 hours)

| Task | File(s) |
|---|---|
| Add "Defaults" to settings tab navigation | `SettingsView.svelte` |
| Build the defaults form UI | `SettingsView.svelte` |
| Wire load/save to Wails bindings | `SettingsView.svelte` |
| Pre-fill "New Discussion" modal with defaults | `App.svelte` |

### Phase 3: Polish & Test (30 min)

| Task |
|---|
| Verify defaults survive app restart |
| Verify defaults don't override explicit selections |
| Test with a fresh database (no defaults set) — should behave identically to current behavior |
| Test with all defaults set, then create a discussion without touching modal dropdowns |
| Dark mode verification |

---

## 8. Edge Cases & Behavior

| Scenario | Expected behavior |
|---|---|
| No defaults configured | Identical to current behavior — modal requires manual selection |
| Default LLM provider is deleted | Setting remains but resolves to `null` on create; user picks manually |
| Default data source is deleted | Same as above |
| User explicitly picks a different provider in the modal | Explicit choice overrides the default |
| User clears a default (sets to "None") | Row deleted from `discussion_defaults`; reverts to system default |
| "Reset to system defaults" clicked | All rows deleted from `discussion_defaults`; form resets |

---

## 9. Migration Safety

- **New table only** — no changes to `conversations`, `llm_providers`, `data_sources`, or any existing table.
- **`runMigration` ensures one-time execution** — tracked in `schema_migrations`.
- **Backward compatible** — existing conversations are completely unaffected.
  The `CreateConversation` Wails binding signature does not change (defaults are
  loaded internally).
- **Go build** — new files only, no signature changes to exported functions
  (except `CreateConversation` which gains internal logic but keeps the same
  Wails binding signature).

---

## 10. Future Enhancements (Out of Scope for v0.4.0)

- **Per-data-source defaults** — Different defaults based on which data source
  is selected (e.g., always enable streaming for BigQuery).
- **Default active skills** — Pre-enable certain skills on every new discussion.
- **Template discussions** — Create a discussion from a template with
  pre-configured settings, system prompt, and starter messages.
- **Import/export defaults** — Share default configurations across machines.