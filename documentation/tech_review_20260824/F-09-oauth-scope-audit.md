# F-9 · Runtime audit assertion that OAuth scopes are read-only

**Severity:** Medium · **Effort:** Small · **Risk categories:** data safety (charter §0 native-integration clause)

## Problem Statement

Charter §0 requires: *"Native, non-`database/sql` integrations (BigQuery,
Google Sheets) must request and use read-only API scopes only. Never expand
an OAuth scope or API permission to include write access."*

Today this rule lives only in documentation and in whatever strings
`google_auth.go` happens to request (line ~29: *"read-only access to
spreadsheets"*). Nothing enforces it:

- A future enhancement adding Sheets import/export, calendar lookups, or
  Drive browsing could quietly widen the requested scope string; review
  might miss it among unrelated diffs.
- Nothing fails loudly if a scope constant is edited incorrectly (e.g.,
  `sheets.readonly` → `sheets` — one deleted word, full write power).
- BigQuery client construction isn't checked either.

## Solution Design

Make the allowed scope set an explicit, self-auditing contract.

```go
// pkg/services/google_scopes.go

// readOnlyScopes is the COMPLETE set of OAuth scopes YourQL may ever
// request against external services. Every scope grants read-only access.
// Adding anything here requires human product review (AGENT_READ_FIRST.md
// §0: never expand OAuth permissions to include write access) and must be
// flagged in documentation/RISK_ANALYSIS_LOG.md.
var readOnlyScopes = []string{
    "https://www.googleapis.com/auth/spreadsheets.readonly",
    "https://www.googleapis.com/auth/bigquery.readonly",
    "https://www.googleapis.com/auth/drive.readonly", // if used for file listing today
}

func init() {
    for _, s := range readOnlyScopes {
        if !strings.HasSuffix(s, ".readonly") {
            panic("google_auth: scope missing .readonly suffix: " + s +
                " — refusing to start (AGENT_READ_FIRST.md §0)")
        }
    }
}
```

Then make every token-request site consume **only** this slice:

```go
config.Scopes = readOnlyScopes
```

and add a compile-time-ish guard test (F-4 suite):

```go
func TestAllGoogleScopesAreReadOnly(t *testing.T) {
    for _, s := range readOnlyScopes {
        if !strings.HasSuffix(s, ".readonly") { t.Fatalf("writable scope: %s", s) }
    }
    // Also assert google_auth.go requests exactly this set:
    // refactor auth code to expose requestedScopes() returning readOnlyScopes
    // so no second literal list can exist.
}
```

The `.readonly` suffix convention is Google's own namespace design —
leveraging it turns a policy into a checkable invariant. Any genuinely new
read-only scope family lacking the suffix (rare) would need an explicit,
commented exception entry reviewed by a human.

For BigQuery's non-OAuth paths (service-account JSON keys), the equivalent
control is documentation + startup logging of the granted scopes/roles where
the API exposes them; the key file itself defines authority, so add a docs
requirement in setup UI copy: *"Use a viewer-role service account."*
(`bigquery.jobs.create` + dataViewer roles.)

## Implementation Plan

1. Create `google_scopes.go` with the canonical slice + init guard.
2. Refactor `google_auth.go` (and `db_bigquery.go` if it builds its own
   config) to reference the slice; delete all inline scope literals
   (`grep -rn "googleapis.com/auth" pkg/` must show only google_scopes.go).
3. Add the guard test; wire into CI (F-3).
4. Add setup-copy guidance for service accounts (viewer role) in the data
   source form help text.
5. One-line update to charter §0 pointing at google_scopes.go as the
   enforcement point (same-commit doc rule).

## Risk Assessment

- **Failure mode:** `panic` in init is aggressive — but it fires only when a
  developer adds a writable scope, i.e., exactly when failing loudly is
  correct (fail-closed, charter-aligned). Alternative: log + strip the bad
  scope; rejected because silently degrading permissions hides mistakes too.
- No behavior change in normal operation; token refreshes unaffected because
  the granted sets match what's already requested.
- Charter §4.3 case 2. Proceed.
