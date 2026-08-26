> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# AGENT_LOOP_CONFIG.md — Agent Loop Configuration

> **Status:** Design specification. Implementation pending.  
> **Version target:** v0.5.0  
> **Created:** 2026-08-07

## 1. Overview

Currently, the agentic loop's tool descriptions, system prompt instructions,
and behavioral guidance are hardcoded in `pkg/services/agentic_loop.go`. Every
time we want to tune the model's behavior — whether it's nudging toward
`respond_to_user`, adjusting exploration safety language, or refining
chart-generation heuristics — we change Go source code and rebuild the binary.

This document specifies a configuration system that lets users customize
exactly what text the model receives for each tool description, each system
prompt section, and each behavioral instruction. Every configurable field
ships with a **sensible default** identical to the current hardcoded values.

**The Agent Loop settings tab is hidden by default.** A toggle on the General
settings page — "Enable advanced agent loop settings" — must be explicitly
turned on before the tab appears. This ensures non-technical users never
stumble onto a page of ~40 text areas, while power users can opt in with a
single click.

**Fundamental goal alignment (§0):** Answer accuracy is priority #1. Giving
users control over the model's instructions carries risk — a poorly worded
prompt can degrade answer quality. This design mitigates that with per-field
reset-to-default buttons, hover tooltips explaining each field's purpose, and
a "Restore All Defaults" escape hatch. The user is in control, but never
stranded.

### 1.1 Phased Rollout

The full inventory in §2 describes ~40 configurable fields. Ship them in two
phases to keep the initial implementation manageable:

**Phase 1 (v0.5.0) — Tool Descriptions + Instruction #3:**

| Category | Fields Shipped | Reason |
|---|---|---|
| Tool descriptions (§2.1) | All 8 fields | Highest leverage — tool descriptions directly shape when and how the model uses each tool. The `respond_to_user` nudge lives here. |
| System instructions (§2.2) | Instruction #3 only | The pairing guidance we just tuned. The single most impactful behavioral instruction. |

**Phase 2 (v0.6.0) — Remaining Sections:**

| Category | Fields Shipped |
|---|---|
| System instructions (§2.2) | Instructions #1, #2, #2a–c, #4–#6 |
| Exploration safety rules (§2.3) | All 6 fields |
| Chart guidance (§2.4) | All 3 fields |
| Fallback persona (§2.5) | 1 field |
| Tool response messages (§2.6) | All 10 fields |

The data model and config infrastructure (storage, service layer, Wails
bindings, enablement toggle) are built in Phase 1 and can read any field —
Phase 2 is purely adding UI surface area and wiring existing keys to the
remaining `buildToolSystemPrompt` and `runAgenticLoop` sites.

---

## 2. What Becomes Configurable

Every string the model receives that shapes its behavior, organized into
five categories. Each category maps to a collapsible section in the UI.

### 2.1 Tool Descriptions

The `description` field of each tool definition sent to the LLM. These are
the most impactful levers — they directly shape how the model chooses and
uses tools.

| Field | Current Default (source) |
|---|---|
| `query_database` description | `"Execute a read-only SELECT query against the connected database. Use this when the user's question requires querying data."` (`agentic_loop.go:27`) |
| `query_database` → `sql` parameter description | `"A valid SELECT SQL query. Must be read-only. Always include a LIMIT clause."` (`agentic_loop.go:33`) |
| `query_database` → `is_exploration` parameter description | `"Set to true for behind-the-scenes queries the user should NOT see..."` (`agentic_loop.go:36`) |
| `query_database` → `reasoning` parameter description | `"Optional. Brief internal reasoning about why this query answers the question..."` (`agentic_loop.go:39`) |
| `respond_to_user` description | `"Provide a direct response to the user. Use this on its own for non-query answers..."` (`agentic_loop.go:50`) |
| `respond_to_user` → `text` parameter description | `"The response text. Use markdown formatting for structure."` (`agentic_loop.go:56`) |
| `render_chart` description | `"Attach a chart visualization to the results..."` (`agentic_loop.go:68`) |
| `render_chart` → `chart_config` parameter description | `"A JSON string describing a Chart.js configuration..."` (`agentic_loop.go:76`) |

### 2.2 System Prompt — Instruction Block

The numbered instruction list injected after the schema and database
connection sections (`agentic_loop.go:601–612`).

| Field | Current Default |
|---|---|
| Instruction #1 (analyze) | `"1. Analyze the user's question and the database schema (if provided).\n"` |
| Instruction #2 (tools available intro) | `"2. You have three tools available:\n"` |
| Instruction #2a (query_database bullet) | `"   - \`query_database\` — run a read-only SELECT query. Set \`is_exploration: true\` for exploratory queries, \`false\` for the final answer.\n"` |
| Instruction #2b (respond_to_user bullet) | `"   - \`respond_to_user\` — provide a direct text response using markdown.\n"` |
| Instruction #2c (render_chart bullet) | `"   - \`render_chart\` — attach a chart ONLY when the user explicitly requests one. Do NOT call without a direct request.\n"` |
| Instruction #3 (pairing guidance) | `"3. **Consider pairing \`respond_to_user\` with your final \`query_database\` (is_exploration: false).** When you do, your text appears above the results table..."` |
| Instruction #4 (respond_to_user alone) | `"4. \`respond_to_user\` is also how you handle follow-ups, explanations, and clarifying questions when no query is needed.\n"` |
| Instruction #5 (read-only SQL) | `"5. All SQL queries must be read-only (SELECT only). Follow the dialect rules in the Database Connection section above.\n"` |
| Instruction #6 (LIMIT clause) | `"6. Always include a LIMIT clause matching the row limit set in Database Connection. Do NOT end SQL queries with a semicolon (;).\n\n"` |

### 2.3 System Prompt — Exploration Safety Rules

The safety-mode-specific block that tells the model what queries are
allowed during exploration (`agentic_loop.go:619–638`).

| Field | Current Default |
|---|---|
| Preamble (all modes) | `"## Exploration Safety Rules (CRITICAL)\nExploration queries (is_exploration: true) run in **%s** mode..."` |
| Strict mode rules | `"- Allowed: SELECT with LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA queries\n- Blocked: JOINs, subqueries, GROUP BY, ORDER BY\n"` |
| Moderate mode rules | `"- Allowed: everything in strict, plus single-table JOIN, GROUP BY, ORDER BY\n- Blocked: subqueries, UNION, multi-table JOINs\n"` |
| Relaxed mode rules | `"- Allowed: everything in moderate, plus subqueries and UNION\n- Blocked: INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE, and other DML/DDL\n"` |
| All-modes footer | `"- All modes: read-only only — no DML/DDL under any circumstances\n- You have up to %d exploration round(s) before being forced to produce a final query.\n- If an exploration query is rejected by safety mode, adapt within constraints OR use respond_to_user to tell the user what you cannot do..."` |
| One-shot rule | `"- ONE-SHOT RULE: Once you deliver a final query (is_exploration: false), your database session closes immediately. You may ONLY call respond_to_user or render_chart after that — no more queries of any kind.\n\n"` |

### 2.4 System Prompt — Chart Guidance

The chart-generation instructions (`agentic_loop.go:640–653`).

| Field | Current Default |
|---|---|
| Chart intro | `"## Charts\nThe \`render_chart\` tool lets you attach a Chart.js visualization — call it ONLY when the user EXPLICITLY asks for a chart, graph, plot, or visualization..."` |
| Chart config format | `"The \`chart_config\` argument must be a JSON string with:\n- \"type\": one of bar, line, pie, doughnut, scatter, radar, polarArea\n- \"data.labels\": [\"$column_name\"]...` |
| Chart timing + restraint | `"- Only call render_chart AFTER seeing the actual column names in the query result.\n- REMEMBER: only call this when the user explicitly requested a chart. If in doubt, skip it.\n\n"` |

### 2.5 System Prompt — Fallback Identity

The fallback "You are a helpful data analyst assistant" line used when
the data source has no custom system prompt set (`agentic_loop.go:485`).

| Field | Current Default |
|---|---|
| Fallback persona | `"You are a helpful data analyst assistant. Your task is to help users query a database using natural language.\n\n"` |

### 2.6 Tool Response Messages (System-to-LLM)

The error and guidance messages the system sends back to the LLM as tool
responses. These shape how the model recovers from mistakes.

| Field | Current Default | Source |
|---|---|---|
| Parse error (bad JSON arguments) | `"Error parsing arguments: %v. Please retry with valid, complete JSON arguments."` | `agentic_loop.go:791` |
| Exploration budget exhausted | `"Exploration budget exhausted. You must now produce a final query_database call (is_exploration: false) or call respond_to_user with what you've learned so far."` | `agentic_loop.go:805` |
| Safety rejection | `"Exploration query rejected: %s. Please revise it to comply with safety constraints."` | `agentic_loop.go:813` |
| One-shot violation (query after final) | `"Final result already delivered. Use respond_to_user if you need to explain limitations or next steps, or render_chart if the user requested a visualization."` | `agentic_loop.go:779` |
| Unknown tool | `"Unknown tool: %s"` | `agentic_loop.go:926` |
| render_chart with no pending result | `"render_chart failed: no pending query result to attach a chart to."` | `agentic_loop.go:908` |
| render_chart with parse error | `"render_chart failed: error parsing arguments: %v."` | `agentic_loop.go:906` |
| respond_to_user parse error | `"Error parsing arguments: %v. Please retry with valid, complete JSON arguments."` | `agentic_loop.go:919` |
| Loop exhausted (max rounds) | `"I wasn't able to complete this request due to repeated invalid responses. Could you try rephrasing your question?"` | `agentic_loop.go:940` |
| Empty/truncated response | `"I received an incomplete response. Could you try rephrasing your question?"` | `agentic_loop.go:763` |

---

## 3. Non-Configurable (Out of Scope)

Not everything becomes configurable. The following are **deliberately
excluded** from the Agent Loop settings:

| Item | Reason |
|---|---|
| Tool names (`query_database`, `respond_to_user`, `render_chart`) | The Go code matches on these string-literally in `switch` statements. Changing them would break the loop. |
| Parameter names (`sql`, `is_exploration`, `reasoning`, `text`, `chart_config`) | Same reason — `json.Unmarshal` targets exact field names. |
| Schema rendering format | The table/column markdown is structural — changing its format would break the LLM's ability to parse the schema. |
| SQL execution safety gates (DML/DDL blocking) | Non-negotiable per the Data Source Read-Only Invariant (§0). |
| Exploration safety *enforcement* (Go-side validation) | The safety mode dropdown (strict/moderate/relaxed) is already configurable per data source. The *descriptions* of those modes are configurable (see §2.3); the Go-side `validateExplorationQuery()` logic is not. |
| Row limit enforcement | Per-data-source config, already exists. |
| Maximum rounds / retry caps | Per-data-source config via `DataSourceConfig`, already exists. |

---

## 4. Enablement Toggle

The Agent Loop tab is gated behind an `app_settings` key. The tab does not
appear in the Settings sidebar unless this key is `"true"`.

| App Setting Key | Default | Description |
|---|---|---|
| `agent_loop_advanced_enabled` | `"false"` | When `"true"`, the Agent Loop tab appears in Settings. |

**UI location:** The toggle lives on the **General** settings tab (the first
tab, where theme, accent, and scale live). It is a simple on/off switch with
the label:

> **Enable advanced agent loop settings**  
> *Customize the prompts, tool descriptions, and instructions your AI model
> receives. For advanced users who want fine-grained control over model
> behavior.*

Flipping it on immediately reveals the Agent Loop tab in the sidebar. Flipping
it off hides the tab — existing overrides in `agent_loop_config` are preserved
(they're not deleted), just no longer visible or editable until re-enabled.

**Wails binding:** The toggle uses the existing `SetAppSetting` / `GetAppSetting`
bindings (already available in `app.go` for theme/accent/scale). No new
bindings needed for the toggle itself.

---

## 5. Data Model

### 5.1 Storage

Follow the existing `app_settings` / `discussion_defaults` pattern: a
simple key-value table. This avoids schema migrations for new fields and
keeps the data model flat.

```sql
CREATE TABLE IF NOT EXISTS agent_loop_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

### 5.2 Key Naming Convention

Keys follow a dotted hierarchy: `category.field.subfield`.

| Pattern | Example |
|---|---|
| `tool.{name}.description` | `tool.query_database.description` |
| `tool.{name}.params.{param}.description` | `tool.query_database.params.sql.description` |
| `instructions.{number}` | `instructions.3` |
| `instructions.{number}a` (sub-bullet) | `instructions.2a` |
| `safety.{mode}.rules` | `safety.strict.rules` |
| `safety.preamble` | `safety.preamble` |
| `safety.footer` | `safety.footer` |
| `safety.oneshot` | `safety.oneshot` |
| `charts.intro` | `charts.intro` |
| `charts.format` | `charts.format` |
| `charts.timing` | `charts.timing` |
| `persona.fallback` | `persona.fallback` |
| `response.{name}` | `response.parse_error` |

### 5.3 Defaults Delivery

Defaults are **never written to the database**. The Go code holds all
defaults in a single map. When building the prompt or tool definitions,
the system reads from the database first, falls back to the in-code
default. This means:

- A fresh install with zero rows in `agent_loop_config` gets exactly
  the current behavior.
- A user who tweaks one field stores only that one row.
- "Restore All Defaults" deletes every row in the table — the code
  falls back to its hardcoded defaults.

### 5.4 Models Layer

```go
// pkg/models/agent_loop_config.go

package models

type AgentLoopConfig struct {
    // Tool Descriptions
    ToolQueryDatabaseDesc                string `json:"tool_query_database_desc"`
    ToolQueryDatabaseSQLDesc             string `json:"tool_query_database_sql_desc"`
    ToolQueryDatabaseIsExplorationDesc   string `json:"tool_query_database_is_exploration_desc"`
    ToolQueryDatabaseReasoningDesc       string `json:"tool_query_database_reasoning_desc"`
    ToolRespondToUserDesc                string `json:"tool_respond_to_user_desc"`
    ToolRespondToUserTextDesc            string `json:"tool_respond_to_user_text_desc"`
    ToolRenderChartDesc                  string `json:"tool_render_chart_desc"`
    ToolRenderChartConfigDesc            string `json:"tool_render_chart_config_desc"`

    // Instructions
    Instruction1  string `json:"instruction_1"`
    Instruction2  string `json:"instruction_2"`
    Instruction2a string `json:"instruction_2a"`
    Instruction2b string `json:"instruction_2b"`
    Instruction2c string `json:"instruction_2c"`
    Instruction3  string `json:"instruction_3"`
    Instruction4  string `json:"instruction_4"`
    Instruction5  string `json:"instruction_5"`
    Instruction6  string `json:"instruction_6"`

    // Safety Rules
    SafetyPreamble  string `json:"safety_preamble"`
    SafetyStrict    string `json:"safety_strict"`
    SafetyModerate  string `json:"safety_moderate"`
    SafetyRelaxed   string `json:"safety_relaxed"`
    SafetyFooter    string `json:"safety_footer"`
    SafetyOneshot   string `json:"safety_oneshot"`

    // Charts
    ChartsIntro  string `json:"charts_intro"`
    ChartsFormat string `json:"charts_format"`
    ChartsTiming string `json:"charts_timing"`

    // Persona
    PersonaFallback string `json:"persona_fallback"`

    // Tool Response Messages
    ResponseParseError              string `json:"response_parse_error"`
    ResponseExplorationExhausted    string `json:"response_exploration_exhausted"`
    ResponseSafetyRejected          string `json:"response_safety_rejected"`
    ResponseOneshotViolation        string `json:"response_oneshot_violation"`
    ResponseUnknownTool             string `json:"response_unknown_tool"`
    ResponseRenderChartNoPending    string `json:"response_render_chart_no_pending"`
    ResponseRenderChartParseError   string `json:"response_render_chart_parse_error"`
    ResponseRespondParseError       string `json:"response_respond_parse_error"`
    ResponseLoopExhausted           string `json:"response_loop_exhausted"`
    ResponseEmptyTruncated          string `json:"response_empty_truncated"`
}
```

---

## 6. Runtime Injection

### 6.1 Service Layer

```go
// pkg/services/agent_loop_config.go

package services

import (
    "YourQL/pkg/models"
)

var agentLoopDefaults = map[string]string{
    // Tool Descriptions
    "tool.query_database.description":              "Execute a read-only SELECT query against the connected database. Use this when the user's question requires querying data.",
    "tool.query_database.params.sql.description":   "A valid SELECT SQL query. Must be read-only. Always include a LIMIT clause.",
    "tool.query_database.params.is_exploration.description": "Set to true for behind-the-scenes queries the user should NOT see. These are your investigative tools — use them to understand the data, test assumptions, and gather context before committing to a final answer. Set to false ONLY for the single final query whose results should be shown to the user. Once you deliver a final query (is_exploration: false), you cannot run any more queries — use respond_to_user to explain your findings.",
    "tool.query_database.params.reasoning.description": "Optional. Brief internal reasoning about why this query answers the question. This is NOT shown to the user — it exists only to improve query quality via chain-of-thought. Keep it short. Omit if not needed.",
    "tool.respond_to_user.description":              "Provide a direct response to the user. Use this on its own for non-query answers, or alongside query_database to introduce, summarize, or add context to results. Users appreciate when their data comes with a brief explanation — even a single sentence of context makes the experience feel more personal. If you're unsure whether to include commentary with a query result, err on the side of adding it.",
    "tool.respond_to_user.params.text.description":  "The response text. Use markdown formatting for structure.",
    "tool.render_chart.description":                  "Attach a chart visualization to the results of the most recent final query_database call. ONLY call this when the user EXPLICITLY asks for a chart, graph, plot, or visualization. Do NOT call this just because the data is numeric or tabular — the default results table is preferred unless the user specifically requests a visual. Only call AFTER seeing the query result. Do not call this for exploration queries.",
    "tool.render_chart.params.chart_config.description": "A JSON string describing a Chart.js configuration using $column references, in the same shape currently produced by the 'viz_config' field.",
    // ... (all defaults enumerated)
}

// GetAgentLoopConfig returns the merged config — user overrides on top of defaults.
func GetAgentLoopConfig() (*models.AgentLoopConfig, error) {
    rows, err := models.DB.Query("SELECT key, value FROM agent_loop_config")
    if err != nil {
        return nil, fmt.Errorf("failed to query agent loop config: %w", err)
    }
    defer rows.Close()

    userOverrides := make(map[string]string)
    for rows.Next() {
        var k, v string
        if err := rows.Scan(&k, &v); err != nil {
            continue
        }
        userOverrides[k] = v
    }

    c := &models.AgentLoopConfig{}
    // For each field, check userOverrides first, then fall back to agentLoopDefaults
    c.ToolQueryDatabaseDesc = coalesce(userOverrides, "tool.query_database.description")
    c.ToolQueryDatabaseSQLDesc = coalesce(userOverrides, "tool.query_database.params.sql.description")
    // ... (all fields)
    return c, nil
}

func coalesce(overrides map[string]string, key string) string {
    if v, ok := overrides[key]; ok && v != "" {
        return v
    }
    return agentLoopDefaults[key]
}

// SetAgentLoopConfigKey upserts a single config key.
func SetAgentLoopConfigKey(key, value string) error {
    defaultValue, exists := agentLoopDefaults[key]
    if !exists {
        return fmt.Errorf("unknown agent loop config key: %s", key)
    }
    if value == "" || value == defaultValue {
        // Revert to default — delete the override
        _, err := models.DB.Exec("DELETE FROM agent_loop_config WHERE key = ?", key)
        return err
    }
    _, err := models.DB.Exec(
        "INSERT INTO agent_loop_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
        key, value,
    )
    return err
}

// ResetAgentLoopConfig deletes all user overrides, returning to defaults.
func ResetAgentLoopConfig() error {
    _, err := models.DB.Exec("DELETE FROM agent_loop_config")
    return err
}
```

### 6.2 Modifications to `agentic_loop.go`

Three functions change:

**`buildToolSystemPrompt`** — Replace every hardcoded `sb.WriteString(...)`
call with a lookup from the config. Example before/after:

```go
// Before (hardcoded):
sb.WriteString("1. Analyze the user's question and the database schema (if provided).\n")

// After (config-driven):
cfg, _ := GetAgentLoopConfig()
sb.WriteString(cfg.Instruction1 + "\n")
```

**Tool variable definitions** (`queryDatabaseTool`, `respondToUserTool`,
`renderChartTool`) — Move from package-level `var` to a constructor
function that reads from config:

```go
// Before (package-level var):
var queryDatabaseTool = Tool{...}

// After (constructor):
func buildTools(cfg *models.AgentLoopConfig, vizEnabled bool) []Tool {
    tools := []Tool{
        {
            Type: "function",
            Function: FunctionDef{
                Name:        "query_database",
                Description: cfg.ToolQueryDatabaseDesc,
                Parameters: map[string]any{
                    "type": "object",
                    "properties": map[string]any{
                        "sql": map[string]any{
                            "type":        "string",
                            "description": cfg.ToolQueryDatabaseSQLDesc,
                        },
                        // ...
                    },
                    "required": []string{"sql"},
                },
            },
        },
        // respond_to_user, render_chart...
    }
    return tools
}
```

**`runAgenticLoop`** — Replace `tools := []Tool{queryDatabaseTool, respondToUserTool}`
with `tools := buildTools(cfg, conversation.VizEnabled)`. Replace all hardcoded
tool response strings with `cfg.Response*` fields.

### 6.3 Performance Consideration

`GetAgentLoopConfig()` hits SQLite for a `SELECT * FROM agent_loop_config`.
This table will have at most ~40 rows (one per configured field). With
WAL mode and the existing single-connection design, this is sub-millisecond.
No caching layer is needed.

---

## 7. UI Design — Agent Loop Tab

### 7.1 Location and Gating

When the "Enable advanced agent loop settings" toggle on the **General** tab
is off (the default), the Agent Loop tab does not appear in the Settings
sidebar. The tab list shows only: **General**, **LLM Providers**,
**Data Sources**, **Skills**, **Defaults**.

When the toggle is on, a new **Agent Loop** tab appears after Defaults in the
sidebar. Toggling it off hides the tab immediately — no page reload needed.

### 7.2 Layout (Phase 1 — Reduced Surface)

Phase 1 ships only tool descriptions and instruction #3. The tab shows two
collapsible sections, both expanded by default (there's so little on screen
that collapsing would hide all the content).

```
┌──────────────────────────────────────────────────────┐
│  Agent Loop                                          │
│                                                      │
│  Customize what your AI model receives. These are    │
│  advanced settings — changes here directly affect    │
│  how the model behaves. [Restore All Defaults]       │
│                                                      │
│  ┌─ Tool Descriptions ────────────────────────────┐  │
│  │  query_database                                 │  │
│  │  ⓘ The main description of the query_database   │  │
│  │    tool — controls when the model uses it.      │  │
│  │  ┌──────────────────────────────────────────┐   │  │
│  │  │ Execute a read-only SELECT query against  │   │  │
│  │  │ the connected database...                 │   │  │
│  │  └──────────────────────────────────────────┘   │  │
│  │  [Reset]                                        │  │
│  │                                                  │  │
│  │  query_database → is_exploration                 │  │
│  │  ⓘ Controls visibility: true = hidden from      │  │
│  │    user, false = surfaced as the final answer.   │  │
│  │  ┌──────────────────────────────────────────┐   │  │
│  │  │ Set to true for behind-the-scenes...      │   │  │
│  │  └──────────────────────────────────────────┘   │  │
│  │  [Reset]                                        │  │
│  │                                                  │  │
│  │  ... (6 more tool description fields)            │  │
│  └──────────────────────────────────────────────────┘  │
│                                                      │
│  ┌─ Pairing Guidance (Instruction #3) ───────────┐  │
│  │  ⓘ Tells the model when to combine             │  │
│  │    respond_to_user text with query results.     │  │
│  │  ┌──────────────────────────────────────────┐   │  │
│  │  │ Consider pairing respond_to_user with     │   │  │
│  │  │ your final query_database...              │   │  │
│  │  └──────────────────────────────────────────┘   │  │
│  │  [Reset]                                        │  │
│  └──────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

#### 7.2.1 Phase 2 — Expanded Layout

When the remaining sections ship, they appear as additional collapsible
sections (collapsed by default) below the Phase 1 content:

- System Instructions (#1, #2, #2a–c, #4–#6)
- Exploration Safety Rules
- Chart Guidance
- Tool Response Messages
- Fallback Persona

Each section follows the same pattern: label, tooltip, text area, [Reset].
Once Phase 2 ships, the Phase 1 sections (Tool Descriptions, Pairing
Guidance) also collapse by default along with the rest — the "expanded by
default" behavior in §7.2 is specific to the small Phase 1 surface area and
does not carry over once the tab has ~40 fields across 6 sections.

### 7.3 Field Behavior

- **Text area** per field, monospace font, 2–4 visible lines, scrollable.
- **Placeholder text** shows the current default when the user's value is
  empty (or identical to the default).
- **[Reset]** button per field — clears the user's override, reverting to
  the hardcoded default. Disabled (greyed out) when the field is already
  at its default.
- **ⓘ Tooltip** on hover — explains what this field controls and when the
  model sees it. Tooltip copy is authored as static frontend strings
  alongside each field's definition in `SettingsView.svelte` (same pattern
  as existing Settings tooltips elsewhere in the app) — it is NOT fetched
  from the backend. There is no need to round-trip static copy through Go
  and SQLite on every render. Example for `tool.query_database.description`:
  > *"The description of the query_database tool sent to the LLM. This
  > text appears in the tool definition and influences when the model
  > chooses to run a database query. Keep it clear and action-oriented."*
- **[Restore All Defaults]** button at the top of the tab — deletes every
  row in `agent_loop_config`. Requires a confirmation dialog ("This will
  reset all Agent Loop settings to their defaults. This cannot be undone.
  Continue?").
- **Token counter** next to each text area — shows approximate token count
  for the field (character count ÷ 4). Purely informational; no hard limit.
- **Save behavior:** Changes save immediately on blur (no explicit Save
  button). A subtle "Saved" indicator appears briefly after each change.

### 7.4 Wails Bindings

Four new methods on `App` in `app.go`. (No tooltip-fetching binding —
tooltip copy lives in the frontend, per §7.3.)

| Method | Purpose |
|---|---|
| `GetAgentLoopConfig()` → `*models.AgentLoopConfig` | Fetch merged config (defaults + overrides) for the UI |
| `SetAgentLoopConfigKey(key, value string) error` | Save one field |
| `ResetAgentLoopConfigKey(key string) error` | Reset one field to default |
| `ResetAllAgentLoopConfig() error` | Restore All Defaults |

---

## 8. Migration

### 8.1 Database

One new table, created via `runMigration` in `models/database.go`:

```go
_ = runMigration("create_agent_loop_config", func() error {
    _, err := models.DB.Exec(`CREATE TABLE IF NOT EXISTS agent_loop_config (
        key   TEXT PRIMARY KEY,
        value TEXT NOT NULL
    )`)
    return err
})
```

No columns to add, no data to migrate. The table starts empty; the Go
defaults handle everything.

### 8.2 New Files

| File | Purpose |
|---|---|
| `pkg/models/agent_loop_config.go` | `AgentLoopConfig` struct |
| `pkg/services/agent_loop_config.go` | Defaults map, Get/Set/Reset functions |

### 8.3 Modified Files

| File | Changes |
|---|---|
| `pkg/services/agentic_loop.go` | Phase 1: `buildTools()` replaces tool vars; `buildToolSystemPrompt` instruction #3 reads from config. Phase 2: remaining `buildToolSystemPrompt` lines and `runAgenticLoop` tool response strings read from config. |
| `pkg/models/database.go` | Add `create_agent_loop_config` migration |
| `app.go` | Add five Wails-bound methods (§7.4) |
| `frontend/src/SettingsView.svelte` | General tab: add enablement toggle. Agent Loop tab (conditional): Phase 1 sections with text areas, reset buttons, tooltips. |
| `frontend/src/variables.css` | Any new CSS variables (unlikely to need more than existing form styles) |

---

## 9. Risk Assessment

Per `AGENT_READ_FIRST.md` §4.

### 9.0 Absolute Rule Reaffirmed (§0)

**No tool description, instruction, or tool-response-message override can
ever cause a write statement to reach a data source.** Every field this
document makes configurable is *text sent to the model* — it can influence
what SQL the model *proposes*, but every proposed query still passes through
`sql_execution.go`'s unmodified, unconfigurable read-only enforcement before
execution. A user who edits a tool description to say "UPDATE statements are
allowed" produces a model that might *attempt* an UPDATE — and that attempt
is rejected by Go-side validation exactly as it is today. This is stated
explicitly here because it is the single most safety-sensitive property of
this feature and must never be assumed rather than verified in code review.

### 9.1 Impact on Fundamental Goal

| Priority | Impact |
|---|---|
| **#1 Correctness** | **Neutral to slight risk.** Users can degrade answer quality with bad prompts. Mitigated by per-field reset buttons, "Restore All Defaults," and tooltips that explain what each field does. The default behavior is unchanged — a user who never opens this tab gets identical prompts to today. |
| **#2 Safety** | **No risk.** Read-only invariant is enforced in Go code (DML/DDL blocking in `sql_execution.go`), not in prompts. No config field can disable safety gates. |
| **#3 Reliability** | **Low risk.** A user who blanks a critical field (e.g., instruction #5 about read-only SQL) gets the hardcoded default — empty strings revert, not blank out. The `coalesce` function ensures every field always resolves to a non-empty string. |
| **#4 Appeal / Comfort** | **Improved.** Power users who want to tune model behavior get a clean, documented interface. Non-technical users never see it. |

### 9.2 Risk Categories

| Category | Assessment |
|---|---|
| **Answer accuracy** | **Low risk.** Users can worsen answers with bad prompts, but (a) defaults are always one click away, (b) tooltips explain each field, (c) the user explicitly opts into this tab — it's not forced. |
| **Answer delivery** | **No risk.** Prompt construction timing is unchanged. One extra SQLite query (sub-millisecond) before each LLM call. |
| **Data safety** | **No risk.** Safety gates are in Go, not prompts — see §9.0. |
| **User trust** | **Improved.** Giving users transparency and control over what the model receives builds trust. |
| **Regression** | **Low.** All defaults match current hardcoded values exactly. The `coalesce` pattern guarantees no field is ever blank. Existing conversations receive identical prompts post-migration. |
| **UX/Comfort** | **Low risk.** If the Agent Loop tab UI is confusing, users simply don't use it. It's off the default path — no onboarding flow directs users here. |

### 9.3 Phase 2 — Specific Failure Mode: Edited Safety-Mode Descriptions

Phase 2 (§2.3) makes the *descriptions* of strict/moderate/relaxed
exploration modes user-editable, while the Go-side `validateExplorationQuery()`
enforcement logic remains fixed (§3). This creates a foreseeable mismatch:

- **Failure mode:** A user edits the "relaxed" mode description to claim
  something is allowed that Go-side validation still blocks (e.g., they
  loosen the wording to imply DDL is fine, or simply describe a JOIN
  pattern the enforcement code doesn't actually permit in that mode). The
  model, trusting the prompt, attempts the query, gets rejected by the
  safety gate, wastes an exploration round, and may need extra retries to
  recover — degrading reliability (§1 Priority #3) without ever risking
  actual safety (§0's invariant is enforced in code regardless of the
  prompt text).
- **Mitigation:** The tooltip for each safety-mode-description field should
  explicitly warn: "This text does not change what queries are actually
  allowed — it only changes what the model is told. Editing it to describe
  more permissive behavior than the selected safety mode enforces will
  cause the model to attempt queries that get rejected." Reset buttons
  remain available if this happens.
- **Not a §0 violation:** Because enforcement is Go-side and unconfigurable,
  this failure mode costs the user reliability, never safety.

### 9.4 Decision

**Proceed.** The risk is confined to users who explicitly choose to modify
prompts, and every field has a one-click reset. The default experience is
unchanged. The reward — giving power users control and reducing the need
for code changes to tune model behavior — is substantial.

---

## 10. Charter Compliance

Per `AGENT_READ_FIRST.md` §6:

- [x] **Is this change aligned with the fundamental goal?** Yes. Gives
      users control over answer quality without compromising defaults.
- [x] **Is the risk/reward analysis documented?** This document (§9).
- [x] **Files identified.** See §8.3.
- [x] **Read-only invariant preserved?** Yes. Safety gates are Go-side,
      not prompt-side — see §9.0.
- [x] **SQL execution safety preserved?** Yes. Unchanged.
- [x] **Existing conversations backward compatible?** Yes. Defaults match
      current hardcoded values.
- [x] **UI responsive?** One extra SQLite query per LLM call — negligible.
- [x] **Dark/light mode?** Follows existing Settings tab patterns.
- [x] **API keys/passwords not logged?** N/A — no logging changes.
- [x] **Migration safety?** Additive only — new table, no column changes.
      Uses `runMigration()` so it never re-runs.
- [x] **Multi-provider verification planned?** See §11 (new) — required
      before Phase 1 ships, per `AGENT_READ_FIRST.md` §5.2.

---

## 11. Pre-Ship Testing Checklist

Per `AGENT_READ_FIRST.md` §5.2 (Multi-Provider Verification), any change to
tool descriptions or prompts must be tested across providers before shipping
— this applies doubly here, since the *entire point* of this feature is
letting users change that text. Before Phase 1 ships:

- [ ] **Default behavior unchanged.** With zero rows in `agent_loop_config`,
      confirm the assembled system prompt and tool definitions are
      byte-for-byte identical to the current hardcoded output, across all
      four provider paths (OpenAI, Anthropic, Ollama, custom endpoint).
- [ ] **Override behavior works per provider.** Set a non-default value for
      each Phase 1 field and confirm it reaches the model correctly for at
      least OpenAI and one other provider (function-calling schema handling
      differs by provider — Anthropic's tool-use format in particular
      serializes descriptions differently than OpenAI's).
- [ ] **Length limits respected.** Some providers impose length limits on
      function/tool description fields. Confirm the UI's token counter
      (§7.3) surfaces this and that an oversized description degrades
      gracefully (truncation warning, not a silent API error) rather than
      failing the whole request.
- [ ] **Reset-to-default round-trip.** Confirm clicking [Reset] on a field
      immediately reverts behavior on the next LLM call, with no stale
      cached config.
- [ ] **Toggle off preserves data.** Confirm turning off "Enable advanced
      agent loop settings" hides the tab but does not delete
      `agent_loop_config` rows, and turning it back on restores the same
      overrides.

---

## 12. Future Considerations

- **Per-data-source overrides:** The current design is global — one Agent
  Loop config for the entire app. A natural extension is allowing per-data-
  source overrides (analogous to how `SystemPrompt` and `BusinessRules`
  live on `DataSourceConfig`). This would let a user have one set of
  instructions for their production PostgreSQL instance and another for
  their development SQLite file. Out of scope for v0.5.0; the `agent_loop_config`
  table could gain an optional `data_source_id` column in a future migration.
- **Import/Export:** Users may want to share prompt configurations. A
  JSON export/import button in the Agent Loop tab would enable this.
- **Prompt preview:** A "Preview assembled prompt" button that shows the
  complete system prompt as the model would receive it, with the user's
  current overrides applied. Useful for debugging.
- **Token budget visualization:** Show total token count for the system
  prompt and warn if it exceeds common context windows.