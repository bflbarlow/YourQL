> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# ANSWER Action Enhancement

**Date:** 2026-07-26  
**Project:** YourQL — Adding an `answer` response type to the discussion engine

---

## 1. Motivation

The discussion engine currently supports three LLM actions:

| Action | Purpose |
|---|---|
| `sql_query` | Execute SQL against the database and render results |
| `clarification` | Ask the user for more information before proceeding |
| `sql_exploration` | Run a read-only query to explore the data, feed results back into context |

There is no action for the LLM to respond directly when the question can be answered from:
- **Previous results already in the conversation** — "What was the highest month in that data?"
- **General knowledge** — "What is a moving average?"
- **Formatting/narrative requests** — "Put those results into a story format."

Currently, the LLM must shoehorn these into `clarification`, which frames the response as "I need something from you" when it's really just providing an answer.

---

## 2. Design

### 2.1 New Action: `answer`

A new `answer` action allows the LLM to respond directly with markdown text. The response is rendered as an assistant message — identical in appearance to a `sql_query` result's explanation block, but without any SQL, table, or chart.

```json
{
  "action": "answer",
  "answer": "A moving average smooths out price data by creating a constantly updated average..."
}
```

### 2.2 When the LLM Should Use `answer`

The system prompt will instruct the LLM:

- Use `answer` when the question **does not require querying the database**. This includes follow-ups about previously returned data, general knowledge questions, and pure formatting/rewriting requests.
- If the question *might* need fresh data, prefer `sql_query` or `sql_exploration`. The LLM should not answer from stale training data when the database has the ground truth.
- If the user's intent is genuinely ambiguous, use `clarification` — not `answer` with a guess.

### 2.3 Rendering

`answer` messages are rendered as standard assistant message bubbles containing the markdown-converted response. No SQL toolbar, no results table, no chart, no summarization (there's no query to summarize). The message looks identical to the current explanation/summary blocks but stands alone.

---

## 3. Implementation Plan

### 3.1 Data Model (`pkg/services/discussion_engine.go`)

**`LLMResponse` struct — add field:**

```go
type LLMResponse struct {
    Action                string `json:"action"`
    SQLQuery              string `json:"sql_query,omitempty"`
    ClarificationQuestion string `json:"clarification_question,omitempty"`
    Answer                string `json:"answer,omitempty"`     // NEW
    Explanation           string `json:"explanation,omitempty"`
    VizConfig             string `json:"viz_config,omitempty"`
}
```

This is backward-compatible — the JSON tag `json:"answer,omitempty"` means existing responses without this field unmarshal fine (field remains empty string), and existing stored `llm_content` JSON in the database parses successfully since `omitempty` on both marshal and unmarshal directions is tolerant.

### 3.2 System Prompt (`buildSystemPrompt`)

In the **Instructions** section (~line 861), extend the action documentation:

```go
sb.WriteString("   - \"action\": one of \"sql_query\", \"clarification\", \"sql_exploration\", or \"answer\"\n")
sb.WriteString("   - \"sql_query\": if action is \"sql_query\" or \"sql_exploration\", provide a valid SELECT query.\n")
sb.WriteString("   - \"clarification_question\": if action is \"clarification\", ask a concise clarifying question.\n")
sb.WriteString("   - \"answer\": if action is \"answer\", provide a direct response to the user.\n")
sb.WriteString("     Use \"answer\" when the question does not require querying the database — e.g.,\n")
sb.WriteString("     follow-ups about previously returned data, general knowledge questions,\n")
sb.WriteString("     or formatting/narrative requests. Do NOT use \"answer\" when the database\n")
sb.WriteString("     has the definitive answer — prefer \"sql_query\" instead.\n")
```

The new prompt text is ~380 bytes. The system prompt is already capped at 16KB and typically runs 3-8KB with a medium schema — well within limits.

### 3.3 Response Parsing — Unknown-Action Recognition (line 274)

**This is the most critical change.** At line 274, the engine checks whether an action is recognized:

```go
} else if llmResp.Action != "sql_query" && llmResp.Action != "clarification" && llmResp.Action != "sql_exploration" {
```

Without adding `"answer"` to this condition, any `answer` response from the LLM would be treated as an **unknown action** and retried (up to `maxActionRetries`), never reaching the new `case "answer":` in the switch statement. The user would see "I couldn't understand your last response (action: \"answer\")" instead of the actual answer.

**Fix:**
```go
} else if llmResp.Action != "sql_query" && llmResp.Action != "clarification" && llmResp.Action != "sql_exploration" && llmResp.Action != "answer" {
```

### 3.4 Response Parsing — Empty Answer Guard

After the unknown-action handling block (after line 290, before the `switch`), add a guard for `answer` actions with no text:

```go
if llmResp.Action == "answer" && llmResp.Answer == "" {
    llmResp.Action = "clarification"
    llmResp.ClarificationQuestion = "I tried to answer but received an empty response. Could you rephrase your question?"
}
```

This must come AFTER the unknown-action block (so that `answer` is recognized as valid first) but BEFORE the switch (so the empty case is converted before dispatch).

### 3.5 Response Parsing — `parseLLMResponse` Fallbacks

`parseLLMResponse` (line 437) has **two** invalid-JSON fallback paths, not one. The document must address both:

**Path 1 (line 446):** Response starts with `{` or `[` (looks like JSON but failed to parse). Currently returns `clarification` with an error message including the raw text:
```go
return LLMResponse{
    Action:                "clarification",
    ClarificationQuestion: fmt.Sprintf("The LLM returned invalid JSON (parse error: %v). Raw:\n\n```\n%s\n```", err, truncateString(cleanedResponse, 300)),
}, nil
```
**Decision: leave as `clarification`.** This is an error condition (malformed JSON), not a valid answer. Converting it to `answer` would render the raw broken JSON as if it were a legitimate response.

**Path 2 (line 451):** Response is plain text (no JSON braces). Currently returns `clarification` with the text as the question:
```go
return LLMResponse{
    Action:                "clarification",
    ClarificationQuestion: cleanedResponse,
}, nil
```
**Decision: change to `answer`.** A plain-English text response from the LLM is more naturally an answer than a clarification. This is a behavior change — currently the user sees the text framed as a question; with the change, it renders as a direct response.

```go
return LLMResponse{
    Action: "answer",        // was "clarification"
    Answer: cleanedResponse, // was ClarificationQuestion
}, nil
```

### 3.6 Main Loop — New `case` (line 293)

Add a new case to the exploration loop's switch statement, alongside `case "sql_query":`, `case "clarification":`, etc.:

```go
case "answer":
    err := handleAnswer(query, llmResp, conversationID)
    if err == nil {
        assistantMessageSaved = true
    }
    return err
```

### 3.7 `handleAnswer` Function

A new function analogous to `handleClarification` but simpler — no question framing, just render the markdown response:

```go
func handleAnswer(query *models.Query, resp LLMResponse, conversationID uint) error {
    if err := UpdateQueryStatus(query.ID, "answer", nil, nil, nil, nil, nil, nil); err != nil {
        return fmt.Errorf("failed to update query: %w", err)
    }

    htmlContent := renderMarkdown(resp.Answer)

    llmContentJSON, _ := json.Marshal(resp)
    llmContent := string(llmContentJSON)

    metadataJSON, _ := json.Marshal(map[string]interface{}{"content_type": "html"})
    metadata := string(metadataJSON)

    _, err := CreateConversationMessage(conversationID, "assistant", 
        fmt.Sprintf("<div class=\"markdown-content\">%s</div>", htmlContent),
        &llmContent, nil, &metadata)
    if err != nil {
        return fmt.Errorf("failed to create answer message: %w", err)
    }
    return nil
}
```

**Key differences from `handleClarification`:**
- No `ClarificationQuestion` / `Explanation` concatenation with parentheses wrapping
- Status is `"answer"` not `"clarification"`
- Content uses `renderMarkdown` for proper formatting
- No `Explanation` field appended (the answer *is* the content)

### 3.8 Retry Path (`executeFinalQueryWithRetry`, line 618)

When a SQL query fails and the LLM is asked to correct it, the retry logic at line 618 handles **all** non-`sql_query` actions identically — it creates an error-state message:

```go
if newResp.Action != "sql_query" {
    // ...
    var displayMsg string
    if newResp.ClarificationQuestion != "" {
        displayMsg = newResp.ClarificationQuestion
    } else if newResp.Explanation != "" {
        displayMsg = newResp.Explanation
    } else {
        displayMsg = fmt.Sprintf("The query failed and the LLM responded with '%s'. Please rephrase your question.", newResp.Action)
    }
    _ = UpdateQueryStatus(query.ID, "error", &lastSQL, nil, stringPtr(fmt.Sprintf("%s: %v", newResp.Action, lastErr)), nil, nil, nil)
    _, _ = CreateConversationMessage(conversation.ID, "assistant", displayMsg, llmContentPtr, nil, nil)
    return
}
```

**Important correction:** An earlier draft of this document claimed the retry path had existing explicit `clarification` handling. That is **incorrect** — the current code treats ALL non-`sql_query` actions (including `clarification`) as errors, displaying "The query failed and the LLM responded with 'clarification'."

With `answer` added, insert a check for `answer` **before** the generic error handling:

```go
if newResp.Action != "sql_query" {
    if newResp.Action == "answer" {
        _ = handleAnswer(query, newResp, conversation.ID)
        return
    }
    // ... existing error handling for all other non-sql_query actions ...
}
```

This treats an `answer` during retry as a valid outcome (the LLM decided the question can be answered without re-querying), while preserving the existing error behavior for `clarification` and other actions. Optionally, `clarification` could also be given its own branch here to render properly instead of as an error — but that's a separate enhancement outside the scope of this change.

### 3.9 Exploration-Exhausted Path (line 403)

When exploration rounds are exhausted, the engine forces a final LLM call with a system message demanding `sql_query`. If the LLM still returns a different action:

- `clarification` → existing behavior, show it.
- `answer` → **NEW**. Accept it. The LLM has determined that even after exploration, the question is better answered directly than queried. For example: "Summarize what we've learned" after 3 exploration rounds produced data the LLM already processed.

The current code at ~line 404-413 converts non-`sql_query` non-`clarification` into `clarification`. Split this:

```go
if finalResp.Action == "clarification" {
    return handleClarification(query, finalResp, conversationID)
}
if finalResp.Action == "answer" {
    return handleAnswer(query, finalResp, conversationID)
}
// existing fallback to clarification
```

### 3.10 Summarization Interaction

`answer` actions do not produce SQL, so there is nothing to summarize. The `renderSQLResults` path (which checks `conversation.Summarize`) is never called for `answer`. No changes needed.

### 3.11 Exploration Context

During exploration rounds, the LLM may decide mid-round that it doesn't need a final query — it can answer directly. Throwing `answer` into the main switch (exploration loop) handles this naturally, since all actions are evaluated each round.

### 3.12 Conversation Context (History to LLM)

When building the LLM message history in `buildLlmMessages`, existing code already strips HTML from assistant content for LLM consumption and includes `llm_content` (the raw JSON) when available. `answer`-type messages work the same way — the assistant's `content` stores HTML, `llm_content` stores the JSON. The existing `buildLlmMessages` logic already handles this since it doesn't special-case by action type. No changes needed.

### 3.13 Stored `sql_results` Column

For `answer` messages, the `sql_results` column is `NULL` — there is no query result to store. The `CreateConversationMessage` call in `handleAnswer` passes `nil` for the `sqlResults` parameter. This is consistent with `clarification` messages which also pass `nil`.

### 3.14 Unknown-Action Retry Prompt (line 279)

The retry message sent to the LLM when an unknown action is received currently lists only three actions:

```go
Content: fmt.Sprintf("Your previous response was valid JSON but did not include a recognized action. You must use one of: \"sql_query\", \"clarification\", or \"sql_exploration\". Please respond again with the correct format."),
```

Add `answer`:

```go
"...one of: \"sql_query\", \"clarification\", \"sql_exploration\", or \"answer\"..."
```

Also update the missing-action fallback message at line 272, which currently says:
```go
"I received your response but couldn't determine what you wanted me to do. Please use one of: sql_query, clarification, or sql_exploration."
```
Add `answer` to this list as well.

---

## 4. Touchpoint Map

Every location that must change, with the nature of the change:

| File | Location | Change |
|---|---|---|
| `LLMResponse` struct | line 16 | Add `Answer` field with `json:"answer,omitempty"` tag |
| `buildSystemPrompt` | line 861 | Document `answer` action in Instructions |
| **Unknown-action recognition** | **line 274** | **CRITICAL: add `&& llmResp.Action != "answer"` to the condition — otherwise `answer` responses are treated as unknown actions and retried** |
| Missing-action fallback message | line 272 | Add `answer` to the list of recognized actions in the error text |
| Unknown-action retry prompt | line 279 | Add `answer` to the list of recognized actions sent to the LLM |
| Empty-answer guard | after line 290, before switch | Convert `answer` with empty text to `clarification` |
| Main exploration switch | line 293 | Add `case "answer":` |
| `handleAnswer` function | new function | Analogous to `handleClarification`, simpler |
| Exploration-exhausted path | line 403 | Accept `answer` alongside `clarification` |
| `executeFinalQueryWithRetry` retry | line 618 | Handle `answer` response during query correction (before generic error handling) |
| `parseLLMResponse` plain-text fallback | line 451 | Change non-JSON, non-SQL fallback from `clarification` to `answer` |
| `parseLLMResponse` invalid-JSON fallback | line 446 | **Leave as `clarification`** — malformed JSON is an error, not an answer |

That's **12 touchpoints**, one of which is a new function. No existing function signatures change. No stored data format changes. No API changes. The line 274 change is the single most critical — without it, the feature silently fails.

---

## 5. Backward Compatibility

### 5.1 Existing Stored Messages

Existing `llm_content` JSON in `conversation_messages` doesn't contain `answer` fields. The `omitempty` tag means deserialization of old rows produces `Answer: ""` — the field is simply absent. No migration needed.

### 5.2 Existing Conversations in Progress

Conversations started before this change use the old system prompt (no `answer` documentation). The LLM will continue returning `clarification` as before. New conversations get the updated prompt. The engine handles both action sets seamlessly.

### 5.3 Frontend Rendering

`answer` messages are rendered as standard `assistant`-role messages with `content_type: "html"` in metadata. The frontend already renders all assistant messages via `{@html message.content}` (or the structured-data path in future). No frontend changes are required — the backend produces the HTML, the frontend displays it.

### 5.4 Technical-Details Display

When technical details are enabled, the `llm_content` field stores the raw LLM JSON response, which now may contain `"action":"answer","answer":"..."`. This is displayed as-is — the `pre` block in technical details shows whatever JSON was returned. No change needed.

### 5.5 `parseLLMResponse` Plain-Text Fallback Change

The plain-text fallback in `parseLLMResponse` (line 451) changes from `clarification` to `answer`. This is a **behavior change for malformed LLM responses**. Previously, when the LLM returned plain text (not JSON, not SQL), the user saw it framed as a clarification question. After the change, it renders as a direct answer. Old conversations stored before this change are unaffected — their stored `content` is already HTML and doesn't pass through `parseLLMResponse` on re-render. The change only affects new message processing.

---

## 6. Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| **Forgetting to add `answer` to the line 274 recognized-action condition** | **High if missed — this is the #1 implementation pitfall** | **The `answer` action would be silently treated as unknown and retried, never reaching the switch. This is called out in §3.3 and the touchpoint map.** |
| LLM uses `answer` instead of `sql_query` for data questions | Medium — depends on prompt quality | Strong prompt wording: "Do NOT use answer when the database has the definitive answer — prefer sql_query instead." If the LLM ignores this, the user simply gets a text response instead of data — they can re-ask. |
| LLM returns `answer` with empty text | Low — models typically fill requested fields | Guard clause (§3.4) converts to `clarification` with a friendly message |
| `parseLLMResponse` plain-text fallback change from `clarification` to `answer` causes visual regression | Low | Only the plain-text fallback (line 451) changes. The invalid-JSON fallback (line 446) stays as `clarification` with its error framing. So malformed JSON still shows an error, while plain-text responses now render directly. |
| LLM conflates `answer` with `clarification` | Low — semantically distinct | The prompt clearly separates them: "clarification = ask the user a question, answer = respond directly" |
| Existing summarization pipeline accidentally triggered | None | `answer` never calls `renderSQLResults`, never calls `summarizeResults`. Code paths are fully disjoint. |
| `answer` during retry path not handled | Medium if §3.9 is skipped | The retry path at line 618 treats ALL non-`sql_query` as errors. Without the `answer` check, an `answer` during retry shows "The query failed and the LLM responded with 'answer'." |

---

## 7. Testing Plan

1. **Unit test `parseLLMResponse`** with valid `answer` JSON, malformed JSON, and plain-text responses to verify fallback behavior.
2. **Conversation test: general knowledge** — ask "What is SQL?" and verify the LLM responds with `answer`, not `clarification` or a dummy query.
3. **Conversation test: follow-up** — after a `sql_query` returns product data, ask "Which one had the highest quantity?" The LLM may use `answer` (since data is in context) or `sql_query`. Both are valid — verify both render correctly.
4. **Conversation test: forced `answer`** — ask the LLM to "Just tell me what you think, don't query the database." Verify it uses `answer`.
5. **Retry test** — force a SQL error and verify the LLM's correction response handles `answer` gracefully.
6. **Exploration exhaustion test** — set `max_exploration_rounds = 1`, ask a question that triggers exploration, and verify the exhaustion-forced response handles `answer`.
7. **Backward compatibility** — load an existing conversation with pre-`answer` LLM responses and verify all messages render correctly.
