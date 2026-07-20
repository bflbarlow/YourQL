# Skills Enhancement

A pluggable skills system that replaces the hardcoded `viz_enabled` toggle with a
general-purpose skill registry. Each skill bundles a prompt fragment, optional
post-processing logic, and an optional frontend component. Skills are togglable
per-conversation and globally configurable in a new Settings tab.

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│  Skill Registry (init-time)                         │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────┐ │
│  │ Data Viz     │  │ Export CSV   │  │ Profiling  │ │
│  │ Skill        │  │ Skill        │  │ Skill      │ │
│  └──────┬───────┘  └──────┬───────┘  └─────┬─────┘ │
└─────────┼─────────────────┼────────────────┼───────┘
          │                 │                │
          ▼                 ▼                ▼
┌─────────────────────────────────────────────────────┐
│  Skill Interface:                                   │
│    Key() string                                     │
│    Name() string                                    │
│    PromptFragment() string                          │
│    PostProcess(resp, results, metadata) error       │
│    FrontendComponent() string                       │
└─────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────┐
│  Database:                                          │
│  ┌──────────────┐    ┌──────────────────────────┐   │
│  │ skills       │    │ conversation_skills      │   │
│  │ (definitions)│◄───│ (per-conversation toggle) │   │
│  └──────────────┘    └──────────────────────────┘   │
└─────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────┐
│  Discussion Engine:                                 │
│                                                     │
│  buildSystemPrompt()                                │
│    → BuildSkillsPrompt(conversationID)              │
│    → for each enabled skill: .PromptFragment()      │
│                                                     │
│  renderSQLResults()                                 │
│    → RunSkillPostProcess(resp, results, metadata)   │
│    → for each enabled skill: .PostProcess()         │
└─────────────────────────────────────────────────────┘
```

The pattern mirrors the existing `DBDriver` registry: each skill is a Go struct implementing
the `Skill` interface, registered via `init()`.

---

## 2. Skill Interface

New file: `pkg/services/skill.go`

```go
package services

import "YourQL/pkg/models"

// Skill defines a pluggable capability that extends LLM behavior and response processing.
type Skill interface {
    // Key returns a unique identifier (e.g., "data_visualization", "export_csv").
    Key() string

    // Name returns a human-readable display name for settings UI.
    Name() string

    // Description returns a short explanation of what the skill does.
    Description() string

    // PromptFragment returns text injected into Zone B of the system prompt.
    // Return empty string if the skill needs no prompt modification.
    PromptFragment() string

    // PostProcess runs after SQL execution. The skill can modify the LLMResponse,
    // inspect results, or add entries to the message metadata map.
    // Return nil to skip processing.
    PostProcess(resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error

    // FrontendComponent returns the name of a Svelte component to render
    // alongside the message (e.g., "VizChart"). Return empty string if none.
    FrontendComponent() string

    // EnabledByDefault returns true if this skill should be enabled for new conversations.
    EnabledByDefault() bool
}
```

---

## 3. Skill Registry

New file: `pkg/services/skill_registry.go`

```go
package services

import (
    "fmt"
    "sort"
)

var skillRegistry = map[string]Skill{}

func RegisterSkill(skill Skill) {
    skillRegistry[skill.Key()] = skill
}

func GetSkill(key string) (Skill, error) {
    s, ok := skillRegistry[key]
    if !ok {
        return nil, fmt.Errorf("unknown skill: %s", key)
    }
    return s, nil
}

func ListSkills() []Skill {
    skills := make([]Skill, 0, len(skillRegistry))
    for _, s := range skillRegistry {
        skills = append(skills, s)
    }
    sort.Slice(skills, func(i, j int) bool {
        return skills[i].Key() < skills[j].Key()
    })
    return skills
}

func GetAllSkillKeys() []string {
    keys := make([]string, 0, len(skillRegistry))
    for k := range skillRegistry {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    return keys
}
```

---

## 4. Skill Definitions

### 4.1 Data Visualization Skill

New file: `pkg/services/skill_data_visualization.go`

This extracts the existing hardcoded viz logic into a skill.

```go
package services

import (
    "encoding/json"
    "fmt"
    "strings"
    "YourQL/pkg/models"
)

func init() {
    RegisterSkill(&DataVisualizationSkill{})
}

type DataVisualizationSkill struct{}

func (s *DataVisualizationSkill) Key() string         { return "data_visualization" }
func (s *DataVisualizationSkill) Name() string        { return "Data Visualization" }
func (s *DataVisualizationSkill) Description() string { return "LLM generates charts (bar, line, pie, scatter) when the user asks for visualizations" }
func (s *DataVisualizationSkill) EnabledByDefault() bool { return true }
func (s *DataVisualizationSkill) FrontendComponent() string { return "VizChart" }

func (s *DataVisualizationSkill) PromptFragment() string {
    return `## Data Visualization
You can generate charts when the user asks for visualizations (bar chart, line graph, pie chart, scatter plot, trend line, etc.).
To create a chart, include a "viz_config" field in your JSON response with a Chart.js configuration object.
Example "viz_config" value (as a JSON string):
{"type":"bar","data":{"labels":["$column_name"],"datasets":[{"label":"Data","data":["$column_name"]}]}}

Rules:
- "type" must be one of: bar, line, pie, doughnut, scatter, radar, polarArea
- "data.labels" is an array of ONE column name from your SQL result (the category/X axis)
- "data.datasets[].data" is an array of ONE column name (the value/Y axis)
- Use "$column_name" syntax to reference SQL result columns (the system replaces them with real data)
- You can define MULTIPLE datasets (as separate objects in the datasets array) for grouped/stacked charts
- For pie/doughnut: labels = category column, data = single value column (limit to 8 or fewer categories)
- For scatter: use data: [{"x": "$col1", "y": "$col2"}] format
- Choose chart type intelligently:
  * bar = comparisons, rankings, categories
  * line = time series, trends, sequential data
  * pie/doughnut = proportions, composition (<=8 categories)
  * scatter = correlation, relationship between two numeric variables
- Do NOT include viz_config unless the user explicitly asks for a chart or the data clearly benefits from one
- The viz_config must be a valid JSON string (double-quote all keys and values, escape internal quotes)
`
}

func (s *DataVisualizationSkill) PostProcess(resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error {
    if resp.VizConfig == "" || results == nil || len(results.Columns) == 0 {
        return nil
    }
    resolved, err := resolveChartConfig(resp.VizConfig, results.Columns, results.Rows)
    if err != nil || resolved == "" {
        return err
    }
    (*metadata)["chart_config"] = json.RawMessage(resolved)
    (*metadata)["skill_component"] = "VizChart"
    return nil
}
```

### 4.2 Export CSV Skill

New file: `pkg/services/skill_export_csv.go`

```go
package services

func init() {
    RegisterSkill(&ExportCSVSkill{})
}

type ExportCSVSkill struct{}

func (s *ExportCSVSkill) Key() string         { return "export_csv" }
func (s *ExportCSVSkill) Name() string        { return "Export to CSV" }
func (s *ExportCSVSkill) Description() string { return "LLM offers to generate CSV downloads when users ask for exports" }
func (s *ExportCSVSkill) EnabledByDefault() bool { return false }
func (s *ExportCSVSkill) FrontendComponent() string { return "" }

func (s *ExportCSVSkill) PromptFragment() string {
    return `## CSV Export
If the user asks to export results, download data, or save to CSV, include a "csv_export" field in your response:
{"csv_export": true, "csv_filename": "suggested_filename.csv"}

The system will automatically generate a CSV download from the query results.
Do NOT include csv_export unless the user explicitly asks to export or download data.
`
}

func (s *ExportCSVSkill) PostProcess(resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error {
    // The frontend handles CSV generation from sql_results.
    // This skill just adds the export flag to metadata.
    // (Future: could serialize CSV data into metadata directly)
    return nil
}
```

### 4.3 Data Profiling Skill

New file: `pkg/services/skill_data_profiling.go`

```go
package services

import "fmt"

func init() {
    RegisterSkill(&DataProfilingSkill{})
}

type DataProfilingSkill struct{}

func (s *DataProfilingSkill) Key() string         { return "data_profiling" }
func (s *DataProfilingSkill) Name() string        { return "Data Profiling" }
func (s *DataProfilingSkill) Description() string { return "LLM provides null counts, distinct values, and summary stats when users ask about data quality" }
func (s *DataProfilingSkill) EnabledByDefault() bool { return false }
func (s *DataProfilingSkill) FrontendComponent() string { return "" }

func (s *DataProfilingSkill) PromptFragment() string {
    return `## Data Profiling
When the user asks about data quality, completeness, or wants to explore a table's structure,
you can run profiling queries. Use these patterns:
- Null check: SELECT COUNT(*) as total, COUNT(column) as non_null FROM table
- Distinct values: SELECT COUNT(DISTINCT column) FROM table
- Value range: SELECT MIN(column), MAX(column), AVG(column) FROM table
- Distribution: SELECT column, COUNT(*) as count FROM table GROUP BY column ORDER BY count DESC LIMIT 10

Present the findings in a clear, structured way in your explanation.
`
}

func (s *DataProfilingSkill) PostProcess(resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error {
    // No special post-processing — the LLM's explanation is sufficient.
    return nil
}
```

### 4.4 Query Explanation Skill

New file: `pkg/services/skill_query_explanation.go`

```go
package services

func init() {
    RegisterSkill(&QueryExplanationSkill{})
}

type QueryExplanationSkill struct{}

func (s *QueryExplanationSkill) Key() string         { return "query_explanation" }
func (s *QueryExplanationSkill) Name() string        { return "Query Explanation" }
func (s *QueryExplanationSkill) Description() string { return "LLM explains what each generated SQL query does in plain English" }
func (s *QueryExplanationSkill) EnabledByDefault() bool { return true }
func (s *QueryExplanationSkill) FrontendComponent() string { return "" }

func (s *QueryExplanationSkill) PromptFragment() string {
    return `## Query Explanation
After executing a SQL query, always include a brief plain-English explanation of what the query does.
Be concise — one or two sentences. Focus on the business meaning, not the SQL syntax.
`
}

func (s *QueryExplanationSkill) PostProcess(resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error {
    return nil
}
```

### 4.5 Anomaly Detection Skill

New file: `pkg/services/skill_anomaly_detection.go`

```go
package services

func init() {
    RegisterSkill(&AnomalyDetectionSkill{})
}

type AnomalyDetectionSkill struct{}

func (s *AnomalyDetectionSkill) Key() string         { return "anomaly_detection" }
func (s *AnomalyDetectionSkill) Name() string        { return "Anomaly Detection" }
func (s *AnomalyDetectionSkill) Description() string { return "LLM identifies outliers and anomalies in numeric data using statistical methods" }
func (s *AnomalyDetectionSkill) EnabledByDefault() bool { return false }
func (s *AnomalyDetectionSkill) FrontendComponent() string { return "" }

func (s *AnomalyDetectionSkill) PromptFragment() string {
    return `## Anomaly Detection
When the user asks about anomalies, outliers, or unusual patterns, use statistical methods:
- Z-score: Identify values where |value - avg| / stddev > 2
- IQR: Identify values outside Q1 - 1.5*IQR and Q3 + 1.5*IQR
- Time series: Identify sudden spikes or drops (>2x moving average)

Present findings as a concise list of anomalies with the value, expected range, and deviation.
Only do this when the user explicitly asks about anomalies or outliers.
`
}

func (s *AnomalyDetectionSkill) PostProcess(resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error {
    return nil
}
```

---

## 5. Database Changes

### 5.1 New Tables

In `pkg/models/database.go`:

```sql
-- Skills: stores built-in skill definitions (synced from registry on startup)
CREATE TABLE IF NOT EXISTS skills (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    key        TEXT NOT NULL UNIQUE,   -- "data_visualization", "export_csv", etc.
    name       TEXT NOT NULL,          -- "Data Visualization", "Export to CSV"
    description TEXT NOT NULL,         -- user-facing description
    is_active  INTEGER DEFAULT 1,      -- global on/off toggle
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- conversation_skills: which skills are enabled for a specific conversation
CREATE TABLE IF NOT EXISTS conversation_skills (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL,
    skill_key       TEXT NOT NULL,
    enabled         INTEGER DEFAULT 1,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    UNIQUE(conversation_id, skill_key)
);
```

### 5.2 Migration

Add to the `ensureColumn` / migration block in `database.go`:

```go
// Create skills table if not exists
_, _ = DB.Exec(`CREATE TABLE IF NOT EXISTS skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    is_active INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
)`)

// Create conversation_skills table if not exists
_, _ = DB.Exec(`CREATE TABLE IF NOT EXISTS conversation_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL,
    skill_key TEXT NOT NULL,
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    UNIQUE(conversation_id, skill_key)
)`)

// Migration: move viz_enabled → conversation_skills
runMigration("migrate_viz_enabled_to_skills", func() error {
    // For each conversation with viz_enabled = 1, add the data_visualization skill
    _, err := DB.Exec(`
        INSERT OR IGNORE INTO conversation_skills (conversation_id, skill_key, enabled)
        SELECT id, 'data_visualization', 1
        FROM conversations
        WHERE viz_enabled = 1
    `)
    return err
})
```

### 5.3 Sync Skills on Startup

On app startup, iterate all registered skills and upsert into the `skills` table:

```go
func SyncSkills() error {
    for _, skill := range services.ListSkills() {
        _, err := models.DB.Exec(`
            INSERT INTO skills (key, name, description, is_active)
            VALUES (?, ?, ?, 1)
            ON CONFLICT(key) DO UPDATE SET
                name = excluded.name,
                description = excluded.description,
                updated_at = CURRENT_TIMESTAMP
        `, skill.Key(), skill.Name(), skill.Description())
        if err != nil {
            return fmt.Errorf("failed to sync skill %s: %w", skill.Key(), err)
        }
    }
    return nil
}
```

Called in `app.go` `Startup()`:

```go
func (a *App) Startup(ctx context.Context) {
    a.ctx = ctx
    if err := services.SyncSkills(); err != nil {
        log.Printf("Failed to sync skills: %v", err)
    }
}
```

### 5.4 Per-Conversation Skill Queries

New file: `pkg/services/skill_service.go`

```go
// GetEnabledSkillsForConversation returns the skill keys enabled for a conversation.
func GetEnabledSkillsForConversation(conversationID uint) ([]string, error) {
    rows, err := models.DB.Query(
        "SELECT skill_key FROM conversation_skills WHERE conversation_id = ? AND enabled = 1",
        conversationID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var keys []string
    for rows.Next() {
        var key string
        if err := rows.Scan(&key); err != nil {
            return nil, err
        }
        keys = append(keys, key)
    }
    return keys, nil
}

// SetConversationSkill enables or disables a skill for a conversation.
func SetConversationSkill(conversationID uint, skillKey string, enabled bool) error {
    _, err := models.DB.Exec(`
        INSERT INTO conversation_skills (conversation_id, skill_key, enabled)
        VALUES (?, ?, ?)
        ON CONFLICT(conversation_id, skill_key) DO UPDATE SET
            enabled = excluded.enabled,
            created_at = CURRENT_TIMESTAMP
    `, conversationID, skillKey, enabled)
    return err
}

// EnableDefaultSkills enables all skills with EnabledByDefault() for a new conversation.
func EnableDefaultSkills(conversationID uint) error {
    for _, skill := range ListSkills() {
        if skill.EnabledByDefault() {
            if err := SetConversationSkill(conversationID, skill.Key(), true); err != nil {
                return err
            }
        }
    }
    return nil
}

// BuildSkillsPrompt returns the combined prompt fragments for all enabled skills.
func BuildSkillsPrompt(conversationID uint) string {
    keys, err := GetEnabledSkillsForConversation(conversationID)
    if err != nil || len(keys) == 0 {
        return ""
    }
    var sb strings.Builder
    for _, key := range keys {
        skill, err := GetSkill(key)
        if err != nil {
            continue
        }
        fragment := skill.PromptFragment()
        if fragment != "" {
            sb.WriteString(fragment)
            sb.WriteString("\n")
        }
    }
    return sb.String()
}

// RunSkillPostProcess runs PostProcess for all enabled skills.
func RunSkillPostProcess(conversationID uint, resp *LLMResponse, results *QueryResult, metadata *map[string]interface{}) error {
    keys, err := GetEnabledSkillsForConversation(conversationID)
    if err != nil {
        return err
    }
    for _, key := range keys {
        skill, err := GetSkill(key)
        if err != nil {
            continue
        }
        if err := skill.PostProcess(resp, results, metadata); err != nil {
            log.Printf("Skill %s PostProcess failed: %v", key, err)
        }
    }
    return nil
}
```

---

## 6. Discussion Engine Changes

### 6.1 `buildSystemPrompt` — Remove Hardcoded Viz Block

Current signature:
```go
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, vizEnabled bool) string
```

New signature:
```go
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, conversationID uint) string
```

Inside the function, replace the `if vizEnabled { ... }` block with:

```go
// Inject skill prompt fragments (Zone B)
skillsPrompt := BuildSkillsPrompt(conversationID)
if skillsPrompt != "" {
    sb.WriteString(skillsPrompt)
}
```

### 6.2 `buildLlmMessages` — Update Call

```go
// Before:
systemPrompt := buildSystemPrompt(schema, hasDB, dbConnection, vizEnabled)

// After:
systemPrompt := buildSystemPrompt(schema, hasDB, dbConnection, conversation.ID)
```

Remove `vizEnabled` parameter from `buildLlmMessages`.

### 6.3 `renderSQLResults` — Replace Hardcoded Viz Check

Before:
```go
if conversation.VizEnabled && resp.VizConfig != "" && results != nil && len(results.Columns) > 0 {
    resolved, err := resolveChartConfig(resp.VizConfig, results.Columns, results.Rows)
    if err == nil && resolved != "" {
        *metadataPtr = string(mustMarshalJSON(map[string]interface{}{
            "content_type": "html",
            "chart_config": json.RawMessage(resolved),
        }))
    }
}
```

After:
```go
// Run skill post-processing pipeline
metadata := map[string]interface{}{"content_type": "html"}
if err := RunSkillPostProcess(conversation.ID, &resp, results, &metadata); err != nil {
    log.Printf("[DiscussionEngine] Skill post-processing failed: %v", err)
}
*metadataPtr = string(mustMarshalJSON(metadata))
```

This also removes the `conversation` parameter from `renderSQLResults` (or keeps it, just uses `conversation.ID`).

### 6.4 `CreateConversation` — Enable Default Skills

After creating a conversation, enable default skills:

```go
func CreateConversation(title string, llmProviderID, dataSourceID *uint) (*models.Conversation, error) {
    // ... existing insert logic ...
    conversation, err := GetConversationByID(uint(id))
    if err != nil {
        return nil, err
    }
    // Enable default skills for new conversation
    if err := EnableDefaultSkills(conversation.ID); err != nil {
        log.Printf("Failed to enable default skills: %v", err)
    }
    return conversation, nil
}
```

### 6.5 `ProcessUserMessage` — Update Caller

Remove the `conversation.VizEnabled` usage. The `buildLlmMessages` call no longer needs `vizEnabled`.

---

## 7. Conversation Model Changes

### 7.1 Remove `VizEnabled` from `Conversation` struct

```go
// Remove this line:
VizEnabled bool `json:"viz_enabled"`

// OR: deprecate the field (keep it, but stop using it in logic)
// The field can remain for backward compatibility of the API, but
// the discussion engine ignores it.
```

### 7.2 Remove `viz_enabled` from SQL queries

Update `GetConversationByID`, `ListConversationsByUser`, and other queries that scan `viz_enabled`. Stop scanning it (or keep it if the column remains for backward compatibility).

### 7.3 Remove `UpdateConversationVizEnabled`

This function is no longer needed. Replace with `SetConversationSkill`.

---

## 8. API Layer Changes — `app.go`

### 8.1 New Endpoints

```go
// ListSkills returns all skills with their global settings.
func (a *App) ListSkills() ([]SkillSetting, error) {
    skills := services.ListSkills()
    settings := make([]SkillSetting, 0, len(skills))
    for _, s := range skills {
        // Get global is_active from DB
        isActive := true
        var active int
        err := models.DB.QueryRow(
            "SELECT is_active FROM skills WHERE key = ?", s.Key(),
        ).Scan(&active)
        if err == nil {
            isActive = active == 1
        }
        settings = append(settings, SkillSetting{
            Key:          s.Key(),
            Name:         s.Name(),
            Description:  s.Description(),
            IsActive:     isActive,
            EnabledByDefault: s.EnabledByDefault(),
        })
    }
    return settings, nil
}

// SetSkillActive globally enables or disables a skill.
func (a *App) SetSkillActive(key string, active bool) error {
    _, err := models.DB.Exec(
        "UPDATE skills SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE key = ?",
        active, key,
    )
    return err
}

// GetConversationSkills returns the enabled skills for a conversation.
func (a *App) GetConversationSkills(conversationID uint) ([]ConversationSkillSetting, error) {
    keys, err := services.GetEnabledSkillsForConversation(conversationID)
    if err != nil {
        return nil, err
    }
    settings := make([]ConversationSkillSetting, len(keys))
    for i, key := range keys {
        skill, err := services.GetSkill(key)
        if err != nil {
            continue
        }
        settings[i] = ConversationSkillSetting{
            Key:  key,
            Name: skill.Name(),
        }
    }
    return settings, nil
}

// SetConversationSkill enables or disables a skill for a conversation.
func (a *App) SetConversationSkill(conversationID uint, skillKey string, enabled bool) error {
    return services.SetConversationSkill(conversationID, skillKey, enabled)
}
```

### 8.2 Data Types

```go
type SkillSetting struct {
    Key              string `json:"key"`
    Name             string `json:"name"`
    Description      string `json:"description"`
    IsActive         bool   `json:"is_active"`
    EnabledByDefault bool   `json:"enabled_by_default"`
}

type ConversationSkillSetting struct {
    Key  string `json:"key"`
    Name string `json:"name"`
}
```

### 8.3 Remove Old Endpoints

- `UpdateConversationVizEnabled` — removed, replaced by `SetConversationSkill`

---

## 9. Frontend Changes

### 9.1 Settings — New "Skills" Tab

Add a fourth tab to `SettingsView.svelte`:

```svelte
<div class="settings-tabs">
    <button class="tab-btn {activeSettingsTab === 'models' ? 'active' : ''}"
            onclick={() => activeSettingsTab = 'models'}>Models</button>
    <button class="tab-btn {activeSettingsTab === 'databases' ? 'active' : ''}"
            onclick={() => activeSettingsTab = 'databases'}>Data Sources</button>
    <button class="tab-btn {activeSettingsTab === 'skills' ? 'active' : ''}"
            onclick={() => activeSettingsTab = 'skills'}>Skills</button>
    <button class="tab-btn {activeSettingsTab === 'general' ? 'active' : ''}"
            onclick={() => activeSettingsTab = 'general'}>General</button>
</div>
```

#### Skills Tab Content

```svelte
{#if activeSettingsTab === 'skills'}
  <div class="settings-section">
    <h2>Skills</h2>
    <div class="safety-hint">Skills extend the LLM's capabilities. Enable or disable globally.</div>

    {#each skills as skill (skill.key)}
      <div class="skill-card">
        <div class="skill-info">
          <div class="skill-name">{skill.name}</div>
          <div class="skill-desc">{skill.description}</div>
          <div class="skill-meta">
            {#if skill.enabled_by_default}
              <span class="badge badge-default">On by default</span>
            {/if}
          </div>
        </div>
        <label class="toggle-switch">
          <input type="checkbox"
                 checked={skill.is_active}
                 onchange={() => handleToggleSkill(skill.key, !skill.is_active)} />
          <span class="slider"></span>
        </label>
      </div>
    {/each}
  </div>
{/if}
```

### 9.2 Conversation Gear Popover — Skill Checklist

Replace the single "Data visualization" checkbox in the gear popover with a skills checklist.

In `App.svelte`, the summarize section currently has:

```svelte
<!-- Data Visualization -->
<div class="gear-popover-section">
    <label>
        <input type="checkbox" checked={selectedConversation.viz_enabled !== false}
               onchange={(e) => handleToggleVizEnabled(selectedConversation.id, e.target.checked)} />
        Data visualization
    </label>
    <div style="color: #999; font-size: var(--font-xs); margin-top: var(--space-2xs);">
        LLM generates charts (bar, line, pie, scatter) when appropriate
    </div>
</div>
```

Replace with a dynamic skills list:

```svelte
<!-- Skills -->
<div class="gear-popover-section">
    <div class="gear-popover-section-title">Skills</div>
    {#each conversationSkills as skill (skill.key)}
        <label class="skill-toggle">
            <input type="checkbox"
                   checked={skill.enabled}
                   onchange={(e) => handleToggleConversationSkill(selectedConversation.id, skill.key, e.target.checked)} />
            {skill.name}
        </label>
    {/each}
</div>
```

Where `conversationSkills` is fetched when the gear popover opens:

```js
async function loadConversationSkills(conversationId) {
    try {
        conversationSkills = await GetConversationSkills(conversationId)
    } catch (e) {
        conversationSkills = []
    }
}
```

### 9.3 Remove `viz_enabled` from Conversation Cards

Remove the `viz_enabled` reference from the conversation card display. The skills are now shown in the gear popover.

### 9.4 Remove `handleToggleVizEnabled`

Replaced by `handleToggleConversationSkill`:

```js
async function handleToggleConversationSkill(conversationId, skillKey, enabled) {
    try {
        await SetConversationSkill(conversationId, skillKey, enabled)
        // Update local state
        const skill = conversationSkills.find(s => s.key === skillKey)
        if (skill) skill.enabled = enabled
        conversationSkills = conversationSkills
    } catch (e) {
        console.error('Failed to toggle skill:', e)
    }
}
```

### 9.5 CSS for Skill Cards

```css
.skill-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-lg) var(--space-xl);
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    margin-bottom: var(--space-md);
    background: #ffffff;
}
.skill-info { flex: 1; }
.skill-name { font-weight: 600; font-size: var(--font-base); }
.skill-desc { color: #666; font-size: var(--font-sm); margin-top: var(--space-2xs); }
.skill-meta { margin-top: var(--space-xs); }
.badge-default {
    font-size: var(--font-xs);
    background: #e3f2fd;
    color: #1565c0;
    padding: 2px 8px;
    border-radius: 4px;
}
.toggle-switch {
    position: relative;
    display: inline-block;
    width: 44px;
    height: 24px;
}
.toggle-switch input { opacity: 0; width: 0; height: 0; }
.slider {
    position: absolute;
    cursor: pointer;
    top: 0; left: 0; right: 0; bottom: 0;
    background: #ccc;
    border-radius: 24px;
    transition: 0.2s;
}
.slider:before {
    position: absolute;
    content: "";
    height: 18px; width: 18px;
    left: 3px; bottom: 3px;
    background: white;
    border-radius: 50%;
    transition: 0.2s;
}
input:checked + .slider { background: #0288d1; }
input:checked + .slider:before { transform: translateX(20px); }
```

---

## 10. Frontend Component Rendering

The `FrontendComponent()` method on the Skill interface tells the frontend which Svelte component to render. This replaces the current hardcoded `VizChart` import in `ConversationView.svelte`.

### 10.1 Dynamic Component Rendering

In `ConversationView.svelte`, after the assistant message content, iterate message metadata for skill components:

```svelte
{#if message.metadata}
  {@const meta = parseMetadata(message.metadata)}
  {#if meta.skill_component === 'VizChart'}
    <VizChart config={meta.chart_config} />
  {/if}
{/if}
```

For now, this is a simple `{#if}` chain. In the future, a Svelte `<svelte:component>` dynamic component could render any skill component automatically.

---

## 11. File Manifest

| File | Action | Lines | Description |
|------|--------|-------|-------------|
| `pkg/services/skill.go` | **New** | ~25 | Skill interface |
| `pkg/services/skill_registry.go` | **New** | ~35 | Skill registry (RegisterSkill, GetSkill, ListSkills) |
| `pkg/services/skill_service.go` | **New** | ~80 | DB queries + BuildSkillsPrompt + RunSkillPostProcess |
| `pkg/services/skill_data_visualization.go` | **New** | ~70 | Data Viz skill (extracted from discussion_engine.go) |
| `pkg/services/skill_export_csv.go` | **New** | ~30 | Export CSV skill (placeholder) |
| `pkg/services/skill_data_profiling.go` | **New** | ~35 | Data Profiling skill (placeholder) |
| `pkg/services/skill_query_explanation.go` | **New** | ~25 | Query Explanation skill |
| `pkg/services/skill_anomaly_detection.go` | **New** | ~30 | Anomaly Detection skill (placeholder) |
| `pkg/models/database.go` | **Edit** | +30 | skills + conversation_skills tables + migration |
| `pkg/models/conversation.go` | **Edit** | -1 | Remove VizEnabled field |
| `pkg/services/conversation.go` | **Edit** | +10 | EnableDefaultSkills in CreateConversation, remove UpdateConversationVizEnabled |
| `pkg/services/discussion_engine.go` | **Edit** | ~-30 +15 | Remove hardcoded viz block, use BuildSkillsPrompt + RunSkillPostProcess |
| `app.go` | **Edit** | +50 | ListSkills, SetSkillActive, GetConversationSkills, SetConversationSkill endpoints |
| `frontend/src/SettingsView.svelte` | **Edit** | +60 | Skills tab with toggle cards |
| `frontend/src/App.svelte` | **Edit** | ~20 | Skill checklist in gear popover, remove viz_enabled toggle |
| **Total** | | **~500** | |

---

## 12. Migration Path

### Step 1: Add tables and migration
- Create `skills` and `conversation_skills` tables
- Run `migrate_viz_enabled_to_skills` migration
- Keep `viz_enabled` column (backward compatible)

### Step 2: Add skill interface + registry
- `skill.go`, `skill_registry.go` — no behavior changes yet

### Step 3: Add skill service
- `skill_service.go` — DB queries, prompt building, post-processing

### Step 4: Add skill definitions
- `skill_data_visualization.go` — extract existing viz logic
- Other skills — placeholder implementations

### Step 5: Update discussion engine
- `buildSystemPrompt` → use `BuildSkillsPrompt`
- `renderSQLResults` → use `RunSkillPostProcess`
- Remove hardcoded viz blocks

### Step 6: Update API
- Add `ListSkills`, `SetSkillActive`, `GetConversationSkills`, `SetConversationSkill`
- Remove `UpdateConversationVizEnabled`

### Step 7: Update frontend
- Settings → Skills tab
- Conversation gear → skill checklist
- Remove viz_enabled toggle

### Step 8: Sync skills on startup
- `SyncSkills()` in `Startup()`

---

## 13. Design Decisions

| Decision | Rationale |
|----------|-----------|
| Skills are Go code, not DB-only | Post-processing hooks need code. Prompt-only skills would be simpler but less powerful. |
| Skill registry pattern mirrors DBDriver | Consistent architecture, same `init()` registration pattern. |
| Conversation-level toggles, not global-only | Different conversations need different capabilities (e.g., viz for sales data, not for user management). |
| EnabledByDefault() on interface | New conversations get sensible defaults. User can turn off globally in Skills tab. |
| `conversation_skills` join table, not JSON column | Enables SQL queries (`WHERE enabled = 1`), indexing, and clean migration. |
| `viz_enabled` column kept (deprecated) | Backward compatibility for existing databases. Migration populates join table. |
| `FrontendComponent()` on interface | Lets the frontend know which component to render without hardcoding skill names. |
| `Metadata` map for PostProcess output | Flexible key-value store. Skills add their own keys without conflicts. |
| Global toggle in `skills` table | Users can disable a skill entirely (won't appear in per-conversation list). |

---

## 14. Future: User-Defined Skills

The `skills` table has a `key` column that currently maps to built-in Go skills. In the future, user-defined skills could be added:

- A new `skill_type` column: `builtin` or `custom`
- Custom skills store only `prompt_fragment` (no PostProcess, no FrontendComponent)
- A "Create Skill" button in the Skills tab
- Custom skills appear in the per-conversation checklist alongside built-in ones

This would require a separate `SkillRegistry` lookup that falls back to reading from the DB for custom skills. Not in scope for MVP.