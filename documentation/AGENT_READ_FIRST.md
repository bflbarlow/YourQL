# YourQL — Agent Read First

> **Every agent that touches this codebase must read this document first.**
> It defines the fundamental goal, the risk framework, and the best practices
> that keep the project aligned with that goal.

---

## 0. The Fundamental Goal

**YourQL exists to serve the answer to a user's question from their database.**

Every line of code, every feature, every design decision must be evaluated
against this single question: *Does this help the user get the right answer
from their data, faster and more reliably?*

### What This Means in Practice

- **Answer accuracy is non-negotiable.** A wrong answer is worse than no answer.
  The LLM prompt, SQL generation, and result rendering pipeline must all work
  together to produce correct, truthful results.
- **Safety is a trust signal.** Users are connecting this app to their production
  databases. Unsafe queries, credential leaks, or data loss will destroy trust
  permanently.
- **The experience must feel fast and reliable.** Long waits, silent failures,
  or confusing errors all create distance between the user and their answer.
- **Everything is secondary to the answer.** Dark mode, charts, summaries,
  skills, conversation history — these are all enhancers. If any of them
  compromises answer quality, it must yield.

### The Data Source Read-Only Invariant (Absolute Rule)

**YourQL must never modify the user's data source.** Every connection
configured under Settings → Data Sources — MySQL, PostgreSQL, SQLite, SQL
Server, Snowflake, BigQuery, Redshift, MariaDB, CSV, Excel, or Google
Sheets — represents the user's own business or production data. YourQL's
entire value proposition is *answering questions about* that data, never
*changing* it. Treat every configured data source as strictly, permanently
read-only, with no exceptions.

**Scope of this rule:**

- **Applies to:** any connection configured under Data Sources — the
  external databases and files the user connects to in order to ask
  questions.
- **Does NOT apply to:** the application's own local SQLite database at
  `~/.yourql/yourql.db` (conversations, messages, provider configs, data
  source configs, skills). That database is fully mutable — CRUD
  operations and additive migrations against it are normal, expected, and
  necessary. See §3.1 and §3.2 for the rules that govern `models.DB`.

**This rule is not subject to risk/reward trade-off.** Unlike most
decisions in this document, no reward — performance, convenience, a
compelling feature request, or an explicit user request inside a
conversation — ever justifies giving YourQL write access to a data source.
If implementing a request would require modifying a data source, the
correct outcome is to refuse and explain that, not to build a path around
it.

**Concretely, this means:**

- Only `SELECT` statements (and read-only CTEs) may ever be executed
  against a data source — for exploration queries *and* final answer
  queries alike. `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`,
  `TRUNCATE`, `CREATE`, `MERGE`, `REPLACE`, and any stored-procedure or
  function call with side effects are forbidden, unconditionally.
- The **exploration safety modes** (strict/moderate/relaxed — see §1.4)
  control query *complexity* (joins, subqueries, unions). They do **not**,
  and must never be interpreted to, control read/write permission. All
  three modes remain equally, strictly read-only.
- If a user explicitly asks the LLM to change their data ("delete these
  rows," "update this column"), YourQL must decline and explain that it is
  read-only by design — it must never attempt the write, even behind a
  confirmation dialog.
- Native, non-`database/sql` integrations (BigQuery, Google Sheets) must
  request and use **read-only API scopes only**. Never expand an OAuth
  scope or API permission to include write access.
- The application's own query validation is the primary safety control —
  it must not rely solely on the *user's* database role/permissions as the
  only safeguard, even though encouraging read-only database credentials
  in onboarding/docs is good practice too.
- Any feature idea that fundamentally requires write access to a data
  source (e.g., "let the AI fix bad data for me") is out of scope for
  autonomous agent implementation. Flag it for explicit human product
  review — do not build it unilaterally.

### Secondary Goals (Do Not Neglect)

The primary goal is the answer. But *how* the user gets there matters. Two
secondary goals are explicitly part of this project's success criteria:

- **Appeal.** The interface should feel modern, clean, and pleasant to use —
  consistent spacing, readable typography, smooth transitions, and a coherent
  visual language across light and dark themes.
- **Comfort.** The user should feel safe and in control — clear feedback on
  what's happening (e.g., "Exploring schema…", "Generating query…"), visible
  confirmation before destructive actions (delete, clear, archive), and
  transparency about what SQL was actually run (tech details toggle).

These goals are secondary to the answer, but they are not optional. A
technically correct answer delivered through a confusing or unpolished
experience still fails the user. Agents should treat UX polish as a
first-class deliverable, not an afterthought — as long as it never competes
with or delays the fundamental goal.

### Goal Priority Order

When goals conflict, resolve in this order:

1. **Correctness of the answer** (never sacrifice for speed or polish)
2. **Safety** (data integrity, credential protection, read-only guarantees)
3. **Reliability of delivery** (no silent failures, no hangs, no crashes)
4. **Appeal and comfort** (secondary goals above)
5. **Nice-to-have polish** (animations, minor visual refinements)

A change that improves #4 or #5 at the expense of #1–#3 must not ship as-is —
redesign it so the higher-priority goal is preserved.

### Graceful Degradation Principle

Secondary features — visualization, summarization, skills, chat history —
must fail independently without ever blocking or hiding the primary result.
If a chart fails to render or a summary fails to generate, the underlying
data table must still reach the user. **Never let an enhancement become a
single point of failure for the answer itself.**

### Risk/Reward Requirement

**Any change that puts the fundamental goal at risk requires a thorough
technical risk/reward analysis.** Before implementing:

1. **Identify what could go wrong.** Be specific — name the failure mode, not
   just "it might break."
2. **Quantify the reward.** What specific user problem does this solve? How many
   users does it affect? How much does it improve their experience?
3. **Compare risk vs. reward.** If the risk is high (answer accuracy, data
   safety, data integrity) and the reward is incremental, **do not proceed**
   without explicit justification.
4. **Document your analysis.** Add it to this file or the PR. Future agents
   (and future you) need to understand why this trade-off was made.

**Think of this like a skills file for the project.** Just as skills inject
domain knowledge into the LLM prompt to improve answer quality, this document
injects project-wide knowledge into every agent that touches the codebase.

### Document Precedence

This file is the project's charter — it defines *why* decisions should be
made. Other files in `documentation/` (e.g., `TECH_REVIEW.md`,
`*_ENHANCEMENT.md`) are feature-specific implementation notes and audits —
they describe *what* was built or reviewed at a point in time. If a
recommendation elsewhere in `documentation/` conflicts with the principles in
this file, **this file wins** for questions of goal alignment, risk
tolerance, and priority. Other documents may still be authoritative for
narrow, verified technical facts (e.g., exact line numbers, file sizes) as of
their date — but always re-verify those facts against the live source before
relying on them, since code moves faster than docs.

---

## 1. Architecture Overview

### 1.1 High-Level Flow

```
User Question → Discussion Engine → LLM Prompt → LLM API → LLM Response
     → SQL Generation → SQL Execution → Result Rendering → User Answer
```

### 1.2 Key Components

| Component | File(s) | Responsibility |
|---|---|---|
| **Wails Bindings** | `app.go` | Go ↔ Svelte bridge. All public methods on `App` are Wails-bound. |
| **Entry Point** | `main.go` | Wails app initialization, asset embedding, lifecycle. |
| **Discussion Engine** | `pkg/services/discussion_engine.go` | Core query pipeline: prompt building, LLM interaction, SQL generation, execution, result processing. |
| **SQL Execution** | `pkg/services/sql_execution.go` | Query execution, safety checks, error handling, retry logic. |
| **Driver Interface** | `pkg/services/db_driver.go` | `DBDriver` and `NativeQuerier` interfaces. |
| **Driver Registry** | `pkg/services/db_registry.go` | Driver registration and lookup. |
| **LLM Interface** | `pkg/services/llm_client.go` | `LLMClient` interface. |
| **LLM Providers** | `llm_openai.go`, `llm_anthropic.go`, `llm_ollama.go`, `llm_local.go` | Provider-specific implementations. |
| **Models** | `pkg/models/*.go` | Data structures, DB schema, migrations. |
| **Frontend** | `frontend/src/` | Svelte 5 components. |

### 1.3 The LLM Response Protocol

The LLM must return a JSON action. The extraction logic in
`discussion_engine.go` handles multiple output formats (markdown code blocks,
unwrapped JSON, thinking/response markers). **Be conservative when modifying
the extraction logic.** It has been tuned for many LLM output formats. A change
that breaks extraction for one provider will silently fail for all users of
that provider.

**When the LLM's intent is ambiguous** (malformed JSON, contradictory action
fields, missing required fields), prefer surfacing a `clarification` request
over guessing. A confidently wrong silent guess is a worse outcome for the
fundamental goal than asking the user one more question.

### 1.4 SQL Execution Safety

`sql_execution.go` enforces:

- **Read-only, always** — Both exploration queries and final answer queries
  are restricted to `SELECT` (and read-only CTEs). This is the Data Source
  Read-Only Invariant from §0 — an absolute rule, not a configurable
  preference.
- **Exploration complexity modes** — `ExplorationSafety` (strict/moderate/
  relaxed) controls query *complexity* (joins, subqueries, unions), not
  read/write permission. Relaxing this setting must never open a path to
  write statements.
- **Row limits** — Default 1000, configurable per data source
- **Query length threshold** — Long queries get a LIMIT added automatically
- **Error retry** — Failed queries are sent back to the LLM for correction
- **Native querier support** — BigQuery, Google Sheets bypass `database/sql`

**Never remove or weaken safety checks, and never add a code path — feature
flag, config option, "advanced mode," or otherwise — that allows a write
statement to reach a data source.** The user is running queries against
their own production databases. Safety is the foundation of trust in this
application.

### 1.5 Schema Introspection

`db_introspection.go` gathers table/column metadata for every connection. This
metadata is the foundation of the LLM's ability to generate correct queries.

**If introspection fails, the user cannot get answers.** Ensure all drivers
return accurate schema data. Test with real databases, not just mocks.

---

## 2. Wails-Specific Best Practices

### 2.1 Wails Bindings (`app.go`)

`app.go` is the single bridge between Go and the Svelte frontend. Every
public method on the `App` struct is automatically bound and callable from
JavaScript.

**Best practices for binding methods:**

- Keep methods focused and small. If a method exceeds ~50 lines, consider
  delegating to a service function.
- Return typed structs for complex data (use `json:"..."` tags).
- Never return raw `*sql.DB` or `context.Context` to the frontend.
- Handle errors explicitly — Wails will serialize errors to the frontend.
- Use `runtime.EventsEmit()` for async events (e.g., OAuth flow completion).

### 2.2 Wails Lifecycle

```go
// main.go
wails.Run(&options.App{
    OnStartup:  app.startup,   // Called once, before the app is ready
    OnShutdown: app.shutdown,  // Called once, before the app quits
})
```

- `startup` is where `models.ConnectDatabase()` is called
- `shutdown` closes the database connection
- The embedded frontend assets come from `//go:embed all:frontend/dist`

**Never call `models.ConnectDatabase()` outside of `startup`.** The global
`models.DB` is the only database connection for app data.

### 2.3 Wails Dev vs Build

```bash
wails dev    # Development with hot reload (frontend)
wails build  # Production build
```

During development, the frontend dev server runs on a random port. The Wails
CLI detects it automatically (`"frontend:dev:serverUrl": "auto"` in wails.json).

### 2.4 Frontend Asset Embedding

```go
//go:embed all:frontend/dist
var assets embed.FS
```

This embeds the entire `frontend/dist` directory at compile time. Any frontend
change requires `wails build` to take effect in the binary. During `wails dev`,
the Vite dev server is used instead.

---

## 3. Project-Specific Best Practices

### 3.1 Global State

`models.DB` is a global `*sql.DB` pointing at the **application's own**
local SQLite database (`~/.yourql/yourql.db`). It is initialized in
`startup` and closed in `shutdown`. All application data (conversations,
messages, providers, connections, skills) flows through this single
connection, and it is fully mutable — this is app state, not user data.

**Never try to open a second app database.** If you need to connect to a
user's data source, use the driver's `BuildDSN()` + `sql.Open()` pattern (as
in `ExecuteQuery`), and always `defer db.Close()`. A connection to a user's
data source is a completely different trust domain from `models.DB` — see
the **Data Source Read-Only Invariant** in §0. `models.DB` may be freely
written to; a user's configured data source must never be written to.

### 3.2 Migration Safety

Migrations in `models/database.go` follow strict rules:

1. **Only ADD COLUMN** in auto-migrations. Never DROP, RENAME, or ALTER existing
   columns in a way that can lose data.
2. **Never use `CREATE TABLE … AS SELECT`** — it strips constraints.
3. **Never DROP TABLE and recreate** — data loss is irreversible.
4. **Use `runMigration()`** for any change beyond `ensureColumn()`, so it records
   success in `schema_migrations` and never re-runs.

**If you need to change a column's type or rename a column, add a new column,
migrate data, then (optionally) drop the old one in a future version.**

### 3.3 Driver Registration

New database drivers must be registered in `db_registry.go` via `init()`.
Unregistered drivers will not appear in the connection type selector and will
fail silently.

**Always verify:** `len(driverRegistry) == 9` after registration.

### 3.4 Data Storage

All application data is stored in `~/.yourql/yourql.db`:

| Table | Purpose |
|---|---|
| `conversations` | Discussions (title, settings, status) |
| `conversation_messages` | Messages (user, assistant, system) |
| `llm_providers` | AI provider configs (name, type, model, API key) |
| `data_sources` | DB connection configs (name, type, credentials) |
| `skills` | Reusable Markdown prompt fragments |
| `conversation_skills` | Many-to-many: active skills per discussion |
| `schema_migrations` | Migration tracking |
| `table_descriptions` | Custom table metadata |
| `column_descriptions` | Custom column metadata |

**Never modify the schema outside of the migration system.** Users may have
existing data that depends on the current schema.

### 3.5 Security

- API keys and database passwords are stored in `yourql.db` (plaintext at rest,
  but the file is local-only).
- **Never log API keys or passwords.** Check any new `slog` or `log` calls.
- No telemetry, no analytics, no phoning home.
- **All queries against a data source — exploration and final alike — are
  strictly read-only.** See the Data Source Read-Only Invariant in §0. This
  is not a default that can be toggled off; it is a hard constraint.

### 3.6 Dark Mode

The app supports light and dark themes. The CSS variables in
`frontend/src/variables.css` define the color system. When adding new UI
elements, ensure they work in both themes. Test both.

### 3.7 Conversation Settings

Each discussion has per-conversation settings:

- Which LLM provider to use
- Which data source to query
- `maxMessages` — Total messages in history
- `maxContextMessages` — Messages sent to the LLM (prevents context overflow)
- `summarize` — Generate plain-English summaries
- `vizEnabled` — Generate charts
- `pinned` — Keep at top of list

**Changes to these settings must be reflected in both the database schema and
the prompt construction logic.**

### 3.8 UX & Trust Best Practices

- **Never expose raw stack traces or driver error strings directly to the
  user without context.** Wrap technical errors in plain language, and put
  the raw detail behind the "tech details" toggle for users who want it.
- **Always show progress during multi-step operations.** The `processingPhase`
  event exists so the UI can say "Exploring schema…" instead of leaving the
  user staring at a spinner. Any new long-running step should emit a phase.
- **Confirm before destructive actions.** Delete, clear, and archive
  operations on conversations should never fire without an explicit
  confirmation step in the UI.
- **Prefer transparency over magic.** Showing the actual SQL that was run
  (even when summarized/visualized) builds trust. Don't hide it by default
  in a way that makes the app feel like a black box.
- **Empty and error states need real content**, not blank screens — tell the
  user what happened and what they can do next.

---

## 4. Risk Assessment Framework

### 4.0 Absolute Rules (No Risk/Reward Trade-off Applies)

Some constraints are never weighed against reward — violating them is not
acceptable no matter how compelling the justification:

- **Never execute a write statement (INSERT, UPDATE, DELETE, DROP, ALTER,
  TRUNCATE, CREATE, MERGE, or any side-effecting call) against a configured
  data source.** See the Data Source Read-Only Invariant in §0.
- **Never log API keys or database passwords.**
- **Never DROP, RENAME, or destructively ALTER an existing column in the
  app's own database** (`~/.yourql/yourql.db`) in a way that loses data.
  See §3.2.

If a proposed change would violate one of these, the answer is always "do
not proceed" — skip the risk/reward analysis and redesign the approach
instead.

### 4.1 Risk Categories

| Category | Questions |
|---|---|
| **Answer accuracy** | Could this cause wrong queries or missing data? |
| **Answer delivery** | Could this slow down or break result rendering? |
| **Data safety** | Could this expose credentials, or allow a write/unsafe query against a data source? |
| **Data integrity** | Could this corrupt or lose user data (app data or data source)? |
| **User trust** | Could this make the user doubt the app's reliability or its promise to never alter their data? |
| **Regression** | Could this break existing conversations or settings? |
| **UX/Comfort** | Could this make the interface harder to use, less appealing, or less trustworthy to interact with? |

### 4.2 When to Pause

Pause and document your reasoning if:

- The change touches `discussion_engine.go` or `sql_execution.go`
- The change modifies the system prompt construction
- The change adds or removes schema columns
- The change affects multiple drivers or LLM providers
- The reward is incremental (not a clear fix for a broken answer)

### 4.3 When to Proceed

Proceed (with testing) if:

- The change fixes a clear bug in answer accuracy or delivery
- The change improves safety without reducing capability
- The change is purely additive (new feature, no modification)
- The risk is isolated and testable in isolation

### 4.4 Risk/Reward Analysis Template

When documenting a risk/reward analysis (per §0), capture at minimum:

- **Change:** short description of what's being modified
- **Fundamental goal impact:** how this affects answer accuracy or delivery
- **Risk category(ies):** one or more from the table in §4.1
- **Failure mode(s):** specific, concrete ways this could go wrong
- **Reward:** the specific user problem solved, and who benefits
- **Mitigations:** tests added, safeguards kept, fallback behavior
- **Decision:** proceed / proceed with changes / do not proceed — and why

Append the completed analysis to [`documentation/RISK_ANALYSIS_LOG.md`](RISK_ANALYSIS_LOG.md)
(or link it from the PR) so future agents see the reasoning, not just the result.

---

## 5. Testing Checklist

### 5.1 Before Committing

- [ ] **Fundamental goal:** Does the change improve or preserve answer quality?
- [ ] **SQL execution:** Test with at least one real database type
- [ ] **LLM integration:** Test with at least one LLM provider
- [ ] **Dark mode:** Verify in both light and dark themes
- [ ] **Data safety:** Confirm no API keys/passwords are logged
- [ ] **Read-only guarantee:** Confirm no code path allows INSERT/UPDATE/
      DELETE/DDL statements to execute against a data source (§0)
- [ ] **Migration safety:** Confirm no data loss risk in schema changes
- [ ] **Wails bindings:** Confirm types serialize correctly to JSON
- [ ] **Existing conversations:** Confirm backward compatibility

### 5.2 Multi-Provider Verification

When changing LLM-related code, test with:

1. OpenAI (standard path)
2. Anthropic (different message format)
3. Ollama (local, no API key)
4. Custom endpoint (base URL variation)

### 5.3 Multi-Database Verification

When changing database-related code, test with:

1. SQLite (local file, pure Go driver)
2. PostgreSQL (pgx driver)
3. MySQL (go-sql-driver/mysql)
4. One driver with `NativeQuerier` (BigQuery or Google Sheets)

---

## 6. Agent Development Checklist

### Pre-Implementation

- [ ] Is this change aligned with the fundamental goal (serving the answer)?
- [ ] If risky, is the risk/reward analysis documented?
- [ ] Have I identified all files that need to change?

### During Implementation

- [ ] Does the LLM prompt still include all required schema metadata?
- [ ] Are business rules and active skills still injected correctly?
- [ ] Is SQL execution safety preserved (read-only exploration and final
      queries, row limits — see §0's Data Source Read-Only Invariant)?
- [ ] Are error messages clear and actionable?
- [ ] Does the UI remain responsive during long operations?

### After Implementation

- [ ] Tested with at least one LLM provider and one database type?
- [ ] Dark mode and light mode both verified?
- [ ] Persisted data schema compatible with existing user data?
- [ ] API keys and passwords not logged?
- [ ] Tech details toggle shows the correct query?
- [ ] Documentation updated if needed?

---

## 7. Quick Reference

### Key Files

| Concept | Key File(s) |
|---|---|
| Wails bindings | `app.go` |
| App entry point | `main.go` |
| Core query flow | `pkg/services/discussion_engine.go` |
| SQL execution | `pkg/services/sql_execution.go` |
| Driver interface | `pkg/services/db_driver.go` |
| Driver registration | `pkg/services/db_registry.go` |
| LLM interface | `pkg/services/llm_client.go` |
| Provider implementations | `pkg/services/llm_openai.go`, `llm_anthropic.go`, `llm_ollama.go`, `llm_local.go` |
| DB connection model | `pkg/models/db_connection.go` |
| Conversation model | `pkg/models/conversation.go` |
| DB schema & migrations | `pkg/models/database.go` |
| Skill model | `pkg/models/skill.go` |
| Frontend app | `frontend/src/App.svelte` |
| Chat view | `frontend/src/ConversationView.svelte` |
| Settings view | `frontend/src/SettingsView.svelte` |
| Charts | `frontend/src/VizChart.svelte` |
| App data storage | `~/.yourql/yourql.db` |

### Common Patterns

```go
// Wails-bound function — must be on *App
func (a *App) SomeFunction() error { ... }

// Service function — called from app.go
func SomeService(args) (*Result, error) { ... }

// Driver registration
func init() {
    driverRegistry.Register(&MyDriver{})
}

// Migration — only add columns
ensureColumn("conversations", "new_column", "TEXT", "''")

// LLM client creation
client, err := NewLLMClient(provider)
response, err := client.ChatCompletion(ctx, messages)
```

### LLM Response Action Types

| Action | Meaning |
|---|---|
| `sql_query` | Execute this SQL and return results |
| `clarification` | Ask the user for more detail |
| `sql_exploration` | Run a read-only query to understand the data |
| `answer` | Provide a direct answer without querying |

None of these actions may ever result in a write to a data source —
`sql_query` and `sql_exploration` are both restricted to `SELECT`-only
statements (see §0's Data Source Read-Only Invariant).

---

## 8. Remember

**The user asked a question. Your job is to give them the right answer from
their database, in a way that feels fast, safe, and delightful — without
ever changing a single byte of their data source.**

Everything else is secondary. If you're unsure whether a change serves that
goal, pause, document your reasoning, and ask for review.

**Keep this document current.** If your change alters anything described here
— architecture, file locations, safety rules, priority order — update the
relevant section in the same change. A stale charter is worse than no charter.

*This document was created to ensure every agent, regardless of when they touch
the codebase, keeps the user's answer as the north star.*


