package engine

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/gomarkdown/markdown"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

var wordRegex = regexp.MustCompile(`([A-Z]+[a-z]*|[a-z]+|\d+)`)

// HumanizeColumnName converts database column names to human-readable labels.
func HumanizeColumnName(col string) string {
	col = strings.ReplaceAll(col, "_", " ")
	col = strings.ReplaceAll(col, "-", " ")

	words := wordRegex.FindAllString(col, -1)
	if words == nil {
		if col == "" {
			return col
		}
		return strings.ToUpper(col[:1]) + strings.ToLower(col[1:])
	}

	var result []string
	for _, w := range words {
		if w == "" {
			continue
		}
		if strings.ToUpper(w) == w && len(w) > 1 {
			result = append(result, w)
		} else {
			result = append(result, strings.ToUpper(w[:1])+strings.ToLower(w[1:]))
		}
	}
	return strings.Join(result, " ")
}

// FormatResults converts QueryResult into a human-readable markdown string.
func FormatResults(result *QueryResult) string {
	if result.RowCount == 0 {
		return "No rows returned."
	}

	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = HumanizeColumnName(col)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%d row(s) returned**\n\n", result.RowCount))

	colWidths := make([]int, len(humanizedCols))
	for i, col := range humanizedCols {
		if len(col) > colWidths[i] {
			colWidths[i] = len(col)
		}
	}
	for _, row := range result.Rows {
		for i, val := range row {
			str := fmt.Sprintf("%v", val)
			if len(str) > colWidths[i] {
				colWidths[i] = len(str)
			}
		}
	}

	for i, col := range humanizedCols {
		sb.WriteString("| ")
		sb.WriteString(padRight(col, colWidths[i]))
		sb.WriteString(" ")
	}
	sb.WriteString("|\n")
	for i := range humanizedCols {
		sb.WriteString("| ")
		sb.WriteString(padRight("", colWidths[i], '-'))
		sb.WriteString(" ")
	}
	sb.WriteString("|\n")

	for _, row := range result.Rows {
		for i, val := range row {
			sb.WriteString("| ")
			sb.WriteString(padRight(fmt.Sprintf("%v", val), colWidths[i]))
			sb.WriteString(" ")
		}
		sb.WriteString("|\n")
	}

	return sb.String()
}

// AssistantResponse holds the structured data for an assistant message.
type AssistantResponse struct {
	Explanation     string
	SQL             string
	Result          *QueryResult
	ExplorationHTML string
	Summary         *string
	HasChart        bool // when true, table is collapsed behind a "View Raw Data" expander
}

// mdRenderer is a shared stateless markdown renderer.
var mdRenderer = mdhtml.NewRenderer(mdhtml.RendererOptions{
	Flags: mdhtml.UseXHTML,
})

// fencedCodeRe matches a fenced code block opening that follows a non-blank line.
var fencedCodeRe = regexp.MustCompile(`([^\n])\n(\x60{3,})`)

// normalizeMarkdown preprocesses LLM-generated markdown to avoid common parser failures.
func normalizeMarkdown(text string) string {
	return fencedCodeRe.ReplaceAllString(text, "$1\n\n$2")
}

// RenderMarkdown converts markdown text to safe HTML.
func RenderMarkdown(text string) string {
	if text == "" {
		return ""
	}
	text = normalizeMarkdown(text)
	p := parser.NewWithExtensions(parser.CommonExtensions | parser.NoEmptyLineBeforeBlock)
	return string(markdown.ToHTML([]byte(text), p, mdRenderer))
}

// ToHTML renders the assistant response as HTML.
func (r *AssistantResponse) ToHTML() string {
	var sb strings.Builder
	summaryHTML := ""
	if r.Summary != nil && *r.Summary != "" {
		summaryHTML = fmt.Sprintf("<div class=\"markdown-content\">%s</div>\n", RenderMarkdown(*r.Summary))
		if !r.HasChart {
			sb.WriteString(summaryHTML)
		}
	}
	if r.Result != nil {
		hasSummary := r.Summary != nil && *r.Summary != ""
		if hasSummary || r.HasChart {
			label := "View Raw Data"
			detailsStyle := "margin-top:0;"
			summaryStyle := "cursor:pointer; color:var(--text-secondary); font-size:0.85rem; padding:6px 1rem 8px; display:block;"
			divStyle := "margin-top:0;"
			if hasSummary {
				label = "View raw results"
				detailsStyle = "margin-top:0.5rem;"
				summaryStyle = "cursor:pointer; color:var(--text-secondary); font-size:0.85rem; padding:4px 8px; background:var(--bg-secondary); border-radius:4px; display:inline-block;"
				divStyle = "margin-top:0.5rem;"
			}
			sb.WriteString(fmt.Sprintf("<details class=\"results-details\" style=\"%s\"><summary style=\"%s\">%s (%d rows)</summary><div style=\"%s\">", detailsStyle, summaryStyle, label, r.Result.RowCount, divStyle))
			if r.HasChart && r.Summary != nil && *r.Summary != "" {
				sb.WriteString(summaryHTML)
			}
			sb.WriteString(FormatResultsHTML(r.Result, r.SQL))
			sb.WriteString("</div></details>")
		} else {
			sb.WriteString(FormatResultsHTML(r.Result, r.SQL))
		}
	}
	if r.ExplorationHTML != "" {
		sb.WriteString(r.ExplorationHTML)
	}
	return sb.String()
}

// FormatResultsHTML converts QueryResult into an HTML table.
func FormatResultsHTML(result *QueryResult, sqlQuery string) string {
	if result.RowCount == 0 {
		return "<p>No rows returned.</p>"
	}

	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = HumanizeColumnName(col)
	}

	hash := sqlQueryHash(sqlQuery)

	const visibleRows = 10
	totalRows := result.RowCount
	hasMore := totalRows > visibleRows

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<div class=\"results-card\" style=\"margin:0.5rem 0;\">"))

	sb.WriteString(`<div class="results-toolbar" style="display:flex; align-items:center; gap:0.5rem; padding:6px 10px; background:var(--bg-surface); border:1px solid var(--border-primary); border-radius:6px; margin-bottom:0.5rem; flex-wrap:wrap;">`)

	sb.WriteString(fmt.Sprintf(`<span class="row-count" style="font-size:0.85rem; color:var(--text-secondary); font-weight:500;">%d row(s)</span>`, result.RowCount))

	sb.WriteString(fmt.Sprintf(`<span class="sql-toggle" style="font-size:0.8rem; color:var(--text-secondary);"><button class="sql-toggle-btn" onclick="toggleSQLSection(this, 'sql-popover-%d')" style="cursor:pointer; padding:2px 8px; background:var(--bg-primary); border:1px solid var(--border-primary); border-radius:4px; font-size:0.75rem; color:var(--text-secondary); user-select:none;">SQL</button></span>`, hash))

	sb.WriteString(fmt.Sprintf(`<button class="csv-btn" onclick="exportCSV(this, %d)" style="font-size:0.8rem; padding:4px 10px; border:1px solid var(--border-primary); border-radius:4px; background:var(--bg-primary); cursor:pointer; color:var(--text-secondary);">↓ CSV</button>`, result.RowCount))

	sb.WriteString(fmt.Sprintf(`<div id="sql-popover-%d" style="display:none; width:100%%; margin-top:0.5rem; padding:0.75rem 1rem; background:var(--bg-primary); border:1px solid var(--border-primary); border-radius:6px;"><pre style="margin:0; padding:0; font-size:0.8rem; overflow-x:auto;"><code id="sql-code-%d">%s</code></pre><button class="copy-sql-btn" onclick="copySQL('sql-code-%d')" style="margin:0.5rem 0 0 0; font-size:0.75rem; padding:4px 10px; border:1px solid var(--border-primary); border-radius:4px; background:var(--bg-primary); cursor:pointer; color:var(--text-secondary);">Copy</button></div>`, hash, hash, html.EscapeString(sqlQuery), hash))
	sb.WriteString(`</div>`)

	sb.WriteString(`<div class="table-container" style="overflow-x: auto;">`)
	dataJSON, _ := json.Marshal(map[string]interface{}{
		"columns": result.Columns,
		"rows":    result.Rows,
	})
	sb.WriteString(fmt.Sprintf(`<table class="result-table sortable" data-sort-rows="%s" style="border-collapse: collapse; width: 100%%;">`, html.EscapeString(string(dataJSON))))

	sb.WriteString(`<thead><tr>`)
	for i := range result.Columns {
		humanized := humanizedCols[i]
		sb.WriteString(fmt.Sprintf(`<th class="sort-header" data-col="%d" style="border:1px solid var(--border-primary); padding:10px 12px; text-align:left; background:var(--bg-surface); position:sticky; top:0; z-index:2; font-weight:600; user-select:none; white-space:nowrap; cursor:pointer;">%s <span class="sort-indicator"></span></th>`,
			i, html.EscapeString(humanized)))
	}
	sb.WriteString(`</tr></thead>`)
	sb.WriteString(`<tbody>`)

	for i, row := range result.Rows {
		rowClass := "result-row"
		rowStyle := ""
		if hasMore && i >= visibleRows {
			rowClass += fmt.Sprintf(" collapsed-row-%d", hash)
			rowStyle = ` style="display:none;"`
		}
		sb.WriteString(fmt.Sprintf(`<tr class="%s"%s>`, rowClass, rowStyle))
		for _, val := range row {
			cell := fmt.Sprintf("%v", val)
			cellClass := ""
			if isNumber(cell) {
				cellClass = "num-cell"
			} else if isDate(cell) {
				cellClass = "date-cell"
			}
			sb.WriteString(fmt.Sprintf(`<td class="%s" style="border:1px solid var(--border-primary); padding:8px 12px; max-width:400px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" title="%s">%s</td>`,
				cellClass, html.EscapeString(cell), html.EscapeString(cell)))
		}
		sb.WriteString(`</tr>`)
	}

	sb.WriteString(`</tbody></table>`)

	if hasMore {
		sb.WriteString(fmt.Sprintf(`<div style="margin-top:0.5rem;"><button class="table-expand-btn" onclick="var rs=this.closest('.results-card').querySelectorAll('.collapsed-row-%d');var ex=rs.length&&rs[0].style.display!=='none';rs.forEach(function(r){r.style.display=ex?'none':''});this.innerHTML=ex?'Show all %d rows &#9660;':'Show first 10 rows &#9650;'" style="font-size:0.8rem; padding:4px 12px; border:1px solid var(--border-primary); border-radius:4px; background:var(--bg-surface); cursor:pointer; color:var(--text-secondary);">Show all %d rows &#9660;</button></div>`, hash, totalRows, totalRows))
	}

	sb.WriteString(`</div>`)
	return sb.String()
}

// BuildCollapsibleSQLBlockHTML returns HTML for a collapsible SQL code block.
func BuildCollapsibleSQLBlockHTML(sqlQuery string) string {
	hash := sqlQueryHash(sqlQuery)
	return fmt.Sprintf(`<details class="sql-block" style="margin: 1rem 0;">
<summary style="cursor:pointer; color:var(--text-secondary); font-size:0.9rem; padding:0.5rem 0.75rem; background:var(--bg-secondary); border-radius:6px; display:flex; justify-content:space-between; align-items:center;">
  <span>Show SQL</span>
</summary>
<pre style="margin:0.5rem 0; padding:1rem; background:var(--bg-tertiary); border-radius:6px; overflow-x:auto; border:1px solid var(--border-primary); position:relative;"><code class="sql-code" id="sql-code-%d">%s</code></pre>
<button class="copy-sql-btn" onclick="copySQL('sql-code-%d')" style="position:absolute; top:8px; right:8px; font-size:0.8rem; padding:4px 10px; border:1px solid var(--border-primary); border-radius:6px; background:var(--bg-primary); cursor:pointer; color:var(--text-secondary); display:none;">Copy</button>
</details>`, hash, html.EscapeString(sqlQuery), hash)
}

// sqlQueryHash returns a simple hash of the SQL query for unique element IDs.
func sqlQueryHash(sql string) int {
	hash := 0
	for i := 0; i < len(sql); i++ {
		hash = ((hash << 5) - hash) + int(sql[i])
	}
	return hash
}

// FormatExplorationHTML formats exploration results as HTML.
func FormatExplorationHTML(results []ExplorationResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(`<details class="explore-block" style="margin:1rem 0;">
<summary style="cursor:pointer; color:var(--text-secondary); font-size:0.9rem; padding:0.5rem 0.75rem; background:var(--color-accent-light); border-radius:6px;">
  <span>&#8981; Show ` + fmt.Sprint(len(results)) + ` intermediate query(ies)</span>
</summary>
<div class="exploration-results">
`)

	for i, er := range results {
		sb.WriteString(fmt.Sprintf(`<div class="explore-round" style="margin-bottom:0.75rem; padding:0.75rem 1rem; background:var(--bg-surface); border-radius:6px; border:1px solid var(--border-primary);">
<div style="font-weight:600; font-size:0.9rem; margin-bottom:0.375rem;">Round %d</div>
`, i+1))
		if er.Explained != "" {
			sb.WriteString(fmt.Sprintf(`<div style="color:var(--text-tertiary); font-size:0.85rem; margin-bottom:0.5rem;">— %s</div>
`, html.EscapeString(er.Explained)))
		}
		sb.WriteString(fmt.Sprintf(`<pre style="margin:0.375rem 0; font-size:0.85rem; overflow-x:auto; background:var(--bg-primary); padding:0.5rem 0.75rem; border-radius:6px; border:1px solid var(--border-primary);"><code class="sql-code">%s</code></pre>
`, html.EscapeString(er.SQL)))
		if er.Result != nil && er.Result.RowCount > 0 {
			sb.WriteString(fmt.Sprintf(`<div style="margin-top:0.375rem; font-size:0.85rem; color:var(--color-success);">&#10003; %d row(s)</div>
`, er.Result.RowCount))
		} else if er.Result != nil {
			sb.WriteString(`<div style="margin-top:0.375rem; font-size:0.85rem; color:var(--text-tertiary);">&#10003; 0 rows</div>
`)
		}
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n</details>\n")
	return sb.String()
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 && (c == '-' || c == '+') {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isDate(s string) bool {
	if len(s) < 8 {
		return false
	}
	return (s[4] == '-' && s[7] == '-') || (s[4] == '/' && s[7] == '/')
}

func padRight(s string, length int, pad ...rune) string {
	if len(s) >= length {
		return s
	}
	padChar := ' '
	if len(pad) > 0 {
		padChar = pad[0]
	}
	return s + strings.Repeat(string(padChar), length-len(s))
}

// FormatResultsDigestForSummarization produces a compact digest of query results.
func FormatResultsDigestForSummarization(result *QueryResult) string {
	if result == nil || len(result.Columns) == 0 || len(result.Rows) == 0 {
		return ""
	}

	var sb strings.Builder
	totalRows := len(result.Rows)

	sb.WriteString(fmt.Sprintf("Query returned %d rows across %d columns.\n\n", totalRows, len(result.Columns)))

	sb.WriteString("Column summary:\n")
	for colIdx, col := range result.Columns {
		sb.WriteString(fmt.Sprintf("  %s: ", HumanizeColumnName(col)))
		stats := computeColumnStats(result.Rows, colIdx)
		if stats.isNumeric {
			sb.WriteString(fmt.Sprintf("numeric — min: %s, max: %s", stats.min, stats.max))
			if stats.mean != "" {
				sb.WriteString(fmt.Sprintf(", mean: %s", stats.mean))
			}
		} else {
			sb.WriteString(fmt.Sprintf("%d distinct values", stats.distinct))
		}
		if stats.nullCount > 0 {
			sb.WriteString(fmt.Sprintf(" (%d null)", stats.nullCount))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	const sampleSize = 5
	if totalRows <= sampleSize*2 {
		sb.WriteString(fmt.Sprintf("All %d rows:\n\n", totalRows))
		sb.WriteString(formatRowsTable(result.Columns, result.Rows, 0, totalRows))
	} else {
		sb.WriteString(fmt.Sprintf("First %d rows (of %d total):\n\n", sampleSize, totalRows))
		sb.WriteString(formatRowsTable(result.Columns, result.Rows, 0, sampleSize))
		sb.WriteString(fmt.Sprintf("\nLast %d rows:\n\n", sampleSize))
		sb.WriteString(formatRowsTable(result.Columns, result.Rows, totalRows-sampleSize, totalRows))
	}

	sb.WriteString("\nNote: The user sees the complete result table. The column statistics and row samples above represent the full data — base your analysis on them. Your summary will be displayed above the full results table in the user's interface.\n")

	return sb.String()
}

type columnStats struct {
	isNumeric bool
	min       string
	max       string
	mean      string
	distinct  int
	nullCount int
}

func computeColumnStats(rows [][]interface{}, colIdx int) columnStats {
	seen := make(map[string]bool)
	var nums []float64
	var cs columnStats

	for _, row := range rows {
		if colIdx >= len(row) || row[colIdx] == nil {
			cs.nullCount++
			continue
		}
		val := fmt.Sprintf("%v", row[colIdx])
		seen[val] = true
		if f, err := parseFloat(row[colIdx]); err == nil {
			nums = append(nums, f)
		}
	}

	cs.distinct = len(seen)
	if len(nums) > 0 && float64(len(nums)) >= float64(len(rows))*0.5 {
		cs.isNumeric = true
		min, max := nums[0], nums[0]
		var sum float64
		for _, n := range nums {
			sum += n
			if n < min {
				min = n
			}
			if n > max {
				max = n
			}
		}
		cs.min = formatFloat(min)
		cs.max = formatFloat(max)
		cs.mean = formatFloat(sum / float64(len(nums)))
	}
	return cs
}

func parseFloat(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case []byte:
		return strconv.ParseFloat(string(t), 64)
	case string:
		return strconv.ParseFloat(t, 64)
	}
	return 0, fmt.Errorf("not numeric")
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// formatRowsTable formats a slice of rows as a markdown table.
func formatRowsTable(columns []string, rows [][]interface{}, start, end int) string {
	var sb strings.Builder
	sb.WriteString("| ")
	for i, col := range columns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(HumanizeColumnName(col))
	}
	sb.WriteString(" |\n")
	sb.WriteString("|" + strings.Repeat("---|", len(columns)) + "\n")

	for i := start; i < end && i < len(rows); i++ {
		sb.WriteString("| ")
		for j, val := range rows[i] {
			if j > 0 {
				sb.WriteString(" | ")
			}
			cell := fmt.Sprintf("%v", val)
			if len(cell) > 80 {
				cell = cell[:80] + "..."
			}
			sb.WriteString(cell)
		}
		sb.WriteString(" |\n")
	}
	return sb.String()
}