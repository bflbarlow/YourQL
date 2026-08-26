# Technical Review — YourQL

**Date:** 2026-08-24
**Scope:** Full-project review conducted against the project charter
(`documentation/AGENT_READ_FIRST.md`), with every finding verified against
the live source tree as of commit `5be20a1` ("v0.4.5 enhancements").
**Companion documents:** this review supersedes nothing; it extends
`documentation/operations/TECH_REVIEW.md` (2026-07-26). The pre-commit hook
(`.githooks/pre-commit`) already references findings **F-3** and **F-4**
below by ID — those IDs are now canonical.

> Charter precedence: per `AGENT_READ_FIRST.md` §"Document Precedence", all
> recommendations below were checked against the charter's priority order:
> answer correctness → safety → reliability → appeal/comfort → polish.
> Nothing below recommends weakening the Data Source Read-Only Invariant,
> logging secrets, or destructive migrations.

---

## 0. Executive Summary

YourQL is in materially good shape for its stage: the read-only invariant is
enforced at a genuine single choke point, the agentic loop is properly
isolated behind interfaces with mock tests, the updater verifies checksums
before staging, and API keys are excluded from JSON serialization
(`json:"-"` on both `DataSource.Password` and `LLMProvider.APIKey`). Build
and `go vet` are clean.

The highest-probability-of-success improvements fall into five buckets:

1. **Correctness bugs that produce wrong or failed answers on specific
   drivers** (SQL Server LIMIT injection, context-less query execution) —
   these directly violate priority #1 and #3 of the charter.
2. **Test and CI coverage gaps** — services at 2.3% coverage, models and
   `app.go` at 0%, zero frontend tests, zero CI. The safety suite exists but
   nothing enforces it outside a local hook.
3. **Defense-in-depth for the read-only invariant** — the validator is a
   strong regex/state-machine layer, but it is the *only* layer. Database
   session-level read-only modes cost little and close the gap entirely.
4. **Structural debt that slows every future change** — `app.go` (1,271
   lines, 86 bound methods), `SettingsView.svelte` (3,795 lines), duplicated
   types "kept in sync manually", mixed logging regimes.
5. **Trust-domain hardening** — headless HTTP server has no authentication;
   credentials are plaintext at rest; `os.Exit(0)` paths bypass graceful
   shutdown.

Each finding has an ID (F-nn), severity, effort estimate, and concrete
remediation.

---

## 1. Correctness & Reliability Bugs (Priority 1)

### F-1 · SQL executed without context — cancellation cannot stop a running query
**Severity:** High · **Effort:** Small
**Files:** `pkg/services/sql_execution.go` (~line 229), all
`db_introspection` / driver files using `db.Query(...)` instead of
`QueryContext`.

`executeSQLWithMode` calls `rows, err := db.Query(sqlQuery)`. The pipeline
has a `context.WithTimeout(ctx, …)` (`discussion_engine.go`, 180s default)
and the UI/headless both expose cancel endpoints — but the context never
reaches the database call. A long-running query against a slow warehouse
(Snowflake, BigQuery via native path aside) will:

- keep burning the user's warehouse credits after the user cancels,
- hold the pipeline open until the driver returns,
- never time out even when the pipeline deadline has passed.

**Remediation:** thread `ctx` into `executeSQLWithMode` /
`ExecuteSQLWithMode` / `NativeQuerier.QueryRowsNative` and use
`db.QueryContext(ctx, ...)`. Do the same for schema-introspection queries in
the drivers. This also makes the 180s pipeline timeout actually enforceable
at the DB layer rather than only between LLM calls.

### F-2 · Auto-LIMIT appends invalid SQL for SQL Server
**Severity:** High (wrong/failed answers for one supported driver) · **Effort:** Medium
**File:** `pkg/services/sql_execution.go` `applyDefaultLimit`

`applyDefaultLimit` appends literal `LIMIT n` (and wraps UNION queries as
`SELECT * FROM (...) subq LIMIT n`). T-SQL supports neither syntax — SQL
Server queries longer than `queryLengthThreshold` (200 chars) or containing
UNION will fail with a syntax error, i.e., a guaranteed failed answer for
SQL Server users whenever the LLM writes an unbounded long query.

Also note the guard `regexp.MustCompile(`(?i)\bLIMIT\b`)` matches `LIMIT`
*anywhere*, including inside string literals and identifiers (e.g., a column
named `limit_status`) — such queries skip limiting entirely. Use
`engine.MaskStringLiterals` before the scan.

**Remediation:** make limit application driver-aware. For SQL Server use
`SELECT TOP (n) …` wrapping (`SELECT TOP (n) * FROM (<query>) subq`) or
inject at parse-free prefix position; keep `LIMIT` for MySQL/Postgres/
SQLite/MariaDB/Redshift; BigQuery and Sheets already go through native
queriers (verify their behavior separately).

### F-3 · Enforce formatting + full build/test gate in CI, not just local hooks *(referenced by `.githooks/pre-commit`)*
**Severity:** High (process) · **Effort:** Small

There is no `.github/workflows/` — the only enforcement of `gofmt`,
`go build ./...`, and `go test ./pkg/engine/... ./pkg/services/...` is the
local pre-commit hook, which runs only if the developer ran
`git config core.hooksPath .githooks` (it is not set in this clone unless
configured manually). Any clone, CI bot, or future agent that skips the hook
can commit unformatted code or break the safety suite silently.

**Remediation:** add a minimal GitHub Actions workflow:

```yaml
name: ci
on: [push, pull_request]
jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: test -z "$(gofmt -l .)"
      - run: go vet ./...
      - run: go build ./...
      - run: go test ./...
  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
      - run: npm ci && npm run build   # in frontend/
```

Keep the hook as a fast local mirror; CI is the authoritative gate.

### F-4 · Safety suite must be non-skippable *(referenced by `.githooks/pre-commit`)*
**Severity:** High · **Effort:** Medium
Related to F-3 but distinct: `pkg/services` sits at **2.3%** statement
coverage and `pkg/models`, `main package`, `headless*.go`, and `app.go` have
**no test files at all**. The most dangerous code in the project —
`executeSQLWithMode` dispatch, DSN building, adapter conversion, migration
helpers — is essentially untested. The engine's 35.2% is better but still
covers only part of `loop.go`'s path space (700 lines, multi-provider
parsing fallbacks).

**Remediation (ordered by risk reduction per hour):**
1. Table-driven tests for `executeSQLWithMode`'s validation/dispatch order
   (mock `NativeQuerier`; assert `ValidateReadOnlySQL` runs *before* any
   driver contact).
2. Tests for `applyDefaultLimit` across drivers (will surface F-2).
3. Adapter round-trip tests: `engine_adapters.go` ↔ SQLite fixtures —
   `engine_adapters_test.go` exists; expand it to cover error mapping.
4. Migration tests: create a DB at old schema, run `runMigration`s, assert
   additive-only and data preserved.
5. Engine loop: add cases per provider quirk documented in §1.3 of the
   charter (malformed tool-call JSON, bare text response, one-shot finality
   rejection, chart retry) — each is currently only manually verified.

### F-5 · `os.Exit(0)` bypasses graceful shutdown
**Severity:** Medium-High · **Effort:** Small
**Files:** `pkg/services/db_switcher.go:124`, `updater_{darwin,windows,linux}.go`

`models.DB.Close()` is wired through Wails `OnShutdown`. Direct `os.Exit(0)`
skips it. For the updater paths this is by design (documented, §3.8), but
for `db_switcher.go` — switching the app's own SQLite database — exiting
without closing can leave WAL files and lose buffered writes to
`~/.yourql/yourql.db`, exactly the kind of app-data integrity regression the
charter forbids (§4.0 rule 3).

**Remediation:** in `db_switcher.go`, explicitly `models.DB.Close()` (and
flush any open exports) before `os.Exit(0)`, or refactor to return an error
to `app.go` so the Wails lifecycle performs the restart. Add a comment at
each `os.Exit` site stating which shutdown work was performed manually.

### F-6 · Updater retains a hardcoded legacy version check
**Severity:** Low-Medium (correctness of intent, drift risk) · **Effort:** Trivial
**File:** `pkg/services/updater.go` (`CheckForUpdate`)

```go
if appVersion == "dev" || appVersion == "0.4.0" {
```

The charter (§3.8) says `appVersion` must default to `"dev"` (which `main.go`
does) and `"dev"` alone gates dev builds. The `"0.4.0"` literal is a stale
artifact from before the ldflags convention and contradicts the adjacent
comment. Worse, any *real* user who somehow ends up reporting `0.4.0` would
silently never receive updates.

**Remediation:** delete the `"0.4.0"` clause and fix the comment above the
function (it still says *"appVersion is '0.4.0'"*). Optionally add a unit
test asserting `CheckForUpdate("dev")` makes no HTTP call.

### F-7 · `UpdateInfo` duplicated between `app.go` and `updater.go`
**Severity:** Low (drift hazard) · **Effort:** Small
The file header itself admits it: *"repeated from app.go … kept in sync
manually."* Manual sync is how the charter's own §3.3 driver-count warning
describes drift. Define `UpdateInfo` once in `pkg/services` and have
`app.go` alias/re-export it, mirroring the established pattern used for
engine types (`type QueryResult = engine.QueryResult`).

---

## 2. Safety Hardening (Priority 2 — preserves the Read-Only Invariant)

### F-8 · Add session-level read-only as defense-in-depth
**Severity:** High (invariant robustness) · **Effort:** Medium
**Files:** `pkg/services/sql_execution.go`, per-driver files

`engine.ValidateReadOnlySQL` is a well-built lexical validator (single-
statement check, comment stripping, string-literal masking, `SELECT … INTO`
detection). But it is pattern-based, and the charter itself states the app
"must not rely solely on the *user's* database role/permissions as the only
safeguard." The inverse is equally true: don't rely solely on the pattern
matcher. Cheap, per-driver session hardening after `sql.Open`:

| Driver | Mechanism |
|---|---|
| PostgreSQL / Redshift | `SET default_transaction_read_only = on` or open tx with `tx.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})` |
| MySQL / MariaDB | `SET SESSION TRANSACTION READ ONLY` |
| SQL Server | `SET TRANSACTION ISOLATION LEVEL READ COMMITTED;` + login with `db_datareader`-only guidance; optionally `ALTER LOGIN … SET` isn't available — wrap in an explicit read-only transaction |
| Snowflake | run in a transaction with `READ ONLY` semantics where supported; otherwise rely on validator + docs |
| SQLite (file data sources) | open with `?mode=ro` in the DSN |

This converts any future validator bypass from "potential write" into
"guaranteed driver error." It is purely additive, requires no UI change, and
is the single strongest improvement available to the project's core promise.

Also add a `safety_test.go` fuzz case: feed randomized SQL fragments
(mutations of valid SELECTs) to `ValidateReadOnlySQL` and assert no panic.

### F-9 · Verify OAuth scopes are actually read-only at runtime
**Severity:** Medium · **Effort:** Small
**Files:** `pkg/services/google_auth.go`, `db_google_sheets.go`, `db_bigquery.go`

The charter requires read-only scopes only. Add a startup/self-test
assertion listing the requested scopes in a table constant, e.g.
`var requiredReadOnlyScopes = []string{…sheets.readonly, …bigquery.readonly}`
and fail loudly (or log an error) if anything outside that set is ever
requested. This prevents a future enhancement from quietly adding a writable
scope.

### F-10 · Headless server has no authentication
**Severity:** Medium-High (when headless mode is used) · **Effort:** Small
**File:** `headless.go`, `headless_handlers.go`

Good: binds `127.0.0.1` only. Gap: any local process (another user's
browser extension doing localhost probes, any malware running as the same
user — though that's game over anyway, and CSRF-style browser requests) can
drive conversations, read conversation history (which contains query results
— i.e., business data), and trigger LLM spend. DNS-rebinding attacks against
unauthenticated localhost APIs are a well-known class.

**Remediation:** generate a random token at startup, require
`Authorization: Bearer <token>` on all routes, print it once to stdout (and
optionally write it mode-0600 to the db directory for scripts). ~40 lines,
no UX impact since the token can be auto-injected by any official client.

### F-11 · Credentials at rest are plaintext
**Severity:** Medium (accepted risk today, documented in §3.5) · **Effort:** Large
All DB passwords and LLM API keys live in `~/.yourql/yourql.db` plaintext.
The charter accepts this, but success probability improves if the common
case migrates to OS keychains (`go-keyring`: macOS Keychain, Windows
Credential Manager, libsecret). Suggested approach: keep the column, store
`keychain:<service>` indirection markers, fall back to plaintext for systems
without keychain access. Strictly additive migration (charter-compliant).

### F-12 · Driver logs emit usernames and hosts
**Severity:** Low · **Effort:** Small
**Files:** `db_{redshift,bigquery,mariadb,snowflake,mysql,postgres,sqlserver}.go`
Lines like `log.Printf("[mysql] Built DSN for database %s, user %s, host %s:%d", …)`.
No passwords are logged (verified — good; mysql redacts the DSN). But
usernames/hosts are mildly sensitive and, more importantly, this uses stdlib
`log` while `main.go`/`headless.go` use structured `log/slog`.
**Remediation:** standardize on `slog.Debug` for these lines so they vanish
at default verbosity and appear in structured logs during debugging. Audit
with a grep test in CI (see F-14).

---

## 3. Answer-Quality Improvements (Priority 1 by charter, but lower urgency than F-1/F-2)

### F-13 · Bound what gets sent back to the LLM from large result sets
**Severity:** Medium · **Effort:** Medium
**Files:** `pkg/engine/loop.go`, `pkg/services/agentic_loop.go`

Row limits cap rows at ~1000, but `formatResults` renders a markdown table
of *every* row into the tool response. A wide 1000-row result can consume
most of a context window, degrade subsequent rounds, and raise latency/cost
— a silent reliability tax on the fundamental goal. Consider truncating the
tool-response rendering (e.g., first 50 rows + "… N more rows available in
results") while keeping the full result for UI rendering. Make the
threshold part of `AgentLoopConfig` so power users can tune it (consistent
with the existing config philosophy).

### F-14 · Guard against prompt/log leakage of credentials with a CI test
**Severity:** Low · **Effort:** Small
Add a Go test that walks provider/driver source with a regex list
(`password`, `apikey`, `authorization: bearer`, DSN interpolation patterns)
and asserts matches occur only in known-safe contexts, or better: unit-test
a central `RedactDSN(s string) string` helper and require all drivers to use
it. Today redaction exists only for MySQL.

### F-15 · Keep README model lists current
**Severity:** Low (appeal/trust) · **Effort:** Trivial
README advertises "Claude 3 Opus/Sonnet/Haiku" — superseded naming. Since
the custom-endpoint path covers new models automatically, just soften the
table to "Claude family and successors via Anthropic API" or date-stamp it.
Stale marketing copy erodes trust for a product whose pitch is accuracy.

---

## 4. Architecture & Maintainability

### F-16 · `app.go` is a god object
**Severity:** Medium · **Effort:** Large (incremental)
1,271 lines and **86 exported methods**, every one auto-bound to JS. Risks:
accidental widening of the frontend API surface, hard-to-audit security
review ("what can the webview call?"), merge conflicts.
**Remediation (incremental, low-risk):** group methods into embedded
sub-structs per domain (`conversationAPI`, `dataSourceAPI`, `settingsAPI`,
`updateAPI`) — Wails binds embedded methods fine, file layout mirrors the
silos already documented in §1.6. Do it opportunistically, one domain per
PR, with no signature changes.

### F-17 · Frontend monolith components
**Severity:** Medium · **Effort:** Large (incremental)
`SettingsView.svelte` = 3,795 lines; `App.svelte` = 2,696;
`ConversationView.svelte` = 1,310. Svelte 5 snippets/runes make extraction
cheap. Extract leaf sections (provider card, data-source form, skill editor)
into `frontend/src/lib/components/`. Also adopt `svelte-check` +
`eslint-plugin-svelte` + Prettier, and add Vitest for pure logic (limit
calculation mirrors, markdown normalization if any moves client-side).
No frontend test tooling exists at all right now (`package.json` confirms).

### F-18 · Inline styles emitted from Go HTML rendering
**Severity:** Low-Medium · **Effort:** Medium
**File:** `pkg/services/sql_execution.go` `ToHTML` / `formatResultsHTML`
Presentation (colors, paddings referencing CSS vars) is hardcoded into Go
strings. This splits the design system across languages, makes dark-mode
audits (§3.6) error-prone, and bloats every stored message blob.
**Remediation:** replace inline styles with stable class names
(`.results-details`, `.summary-chip`, …) defined in `variables.css`-adjacent
stylesheet. Backward-compatible because stored HTML degrades gracefully to
browser defaults; new messages get classes immediately.

### F-19 · Dead code / minor smells
- `checkSingleStatement`: `hasContent` is computed then discarded (`_ =
  hasContent`). Either delete or use it (e.g., reject empty statements).
- `applyDefaultLimit`: `if threshold == 0 { threshold = … }` followed by
  `if threshold == 0 { return }` — the second branch is unreachable given
  `queryLengthThreshold = 200`. Remove or document why.
- `documentation/AGENT_READ_FIRST.md` §3.8 quotes §"Security Considerations"
  of `VERSION_UPGRADE.md`; verify anchors still resolve after recent doc
  deletions (many files show as deleted-but-uncommitted in `git status`).

### F-20 · Commit hygiene
**Severity:** Low · **Effort:** Trivial
Working tree has dozens of uncommitted deletions/moves (docs reorganization)
plus modified `app.go`, `.gitignore`, `README.md`. Land them as a dedicated
housekeeping commit so the next agent's `git status` is clean and blame
stays useful. Ensure `.gitignore` already excludes `YourQL` binary and
`frontend/dist` (it does — keep it that way; previous review removed a
tracked binary).

---

## 5. Documentation Drift (keeps the charter trustworthy)

| Item | Where | Issue |
|---|---|---|
| Updater comment says version gate is `"0.4.0"` | `updater.go` | Contradicts charter §3.8 (`"dev"` only) — see F-6 |
| `UpdateInfo` "kept in sync manually" | `updater.go` header | Charter §1.6 explicitly forbids duplicate structurally-identical types |
| Pre-commit hook cites `TECH_REVIEW_20260824.md` F-3/F-4 | `.githooks/pre-commit` | Now resolved — this document defines them |
| README Claude 3 model names | `README.md` | See F-15 |
| Driver count (11) | charter §3.3 | Charter self-warns about drift — recount in next touchpoint |

Per the charter's own closing instruction ("A stale charter is worse than no
charter"), fix F-6/F-7 and the corresponding doc lines in the same PR.

---

## 6. Prioritized Roadmap

| Order | Finding | Why first |
|---|---|---|
| 1 | **F-3** CI pipeline | Every other finding becomes enforceable |
| 2 | **F-1** QueryContext everywhere | Cancels/timeouts actually work; small diff |
| 3 | **F-2** SQL Server LIMIT bug | Fixes guaranteed broken answers for a supported driver |
| 4 | **F-8** Session read-only defense-in-depth | Strongest possible upgrade to the core trust promise |
| 5 | **F-6, F-7, F-19** Quick cleanups | Trivial effort, removes drift hazards |
| 6 | **F-4** Test expansion (services first) | Locks in 1–5; enables safe refactors |
| 7 | **F-10** Headless bearer token | Closes real local attack surface |
| 8 | **F-13** LLM-facing result truncation | Latency/cost/context reliability win |
| 9 | **F-5** Graceful exit on DB switch | App-data integrity edge case |
| 10 | **F-11, F-16, F-17, F-18** | Larger structural investments, schedule incrementally |
| 11 | **F-9, F-12, F-14, F-15, F-20** | Polish batch |

Items 1–5 are all low-to-medium effort and can land within days; they
collectively eliminate every known path to a wrong-or-failed answer and make
the read-only guarantee enforceable by the database engine itself — the two
things the charter identifies as existential.

---

## 7. What Is Already Excellent (do not regress)

- **Single read-only choke point** (`executeSQLWithMode` →
  `ValidateReadOnlySQL`) with a genuinely careful lexer (comment stripping,
  string masking, stacked-statement rejection, `SELECT INTO` detection).
- **Engine isolation**: `pkg/engine` imports neither `database/sql`, Wails,
  nor `pkg/models`; adapters are true shims; mock-based loop tests exist.
- **Updater discipline**: SHA256 verification before staging, `"dev"` builds
  skip update checks, per-OS replace scripts preserve signatures and never
  elevate.
- **Secrets out of JSON**: `Password` and `APIKey` both `json:"-"`.
- **Charter quality**: `AGENT_READ_FIRST.md` is unusually clear about goal
  priority, absolute rules, and where past reviews went wrong — this review
  could verify its claims quickly because of it. Keep updating it in the
  same commit as behavior changes.

---

*Review method: source inspection of all `pkg/**` files, `app.go`,
`main.go`, `headless*.go`, frontend manifests, git history/status, plus
execution of `go build ./...`, `go vet ./...`, and `go test ./... -cover`
(clean build/vet; coverage figures quoted verbatim).*
