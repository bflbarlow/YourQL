# F-18 · Replace Go-emitted inline styles with CSS classes in rendered results HTML

**Severity:** Low-Medium · **Effort:** Medium · **Risk categories:** appeal/comfort (secondary), maintainability

## Problem Statement

`pkg/services/sql_execution.go` builds result HTML server-side with inline
styles hardcoding the design system:

```go
summaryStyle := "cursor:pointer; color:var(--text-secondary); font-size:0.85rem; padding:6px 1rem 8px; display:block;"
divStyle := "margin-top:0;"
...
sb.WriteString(fmt.Sprintf("<details class=\"results-details\" style=\"%s\">…", detailsStyle, …))
```

Problems:

1. **Design system split across languages** — colors/sizing live in
   `frontend/src/variables.css` *and* duplicated in Go string literals.
   Any accent/scale/theme retune requires editing Go and re-releasing the
   binary (§2.4: embedded assets rebuild), defeating CSS's purpose.
2. **Theme audit risk** — charter §3.7/§3.6 require light+dark verification;
   inline styles referencing vars mostly work, but hardcoded paddings/sizes
   ignore the user's UI-scale setting inconsistently across old vs. new
   messages.
3. **Storage bloat & history inconsistency** — rendered HTML persists in
   `conversation_messages.content`. Every styling tweak forks rendering
   between old stored blobs and new ones forever.
4. Several branches already emit near-duplicate style strings differing by a
   couple properties (`hasSummary` vs `HasChart` variants) — copy-paste
   drift has clearly begun.

## Solution Design

Move all presentation into classes; keep Go emitting structure only.

### CSS (frontend/src/results.css, imported by main.js)

```css
.results-details { margin-top: 0; }
.results-details.has-summary { margin-top: .5rem; }
.results-summary {
  cursor: pointer; color: var(--text-secondary);
  font-size: .85rem; padding: 6px 1rem 8px; display: block;
}
.has-summary > .results-summary {
  padding: 4px 8px; background: var(--bg-secondary);
  border-radius: 4px; display: inline-block;
}
.results-body { margin-top: 0; }
.has-summary > .results-body { margin-top: .5rem; }
```

### Go side

```go
cls := "results-details"
if hasSummary { cls += " has-summary" }
sb.WriteString(fmt.Sprintf("<details class=%q><summary class=\"results-summary\">%s (%d rows)</summary><div class=\"results-body\">", cls, label, r.Result.RowCount))
```

Note the variant logic collapses naturally: the two style-string variants
were encoding exactly one boolean — now it's one class modifier.

### Backward compatibility (critical)

Stored historical messages contain old inline-styled HTML. Options:

- **Do nothing special** — browsers render legacy inline styles identically
  to today; they simply never receive future refinements. Acceptable and
  zero-risk. Recommended.
- Optional polish later: tiny DOM-normalizer on message load that strips
  known legacy inline styles so old messages adopt current CSS (defer until
  requested; adds a mutation surface).

Either way there is **no migration**, satisfying §3.2's spirit (no data
rewrites).

## Implementation Plan

1. Inventory every inline-style emission site:
   `grep -n 'style="' pkg/services/sql_execution.go pkg/engine/rendering.go pkg/services/export.go`
2. Create `results.css`; port each property set into a named class; keep
   computed values byte-identical initially (visual no-op).
3. Rewrite emitters; delete dead branches (the empty `if r.SQL != "" {}`
   block above it — see F-19).
4. Manual visual diff: open conversations containing (a) chart+summary,
   (b) summary-only, (c) table-only, in both themes, at default and 125%
   scale. Screenshot compare against pre-change build.
5. Verify export paths (`export.go`, `total_export.go`) still produce
   self-contained HTML — exports must either embed the same stylesheet or
   keep minimal inline styles deliberately; choose embedding
   `<style>results.css contents</style>` in export documents.
6. Both-themes checklist sign-off per §5.1.

## Risk Assessment

- **Visual regression** → mitigated by byte-identical port + screenshot
  comparison; classes are additive.
- **Export breakage** → explicit step 5 prevents the classic "app fine,
  PDFs ugly" outcome.
- Charter: improves #4 without touching #1–#3. Proceed per §4.3 case 3.
