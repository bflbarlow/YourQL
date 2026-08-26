# F-20 · Commit hygiene — land the pending reorganization

**Severity:** Low · **Effort:** Trivial · **Risk categories:** regression (process), maintainability

## Problem Statement

As of 2026-08-24 the working tree carries a large uncommitted set:

```
D  .DS_Store                      M .gitignore
M  README.md                      M app.go
D  documentation/AGENT_LOOP_CONFIG.md
D  documentation/ANSWER_CLARIFICATION_ISSUE.md
D  documentation/... (dozens more moves/deletions)
```

Risks while this sits uncommitted:

1. **Blame/history confusion** — `app.go` modifications mix with whatever
   lands next; bisecting future regressions becomes guesswork.
2. **Agent-workflow hazard** — this project is explicitly built around
   autonomous agents reading the tree; deleted-but-present docs (and
   present-but-deleted status) cause agents to cite documents that no longer
   exist (see F-19 item 3) or miss ones that moved.
3. **Accidental loss** — uncommitted deletions/modifications are one
   careless `git checkout .` away from vanishing.

## Solution Design

Land the reorganization as a dedicated, well-described housekeeping commit,
separate from any functional change:

```
git add -A
git commit -m "chore: reorganize documentation; drop .DS_Store tracking; housekeeping

- Move review/enhancement docs into documentation/{operations,enhancements,issues,testing}
- Untrack .DS_Store; ensure .gitignore covers macOS artifacts
- README/app.go updates staged separately reviewed"
```

Prefer splitting into two commits if `app.go` changes turn out to be
functional rather than cosmetic — inspect first:

```bash
git diff --stat app.go
git diff app.go | head -80
```

Rules going forward (add to team/agent conventions):

- Documentation moves land in their own commits with `docscope:` prefix.
- `.DS_Store` must never be tracked again — `.gitignore` entry exists;
  also run `git rm --cached .DS_Store` equivalents whenever spotted
  (already reflected in the staged deletions here).
- Tag releases consistently (`v0.4.5` style exists); keep tag ↔
  `appVersion` ldflags linkage per RELEASE_DEPLOYMENT.md so updater checks
  behave (ties into F-6).

## Implementation Plan

1. Review the `app.go` diff; split functional vs. cosmetic if needed.
2. Single housekeeping commit for docs/.gitignore/.DS_Store.
3. Push; confirm CI (F-3) green on the result.
4. Optional: add `.gitignore` entries for OS cruft completeness
   (`.DS_Store` already there; consider `Thumbs.db`, `*.log`).

## Risk Assessment

None beyond normal commit review. Purely process. Charter §4.3 case 3.
