package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"YourQL/pkg/models"

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
func applyDefaultLimit(sqlQuery string, conn *models.DBConnection, isExploration bool) string {
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
type QueryResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	RowCount int             `json:"row_count"`
}

// executeSQL connects to the external database and runs the given SQL query.
func executeSQL(conn *models.DBConnection, sqlQuery string) (*QueryResult, error) {
	return executeSQLWithMode(conn, sqlQuery, false)
}

// executeNativeQuery runs a query via the NativeQuerier interface (used by BigQuery etc.).
func executeNativeQuery(nq NativeQuerier, conn *models.DBConnection, sqlQuery string) (*QueryResult, error) {
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

// executeSQLWithMode is like executeSQL but allows specifying exploration mode.
func executeSQLWithMode(conn *models.DBConnection, sqlQuery string, isExploration bool) (*QueryResult, error) {
	sqlQuery = applyDefaultLimit(sqlQuery, conn, isExploration)

	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	isSelect := strings.HasPrefix(upper, "SELECT")
	isCTE := false
	if strings.HasPrefix(upper, "WITH") && len(upper) > 4 {
		next := upper[4]
		isCTE = next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '(' || next == 'R'
	}
	if !isSelect && !isCTE {
		return nil, fmt.Errorf("only SELECT queries are allowed")
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
}

// ToHTML renders the assistant response as HTML.
func (r *AssistantResponse) ToHTML() string {
	var sb strings.Builder
	if r.Summary != nil && *r.Summary != "" {
		sb.WriteString(fmt.Sprintf("<p>%s</p>\n", html.EscapeString(*r.Summary)))
	}
	if r.Summary == nil || *r.Summary == "" {
		if r.Explanation != "" {
			sb.WriteString(fmt.Sprintf("<p>%s</p>\n", html.EscapeString(r.Explanation)))
		}
	}
	if r.SQL != "" {
		// SQL is now shown in the results toolbar toggle, not as a separate block
	}
	if r.Result != nil {
		if r.Summary != nil && *r.Summary != "" {
			// Collapse the table behind a details element
			sb.WriteString(fmt.Sprintf("<details class=\"results-details\" style=\"margin-top:0.5rem;\"><summary style=\"cursor:pointer; color:#666; font-size:0.85rem; padding:4px 8px; background:#f5f5f5; border-radius:4px; display:inline-block;\">View raw results (%d rows)</summary><div style=\"margin-top:0.5rem;\">", r.Result.RowCount))
			if r.Explanation != "" {
				sb.WriteString(fmt.Sprintf("<p style=\"color:#666; font-size:0.9rem;\"><em>%s</em></p>\n", html.EscapeString(r.Explanation)))
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
	sb.WriteString(`<div class="results-toolbar" style="display:flex; align-items:center; gap:0.5rem; padding:6px 10px; background:#f8f9fa; border:1px solid #e8e8e8; border-radius:6px; margin-bottom:0.5rem; flex-wrap:wrap;">`)

	// Row count
	sb.WriteString(fmt.Sprintf(`<span class="row-count" style="font-size:0.85rem; color:#666; font-weight:500;">%d row(s)</span>`, result.RowCount))

	// SQL toggle button
	sb.WriteString(fmt.Sprintf(`<span class="sql-toggle" style="font-size:0.8rem; color:#666;"><button class="sql-toggle-btn" onclick="toggleSQLSection(this, 'sql-popover-%d')" style="cursor:pointer; padding:2px 8px; background:#fff; border:1px solid #ddd; border-radius:4px; font-size:0.75rem; color:#666; user-select:none;">SQL</button></span>`, hash))

	// CSV button
	sb.WriteString(fmt.Sprintf(`<button class="csv-btn" onclick="exportCSV(this, %d)" style="font-size:0.8rem; padding:4px 10px; border:1px solid #e8e8e8; border-radius:4px; background:white; cursor:pointer; color:#666;">↓ CSV</button>`, result.RowCount))

	// SQL code (hidden by default, expands below buttons)
	sb.WriteString(fmt.Sprintf(`<div id="sql-popover-%d" style="display:none; width:100%%; margin-top:0.5rem; padding:0.75rem 1rem; background:#fff; border:1px solid #e8e8e8; border-radius:6px;"><pre style="margin:0; padding:0; font-size:0.8rem; overflow-x:auto;"><code id="sql-code-%d">%s</code></pre><button class="copy-sql-btn" onclick="copySQL('sql-code-%d')" style="margin:0.5rem 0 0 0; font-size:0.75rem; padding:4px 10px; border:1px solid #ddd; border-radius:4px; background:white; cursor:pointer; color:#666;">Copy</button></div>`, hash, hash, html.EscapeString(sqlQuery), hash))
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
		sb.WriteString(fmt.Sprintf(`<th class="sort-header" data-col="%d" style="border:1px solid #e8e8e8; padding:10px 12px; text-align:left; background:#f8f9fa; position:sticky; top:0; z-index:2; font-weight:600; user-select:none; white-space:nowrap; cursor:pointer;">%s <span class="sort-indicator"></span></th>`,
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
			sb.WriteString(fmt.Sprintf(`<td class="%s" style="border:1px solid #e8e8e8; padding:8px 12px; max-width:400px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;" title="%s">%s</td>`,
				cellClass, html.EscapeString(cell), html.EscapeString(cell)))
		}
		sb.WriteString(`</tr>`)
	}

	sb.WriteString(`</tbody></table>`)

	// Expand/collapse button for tall tables
	if hasMore {
		sb.WriteString(fmt.Sprintf(`<div style="margin-top:0.5rem;"><button class="table-expand-btn" onclick="var rs=this.closest('.results-card').querySelectorAll('.collapsed-row-%d');var ex=rs.length&&rs[0].style.display!=='none';rs.forEach(function(r){r.style.display=ex?'none':''});this.innerHTML=ex?'Show all %d rows &#9660;':'Show first 10 rows &#9650;'" style="font-size:0.8rem; padding:4px 12px; border:1px solid #e0e0e0; border-radius:4px; background:#f8f9fa; cursor:pointer; color:#666;">Show all %d rows &#9660;</button></div>`, hash, totalRows, totalRows))
	}

	sb.WriteString(`</div>`)
	return sb.String()
}

// buildCollapsibleSQLBlockHTML returns HTML for a collapsible SQL code block with a copy button.
func buildCollapsibleSQLBlockHTML(sqlQuery string) string {
	hash := sqlQueryHash(sqlQuery)
	return fmt.Sprintf(`<details class="sql-block" style="margin: 1rem 0;">
<summary style="cursor:pointer; color:#666; font-size:0.9rem; padding:0.5rem 0.75rem; background:#f5f5f5; border-radius:6px; display:flex; justify-content:space-between; align-items:center;">
  <span>Show SQL</span>
</summary>
<pre style="margin:0.5rem 0; padding:1rem; background:#f8f8f8; border-radius:6px; overflow-x:auto; border:1px solid #e8e8e8; position:relative;"><code class="sql-code" id="sql-code-%d">%s</code></pre>
<button class="copy-sql-btn" onclick="copySQL('sql-code-%d')" style="position:absolute; top:8px; right:8px; font-size:0.8rem; padding:4px 10px; border:1px solid #ddd; border-radius:6px; background:white; cursor:pointer; color:#666; display:none;">Copy</button>
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
<summary style="cursor:pointer; color:#666; font-size:0.9rem; padding:0.5rem 0.75rem; background:#f0f4ff; border-radius:6px;">
  <span>&#8981; Show ` + fmt.Sprint(len(results)) + ` intermediate query(ies)</span>
</summary>
<div class="exploration-results">
`)

	for i, er := range results {
		sb.WriteString(fmt.Sprintf(`<div class="explore-round" style="margin-bottom:0.75rem; padding:0.75rem 1rem; background:#f5f7fa; border-radius:6px; border:1px solid #e8e8e8;">
<div style="font-weight:600; font-size:0.9rem; margin-bottom:0.375rem;">Round %d</div>
`, i+1))
		if er.Explained != "" {
			sb.WriteString(fmt.Sprintf(`<div style="color:#888; font-size:0.85rem; margin-bottom:0.5rem;">— %s</div>
`, html.EscapeString(er.Explained)))
		}
		sb.WriteString(fmt.Sprintf(`<pre style="margin:0.375rem 0; font-size:0.85rem; overflow-x:auto; background:#fff; padding:0.5rem 0.75rem; border-radius:6px; border:1px solid #e8e8e8;"><code class="sql-code">%s</code></pre>
`, html.EscapeString(er.SQL)))
		if er.Result != nil && er.Result.RowCount > 0 {
			sb.WriteString(fmt.Sprintf(`<div style="margin-top:0.375rem; font-size:0.85rem; color:#27ae60;">&#10003; %d row(s)</div>
`, er.Result.RowCount))
		} else if er.Result != nil {
			sb.WriteString(`<div style="margin-top:0.375rem; font-size:0.85rem; color:#888;">&#10003; 0 rows</div>
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
type ExplorationSafetyMode int

const (
	ExplorationStrict    ExplorationSafetyMode = iota
	ExplorationModerate
	ExplorationRelaxed
)

// isRetryableError determines whether a SQL execution error is retryable.
func isRetryableError(err error) bool {
	msg := err.Error()

	fatalPatterns := []string{
		"dial", "handshake", "authentication", "max connections",
		"connection reset", "i/o timeout", "connection refused",
		"no such host", "tls:", "certificate",
	}
	upper := strings.ToUpper(msg)
	for _, pat := range fatalPatterns {
		if strings.Contains(upper, strings.ToUpper(pat)) {
			return false
		}
	}

	retryablePatterns := []string{
		"unknown column", "unknown table", "doesn't exist", "syntax error",
		"you have an error in your", "truncated incorrect", "incorrect string value",
		"invalid use of group", "ambiguous column", "multiple primary key",
		"deadlock", "lock wait timeout", "too many connections",
		"table is marked as crashed", "incorrect key value", "data too long",
		"out of range", "division by zero",
		"subquery returns more than 1 row", "subquery", "1242",
		"invalid character", "invalid utf8", "invalid utf8mb4",
		"field doesn't have", "not found", "not exists",
		"access denied", "permission denied", "command denied",
		"function doesn't exist", "column '.*' in", "not in group by",
		"invalid reference", "conflicting types", "can't drop",
		"duplicate entry", "foreign key constraint", "cannot add foreign key",
		"cannot truncate", "view's", "stored function", "prepared statement",
		"invalid collation", "incorrect date value", "incorrect datetime value",
		"incorrect time value", "incorrect year value", "incorrect double value",
		"overflow", "underflow", "truncated", "out of memory",
		"temporary file", "disk full",
	}
	for _, pat := range retryablePatterns {
		if regexp.MustCompile("(?i)" + pat).MatchString(msg) {
			return true
		}
	}

	return true
}

// ParseExplorationSafety parses a string into an ExplorationSafetyMode.
func ParseExplorationSafety(s string) ExplorationSafetyMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "moderate":
		return ExplorationModerate
	case "relaxed":
		return ExplorationRelaxed
	default:
		return ExplorationStrict
	}
}

// validateExplorationQuery checks whether an exploration query is allowed.
func validateExplorationQuery(sqlQuery string, mode ExplorationSafetyMode) error {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	isSelect := strings.HasPrefix(upper, "SELECT")
	isCTE := false
	if strings.HasPrefix(upper, "WITH") && len(upper) > 4 {
		next := upper[4]
		isCTE = next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '('
	}
	if !isSelect && !isCTE {
		return fmt.Errorf("exploration queries must be SELECT statements")
	}

	// Block DML/DDL — strip comments first (§5.6)
	stripped := stripSQLComments(upper)
	dangerousPatterns := []string{
		"INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER ", "TRUNCATE ",
		"CREATE ", "REPLACE ", "GRANT ", "REVOKE ", "LOAD_FILE",
		"INTO OUTFILE", "INTO DUMPFILE", "BENCHMARK(", "SLEEP(",
		"EXEC ", "EXECUTE ", "xp_", "sp_",
	}
	for _, pat := range dangerousPatterns {
		if strings.Contains(stripped, pat) {
			return fmt.Errorf("exploration query blocked: contains '%s'", pat)
		}
	}

	hasJoin := strings.Contains(upper, "JOIN")
	hasSubquery := strings.Contains(upper, "(") && strings.Contains(strings.TrimPrefix(upper, "SELECT"), "SELECT")
	hasUnion := strings.Contains(upper, "UNION")
	hasGroupBy := strings.Contains(upper, "GROUP BY")
	hasOrderBy := strings.Contains(upper, "ORDER BY")

	switch mode {
	case ExplorationStrict:
		if hasJoin || hasSubquery || hasUnion || hasGroupBy || hasOrderBy {
			return fmt.Errorf("strict mode: only simple SELECT queries allowed")
		}
	case ExplorationModerate:
		if hasSubquery {
			return fmt.Errorf("moderate mode: subqueries are not allowed")
		}
		if hasUnion {
			return fmt.Errorf("moderate mode: UNION is not allowed")
		}
		fromJoinCount := len(regexp.MustCompile(`\b(FROM|JOIN)\b`).FindAllString(upper, -1))
		if fromJoinCount > 2 {
			return fmt.Errorf("moderate mode: multi-table JOINs are not allowed")
		}
	case ExplorationRelaxed:
		// Only DML/DDL blocked (above)
	}

	return nil
}

// stripSQLComments removes SQL line and block comments from the query text.
func stripSQLComments(s string) string {
	// Remove block comments
	blockRe := regexp.MustCompile(`/\*.*?\*/`)
	s = blockRe.ReplaceAllString(s, "")
	// Remove line comments
	lineRe := regexp.MustCompile(`--.*$`)
	s = lineRe.ReplaceAllString(s, "")
	return s
}
