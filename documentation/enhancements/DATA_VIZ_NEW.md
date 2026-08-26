> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Data Visualization Enhancement — Chart as Primary Content

**Date:** 2026-08-12  
**Project:** YourQL — Elevate charts to primary message content with a collapsible raw-data table

---

## 1. Motivation

Currently, when a chart is rendered from a query result:

```
┌─────────────────────────────┐
│  Table (7 row(s))          │
│  ┌──────────┬────────────┐ │
│  │ Classic C│ $3,853,922 │ │
│  │ Vintage │ $1,797,559  │ │
│  │ ...      │ ...        │ │
│  └──────────┴────────────┘ │
├─────────────────────────────┤
│  ┌───────────────────────┐  │
│  │    Bar Chart          │  │
│  │  ████████             │  │
│  │  ██████               │  │
│  └───────────────────────┘  │
└─────────────────────────────┘
```

The table is the primary visual and the chart is appended below it. This is backwards — when
the user explicitly asks for a visualization, the chart should be the star. The raw data
should be accessible but secondary.

**Desired behavior: the chart is primary; the table is collapsed behind a "View Raw Data"
expander.** This mirrors how summarization already works in the codebase: when a summary is
present, the summary text is the primary message content and the table is collapsed behind a
"View raw results (N rows)" `<details>` element.

```
┌─────────────────────────────┐
│  ┌───────────────────────┐  │
│  │    Bar Chart          │  │
│  │  ████████             │  │
│  │  ██████               │  │
│  └───────────────────────┘  │
│                             │
│  ▶ View Raw Data (7 rows)   │
├─────────────────────────────┤
│  ...exploration trace...    │
└─────────────────────────────┘
```

---

## 2. Design

### 2.1 Backend changes

#### 2.1.1 `AssistantResponse.HasChart` (`pkg/services/sql_execution.go`)

Add a `HasChart bool` field to the `AssistantResponse` struct. When true, `ToHTML()` wraps the
results table in a `<details>` element with the summary label "View Raw Data (N rows)" —
exactly the same mechanic used for summaries ("View raw results (N rows)"), but with a
chart-appropriate label.

**Existing summary path (unchanged):**

```
Summary == non-empty  →  table collapsed behind "View raw results (N rows)"
```

**New chart path:**

```
Summary == empty, HasChart == true  →  table collapsed behind "View Raw Data (N rows)"
```

When **both** summary and chart are present, the summary label ("View raw results") takes
precedence since the summary is the primary content and the table is already collapsed.

#### 2.1.2 `renderToolQueryResults` (`pkg/services/agentic_loop.go`)

Sets `HasChart: chartConfig != ""` on the `AssistantResponse` before calling `ToHTML()`.
This is a one-line change to the constructor.

### 2.2 Frontend changes

#### 2.2.1 Render order swap (`frontend/src/ConversationView.svelte`)

Swap the chart and HTML-content render order in the assistant-message template. Currently:

```svelte
<div class="assistant-message">{@html message.content}</div>
{#if getChartConfig(message)}
  <VizChart config={getChartConfig(message)} />
{/if}
```

Changed to:

```svelte
{#if getChartConfig(message)}
  <VizChart config={getChartConfig(message)} />
{/if}
<div class="assistant-message">{@html message.content}</div>
```

Since the backend now wraps the table in a `<details>` when a chart is present, the HTML
content will show the collapsed "View Raw Data" expander below the chart.

If the LLM also provided a `respond_to_user` text alongside the chart, that text appears
in the HTML content between the chart and the "View Raw Data" expander — natural placement.

### 2.3 Interaction with summarization

| Scenario | Primary content | Table display |
|---|---|---|
| No summary, no chart | Table | Full table |
| Summary only | Summary text (markdown) | Collapsed — "View raw results (N rows)" |
| Chart only | Chart (canvas) | Collapsed — "View Raw Data (N rows)" |
| Summary + chart | Summary text (markdown) | Collapsed — "View raw results (N rows)" |
| | Chart (canvas) below summary | |

The chart canvas is rendered by `VizChart.svelte` from `message.metadata.chart_config`,
not from the HTML content. So the chart always renders outside the `<details>` wrapper —
it's never accidentally collapsed.

---

## 3. Implementation Checklist

### Backend

- [ ] Add `HasChart bool` to `AssistantResponse` struct in `sql_execution.go`
- [ ] Modify `ToHTML()` condition: collapse table when `HasChart` is true (no summary) or
      when summary is present (existing behavior)
- [ ] Set `HasChart` in `renderToolQueryResults` when `chartConfig != ""`

### Frontend

- [ ] Swap chart and HTML-content render order in `ConversationView.svelte`

### Testing

- [ ] Ask "create a bar chart of X" — verify chart is primary, "View Raw Data" expander below
- [ ] Ask "summarize this data" (no chart) — verify existing summary behavior unchanged
- [ ] Ask "create a chart and summarize" — verify both work, summary label takes precedence
- [ ] Ask a plain query with no chart — verify full table renders as before
- [ ] Verify the "View Raw Data" expander expands/collapses correctly
- [ ] Verify dark mode styling on the expander summary element

---

## 4. Risk Assessment

| Category | Assessment |
|---|---|
| **Answer accuracy** | No impact — SQL generation and execution unchanged |
| **Answer delivery** | Improved — chart is now the first thing the user sees |
| **Data safety** | No impact — no new code paths touch data sources |
| **Data integrity** | No impact — no schema changes, no new storage |
| **User trust** | Improved — chart-first experience feels more polished |
| **Regression** | Low — only `ToHTML()` condition changes; fallback to existing behavior when no chart |
| **UX/Comfort** | Improved — matches summarization pattern users are familiar with |

**Decision: proceed.** The change is purely presentational (HTML structure + render order),
zero impact on the query pipeline or data safety, and follows an established pattern
(summarization's collapsible table) already tested in the codebase.
