# F-3 · Continuous Integration pipeline *(referenced by `.githooks/pre-commit`)*

**Severity:** High (process) · **Effort:** Small · **Risk categories:** regression across all categories

## Problem Statement

There is no `.github/` directory — nothing runs automatically on push or PR.
Quality enforcement today consists solely of `.githooks/pre-commit`
(gofmt check, `go build ./...`, engine+services test run), which:

1. **Is opt-in per clone**: active only after
   `git config core.hooksPath .githooks`. Fresh clones, CI bots, other
   machines, and autonomous agents get zero enforcement.
2. **Covers Go only**: frontend build (`vite build`), Svelte compilation,
   and dependency installation are never verified anywhere automated.
3. **Cannot gate merges**: hooks are advisory to anyone using
   `--no-verify` and invisible to PR review.

Given the project's own charter mandates (§5 Testing Checklist,
§3 safety rules), the absence of an authoritative gate is the single
highest-leverage process fix: every other finding's fix becomes enforceable
once CI exists.

## Solution Design

Minimal, fast, dependency-light workflow. Two jobs, run in parallel.

### `.github/workflows/ci.yml`

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:

jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: gofmt (fail on unformatted)
        run: test -z "$(gofmt -l . )" || { gofmt -l .; exit 1; }

      - name: go vet
        run: go vet ./...

      - name: build
        run: go build ./...

      - name: test (with race detector)
        run: go test -race -coverprofile=coverage.txt ./...
        env:
          CGO_ENABLED: 0   # modernc.org/sqlite is pure Go; keeps runner simple

  frontend:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: frontend
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: frontend/package-lock.json

      - run: npm ci
      - run: npm run build     # vite build — catches Svelte compile errors
```

### Design decisions & rationale

| Decision | Rationale |
|---|---|
| No full `wails build` initially | Requires platform webkits/GTK on Linux runners; slow and brittle. `go build ./...` compiles all packages including embedded assets? — Note: `//go:embed all:frontend/dist` requires `frontend/dist` to exist. Fix: commit a placeholder or add a step `mkdir -p frontend/dist && touch frontend/dist/.keep` **before** `go build`. This is essential; document it loudly. |
| `-race` on tests | The app has goroutines (streaming callbacks, headless inflight maps, updater handoff); race failures are exactly the class of bug that ships silently today. |
| `CGO_ENABLED=0` | All DB drivers except none use pure-Go clients (`modernc.org/sqlite`, pgx, go-sql-driver, snowflake, bigquery). Verify Redshift/SQL Server drivers compile CGO-free in the first run; if one requires CGO, drop the env var for that job rather than fighting it. |
| Coverage uploaded but not gated | See F-4: set informational targets first, gate later once stable. Optionally add `codecov/codecov-action@v4` or just print `go tool cover -func` summary. |
| Frontend job runs `npm run build` only | `svelte-check`/lint arrive with F-17; add them to this job then. |

### Optional second stage (after initial stability): release dry-run

A manually-triggered (`workflow_dispatch`) job running
`wails build -platform darwin/universal` on macOS runner to catch
platform-specific compile breaks early. Defer until needed — cost per
minute on macOS runners is 10×.

## Implementation Plan

1. Create `ci.yml` as above, including the `frontend/dist` placeholder step.
2. First-run triage: expect possible failures from `-race` (latent data
   races in streaming paths are plausible). Treat each as a real bug — file
   under issues/, fix or temporarily scope the race flag off the offending
   package with a TODO.
3. Enable branch protection on `main`: require the two checks to pass.
4. Mirror-check: confirm `.githooks/pre-commit` stays consistent (same three
   gates) so local feedback matches CI.
5. Update `AGENT_READ_FIRST.md` §5 with a line pointing to the workflow.

## Risk Assessment

- **Risk:** CI red blocks the solo developer's flow. Mitigation: start with
  the cheap green set (fmt/vet/build/test/build-frontend); add stricter
  gates incrementally.
- **Risk:** embed-placeholder hack masks missing dist in release builds.
  Mitigation: release pipeline (`scripts/build-*`, RELEASE_DEPLOYMENT.md)
  always runs `wails build`, which builds the frontend itself; CI is not the
  release path. Note this explicitly in the workflow comments.
- Charter §4.3 case 3: purely additive process change; proceed.
