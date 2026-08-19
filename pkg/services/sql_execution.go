package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"

	"github.com/gomarkdown/markdown"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"

	_ "modernc.org/sqlite"
)

// Default limits (previously from pkg/configuration).
const (
	defaultLimit            = 1000
	explorationDefaultLimit = 100
	queryLengthThreshold    = 200
)

// humanizeColumnName converts database column names to human-readable labels.
func humanizeColumnName(col string) string {
	col = strings.ReplaceAll(col, "_", " ")
	col = strings.ReplaceAll(col, "-", " ")

	words := wordRegex.FindAllString(col, -1)
	if words == nil {
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

var wordRegex = regexp.MustCompile(`([A-Z]+[a-z]*|[a-z]+|\d+)`)

// applyDefaultLimit appends a LIMIT clause if not already present and query exceeds threshold.
func applyDefaultLimit(sqlQuery string, conn *models.DataSource, isExploration bool) string {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	if regexp.MustCompile(`(?i)\bLIMIT\b`).MatchString(upper) {
		return sqlQuery
	}

	var threshold int
	if conn != nil {
		config, err := conn.ParseConfig()
		if err == nil && config.QueryLengthThreshold != 0 {
			threshold = config.QueryLengthThreshold
		}
	}
	if threshold == 0 {
		threshold = queryLengthThreshold
	}
	if threshold == 0 {
		return sqlQuery
	}
	if len(trimmed) <= threshold {
		return sqlQuery
	}

	var limitValue int
	if conn != nil {
		config, err := conn.ParseConfig()
		if err == nil {
			if isExploration && config.ExplorationDefaultLimit > 0 {
				limitValue = config.ExplorationDefaultLimit
			} else if !isExploration && config.DefaultLimit > 0 {
				limitValue = config.DefaultLimit
			}
		}
	}
	if limitValue == 0 {
		if isExploration {
			limitValue = explorationDefaultLimit
		} else {
			limitValue = defaultLimit
		}
	}
	if limitValue <= 0 {
		return sqlQuery
	}

	cleaned := strippedTrailingComments(trimmed)

	// Strip trailing semicolons — they're statement terminators, not part
	// of the query body. If left in place, the appended LIMIT would produce
	// "...LIMIT 1; LIMIT 1000" which is invalid SQL.
	cleaned = strings.TrimRight(cleaned, "; \t\n\r")

	// CTE (WITH ...): append after the final closing paren
	if strings.HasPrefix(strings.ToUpper(cleaned), "WITH") {
		depth := 0
		for i := len(cleaned) - 1; i >= 0; i-- {
			switch cleaned[i] {
			case ')':
				depth++
			case '(':
				depth--
				if depth <= 0 {
					return cleaned[:i+1] + " LIMIT " + fmt.Sprintf("%d", limitValue)
				}
			}
		}
		return cleaned + " LIMIT " + fmt.Sprintf("%d", limitValue)
	}

	// UNION queries: wrap in outer SELECT to apply limit correctly (§5.5)
	if regexp.MustCompile(`(?i)\bUNION\b`).MatchString(cleaned) {
		return "SELECT * FROM (" + cleaned + ") subq LIMIT " + fmt.Sprintf("%d", limitValue)
	}

	// Plain SELECT: just append
	return cleaned + " LIMIT " + fmt.Sprintf("%d", limitValue)
}

// strippedTrailingComments removes trailing SQL comments.
func strippedTrailingComments(s string) string {
	trimmed := strings.TrimSpace(s)

	// Strip trailing line comments
	lastDash := strings.LastIndex(trimmed, "--")
	if lastDash >= 0 && (lastDash == 0 || trimmed[lastDash-1] == ' ' || trimmed[lastDash-1] == '\t' || trimmed[lastDash-1] == '\n') {
		before := strings.TrimSpace(trimmed[:lastDash])
		if before != "" {
			trimmed = before
		}
	}

	// Strip trailing block comments
	lastBlock := strings.LastIndex(trimmed, "*/")
	if lastBlock >= 0 {
		openIdx := strings.LastIndex(trimmed[:lastBlock], "/*")
		if openIdx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:openIdx])
		}
	}

	return trimmed
}

// QueryResult holds the results of a SQL query.
// Canonical definition now in pkg/engine.
type QueryResult = engine.QueryResult

// executeSQL connects to the external database and runs the given SQL query.
func executeSQL(conn *models.DataSource, sqlQuery string) (*QueryResult, error) {
	return executeSQLWithMode(conn, sqlQuery, false)
}

// executeNativeQuery runs a query via the NativeQuerier interface (used by BigQuery etc.).
func executeNativeQuery(nq NativeQuerier, conn *models.DataSource, sqlQuery string) (*QueryResult, error) {
	columns, rows, err := nq.QueryRowsNative(conn, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer nq.CloseNative(conn)

	return &QueryResult{
		Columns:  columns,
		Rows:     rows,
		RowCount: len(rows),
	}, nil
}

// ExecuteSQLWithMode is the exported wrapper for executeSQLWithMode,
// used by the QueryExecutorAdapter in services/adapters.
func ExecuteSQLWithMode(conn *models.DataSource, sqlQuery string, isExploration bool) (*QueryResult, error) {
	return executeSQLWithMode(conn, sqlQuery, isExploration)
}

// executeSQLWithMode is like executeSQL but allows specifying exploration mode.
func executeSQLWithMode(conn *models.DataSource, sqlQuery string, isExploration bool) (*QueryResult, error) {
	sqlQuery = applyDefaultLimit(sqlQuery, conn, isExploration)

	// Enforce the Data Source Read-Only Invariant for every query —
	// exploration and final alike — before anything reaches a driver.
	// This is the single choke point for read-only enforcement
	// (AGENT_READ_FIRST.md §0).
	if err := engine.ValidateReadOnlySQL(sqlQuery); err != nil {
		return nil, err
	}

	dsn, err := BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	// Check if the driver supports native query execution (e.g., BigQuery)
	driver, _ := GetDriver(conn.Type)
	if nq, ok := driver.(NativeQuerier); ok {
		return executeNativeQuery(nq, conn, sqlQuery)
	}

	db, err := sql.Open(openDriverName(conn.Type), dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	db.SetConnMaxLifetime(30 * time.Second)
	db.SetMaxOpenConns(5)

	rows, err := db.Query(sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var resultRows [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		for i, val := range values {
			if b, ok := val.([]byte); ok {
				values[i] = string(b)
			}
		}

		resultRows = append(resultRows, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return &QueryResult{
		Columns:  columns,
		Rows:     resultRows,
		RowCount: len(resultRows),
	}, nil
}

// formatResults converts QueryResult into a human-readable markdown string.
func formatResults(result *QueryResult) string {
	if result.RowCount == 0 {
		return "No rows returned."
	}

	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = humanizeColumnName(col)
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

// mdRenderer is a shared stateless markdown renderer (safe to reuse across calls).
var mdRenderer = mdhtml.NewRenderer(mdhtml.RendererOptions{
	Flags: mdhtml.UseXHTML,
})

// fencedCodeRe matches a fenced code block opening that follows a non-blank
// line. Used by normalizeMarkdown to ensure robust parsing.
var fencedCodeRe = regexp.MustCompile(`([^\n])\n(\x60{3,})`)

// normalizeMarkdown preprocesses LLM-generated markdown to avoid common
// parser failures. Specifically, it ensures a blank line before fenced code
// blocks, which LLMs frequently omit despite most parsers requiring it.
func normalizeMarkdown(text string) string {
	return fencedCodeRe.ReplaceAllString(text, "$1\n\n$2")
}

// renderMarkdown converts markdown text to safe HTML. Each call creates a new
// parser because gomarkdown parsers are not reusable across Parse() invocations.
func renderMarkdown(text string) string {
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
	// Render summary at top level only when there is no chart. When a chart
	// is present, the summary moves inside the collapsed results block so it
	// sits alongside the raw data it describes (§SUMMARIZE_DATA_VIZ_CONFLICT).
	summaryHTML := ""
	if r.Summary != nil && *r.Summary != "" {
		summaryHTML = fmt.Sprintf("<div class=\"markdown-content\">%s</div>\n", renderMarkdown(*r.Summary))
		if !r.HasChart {
			sb.WriteString(summaryHTML)
		}
	}
	if r.SQL != "" {
		// SQL is now shown in the results toolbar toggle, not as a separate block
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
			// Collapse the table behind a details element
			sb.WriteString(fmt.Sprintf("<details class=\"results-details\" style=\"%s\"><summary style=\"%s\">%s (%d rows)</summary><div style=\"%s\">", detailsStyle, summaryStyle, label, r.Result.RowCount, divStyle))
			// When a chart is present, move the summary inside the
			// collapsed block alongside the raw table instead of
			// rendering it above the chart.
			if r.HasChart && r.Summary != nil && *r.Summary != "" {
				sb.WriteString(summaryHTML)
			}
			sb.WriteString(formatResultsHTML(r.Result, r.SQL))
			sb.WriteString("</div></details>")
		} else {
			sb.WriteString(formatResultsHTML(r.Result, r.SQL))
		}
	}
	if r.ExplorationHTML != "" {
		sb.WriteString(r.ExplorationHTML)
	}
	return sb.String()
}

// formatResultsHTML converts QueryResult into an HTML table.
func formatResultsHTML(result *QueryResult, sqlQuery string) string {
	if result.RowCount == 0 {
		return "<p>No rows returned.</p>"
	}

	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = humanizeColumnName(col)
	}

	hash := sqlQueryHash(sqlQuery)

	// Row collapse: show 10 rows by default, expandable if more
	const visibleRows = 10
	totalRows := result.RowCount
	hasMore := totalRows > visibleRows

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<div class=\"results-card\" style=\"margin:0.5rem 0;\">"))

	// Compact toolbar: row count + SQL toggle + CSV button on one line
	sb.WriteString(`<div class="results-toolbar" style="display:flex; align-items:center; gap:0.5rem; padding:6px 10px; background:var(--bg-surface); border:1px solid var(--border-primary); border-radius:6px; margin-bottom:0.5rem; flex-wrap:wrap;">`)

	// Row count
	sb.WriteString(fmt.Sprintf(`<span class="row-count" style="font-size:0.85rem; color:var(--text-secondary); font-weight:500;">%d row(s)</span>`, result.RowCount))

	// SQL toggle button
	sb.WriteString(fmt.Sprintf(`<span class="sql-toggle" style="font-size:0.8rem; color:var(--text-secondary);"><button class="sql-toggle-btn" onclick="toggleSQLSection(this, 'sql-popover-%d')" style="cursor:pointer; padding:2px 8px; background:var(--bg-primary); border:1px solid var(--border-primary); border-radius:4px; font-size:0.75rem; color:var(--text-secondary); user-select:none;">SQL</button></span>`, hash))

	// CSV button
	sb.WriteString(fmt.Sprintf(`<button class="csv-btn" onclick="exportCSV(this, %d)" style="font-size:0.8rem; padding:4px 10px; border:1px solid var(--border-primary); border-radius:4px; background:var(--bg-primary); cursor:pointer; color:var(--text-secondary);">↓ CSV</button>`, result.RowCount))

	// SQL code (hidden by default, expands below buttons)
	sb.WriteString(fmt.Sprintf(`<div id="sql-popover-%d" style="display:none; width:100%%; margin-top:0.5rem; padding:0.75rem 1rem; background:var(--bg-primary); border:1px solid var(--border-primary); border-radius:6px;"><pre style="margin:0; padding:0; font-size:0.8rem; overflow-x:auto;"><code id="sql-code-%d">%s</code></pre><button class="copy-sql-btn" onclick="copySQL('sql-code-%d')" style="margin:0.5rem 0 0 0; font-size:0.75rem; padding:4px 10px; border:1px solid var(--border-primary); border-radius:4px; background:var(--bg-primary); cursor:pointer; color:var(--text-secondary);">Copy</button></div>`, hash, hash, html.EscapeString(sqlQuery), hash))
	sb.WriteString(`</div>`)

	// Table
	sb.WriteString(`<div class="table-container" style="overflow-x: auto;">`)
	// Encode raw data for client-side sorting
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

	// Expand/collapse button for tall tables
	if hasMore {
		sb.WriteString(fmt.Sprintf(`<div style="margin-top:0.5rem;"><button class="table-expand-btn" onclick="var rs=this.closest('.results-card').querySelectorAll('.collapsed-row-%d');var ex=rs.length&&rs[0].style.display!=='none';rs.forEach(function(r){r.style.display=ex?'none':''});this.innerHTML=ex?'Show all %d rows &#9660;':'Show first 10 rows &#9650;'" style="font-size:0.8rem; padding:4px 12px; border:1px solid var(--border-primary); border-radius:4px; background:var(--bg-surface); cursor:pointer; color:var(--text-secondary);">Show all %d rows &#9660;</button></div>`, hash, totalRows, totalRows))
	}

	sb.WriteString(`</div>`)
	return sb.String()
}

// buildCollapsibleSQLBlockHTML returns HTML for a collapsible SQL code block with a copy button.
func buildCollapsibleSQLBlockHTML(sqlQuery string) string {
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

// formatExplorationHTML formats exploration results as HTML.
func formatExplorationHTML(results []ExplorationResult) string {
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

// ExplorationSafetyMode controls what types of exploration queries are permitted.
// Canonical definition now in pkg/engine.
type ExplorationSafetyMode = engine.ExplorationSafetyMode

const (
	ExplorationStrict   = engine.ExplorationStrict
	ExplorationModerate = engine.ExplorationModerate
	ExplorationRelaxed  = engine.ExplorationRelaxed
)

// augmentSQLError detects common SQL error patterns and appends schema
// guidance to help the LLM self-correct. Currently handles MySQL Error 1054
// (Unknown column). For all other errors, the original message is returned
// unchanged.
func augmentSQLError(errStr string) string {
	// MySQL Error 1054: Unknown column 'X' in 'field list'
	if idx := strings.Index(errStr, "Error 1054"); idx != -1 {
		return errStr + "\n\n[Hint: This is a schema error — a column in your query doesn't exist on the table you referenced. Check:\n1. Did you reference the correct table/alias for this column?\n2. Is the column on a different table that needs a JOIN? Use `products p ON od.productCode = p.productCode` then reference `p.columnName`.\n3. Check table names and column names against the schema above.\n4. Fix the column reference and retry.]"
	}
	return errStr
}

// isRetryableError determines whether a SQL execution error is retryable.
func isRetryableError(err error) bool {
	msg := err.Error()
	upper := strings.ToUpper(msg)

	// 1. PERMANENTLY FATAL — auth/permission failures never retry.
	permanentlyFatal := []string{
		"AUTHENTICATION", "ACCESS DENIED", "PERMISSION DENIED",
		"INVALID PASSWORD", "INVALID API KEY", "COMMAND DENIED",
		"UNAUTHORIZED", "FORBIDDEN",
	}
	for _, pat := range permanentlyFatal {
		if strings.Contains(upper, pat) {
			return false
		}
	}

	// 2. SQL ENGINE ERROR — any error that looks like it originated from the
	//    database engine (has an error code, mentions database objects, or
	//    contains SQL-level keywords) is potentially LLM-correctable. This
	//    replaces the previous 40+ driver-specific patterns with a few broad
	//    structural checks that work across MySQL, PostgreSQL, SQLite, etc.
	if looksLikeSQLError(msg) {
		return true
	}

	// 3. TRANSIENT CONNECTION — retryable with exponential backoff.
	transientConnection := []string{
		"DIAL", "HANDSHAKE", "CONNECTION RESET", "I/O TIMEOUT",
		"CONNECTION REFUSED", "NO SUCH HOST", "TLS:", "CERTIFICATE",
		"MAX CONNECTIONS", "TOO MANY CONNECTIONS", "DEADLOCK",
		"LOCK WAIT TIMEOUT", "CONNECTION",
	}
	for _, pat := range transientConnection {
		if strings.Contains(upper, pat) {
			return true
		}
	}

	// 4. UNKNOWN — default to false. The retry loop gives unknown
	//    non-auth errors one correction attempt as a safety net.
	return false
}

// looksLikeSQLError checks whether an error message matches the broad
// structural signature of a database-level SQL error (as opposed to a
// connection, auth, or infrastructure error). This uses error-code patterns
// and SQL-related keywords that are common across MySQL, PostgreSQL, SQLite,
// SQL Server, Snowflake, BigQuery, Redshift, and MariaDB.
func looksLikeSQLError(msg string) bool {
	upper := strings.ToUpper(msg)

	// Has a database error code: MySQL "Error 1305", PG "ERROR:",
	// SQLite "Error:", SQL Server "Msg 208", etc.
	if regexp.MustCompile(`(?i)(error|msg|sqlstate)\s+\d+`).MatchString(msg) {
		return true
	}

	// Contains database-object references or SQL-level error keywords.
	// These appear in errors from all major database engines.
	sqlIndicators := []string{
		"COLUMN", "TABLE", "FUNCTION", "PROCEDURE", "TRIGGER",
		"SYNTAX", "UNKNOWN", "DOES NOT EXIST", "DOESN'T EXIST",
		"AMBIGUOUS", "TRUNCATED", "INCORRECT", "INVALID",
		"DUPLICATE", "FOREIGN KEY", "PRIMARY KEY", "NOT NULL",
		"CONSTRAINT", "SUBQUERY", "GROUP BY", "ORDER BY",
		"DIVISION BY ZERO", "OUT OF RANGE", "OVERFLOW",
		"DOESN'T HAVE", "CANNOT", "NOT FOUND",
	}
	for _, pat := range sqlIndicators {
		if strings.Contains(upper, pat) {
			return true
		}
	}
	return false
}

// backoffDuration returns an exponential backoff duration with jitter.
// base = 500ms, max = 15s. Used before retrying transient connection errors.
func backoffDuration(attempt int) time.Duration {
	base := 500 * time.Millisecond
	max := 15 * time.Second
	backoff := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	if backoff > max {
		backoff = max
	}
	// Add ±25% jitter
	jitter := time.Duration(float64(backoff) * 0.5 * (rand.Float64() - 0.5))
	return backoff + jitter
}

// sanitizeSQLError strips embedded DSN credentials from database driver
// error messages. Go SQL drivers frequently embed the full connection
// string (including password) in error text. This ensures no credential
// reaches tech-details-visible surfaces (buildErrorMetadata, storeTechDetail).
// Per AGENT_READ_FIRST.md §3.5/§4.0, credential logging is never acceptable.
// Accepts a string so it can be used with raw error text from any source.
func sanitizeSQLError(msg string) string {
	if msg == "" {
		return ""
	}
	// Strip URI-style credentials: user:password@ or ://user:pass@host
	re := regexp.MustCompile(`(://[^:@]+):[^@]+@`)
	msg = re.ReplaceAllString(msg, "$1:***@")
	// Also catch bare user:password@ (no scheme prefix)
	re2 := regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9]*):[^@\s]+@`)
	msg = re2.ReplaceAllString(msg, "$1:***@")
	return msg
}

// ParseExplorationSafety parses a string into an ExplorationSafetyMode.
// Delegates to the canonical implementation in pkg/engine.
var ParseExplorationSafety = engine.ParseExplorationSafety

// All safety validation functions (ValidateReadOnlySQL, ValidateExplorationQuery,
// StripSQLComments, and their helpers) now live in pkg/engine/safety.go.
// The call sites in this package have been updated to use engine.Validate* directly.
