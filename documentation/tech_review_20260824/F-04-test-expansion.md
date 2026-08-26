# F-4 · Test coverage expansion — lock down the untested core

*(Referenced by `.githooks/pre-commit`, which runs the suite this document expands.)*

**Severity:** High · **Effort:** Medium · **Risk categories:** regression, data safety, answer accuracy

## Problem Statement

Coverage measured 2026-08-24 (`go test ./... -cover`):

| Package | Coverage |
|---|---|
| root (`app.go`, `main.go`, `headless*.go`) | 0% — no test files |
| `pkg/engine` | 35.2% |
| `pkg/models` | 0% — no test files |
| `pkg/services` | 2.3% |

The most dangerous code paths in the project are effectively untested:

- `executeSQLWithMode` — the **single choke point for the read-only
  invariant** (charter §0/§1.4). Its validation-before-dispatch ordering,
  native-querier branching, and row-scanning have zero automated coverage.
- `applyDefaultLimit` — SQL rewriting logic (see F-2; a bug shipped here).
- `engine_adapters.go` — the bridge between the isolated engine and SQLite
  CRUD; conversion mistakes here corrupt transcripts/results silently.
- Migration helpers in `pkg/models/database.go` — data-loss rules from
  charter §3.2 are enforced only by convention.
- `loop.go` provider-parsing fallbacks — documented as "tuned across
  providers," i.e., high-regret-to-break, but only partially exercised.

The pre-commit hook runs whatever exists; the point of F-3 (CI) is to make
that permanent. This finding is about making what exists *worth running*.

## Solution Design — prioritized test additions

Ordered by risk reduction per hour invested. All are standard-library
table-driven Go tests; no new dependencies.

### Tier 1 — Safety-critical (do first)

**T1. Validation ordering test** (`pkg/services/sql_execution_test.go`):
register a stub `DBDriver` whose `BuildDSN` panics. Feed a write statement
(`UPDATE t SET x=1`) through `ExecuteSQLWithMode`. Assert the returned error
is the *validation* error, proving `ValidateReadOnlySQL` executes before any
driver contact. This encodes the §1.4 guarantee "cannot be bypassed."

**T2. `ValidateReadOnlySQL` fuzz test** (`pkg/engine/safety_test.go`):
`testing.F` seed corpus of valid SELECTs; mutate randomly (insert `;DROP`,
comment tricks `/**/UNION/**/`, string-literal escapes, `SELECT…INTO`,
unicode homoglyph keywords). Property: never panics, never returns nil for
seeds containing write statements outside literals. Run `-fuzz` locally in
short bursts; corpus seeds run in normal `go test`.

**T3. `applyDefaultLimit` golden tests** — see F-2 test table. These are the
regression lock for the F-2 fix.

**T4. Native-querier bypass test**: stub `NativeQuerier` recording whether
it was invoked; assert `executeSQLWithMode` dispatches to it for a fake
native type and that validation still ran first (T1 variant).

### Tier 2 — Adapter & model integrity

**T5. Adapter round-trips** (`pkg/services/engine_adapters_test.go` exists —
extend): for each of the 8 engine interfaces, exercise the adapter against a
temp-file SQLite DB created with `models.ConnectDatabaseAt(t.TempDir()+"/t.db")`:
save a conversation/message/provider/data-source via models CRUD, read back
through the engine-typed adapter, assert field-by-field equality including
JSON config parsing. Conversion shims are where silent truncations live.

**T6. Migration tests** (`pkg/models/database_migration_test.go`):
create a DB at the previous schema version (hand-write old DDL in the test),
run `runMigration`s, assert (a) all expected columns exist, (b) pre-existing
row values survived verbatim, (c) `schema_migrations` recorded each step,
(d) re-running is idempotent. This turns charter §3.2's "only ADD COLUMN"
from convention into executable policy.

**T7. Credential non-exposure test**: marshal `models.LLMProvider` and
`models.DataSource` to JSON; assert `APIKey`/`Password` absent. Two lines
today; priceless after future refactors touch the structs.

### Tier 3 — Loop path space

**T8. Provider-quirk cases** in `pkg/engine/loop_test.go` using existing
mock `LLMClient`:
1. malformed tool-call JSON → forced extra round (per §1.3 ambiguity rule);
2. bare-text response after successful final query → treated as
   `respond_to_user`;
3. second `query_database(is_exploration:false)` after finality → rejected;
4. `render_chart` retry path (`roundKindChart`);
5. `TotalRoundCap` exhaustion → graceful degradation output, not hang.
Each is currently only verified manually against live providers — the exact
fragility the charter warns about ("a change that breaks parsing for one
provider will silently fail for all users of that provider").

**T9. Headless integration test** (`headless_test.go`, root package): spin
`httptest.NewServer` on a `mux` built identically to `runHeadless` (refactor
route registration into a `routes(mux)` function to make it testable), drive
health → create conversation → send message with a stubbed LLM client
(requires a seam: see below), assert message persistence + SSE shape.

*Seam needed:* `services.NewLLMClient(provider)` is constructed deep inside
the orchestrator. Introduce a package-level factory variable
`var llmClientFactory = services.NewLLMClient` overridable in tests. Purely
additive; no production behavior change.

## Implementation Plan

1. Tier 1 (T1–T4): ~half day; land alongside F-2.
2. Tier 2 (T5–T7): ~one day.
3. Tier 3 (T8): ~half day; T9 requires the factory seam (~30 min) then ~2h.
4. Set soft targets in CI (informational, not gating initially):
   `go test -cover` ≥ 40% engine, ≥ 25% services. Gate only after stable.

## Risk Assessment

- Tests themselves carry risk of encoding wrong behavior. Mitigation: each
  test cites the charter section or issue doc it encodes; golden files
  reviewed in PR.
- No production code changes except the T9 factory seam and the `routes()`
  refactor — both additive, reviewed under §4.3 case 3.

## Charter Alignment

Directly serves §5 Testing Checklist and §1.6's rationale for silos
("independently testable with mocks"). Highest-leverage investment in the
project's long-term success probability.
