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

	// Block DML/DDL and write-adjacent patterns — strip comments first.
	stripped := StripSQLComments(upper)
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