package services

import (
	"testing"

	"YourQL/pkg/engine"
)

func TestValidateReadOnlySQL_AllowsSelects(t *testing.T) {
	cases := []string{
		"SELECT * FROM orders",
		"SELECT id, name FROM users WHERE id = 1",
		"WITH recent AS (SELECT * FROM orders WHERE created_at > NOW()) SELECT * FROM recent",
		"(SELECT id FROM a) UNION ALL (SELECT id FROM b)",
		"SELECT COUNT(*) AS total FROM t GROUP BY x ORDER BY total",
		// "INTO" inside a string literal must not be treated as table creation.
		"SELECT 'INTO the woods' AS title FROM t",
		// Variable assignment is not a persisted write.
		"SELECT total INTO @running_total FROM t",
		// Comments are stripped before pattern matching.
		"SELECT * FROM t -- trailing note\nWHERE id = 1",
	}
	for _, q := range cases {
		if err := engine.ValidateReadOnlySQL(q); err != nil {
			t.Errorf("expected allow, got error for %q: %v", q, err)
		}
	}
}

func TestValidateReadOnlySQL_BlocksWrites(t *testing.T) {
	cases := []string{
		"INSERT INTO t (a) VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"DROP TABLE t",
		"ALTER TABLE t ADD COLUMN x INT",
		"TRUNCATE TABLE t",
		"CREATE TABLE t (x INT)",
		"REPLACE INTO t VALUES (1)",
		"GRANT SELECT ON t TO u",
		"REVOKE SELECT ON t FROM u",
		"MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.a = s.a",
		// Write operations that start with SELECT / WITH and therefore pass a
		// naive prefix check:
		"SELECT * FROM t FOR UPDATE",
		"SELECT * INTO newtable FROM src",
		"SELECT a, b INTO OUTFILE '/tmp/x' FROM t",
		"SELECT a, b INTO DUMPFILE '/tmp/x' FROM t",
		"WITH c AS (SELECT * FROM t) INSERT INTO u SELECT * FROM c",
		"WITH c AS (SELECT * FROM t) UPDATE u SET x = 1 FROM c",
		"WITH c AS (SELECT * FROM t) DELETE FROM u USING c",
		"SELECT LOAD_FILE('/etc/passwd')",
		"EXEC sp_help",
		// Non-SELECT prefixes.
		"COPY t TO '/tmp/t'",
		"CALL some_side_effecting_proc()",
	}
	for _, q := range cases {
		if err := engine.ValidateReadOnlySQL(q); err == nil {
			t.Errorf("expected block, got allow for %q", q)
		}
	}
}

func TestValidateReadOnlySQL_CommentBypassBlocked(t *testing.T) {
	// A write hidden behind a comment must still be caught. The line comment
	// itself would remove the visible keyword, so use a block comment form
	// that leaves the write in place after stripping.
	q := "SELECT * FROM t; DROP TABLE t"
	if err := engine.ValidateReadOnlySQL(q); err == nil {
		t.Errorf("expected block for %q", q)
	}
}

func TestValidateExplorationQuery_ComplexityModes(t *testing.T) {
	strictBlocked := []string{
		"SELECT a FROM t JOIN u ON t.id = u.id",
		"SELECT * FROM (SELECT * FROM t) s",
		"SELECT * FROM t UNION SELECT * FROM u",
		"SELECT * FROM t GROUP BY x",
		"SELECT * FROM t ORDER BY x",
	}
	for _, q := range strictBlocked {
		if err := engine.ValidateExplorationQuery(q, engine.ExplorationStrict); err == nil {
			t.Errorf("strict: expected block for %q", q)
		}
	}
	if err := engine.ValidateExplorationQuery("SELECT * FROM t", engine.ExplorationStrict); err != nil {
		t.Errorf("strict: expected allow for simple SELECT, got %v", err)
	}

	moderateBlocked := []string{
		"SELECT * FROM (SELECT * FROM t) s",
		"SELECT * FROM t UNION SELECT * FROM u",
	}
	for _, q := range moderateBlocked {
		if err := engine.ValidateExplorationQuery(q, engine.ExplorationModerate); err == nil {
			t.Errorf("moderate: expected block for %q", q)
		}
	}
	if err := engine.ValidateExplorationQuery("SELECT a FROM t JOIN u ON t.id = u.id", engine.ExplorationModerate); err != nil {
		t.Errorf("moderate: expected allow for simple join, got %v", err)
	}

	relaxedAllowed := []string{
		"SELECT * FROM (SELECT * FROM t) s",
		"SELECT * FROM t UNION SELECT * FROM u",
		"SELECT a FROM t JOIN u ON t.id = u.id",
	}
	for _, q := range relaxedAllowed {
		if err := engine.ValidateExplorationQuery(q, engine.ExplorationRelaxed); err != nil {
			t.Errorf("relaxed: expected allow for %q, got %v", q, err)
		}
	}
}

func TestValidateExplorationQuery_ReadOnlyEnforcedInAllModes(t *testing.T) {
	// Complexity modes must never relax the read-only invariant.
	writes := []string{
		"INSERT INTO t (a) VALUES (1)",
		"SELECT * INTO newtable FROM src",
		"WITH c AS (SELECT * FROM t) DELETE FROM u USING c",
	}
	for _, mode := range []engine.ExplorationSafetyMode{engine.ExplorationStrict, engine.ExplorationModerate, engine.ExplorationRelaxed} {
		for _, q := range writes {
			if err := engine.ValidateExplorationQuery(q, mode); err == nil {
				t.Errorf("mode=%d: expected read-only block for %q", mode, q)
			}
		}
	}
}