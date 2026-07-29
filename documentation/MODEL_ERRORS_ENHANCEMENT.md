# MODEL_ERRORS_ENHANCEMENT.md

## Goal

Improve error handling across the LLM→SQL→Results pipeline so that transient failures, LLM misbehaviors, and SQL execution errors are handled gracefully in the backend — without exposing raw error messages to users. The system should recover automatically when possible, and when it cannot, it should present a friendly, actionable response.

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
| `UpdateQueryStatus()` | `models/database.go` | query status updates | Persists status to DB |
| `CreateConversationMessage()` | `models/database.go` | 800+ | Creates assistant message |

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

**Proposed fix:**
```go
if resp.SQLQuery == lastSQL {
    // Try one more time with a stronger prompt hint
    llmMessages = append(llmMessages, ChatMessage{
        Role: "system",
        Content: fmt.Sprintf("The previous query returned the same result. Please try a different approach or a different query. Do NOT repeat the previous query."),
    })
    
    // Attempt one more LLM call
    responseText, _, _, err := client.ChatCompletionWithPayload(ctx, llmMessages)
    if err != nil {
        lastErr = fmt.Errorf("retry prompt failed")
        break
    }
    
    cleanedResponse := extractJSONFromResponse(responseText)
    newResp, err := parseLLMResponse(cleanedResponse)
    if err != nil || newResp.Action != "sql_query" || newResp.SQLQuery == lastSQL {
        lastErr = fmt.Errorf("query loop detected")
        break
    }
    resp = newResp
    continue
}
```

**Alternative (simpler):** Detect same-query earlier and render a friendly message:
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
    
    // Categorize error for user-friendly messaging
    msg := categorizeAndFormatError(lastErr, resp.SQLQuery)
    
    llmContentJSON, _ := json.Marshal(resp)
    llmContent := string(llmContentJSON)
    llmContentPtr := &llmContent
    _, _ = CreateConversationMessage(conversationID, "assistant", msg, llmContentPtr, nil, nil)
}

func categorizeAndFormatError(err error, sqlQuery string) string {
    msg := err.Error()
    
    // Connection errors
    if strings.Contains(strings.ToLower(msg), "dial") || strings.Contains(strings.ToLower(msg), "connection refused") {
        return "I'm having trouble connecting to the database. Please check your connection settings and try again."
    }
    
    // SQL syntax errors
    if strings.Contains(strings.ToLower(msg), "syntax error") || strings.Contains(strings.ToLower(msg), "unknown column") {
        return "I had trouble understanding your question. Could you try rephrasing it?"
    }
    
    // Query loop detection
    if strings.Contains(msg, "twice in a row") || strings.Contains(msg, "loop detected") {
        return "I'm having trouble generating a new query. Could you try rephrasing your question?"
    }
    
    // Default: generic friendly message, hide raw error
    return "I encountered an issue processing your request. Please try again or rephrase your question."
}
```

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

**Proposed fix:**
1. Separate transient network errors from fatal connection errors
2. Add exponential backoff with jitter
3. Classify SQL errors into categories:
   - **Retryable** (transient): deadlock, lock timeout, too many connections
   - **Should retry with different prompt** (LLM may be stuck): syntax error, unknown column, doesn't exist
   - **Fatal** (no retry useful): connection refused, auth failed, max connections

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
    Content: fmt.Sprintf("The previous query failed. Please try a different approach:\n\n- Use a different query structure\n- Avoid the pattern that caused the error\n- If you're unsure, provide a simpler query\n\nRespond with a new 'sql_query' action."),
})
```

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
        friendlyMsg := "I encountered an unexpected issue. Please try again."
        _, _ = CreateConversationMessage(conversationID, "assistant", friendlyMsg, nil, nil, nil)
    }
}()
```

### Touchpoint 6: Schema Fetch Failure (Line 186)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to fetch database schema: %v", err)), nil, nil, nil)
```

**Issues:**
- Raw error exposed to user
- No fallback (e.g., cached schema, partial schema)

**Proposed fix:**
```go
if err != nil {
    log.Printf("[DiscussionEngine] Failed to fetch schema for %s: %v", dbConnection.Type, err)
    _ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Unable to load database schema. Please check your connection settings."), nil, nil, nil)
    // Continue with empty schema — LLM may still produce queries
}
```

### Touchpoint 7: LLM Client Creation Failure (Line 228)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("Failed to create LLM client: %v", err)), nil, nil, nil)
```

**Proposed fix:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr("Unable to connect to the AI model. Please check your API key and try again."), nil, nil, nil)
```

### Touchpoint 8: LLM Request Failure (Lines 252, 402)

**Current code:**
```go
_ = UpdateQueryStatus(query.ID, "error", nil, nil, stringPtr(fmt.Sprintf("LLM request failed: %v", err)), nil, nil, nil)
```

**Proposed fix:**
```go
func formatLLMError(err error) string {
    msg := strings.ToLower(err.Error())
    if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
        return "The AI model is taking longer than expected. Please try again."
    }
    if strings.Contains(msg, "api key") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") {
        return "Authentication failed. Please check your API key in Settings."
    }
    if strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests") {
        return "Too many requests. Please try again in a moment."
    }
    return "The AI model is temporarily unavailable. Please try again."
}
```

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

| Category | Trigger | User Message | Retry? |
|---|---|---|---|
| **Transient Network** | Connection timeout, dial error | "I'm having trouble connecting. Please try again." | Yes |
| **Auth Failure** | API key invalid, unauthorized | "Authentication failed. Please check your API key." | No |
| **Rate Limit** | Too many requests | "Too many requests. Please try again shortly." | Yes (delayed) |
| **SQL Syntax** | Unknown column, syntax error | "I had trouble understanding your question. Could you rephrase?" | Prompt retry |
| **Query Loop** | Same query twice | "I'm having trouble generating a new query. Could you rephrase?" | No |
| **Schema Failure** | Cannot fetch schema | "Unable to load database schema. Please check your connection." | No |
| **Model Unavailable** | LLM offline, 503 | "The AI model is temporarily unavailable. Please try again." | Yes |
| **Internal Error** | Panic, unexpected state | "I encountered an unexpected issue. Please try again." | No |

## Implementation Plan

### Phase 1: User-Facing Error Messages (Low Risk)

1. Create `formatError(err error) string` helper in `discussion_engine.go`
2. Replace all raw error strings in `renderSQLError()`, `ProcessUserMessage()` defer, and error paths
3. Keep raw error in `UpdateQueryStatus()` for debugging/logging
4. **Files touched:** `discussion_engine.go` (~15 changes)

### Phase 2: Query Loop Detection Improvement (Medium Risk)

1. Add one-shot retry prompt when same query detected
2. If still same query, render friendly message and return
3. **Files touched:** `discussion_engine.go` (~20 lines)

### Phase 3: Retry Strategy Improvement (Higher Risk)

1. Improve `isRetryableError()` with better classification
2. Add exponential backoff for transient errors
3. Add rate limit detection with longer retry delay
4. **Files touched:** `sql_execution.go`, `discussion_engine.go` (~40 lines)

### Phase 4: Error Tracking & Analytics (Future)

1. Track error categories in query records
2. Add `error_category` field to `queries` table
3. Expose error stats in Settings view
4. **Files touched:** `models/query.go`, `models/database.go`, `SettingsView.svelte`

## Testing Strategy

| Test Case | Expected Result |
|---|---|
| LLM produces same query twice | Friendly message, no raw error shown |
| SQL syntax error | Generic "I had trouble understanding" message |
| Connection timeout | "Having trouble connecting" message |
| Invalid API key | "Authentication failed" message |
| Schema fetch fails | Continue with partial schema, no error shown to user |
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

All changes are additive — no breaking changes to:
- Query record schema
- Conversation message format
- API surface
- User data

If issues arise, revert `formatError()` and `categorizeAndFormatError()` to restore original behavior.

## Related Files

| File | Role |
|---|---|
| `pkg/services/discussion_engine.go` | Main orchestrator, error paths |
| `pkg/services/sql_execution.go` | `isRetryableError()`, SQL execution |
| `pkg/models/query.go` | Query record schema |
| `pkg/models/database.go` | `UpdateQueryStatus()`, `CreateConversationMessage()` |
| `frontend/src/ConversationView.svelte` | Displays error messages |
| `frontend/src/SettingsView.svelte` | Shows error status in data source list |
