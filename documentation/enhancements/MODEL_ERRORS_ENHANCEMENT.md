> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# MODEL_ERRORS_ENHANCEMENT.md

## Goal

Improve error handling across the LLM→SQL→Results pipeline so that transient failures, LLM misbehaviors, and SQL execution errors are handled gracefully in the backend — without exposing raw error messages to users. The system should recover automatically when possible, and when it cannot, it should present a friendly, actionable response.

## Risk/Reward Analysis

> Per `AGENT_READ_FIRST.md` §4.4 — required before implementation of changes touching the
> query pipeline. This section captures the trade-off decision for the changes below.

- **Change:** Replace raw implementation-specific error messages with user-friendly,
  categorized messages across all 10 error touchpoints. Add retry-strategy improvements
  (exponential backoff, better `isRetryableError` classification). Raw errors are
  preserved in the `queries.error_message` column and visible via the tech-details
  toggle — only the user-facing chat bubble changes.
- **Fundamental goal impact:**
  - **Phase 1 (message formatting):** No impact on answer accuracy — purely cosmetic.
    Slight improvement to answer *delivery reliability*, since clear actionable messages
    reduce user confusion and increase retry success.
  - **Phase 2 (retry logic changes):** Moderate answer-delivery risk. Reclassifying
    errors (`dial` moving from fatal→retryable) could add latency for scenarios that
    were failing fast before; flipping the `isRetryableError` default from `true` to
    `false` could cause legitimate-correctable errors to fail fast. Both changes must
    be tested against real databases and LLM providers before shipping.
- **Risk categories:** User trust (primary — this is the justification), Answer
  delivery (secondary for Phase 2), Data safety (none — no new code paths touch
  data sources).
- **Failure modes:**
  1. Friendly messages obscure the real cause, frustrating advanced users during
     debugging. *Mitigated:* raw error preserved in `UpdateQueryStatus` and visible
     behind the tech-details toggle (already in place — no new mechanism needed).
  2. `dial` reclassified as retryable → network-down scenarios retry up to
     `maxFinalRetries` times before failing, adding latency. *Mitigated:* exponential
     backoff caps total wait; the user sees a progress indicator.
  3. Default flipped to `false` → a new SQL error that doesn't match any
     `retryablePatterns` fails fast even though a retry with LLM correction might
     have succeeded. *Mitigated:* the existing `retryablePatterns` list is comprehensive;
     any gap can be added as a follow-up after observing production error patterns.
- **Reward:** Every user who encounters an error gets a clear, actionable response
  instead of raw implementation strings. Trust improves measurably — the app feels
  polished, not like it "leaked internals." Transient-network retry can recover from
  blips that currently show an error.
- **Decision:** Proceed with Phase 1 immediately (cosmetic-only, low risk, clears a
  known UX gap). Proceed with Phase 2 — the `isRetryableError` classification changes
  and default-flip (`true`→`false`) will be manually verified against all nine
  supported database drivers before release. Proceed with Phase 3 (`error_category`
  column) as an additive migration alongside Phase 1 since it requires no behavioral
  changes and is safe to deploy.

---

## Problem Statement

When an LLM produces the same SQL query twice in a row (or other LLM misbehaviors), the system currently:

1. Throws a raw error: `"LLM produced the same query (SELECT ...) twice in a row; cannot make further progress"`
2. Passes it through `renderSQLError()` which formats it as: `"I tried to execute the SQL query but encountered an error: [sql] **Error**: [raw error]"`
3. Displays this directly to the user in the conversation

This exposes internal implementation details (the "twice in a row" detection) to the user and provides no guidance on what to do next.

### Other Error Cases Currently Exposed to Users

| Error Type | Current Behavior |
|---|---|
| LLM produces same query twice | Raw error message shown |
| LLM request timeout/failure | `"I encountered an error: [raw error]"` (from defer) |
| SQL syntax error (non-retryable) | Raw SQL error shown |
| Connection failure (non-retryable) | Raw connection error shown |
| Schema fetch failure | `"Failed to fetch database schema: [error]"` |
| LLM client creation failure | `"Failed to create LLM client: [error]"` |
| No DB connection | `"Cannot execute SQL without a database connection"` |
| LLM returns non-sql_query action (error path) | `"[action]: [error]"` (e.g., `"clarification: context cancelled"`) |

## Architecture Overview

### Current Flow

```
ProcessUserMessage()
  ├── Get conversation + history
  ├── Create query record
  ├── Fetch schema
  ├── Create LLM client
  ├── Exploration loop (up to maxRounds)
  │   └── LLM → action → executeFinalQueryWithRetry()
  └── Final query attempt
      └── executeFinalQueryWithRetry()
          ├── Retry loop (up to maxRetries)
          │   ├── LLM retry prompt on failure
          │   ├── Parse response
          │   ├── Check for same query (line 661)
          │   ├── Execute SQL
          │   └── isRetryableError() check
          └── renderSQLError() on failure
```

### Key Components

| Component | File | Lines | Role |
|---|---|---|---|
| `ProcessUserMessage()` | `discussion_engine.go` | 106–460 | Main orchestrator |
| `executeFinalQueryWithRetry()` | `discussion_engine.go` | 601–691 | SQL retry loop |
| `renderSQLError()` | `discussion_engine.go` | 1152–1172 | Error → user message |
| `isRetryableError()` | `sql_execution.go` | 562–630 | Determines if SQL error is retryable |
| `UpdateQueryStatus()` | `pkg/services/query.go` | 88 | Persists status to DB |
| `CreateConversationMessage()` | `pkg/services/conversation.go` | 316 | Creates assistant message |

## Error Handling Touchpoints

### Touchpoint 1: Same-Query Detection (Line 661)

**Current code:**
```go
if resp.SQLQuery == lastSQL {
    lastErr = fmt.Errorf("LLM produced the same query (%s) twice in a row; cannot make further progress", truncateString(resp.SQLQuery, 100))
    break
}
```

**Issues:**
- Error message is implementation-specific ("twice in a row")
- No graceful degradation path
- No user-friendly message rendered

**Resolved design (chosen):** Detect same-query and render a friendly message immediately — do not add an extra LLM round-trip. Rationale: the LLM already proved it's stuck by repeating the query; an additional call adds latency, cost, and complexity for a low-probability recovery.

```go
if resp.SQLQuery == lastSQL {
    friendlyMsg := "I'm having trouble generating a new query. Could you try rephrasing your question?"
    llmContentJSON, _ := json.Marshal(resp)
    llmContent := string(llmContentJSON)
    llmContentPtr := &llmContent
    _, _ = CreateConversationMessage(conversation.ID, "assistant", friendlyMsg, llmContentPtr, nil, nil)
    _ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr("query loop detected"), nil, nil, nil)
    return
}
```

**Why not the LLM-retry option:** The LLM already repeated the same query — it doesn't have new information that would change its output. Sending another prompt burns a costly API call with low expected value. If the user rephrases their question (as the friendly message suggests), the LLM will have genuinely new input to work with.

### Touchpoint 2: `renderSQLError()` (Line 1152)

**Current code:**
```go
func renderSQLError(query *models.Query, resp LLMResponse, dbConnection *models.DataSource, conversationID uint, explorationResults []ExplorationResult, lastErr error) {
    if lastErr != nil {
        _ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr(lastErr.Error()), nil, nil, nil)
    }
    var sqlBlock string
    if resp.SQLQuery != "" {
        sqlBlock = fmt.Sprintf("\n```sql\n%s\n```", resp.SQLQuery)
    }
    errorMsg := fmt.Sprintf("I tried to execute the SQL query but encountered an error:%s\n\n**Error**: %s", sqlBlock, lastErr.Error())
    // ...
}
```

**Issues:**
- Always shows the raw SQL and raw error — both are implementation details
- No error categorization (transient vs. fatal)
- No retry guidance or alternative suggestions

**Proposed fix:**

```go
func renderSQLError(query *models.Query, resp LLMResponse, dbConnection *models.DataSource, conversationID uint, explorationResults []ExplorationResult, lastErr error) {
    if lastErr != nil {
        _ = UpdateQueryStatus(query.ID, "error", &resp.SQLQuery, nil, stringPtr(lastErr.Error()), nil, nil, nil)
    }

    // Use the shared error formatter — same function used by Touchpoints 5 and 8.
    msg := formatUserError(lastErr)

    llmContentJSON, _ := json.Marshal(resp)
    llmContent := string(llmContentJSON)
    llmContentPtr := &llmContent
    _, _ = CreateConversationMessage(conversationID, "assistant", msg, llmContentPtr, nil, nil)
}
```

**Shared error formatting helper** (for Touchpoints 2, 5, and 8):

```go
// formatUserError maps an error to a user-friendly message.
// Raw error details are preserved in UpdateQueryStatus (for debugging) —
// this function produces only the text shown in the chat bubble.
func formatUserError(err error) string {
    if err == nil {
        return "I encountered an unexpected issue. Please try again."
    }
    msg := err.Error()
    lower := strings.ToLower(msg)

    // Auth / permission errors (both LLM API keys and DB credentials)
    if strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized") ||
       strings.Contains(lower, "forbidden") || strings.Contains(lower, "authentication") ||
       strings.Contains(lower, "access denied") || strings.Contains(lower, "permission denied") {
        return "Authentication failed. Please check your API key and connection credentials in Settings."
    }

    // Rate limiting (LLM APIs)
    if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") {
        return "The AI model is receiving too many requests right now. Please wait a moment and try again."
    }

    // Schema / connection setup errors
    if strings.Contains(lower, "no database connection") || strings.Contains(lower, "cannot execute sql without") {
        return "No database connection configured. Please add a data source in Settings."
    }

    // Transient connection problems
    if strings.Contains(lower, "dial") || strings.Contains(lower, "connection refused") ||
       strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "connection reset") ||
       strings.Contains(lower, "no such host") || strings.Contains(lower, "tls") {
        return "I'm having trouble connecting to the database. Please check your connection settings and try again."
    }

    // Timeouts (LLM or DB)
    if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
        return "The request is taking longer than expected. Please try again."
    }

    // SQL / LLM-generated errors (the LLM produced a bad query)
    if strings.Contains(lower, "syntax error") || strings.Contains(lower, "unknown column") ||
       strings.Contains(lower, "unknown table") || strings.Contains(lower, "doesn't exist") ||
       strings.Contains(lower, "you have an error in your") {
        return "I had trouble understanding your question. Could you try rephrasing it?"
    }

    // Query loop detection (same query repeated — from Touchpoint 1)
    if strings.Contains(lower, "twice in a row") || strings.Contains(lower, "loop detected") {
        return "I'm having trouble generating a new query. Could you try rephrasing your question?"
    }

    // Schema fetch failure (from Touchpoint 6)
    if strings.Contains(lower, "failed to fetch database schema") || strings.Contains(lower, "unable to load database schema") {
        return "Unable to load your database schema. Please check your connection settings."
    }

    // Fallback — generic, no raw error exposed
    return "I encountered an issue processing your request. Please try again or rephrase your question."
}
```

**Design note:** This single `formatUserError` function replaces the separate
`categorizeAndFormatError` and `formatLLMError` functions from the original draft.
Touchpoints 2 (renderSQLError), 5 (defer handler), and 8 (LLM request failure)
all call this one shared helper. Touchpoints 6, 7, and 9 still hardcode their
`UpdateQueryStatus` message directly since they fire before the error reaches the
defer handler, but they use the same human-readable strings for consistency.

**Important:** The original draft's `categorizeAndFormatError` is subsumed by
`formatUserError` above. The original draft's `formatLLMError` (Touchpoint 8) is
also subsumed. Only one error-formatter exists in the final implementation.

### Touchpoint 3: `isRetryableError()` (Line 562)

**Current code:**
```go
func isRetryableError(err error) bool {
    msg := err.Error()
    fatalPatterns := []string{
        "dial", "handshake", "authentication", "max connections",
        "connection reset", "i/o timeout", "connection refused",
        "no such host", "tls:", "certificate",
    }
    retryablePatterns := []string{
        "unknown column", "unknown table", "doesn't exist", "syntax error",
        // ... many more patterns
    }
    // ...
}
```

**Issues:**
- Pattern matching is fragile and incomplete
- Some "fatal" patterns (like `dial`) are actually transient network errors that should be retried
- Some "retryable" patterns (like `unknown column`) indicate the LLM is stuck and retrying won't help
- No exponential backoff

**Proposed fix (concrete, resolving the `dial` contradiction):**

```go
func isRetryableError(err error) bool {
    msg := err.Error()
    upper := strings.ToUpper(msg)

    // 1. PERMANENTLY FATAL — retrying will never help.
    //    These are auth/permission failures that won't change on retry.
    permanentlyFatal := []string{
        "AUTHENTICATION", "ACCESS DENIED", "PERMISSION DENIED",
        "INVALID PASSWORD", "INVALID API KEY", "COMMAND DENIED",
        "UNAUTHORIZED", "FORBIDDEN",
    }
    for _, pat := range permanentlyFatal {
        if strings.Contains(upper, pat) {
            return false
        }
    }

    // 2. LLM-CORRECTABLE — the LLM can produce a better query.
    //    (Preserved from the existing retryablePatterns list — same logic, just
    //     re-organized into a named category.)
    llmCorrectable := []string{
        "UNKNOWN COLUMN", "UNKNOWN TABLE", "DOESN'T EXIST", "SYNTAX ERROR",
        "YOU HAVE AN ERROR IN YOUR", "TRUNCATED INCORRECT", "INCORRECT STRING VALUE",
        "INVALID USE OF GROUP", "AMBIGUOUS COLUMN", "MULTIPLE PRIMARY KEY",
        "TABLE IS MARKED AS CRASHED", "INCORRECT KEY VALUE", "DATA TOO LONG",
        "OUT OF RANGE", "DIVISION BY ZERO",
        "SUBQUERY RETURNS MORE THAN 1 ROW", "1242",
        "INVALID CHARACTER", "INVALID UTF8", "INVALID UTF8MB4",
        "FIELD DOESN'T HAVE", "NOT FOUND", "NOT EXISTS",
        "FUNCTION DOESN'T EXIST", "COLUMN '.*' IN", "NOT IN GROUP BY",
        "INVALID REFERENCE", "CONFLICTING TYPES", "CAN'T DROP",
        "DUPLICATE ENTRY", "FOREIGN KEY CONSTRAINT", "CANNOT ADD FOREIGN KEY",
        "CANNOT TRUNCATE", "VIEW'S", "STORED FUNCTION", "PREPARED STATEMENT",
        "INVALID COLLATION", "INCORRECT DATE VALUE", "INCORRECT DATETIME VALUE",
        "INCORRECT TIME VALUE", "INCORRECT YEAR VALUE", "INCORRECT DOUBLE VALUE",
        "OVERFLOW", "UNDERFLOW", "TRUNCATED", "OUT OF MEMORY",
        "TEMPORARY FILE", "DISK FULL",
    }
    for _, pat := range llmCorrectable {
        if regexp.MustCompile("(?i)" + pat).MatchString(msg) {
            return true
        }
    }

    // 3. TRANSIENT CONNECTION — retryable, but add backoff.
    //    These were in fatalPatterns before. They're now retryable because
    //    they represent temporary network/connection blips, not permanent
    //    failures. See Phase 2 backoff implementation below.
    transientConnection := []string{
        "DIAL", "HANDSHAKE", "CONNECTION RESET", "I/O TIMEOUT",
        "CONNECTION REFUSED", "NO SUCH HOST", "TLS:", "CERTIFICATE",
        "MAX CONNECTIONS", "TOO MANY CONNECTIONS", "DEADLOCK",
        "LOCK WAIT TIMEOUT",
    }
    for _, pat := range transientConnection {
        if strings.Contains(upper, pat) {
            return true
        }
    }

    // 4. DEFAULT — don't retry unknown errors.
    //    This flips from `true` to `false` (a behavior change). If the error
    //    doesn't match any known category, assume it's fatal rather than
    //    wasting cycles retrying. This is safer: unknown errors are more
    //    likely to be genuine bugs or infrastructure problems.
    return false
}
```

**Exponential-backoff helper for transient connection retries:**

```go
import "math"

// backoffDuration returns an exponential backoff duration with jitter.
// base = 500ms, max = 15s. Used before retrying transient connection errors.
func backoffDuration(attempt int) time.Duration {
    base := 500 * time.Millisecond
    max := 15 * time.Second
    backoff := time.Duration(float64(base) * math.Pow(2, float64(attempt)))
    if backoff > max {
        backoff = max
    }
    // Add ±25% jitter
    jitter := time.Duration(float64(backoff) * 0.5 * (rand.Float64() - 0.5))
    return backoff + jitter
}
```

Apply the backoff in `executeFinalQueryWithRetry` before each transient retry
by calling `time.Sleep(backoffDuration(attempt))` after `isRetryableError`
returns true for a transient-connection pattern. This is additive — the existing
retry loop structure doesn't change, only a sleep is inserted before the LLM
retry prompt is appended.

**Key behavioral changes and why:**

| Change | Before | After | Rationale |
|---|---|---|---|
| `dial` | fatal (no retry) | retryable with backoff | Transient network blip — retrying usually succeeds |
| `i/o timeout` | fatal | retryable with backoff | Same as above |
| Default unknown error | retryable (`return true`) | fatal (`return false`) | Safer — don't hammer unfamiliar errors |
| Backoff | none | exponential with jitter | Prevents thundering herd on connection recovery |

### Touchpoint 4: LLM Retry Prompt (Line 614)

**Current code:**
```go
llmMessages = append(llmMessages, ChatMessage{
    Role: "system",
    Content: fmt.Sprintf("The previous SQL query failed:\n\n```sql\n%s\n```\n\nError: %s\n\nPlease correct the query and respond with a new 'sql_query' action.", lastSQL, lastErr.Error()),
})
```

**Issues:**
- Error message may contain implementation details
- No guidance on how to fix the error
- Same error may produce same response (infinite loop)

**Proposed fix:**

```go
llmMessages = append(llmMessages, ChatMessage{
    Role: "system",
    Content: fmt.Sprintf(
        "The previous SQL query failed. Error details (for your reference; do NOT include these in your user-facing response):\n\n"+
        "```sql\n%s\n```\n\n"+
        "Error: %s\n\n"+
        "Please correct the query and respond with a new 'sql_query' action. "+
        "Try a different query structure, avoid the pattern that caused the error, "+
        "and if you're unsure, provide a simpler query.",
        lastSQL, lastErr.Error(),
    ),
})
```

**Design decision — keep the error details, improve the guidance:** The original
draft proposed stripping the SQL and error text from this message entirely.
That would **cripple the LLM's ability to self-correct.** This is a system
message to the LLM, not user-visible content — the LLM needs the actual failing
query and error to know what to fix. The user-facing error formatting is handled
separately by `formatUserError` (Touchpoint 2). The only change here is adding
more specific guidance on *how* to correct the query, and a note telling the LLM
not to regurgitate the error in its user-facing response.

### Touchpoint 5: `ProcessUserMessage()` Deferred Error Handler (Line 135)

**Current code:**
```go
defer func() {
    if r := recover(); r != nil {
        err = fmt.Errorf("internal error: %v", r)
    }
    if err != nil && !assistantMessageSaved {
        errorMsg := fmt.Sprintf("I encountered an error: %s", err.Error())
        _, _ = CreateConversationMessage(conversationID, "assistant", errorMsg, nil, nil, nil)
    }
}()
```

**Issues:**
- Raw panic message exposed to user
- Generic "I encountered an error" provides no guidance
- No differentiation between panics and normal errors

**Proposed fix:**

```go
defer func() {
    if r := recover(); r != nil {
        err = fmt.Errorf("internal error: %v", r)
        log.Printf("[DiscussionEngine] Panic recovered: %v", r)
    }
    if err != nil && !assistantMessageSaved {
        // Use the shared formatter; it falls through to the generic message
        // for panics since the error string won't match any specific pattern.
        friendlyMsg := formatUserError(err)
        _, _ = CreateConversationMessage(conversationID, "assistant", friendlyMsg, nil, nil, nil)
    }
}()
```

**Design note:** Calling `formatUserError` here means panics and unrecognized errors
both produce the same consistent fallback message ("I encountered an issue processing
your request. Please try again or rephrase your question."). The recover path still
logs the full panic detail to the Go logger for debugging.

### Touchpoint 6: Schema Fetch Failure (Line 186)

**Current code:**
```go
if err != nil {
    log.Printf("Failed to fetch schema for %s: %v", dbConnection.Type, err)
    _ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to fetch database schema: %v", err)), nil, nil, nil)
} else if schema != nil {
    log.Printf("Fetched schema: %d tables", len(schema.Tables))
}
```

**Issues:**
- Raw error is stored in `UpdateQueryStatus` and also propagates as the named
  `err` return value, triggering the defer handler with a raw-error chat message.
- Execution continues with `schema == nil` — the LLM cannot produce accurate
  SQL without schema context, so continuing is a degraded experience.

**Proposed fix — abort, don't continue:**

```go
if err != nil {
    log.Printf("[DiscussionEngine] Failed to fetch schema for %s: %v", dbConnection.Type, err)
    return fmt.Errorf("failed to fetch database schema: %w", err)
}
log.Printf("Fetched schema: %d tables", len(schema.Tables))
```

The returned error propagates to the defer handler, which calls `formatUserError(err)`.
Because the error string contains `"failed to fetch database schema"`, the shared
formatter catches the `if strings.Contains(lower, "failed to fetch database schema")`
case and produces: `"Unable to load your database schema. Please check your connection
settings."`

**No raw error reaches the user.** The abort happens before any conversation message
is written (`assistantMessageSaved` stays `false`), the defer handler generates one
clean chat bubble, and the raw error is logged for debugging.

> **Behavior change:** Previously, a schema-fetch failure continued silently and let
> the LLM attempt a query without context (often producing garbage). Now it aborts
> immediately with a clear message.

### Touchpoint 7: LLM Client Creation Failure (Line 228)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to create LLM client: %v", err)), nil, nil, nil)
```

**Proposed fix:** Use `formatUserError` to keep all user-facing error messages aligned:

```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
```

The error returned from this block (`return fmt.Errorf("failed to create LLM client: %w", err)`)
triggers the defer handler, which also calls `formatUserError`. The user sees one consistent
message (typically `"Authentication failed. Please check your API key and connection
credentials in Settings."` from the auth path).

### Touchpoint 8: LLM Request Failure (Lines 252, 402)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("LLM request failed: %v", err)), nil, nil, nil)
```

**Proposed fix:** Use the shared `formatUserError` helper (defined in Touchpoint 2).

```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(formatUserError(err)), nil, nil, nil)
```

No separate `formatLLMError` function is needed — `formatUserError` already handles
timeout, API key, and rate-limit patterns. The raw error is still available in
`err.Error()` for the `UpdateQueryStatus` call's error_message column if desired,
but the user-facing text is clean.

### Touchpoint 9: No DB Connection (Lines 301, 434)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Cannot execute SQL without a database connection"), nil, nil, nil)
```

**Status:** Already reasonably user-friendly. Could be improved to suggest action:
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("No database connection configured. Please add a data source in Settings."), nil, nil, nil)
```

### Touchpoint 10: Non-sql_query Action on Error Path (Line 653)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", &lastSQL, nil, stringPtr(fmt.Sprintf("%s: %v", newResp.Action, lastErr)), nil, nil, nil)
_, _ = CreateConversationMessage(conversationID, "assistant", displayMsg, llmContentPtr, nil, nil)
```

**Issues:**
- `displayMsg` may contain raw error details
- `clarification` action with error context is confusing

**Proposed fix:**
```go
var displayMsg string
switch newResp.Action {
case "clarification":
    displayMsg = newResp.ClarificationQuestion
case "answer":
    displayMsg = newResp.Explanation
default:
    displayMsg = "I'm not sure how to help with that. Could you try rephrasing your question?"
}
```

## Proposed Error Categories

| Category | Trigger | User Message | Retry? | `error_category` |
|---|---|---|---|---|
| **Auth Failure** | API key invalid, unauthorized, access denied | "Authentication failed. Please check your API key and connection credentials in Settings." | No | `auth_failure` |
| **Rate Limit** | Too many requests | "The AI model is receiving too many requests right now. Please wait a moment and try again." | Yes (delayed) | `rate_limit` |
| **Transient Network** | Dial error, connection refused, i/o timeout, TLS | "I'm having trouble connecting to the database. Please check your connection settings and try again." | Yes (with backoff) | `transient_network` |
| **LLM Timeout** | Deadline exceeded, timeout | "The request is taking longer than expected. Please try again." | No | `llm_timeout` |
| **SQL Syntax / Bad Query** | Unknown column, syntax error, it doesn't exist | "I had trouble understanding your question. Could you try rephrasing it?" | Prompt retry | `bad_query` |
| **Query Loop** | Same query twice | "I'm having trouble generating a new query. Could you rephrase?" | No | `query_loop` |
| **Schema Failure** | Cannot fetch schema | "Unable to load your database schema. Please check your connection settings." | No | `schema_failure` |
| **No DB Connection** | No data source configured | "No database connection configured. Please add a data source in Settings." | No | `no_db_connection` |
| **Internal Error** | Panic, unexpected, unrecognized error | "I encountered an issue processing your request. Please try again or rephrase your question." | No | `internal_error` |

All messages produced by the single `formatUserError` helper (Touchpoint 2).
The "Retry?" column here describes the *backend retry behavior* — not a user prompt.

## Implementation Plan

### Phase 1: User-Facing Error Messages (Low Risk — Ready Now)

1. Create a single `formatUserError(err error) string` helper in `discussion_engine.go`
   (the shared formatter defined in Touchpoint 2).
2. Update `renderSQLError()` to use it (Touchpoint 2).
3. Update `ProcessUserMessage()` defer handler to use it (Touchpoint 5).
4. Update LLM request-failure `UpdateQueryStatus` calls to use it (Touchpoint 8).
5. Update schema-fetch-failure `UpdateQueryStatus` to use a hardcoded friendly string (Touchpoint 6).
6. Update LLM-client-creation-failure `UpdateQueryStatus` (Touchpoint 7).
7. Update no-DB-connection `UpdateQueryStatus` to suggest adding a data source (Touchpoint 9).
8. Apply the same-query-loop friendly-message fix (Touchpoint 1).
9. Apply the non-sql_query-action display-message cleanup (Touchpoint 10).
10. Raw errors remain in `error_message` for debugging — only the user-facing text changes.

**Files touched:** `pkg/services/discussion_engine.go` (~20 changes, all additive/substitution).
**Risk:** None to answer accuracy. No behavior change. Rollback is trivial.

### Phase 2: Retry Strategy Improvement (Medium Risk — Ready, Manual Testing Required)

1. Replace `isRetryableError()` with the three-category classification (Touchpoint 3):
   - `permanentlyFatal` — auth/permission errors, never retry.
   - `llmCorrectable` — SQL errors the LLM can fix, retry with prompt correction.
   - `transientConnection` — network/connection blips, retry with exponential backoff.
   - Default: `return false` (safer — if we don't recognize the error, don't hammer).
2. Add `backoffDuration()` helper with exponential backoff + jitter.
3. Insert `time.Sleep(backoffDuration(attempt))` in `executeFinalQueryWithRetry` before
   each retry that's classified as `transientConnection`.
4. Improve the LLM retry prompt with correction guidance (Touchpoint 4).

**Files touched:** `pkg/services/sql_execution.go` (rewrite `isRetryableError`),
`pkg/services/discussion_engine.go` (~10 lines: backoff import + sleep call + prompt tweak).
**Testing:** Manual verification against all nine supported database drivers before release.
The default-flip (`true`→`false`) and the `dial` reclassification are the highest-leverage
decisions — verify with network-down and intentionally-bad-query scenarios.

### Phase 3: Error Tracking & Analytics (Ready Now — Additive Migration)

1. Add `error_category` column to the `queries` table via `ensureColumn`:
   ```go
   ensureColumn("queries", "error_category", "TEXT DEFAULT ''")
   ```
2. Add the field to the `Query` model in `pkg/models/query.go`:
   ```go
   ErrorCategory *string `json:"error_category,omitempty"`
   ```
3. Populate `error_category` in all `UpdateQueryStatus` call sites using a
   `classifyErrorCategory(err)` helper that maps errors to one of the categories
   from the **Proposed Error Categories** table (e.g., `"auth_failure"`,
   `"transient_network"`, `"bad_query"`, `"schema_failure"`, `"internal_error"`).
4. Update `UpdateQueryStatus` signature to accept an optional `errorCategory` parameter
   — or add a separate `UpdateQueryErrorCategory` function to avoid signature churn.

**Files touched:** `pkg/models/database.go` (migration), `pkg/models/query.go` (model),
`pkg/services/query.go` (populate on write), `pkg/services/discussion_engine.go`
(`classifyErrorCategory` helper).

**No Settings UI changes in this phase** — the category is stored but not yet exposed
in the frontend. A future phase can add the stats view to `SettingsView.svelte`.

> **Migration safety:** `ensureColumn` is additive-only. Existing rows get an empty
> string default — backward-compatible.

## Testing Strategy

| Test Case | Expected Result |
|---|---|
| LLM produces same query twice | Friendly message, no raw error shown |
| SQL syntax error | Generic "I had trouble understanding" message |
| Connection timeout | "Having trouble connecting" message |
| Invalid API key | "Authentication failed" message |
| Schema fetch fails | Friendly message, flow aborts (no query attempted) |
| LLM returns 503 | "Model unavailable" message |
| Panic in discussion engine | "Unexpected issue" message, no spinner stuck |
| No DB connection | "Add a data source" message |

## Edge Cases

1. **Multiple concurrent queries** — Error messages should be conversation-scoped
2. **Rapid retries** — Add jitter to backoff to avoid thundering herd
3. **Long-running queries** — Timeout should be configurable
4. **Error message length** — Truncate to ~200 characters for UI display
5. **Markdown rendering** — Ensure error messages don't contain unescaped markdown
6. **Database content compatibility** — Raw errors remain in DB for debugging

## Rollback Plan

All Phase 1 changes are additive — no breaking changes to:
- Query record schema
- Conversation message format
- API surface
- User data

If issues arise, revert `formatUserError()` and the touchpoint call sites to restore
original behavior. Each touchpoint is independently revertible.

Phase 2 changes (retry classification) are also additive but the behavioral change
(`isRetryableError` default-flip) should be tested against all nine drivers before
final approval. Rollback: restore the original `isRetryableError` function and remove
the backoff sleep.

---

## Related Files

| File | Role |
|---|---|
| `pkg/services/discussion_engine.go` | Main orchestrator, error paths |
| `pkg/services/sql_execution.go` | `isRetryableError()`, SQL execution |
| `pkg/models/query.go` | Query record schema |
| `pkg/services/query.go` | `UpdateQueryStatus()` |
| `pkg/services/conversation.go` | `CreateConversationMessage()` |
| `frontend/src/ConversationView.svelte` | Displays error messages |
| `frontend/src/SettingsView.svelte` | Shows error status in data source list |

---

## Implementation Status (2026-08-03)

All three phases from the original plan have been implemented, plus additional
fixes discovered during testing and iteration.

### Phase 1: User-Facing Error Messages — ✅ Complete

| Touchpoint | Status | Notes |
|---|---|---|
| T1 — Same-query detection | ✅ | Friendly message + return instead of raw error + break |
| T2 — `renderSQLError` | ✅ | Uses shared `formatUserError` helper |
| T4 — LLM retry prompt | ✅ | Assertive language + keeps error details for LLM self-correction |
| T5 — Defer handler | ✅ | `formatUserError(err)` + panic logging |
| T6 — Schema fetch failure | ✅ | **Aborts immediately** instead of continuing with nil schema |
| T7 — LLM client creation | ✅ | Uses `formatUserError` |
| T8 — LLM request failures | ✅ | Both exploration and final-attempt use `formatUserError` |
| T9 — No DB connection | ✅ | Improved message + `error_category` tracking |
| T10 — Non-sql_query display | ✅ | Switch-based, no raw action names in messages |
| — `formatUserError(err)` | ✅ | Single shared error formatter for all user-facing messages |
| — `classifyErrorCategory(err)` | ✅ | Maps errors to stable category strings |
| — Frontend raw error display | ✅ | `⚠ Raw Error Details` toggle in tech-details panel |

### Phase 2: Retry Strategy Improvement — ✅ Complete

| Change | Status | Notes |
|---|---|---|
| `isRetryableError` rewrite | ✅ | Three-category classification: permanentlyFatal, looksLikeSQLError, transientConnection |
| `maxFinalRetries` increased | ✅ | Default 1 → 2 (three total attempts) |
| `backoffDuration` helper | ✅ | Exponential backoff 500ms–15s with ±25% jitter |
| Backoff sleep in retry loop | ✅ | Applied after retryable errors |
| Retry-once for unknowns | ✅ | Unknown non-auth errors get one LLM correction attempt |
| Assertive retry prompt | ✅ | "REJECTED" language, strip failing message from LLM context |
| Dialect hint in retry prompt | ✅ | Injects driver `SQLDialectHint()` into correction message |

### Phase 3: Error Tracking — ✅ Complete

| Change | Status | Notes |
|---|---|---|
| `error_category` column | ✅ | Additive `ensureColumn` migration |
| `ErrorCategory` model field | ✅ | Added to `Query` struct |
| `UpdateQueryErrorCategory` | ✅ | Separate function to avoid signature churn |
| `classifyErrorCategory` helper | ✅ | Wired at all 9 error touchpoints |

---

## Implemented Beyond Original Scope

During testing and iteration, several additional fixes were made that go beyond
what the original enhancement document specified. These were driven by real
failures observed with gemma-4-e4b-it against a MySQL database.

### Pattern-Matching Gap: "does not exist" vs "doesn't exist"

**Problem:** MySQL errors use `"FUNCTION classicmodels.strftime does not exist"`
(expanded form), not `"doesn't exist"` (contracted form). The original
`isRetryableError`, `formatUserError`, and `classifyErrorCategory` all matched
only `"doesn't exist"`, so MySQL errors fell through to the default path.

**Fix:** Added `"does not exist"` as a parallel pattern in all three functions.
This was then superseded by the broader `looksLikeSQLError` structural matching.

### Broad Structural Error Matching (`looksLikeSQLError`)

**Problem:** The original `isRetryableError` had 40+ driver-specific patterns
covering only MySQL error text. PostgreSQL, SQLite, SQL Server, and other
drivers produce different error formats for the same logical failure.

**Fix:** Replaced 40+ fragile patterns with `looksLikeSQLError()` — a structural
classifier that matches any error containing:
- A database error code (`Error 1305`, `SQLSTATE 42000`, `Msg 208`, etc.) — regex `(?i)(error|msg|sqlstate)\s+\d+`
- SQL-related keywords (`COLUMN`, `TABLE`, `FUNCTION`, `SYNTAX`, `DOES NOT EXIST`, `DUPLICATE`, `FOREIGN KEY`, etc.)

This works across MySQL, PostgreSQL, SQLite, SQL Server, Snowflake, BigQuery,
Redshift, and MariaDB without driver-specific patterns.

### Conversation History Contamination

**Problem:** When the LLM produced a failing query twice, the system retried,
but the LLM's context window contained its own failed query as a prior
assistant message. The model treated its wrong answer as established context
and repeated it despite the correction prompt. Observed with `strftime` being
repeated 3+ times in a row despite explicit correction instructions.

**Fix:** Before sending the correction prompt, the retry loop now strips the
most recent assistant message from the LLM context if it contains the failing
SQL. This prevents the model from seeing its rejected answer as valid context.

### Retry-on-Unknown Safety Net

**Problem:** The `isRetryableError` default was `return false`, meaning any
error that didn't match a known pattern would fail immediately. This could
cause correctable errors from unsupported drivers to fail without giving the
LLM a chance to self-correct.

**Fix:** In `executeFinalQueryWithRetry`, when `isRetryableError` returns false
on the first attempt and the error is not auth-related, the system gives the
LLM one correction round anyway. Subsequent failures on the same error still
break immediately.

### Lenient JSON Parsing for Unescaped Quotes

**Problem:** LLMs frequently produce JSON with unescaped double quotes inside
string values (e.g., `"he said "hello""` instead of `"he said \"hello\""`)
when generating `clarification_question` or `answer` text. Go's strict
`json.Unmarshal` rejects this, and the original code showed the user a raw
parse error: `"invalid character 't' after object key:value pair"`.

**Fix:** Added `parseLenientJSON` — a boundary-based fallback parser that
activates when strict parsing fails on a `{`-prefixed response. It extracts the
`action` field and the main content field by finding the *last* `"}` or `","`
boundary after the value, which naturally skips over unescaped inner quotes.

### JSON Parse Error Retry

**Problem:** If even lenient parsing failed, the raw parse error was stored as
a `clarification` message and permanently burned into the conversation history.
On subsequent turns, the LLM saw messages like *"The LLM returned invalid
JSON..."* as if it were its own prior utterance.

**Fix:** Added a `_parse_error` pseudo-action. When neither strict nor lenient
parsing succeeds, the system retries with the LLM (asking it to fix its JSON
formatting with explicit escaping instructions). If retries are exhausted, the
user sees *"I received a response I couldn't understand. Could you try
rephrasing your question?"* — never raw JSON or parse error codes.

### Error Message Type Filtering (is_error)

**Problem:** Error messages stored in `conversation_messages` as `role: "assistant"`
were sent to the LLM on every subsequent turn. The model saw its own errors
repeated in the context window, which could compound confusion over long
conversations.

**Fix:** Error messages are now stamped with `"is_error": true` in their
metadata. `buildLlmMessages` calls `isErrorMessage()` on each assistant message
and skips any flagged as errors. The user sees them in the chat UI, but the
LLM's context window stays clean.


---

## Possible Enhancements (Strategies from Other Harnesses)

These are deferred ideas drawn from how other AI data tools and LLM harnesses
handle error recovery. None are implemented. They're sorted by estimated
impact — the first two would have prevented the most frequently observed errors
during testing (wrong-dialect functions and hallucinated columns).

### 1. Dialect Function Whitelisting in System Prompt (High Impact, Low Cost)

**Source:** LangChain SQL agent, Databricks text-to-SQL

**Problem it solves:** The LLM generates SQLite functions (`strftime`) for MySQL
databases, PostgreSQL functions for SQLite, etc. The system prompt says *"must
be compatible with MySQL"* but doesn't tell the model *what MySQL functions
actually exist.*

**Proposed approach:** Each driver already has `SQLDialectHint()`. Expand the
driver interface to include a `SupportedFunctions()` method listing common
functions. Inject into the system prompt:

> *"You are querying a MySQL database. MySQL date functions: DATE_FORMAT,
> DATE_ADD, DATE_SUB, DATEDIFF, YEAR, MONTH, DAY. Do NOT use: strftime,
> datetime, julianday (SQLite functions), to_char, date_trunc (PostgreSQL
> functions)."*

This is proactive — the model never generates a wrong-dialect function because
it's explicitly told which functions are available and which belong to other
databases.

### 2. Schema Pre-Check Before SQL Execution (High Impact, Medium Cost)

**Source:** DIN-SQL research, various text-to-SQL systems

**Problem it solves:** The LLM hallucinates column names that don't exist, or
references tables with wrong aliases. The query fails at execution time, burning
a retry round that could have been avoided.

**Proposed approach:** Before executing SQL, extract all column and table
references and validate them against the introspected schema. If a reference
doesn't exist, send it back to the LLM with the correct schema before
attempting execution. No database round-trip needed for the validation step.

### 3. EXPLAIN Validation (Medium Impact, Low Cost)

**Source:** PostgreSQL tooling, DBeaver

**Problem it solves:** Syntax errors, type mismatches, and invalid references
that the database catches at parse time. Currently these go through the retry
loop, costing an extra LLM call.

**Proposed approach:** Run `EXPLAIN` on the generated SQL before executing it.
If EXPLAIN fails, the query is syntactically invalid — retry with the LLM
before attempting execution. Most databases support EXPLAIN (MySQL, PostgreSQL,
SQLite). This catches syntax errors without touching data.

### 4. Self-Critique Loop (Medium Impact, High Cost)

**Source:** DIN-SQL, DAIL-SQL research

**Problem it solves:** The LLM generates a query that looks valid but is
semantically wrong (wrong JOIN condition, missing WHERE clause, incorrect
aggregation).

**Proposed approach:** After generating a query, send it to a separate "critic"
prompt: *"Review this SQL query against the schema. Are all column names
correct? Are the JOIN conditions right? Will this answer the user's question?"*
If the critic finds issues, regenerate without executing. Cost: one extra LLM
call per query, but only for complex queries where validation is useful.

### 5. Multi-Shot Generation with Voting (Low Impact, Very High Cost)

**Source:** Databricks, research text-to-SQL systems

**Problem it solves:** Outlier hallucination — the LLM occasionally produces a
completely wrong query that looks valid.

**Proposed approach:** Generate 3 candidate queries per turn. If two agree and
one is radically different, execute the majority. If all three disagree, send
the disagreements back to the LLM. Cost: 3× LLM calls per turn. Only practical
for high-value queries or when using local/free models.

### 6. Progressive Disclosure of Errors (Medium Impact, Medium Cost)

**Source:** LangChain, Claude tool-use pattern

**Problem it solves:** Users see errors too quickly. The LLM often needs 2-3
tries to self-correct, but the user sees a failure message on the first try.

**Proposed approach:** Burn one silent retry before showing the user anything.
First failure: send a stronger correction prompt, don't surface to user. Second
failure: show "Let me try a different approach." Third failure: show the full
friendly error with raw details available in tech details.

### 7. Per-Conversation Error Memory (Medium Impact, Low Cost)

**Source:** Anthropic's conversation summarization pattern

**Problem it solves:** The LLM repeats the same class of error across multiple
turns (e.g., using `strftime` on three different questions in the same
conversation).

**Proposed approach:** After an error is resolved, inject a system note:
*"Previous errors in this conversation: strftime is not available in MySQL —
use DATE_FORMAT. MAX() cannot be used inside DATE_SUB without a subquery."*
The LLM learns within the conversation and stops repeating the same mistake
class. This is an injected skill-like note, not a conversation message.

