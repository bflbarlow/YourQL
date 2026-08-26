package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// ExplorationSafetyMode controls what types of exploration queries are permitted.
type ExplorationSafetyMode int

const (
	ExplorationStrict ExplorationSafetyMode = iota
	ExplorationModerate
	ExplorationRelaxed
)

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

// ValidateReadOnlySQL enforces the Data Source Read-Only Invariant
// (AGENT_READ_FIRST.md §0): every statement executed against a configured
// data source must be a SELECT or a read-only CTE. This is the single choke
// point used by executeSQLWithMode for both exploration and final queries,
// blocking DML/DDL and write-adjacent patterns before a statement ever
// reaches a driver.
func ValidateReadOnlySQL(sqlQuery string) error {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	// Single-statement enforcement: stacked statements ("SELECT 1; CALL
	// proc()") must never reach a driver, since only the first statement's
	// prefix would look read-only and side-effecting calls (CALL/EXEC) are
	// not otherwise pattern-matchable. A single trailing semicolon is fine.
	if err := checkSingleStatement(trimmed); err != nil {
		return err
	}

	isSelect := strings.HasPrefix(upper, "SELECT")
	isCTE := false
	if strings.HasPrefix(upper, "WITH") && len(upper) > 4 {
		next := upper[4]
		isCTE = next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '('
	}
	// Allow parenthesized SELECTs (e.g. "(SELECT ...) UNION ALL (SELECT ...)")
	if !isSelect && !isCTE && strings.HasPrefix(upper, "(") {
		unwrapped := strings.TrimLeft(upper[1:], " \t\n\r")
		isSelect = strings.HasPrefix(unwrapped, "SELECT")
		isCTE = strings.HasPrefix(unwrapped, "WITH")
	}
	if !isSelect && !isCTE {
		return fmt.Errorf("only SELECT statements are allowed")
	}

	// Block DML/DDL and write-adjacent patterns — strip comments first, then
	// mask string literals so keywords inside them ('DELETE FROM t' as data)
	// cannot cause false blocks on legitimate queries.
	stripped := MaskStringLiterals(StripSQLComments(upper))
	dangerousPatterns := []string{
		"INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER ", "TRUNCATE ",
		"CREATE ", "REPLACE ", "GRANT ", "REVOKE ", "LOAD_FILE",
		"INTO OUTFILE", "INTO DUMPFILE", "BENCHMARK(", "SLEEP(",
		"EXEC ", "EXECUTE ", "xp_", "sp_",
		"MERGE INTO", "FOR UPDATE",
	}
	for _, pat := range dangerousPatterns {
		if strings.Contains(stripped, pat) {
			return fmt.Errorf("query blocked: contains '%s'", pat)
		}
	}

	// SELECT ... INTO <table> is a write that starts with SELECT.
	if containsSelectIntoTable(stripped) {
		return fmt.Errorf("query blocked: contains 'SELECT INTO' (table creation)")
	}

	return nil
}

// ValidateExplorationQuery checks whether an exploration query is allowed:
// it must be read-only (ValidateReadOnlySQL) and satisfy the configured
// exploration complexity mode (joins, subqueries, unions).
func ValidateExplorationQuery(sqlQuery string, mode ExplorationSafetyMode) error {
	if err := ValidateReadOnlySQL(sqlQuery); err != nil {
		return err
	}

	upper := strings.ToUpper(strings.TrimSpace(sqlQuery))

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
		// Read-only only; no complexity limits.
	}

	return nil
}

// StripSQLComments removes SQL line and block comments from the query text.
func StripSQLComments(s string) string {
	// Remove block comments
	blockRe := regexp.MustCompile(`/\*.*?\*/`)
	s = blockRe.ReplaceAllString(s, "")
	// Remove line comments
	lineRe := regexp.MustCompile(`--.*$`)
	s = lineRe.ReplaceAllString(s, "")
	return s
}

// MaskStringLiterals replaces the contents of single-quoted SQL string
// literals with a placeholder of equal length, so keyword scans over the
// masked text cannot match on data that merely looks like SQL. Quote-aware:
// handles doubled (”) escaped quotes. Call only after comment stripping.
func MaskStringLiterals(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteString("__") // escaped quote pair
					i++
					continue
				}
				inString = false
				b.WriteByte(c)
				continue
			}
			b.WriteByte('_') // mask literal content
			continue
		}
		if c == '\'' {
			inString = true
			b.WriteByte(c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// checkSingleStatement reports an error when sqlQuery contains more than one
// statement. Semicolons inside string literals, line comments, and block
// comments are ignored; a trailing empty statement is allowed.
func checkSingleStatement(sqlQuery string) error {
	inString, inLineComment, inBlockComment := false, false, false
	hasContent := false // content seen in the current statement
	for i := 0; i < len(sqlQuery); i++ {
		c := sqlQuery[i]
		switch {
		case inLineComment:
			if c == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			if c == '*' && i+1 < len(sqlQuery) && sqlQuery[i+1] == '/' {
				inBlockComment = false
				i++
			}
		case inString:
			if c == '\'' {
				// Doubled quote ('') is an escaped quote, not a boundary.
				if i+1 < len(sqlQuery) && sqlQuery[i+1] == '\'' {
					i++
				} else {
					inString = false
				}
			}
		case c == '\'':
			inString = true
			hasContent = true
		case c == '-' && i+1 < len(sqlQuery) && sqlQuery[i+1] == '-':
			inLineComment = true
			i++
		case c == '/' && i+1 < len(sqlQuery) && sqlQuery[i+1] == '*':
			inBlockComment = true
			i++
		case c == ';':
			rest := strings.TrimSpace(sqlQuery[i+1:])
			if rest != "" && !onlyComments(rest) {
				return fmt.Errorf("query blocked: multiple statements are not allowed")
			}
			return nil // nothing but a trailing (possibly commented) semicolon
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				hasContent = true
			}
		}
	}
	_ = hasContent
	return nil
}

// onlyComments reports whether s consists solely of whitespace and SQL
// comments (line or block). Used to allow a trailing comment after the final
// semicolon without counting it as a second statement.
func onlyComments(s string) bool {
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if strings.HasPrefix(s, "--") {
			if idx := strings.IndexByte(s, '\n'); idx >= 0 {
				s = s[idx+1:]
				continue
			}
			return true
		}
		if strings.HasPrefix(s, "/*") {
			idx := strings.Index(s, "*/")
			if idx < 0 {
				return true // unterminated block comment swallows the rest
			}
			s = s[idx+2:]
			continue
		}
		return s == ""
	}
}

// containsSelectIntoTable detects "SELECT ... INTO <table>", the table-creation
// form that writes despite starting with SELECT. It ignores harmless INTO
// forms: INTO @variable (variable assignment) and INTO OUTFILE / INTO
// DUMPFILE (matched separately above).
func containsSelectIntoTable(sql string) bool {
	re := regexp.MustCompile(`\bINTO\s+`)
	for _, loc := range re.FindAllStringIndex(sql, -1) {
		if insideSingleQuotedString(sql, loc[0]) {
			continue // "INTO" inside a string literal, not a clause
		}
		rest := strings.TrimSpace(sql[loc[1]:])
		if rest == "" {
			continue
		}
		if rest[0] == '@' {
			continue // SELECT ... INTO @variable — variable assignment
		}
		upperRest := strings.ToUpper(rest)
		if strings.HasPrefix(upperRest, "OUTFILE") || strings.HasPrefix(upperRest, "DUMPFILE") {
			continue // already blocked above; not table creation
		}
		return true
	}
	return false
}

// insideSingleQuotedString reports whether byte offset i of s lies within a
// single-quoted SQL string literal. It is a naive scan (no backtick/double-
// quote/dollar-quote awareness) sufficient for the read-only guard, which is
// deliberately biased toward blocking rather than allowing.
func insideSingleQuotedString(s string, i int) bool {
	inString := false
	for j := 0; j < i && j < len(s); j++ {
		if s[j] != '\'' {
			continue
		}
		// Doubled quote ('' ) is an escaped quote, not a string boundary.
		if j+1 < len(s) && s[j+1] == '\'' {
			j++
			continue
		}
		inString = !inString
	}
	return inString
}
