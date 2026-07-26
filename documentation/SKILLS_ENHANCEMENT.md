# Skills Enhancement

Skills are **named blocks of markdown text** that get injected into the system prompt,
giving the LLM additional context or behavioral guidance. They are pure text — no code
hooks, no post-processing, no frontend components. Think of them as reusable,
toggleable prompt fragments.

**What skills are NOT:**
- Not code plugins (no Go interfaces, no post-processing hooks)
- Not a replacement for data visualization (viz stays as-is, hardcoded in Zone B)
- Not user-defined functions or tools

**What skills ARE:**
- A name (e.g., "Sales Domain Context")
- A markdown text block (e.g., "Revenue is in USD. Fiscal year starts July 1. Exclude test accounts.")
- Globally configurable in Settings
- Toggleable per conversation from the gear menu
- Appended to the system prompt when enabled

---

## 1. Database

### 1.1 New Tables

Add to `pkg/models/database.go`:

```sql
CREATE TABLE IF NOT EXISTS skills (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    markdown_content TEXT NOT NULL DEFAULT '',
    is_active        INTEGER DEFAULT 1,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS conversation_skills (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL,
    skill_id        INTEGER NOT NULL,
    enabled         INTEGER DEFAULT 1,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (skill_id) REFERENCES skills(id) ON DELETE CASCADE,
    UNIQUE(conversation_id, skill_id)
);
```

### 1.2 Model

Add to `pkg/models/skill.go` (new file):

```go
package models

import "time"

type Skill struct {
    ID              uint      `json:"id"`
    Name            string    `json:"name"`
    MarkdownContent string    `json:"markdown_content"`
    IsActive        bool      `json:"is_active"`
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}
```

No separate `ConversationSkill` model needed — the join table is queried inline.

---

## 2. Service Layer

New file: `pkg/services/skill_service.go`

```go
package services

import (
    "fmt"
    "strings"
    "YourQL/pkg/models"
)

// ListSkills returns all skills.
func ListSkills() ([]models.Skill, error) {
    rows, err := models.DB.Query(
        "SELECT id, name, markdown_content, is_active, created_at, updated_at FROM skills ORDER BY name",
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var skills []models.Skill
    for rows.Next() {
        var s models.Skill
        if err := rows.Scan(&s.ID, &s.Name, &s.MarkdownContent, &s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
            return nil, err
        }
        skills = append(skills, s)
    }
    return skills, rows.Err()
}

// CreateSkill creates a new skill.
func CreateSkill(name, markdownContent string) (*models.Skill, error) {
    result, err := models.DB.Exec(
        "INSERT INTO skills (name, markdown_content) VALUES (?, ?)",
        name, markdownContent,
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create skill: %w", err)
    }
    id, _ := result.LastInsertId()
    return GetSkill(uint(id))
}

// UpdateSkill updates a skill's name and markdown content.
func UpdateSkill(id uint, name, markdownContent string) (*models.Skill, error) {
    _, err := models.DB.Exec(
        "UPDATE skills SET name = ?, markdown_content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
        name, markdownContent, id,
    )
    if err != nil {
        return nil, fmt.Errorf("failed to update skill: %w", err)
    }
    return GetSkill(id)
}

// DeleteSkill deletes a skill and its conversation associations.
func DeleteSkill(id uint) error {
    _, err := models.DB.Exec("DELETE FROM skills WHERE id = ?", id)
    return err
}

// SetSkillActive globally enables or disables a skill.
func SetSkillActive(id uint, active bool) error {
    _, err := models.DB.Exec(
        "UPDATE skills SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
        active, id,
    )
    return err
}

// GetSkill returns a single skill by ID.
func GetSkill(id uint) (*models.Skill, error) {
    var s models.Skill
    err := models.DB.QueryRow(
        "SELECT id, name, markdown_content, is_active, created_at, updated_at FROM skills WHERE id = ?", id,
    ).Scan(&s.ID, &s.Name, &s.MarkdownContent, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
    if err != nil {
        return nil, err
    }
    return &s, nil
}

// GetEnabledSkillsForConversation returns the markdown content of all enabled
// skills for a conversation, concatenated with double newlines.
func GetEnabledSkillsForConversation(conversationID uint) (string, error) {
    rows, err := models.DB.Query(`
        SELECT s.markdown_content FROM skills s
        JOIN conversation_skills cs ON cs.skill_id = s.id
        WHERE cs.conversation_id = ? AND cs.enabled = 1 AND s.is_active = 1
    `, conversationID)
    if err != nil {
        return "", err
    }
    defer rows.Close()

    var parts []string
    for rows.Next() {
        var content string
        if err := rows.Scan(&content); err != nil {
            return "", err
        }
        if strings.TrimSpace(content) != "" {
            parts = append(parts, content)
        }
    }
    return strings.Join(parts, "\n\n"), rows.Err()
}

// GetConversationSkillIDs returns the IDs of skills enabled for a conversation.
func GetConversationSkillIDs(conversationID uint) ([]uint, error) {
    rows, err := models.DB.Query(
        "SELECT skill_id FROM conversation_skills WHERE conversation_id = ? AND enabled = 1",
        conversationID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var ids []uint
    for rows.Next() {
        var id uint
        if err := rows.Scan(&id); err != nil {
            return nil, err
        }
        ids = append(ids, id)
    }
    return ids, rows.Err()
}

// SetConversationSkill enables or disables a skill for a conversation.
func SetConversationSkill(conversationID, skillID uint, enabled bool) error {
    _, err := models.DB.Exec(`
        INSERT INTO conversation_skills (conversation_id, skill_id, enabled)
        VALUES (?, ?, ?)
        ON CONFLICT(conversation_id, skill_id) DO UPDATE SET enabled = excluded.enabled
    `, conversationID, skillID, enabled)
    return err
}
```

---

## 3. System Prompt Injection

### 3.1 `buildSystemPrompt` — Single Line Addition

In `pkg/services/discussion_engine.go`, `buildSystemPrompt()`, add after the safety rules and before the "Your response must be..." line:

```go
// Append conversation skills (user-defined context)
if conversationID > 0 {
    skillsContent, err := GetEnabledSkillsForConversation(conversationID)
    if err == nil && skillsContent != "" {
        sb.WriteString("\n## Additional Context (from Skills)\n")
        sb.WriteString(skillsContent)
        sb.WriteString("\n")
    }
}
```

This is the ONLY change to the discussion engine. The current signature:
```go
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, vizEnabled bool) string
```
stays exactly the same. `conversationID` is not needed in the signature — `GetEnabledSkillsForConversation` can be called from the caller (`buildLlmMessages`) and the skill content passed as an additional parameter. Or the signature changes minimally:

```go
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, vizEnabled bool, skillsContent string) string
```

And in `buildLlmMessages`:
```go
skillsContent, _ := GetEnabledSkillsForConversation(conversationID)
systemPrompt := buildSystemPrompt(schema, hasDB, dbConnection, vizEnabled, skillsContent)
```

This keeps the function pure — no DB call inside prompt building.

---

## 4. API Layer — `app.go`

### 4.1 Endpoints

```go
// ListSkills returns all skills.
func (a *App) ListSkills() ([]models.Skill, error) {
    return services.ListSkills()
}

// CreateSkill creates a new skill.
func (a *App) CreateSkill(name, markdownContent string) (*models.Skill, error) {
    return services.CreateSkill(name, markdownContent)
}

// UpdateSkill updates a skill.
func (a *App) UpdateSkill(id uint, name, markdownContent string) (*models.Skill, error) {
    return services.UpdateSkill(id, name, markdownContent)
}

// DeleteSkill deletes a skill.
func (a *App) DeleteSkill(id uint) error {
    return services.DeleteSkill(id)
}

// SetSkillActive globally enables or disables a skill.
func (a *App) SetSkillActive(id uint, active bool) error {
    return services.SetSkillActive(id, active)
}

// GetConversationSkillIDs returns enabled skill IDs for a conversation.
func (a *App) GetConversationSkillIDs(conversationID uint) ([]uint, error) {
    return services.GetConversationSkillIDs(conversationID)
}

// SetConversationSkill enables or disables a skill for a conversation.
func (a *App) SetConversationSkill(conversationID uint, skillID uint, enabled bool) error {
    return services.SetConversationSkill(conversationID, skillID, enabled)
}
```

---

## 5. Frontend — Settings "Skills" Tab

### 5.1 Tab Structure

Add a fourth tab to `SettingsView.svelte`:

```svelte
<div class="settings-tabs">
    <button class="tab-btn" onclick={() => activeSettingsTab = 'models'}>Models</button>
    <button class="tab-btn" onclick={() => activeSettingsTab = 'databases'}>Data Sources</button>
    <button class="tab-btn" onclick={() => activeSettingsTab = 'skills'}>Skills</button>
    <button class="tab-btn" onclick={() => activeSettingsTab = 'general'}>General</button>
</div>
```

### 5.2 Skills Tab Content

```
┌─────────────────────────────────────────────────────┐
│ Skills                                              │
│                                                     │
│ Skills are markdown text blocks added to the system │
│ prompt. Use them to give the LLM domain context or  │
│ behavioral guidance.                                │
│                                                     │
│ ┌─────────────────────────────────────┐ [toggle] ┐  │
│ │ Sales Domain Context                │ [  ●  ] │  │
│ │ Revenue is in USD. Fiscal year      │          │  │
│ │ starts July 1. Exclude test         │ [edit]   │  │
│ │ accounts from all queries.          │ [delete] │  │
│ └─────────────────────────────────────┘          │  │
│                                                     │
│ ┌─────────────────────────────────────┐ [toggle] ┐  │
│ │ HIPAA Compliance                    │ [  ○  ] │  │
│ │ Never expose patient names or IDs.  │          │  │
│ │ Always aggregate counts to >= 10.   │ [edit]   │  │
│ └─────────────────────────────────────┘          │  │
│                                                     │
│ [+ New Skill]                                       │
└─────────────────────────────────────────────────────┘
```

Each skill card shows:
- Name (bold)
- First 100 chars of markdown (truncated)
- Toggle switch (globally enable/disable)
- Edit button (opens inline editor or modal)
- Delete button (with confirmation)

### 5.3 Skill Editor (Inline or Modal)

```
┌──────────────────────────────────────────┐
│ Skill Name: [Sales Domain Context    ]   │
│                                          │
│ Markdown Content:                        │
│ ┌──────────────────────────────────────┐ │
│ │ Revenue is in USD.                   │ │
│ │ Fiscal year starts July 1.           │ │
│ │ Exclude test accounts from queries.  │ │
│ │                                      │ │
│ └──────────────────────────────────────┘ │
│                                          │
│ [Save]  [Cancel]                         │
└──────────────────────────────────────────┘
```

The markdown text area should be ~8 rows, monospace or comfortable font. Markdown is plain text — no preview needed (the LLM reads raw markdown).

### 5.4 Imports

Add to `SettingsView.svelte` imports:
```js
import {
    ListSkills, CreateSkill, UpdateSkill, DeleteSkill, SetSkillActive
} from '../wailsjs/go/main/App.js'
```

### 5.5 State

```js
let skills = $state([])
let skillEditor = $state(null) // { id, name, markdown_content } or null
let isEditing = $state(false)
```

### 5.6 Key Functions

```js
async function loadSkills() {
    skills = await ListSkills()
}

async function handleSaveSkill() {
    if (skillEditor.id) {
        await UpdateSkill(skillEditor.id, skillEditor.name, skillEditor.markdown_content)
    } else {
        await CreateSkill(skillEditor.name, skillEditor.markdown_content)
    }
    skillEditor = null
    await loadSkills()
}

async function handleDeleteSkill(id) {
    if (confirm('Delete this skill?')) {
        await DeleteSkill(id)
        await loadSkills()
    }
}
```

---

## 6. Frontend — Conversation Gear Popover

### 6.1 Skill Checklist

In `App.svelte`, add a "Skills" section to the conversation gear popover (after the existing Summarize and Data Visualization sections):

```svelte
<div class="gear-popover-section">
    <div class="gear-popover-section-title">Skills</div>
    {#each allSkills as skill (skill.id)}
        {@const enabled = conversationSkillIDs.includes(skill.id)}
        <label class="skill-toggle">
            <input type="checkbox"
                   checked={enabled}
                   disabled={!skill.is_active}
                   onchange={(e) => handleToggleConversationSkill(selectedConversation.id, skill.id, e.target.checked)} />
            {skill.name}
        </label>
    {/each}
    {#if allSkills.length === 0}
        <div style="color: #999; font-size: var(--font-xs);">No skills configured. Add skills in Settings.</div>
    {/if}
</div>
```

### 6.2 State & Imports

```js
import { GetConversationSkillIDs, SetConversationSkill } from '../wailsjs/go/main/App.js'

let allSkills = $state([])
let conversationSkillIDs = $state([])
```

### 6.3 Loading Skills

Load skills when the gear popover opens or when the conversation changes:

```js
async function loadAllSkills() {
    allSkills = await ListSkills()
}

async function loadConversationSkills(conversationId) {
    conversationSkillIDs = await GetConversationSkillIDs(conversationId)
}
```

### 6.4 Toggle Handler

```js
async function handleToggleConversationSkill(conversationId, skillId, enabled) {
    await SetConversationSkill(conversationId, skillId, enabled)
    if (enabled) {
        conversationSkillIDs = [...conversationSkillIDs, skillId]
    } else {
        conversationSkillIDs = conversationSkillIDs.filter(id => id !== skillId)
    }
}
```

---

## 7. CSS

### 7.1 Skill Cards (Settings)

```css
.skill-card {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    padding: var(--space-lg) var(--space-xl);
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    margin-bottom: var(--space-md);
    background: #ffffff;
    gap: var(--space-lg);
}
.skill-card-main { flex: 1; min-width: 0; }
.skill-card-name {
    font-weight: 600;
    font-size: var(--font-base);
    margin-bottom: var(--space-2xs);
}
.skill-card-preview {
    color: #666;
    font-size: var(--font-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.skill-card-actions {
    display: flex;
    align-items: center;
    gap: var(--space-md);
    flex-shrink: 0;
}
.skill-card-actions button {
    padding: var(--space-xs) var(--space-sm);
    font-size: var(--font-sm);
    border: 1px solid #e0e0e0;
    border-radius: 4px;
    background: #fff;
    cursor: pointer;
}
.skill-card-actions button:hover { background: #f5f5f5; }
.skill-card-actions button.delete:hover { background: #ffebee; color: #c62828; border-color: #ffcdd2; }
```

### 7.2 Skill Editor Modal

```css
.skill-editor-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.4);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
}
.skill-editor {
    background: #fff;
    border-radius: 12px;
    padding: var(--space-2xl);
    width: 600px;
    max-width: 90vw;
    max-height: 80vh;
    overflow-y: auto;
}
.skill-editor h3 { margin: 0 0 var(--space-lg) 0; }
.skill-editor label {
    display: block;
    font-weight: 600;
    margin-bottom: var(--space-xs);
    font-size: var(--font-sm);
}
.skill-editor input[type="text"] {
    width: 100%;
    padding: var(--space-sm);
    border: 1px solid #ccc;
    border-radius: 4px;
    font-size: var(--font-base);
    margin-bottom: var(--space-lg);
}
.skill-editor textarea {
    width: 100%;
    min-height: 200px;
    padding: var(--space-sm);
    border: 1px solid #ccc;
    border-radius: 4px;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: var(--font-sm);
    line-height: 1.5;
    resize: vertical;
    margin-bottom: var(--space-lg);
}
.skill-editor-actions {
    display: flex;
    gap: var(--space-md);
    justify-content: flex-end;
}
```

### 7.3 Gear Popover Skill Toggles

```css
.gear-popover-section-title {
    font-weight: 600;
    font-size: var(--font-sm);
    margin-bottom: var(--space-sm);
    color: #333;
}
.skill-toggle {
    display: flex;
    align-items: center;
    gap: var(--space-sm);
    padding: var(--space-xs) 0;
    font-size: var(--font-sm);
    cursor: pointer;
}
.skill-toggle input[type="checkbox"] {
    accent-color: #0288d1;
}
.skill-toggle input[type="checkbox"]:disabled + span {
    color: #bbb;
}
```

---

## 8. File Manifest

| File | Action | Lines | Description |
|------|--------|-------|-------------|
| `pkg/models/skill.go` | **New** | ~15 | Skill struct |
| `pkg/models/database.go` | **Edit** | +20 | skills + conversation_skills tables |
| `pkg/services/skill_service.go` | **New** | ~90 | CRUD + conversation join queries |
| `pkg/services/discussion_engine.go` | **Edit** | +5 | Append skills content to system prompt |
| `app.go` | **Edit** | +40 | 7 API endpoints |
| `frontend/src/SettingsView.svelte` | **Edit** | +100 | Skills tab with cards, editor modal |
| `frontend/src/App.svelte` | **Edit** | +30 | Skill checklist in gear popover |
| `frontend/wailsjs/...` | **Regen** | — | `wails generate module` |
| **Total** | | **~300** | |

---

## 9. What Does NOT Change

| Component | Status |
|-----------|--------|
| Data Visualization | Untouched — stays as hardcoded Zone B logic |
| `viz_enabled` column | Untouched — independent of skills |
| `VizChart.svelte` | Untouched |
| `LLMResponse` struct | Untouched |
| `buildSystemPrompt` signature | +1 param (skillsContent string) |
| Conversation model | Untouched |
| LLM providers | Untouched |
| Data sources | Untouched |

Skills are additive — they do not replace or refactor any existing feature.

---

## 10. Example Skills

To make the feature immediately useful, seed the database with two example skills on first run (or just document examples):

**Sales Domain Context:**
```markdown
All revenue columns are in USD unless specified otherwise.
Fiscal year starts on July 1 and ends on June 30.
Always exclude records where account_type = 'test' or account_type = 'internal'.
The `deal_size` column uses these tiers: Small (< $10k), Medium ($10k-$50k), Large (> $50k).
```

**Query Style Guide:**
```markdown
When presenting results, always include the total row count.
For time-based queries, default to the last 12 months unless the user specifies otherwise.
Use CTEs (WITH clauses) for complex queries rather than nested subqueries.
If a query returns more than 50 rows, summarize with GROUP BY instead of listing all rows.
```

These are optional starting points — not seeded automatically (user creates them).

---

## 11. Implementation Order

| Step | What | Time |
|------|------|------|
| 1 | Database tables (`database.go`) | 5 min |
| 2 | Skill model (`skill.go`) | 5 min |
| 3 | Service layer (`skill_service.go`) | 15 min |
| 4 | API endpoints (`app.go`) | 10 min |
| 5 | Regenerate Wails bindings | 1 min |
| 6 | System prompt injection (`discussion_engine.go`) | 5 min |
| 7 | Settings Skills tab (`SettingsView.svelte`) | 30 min |
| 8 | Gear popover checklist (`App.svelte`) | 15 min |
| 9 | CSS | 10 min |
| **Total** | | **~1.5 hours** |

---

## 12. Design Decisions

| Decision | Rationale |
|----------|-----------|
| No code hooks | User's vision is pure text. Code hooks add complexity users don't need. |
| Viz stays separate | Data viz requires `$column` resolution and Chart.js rendering — not a good fit for text-only skills. |
| `conversation_skills` join table | Enables per-conversation toggling via SQL. Cleaner than a JSON array column. |
| Global toggle (`is_active`) | Disabled skills don't appear in conversation checklist — reduces clutter. |
| Markdown, not plain text | LLMs parse markdown naturally. Users can use headers, lists, bold for structure. |
| No skill ordering | Skills are appended in name-alphabetical order. Deterministic but no drag-to-reorder. |
| No skill categories/tags | Too much UI complexity for a text feature. If users have 20 skills, search/filter can be added later. |
| Editor is a modal | Keeps the Settings tab clean. Editing is infrequent. |
| No markdown preview | Skills are for the LLM, not for display. Raw text is sufficient. |

---

## 13. Future Enhancements (Not in Scope)

- **Skill ordering**: Drag-and-drop to control the order skills appear in the prompt
- **Skill templates**: A library of pre-written skill templates users can import
- **Skill tags/categories**: Filter skills by tag in the conversation gear popover
- **Per-data-source skills**: Auto-enable certain skills based on the selected data source
- **Skill variables**: `{{table_name}}` placeholders that get replaced with actual schema names
- **Import/export skills**: Share skill definitions as JSON files

None of these change the core model — just UI and convenience features.