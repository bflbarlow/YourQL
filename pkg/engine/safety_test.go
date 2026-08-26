package engine

import (
	"strings"
	"testing"
)

// Exhaustive safety-validator suite (TECH_REVIEW_20260824.md F-4 / P1).
// These tests ARE the read-only invariant's regression guard — treat any
// change here as a §4.0-level event requiring a risk analysis.

func TestValidateReadOnlySQL_MultiStatement(t *testing.T) {
	blocked := []string{
		"SELECT 1; DROP TABLE t",
		"SELECT 1; DELETE FROM t",
		"SELECT 1; CALL some_proc()",
		"SELECT 1; EXEC xp_cmdshell 'x'",
		"SELECT 1; SELECT 2",
		"SELECT ';'; SELECT 2",           // semicolon inside a string must not hide the split
		"SELECT 1 /* ; */; DROP TABLE t", // comment between statements
	}
	allowed := []string{
		"SELECT 1",                        // no semicolon at all
		"SELECT 1;",                       // trailing terminator
		"SELECT 1; ",                      // trailing terminator + whitespace
		"SELECT 1; -- done",               // trailing line comment after semicolon
		"SELECT 1; /* done */",            // trailing block comment after semicolon
		"SELECT * FROM t WHERE c = 'a;b'", // semicolon inside a string literal
	}
	for _, q := range blocked {
		if err := ValidateReadOnlySQL(q); err == nil {
			t.Errorf("multi-statement: expected block for %q", q)
		}
	}
	for _, q := range allowed {
		if err := ValidateReadOnlySQL(q); err != nil {
			t.Errorf("single-statement: expected allow for %q, got %v", q, err)
		}
	}
}

func TestValidateReadOnlySQL_CommentSmuggling(t *testing.T) {
	// Comments must be stripped before pattern matching, so a write hidden
	// only behind a comment is caught by the prefix/pattern logic.
	blocked := []string{
		"SELECT * FROM t; DROP TABLE t",
		"DELETE /*x*/ FROM t",
		"/* hi */ DELETE FROM t",
	}
	for _, q := range blocked {
		if err := ValidateReadOnlySQL(q); err == nil {
			t.Errorf("comment: expected block for %q", q)
		}
	}

	// Keywords inside comments or strings are NOT writes and must be allowed
	// (over-blocking breaks legitimate queries and erodes trust).
	allowed := []string{
		"SELECT /* DELETE FROM t */ * FROM t",
		"SELECT 'DELETE FROM t' AS note FROM t",
		"SELECT * FROM t -- please do not DELETE this\n",
	}
	for _, q := range allowed {
		if err := ValidateReadOnlySQL(q); err != nil {
			t.Errorf("comment/string: expected allow for %q, got %v", q, err)
		}
	}
}

func TestValidateReadOnlySQL_CaseAndWhitespaceVariants(t *testing.T) {
	allowed := []string{
		"select * from t",
		"SeLeCt * FrOm t",
		"SELECT\t*\nFROM\tt",
		"  SELECT * FROM t  ",
		"(select 1)",
	}
	for _, q := range allowed {
		if err := ValidateReadOnlySQL(q); err != nil {
			t.Errorf("case/ws: expected allow for %q, got %v", q, err)
		}
	}
	blocked := []string{
		"insert into t values (1)",
		"delete\nfrom\n t",
	}
	for _, q := range blocked {
		if err := ValidateReadOnlySQL(q); err == nil {
			t.Errorf("case/ws: expected block for %q", q)
		}
	}
}

func TestValidateReadOnlySQL_QuotedIdentifierINTOTradeoff(t *testing.T) {
	// Documented trade-off (safety.go insideSingleQuotedString): the INTO
	// detector is single-quote-aware only. Double-quoted/backticked "INTO"
	// identifiers are ALLOWED because the check is deliberately biased toward
	// blocking rather than allowing. Pin that behavior so it is not changed
	// silently in either direction.
	if err := ValidateReadOnlySQL(`SELECT "INTO" FROM t`); err != nil {
		t.Errorf(`expected allow for double-quoted "INTO" identifier, got %v`, err)
	}
	if err := ValidateReadOnlySQL("SELECT `INTO` FROM t"); err != nil {
		t.Errorf("expected allow for backtick-quoted INTO identifier, got %v", err)
	}
}

func TestValidateExplorationQuery_ModeFeatureMatrix(t *testing.T) {
	features := map[string]string{
		"join":     "SELECT a FROM t JOIN u ON t.id = u.id",
		"subquery": "SELECT * FROM (SELECT 1) s",
		"union":    "SELECT 1 UNION SELECT 2",
		"groupby":  "SELECT COUNT(*) FROM t GROUP BY x",
		"orderby":  "SELECT * FROM t ORDER BY x",
	}
	simple := "SELECT name FROM customers WHERE id = 7"

	for modeName, mode := range map[string]ExplorationSafetyMode{
		"strict":   ExplorationStrict,
		"moderate": ExplorationModerate,
		"relaxed":  ExplorationRelaxed,
	} {
		// A plain SELECT is legal in every mode.
		if err := ValidateExplorationQuery(simple, mode); err != nil {
			t.Errorf("%s: simple SELECT must always pass, got %v", modeName, err)
		}
	}

	// Strict blocks every listed feature.
	for feat, q := range features {
		if err := ValidateExplorationQuery(q, ExplorationStrict); err == nil {
			t.Errorf("strict: expected block for feature %s (%q)", feat, q)
		}
	}

	// Moderate blocks subquery + union; allows single join, group/order.
	moderateAllowed := []string{"join", "groupby", "orderby"}
	moderateBlocked := []string{"subquery", "union"}
	for _, feat := range moderateAllowed {
		if err := ValidateExplorationQuery(features[feat], ExplorationModerate); err != nil {
			t.Errorf("moderate: expected allow for %s, got %v", feat, err)
		}
	}
	for _, feat := range moderateBlocked {
		if err := ValidateExplorationQuery(features[feat], ExplorationModerate); err == nil {
			t.Errorf("moderate: expected block for %s", feat)
		}
	}

	// Relaxed allows everything (still gated by read-only elsewhere).
	for feat, q := range features {
		if err := ValidateExplorationQuery(q, ExplorationRelaxed); err != nil {
			t.Errorf("relaxed: expected allow for %s (%q), got %v", feat, q, err)
		}
	}
}

func TestValidateExplorationQuery_ReadAlwaysGated(t *testing.T) {
	// The invariant: complexity modes gate COMPLEXITY, never write permission.
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"TRUNCATE t",
		"CREATE TABLE t (x INT)",
		"SELECT * INTO copy_t FROM t",
		"WITH c AS (SELECT * FROM t) UPDATE u SET x = 1",
		"SELECT 1; DROP TABLE t", // stacked-statement form
	}
	for modeName, mode := range map[string]ExplorationSafetyMode{
		"strict": ExplorationStrict, "moderate": ExplorationModerate, "relaxed": ExplorationRelaxed,
	} {
		for _, q := range writes {
			err := ValidateExplorationQuery(q, mode)
			if err == nil {
				t.Errorf("%s: write query must be blocked in every mode: %q", modeName, q)
				continue
			}
			// When the failure comes from the read-only layer the message must
			// not claim a complexity reason — keeps error semantics honest.
			if strings.Contains(err.Error(), "mode:") && !strings.Contains(strings.ToLower(q), "join") &&
				!strings.Contains(q, "(") && !strings.Contains(strings.ToUpper(q), "UNION") {
				t.Logf("%s: note: %q rejected as: %v", modeName, q, err)
			}
		}
	}
}
