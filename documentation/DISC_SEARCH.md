# DISC_SEARCH.md — Discussion Tags & Search

> **Date:** 2026-08-10
> **Project:** YourQL — Tag, search, and filter the discussion list
> **Status:** Design specification. Implementation pending.

---

## 1. Motivation

Today the Discussions view is a flat, chronological list sorted by pinned status
and `updated_at`. Every conversation the user has ever created sits in one long
scrollable column with no way to find a specific thread except visual scanning.
For users with more than a dozen conversations — or who revisit old threads
across different databases — this is unusable.

Users want to:

- **Find a specific discussion** by name, data source, or model — without
  scrolling through the entire list.
- **Group related discussions** — e.g., all threads about the `sales` database,
  or all threads using Claude, or all threads about a particular analytics
  project.
- **Reduce visual noise** — temporarily hide everything that doesn't match the
  current task.

Tags and search are the two mechanisms that solve this. Tags are a user-owned
labeling system (free-form, lightweight, no predefined ontology). Search is
instant, in-memory, client-side filtering across title, tags, data source name,
and model name — no network round-trip.

---

## 2. Design

### 2.1 Tags — Free-form, per-conversation labels

Tags are **short text strings** (1–50 characters, no commas or pipes since those
are display delimiters) that the user attaches to a conversation. A conversation
can have zero or more tags. Tags are:

- **Created implicitly** — typing a new tag name in the conversation settings
  creates it. No separate "manage tags" page.
- **Deleted implicitly** — the last conversation using a tag keeps it alive;
  when the last usage is removed, the tag row is cleaned up.
- **Case-insensitive but case-preserved** — `"Sales"` and `"sales"` are the
  same tag, stored as `"Sales"` (the first spelling wins; existing tag is reused
  rather than creating a duplicate).
- **Reusable across conversations** — tagging one conversation `"audit"` doesn't
  create a private tag; it's available as a suggestion for every other
  conversation.

Tag management lives **in the gear popover** (the conversation settings panel
that already appears when you click the ⚙️ button on a conversation row). The
existing popover has sections for LLM provider, data source, and toggles — tags
get a new section at the bottom.

#### Tag input UX

```
Tags
┌─────────────────────────────────────────┐
│ [sales ] [audit ] [quarterly] [×]       │  ← existing tags shown as chips
│                                         │
│ Type a tag and press Enter…  [▼]        │  ← text input with autocomplete
└─────────────────────────────────────────┘
```

- **Chips:** Each existing tag renders as a chip. Clicking the `×` on a chip
  removes the tag from this conversation (does not delete the tag itself).
- **Autocomplete:** As the user types, the dropdown shows matching tags from
  other conversations. Clicking a suggestion adds it. Pressing Enter with a new
  value creates and adds a new tag.
- **Tag display on conversation rows:** Tags appear as small colored chips in
  the conversation list row, between the date and the model/db badges.

### 2.2 Search & Filter Bar

A filter bar sits at the top of the Discussions view, above the conversation
list. It operates **entirely client-side** — all conversations are fetched once
(`ListConversations`), and filtering is done in JavaScript against the already-
loaded data. This means filtering is instant and never hits the database.

```
┌──────────────────────────────────────────────────────────────────┐
│ [🔍 Search discussions…                     ]  [Clear filters]   │
│                                                                  │
│ Quick filters:  All  [Sales DB ▼]  [Claude ▼]  [quarterly]  …  │
└──────────────────────────────────────────────────────────────────┘
```

The search input matches against:

| Field | Example match |
|---|---|
| Conversation title | `title.toLowerCase().includes(query.toLowerCase())` |
| Tag names | Any tag on the conversation matches the query |
| Data source name | Resolved from `dataSourceNameByID` (already on the frontend) |
| Model/LLM provider name | Resolved from `llmNameByID` (already on the frontend) |

All four fields are searched simultaneously — typing `"sales"` finds a
conversation titled "Sales pipeline analysis" AND any conversation tagged
`"sales"` AND conversations connected to a data source named "Sales DB".

**Quick-filter chips** below the search bar let the user filter by a single
data source or a single model with one click. These are dropdowns populated
from the same `dataSources` / `llmProviders` arrays already loaded on the
frontend. Selecting "Sales DB" from the dropdown adds a removable chip:
`[Sales DB ×]` — clicking `×` removes it, restoring the full list.

**Combining filters:** The search query and the quick-filter chips are AND-ed
together. If the user types "revenue" AND selects `[Sales DB ×]`, only
conversations matching both appear. This is standard faceted-search behavior
and requires no special UI — the derived list simply applies both predicates.

**Clear filters** restores the unfiltered list with one click. It also appears
automatically when the user clears the search box and has no active quick-filter
chips.

### 2.3 Tag display on conversation rows

When a conversation has tags, the row in the discussion list shows them as
small colored chips between the date and the model/db badges:

```
┌─────────────────────────────────────────────────────────────┐
│ Sales pipeline analysis                          ⚙          │
│ Aug 5   [sales] [quarterly]   Claude   Sales DB             │
└─────────────────────────────────────────────────────────────┘
```

Tags use a muted background color from the existing accent palette — distinct
from the primary accent used for buttons, so they don't scream for attention
but are visually identifiable. The tag chip is not clickable from the list row
(it's informational only) — tag management lives in the gear popover.

### 2.4 Empty state

When filters are active and no conversations match:

> No discussions match your filters. [Clear filters]

The "Clear filters" link resets both search and quick-filter chips.

### 2.5 Pinning — Always-on-top conversations

Pinning lets the user pin conversations to the top of the list where they
stay regardless of active filters or sort order. The pin control already
exists in the gear popover (⚙️ → checkbox "Pin to top of list") and the
backend already sorts by `pinned DESC, updated_at DESC`.

**Interaction with filters:** Pinned conversations always appear first,
above the filtered results, with a subtle horizontal divider between them
and the regular filtered list. A pinned conversation that doesn't match the
active search/filter still appears at the top — pinning always overrides
filtering. This lets the user keep a reference conversation visible while
searching for something else.

**Visual indicator:** Pinned conversations show a small 📌 pin icon before
the title, so they're recognizable at a glance even when the list also
contains tag chips.

---

## 3. Data Model

### 3.1 New tables

**Additive migration only** — no existing columns or tables are modified.

```sql
-- Tags table — lightweight label catalog
CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Join table — many-to-many between conversations and tags
CREATE TABLE IF NOT EXISTS conversation_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE,
    UNIQUE(conversation_id, tag_id)
);
```

`COLLATE NOCASE` on `tags.name` ensures `"Sales"` and `"sales"` are treated as
the same tag by the UNIQUE constraint — the database enforces case-insensitive
uniqueness, not application code.

`ON DELETE CASCADE` on both foreign keys means deleting a conversation cleans
up its tag associations automatically, and deleting a tag cleans up all usages
(though tag deletion is handled implicitly by cleanup — see §3.2).

### 3.2 Tag lifecycle

| Action | What happens |
|---|---|
| User adds a tag to a conversation | `INSERT OR IGNORE INTO tags (name)`, then `INSERT INTO conversation_tags` |
| User removes a tag from a conversation | `DELETE FROM conversation_tags WHERE conversation_id = ? AND tag_id = ?` |
| Cleanup (after any tag removal) | `DELETE FROM tags WHERE id NOT IN (SELECT DISTINCT tag_id FROM conversation_tags)` — removes orphaned tags |
| Conversation is deleted | Cascade deletes all `conversation_tags` rows. The tag itself stays in `tags` as long as another conversation uses it. Cleanup runs on the next tag operation. |

### 3.3 Model changes

`Conversation` model adds one field:

```go
type Conversation struct {
    // ... existing fields ...
    Tags []string `json:"tags,omitempty"`  // resolved tag names, not IDs
}
```

`Tags` is populated by a JOIN when listing conversations. It's not a column on
`conversations` — it's derived from the join table — so the existing
`SELECT id, title, ...` query in `ListConversationsByUser()` needs to be
extended with a subquery or LEFT JOIN. `GetConversationByID()` should also
include tags, since the gear popover needs them when it opens.

The `CreateConversation` and `UpdateConversation` functions are unchanged —
tags are managed through separate service functions (`AddTagToConversation`,
`RemoveTagFromConversation`, `ListTags`).

---

## 4. Backend

### 4.1 New service: `pkg/services/tags.go`

```go
// AddTagToConversation adds a tag to a conversation. If the tag doesn't exist
// yet, it is created. Tag names are trimmed and truncated to 50 characters.
func AddTagToConversation(conversationID uint, tagName string) error

// RemoveTagFromConversation removes a tag from a conversation. If the tag is
// no longer used by any conversation, it is deleted (orphan cleanup).
func RemoveTagFromConversation(conversationID uint, tagName string) error

// GetTagsForConversation returns all tag names for a conversation.
func GetTagsForConversation(conversationID uint) ([]string, error)

// ListAllTags returns every tag name in the database, sorted alphabetically.
// Used for autocomplete suggestions in the tag input.
func ListAllTags() ([]string, error)

// ListConversationsWithTags returns all conversations with their tags
// pre-resolved. Replaces ListConversationsByUser for the main list endpoint.
func ListConversationsWithTags() ([]*models.Conversation, error)
```

`ListConversationsWithTags` replaces the existing `ListConversationsByUser()`.
It extends the query to include tags via a GROUP_CONCAT subquery:

```sql
SELECT c.id, c.title, ..., GROUP_CONCAT(t.name, '||') as tag_names
FROM conversations c
LEFT JOIN conversation_tags ct ON ct.conversation_id = c.id
LEFT JOIN tags t ON t.id = ct.tag_id
WHERE c.status != 'deleted'
GROUP BY c.id
ORDER BY c.pinned DESC, c.updated_at DESC
```

The `'||'` delimiter is safe because pipes are forbidden in tag names (validated
on input). Tags are split client-side or in Go with `strings.Split`.

### 4.2 Wails bindings: `app.go`

```go
// Tags
func (a *App) AddTagToConversation(conversationID uint, tagName string) error
func (a *App) RemoveTagFromConversation(conversationID uint, tagName string) error
func (a *App) GetTagsForConversation(conversationID uint) ([]string, error)
func (a *App) ListAllTags() ([]string, error)
func (a *App) ListConversationsWithTags() ([]*models.Conversation, error)
```

### 4.3 Migration

Add to `pkg/models/database.go`'s `migrate()` function — this is a pure
`CREATE TABLE IF NOT EXISTS`, no existing data is touched:

```go
_ = runMigration("create_tags", func() error {
    _, err := DB.Exec(`CREATE TABLE IF NOT EXISTS tags (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL UNIQUE COLLATE NOCASE,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
    if err != nil {
        return err
    }
    _, err = DB.Exec(`CREATE TABLE IF NOT EXISTS conversation_tags (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        conversation_id INTEGER NOT NULL,
        tag_id INTEGER NOT NULL,
        FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
        FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE,
        UNIQUE(conversation_id, tag_id)
    )`)
    return err
})
```

---

## 5. Frontend

### 5.1 Files to modify

| File | Changes |
|---|---|
| `frontend/src/App.svelte` | Add filter bar above the conversation list; add tag chips to conversation rows; pass tags to the gear popover; update `loadData` to call `ListConversationsWithTags` |
| `frontend/src/App.svelte` (gear popover) | Add tag management section: chip display, text input with autocomplete, add/remove logic |
| `frontend/src/App.svelte` (imports) | Add `AddTagToConversation`, `RemoveTagFromConversation`, `GetTagsForConversation`, `ListAllTags`, `ListConversationsWithTags` |
| `frontend/src/App.svelte` (CSS) | Styles for filter bar, search input, quick-filter chips, tag chips in rows and popover |

### 5.2 Filter bar markup

The filter bar sits inside the `{:if activeView === 'discussions'}` block,
between the view header and the conversation list:

```svelte
<div class="discussion-filters">
  <div class="search-row">
    <input
      type="text"
      class="search-input"
      placeholder="Search discussions by title, tag, source, or model…"
      bind:value={searchQuery}
    />
    {#if searchQuery || activeFilters.length > 0}
      <button class="btn btn-tiny" onclick={clearFilters}>Clear filters</button>
    {/if}
  </div>
  <div class="quick-filters">
    <select bind:value={filterDataSource} onchange={applyDataSourceFilter}>
      <option value="">All data sources</option>
      {#each dataSources as ds}
        <option value={ds.id}>{ds.name}</option>
      {/each}
    </select>
    <select bind:value={filterLLM} onchange={applyLLMFilter}>
      <option value="">All models</option>
      {#each llmProviders as p}
        <option value={p.id}>{p.name}</option>
      {/each}
    </select>
  </div>
</div>
```

The search is a derived state — `filteredConversations = $derived(...)` that
applies `searchQuery`, `filterDataSource`, and `filterLLM` to the full
`conversations` array. The `{#each}` in the list iterates over
`filteredConversations` instead of `conversations` directly.

### 5.3 Tag chips in conversation rows

Inside the `{#each conversations as conv}` loop, add tag chips between the
date and the model/db badges:

```svelte
<div class="conversation-meta">
  <span class="conversation-date">{...}</span>
  {#if conv.pinned}
    <span class="pin-indicator" title="Pinned">📌</span>
  {/if}
  {#if conv.tags && conv.tags.length > 0}
    {#each conv.tags as tag}
      <span class="tag-chip tag-chip-row">{tag}</span>
    {/each}
  {/if}
  {#if conv.llm_provider_id}
    <span class="conversation-model">...</span>
  {/if}
  ...
</div>
```

### 5.4 Tag management in gear popover

Add a new section at the bottom of the gear popover (after the existing toggles):

```svelte
<div class="gear-popover-section">
  <label>Tags</label>
  <div class="tag-chips">
    {#each convTags as tag}
      <span class="tag-chip">
        {tag}
        <button class="tag-remove" onclick={() => handleRemoveTag(tag)}>×</button>
      </span>
    {/each}
  </div>
  <div class="tag-input-wrapper">
    <input
      type="text"
      class="tag-input"
      placeholder="Add a tag…"
      bind:value={tagInput}
      onkeydown={handleTagKeydown}
    />
    {#if tagSuggestions.length > 0}
      <div class="tag-suggestions">
        {#each tagSuggestions as suggestion}
          <button class="tag-suggestion" onclick={() => handleAddTag(suggestion)}>
            {suggestion}
          </button>
        {/each}
      </div>
    {/if}
  </div>
</div>
```

**States:**
- `convTags` — array of tag names for the open conversation, loaded when the
  gear popover opens (`GetTagsForConversation`).
- `tagInput` — text in the input field.
- `tagSuggestions` — derived: `allTags.filter(t => t.toLowerCase().includes(tagInput.toLowerCase()) && !convTags.includes(t))`, where `allTags` is loaded once via `ListAllTags()`.
- `handleTagKeydown` — on Enter: if `tagInput` is a match in `tagSuggestions`,
  add that. Otherwise, if `tagInput` is non-empty and ≤50 chars and contains no
  commas or pipes, create a new tag (via `AddTagToConversation`). After adding,
  clear the input.
- `handleAddTag(name)` — calls `AddTagToConversation(conv.id, name)`, refreshes
  `convTags`.
- `handleRemoveTag(name)` — calls `RemoveTagFromConversation(conv.id, name)`,
  refreshes `convTags`.

---

## 6. UX Design

### 6.1 Discussion list with active filters

```
┌──────────────────────────────────────────────────────────────┐
│ Discussions                                                  │
│                                                              │
│ ┌──────────────────────────────────────────────────────┐     │
│ │ 🔍 revenue                                         ✕ │     │
│ │ [All data sources ▼]  [Sales DB ▼]  [All models ▼]  │     │
│ └──────────────────────────────────────────────────────┘     │
│                                                              │
│ ┌──────────────────────────────────────────────────────┐     │
│ │ 📌Revenue by region Q3                       ⚙        │     │
│ │ Aug 5   [revenue] [q3]   Claude   Sales DB         │     │
│ └──────────────────────────────────────────────────────┘     │
│ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─│     │
│ ┌──────────────────────────────────────────────────────┐     │
│ │ Customer churn analysis                     ⚙        │     │
│ │ Jul 28   [churn]   OpenAI   Marketing DB            │     │
│ └──────────────────────────────────────────────────────┘     │
│   ↑ This row appears because "Marketing DB" doesn't         │
│     match the "Sales DB" filter. The pinned row above it     │
│     always shows regardless.                                 │
└──────────────────────────────────────────────────────────────┘
```

### 6.2 Tag input in gear popover

```
Tags
┌─────────────────────────────────────────┐
│ [sales ×] [quarterly ×] [revenue ×]     │
│                                         │
│ quar▌                                   │  ← user typed "quar"
│ ┌─────────────────────────┐             │
│ │ quarterly               │             │  ← autocomplete suggestion
│ └─────────────────────────┘             │
└─────────────────────────────────────────┘
```

---

## 7. Implementation Plan

| Phase | What | Effort |
|---|---|---|
| 1 | **Migration:** Add `tags` and `conversation_tags` tables to `database.go` | Tiny |
| 2 | **Models:** Add `Tags []string` to `Conversation` struct | Tiny |
| 3 | **Service:** `pkg/services/tags.go` — `ListConversationsWithTags`, `AddTagToConversation`, `RemoveTagFromConversation`, `ListAllTags`, `GetTagsForConversation` | Small |
| 4 | **Wails bindings:** Five new methods on `*App` in `app.go` — `AddTagToConversation`, `RemoveTagFromConversation`, `GetTagsForConversation`, `ListAllTags`, `ListConversationsWithTags` | Small |
| 5 | **Frontend — filter bar:** Search input, quick-filter dropdowns, derived `filteredConversations` (pin-aware: pinned always on top regardless of filter), empty state | Small |
| 6 | **Frontend — tag chips on rows:** Render tag chips + pin indicator (📌) in the conversation list row | Tiny |
| 7 | **Frontend — tag management:** Tag input with autocomplete in the gear popover, add/remove logic | Small |
| 8 | **Testing:** Create conversations with tags, verify search across all four fields, verify filter combinations, verify pin+filter interaction, verify tag lifecycle (add, remove, orphan cleanup) | Small |

Total effort: **1–2 work sessions.** All phases are small and additive. The
frontend work is the largest piece (Phases 5–7), but the logic is all
client-side derived state — no complex backend queries beyond the tag join.

---

## 8. Risk Assessment

Per `AGENT_READ_FIRST.md` §4.

| Priority | Impact |
|---|---|
| **#1 Correctness** | **None.** This is purely organizational — no change to the LLM pipeline, SQL execution, or answer delivery. |
| **#2 Safety** | **None.** Tags are stored in `yourql.db` (app database, fully mutable). No data source is touched. No credentials are involved. |
| **#3 Reliability** | **Low risk.** Client-side filtering means there's no new failure mode in the query path. If tag resolution fails (e.g., JOIN returns NULL), conversations still render — they just don't show tag chips. |
| **#4 Appeal / Comfort** | **Improved.** Organization and findability directly address the primary user complaint about the discussion list. |

### Failure modes

1. **Tag name collision with pipe delimiter.** Mitigation: validate on input —
   reject commas and pipes. The `||` delimiter in `GROUP_CONCAT` is only
   vulnerable if a tag name contains `||`, which is impossible since pipes are
   rejected. No SQL injection risk — tag names are parameterized through `?`
   placeholders.

2. **Orphan tag cleanup fails silently.** If the cleanup query errors (unlikely
   for a simple DELETE with subquery on SQLite), orphaned tags accumulate. No
   data is lost — the tags table just has unused rows. Mitigation: log the error
   and move on. A future maintenance pass can add a "vacuum tags" button.

3. **Large number of tags makes autocomplete slow.** Mitigation: `ListAllTags`
   returns a flat list of strings — even 1,000 tags is negligible for a
   client-side `.filter()`. The autocomplete only filters the already-loaded
   array. No network calls on every keystroke.

### Decision

**Proceed.** Zero risk to the fundamental goal pipeline. Additive schema
migration only. Client-side filtering means no new backend complexity. Clear
user value for the most common UX complaint about the discussion list.

---

## 9. Charter Compliance

Per `AGENT_READ_FIRST.md`:

- [ ] **Is this change aligned with the fundamental goal?** Indirectly — better
      organization helps users find and resume previous analyses faster. Does
      not affect answer accuracy or delivery.
- [ ] **Read-only invariant preserved?** Yes — no data source interaction.
- [ ] **SQL execution safety preserved?** Yes — no changes to SQL execution.
- [ ] **Migration safety preserved?** Yes — two new `CREATE TABLE IF NOT EXISTS`
      statements, no existing columns touched.
- [ ] **API keys/passwords not logged?** Yes — tag names are user-generated
      labels with no relationship to credentials.
- [ ] **Existing conversations backward compatible?** Yes — existing
      conversations just have an empty `Tags` array. Nothing breaks.
- [ ] **UI responsive?** All filtering is client-side derived state — instant.
- [ ] **Dark/light mode?** Tag chips and the filter bar use existing CSS
      variable tokens (border colors, text colors, accent palette). Test both
      themes.
- [ ] **Documentation updated?** This document is the spec.

---

## 10. Future Considerations

- **Tag colors:** Let users assign a color to a tag, making it visually
  distinguishable in the list. Out of scope for v1 — all tags use the same
  muted accent tone.
- **Pinned / saved searches:** Let users save a filter configuration (search
  query + active quick-filter chips) as a named "view" accessible from the
  sidebar. Out of scope for v1 — the filter bar alone solves the immediate
  findability problem.
- **Server-side search:** If a user has thousands of conversations and
  client-side filtering becomes slow (unlikely — even 1,000 conversations is a
  trivial array filter), add a server-side `ListConversationsFiltered` endpoint
  that pushes the search to the database with `LIKE` clauses. V1 stays
  client-side.
