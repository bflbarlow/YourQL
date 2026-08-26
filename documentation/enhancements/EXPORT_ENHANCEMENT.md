> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Export Enhancement

**Date:** 2026-08-07
**Project:** YourQL — Replace single print button with a three-option export dropdown

---

## 1. Motivation

The current export mechanism is a single button in the conversation header that opens the
OS print dialog (`runtime.WindowPrint` / `window.print()`). This has two problems:

1. **Pagination is uncontrollable.** The browser paginates to physical paper dimensions
   (A4/Letter). Long conversations inevitably span multiple pages. There is no way to
   produce a single continuous-page export through the print dialog.

2. **One format only.** Users who want a portable file (HTML to open in any browser,
   Markdown to paste into docs/Notion) have no option. The only alternative is the
   per-result-table CSV download, which captures data but not the conversation itself.

### What Users Want

- A **single-page** artifact they can scroll through, search, and archive
- **Multiple formats** — HTML for fidelity, Markdown for portability, PDF for formal sharing
- A workflow that doesn't force them through the OS print dialog if they just want a file

---

## 2. Design

### 2.1 Three-option dropdown

Replace the single export button in `ConversationView.svelte` with a dropdown that
reveals three options:

| Option | Icon | Action | Format |
|---|---|---|---|
| Print to PDF | Printer | Existing `ExportConversationPDF()` | PDF via OS dialog |
| Export as HTML | Code/File | New `ExportConversationHTML(conversationID)` | `.html` file |
| Export as Markdown | FileText | New `ExportConversationMarkdown(conversationID)` | `.md` file |

The dropdown uses the existing `FileDown` icon as the button face and a popover
menu for the three options. This matches the pattern already used for the gear
(settings) button in the conversation header.

### 2.2 Export as HTML

Generate a standalone `.html` file containing the entire conversation as a single
scrollable page. The file is self-contained — all CSS is embedded inline, no external
dependencies.

**Data source:** The `conversation_messages` table already stores rendered HTML in
the `content` column. Assistant messages contain markup like
`<div class="markdown-content">...</div>`, `<div class="results-card">...</div>`,
and `<details>` blocks for exploration traces. User messages are plain text.

**Approach:**

1. Fetch all messages for the conversation from `GetConversationMessages()`
2. Build an HTML document with embedded CSS that mirrors the app's light-mode theme
3. Each message is included as-is (the `content` HTML) wrapped in message-bubble `<div>`s
4. User messages get the accent-colored bubble style; assistant messages get the
   light card style
5. Use `runtime.SaveFileDialog` to let the user pick a save location
6. Write the file to disk

**CSS considerations:**
- Use the light-mode color palette (prints better, users expect it)
- Keep the same typography and spacing as the app
- Tables, code blocks, and exploration traces render correctly (they're just HTML)
- No JavaScript, no interactivity — static and portable

**Charts:** Same constraint as Markdown export — chart visualizations live in
`VizChart.svelte` as a client-side `<canvas>` + Chart.js render, driven by metadata that
is separate from the stored `content` HTML. The exported `content` for a chart-bearing
message will not include the chart. Render a static placeholder block in its place:

```html
<div class="chart-placeholder">📊 Chart: {chart type} — view in the app for the interactive visualization</div>
```

A future enhancement could rasterize the chart to a `<img>` (e.g., via a headless canvas
render) before export, but that is out of scope here — flag it as a follow-up, not a
silent gap.

### 2.3 Export as Markdown

Convert the conversation to a plain `.md` file. User messages are already plain text
and can be emitted directly. Assistant messages are HTML and need conversion.

**Conversion approach:**

A targeted, focused converter that handles the specific HTML patterns YourQL emits —
no full HTML→MD library needed. The patterns are well-known and finite:

| HTML Pattern | Markdown Output |
|---|---|
| `<div class="markdown-content"><h2>...</h2></div>` | `## ...` |
| `<div class="markdown-content"><h3>...</h3></div>` | `### ...` |
| `<div class="markdown-content"><p>...</p></div>` | Plain text paragraph |
| `<div class="markdown-content"><strong>...</strong></div>` | `**...**` |
| `<div class="markdown-content"><em>...</em></div>` | `*...*` |
| `<div class="markdown-content"><ul><li>...</li></ul></div>` | `- ...` |
| `<div class="markdown-content"><ol><li>...</li></ol></div>` | `1. ...` |
| `<div class="markdown-content"><hr></div>` | `---` |
| `<div class="markdown-content"><table>...</table></div>` | Markdown pipe table |
| `<div class="markdown-content"><pre><code>...</code></pre></div>` | Fenced code block |
| `<div class="results-card"><table>...</table></div>` | Markdown pipe table |
| `<details><summary>...</summary>...</details>` | `> **...**` blockquote + content |
| Plain text user message | Blockquote: `> ...` |

**File structure:**

```markdown
# YourQL Conversation: {title}
**Date:** {date} · **Provider:** {provider} · **Data Source:** {data source}

---

## User
> {message text}

## Assistant
{converted markdown content}

---

## User
> {message text}

...
```

**Resolving header metadata:** `models.Conversation.Title` is `*string` — nil-safe fallback
to `"Untitled"`, matching the frontend's existing
`activeConversation?.title || 'Untitled'` pattern. `LLMProviderID` and `DataSourceID` are
both `*uint` and may be nil (a conversation is not required to have either set). When
present, resolve the display name via `GetLLMProviderByID` / `GetDataSourceByID`; when
nil, omit that line from the header entirely rather than printing "Provider: none" or
similar filler.

**Charts:** Chart visualizations are rendered client-side by `VizChart.svelte` via
`<canvas>` + Chart.js — they are **not** part of the stored `content` HTML and cannot be
reconstructed by a server-side HTML→MD converter. When a message has chart metadata
(the same data `getChartConfig(message)` reads on the frontend), emit a placeholder line
instead of attempting conversion:

```markdown
_[Chart: {chart type} — view in the app to see the interactive visualization]_
```

This keeps chart-bearing messages from silently vanishing or crashing the converter on
unexpected `<canvas>`-adjacent markup (there is none in `content`, but the metadata must
still be handled explicitly rather than ignored).

### 2.4 Print to PDF (existing, improved)

The existing `ExportConversationPDF()` method is preserved as-is. Additionally, the
`@media print` CSS in `ConversationView.svelte` receives small improvements:

| Change | Rationale |
|---|---|
| Add `@page { margin: 0.5in; }` | Maximize usable page area |
| Remove `break-inside: avoid` on all `.message` | Prevents large gaps when a single message spans most of a page |
| Add a visible print header with conversation title + date | Identifies the export |
| Show collapsed exploration traces in print | Full fidelity |

These changes reduce wasted paper and only modify the existing `@media print` block —
with one exception. Keeping a user question visually grouped with its answer would
require a `.message-pair` wrapper class, but **no such wrapper exists in the current
markup** — `ConversationView.svelte` renders a flat list of `<div class="message
{message.role}">` elements with no DOM grouping of a question with its answer. Grouping
Q&A pairs on the printed page is therefore **out of scope** for this pass; it would
require a template change to introduce the wrapper (a small, separate frontend change,
not bundled into this CSS-only cleanup). Flag as a possible follow-up, not a silent gap.

---

## 3. Implementation Plan

### 3.1 Files Affected

| File | Change | Lines (est.) |
|---|---|---|
| `frontend/src/ConversationView.svelte` | Replace single button with dropdown; wire three onclick handlers | ~30 |
| `app.go` | Add `ExportConversationHTML()` and `ExportConversationMarkdown()` Wails-bound methods | ~130 |
| `pkg/services/export.go` (new) | HTML template builder and HTML→MD converter | ~120 |
| `pkg/services/conversation.go` | Existing — no changes; `GetConversationMessages()` is already available | 0 |

### 3.2 Go Bindings (`app.go`)

Two new methods on `*App`, matching the existing `ExportConversationPDF()` pattern:

```go
// ExportConversationHTML generates a standalone HTML file for the
// conversation and opens a save dialog.
func (a *App) ExportConversationHTML(conversationID uint) error

// ExportConversationMarkdown generates a Markdown file for the
// conversation and opens a save dialog.
func (a *App) ExportConversationMarkdown(conversationID uint) error
```

Both methods:
1. Fetch conversation metadata (`GetConversationByID`)
2. Fetch all messages (`GetConversationMessages`)
3. Build the output (HTML or Markdown) using `services` helpers
4. Call `runtime.SaveFileDialog(ctx, SaveDialogOptions{...})` with the appropriate
   default filename and extension filter (`.html` or `.md`)
5. `SaveFileDialog` returns `(string, error)`. An **empty string with a nil error means
   the user cancelled** — this is not an error condition. In that case, return `nil`
   immediately and skip the write. Only a non-nil `error` return (or a write failure)
   should be surfaced to the user as an actual error.
6. Write the file to the returned path if non-empty

### 3.3 HTML Template (`pkg/services/export.go`)

```go
func BuildConversationHTML(conversation *models.Conversation, messages []*models.ConversationMessage) string
```

Generates a complete HTML document:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>YourQL - {title}</title>
  <style>
    /* Embedded light-mode CSS matching the app's design */
  </style>
</head>
<body>
  <header>
    <h1>{title}</h1>
    <p>{date} · {provider} · {data source}</p>
  </header>
  <main>
    <!-- Each message rendered in a styled bubble -->
  </main>
</body>
</html>
```

### 3.4 Markdown Converter (`pkg/services/export.go`)

```go
func ConvertHTMLToMarkdown(htmlContent string) string
```

A focused converter that handles the specific HTML patterns in §2.3. Uses
Go's `html/template`-safe parsing (no `regexp` on full HTML — walk a tokenized
tree or use targeted `strings.Replace` for simple patterns). User messages
(plain text) are blockquoted directly without conversion.

### 3.5 Frontend (`ConversationView.svelte`)

The current single button:

```svelte
<button class="export-btn-header" onclick={...}>
  <FileDown size={16} />
</button>
```

Becomes a dropdown triggered by the same button. On click, a small popover
menu appears with three items. Each item calls the corresponding Go binding.
The popover closes on click or blur.

---

## 4. Risk/Reward Analysis

### 4.1 Fundamental Goal Impact

**No impact on answer correctness or delivery.** Export is a post-answer
operation — it reads from the already-persisted conversation messages.
It does not touch the LLM pipeline, SQL execution, or schema introspection.

### 4.2 Risk Assessment

| Category | Risk | Mitigation |
|---|---|---|
| Answer accuracy | None | Export reads already-rendered messages; no computation |
| Answer delivery | None | No change to the message rendering or streaming paths |
| Data safety | Low | Export writes to a user-chosen file path, never a data source |
| Data integrity | None | Read-only on `conversation_messages` table |
| User trust | Positive | More export options increase trust and utility |
| Regression | Low | Existing `ExportConversationPDF()` is unchanged; new methods are additive |
| UX/Comfort | Positive | Dropdown is a familiar pattern; three clear options |

### 4.3 Absolute Rules Check

- **Read-only invariant:** ✅ Export only reads `conversation_messages` and writes to a user-chosen local file. No data source interaction.
- **No API key/password logging:** ✅ No sensitive data in export output (credentials are in `llm_providers` and `data_sources` tables, not in messages).
- **No destructive schema changes:** ✅ No schema changes at all.

### 4.4 Decision

**Proceed.** Additive feature with no impact on the fundamental goal pipeline.
Low risk, clear user value, minimal code surface.

---

## 5. Testing Checklist

- [ ] HTML export opens correctly in Chrome, Firefox, Safari, Edge
- [ ] HTML export renders all message types: user text, assistant markdown, result tables, exploration traces, error messages
- [ ] HTML export shows the chart placeholder (not a blank gap or crash) for chart-bearing messages
- [ ] Markdown export shows the chart placeholder text for chart-bearing messages
- [ ] Conversations with no LLM provider set and/or no data source set export a clean header (no "undefined"/"null"/empty-string artifacts)
- [ ] Conversations with an untitled (`null`) title export as "Untitled", matching in-app display
- [ ] Cancelling the Save dialog (empty path, nil error) is treated as a no-op, not an error toast
- [ ] HTML export handles conversations with 0 messages (empty), 1 message, 50+ messages
- [ ] HTML export preserves special characters, emoji, and non-ASCII text
- [ ] Markdown export produces valid, readable `.md` for all message types
- [ ] Markdown tables from query results are well-formed
- [ ] Markdown code blocks from SQL blocks are properly fenced
- [ ] Print-to-PDF still works after CSS changes
- [ ] Print-to-PDF doesn't waste paper with large gaps
- [ ] Dropdown button works with keyboard (Tab, Enter, Escape)
- [ ] Dropdown closes on blur and on selection
- [ ] Dropdown respects the UI scale setting (small/medium/large)
- [ ] Both light and dark themes render the dropdown correctly
- [ ] Canceling the Save dialog does not crash or leave partial state
- [ ] File dialog shows the correct extension filter (`.html` or `.md`)

---

## 6. Notes

- The HTML export intentionally uses the light-mode color palette regardless of
  the user's active theme. Light mode prints better, and users can always toggle
  dark mode if they open the file in a browser with Dark Reader or similar.
- The Markdown converter does not need to handle arbitrary HTML — only the
  specific patterns YourQL's Go backend emits. This keeps the converter small
  and avoids pulling in a third-party HTML→MD library.
- The `runtime.SaveFileDialog` API in Wails v2 returns the chosen path as a
  string (empty if cancelled). The Go backend writes the file directly — no
  need to shuttle file content back to the frontend.
- Per-result-table CSV export (the existing `exportCSV` function in
  `ConversationView.svelte`) is unchanged and remains available as a separate
  control on each results card.