package engine

import (
	"regexp"
	"strings"
)

// AugmentSQLError detects common SQL error patterns and appends schema
// guidance to help the LLM self-correct.
func AugmentSQLError(errStr string) string {
	if idx := strings.Index(errStr, "Error 1054"); idx != -1 {
		return errStr + "\n\n[Hint: This is a schema error — a column in your query doesn't exist on the table you referenced. Check:\n1. Did you reference the correct table/alias for this column?\n2. Is the column on a different table that needs a JOIN? Use `products p ON od.productCode = p.productCode` then reference `p.columnName`.\n3. Check table names and column names against the schema above.\n4. Fix the column reference and retry.]"
	}
	return errStr
}

// IsRetryableError determines whether a SQL execution error is retryable.
func IsRetryableError(err error) bool {
	msg := err.Error()
	upper := strings.ToUpper(msg)

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

	if looksLikeSQLError(msg) {
		return true
	}

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

	return false
}

// looksLikeSQLError checks whether an error message matches the broad
// structural signature of a database-level SQL error.
func looksLikeSQLError(msg string) bool {
	upper := strings.ToUpper(msg)

	if regexp.MustCompile(`(?i)(error|msg|sqlstate)\s+\d+`).MatchString(msg) {
		return true
	}

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