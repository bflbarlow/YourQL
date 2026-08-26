> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Tech Details Overhaul — Tool-Calling Era

**Status:** Design specification. Not yet implemented.

**Summary:** The old "Show Technical Details" toggle was built for a
single-request/single-response protocol where one LLM call produced one JSON
object containing the action, SQL, and answer. Tool calling splits each
question into multiple serial rounds of LLM calls, tool execution, and
streaming events. The old payload format — one blob of `{request_json,
response_json, llm_messages}` per round — is inadequate for debugging
multi-round streaming conversations. This document redesigns the tech
details surface to capture the right data, in the right structure, with
minimal redundancy.

This document assumes familiarity with the tool-calling architecture
([`TOOL_CALL_ENHANCEMENT.md`](TOOL_CALL_ENHANCEMENT.md)) and the project
charter ([`AGENT_READ_FIRST.md`](../AGENT_READ_FIRST.md)).

---

## 1. What's wrong with the current tech details

### 1.1 The old design (legacy)

`storePayload` creates a `role: "exploration"` message per LLM call containing:

```json
{
  "round": 0,
  "request_json": "{...the raw HTTP request...}",
  "response_json": "{...the raw HTTP response...}",
  "llm_messages": [{...the full conversation array...}]
}
```

The frontend renders each as a collapsible section with expandable
Request/Response/Messages panes inside a disclosure triangle.

### 1.2 What's broken

| Problem | Why |
|---|---|
| **Streaming response is empty** | `ChatCompletionWithToolsStreaming` returns `""` as the `responseJSON` — the raw SSE stream was discarded. Debugging "the model returned nothing" is impossible without the raw wire data. |
| **Request and messages are redundant** | `request_json` IS the marshalled version of `llm_messages` plus model/temperature fields. Storing both doubles the payload without adding information. |
| **Rounds have no structure** | An exploration round, a final query round, a chart round, and an error-retry round all get the same blob format. The frontend can't distinguish them except by the label string. |
| **No SQL visibility** | The old protocol embedded SQL in the JSON response. Tool calling puts it in the `query_database` tool's arguments. The tech details don't surface executed SQL or execution errors unless they go through `renderSQLError`. |
| **No execution timing** | You can see when a message was created but not how long SQL execution took or how many tokens each round consumed. |
| **Payload bloat** | Storing the full conversation array (including system prompt, schema, history) in every round's payload balloons the database. The system prompt alone is 2–4KB. Three rounds = 6–12KB per turn just in the system prompt repeated verbatim. |

### 1.3 What still works

- The frontend's collapsible disclosure triangle UI pattern is fine.
- The `showTechDetails` toggle gating is fine.
- The `role: "exploration"` message type for tech-details rows is fine.

---

## 2. New design

### 2.1 Per-round data model

Each LLM round stores a single `tech_detail` row with a typed, structured
JSON object. No more all-in-one blobs.

```go
type TechDetail struct {
    Version  int    `json:"version"`  // 1
    Round    int    `json:"round"`    // 0-based
    Kind     string `json:"kind"`     // "final", "exploration", "error_retry", "chart", "other"

    // Request — the messages array sent to the LLM, excluding the system
    // prompt (which is identical every round and can be inferred from the
    // first round's entry if needed).
    Request struct {
        MessageCount int    `json:"message_count"`
        LastUserMsg  string `json:"last_user_msg,omitempty"`  // truncated to 200 chars
    } `json:"request"`

    // Response — what the LLM returned.
    Response struct {
        FinishReason string      `json:"finish_reason,omitempty"` // "stop", "tool_calls", etc.
        TextContent  string      `json:"text_content,omitempty"`  // plain text (truncated to 500 chars)
        ToolCalls    []ToolCallSummary `json:"tool_calls,omitempty"`
    } `json:"response"`

    // SQL — non-empty only when this round involved a query_database call.
    // Error MUST be passed through sanitizeSQLError before storage (§2.3)
    // — driver connection errors can embed the DSN, which embeds the
    // data source password in plaintext.
    SQL struct {
        Query       string `json:"query"`
        ExecTimeMs  int    `json:"exec_time_ms,omitempty"`
        RowCount    int    `json:"row_count,omitempty"`
        Error       string `json:"error,omitempty"`  // only on failure, sanitized
    } `json:"sql,omitempty"`

    // Streaming — present only when streaming was enabled for this round.
    Stream struct {
        ChunkCount int    `json:"chunk_count"`
        ByteCount  int    `json:"byte_count"`
    } `json:"stream,omitempty"`

    // Timing
    DurationMs int `json:"duration_ms"` // wall clock for this round
}

type ToolCallSummary struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments,omitempty"` // truncated to 300 chars
}
```

### 2.2 What's NOT stored

| Removed | Why |
|---|---|
| `request_json` (full HTTP body) | Identical to the marshalled messages array. Redundant. |
| `llm_messages` (full conversation) | 2–8KB per round, mostly the system prompt repeated verbatim. If needed for a specific debug session, enable a temporary dev-mode log — it doesn't belong in persistent storage. |
| `response_json` (raw HTTP/SSE body) | For non-streaming, it's 1:1 with the structured `ToolCalls`/`TextContent` fields above. For streaming, the raw SSE stream is large and transient — captured at dev-time on demand (see §2.6), not persisted. |
| The system prompt per round | Same for every round in a turn. The first round's `message_count` and `last_user_msg` are enough to reconstruct the structure. |

### 2.3 Credential sanitization (absolute rule, not optional)

Per `AGENT_READ_FIRST.md` §3.5 and §4.0: **never log API keys or database
passwords.** This is not subject to risk/reward trade-off.

`BuildDSN` (in every `db_*.go` driver) embeds the data source password
directly in the connection string (`user:password@host:port/database`).
Go's `database/sql` drivers frequently include the DSN verbatim in
connection-failure error text (e.g., "dial tcp: connection refused to
user:hunter2@34.123.18.208:3306"). Today, `formatUserError` already
strips this before showing anything to the user in the main chat bubble—
but `buildErrorMetadata`'s `raw_error` field, which IS surfaced behind the
tech-details toggle, stores the **unmodified** error string. This is a
pre-existing exposure that this redesign inherits and must close, not
propagate further.

**Fix required before Phase 1 ships:** add `sanitizeSQLError(err error) string`
to `sql_execution.go` that regex-strips any `://user:pass@` or `user:pass@host`
pattern from error text before it reaches `TechDetail.SQL.Error` or
`buildErrorMetadata`'s `raw_error`. Apply it at both call sites — the tech
details redesign is the reason this gap was found, so it should be the one
that closes it, not a follow-up.

### 2.4 One row per round, stored in the existing `conversation_messages` table

Uses the existing `role: "exploration"` pattern. The `content` field holds
a human-readable label (e.g., "Round 1 — final query"), and the `metadata`
field holds the JSON-serialized `TechDetail` struct. This means:

- No schema change needed — reuses `metadata TEXT` column.
- The frontend's existing `role === 'exploration'` rendering already gates
  behind `showTechDetails`.
- The `storePayload` function is replaced by `storeTechDetail`.

```go
func storeTechDetail(conversationID uint, detail TechDetail) error {
    detailJSON, _ := json.Marshal(detail)
    detailStr := string(detailJSON)
    label := fmt.Sprintf("[Round %d — %s]", detail.Round, detail.Kind)
    _, err := CreateConversationMessage(conversationID, "exploration", label, nil, nil, &detailStr)
    return err
}
```

### 2.5 What the frontend shows

When `showTechDetails` is enabled, each round renders as a single summary
row — not three expandable panes. The user sees at a glance:

```
┌─ Round 1 — final ─────────────────────────────┐
│  Query: SELECT COUNT(*) FROM orders            │
│  Result: 1 row, 42ms                           │
│  Model: tool_calls → 1 call (query_database)   │
│  Stream: 23 chunks, 412 bytes                  │
│  Duration: 3.2s                                │
└────────────────────────────────────────────────┘
│ [expand for full tool call arguments]
│ [expand for SQL error detail]
```

Each round is a single line per data point — compact, scannable, no JSON
blobs to scroll through. The "expand" options only appear when there's
detail beyond what fits in the summary row (long tool arguments, SQL
errors).

### 2.6 Raw SSE capture (dev-time only)

For debugging streaming failures like "the model returned an empty
response," you need the raw SSE stream. This is NOT persisted in
`conversation_messages` — it's too large and only needed during active
debugging.

Add a field to `TechDetail.Stream` that, when a debug environment variable
is set (`YOURQL_DEBUG_STREAMS=1`), writes the raw SSE body to a file in
`~/.yourql/debug/streams/{conversation_id}_{round}_{timestamp}.txt` and
stores the file path in `TechDetail.Stream.DebugFile`. The frontend can
show a "Download raw stream" link.

Without the env var, `Stream` only contains `chunk_count` and `byte_count`
— enough to see that streaming occurred and how much data flowed.

### 2.7 Token and cost tracking

Add optional fields to `TechDetail` when the provider returns usage data:

```go
Tokens struct {
    Prompt     int `json:"prompt,omitempty"`
    Completion int `json:"completion,omitempty"`
} `json:"tokens,omitempty"`
```

OpenAI and Anthropic return this in every response. Ollama returns it in
the `done` object. The provider layer extracts it and the agentic loop
passes it to `storeTechDetail`. The frontend shows `Tokens: 2011 → 90` as
a compact summary on the round row.

---

## 3. Implementation plan

### Phase 1 — Replace `storePayload` with `storeTechDetail`

- Add `sanitizeSQLError` to `sql_execution.go` and apply it at both call
  sites (`TechDetail.SQL.Error` and `buildErrorMetadata`'s `raw_error`) —
  **blocking requirement, not deferred** (§2.3, §3.5).
- Define the `TechDetail` struct and per-round summary types.
- Replace `storePayload` calls in `runAgenticLoop` with `storeTechDetail`,
  populated from the round's actual data (tool calls, SQL, timing).
- The `storePayload` function is deleted.
- Existing `role: "exploration"` messages in old conversations are
  unaffected — the frontend handles both formats.

### Phase 2 — Frontend rendering

- Replace the three-pane (Request/Response/Messages) expander with the
  compact summary-row design described in §2.5.
- Add expand-for-detail toggles for long tool call arguments and SQL
  errors.
- Add token count display when available.

### Phase 3 — Raw SSE capture

- Add the `YOURQL_DEBUG_STREAMS` env var gate.
- Write raw SSE bodies to `~/.yourql/debug/streams/` when enabled.
- Add `Stream.DebugFile` to the tech detail.
- Add a "Download raw stream" link in the frontend.

### Phase 4 — Remove obsolete payload fields

- Remove `request_json`, `response_json`, and `llm_messages` from all
  payload-logging code paths (also used in `summarizeResults`).
- The `ChatCompletionWithToolsStreaming` return signature no longer needs
  to return `responseJSON` as `""` — it returns the assembled `*ChatMessage`
  marshalled as JSON (identical to what the blocking path returns for
  `storePayload` compatibility during the transition).

---

### 3.5 Risk/reward analysis (per `AGENT_READ_FIRST.md` §4.4)

**Change:** Replace the per-round payload logging format (`storePayload`)
with a structured, typed `TechDetail` format (`storeTechDetail`), and add
credential sanitization to the SQL error path that tech details expose.

**Fundamental goal impact:** None on answer accuracy or delivery — this is
a debugging/observability surface, not part of the answer pipeline. It
does, however, directly affect **User trust** (§4.1): tech details exist so
power users can verify what the app did, and if that surface leaks a
credential, it actively harms the read-only-and-safe trust promise this
app depends on.

**Risk category(ies):** Data safety (credential exposure via `raw_error`/
`SQL.Error`, per §2.3), Regression (existing conversations' old-format
tech detail rows must still render), Data integrity (none — additive only,
no destructive schema change).

**Failure mode(s):**
1. A driver connection error containing the DSN password reaches
   `TechDetail.SQL.Error` unsanitized and is visible to any user who
   enables the tech-details toggle.
2. Old conversations' `role: "exploration"` rows (old blob format) render
   incorrectly or crash the frontend once it expects the new `TechDetail`
   shape.
3. The raw SSE debug-file feature (§2.6) writes model output to disk
   indefinitely, silently consuming disk space if never cleaned up.

**Reward:** Restores tech-details usefulness for the tool-calling
architecture — currently the streaming response is silently empty in the
payload log, making the exact failure mode raised in this conversation
(model returns nothing, need to see the raw stream) undebuggable today.
Also cuts persisted payload size substantially (no repeated system prompt
per round) and adds token/timing visibility that didn't exist before.

**Mitigations:**
1. `sanitizeSQLError` closes the credential-leak gap as a required part of
   Phase 1, not a follow-up (§2.3).
2. The frontend detects old-format rows by the presence of the
   `request_json` key and renders them with the legacy three-pane view;
   new rows are detected by the `version` key. No migration of historical
   rows required — both formats render correctly, permanently.
3. Raw SSE capture is opt-in via `YOURQL_DEBUG_STREAMS` and only ever
   active for local development — it is never enabled by default in a
   shipped build, and the debug directory is documented as something the
   developer manually clears.

**Decision:** Proceed, with the credential sanitization fix in §2.3 as a
blocking requirement for Phase 1 — not deferred, since the charter treats
credential logging as an absolute rule with no risk/reward trade-off
available (§4.0).

---

## 4. Files affected

| File | Change |
|---|---|
| `pkg/services/agentic_loop.go` | Add `storeTechDetail` call at every `logRound` site. Pass structured round data. Delete `storePayload` references. |
| `pkg/services/discussion_engine.go` | Delete `storePayload` function. Update `summarizeResults` to use `storeTechDetail` for its single-round debug entry. |
| `pkg/services/llm_client.go` | Add token/cost fields to `StreamEvent` or a separate return value for `ChatCompletionWithToolsStreaming`. |
| `pkg/services/llm_openai.go` | Extract `usage` from the final SSE chunk for token tracking. |
| `pkg/services/llm_anthropic.go` | Extract `usage` from `message_stop` event. |
| `pkg/services/sql_execution.go` | Add `sanitizeSQLError(err error) string` — strips embedded DSN credentials from error text before it reaches any tech-details-visible field (§2.3). Applied at the `TechDetail.SQL.Error` and `buildErrorMetadata` call sites. |
| `frontend/src/ConversationView.svelte` | Replace three-pane expander with summary-row rendering. Handle both old format (detected by presence of `request_json` key) and new format (`version` key). |
