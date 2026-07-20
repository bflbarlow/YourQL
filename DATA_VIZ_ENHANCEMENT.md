# Data Visualization Enhancement Plan

## Overview

Enable YourQL to recognize when a user wants a data visualization and automatically render
an interactive chart alongside (or instead of) the standard result table. The LLM detects
the user's intent, generates a Chart.js configuration object from the query results, and the
frontend renders it using Chart.js — an MIT-licensed, ~80 KB, zero-dependency canvas-based
charting library.

---

## Library Selection: Chart.js v4

| Criteria | Chart.js | ECharts | D3.js | Vega-Lite |
|----------|----------|---------|-------|-----------|
| License | MIT | Apache 2.0 | ISC | BSD-3 |
| Bundle size | ~80 KB | ~1 MB | ~250 KB | ~150 KB |
| Config format | Simple JSON | Complex JSON | JS functions | JSON spec |
| Chart types | 8 core types | 20+ types | Any (manual) | Many |
| Svelte integration | Raw canvas (easy) | echarts-svelte | Manual | vegalite-svelte |
| LLM-friendliness | **Best** — flat JSON, well-documented | Good | Poor (code) | Good but verbose |

**Winner: Chart.js v4.2+**

- Flat, predictable JSON config — easy for LLMs to generate correctly
- MIT license, zero dependencies, tiny bundle
- Core chart types cover 90% of user requests: bar, line, pie/doughnut, scatter/bubble, radar, polarArea
- Responsive canvas rendering works naturally in Svelte with a simple `<canvas>` + `onMount`
- Mature, well-documented, huge community

---

## Architecture

```
User: "Show monthly revenue as a bar chart"
  ↓
LLM generates SQL + <visualization> block
  ↓
┌─────────────────────────────────────────────┐
│              Discussion Engine               │
│                                             │
│  1. Parse LLM response                      │
│  2. Extract <visualization>...</viz> block  │
│  3. Execute SQL, get results                │
│  4. Attach viz config + results to message  │
│     metadata                                │
└─────────────────────────────────────────────┘
  ↓
Message stored in SQLite with metadata JSON
  ↓
┌─────────────────────────────────────────────┐
│              ConversationView.svelte         │
│                                             │
│  1. Check message.metadata.viz_config       │
│  2. If present, render <VizChart> component │
│  3. Show table toggle button                │
└─────────────────────────────────────────────┘
```

---

## Phase 1: LLM Prompt Enhancement

### Prompt Placement Strategy

`buildSystemPrompt()` in `discussion_engine.go` has two zones:

```
Zone A: Custom prompt (if set in data source config) — replaces base persona
        ↓
Zone B: Operational sections appended AFTER Zone A (always present):
        - Database Schema
        - SQL dialect instructions
        - Safety & security rules
        - Visualization instructions  ← NEW
```

**Key design decision:** Viz instructions live in Zone B, so they are never overridden
by a user's custom system prompt. They are operational — like schema, safety rules,
and dialect hints — not persona-defining.

### Viz Toggle in Discussion Settings

Add a `viz_enabled` boolean to the `Conversation` model, mirroring `tech_details`,
`context_details`, and `summarize`:

**Go model** (`pkg/models/conversation.go`):
```go
type Conversation struct {
    // ... existing fields ...
    Summarize       bool  `json:"summarize"`
    VizEnabled      bool  `json:"viz_enabled"`  // NEW: allow/disallow data visualizations
}
```

**SQL migration** (`pkg/models/database.go`):
```go
ensureColumn("conversations", "viz_enabled", "INTEGER DEFAULT 1")
```

**buildSystemPrompt signature update**:
```go
func buildSystemPrompt(schema *DataSchema, hasDB bool, dbConnection *models.DataSource, vizEnabled bool) string
```

When `vizEnabled` is `false`, the viz instructions section is omitted entirely from the
system prompt. The `<visualization>` block parser still runs (in case an older message
has one), but the LLM won't generate new viz blocks.

**Frontend** — add checkbox in the discussion settings panel:
```svelte
<label>
  <input type="checkbox" bind:checked={discussionSettings.vizEnabled} />
  Allow data visualizations (charts)
</label>
```

### System Prompt Addition

The `buildSystemPrompt()` function in `discussion_engine.go` gets a new section:

```
## Data Visualization

You can generate charts when the user asks for visualizations (bar chart, line graph,
pie chart, scatter plot, trend line, etc.) or when a chart would clearly benefit the answer.

To create a visualization, include this block AFTER your SQL query:

<visualization>
{
  "type": "bar",
  "title": "Monthly Revenue by Region",
  "data": {
    "labels": ["$column_name"],
    "datasets": [{
      "label": "Revenue",
      "data": ["$column_name"],
      "backgroundColor": "rgba(54, 162, 235, 0.6)",
      "borderColor": "rgba(54, 162, 235, 1)",
      "borderWidth": 1
    }]
  },
  "options": {
    "responsive": true,
    "plugins": {
      "legend": { "display": true, "position": "top" },
      "title": { "display": true, "text": "Monthly Revenue by Region" }
    },
    "scales": {
      "y": { "beginAtZero": true }
    }
  }
}
</visualization>

Rules:
- "type" must be one of: bar, line, pie, doughnut, scatter, radar, polarArea
- "data.labels" is an array of column names from your SQL result — the category axis
- "data.datasets[].data" is an array of column names — the value axis
- Use "$column_name" syntax to reference SQL result columns (the engine replaces them)
- You can define MULTIPLE datasets for grouped/stacked charts
- For pie/doughnut: labels = category column, data = single value column
- For scatter: data = [{x: "$col1", y: "$col2"}] format
- Choose chart type intelligently:
  * bar = comparisons, rankings, categories
  * line = time series, trends, sequential data
  * pie/doughnut = proportions, composition (≤8 categories)
  * scatter = correlation, distribution, relationship between two variables
- Use semantic colors: blues for single series, distinct colors for multi-series
- Always set responsive: true
- If chart doesn't add value, don't include one
```

### Column Reference Resolution

The LLM uses `"$column_name"` placeholders in the viz config. The discussion engine
replaces them with actual data arrays from the query results:

```go
func resolveVizConfig(configJSON string, columns []string, rows [][]interface{}) (string, error) {
    // Parse the config
    var config map[string]interface{}
    json.Unmarshal([]byte(configJSON), &config)

    // Build column index map
    colIndex := map[string]int{}
    for i, col := range columns {
        colIndex[strings.ToLower(col)] = i
    }

    // Walk the config tree replacing "$column_name" references
    resolved := resolveRefs(config, colIndex, rows)

    return json.Marshal(resolved)
}

func resolveRefs(node interface{}, colIndex map[string]int, rows [][]interface{}) interface{} {
    switch v := node.(type) {
    case string:
        if strings.HasPrefix(v, "$") {
            // Extract column data
            colName := strings.ToLower(v[1:])
            if idx, ok := colIndex[colName]; ok {
                data := make([]interface{}, len(rows))
                for i, row := range rows {
                    if idx < len(row) {
                        data[i] = row[idx]
                    }
                }
                return data
            }
        }
        return v
    case map[string]interface{}:
        result := make(map[string]interface{})
        for k, val := range v {
            result[k] = resolveRefs(val, colIndex, rows)
        }
        return result
    case []interface{}:
        result := make([]interface{}, len(v))
        for i, val := range v {
            result[i] = resolveRefs(val, colIndex, rows)
        }
        return result
    }
    return node
}
```

---

## Phase 2: Backend Changes

### Files Modified

| File | Change |
|------|--------|
| `pkg/services/discussion_engine.go` | Parse `<visualization>` blocks, resolve `$col` refs, attach to metadata |
| `pkg/models/conversation.go` | Add `ChartConfig` field to message metadata struct |
| `pkg/models/database.go` | Ensure `metadata` JSON column exists on `conversation_messages` |

### Response Parsing

The LLM response format:

```
```sql
SELECT region, SUM(amount) as total FROM data GROUP BY region ORDER BY total DESC
```

<visualization>
{
  "type": "bar",
  "title": "Revenue by Region",
  "data": {
    "labels": ["$region"],
    "datasets": [{
      "label": "Revenue",
      "data": ["$total"]
    }]
  }
}
</visualization>

Here's the breakdown by region...
```

The discussion engine already parses SQL from ```sql blocks. We add a second pass
for `<visualization>...</visualization>` using a regex:

```go
var vizRegex = regexp.MustCompile(`(?s)<visualization>\s*(.*?)\s*</visualization>`)

func extractVizConfig(llmResponse string) (string, string) {
    matches := vizRegex.FindStringSubmatch(llmResponse)
    if len(matches) < 2 {
        return llmResponse, "" // no viz block
    }
    cleanResponse := vizRegex.ReplaceAllString(llmResponse, "")
    return cleanResponse, strings.TrimSpace(matches[1])
}
```

### Metadata Storage

The `conversation_messages` table already has a `metadata TEXT` column (JSON). We add a
`chart_config` field:

```json
{
  "chart_config": {
    "type": "bar",
    "data": {
      "labels": ["West", "East", "North"],
      "datasets": [{
        "label": "Revenue",
        "data": [15000, 12000, 8000],
        "backgroundColor": "rgba(54, 162, 235, 0.6)"
      }]
    },
    "options": {
      "responsive": true,
      "plugins": {
        "title": { "display": true, "text": "Revenue by Region" }
      }
    }
  },
  "has_viz": true
}
```

### Processing Flow in `discussion_engine.go`

```
buildSystemPrompt(schema, hasDB, dbConnection, conversation.VizEnabled):
  1. Zone A: Custom prompt (if set) or default persona
  2. Zone B (always appended):
     a. Database Schema (tables, columns, indexes, FKs)
     b. SQL dialect hints
     c. Safety & security rules
     d. IF vizEnabled: Visualization instructions ← NEW
```


```
ProcessUserMessage():
  1. Build system prompt (with viz instructions IF conversation.VizEnabled)
  2. Call LLM
  3. Extract SQL from ```sql blocks
  4. Extract <visualization> block
  5. Execute SQL → get columns + rows
  6. If viz block exists AND conversation.VizEnabled:
     a. Resolve $column_name references → real data arrays
     b. Validate chart type (must be in [bar, line, pie, doughnut, scatter, radar, polarArea])
     c. Set default responsive:true if missing
     d. Store resolved config in message.metadata
  7. If viz block exists BUT vizEnabled is false: ignore it, log warning
  8. Store message
  9. Return to frontend
```

---

## Phase 3: Frontend Changes

### Files Modified

| File | Change |
|------|--------|
| `frontend/src/ConversationView.svelte` | Add `<VizChart>` component usage |
| `frontend/src/VizChart.svelte` | **New** — Chart.js canvas component |
| `frontend/package.json` | Add `chart.js` dependency (~80 KB) |

### VizChart.svelte (New Component)

```svelte
<script>
  import { onMount, onDestroy } from 'svelte';
  import { Chart, registerables } from 'chart.js';

  // Register all Chart.js components
  Chart.register(...registerables);

  let { config, results } = $props();
  let canvas;
  let chart;

  onMount(() => {
    if (canvas && config) {
      chart = new Chart(canvas, config);
    }
  });

  onDestroy(() => {
    if (chart) chart.destroy();
  });

  // Re-render when config changes
  $effect(() => {
    if (chart && config) {
      chart.destroy();
      chart = new Chart(canvas, config);
    }
  });
</script>

<div class="viz-chart-container">
  <canvas bind:this={canvas}></canvas>
</div>

<style>
  .viz-chart-container {
    position: relative;
    width: 100%;
    max-height: 500px;
    margin: 1rem 0;
    padding: 1rem;
    background: var(--bg-secondary);
    border-radius: 8px;
    border: 1px solid var(--border-color);
  }
  canvas {
    max-height: 450px;
  }
</style>
```

### ConversationView.svelte — Message Rendering

Add conditional rendering after the SQL results table:

```svelte
{#if message.metadata?.chart_config}
  <div class="message-viz">
    <div class="viz-controls">
      <button class="btn btn-small" onclick={toggleVizView}>
        {showViz ? '📊 Chart' : '📋 Table'} (click to switch)
      </button>
    </div>
    {#if showViz}
      <VizChart config={message.metadata.chart_config} />
    {:else}
      <ResultsTable results={parseResults(message.sql_results)} />
    {/if}
  </div>
{/if}
```

### Toggle Behavior

- Default: show chart if viz config exists, with toggle to table
- `showViz` state per message, defaulting to `true` when chart_config is present
- If user clicks "Table", show the result table and remember preference
- Both chart and table use the same underlying data (from query results)

---

## Phase 4: Chart Type Intelligence

The LLM chooses chart types based on heuristics baked into the system prompt:

### Type Selection Rules (in system prompt)

| User Request | Data Pattern | Chart Type |
|-------------|-------------|------------|
| "compare", "ranking", "vs", "breakdown" | Categorical + value | `bar` (vertical) |
| "over time", "trend", "growth", "daily/monthly" | Temporal + value | `line` |
| "proportion", "share", "percentage", "breakdown of" | Single column, ≤8 unique values | `pie` or `doughnut` |
| "relationship", "correlation", "vs" with two numerics | Two numeric columns | `scatter` |
| "distribution", "spread" | Single numeric column | `bar` (histogram) |
| Implicit: 1 cat + 1+ values, small cardinality | General comparison | `bar` (default) |

### Chart.js Configuration Defaults (system prompt)

```
Default color palette (for single dataset):
  backgroundColor: 'rgba(54, 162, 235, 0.6)'
  borderColor: 'rgba(54, 162, 235, 1)'

Multi-dataset palette:
  Dataset 1: rgba(54, 162, 235, 0.6)  — blue
  Dataset 2: rgba(255, 99, 132, 0.6)  — red
  Dataset 3: rgba(75, 192, 192, 0.6)  — teal
  Dataset 4: rgba(255, 159, 64, 0.6)  — orange
  Dataset 5: rgba(153, 102, 255, 0.6) — purple

Accessibility:
  - Always set responsive: true
  - Use maintainAspectRatio: false with fixed container height
  - Pie charts: include legend, position: 'right' for >5 segments
  - Bar/line charts: y.beginAtZero: true unless data is all negative
  - Include axis labels when axes have clear meaning
```

---

## Phase 5: Error Handling & Edge Cases

### Graceful Degradation

| Scenario | Behavior |
|----------|----------|
| Viz JSON is invalid | Log warning, show table only |
| `$column_name` not found in results | Log warning, skip viz, show table |
| Chart.js fails to render | Try/catch in VizChart, show fallback message |
| User asked for viz but LLM didn't include one | Normal table display (no error) |
| Empty results | No viz rendered (no data to chart) |
| Very large datasets (>10,000 points) | LLM should aggregate or the engine can sample |

### Viz JSON Validation (Backend)

```go
func validateVizConfig(config map[string]interface{}) error {
    validTypes := map[string]bool{
        "bar": true, "line": true, "pie": true, "doughnut": true,
        "scatter": true, "radar": true, "polarArea": true,
    }
    chartType, _ := config["type"].(string)
    if !validTypes[chartType] {
        return fmt.Errorf("invalid chart type: %s", chartType)
    }
    // Ensure data object exists
    if config["data"] == nil {
        return fmt.Errorf("missing data field")
    }
    return nil
}
```

### Data Size Limits

```go
const maxChartDataPoints = 5000

func sampleDataForChart(rows [][]interface{}, maxPoints int) [][]interface{} {
    if len(rows) <= maxPoints {
        return rows
    }
    // Evenly sample rows
    step := float64(len(rows)) / float64(maxPoints)
    sampled := make([][]interface{}, maxPoints)
    for i := 0; i < maxPoints; i++ {
        sampled[i] = rows[int(float64(i)*step)]
    }
    return sampled
}
```

---

## Phase 6: Future Enhancements (out of scope for v1)

| Feature | Priority | Notes |
|---------|----------|-------|
| User-customizable colors | Low | Store palette preferences in settings |
| Chart export (PNG/SVG) | Medium | Chart.js `toBase64Image()`, trigger download |
| Multiple charts per response | Low | Array of `<visualization>` blocks |
| Annotations (threshold lines, highlights) | Low | chartjs-plugin-annotation |
| Streaming chart data | Low | For real-time data sources |
| Dashboard view | High (v2) | Pin charts to a persistent dashboard |

---

## Implementation Order

| Step | Phase | Effort | Depends On |
|------|-------|--------|------------|
| 1. Add `viz_enabled` to Conversation model | Phase 1 | Small | — |
| 2. Add SQL migration for viz_enabled column | Phase 1 | Small | Step 1 |
| 3. Add viz section to system prompt (Zone B) | Phase 1 | Small | Step 1 |
| 4. Update buildSystemPrompt signature + wiring | Phase 1 | Small | Steps 2, 3 |
| 5. Parse `<visualization>` blocks from LLM response | Phase 2 | Small | Step 4 |
| 6. Implement `$column` reference resolution | Phase 2 | Medium | Step 5 |
| 7. Store chart_config in message metadata | Phase 2 | Small | Step 6 |
| 8. Install chart.js & create VizChart.svelte | Phase 3 | Small | — |
| 9. Wire VizChart into ConversationView | Phase 3 | Medium | Steps 7, 8 |
| 10. Add viz toggle to discussion settings UI | Phase 1 | Small | Step 3 |
| 11. Add table/chart toggle button | Phase 3 | Small | Step 9 |
| 12. Validation & error handling | Phase 5 | Small | Step 7 |
| 13. Test with various chart types | Testing | Medium | All |

### Total Effort Estimate

- **Backend (Go):** ~200 lines new code across 3 files
- **Frontend (Svelte):** ~100 lines new code across 2 files
- **Dependencies:** 1 new npm package (`chart.js`, ~80 KB)
- **Go dependencies:** None (uses existing `encoding/json`)
- **Total time:** 4-6 hours

---

## Summary

| Aspect | Detail |
|--------|--------|
| Library | Chart.js v4.2+ (MIT, ~80 KB) |
| Config format | LLM generates simple JSON with `$column` placeholders |
| Resolution | Backend replaces `$column` refs with actual query result arrays |
| Storage | `chart_config` in `conversation_messages.metadata` JSON |
| Rendering | New `VizChart.svelte` component with `<canvas>` |
| Toggle | User can switch between chart and table view |
| Discussion setting | `viz_enabled` boolean — disables viz instructions in prompt + ignores viz blocks |
| Prompt placement | Zone B (operational) — never overridden by custom data source prompt |
| Error handling | Graceful fallback to table on any viz failure |
| Chart types | bar, line, pie, doughnut, scatter, radar, polarArea |
| Go changes | ~4 files modified, ~250 lines |
| Frontend changes | ~3 files (1 new, 2 modified), ~130 lines |
| Dependencies | 1 npm package, 0 Go packages |