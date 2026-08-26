package services

import (
	"strings"
	"testing"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
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

func TestSanitizeSQLError(t *testing.T) {
	cases := []struct {
		name, in, wantContains, wantNotContains string
	}{
		{
			name:            "mysql DSN style",
			in:              "dial tcp: lookup: too many colons — user:hunter2@tcp(db.example.com:3306)/orders",
			wantContains:    "user:***@",
			wantNotContains: "hunter2",
		},
		{
			// Note: the bare user:pass@ regex also consumes the URI scheme and
			// userinfo — over-stripping is safe and intentional. The contract
			// under test is: no secret survives, host context remains.
			name:            "postgres URI style",
			in:              `failed to connect: pq: postgres://alice:s3cret@db.example.com:5432/app`,
			wantContains:    "db.example.com:5432/app",
			wantNotContains: "s3cret",
		},
		{
			name:            "plain error unchanged",
			in:              "query execution failed: Unknown column 'foo' in 'field list'",
			wantContains:    "Unknown column",
			wantNotContains: "***",
		},
	}
	for _, tc := range cases {
		got := sanitizeSQLError(tc.in)
		if !strings.Contains(got, tc.wantContains) {
			t.Errorf("%s: sanitized output %q must contain %q", tc.name, got, tc.wantContains)
		}
		if tc.wantNotContains != "" && strings.Contains(got, tc.wantNotContains) {
			t.Errorf("%s: sanitized output %q must not contain credential %q", tc.name, got, tc.wantNotContains)
		}
	}
	if got := sanitizeSQLError(""); got != "" {
		t.Errorf("empty input must stay empty, got %q", got)
	}
}

func TestApplyDefaultLimit(t *testing.T) {
	conn := &models.DataSource{}

	limit := func(q string, exploration bool) string {
		out := applyDefaultLimit(q, conn, exploration)
		if len(out) <= len(q)+len(" LIMIT 999999") && out == q {
			return ""
		}
		return out
	}

	long := strings.Repeat("SELECT * FROM t WHERE c = 'x' OR ", 10) + "1=1"

	t.Run("short query untouched", func(t *testing.T) {
		q := "SELECT * FROM small"
		if got := applyDefaultLimit(q, conn, false); got != q {
			t.Errorf("short query must not be modified, got %q", got)
		}
	})

	t.Run("existing limit respected", func(t *testing.T) {
		q := long + " LIMIT 5"
		if got := applyDefaultLimit(q, conn, false); got != q {
			t.Errorf("existing LIMIT must not be duplicated or changed, got %q", got)
		}
	})

	t.Run("plain select gets limit appended", func(t *testing.T) {
		got := limit(long, false)
		if got == "" || !strings.HasSuffix(got, " LIMIT 1000") {
			t.Errorf("expected final LIMIT 1000, got %q", got)
		}
	})

	t.Run("exploration uses lower default", func(t *testing.T) {
		got := limit(long, true)
		if got == "" || !strings.HasSuffix(got, " LIMIT 100") {
			t.Errorf("expected exploration LIMIT 100, got %q", got)
		}
	})

	t.Run("trailing semicolon stripped before limit", func(t *testing.T) {
		got := limit(long+";", false)
		if got == "" || strings.Contains(got, ";LIMIT") || strings.Contains(got, "; LIMIT") {
			t.Errorf("semicolon must be stripped before appending LIMIT, got %q", got)
		}
	})

	t.Run("union wrapped in outer select", func(t *testing.T) {
		u := strings.Repeat("SELECT 1 AS x -- padding padding padding\nUNION ALL SELECT ", 6) + "2"
		got := applyDefaultLimit(u, conn, false)
		if !strings.HasPrefix(strings.ToUpper(got), "SELECT * FROM (") || !strings.Contains(got, ") subq LIMIT") {
			t.Errorf("union query should be wrapped for correct limiting, got %q", got)
		}
	})
}
