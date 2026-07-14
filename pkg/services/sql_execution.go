package services

import (
	"database/sql"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"YourQL/pkg/configuration"
	"YourQL/pkg/models"

	_ "modernc.org/sqlite"
)

// humanizeColumnName converts database column names to human-readable labels.
func humanizeColumnName(col string) string {
	// Replace underscores and hyphens with spaces
	col = strings.ReplaceAll(col, "_", " ")
	col = strings.ReplaceAll(col, "-", " ")
	
	// Find word boundaries using regex
	words := wordRegex.FindAllString(col, -1)
	if words == nil {
		return strings.Title(col)
	}
	
	var result []string
	for _, w := range words {
		if w == "" {
			continue
		}
		// Keep acronyms all uppercase
		if strings.ToUpper(w) == w && len(w) > 1 {
			result = append(result, w)
		} else {
			// Title case (first letter uppercase, rest lower)
			result = append(result, strings.ToUpper(w[:1])+strings.ToLower(w[1:]))
		}
	}
	return strings.Join(result, " ")
}

var (
	wordRegex = regexp.MustCompile(`([A-Z]+[a-z]*|[a-z]+|\d+)`)
)

// applyDefaultLimit appends a LIMIT clause to the query if one is not already present
// and the query length exceeds the configured threshold. Short queries (e.g.
// "SELECT COUNT(*) FROM users") are assumed intentional and left unbounded.
func applyDefaultLimit(sqlQuery string, conn *models.DBConnection, isExploration bool) string {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	// Check if LIMIT is already present (word-boundary aware).
	if regexp.MustCompile(`(?i)\bLIMIT\b`).MatchString(upper) {
		return sqlQuery
	}

	// Determine the threshold: per-connection override → app default.
	var threshold int
	if conn != nil {
		config, err := conn.ParseConfig()
		if err == nil && config.QueryLengthThreshold != 0 {
			threshold = config.QueryLengthThreshold
		}
	}
	if threshold == 0 {
		threshold = configuration.Config.SQLQuery.QueryLengthThreshold
	}

	// If threshold is -1, always apply the limit regardless of length.
	// If threshold is 0 (disabled), never apply the limit.
	if threshold == 0 {
		return sqlQuery
	}

	// Check query length against threshold.
	if len(trimmed) <= threshold {
		return sqlQuery
	}

	// Determine the limit value: per-connection override → app default.
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
			limitValue = configuration.Config.SQLQuery.ExplorationDefaultLimit
		} else {
			limitValue = configuration.Config.SQLQuery.DefaultLimit
		}
	}
	if limitValue <= 0 {
		return sqlQuery // disabled
	}

	// Find the logical end of the outermost query to append LIMIT.
	// Strip trailing comments first.
	cleaned := strippedTrailingComments(trimmed)
	cleanedUpper := strings.ToUpper(cleaned)

	// CTE (WITH ...): append after the final closing paren.
	if strings.HasPrefix(cleanedUpper, "WITH") {
		// Find the last ')' that closes the CTE chain.
		// Walk backwards to find the outermost closing paren.
		depth := 0
		lastClose := -1
		for i := len(cleaned) - 1; i >= 0; i-- {
			switch cleaned[i] {
			case ')':
				depth++
				lastClose = i
			case '(':
				depth--
				if depth <= 0 {
					// Found the outermost closing paren
					return cleaned[:lastClose+1] + " LIMIT " + fmt.Sprintf("%d", limitValue)
				}
			}
		}
		// No closing paren found — just append
		return cleaned + " LIMIT " + fmt.Sprintf("%d", limitValue)
	}

	// UNION / UNION ALL: append after the last query in the chain.
	// Find the last SELECT keyword before the end.
	lastSelect := -1
	for {
		idx := strings.Index(cleanedUpper[lastSelect+1:], "SELECT")
		if idx == -1 {
			break
		}
		lastSelect = lastSelect + 1 + idx
	}
	if lastSelect >= 0 {
		// Append after the last SELECT's logical end (which is the end of the string)
		return cleaned + " LIMIT " + fmt.Sprintf("%d", limitValue)
	}

	// Plain SELECT: just append.
	return cleaned + " LIMIT " + fmt.Sprintf("%d", limitValue)
}

// strippedTrailingComments removes trailing SQL comments (-- ... and /* ... */) from the end of a string.
func strippedTrailingComments(s string) string {
	trimmed := strings.TrimSpace(s)
	
	// Strip trailing line comments (-- ...)
	for strings.HasSuffix(trimmed, "--") || strings.Contains(trimmed, "\n--") {
		idx := strings.LastIndex(trimmed, "--")
		if idx >= 0 {
			// Check if it's actually a comment (preceded by whitespace or at start)
			if idx == 0 || trimmed[idx-1] == ' ' || trimmed[idx-1] == '\t' || trimmed[idx-1] == '\n' {
				trimmed = trimmed[:idx]
				trimmed = strings.TrimRight(trimmed, " \t\n\r")
				break
			}
		}
		break
	}

	// Strip trailing block comments (/* ... */)
	for strings.HasSuffix(trimmed, "/*") || strings.HasSuffix(trimmed, "*/") {
		idx := strings.LastIndex(trimmed, "/*")
		if idx >= 0 {
			endIdx := strings.Index(trimmed[idx:], "*/")
			if endIdx >= 0 {
				endIdx = idx + endIdx + 2
				trimmed = strings.TrimRight(trimmed[:idx], " \t\n\r")
				break
			}
		}
		break
	}

	return trimmed
}

// QueryResult holds the results of a SQL query.
type QueryResult struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	RowCount int            `json:"row_count"`
}

// executeSQL connects to the external database and runs the given SQL query.
// Only SELECT queries are allowed for safety.
func executeSQL(conn *models.DBConnection, sqlQuery string) (*QueryResult, error) {
	return executeSQLWithMode(conn, sqlQuery, false)
}

// executeSQLWithMode is like executeSQL but allows specifying exploration mode.
func executeSQLWithMode(conn *models.DBConnection, sqlQuery string, isExploration bool) (*QueryResult, error) {
	// Apply default LIMIT if not already present
	sqlQuery = applyDefaultLimit(sqlQuery, conn, isExploration)

	// Validate query: only read-only queries allowed
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	// Allow SELECT and CTE (WITH) queries
	isSelect := strings.HasPrefix(upper, "SELECT")
	isCTE := false
	if strings.HasPrefix(upper, "WITH") && len(upper) > 4 {
		// Check that after "WITH" there is whitespace or '(' (for CTE)
		next := upper[4]
		isCTE = next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '(' || next == 'R'
	}
	if !isSelect && !isCTE {
		return nil, fmt.Errorf("only SELECT queries are allowed")
	}

	// For CTE queries, we rely on the database to validate the query.
	// SQLite and MySQL both support CTEs and will reject
	// non-SELECT statements inside CTEs.

	dsn, err := BuildDSN(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to build DSN: %w", err)
	}

	db, err := sql.Open(conn.Type, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	db.SetConnMaxLifetime(30 * time.Second)
	db.SetMaxOpenConns(5)

	// Execute query
	rows, err := db.Query(sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Scan rows
	var resultRows [][]interface{}
	for rows.Next() {
		// Create slice of interface{} to hold column values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert []byte to string for readability
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
		Columns: columns,
		Rows:    resultRows,
		RowCount: len(resultRows),
	}, nil
}

// formatResults converts QueryResult into a human‑readable string.
func formatResults(result *QueryResult) string {
	if result.RowCount == 0 {
		return "No rows returned."
	}

	// Humanize column names for display
	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = humanizeColumnName(col)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%d row(s) returned**\n\n", result.RowCount))

	// Determine column widths based on humanized column names and data
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

	// Build markdown table header
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

	// Rows
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
}

// ToHTML renders the assistant response as HTML.
func (r *AssistantResponse) ToHTML() string {
	var sb strings.Builder
	if r.Explanation != "" {
		sb.WriteString(fmt.Sprintf("<p>%s</p>\n", html.EscapeString(r.Explanation)))
	}
	if r.SQL != "" {
		sb.WriteString(buildCollapsibleSQLBlockHTML(r.SQL))
	}
	if r.Result != nil {
		sb.WriteString(formatResultsHTML(r.Result))
	}
	if r.ExplorationHTML != "" {
		sb.WriteString(r.ExplorationHTML)
	}
	return sb.String()
}

// formatResultsHTML converts QueryResult into an HTML table with sticky header,
// row limiting, show-more toggle, CSV export, and sortable columns.
func formatResultsHTML(result *QueryResult) string {
	if result.RowCount == 0 {
		return "<p>No rows returned.</p>"
	}

	// Humanize column names for display
	humanizedCols := make([]string, len(result.Columns))
	for i, col := range result.Columns {
		humanizedCols[i] = humanizeColumnName(col)
	}

	const defaultLimit = 100
	showAll := result.RowCount <= defaultLimit
	displayedRows := defaultLimit
	if showAll {
		displayedRows = result.RowCount
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<p><strong>%d row(s) returned</strong></p>\n", result.RowCount))

	// Table toolbar with toggle and CSV buttons
	sb.WriteString(`<div class="table-toolbar" style="margin-bottom:0.5rem; display:flex; gap:0.75rem; align-items:center;">
`)
	if !showAll {
		sb.WriteString(fmt.Sprintf(`<button class="table-toggle-btn" data-target="data-%d" onclick="toggleRows(this, %d, %d)">Show more</button>
`,
			result.RowCount, result.RowCount, displayedRows))
	}
	sb.WriteString(fmt.Sprintf(`<button class="table-toggle-btn" data-target="csv-%d" onclick="exportCSV(this, %d)">↓ CSV</button>
`,
		result.RowCount, result.RowCount))
	sb.WriteString(`</div>
`)

	sb.WriteString(`<div class="table-container" style="overflow-x: auto;">`)
	sb.WriteString(`<table class="result-table sortable" data-row-count="` + fmt.Sprint(result.RowCount) + `" style="border-collapse: collapse; width: 100%;">`)

	// Sticky header
	sb.WriteString(`<thead><tr>`)
	for i, col := range result.Columns {
		humanized := humanizedCols[i]
		sb.WriteString(fmt.Sprintf(`<th class="sortable-col" data-col="%s" onclick="sortColumn(this)" style="border:1px solid #e8e8e8; padding:10px 12px; text-align:left; background:#f8f9fa; position:sticky; top:0; z-index:2; font-weight:600; cursor:pointer; user-select:none; white-space:nowrap;">%s <span class="sort-icon" style="margin-left:4px;opacity:0.3;">⇅</span></th>`,
			html.EscapeString(col), html.EscapeString(humanized)))
	}
	sb.WriteString(`</tr></thead>`)
	sb.WriteString(`<tbody data-visible="` + fmt.Sprint(displayedRows) + `">`)

	for i, row := range result.Rows {
		if i >= displayedRows {
			break
		}
		sb.WriteString(`<tr class="result-row">`)
		for _, val := range row {
			cell := fmt.Sprintf("%v", val)
			// Right-align numbers, color-code dates
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

	sb.WriteString(`</tbody></table></div>`)
	return sb.String()
}

// buildCollapsibleSQLBlockHTML returns HTML for a collapsible SQL code block with a copy button.
func buildCollapsibleSQLBlockHTML(sqlQuery string) string {
	// Store raw SQL in a data attribute (not escaped — JS reads it directly)
	return fmt.Sprintf(`<details class="sql-block" style="margin: 1rem 0;">
<summary style="cursor:pointer; color:#666; font-size:0.9rem; padding:0.5rem 0.75rem; background:#f5f5f5; border-radius:4px; display:flex; justify-content:space-between; align-items:center;">
  <span>Show SQL</span>
  <button class="copy-btn" onclick="copySQL(this)" style="font-size:0.8rem; padding:2px 8px; border:1px solid #ddd; border-radius:4px; background:white; cursor:pointer; color:#666;">📋 Copy</button>
</summary>
<pre style="margin:0.5rem 0; padding:1rem; background:#f8f8f8; border-radius:4px; overflow-x:auto; border:1px solid #e8e8e8;"><code class="sql-code">%s</code></pre>
</details>`, html.EscapeString(sqlQuery))
}

// formatExplorationHTML formats exploration results as HTML with a "collapse all" toggle.
func formatExplorationHTML(results []ExplorationResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(`<details class="explore-block" style="margin:1rem 0;">
<summary style="cursor:pointer; color:#666; font-size:0.9rem; padding:0.5rem 0.75rem; background:#f0f4ff; border-radius:4px; display:flex; justify-content:space-between; align-items:center;">
  <span>🔍 Show ` + fmt.Sprint(len(results)) + ` intermediate query(ies)</span>
  <button class="collapse-all-btn" onclick="toggleAllExploration(this)" style="font-size:0.8rem; padding:2px 8px; border:1px solid #c0d0f0; border-radius:4px; background:white; cursor:pointer; color:#4a90d9;">Collapse all</button>
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
		sb.WriteString(fmt.Sprintf(`<pre style="margin:0.375rem 0; font-size:0.85rem; overflow-x:auto; background:#fff; padding:0.5rem 0.75rem; border-radius:4px; border:1px solid #e8e8e8;"><code class="sql-code">%s</code></pre>
`, html.EscapeString(er.SQL)))
		if er.Result != nil && er.Result.RowCount > 0 {
			// Humanize column names for display
			humanizedCols := make([]string, len(er.Result.Columns))
			for i, col := range er.Result.Columns {
				humanizedCols[i] = humanizeColumnName(col)
			}
			sb.WriteString(fmt.Sprintf(`<div style="margin-top:0.375rem; display:flex; gap:1rem; font-size:0.85rem;">
<span style="color:#27ae60;">✅ %d row(s)</span>
<span style="color:#888;">Columns: %s</span>
</div>
`, er.Result.RowCount, strings.Join(humanizedCols, ", ")))
		} else if er.Result != nil {
			sb.WriteString(`<div style="margin-top:0.375rem; font-size:0.85rem; color:#888;">✅ 0 rows</div>
`)
		}
		sb.WriteString("</div>\n")
	}

	sb.WriteString("</div>\n</details>\n")
	return sb.String()
}

// isNumber returns true if the string looks like a number.
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

// isDate returns true if the string looks like a date (YYYY-MM-DD or contains / as separator).
func isDate(s string) bool {
	if len(s) < 8 {
		return false
	}
	// Check YYYY-MM-DD pattern
	if (s[4] == '-' && s[7] == '-') || (s[4] == '/' && s[7] == '/') {
		return true
	}
	return false
}

// padRight pads a string to the given length with spaces (or a specified rune).
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
	ExplorationStrict    ExplorationSafetyMode = iota // LIMIT, COUNT, DISTINCT, SHOW COLUMNS, DESCRIBE, INFORMATION_SCHEMA only
	ExplorationModerate                                // strict + single-table JOIN, GROUP BY, ORDER BY
	ExplorationRelaxed                                 // moderate + subqueries, UNION
)

// isRetryableError determines whether a SQL execution error is likely to be
// corrected by the LLM (retryable) or is a fundamental connectivity issue (fatal).
func isRetryableError(err error) bool {
	msg := err.Error()

	// Fatal errors: these indicate infrastructure problems the LLM cannot fix
	fatalPatterns := []string{
		"dial",              // connection refused / network error
		"handshake",         // SSL/TLS handshake failure
		"authentication",    // auth failure
		"max connections",   // server capacity
		"connection reset",  // network drop
		"i/o timeout",       // network timeout
		"connection refused", // server not running
		"no such host",      // DNS failure
		"tls:",              // TLS error
		"certificate",       // cert error
	}
	upper := strings.ToUpper(msg)
	for _, pat := range fatalPatterns {
		if strings.Contains(upper, strings.ToUpper(pat)) {
			return false
		}
	}

	// Retryable errors: LLM can likely fix these
	retryablePatterns := []string{
		"unknown column",           // typo in column name
		"unknown table",            // typo in table name
		"doesn't exist",            // table doesn't exist
		"syntax error",             // SQL syntax issue
		"you have an error in your", // MySQL syntax error
		"truncated incorrect",      // data type issue
		"incorrect string value",   // encoding issue
		"invalid use of group",     // GROUP BY issue
		"ambiguous column",         // JOIN ambiguity
		"multiple primary key",     // duplicate key
		"deadlock",                 // transient deadlock
		"lock wait timeout",        // transient lock
		"too many connections",     // might resolve
		"table is marked as crashed", // might self-repair
		"incorrect key value",      // constraint issue
		"data too long",            // column length
		"out of range",             // numeric overflow
		"division by zero",         // logic issue
		"subquery returns more than 1 row", // subquery cardinality
		"subquery",                        // general subquery issues
		"1242",                            // MySQL error code for subquery returns more than 1 row
		"invalid character",        // encoding
		"invalid utf8",             // encoding
		"invalid utf8mb4",          // encoding
		"truncated incorrect",      // type conversion
		"field doesn't have",       // missing field
		"not found",                // might be wrong table reference
		"not exists",               // might be wrong table reference
		"access denied",            // permission on specific table
		"permission denied",        // permission issue
		"command denied",           // MySQL command permission
		"function doesn't exist",   // function call issue
		"column '.*' in field list", // column issue
		"column '.*' in where",     // column issue
		"column '.*' in order by",  // column issue
		"column '.*' in group by",  // column issue
		"column '.*' in having",    // column issue
		"not in group by",          // GROUP BY issue
		"invalid reference",        // JOIN reference issue
		"conflicting types",        // type mismatch
		"can't drop",               // constraint issue
		"duplicate entry",           // constraint issue
		"foreign key constraint",   // FK constraint
		"cannot add foreign key",   // FK constraint
		"cannot truncate",           // permission
		"view's",                   // view issue
		"stored function",          // function issue
		"prepared statement",        // prepared stmt issue
		"invalid collation",         // collation issue
		"incorrect date value",      // date issue
		"incorrect datetime value",  // datetime issue
		"incorrect time value",      // time issue
		"incorrect year value",      // year issue
		"incorrect double value",    // numeric issue
		"overflow",                 // numeric overflow
		"underflow",               // numeric underflow
		"truncated",               // truncation
		"out of memory",           // might resolve
		"temporary file",           // disk issue
		"disk full",               // disk issue
	}
	for _, pat := range retryablePatterns {
		if regexp.MustCompile("(?i)" + pat).MatchString(msg) {
			return true
		}
	}

	// Default: if we can't classify, treat as retryable (optimistic)
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

// validateExplorationQuery checks whether an exploration query is allowed under the given safety mode.
func validateExplorationQuery(sqlQuery string, mode ExplorationSafetyMode) error {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	// Must start with SELECT or WITH (CTE)
	isSelect := strings.HasPrefix(upper, "SELECT")
	isCTE := false
	if strings.HasPrefix(upper, "WITH") && len(upper) > 4 {
		// Check that after "WITH" there is whitespace or '(' (for CTE)
		next := upper[4]
		isCTE = next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '('
	}
	if !isSelect && !isCTE {
		return fmt.Errorf("exploration queries must be SELECT statements")
	}

	// Block all DML/DDL/other dangerous operations
	dangerousPatterns := []string{
		"INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER ", "TRUNCATE ",
		"CREATE ", "REPLACE ", "GRANT ", "REVOKE ", "LOAD_FILE",
		"INTO OUTFILE", "INTO DUMPFILE", "BENCHMARK(", "SLEEP(",
		"EXEC ", "EXECUTE ", "xp_", "sp_",
		"--", "/*", "*/", // block comments
	}
	for _, pat := range dangerousPatterns {
		if strings.Contains(upper, pat) {
			return fmt.Errorf("exploration query blocked: contains '%s'", pat)
		}
	}

	// Count top-level keywords to determine query complexity
	hasJoin := strings.Contains(upper, "JOIN")
	hasSubquery := strings.Contains(upper, "(") && strings.Contains(strings.TrimPrefix(upper, "SELECT"), "SELECT")
	hasUnion := strings.Contains(upper, "UNION")
	hasGroupBy := strings.Contains(upper, "GROUP BY")
	hasOrderBy := strings.Contains(upper, "ORDER BY")

	switch mode {
	case ExplorationStrict:
		if hasJoin {
			return fmt.Errorf("strict mode: JOINs are not allowed in exploration queries")
		}
		if hasSubquery {
			return fmt.Errorf("strict mode: subqueries are not allowed in exploration queries")
		}
		if hasUnion {
			return fmt.Errorf("strict mode: UNION is not allowed in exploration queries")
		}
		if hasGroupBy {
			return fmt.Errorf("strict mode: GROUP BY is not allowed in exploration queries")
		}
		if hasOrderBy {
			return fmt.Errorf("strict mode: ORDER BY is not allowed in exploration queries")
		}
	case ExplorationModerate:
		if hasSubquery {
			return fmt.Errorf("moderate mode: subqueries are not allowed in exploration queries")
		}
		if hasUnion {
			return fmt.Errorf("moderate mode: UNION is not allowed in exploration queries")
		}
		// Check for multi-table JOIN (very rough: count FROM/JOIN keywords)
		fromJoinCount := len(regexp.MustCompile(`\b(FROM|JOIN)\b`).FindAllString(upper, -1))
		if fromJoinCount > 2 {
			return fmt.Errorf("moderate mode: multi-table JOINs are not allowed in exploration queries")
		}
	case ExplorationRelaxed:
		// Only check for truly dangerous patterns (already covered above)
	}

	return nil
}