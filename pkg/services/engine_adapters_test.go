package services

import (
	"strings"
	"testing"

	"YourQL/pkg/engine"
	"YourQL/pkg/models"
)

// Defense-in-depth suite for QueryExecutorAdapter (TECH_REVIEW_20260824.md
// F-4 / P2). The engine loop gates exploration queries, and
// executeSQLWithMode enforces the read-only invariant — but the charter
// (§1.4) requires the adapter itself to re-check exploration complexity so
// no single point of failure exists. These tests prove the adapter's gate
// fires BEFORE any driver/DSN work: a rejected query must fail without
// needing (or touching) a real database.

func newTestAdapter(mode engine.ExplorationSafetyMode) *QueryExecutorAdapter {
	return &QueryExecutorAdapter{
		DataSource: &models.DataSource{Type: "sqlite"},
		SafetyMode: mode,
	}
}

func TestQueryExecutorAdapter_RejectsWritesWithoutDatabase(t *testing.T) {
	writes := []string{
		"DELETE FROM t",
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DROP TABLE t",
		"SELECT * FROM t; DROP TABLE t",
	}
	for _, isExploration := range []bool{true, false} {
		for _, q := range writes {
			adapter := newTestAdapter(engine.ExplorationRelaxed)
			_, err := adapter.Execute(q, isExploration)
			if err == nil {
				t.Errorf("isExploration=%v: adapter must reject write %q", isExploration, q)
				continue
			}
			if strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "DSN") || strings.Contains(err.Error(), "dial") {
				t.Errorf("isExploration=%v: %q was rejected by connection layer, not by safety validation — read-only gate fired too late", isExploration, q)
			}
		}
	}
}

func TestQueryExecutorAdapter_ExplorationComplexityGate(t *testing.T) {
	strictBlocked := []string{
		"SELECT a FROM t JOIN u ON t.id = u.id",
		"SELECT * FROM t UNION SELECT * FROM u",
	}
	for _, q := range strictBlocked {
		_, err := newTestAdapter(engine.ExplorationStrict).Execute(q, true)
		if err == nil {
			t.Errorf("strict exploration must reject %q", q)
		} else if !strings.Contains(err.Error(), "mode") && !strings.Contains(err.Error(), "statements") {
			t.Errorf("strict exploration rejection for %q came from wrong layer: %v", q, err)
		}
	}
}

func TestQueryExecutorAdapter_FinalQueriesSkipComplexityGate(t *testing.T) {
	// A complex FINAL query must pass the complexity gate (it will then fail
	// on connection — which proves it got past validation).
	_, err := newTestAdapter(engine.ExplorationStrict).Execute(
		"SELECT a FROM t JOIN u ON t.id = u.id", false)
	if err == nil {
		t.Fatal("expected connection failure (no such db), got success")
	}
	if strings.Contains(err.Error(), "mode") {
		t.Errorf("final query wrongly hit the exploration complexity gate: %v", err)
	}
}
