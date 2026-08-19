# YourQL Headless Mode — Technical Design

**Date:** 2026-08-16  
**Project:** YourQL — a headless execution mode for the external testing
harness (`yourql-test-harness`)  
**Status:** Proposal — no code implemented  
**Companion to:** [`AGENT_READ_FIRST.md`](AGENT_READ_FIRST.md),
[`SQLITE_DB_SWITCHER.md`](SQLITE_DB_SWITCHER.md),
[`TESTING_EXECUTION.md`](TESTING_EXECUTION.md)

---

## 1. Purpose

The external test harness must drive the **real** YourQL pipeline — the LLM →
SQL → result → message path in `pkg/services/discussion_engine.go` and
`pkg/services/agentic_loop.go` — without linking against YourQL's Go code.
The harness is a separate Go module that talks to a running YourQL process
over HTTP.

This document prescribes that headless mode: a minimal, localhost-only HTTP
API that reuses the existing `ProcessUserMessageWithContext` pipeline and the
existing SQLite app database, with **no UI/window**.

### 1.1 Hard Constraints (from `AGENT_READ_FIRST.md`)

- **The read-only invariant is unchanged.** Headless mode does not add,
  weaken, or bypass any safety check. Every message still flows through
  `ProcessUserMessageWithContext` → `runAgenticLoop` →
  `executeSQLWithMode`, which enforces SELECT-only and the exploration
  safety modes exactly as the GUI does. Headless mode is a *transport*, not a
  new execution path around the safety gates.
- **No new credential exposure.** The API binds to `127.0.0.1` only, never
  `0.0.0.0`. It never returns API keys or data-source passwords (the
  models already carry `json:"-"` on those fields).
- **App-only, never data sources.** Headless mode manipulates only
  `models.DB` (the app's own local SQLite database) and the agentic-loop
  pipeline. It does not modify, write to, or bypass any configured *data
  source*.

### 1.2 What Headless Mode Is Not

- It is **not** a general-purpose remote API. It is single-user, localhost,
  unauthenticated, and intended only for the test harness and local
  diagnostics.
- It is **not** a replacement for the GUI. No HTML rendering, no Svelte
  components, no window. Charts/summaries are still *computed* by the
  pipeline and recorded in the database; the harness asserts on their
  presence/fields rather than their pixels.
- It is **not** a multi-instance coordinator. One headless process owns one
  `models.DB` (the database selected by the pointer file / `-db-path` flag).

---

## 2. Launch & Configuration

### 2.1 CLI Flags

Headless mode is selected at launch:

```
YourQL -headless -port 8745 [-db-path /path/to/yourql.db]
```

| Flag | Default | Meaning |
|---|---|---|
| `-headless` | `false` | Start the HTTP server instead of the Wails window. Without this flag, behavior is unchanged (normal GUI launch). |
| `-port` | `8745` | Localhost TCP port for the HTTP API. Must be `127.0.0.1`, never `0.0.0.0`. |
| `-db-path` | *(empty → use pointer file/default)* | Explicit app-database file to open. When set, it overrides `resolveDBPath()` and the `~/.yourql/active_db_path.json` pointer file for this launch only. |

`-db-path` is the primary integration point for the harness: the harness
creates a blank database at a path it controls and launches YourQL with
`-headless -db-path <that file>`. This is cleaner than writing the pointer
file for each run and avoids clobbering the user's real pointer. The pointer
file mechanism (`SQLITE_DB_SWITCHER.md`) remains the GUI-only path.

> **Implementation note.** As of this writing, `pkg/models/database.go`
> exposes `ConnectDatabase()` (pointer-file-aware) and `CreateBlankDatabaseAt(path)`
> (creates+migrates a *new* file), but no function that opens an *existing*
> arbitrary path bypassing the pointer file. `-db-path` requires one new
> additive function, `ConnectDatabaseAt(path string) error` — open the given
> file and run `migrate()`, mirroring `ConnectDatabase()` minus the
> `resolveDBPath()` call. This is new code introduced by this document, not
> a pre-existing helper (an earlier draft of this document and of
> `TESTING_EXECUTION.md` assumed it already existed; it does not — see §5.2).

### 2.2 Startup Sequence (headless)

1. Parse flags. If `-headless`, skip `wails.Run` entirely.
2. If `-db-path` is set, call the new `models.ConnectDatabaseAt(path)` (§2.1
   note); otherwise call the existing `models.ConnectDatabase()`, which uses
   `resolveDBPath()` → pointer file → default. Either path creates and
   migrates a blank database if the file is new or empty (the existing
   migration system already produces a blank slate for free).
3. `setupLogging()` (unchanged, reads `app_settings`).
4. Wire `services.SetAppVersionGetter(func() string { return appVersion })`
   exactly as `app.go`'s `startup()` does, so `/api/health` can report the
   real build version.
5. Start the `net/http` server bound to `127.0.0.1:<port>`.
6. Log a single, greppable line: `headless server listening on 127.0.0.1:<port> (db=<path>)`.

### 2.3 Shutdown

- `SIGINT` / `SIGTERM` → stop the HTTP server, close `models.DB`, exit 0.
- A `POST /api/shutdown` endpoint is **intentionally omitted** — the harness
  terminates the process via signal, which is the cleanest cross-platform
  contract and avoids an unauthenticated remote-kill primitive.

---

## 3. HTTP API

### 3.1 Conventions

- Base URL: `http://127.0.0.1:<port>`
- All request/response bodies are `application/json` (UTF-8).
- Errors return an envelope:

```json
{ "error": "human-readable message", "code": "conversation_not_found" }
```

- Error codes are stable strings the harness can assert on (see §3.7).
- Non-2xx status codes follow HTTP semantics (400 bad input, 404 not found,
  409 conflict, 500 internal error).
- All IDs are YourQL's `uint` IDs (JSON numbers).

### 3.2 Endpoint Summary

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/health` | Readiness + app info |
| `POST` | `/api/conversations` | Create a conversation (reuses `CreateConversationWithDefaults`) |
| `GET` | `/api/conversations/{id}` | Read conversation settings |
| `PATCH` | `/api/conversations/{id}` | Update conversation settings |
| `POST` | `/api/conversations/{id}/messages` | Send a user message — **blocking**; returns when processing completes |
| `POST` | `/api/conversations/{id}/messages/stream` | Send a user message — **SSE**; streams phase/delta events |
| `POST` | `/api/conversations/{id}/cancel` | Cancel in-progress processing |
| `GET` | `/api/conversations/{id}/messages` | Read all messages for a conversation |
| `GET` | `/api/conversations/{id}/queries` | Read the query-execution log for a conversation |
| `GET` | `/api/settings` | Read all app settings (incl. resolved timeouts) |
| `PUT` | `/api/settings` | Upsert app settings (validated timeout keys) |

### 3.3 `GET /api/health`

Returns `200 OK` when the app is connected and ready to accept messages.

```json
{
  "status": "ok",
  "version": "dev",
  "db_path": "/Users/me/.yourql/yourql.db",
  "headless": true,
  "uptime_ms": 1234
}
```

- `version` is read via the same `services.SetAppVersionGetter` mechanism
  `app.go`'s `startup()` uses today (`pkg/services/total_export.go`) — not a
  new version channel.
- `db_path` is the resolved active database path (the harness asserts this to
  confirm it's running against the intended blank database).

### 3.4 `POST /api/conversations`

Create a conversation using the *same* logic the GUI uses
(`services.CreateConversationWithDefaults`), so discussion defaults and the
`MaxContextMessages` floor are applied identically.

**Request:**

```json
{
  "title": "Optional title",
  "llm_provider_id": 1,
  "data_source_id": 2
}
```

`llm_provider_id` / `data_source_id` may be omitted to fall back to the
configured defaults (same as GUI).

**Response `200 OK`:**

```json
{
  "id": 7,
  "title": "Optional title",
  "llm_provider_id": 1,
  "data_source_id": 2,
  "status": "active",
  "max_messages": 20,
  "max_context_messages": 5,
  "pinned": false,
  "tech_details": false,
  "context_details": false,
  "summarize": false,
  "viz_enabled": true,
  "streaming_enabled": false,
  "created_at": "2026-08-16T19:00:00Z",
  "updated_at": "2026-08-16T19:00:00Z"
}
```

(JSON field names match `models.Conversation`'s tags. `viz_enabled` defaults
to `true` and `streaming_enabled` to `false` per the `conversations` table's
`ensureColumn` defaults in `pkg/models/database.go` — confirm against the
live schema before relying on any default shown here, per §7's caveat.)

### 3.5 `GET /api/conversations/{id}` and `PATCH /api/conversations/{id}`

Read / update conversation settings. The `PATCH` accepts a partial body with
only the fields to change, mirroring the existing `UpdateConversation*`
service functions:

```json
{
  "title": "new title",
  "status": "archived",
  "llm_provider_id": 3,
  "data_source_id": 4,
  "max_messages": 30,
  "max_context_messages": 8,
  "pinned": true,
  "tech_details": true,
  "context_details": true,
  "summarize": true,
  "viz_enabled": true,
  "streaming_enabled": false
}
```

Omitted fields are left unchanged. `GET` returns the same shape as §3.4.

### 3.6 `POST /api/conversations/{id}/messages` (blocking)

The core endpoint. Sends a user message through the full pipeline and blocks
until processing completes, is cancelled, or times out.

**Request:**

```json
{ "message": "How many customers are there?" }
```

**Response `200 OK`** (also used on a pipeline error — the error is
returned as `status`, not as an HTTP failure, because a pipeline error is a
*successful observation* the harness wants to grade):

```json
{
  "conversation_id": 7,
  "status": "completed",
  "error": null,
  "duration_ms": 1234,
  "phase": "Complete",
  "messages_added": [
    {
      "id": 100,
      "conversation_id": 7,
      "role": "assistant",
      "content": "There are 5 customers.",
      "llm_content": "...",
      "sql_results": "...",
      "metadata": "{...}",
      "tool_transcript": "...",
      "created_at": "2026-08-16T19:00:01Z"
    }
  ],
  "query": {
    "id": 99,
    "status": "success",
    "generated_sql": "SELECT COUNT(*) AS cnt FROM customers",
    "result_summary": "...",
    "error_message": null,
    "execution_time_ms": 12,
    "tokens_used": 450
  },
  "stream_events": [
    { "type": "content_delta", "content": "There" },
    { "type": "content_delta", "content": " are" },
    { "type": "done", "finish_reason": "stop" }
  ]
}
```

**Status values** (stable, harness-assertable):

| `status` | Meaning |
|---|---|
| `completed` | Pipeline finished and produced a response (possibly an error message to the user). |
| `error` | Pipeline returned an error before producing a response (e.g. no provider, schema introspection failed). `error` is set. |
| `cancelled` | Processing was cancelled via §3.8. |
| `timeout` | Processing exceeded the per-request timeout (§3.9) and was cancelled. |

**Details:**

- `messages_added` is the set of `conversation_messages` rows written during
  this call (typically the persisted user message plus one assistant/system
  message). The harness grades on `messages_added`, `query`, and
  `stream_events` — but it may also re-read the full conversation via §3.10
  or directly from SQLite.
- `stream_events` captures the `onPhase`/`onStream` callbacks for the call.
  It is included even on the blocking path so the harness can assert on
  phase transitions and deltas without a second request.
- `query` reflects the latest `queries` row for this message.

### 3.7 `POST /api/conversations/{id}/messages/stream` (SSE)

Same as §3.6 but streams events over Server-Sent Events instead of blocking.

**Request:** identical JSON body.

**Response:** `text/event-stream`. Each line is a JSON event:

```
event: phase
data: {"phase":"Thinking with LLM..."}

event: stream
data: {"type":"content_delta","content":"There"}

event: result
data: {"status":"completed","conversation_id":7,"query":{...},"messages_added":[...]}

event: error
data: {"status":"error","error":"..."}
```

The terminal event is always `result` or `error`, after which the connection
closes. The harness may use this endpoint for fine-grained timing/streaming
assertions; the blocking endpoint remains the primary path.

### 3.8 `POST /api/conversations/{id}/cancel`

Cancels in-progress processing for the conversation.

**Response `200 OK`:** `{ "cancelled": true }`  
**Response `409 Conflict`:** `{ "error": "no active processing", "code": "no_active_processing" }`

`App.CancelProcessing` (`app.go`) already implements this exact pattern —a
`map[uint]context.CancelFunc` guarded by a mutex, keyed by conversation ID —
but it is a method on `*App`, which headless mode does not instantiate (no
Wails runtime, no window). Headless mode needs its own package-level
`map[uint]context.CancelFunc` + `sync.Mutex` with the same register/
unregister/cancel shape, not a literal reuse of `App`'s map.

### 3.9 `GET /api/conversations/{id}/messages`

Returns all messages for the conversation, oldest first:

```json
{
  "messages": [
    {
      "id": 99,
      "conversation_id": 7,
      "role": "user",
      "content": "How many customers are there?",
      "llm_content": null,
      "sql_results": null,
      "metadata": null,
      "tool_transcript": null,
      "created_at": "2026-08-16T19:00:00Z"
    }
  ]
}
```

### 3.10 `GET /api/conversations/{id}/queries`

Returns the query-execution log for the conversation, oldest first:

```json
{
  "queries": [
    {
      "id": 99,
      "conversation_id": 7,
      "question": "How many customers are there?",
      "generated_sql": "SELECT COUNT(*) AS cnt FROM customers",
      "status": "success",
      "result_summary": "1 row",
      "error_message": null,
      "execution_time_ms": 12,
      "tokens_used": 450,
      "created_at": "2026-08-16T19:00:01Z"
    }
  ]
}
```

### 3.11 `GET /api/settings`

Returns the full `app_settings` table plus the resolved/effective timeout
values. This is the canonical way for the harness to confirm what timeout
values the pipeline will actually use before sending a message.

**Response `200 OK`:**

```json
{
  "settings": {
    "pipeline_timeout_seconds": "300",
    "summarization_timeout_seconds": "120"
  },
  "timeouts": {
    "pipeline_timeout_seconds": 300,
    "summarization_timeout_seconds": 120
  }
}
```

- `settings` is the raw `app_settings` key/value table (verbatim strings;
  keys are absent when unset).
- `timeouts` is the **resolved/effective** value after defaults are applied
  (defaults: `pipeline_timeout_seconds` = 180, `summarization_timeout_seconds`
  = 120). Assert on this field when you need to know what will actually run,
  not just what is stored.

### 3.12 `PUT /api/settings`

Upserts one or more app settings. The two timeout keys are validated against
the same bounds the GUI enforces; all other keys are stored verbatim. This
endpoint is the harness's replacement for writing `app_settings` rows via raw
SQL when running against headless mode.

**Request:**

```json
{
  "settings": {
    "pipeline_timeout_seconds": "600",
    "summarization_timeout_seconds": "180"
  }
}
```

**Rules:**

| Key | Bounds | Default when unset/empty |
|---|---|---|
| `pipeline_timeout_seconds` | integer 30–3600 | 180 |
| `summarization_timeout_seconds` | integer 10–600 | 120 |
| any other key | none (stored verbatim) | n/a |

- An empty string (`""`) deletes the key, making the pipeline fall back to
  its default (consistent with `services.SetAppSetting`).
- Validation is **all-or-nothing**: if any value is invalid, the request is
  rejected with `400 invalid_setting` before anything is written.

**Response `200 OK`:** same shape as §3.11 (resulting settings + resolved
 timeouts).

**Timeout semantics (important for the harness):**

- `pipeline_timeout_seconds` caps the entire LLM conversation turn. It is
  read fresh inside `ProcessUserMessageWithContext` (`discussion_engine.go`).
- `summarization_timeout_seconds` caps only the post-answer summary
  generation (`agentic_loop.go`), independently of the pipeline timeout.
- Both are read **fresh on every message**, so a `PUT /api/settings` issued
  before `POST .../messages` takes effect on that message with **no
  restart**.
- When the pipeline timeout is exceeded, the message envelope reports
  `status: "timeout"` (§3.6).

### 3.13 Error Codes

| Code | HTTP | Meaning |
|---|---|---|
| `conversation_not_found` | 404 | `{id}` doesn't exist |
| `invalid_request` | 400 | Malformed body / missing required field |
| `no_active_processing` | 409 | `cancel` called with nothing in flight |
| `processing_already_active` | 409 | A message is already processing for this conversation |
| `invalid_setting` | 400 | A settings value failed validation (e.g. timeout out of range) |
| `internal_error` | 500 | Unexpected panic/error (stack logged, not leaked) |

(An earlier draft of this table included a `not_headless` code for "endpoint
hit in a non-headless build." Dropped: these routes are only ever
registered inside `runHeadless()`, so a non-headless binary has no mux to
hit them on — the code described an unreachable state, not a real one.)

---

## 4. Concurrency & State Model

- **One database, one owner.** Headless mode owns `models.DB` exactly like the
  GUI does. `SetMaxOpenConns(1)` + WAL is unchanged.
- **Single-flight per conversation.** Only one message may process per
  conversation at a time. A second concurrent `POST .../messages` returns
  `409 processing_already_active`. Different conversations may technically
  interleave, but the harness should run scenarios sequentially; the
  single-flight guard is a safety net, not a concurrency feature.
- **`onPhase` / `onStream` capture.** In headless mode these callbacks append
  to an in-memory slice owned by the request goroutine (the GUI path emits
  Wails events instead). No shared mutable state across requests.
- **Panic recovery.** Each HTTP handler wraps the pipeline call in a `recover()`
  that logs the stack and returns `500 internal_error`, so a pipeline panic
  doesn't take down the server.

---

## 5. Implementation Sketch (YourQL side)

All changes are **additive** and gated behind `-headless`; the GUI path is
unchanged.

### 5.1 `main.go`

```go
// Before wails.Run(...):
if *headlessFlag {
    if err := runHeadless(*portFlag, *dbPathFlag); err != nil {
        slog.Error("headless error", "error", err)
        os.Exit(1)
    }
    return
}
// ... existing wails.Run unchanged ...
```

### 5.2 New file `headless.go` (package main)

```go
func runHeadless(port int, dbPath string) error {
    if dbPath != "" {
        if err := models.ConnectDatabaseAt(dbPath); err != nil { // new function, see §2.1
            return fmt.Errorf("connect database: %w", err)
        }
    } else if err := models.ConnectDatabase(); err != nil {
        return fmt.Errorf("connect database: %w", err)
    }
    defer models.DB.Close()
    setupLogging()
    services.SetAppVersionGetter(func() string { return appVersion })

    srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port)}
    mux := http.NewServeMux()
    registerHeadlessHandlers(mux)  // §5.3
    srv.Handler = mux

    slog.Info("headless server listening", "addr", srv.Addr, "db", dbPath)
    return srv.ListenAndServe()
}
```

`ConnectDatabaseAt(path string) error` is new code this document introduces
(§2.1) — it does not exist in `pkg/models/database.go` today. It should open
`path` directly with `sql.Open("sqlite", ...)`, assign `models.DB`, and call
the existing unexported `migrate()`, skipping `resolveDBPath()`/the pointer
file entirely. `CreateBlankDatabaseAt` (already implemented, per
`SQLITE_DB_SWITCHER.md`) is a different function — it *creates* a new file
and refuses to touch a non-empty one; `ConnectDatabaseAt` *opens* whatever is
already at `path` (new or existing) the same way `ConnectDatabase()` opens
the resolved default path.

### 5.3 `headless_handlers.go` (package main)

Each handler maps 1:1 onto existing service functions — **no new pipeline
logic**:

| Endpoint | Backing function(s) |
|---|---|
| `POST /api/conversations` | `services.CreateConversationWithDefaults` |
| `GET /api/conversations/{id}` | `services.GetConversationByID` |
| `PATCH /api/conversations/{id}` | `services.UpdateConversation*` |
| `POST /api/conversations/{id}/messages` | `services.ProcessUserMessageWithContext(ctx, ...)` with captured `onPhase`/`onStream` |
| `POST /api/conversations/{id}/cancel` | a package-level `map[uint]context.CancelFunc` + mutex, mirroring `App.activeCancels`'s register/unregister/cancel shape (see §3.8) — headless has no `*App` to share it with |
| `GET /api/conversations/{id}/messages` | `services.GetConversationMessages` |
| `GET /api/conversations/{id}/queries` | new function `services.GetQueriesByConversation(conversationID uint) ([]*models.Query, error)` — does not exist today; the closest existing code is `CreateQuery`/`UpdateQueryStatus` in `pkg/services/query.go`, which write `queries` rows but nothing currently reads them back by conversation. This needs a plain `SELECT ... FROM queries WHERE conversation_id = ? ORDER BY created_at ASC`, following the exact pattern `GetConversationMessages` already uses for `conversation_messages`. |
| `GET /api/settings` | `services.GetAllAppSettings` + `services.GetTimeoutSetting` (both already exist in `pkg/services/app_settings.go`). |
| `PUT /api/settings` | `services.SetAppSetting` (already exists) with a thin validation layer for the two timeout keys; no new persistence logic. |

The blocking message handler applies a timeout context (default 180s,
matching the GUI's `context.WithTimeout`) and maps the result to the §3.6
envelope.

### 5.4 What Must NOT Be Done

- Do **not** short-circuit to `executeSQLWithMode` directly — messages must
  go through `ProcessUserMessageWithContext` so the safety gates, exploration
  modes, and one-shot finality all run exactly as in the GUI.
- Do **not** bind to `0.0.0.0`.
- Do **not** log message content, API keys, or data-source passwords.
- Do **not** add an unauthenticated shutdown/kill endpoint.

---

## 6. Harness Integration Flow (reference)

The external Go harness does the following per config×scenario run:

```
1. Create a blank DB file at a temp path (or use models.CreateBlankDatabaseAt
   equivalent via raw SQLite + the harness's own driver — no YourQL import).
2. Seed the DB via raw SQL (documented schema contract, §7):
   - llm_providers: an "openai" provider whose base_url points at the
     harness's own mock OpenAI-compatible LLM server (the openai client
     supports a custom base_url without an API key, and it speaks the
     standard tool-calls format that supports is_exploration — the "local"
     provider's fallback protocol cannot express exploration queries, so do
     NOT use it).
   - data_sources: a SQLite fixture path with seeded tables.
   - skills / discussion_defaults / agent_loop_config: optional.
3. Launch: YourQL -headless -port 8745 -db-path <blank.db>
4. Poll GET /api/health until 200 (readiness).
5. POST /api/conversations  → conversation ID (defaults applied by YourQL).
6. Optional `PUT /api/settings { settings: { pipeline_timeout_seconds: "...", summarization_timeout_seconds: "..." } }` — global timeouts, applied to the next message with **no restart** (§3.12); and/or `PATCH /api/conversations/{id}` for per-conversation settings.
7. `POST /api/conversations/{id}/messages { "message": "..." }` → block until done (respects the timeouts set in step 6; see §3.12).
8. Read GET .../messages and .../queries (or the response envelope).
9. Grade success with the harness's OWN heuristics (no YourQL code).
10. Append finding to the bug report.
11. SIGTERM the process; delete the temp DB.
```

The harness never imports `YourQL/pkg/...`. It only knows: the SQLite schema
(§7, a documented contract), the HTTP API (§3), and the CLI flags (§2).

---

## 7. SQLite Schema Contract (for seeding/reading)

The harness reads/writes these tables with a stock SQLite driver. Field names
below are the column names.

- `llm_providers(id, name, provider, model, base_url, api_key, is_default,
  is_active, config, created_at, updated_at)` — for a deterministic run,
  seed `provider='openai'`, `base_url='http://127.0.0.1:<mock-port>'` (no
  `/v1` suffix — the openai client appends `/chat/completions` directly),
  `api_key=''` (custom base URLs skip the key check). Do NOT use
  `provider='local'`: its fallback protocol always sets
  `is_exploration: false`, so it cannot exercise exploration modes.
- `data_sources(id, name, type, host, port, database_name, username,
  password, ssl_mode, is_default, is_active, config, extra, file_path,
  file_type, created_at, updated_at)` — for SQLite fixtures, set
  `type='sqlite'` and `database_name` to the fixture file path (the SQLite
  driver reads `conn.Database` = the `database_name` column; `file_path` /
  `file_type` are for CSV/Excel only). `config` is JSON holding
  `ExplorationSafety`, `DefaultLimit`, `BusinessRules`, etc. (see
  `models.DataSourceConfig`).
- `conversations(id, title, llm_provider_id, data_source_id, status,
  max_messages, max_context_messages, pinned, created_at, updated_at,
  deleted_at, tech_details, context_details, summarize, viz_enabled,
  streaming_enabled)`.
- `conversation_messages(id, conversation_id, role, content, llm_content,
  sql_results, metadata, tool_transcript, created_at)` — the primary grading
  surface (`role='assistant'`, `sql_results`, `metadata` chart/summary flags).
- `queries(id, conversation_id, question, llm_provider_id, data_source_id,
  original_query, generated_sql, data_source_name, status, result_summary,
  error_message, execution_time_ms, tokens_used, cost_estimate, created_at,
  updated_at)`. `error_category` (referenced in `models.Query`) is an
  `ensureColumn`-added column, not part of the original `CREATE TABLE`;
  confirm it's present if seeding this table directly rather than through
  YourQL's own migrations. **No existing service function reads this table
  back by conversation ID** — §5.3 flags `GetQueriesByConversation` as new
  code the headless implementation must add.
- `skills(id, name, markdown_content, is_active, created_at, updated_at)`,
  `conversation_skills(conversation_id, skill_id)`.
- `app_settings(key, value)`, `discussion_defaults(key, value)`,
  `agent_loop_config(key, value)`.

The harness is expected to re-verify these against the live schema before
relying on them (schema drifts faster than docs) — see
`AGENT_READ_FIRST.md`'s note that docs describe a point in time.

---

## 8. Testing Headless Mode Itself

A handful of self-tests verify the headless surface without the external
harness:

- Launch `-headless` with a temp `-db-path`; assert `/api/health` reports the
  correct `db_path` and the DB was auto-migrated (blank slate).
- `POST /api/conversations` then `GET /api/conversations/{id}` round-trips.
- `POST .../messages` with a mock `openai` provider (custom base_url) returns the §3.6 envelope.
- A second concurrent `POST .../messages` returns `409 processing_already_active`.
- `POST .../cancel` on an idle conversation returns `409 no_active_processing`.
- Unknown conversation ID returns `404 conversation_not_found`.

These are YourQL-internal checks only; the *behavioral* grading belongs to the
external harness, not here.

---

## 9. Open Questions / Non-Goals

- **No authentication.** Headless mode trusts anything on `127.0.0.1`. If this
  is ever exposed beyond localhost, add a per-launch random token. Out of
  scope now.
- **No multi-conversation parallelism guarantees.** Sequential runs only.
- **No Windows/macOS GUI coexistence.** `-headless` and the GUI are mutually
  exclusive launches; you run one or the other, not both.
- **Streaming fidelity.** The SSE endpoint mirrors the GUI's event stream but
  does not reproduce UI pacing; timing assertions should be loose.

---

## 10. Compliance Summary

> **Not the same rule as the testing harness's "find and document only"
> banner.** `TESTING_EXECUTION.md`/`TESTING_SUITE.md` forbid the *testing
> process* from writing application code — that rule governs running tests,
> not building test infrastructure. Implementing headless mode is a
> legitimate YourQL feature (like `SQLITE_DB_SWITCHER.md`), built once,
> ahead of time, by a human-reviewed change — not something the harness does
> to itself while running.

| Charter Reference | Compliance |
|---|---|
| §0 Data Source Read-Only Invariant | Messages flow through the unchanged `ProcessUserMessageWithContext` → `runAgenticLoop` → `executeSQLWithMode` chain; headless adds no write path. §5.4 explicitly forbids short-circuiting past this chain. |
| §2.2 Wails Lifecycle ("Never call `models.ConnectDatabase()` outside of `startup`") | Headless mode has no Wails runtime, so there is no `startup` callback to call it from. The *intent* of the rule — connect exactly once, before any request is served, never again for the life of the process — is preserved by §2.2's startup sequence: `ConnectDatabase()`/`ConnectDatabaseAt()` runs once, synchronously, before `http.Server.ListenAndServe()`. This is a literal exception to the rule's wording (there is no `startup` to call it from) but not to its spirit. Worth flagging explicitly to any future agent auditing this rule, since a naive grep for "outside of startup" would flag this call site. |
| §3.2 Migration Safety | The new `ConnectDatabaseAt(path)` (§2.1, §5.2) still calls the existing, unmodified `migrate()` — additive-only, `ensureColumn`/`CREATE TABLE IF NOT EXISTS` — exactly as `ConnectDatabase()` does. No new migration logic is introduced. |
| §3.5 Security | `127.0.0.1`-only bind; API-key/password fields carry `json:"-"`; no logging of message content or credentials. |
| §3.9 UX/Trust | N/A to the GUI; headless error envelopes use clear, stable codes rather than leaking raw stack traces. |
| §3.1 `models.DB` | Headless owns `models.DB` exactly as the GUI does; `-db-path`/`-headless` are additive and leave the GUI path unchanged. |
| §4.2 When to Pause | This adds process-lifecycle code, a new persistent network listener, and two new Go functions (`ConnectDatabaseAt`, `GetQueriesByConversation`) — it does not touch schema, drivers, or prompt construction, but the new-code-path bar in §4.2 is still met and a §4.4 analysis is required before implementation (see §10.1). |

### 10.1 Risk/Reward Analysis (per §4.4's template)

- **Change:** Add a `-headless -port -db-path` launch mode plus a minimal
  HTTP API (§3) that reuses the existing agentic-loop pipeline; add two new
  Go functions (`models.ConnectDatabaseAt`, `services.GetQueriesByConversation`).
- **Fundamental goal impact:** None on answer accuracy/delivery for GUI
  users — the GUI code path is untouched. Indirectly *improves* the
  fundamental goal by enabling the automated testing program
  (`TESTING_EXECUTION.md`) that exists to find answer-quality regressions.
- **Risk categories:** Data safety (a new open network listener, even on
  loopback) / Regression (new startup branch in `main.go`) / Reliability
  (a long-lived HTTP server introduces a new failure surface: hung requests,
  port conflicts, panics).
- **Failure modes considered:** (1) listener accidentally bound to
  `0.0.0.0` — mitigated by hardcoding `127.0.0.1` in `runHeadless`, never
  taking a bind-address flag; (2) a pipeline panic inside an HTTP handler
  crashing the process — mitigated by per-handler `recover()` (§4); (3) a
  headless flag accidentally reaching a production/GUI build — mitigated by
  `-headless` defaulting to `false` and being off by default in every
  existing launch path (`wails dev`, `wails build`, release binaries); (4)
  `ConnectDatabaseAt` being pointed at the user's real `~/.yourql/yourql.db`
  by a misconfigured harness — out of scope for this document to fully
  mitigate (it is the harness's responsibility to only ever pass disposable
  paths), but worth a loud warning in the harness's own docs.
- **Reward:** Enables deterministic, scriptable, config-matrix testing of
  the real pipeline (per `TESTING_EXECUTION.md`) without UI-automation
  flakiness — the primary blocker identified when deciding the harness's
  architecture.
- **Mitigations:** loopback-only bind (no flag to change it), per-handler
  panic recovery, single-flight guard per conversation, no shutdown/kill
  endpoint, `-headless` off by default everywhere.
- **Decision:** Proceed with implementation — low risk given the
  loopback-only, opt-in, additive design; does not touch any §4.0 absolute
  rule. Log this analysis (or a refined version, once implemented) in
  `documentation/RISK_ANALYSIS_LOG.md` per §4.4, mirroring
  `SQLITE_DB_SWITCHER.md` §7.1's precedent.
