# Summary Enhancement — Discussion Engine

**Status**: Design / Planning  
**Date**: 2026-07-15  

---

## 1. Motivation

Currently, YourQL's Discussion Engine always renders SQL query results as an HTML table. The user asked for an alternative mode where the results are **summarized by the LLM in natural language**, directly answering the user's question. The table is still available but collapsed by default.

This gives users two complementary modes for the same discussion:

| Mode | Behavior |
|------|----------|
| **Table (default)** | Results rendered as a sortable HTML table — great for data exploration |
| **Summary** | Results sent back to the LLM for a plain-English answer — great for quick questions |

A single discussion setting checkbox toggles between them.

---

## 2. Data Model

### 2.1 New column on `conversations`

```sql
ALTER TABLE conversations ADD COLUMN summarize INTEGER DEFAULT 0;
```

### 2.2 Go model (`pkg/models/conversation.go`)

```go
type Conversation struct {
    // ...existing fields...
    Summarize bool `json:"summarize"`
}
```

### 2.3 Schema migration

Add `ensureColumn("conversations", "summarize", "INTEGER DEFAULT 0")` to `initializeSchema()` in `pkg/models/database.go`.

---

## 3. Backend — Service Layer

### 3.1 Conversation service (`pkg/services/conversation.go`)

Add `UpdateConversationSummarize` function (patterned after `UpdateConversationMaxContextMessages`):

```go
func UpdateConversationSummarize(conversationID uint, summarize bool) error {
    _, err := models.DB.Exec(
        "UPDATE conversations SET summarize = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
        summarize, conversationID,
    )
    if err != nil {
        return fmt.Errorf("failed to update conversation summarize: %w", err)
    }
    return nil
}
```

Also include `summarize` in SELECT queries and `DuplicateConversation` INSERT.

### 3.2 App bindings (`app.go`)

```go
func (a *App) UpdateConversationSummarize(conversationID uint, summarize bool) error {
    return services.UpdateConversationSummarize(conversationID, summarize)
}
```

### 3.3 Discussion engine — core logic (`pkg/services/discussion_engine.go`)

The change happens in `ProcessUserMessage`, around the final rendering. Currently the flow is:

```
executeFinalQueryWithRetry → renderSQLResults → done
```

With summarize mode:

```
executeFinalQueryWithRetry (collects results)
    ↓
if conversation.Summarize:
    call LLM with summarization prompt + results
    ↓
renderSQLResults (includes summary text if available)
    ↓
done
```

**Approach A (preferred): Modify `executeFinalQueryWithRetry`** to accept the conversation pointer, call a new `summarizeResults` function before `renderSQLResults`, and thread the summary text into `renderSQLResults` / `AssistantResponse`.

**Alternative Approach B: Modify `renderSQLResults`** to accept the conversation and conditionally call the LLM for summarization. This keeps the change more contained but mixes rendering with LLM calls.

I recommend **Approach A** because the LLM call belongs in the engine's control flow, not in a rendering helper.

### 3.4 Summarization prompt

```go
func buildSummarizationPrompt(userQuestion string, sqlQuery string, results *QueryResult) string {
    // Format results compactly (reuse formatSQLResultsForLLM pattern)
    formatted := formatSQLResultsForLLMFromQueryResult(results)

    return fmt.Sprintf(`You are a helpful data analyst. Summarize the following SQL query results in 
plain English, directly answering the user's question.

**User's question**: %s

**SQL executed**: 
` + "```sql\n%s\n```" + `

**Query results**:
%s

**Instructions**:
- Answer the user's question directly, referencing specific numbers and facts from the data.
- Keep it concise — 3-5 sentences is ideal.
- If the results are empty, clearly state that no data matched the query.
- Do NOT include a markdown table — this is a prose summary.
- Do NOT suggest next steps — just answer the question.`, userQuestion, sqlQuery, formatted)
}
```

### 3.5 Revised `renderSQLResults` signature

```go
func renderSQLResults(query *models.Query, resp LLMResponse, dbConnection *models.DBConnection, 
    conversationID uint, explorationResults []ExplorationResult, results *QueryResult,
    summary *string)  // NEW: optional summary text
```

In `AssistantResponse.ToHTML()`, when summary is present:
- Render the summary text prominently at the top (larger font, no special styling needed)
- Render the table below in a `<details><summary>View raw results (N rows)</summary>` collapsible section

### 3.6 Edge cases

| Scenario | Handling |
|----------|----------|
| LLM summarization call fails | Fall back to table mode, log the error |
| Results have 0 rows | Send to LLM anyway — it should say "no matching data found" |
| Results have 50,000 rows | Truncate to 200 rows before sending (same cap as existing `formatSQLResultsForLLM`) |
| User toggles summary mid-conversation | Only affects future messages — historical messages retain their format |
| Exploration results + summary | Include a brief note about exploration, then the summary of the final query |

### 3.7 Token budget

The summarization prompt + truncated results should fit comfortably under 4K tokens. The 200-row cap with 80-char cell truncation ensures this. Even worst-case (200 rows × 5 columns × 80 chars) ≈ 80K characters ≈ 20K tokens, so we may want a tighter cap for summarization — say **50 rows**.

---

## 4. Frontend — Svelte

### 4.1 Discussion settings checkbox (`App.svelte`)

Add a new `gear-popover-section` row between "Visible Messages" and "Messages in LLM Context" (or at the end of the settings):

```svelte
<div class="gear-popover-section">
  <label>
    <input type="checkbox" 
      checked={summarize}
      onchange={(e) => handleSetSummarize(selectedConversation.ID, e.target.checked)}
    />
    Summarize results
  </label>
  <div style="color: #999; font-size: 11px; margin-top: 2px;">
    LLM summarizes query results as a plain-English answer
  </div>
</div>
```

### 4.2 Handler

```js
async function handleSetSummarize(conversationID, value) {
    await UpdateConversationSummarize(conversationID, value);
    // Update local state
    if (selectedConversation && selectedConversation.id === conversationID) {
        selectedConversation = { ...selectedConversation, summarize: value };
    }
}
```

### 4.3 Conversation list indicator

Optionally show a small badge or icon on the discussion in the sidebar when summarize mode is active (e.g., "📝"). This is a nice-to-have polish feature.

---

## 5. Implementation Order

1. **DB migration** — `ensureColumn("conversations", "summarize", "INTEGER DEFAULT 0")`
2. **Model** — add `Summarize bool` to `Conversation`
3. **Conversation service** — add `UpdateConversationSummarize`, update all SELECT queries
4. **App bindings** — add `UpdateConversationSummarize` Go binding
5. **Discussion engine** — add summarization prompt builder, call it in the flow, thread summary into rendering
6. **SQL execution rendering** — update `AssistantResponse.ToHTML()` / `formatResultsHTML()` to show summary + collapsed table
7. **Frontend** — checkbox in gear popover, handler, regenerate Wails bindings
8. **Frontend** — verify rendering of summary-style messages

---

## 6. Risks & Open Questions

| Risk / Question | Mitigation / Answer |
|-----------------|---------------------|
| Extra LLM API cost per query | User explicitly opts in; cost is one extra call per final answer |
| Latency (extra round trip) | Summarization is fast — small prompt, small response |
| Summary quality varies by model | User can toggle off if summaries are poor |
| What about clarification responses? | Summarize only applies to `sql_query` actions; clarifications are unaffected |
| Should exploration queries also be summarized? | No — only the final query's results are summarized. Exploration results remain as tech details |

---

## 7. Future Considerations

- **Cache summaries**: If the same query+results appear again, skip the LLM call. Low priority since the table view is always available.
- **Summary style**: Let users customize the summarization prompt per connection (e.g., "be sarcastic", "use bullet points", "speak like a pirate"). Out of scope for v1.
- **Streaming**: If the LLM provider supports streaming, stream the summary token-by-token for a snappier UX. Out of scope — start with a blocking call.
