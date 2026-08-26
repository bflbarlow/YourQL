# F-19 · Dead code and minor smells cleanup

**Severity:** Low · **Effort:** Trivial · **Risk categories:** maintainability

## Problem Statement

Small verified items; each is cheap but collectively they erode trust in
reading the code ("if this is dead, what else lies?") and occasionally hide
real bugs.

### Item 1 — Unused `hasContent` in `checkSingleStatement`

`pkg/engine/safety.go`:

```go
case c == '\'':
    inString = true
    hasContent = true     // set here...
...
_ = hasContent            // ...then explicitly discarded
return nil
```

The variable is assigned in two branches and discarded. Either:
- **Delete it** (recommended — the function's contract is "reject >1
  statement"; emptiness is handled upstream by `strings.HasPrefix(upper,
  …)` checks), or
- Use it: `if !hasContent { return fmt.Errorf("empty statement") }` — but
  note callers may pass whitespace-only queries today and rely on downstream
  "only SELECT allowed" errors; changing the error surface needs a quick
  check of what the LLM retry path expects. Deletion is zero-risk.

### Item 2 — Unreachable branch in `applyDefaultLimit`

`pkg/services/sql_execution.go`:

```go
if threshold == 0 { threshold = queryLengthThreshold }   // queryLengthThreshold = 200
...
if threshold == 0 { return sqlQuery }                    // unreachable
```

The second check presumably guarded a former config value of 0 meaning
"disable". If disabling is intended to remain possible via
`QueryLengthThreshold == 0` in data-source config, the *first* assignment
destroyed that semantic by defaulting over it. Decide intent:
- If "0 disables limiting": fix order — treat explicit config 0 as disable
  before applying the default (behavior change! document + RISK_ANALYSIS_LOG).
- If "0 not special": delete the dead branch.
Recommended: preserve disable-semantics deliberately (a power-user escape
hatch aligns with per-source configurability elsewhere) — implement:

```go
threshold := queryLengthThreshold
if conn != nil {
    if cfg, err := conn.ParseConfig(); err == nil {
        if cfg.QueryLengthThreshold < 0 { return sqlQuery }       // explicit disable
        if cfg.QueryLengthThreshold > 0 { threshold = cfg.QueryLengthThreshold }
    }
}
```

(Negative = disable avoids ambiguity with legacy stored zeros.)

### Item 3 — Empty conditional in `AssistantResponse.ToHTML`

```go
if r.SQL != "" {
    // SQL is now shown in the results toolbar toggle, not as a separate block
}
```

Dead scaffold from the pre-toolbar design. Delete; the comment lives better
as a note near the toolbar renderer.

### Item 4 — Documentation anchor rot risk

Charter §3.8 cites "`VERSION_UPGRADE.md` §'Security Considerations'" and
many docs were moved/deleted in the pending reorganization (F-20). Sweep
after F-20 lands:

```bash
# extract markdown links + section refs from all docs; verify targets exist
grep -rnoE '\]\(([^)]+\.md)' documentation README.md | sort -u
grep -rn 'TECH_REVIEW\|_ENHANCEMENT.md\|_ISSUE.md' documentation/AGENT_READ_FIRST.md
```

Fix or remove dangling references so agents never chase ghosts.

## Implementation Plan

Single small PR: items 1–3 (code) + item 4 sweep (docs). All changes are
deletions or clearly-commented semantics fixes. Run full test suite + one
manual query against SQLite to confirm rendering path unchanged.

## Risk Assessment

Only Item 2 has behavioral surface (limit-disable semantics); resolved by
the explicit decision above and covered by F-2/F-4's golden tests. Items
1/3/4 are inert deletions. Charter §4.3 case 3.
