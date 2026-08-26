> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Application Help Chat Enhancement

**Date:** 2026-07-26  
**Project:** YourQL — In-app contextual help powered by a configured LLM

---

## 1. Overview

Add a floating help chat to YourQL, powered by a user's configured LLM. When a provider has "Allow Application Help" enabled, a chat icon appears in the bottom-right corner of the app. Clicking it opens a help panel where the user can ask questions about the application, the current page, or their data. The LLM receives a system prompt describing YourQL, a snapshot of the current page context, and the user's question — and responds with a helpful answer.

This is **not** a discussion thread. It is a single-shot Q&A interface with no conversation history. Every question is a fresh prompt.

---

## 2. Design

### 2.1 Provider Configuration

A new checkbox on the **Model Configurations** tab:

```
[x] Allow Application Help
```

When checked on at least one provider, the help icon appears. If multiple providers have it enabled, the **default** provider is used. If no default is set, the first enabled provider alphabetically is used.

The checkbox state is stored in the existing `llm_providers.config` JSON column:

```json
{
  "allow_help": true
}
```

This requires no schema migration — the `config` column already exists as a JSON text field and is parsed/unmarshaled generically. Adding a new key is backward-compatible (absent key → `false`).

### 2.2 Help Icon & Panel

**Icon:** A small circular button in the bottom-right corner of the app viewport, fixed position, z-indexed above all content. Uses the `HelpCircle` icon from lucide-svelte.

**Panel:** A resizable/draggable chat panel that slides up from the icon. Contains:
- A header: "YourQL Help" with a close button
- A message area showing the last Q&A pair
- A text input + send button at the bottom
- A loading indicator while waiting for the LLM

**States:**
- Collapsed: only the icon is visible
- Expanded: the panel is open, showing the last question/answer (or empty state)
- Loading: panel is open, spinner visible, input disabled

### 2.3 Prompt Structure

Every help query sends three pieces of information to the LLM:

**1. System prompt** — static description of YourQL:

```
You are a helpful assistant for YourQL, a desktop application that lets users
query databases using natural language. YourQL translates questions into SQL
and executes them against the user's configured databases.

Key features:
- Create discussions to ask questions about your data
- Configure LLM providers (OpenAI, Anthropic, Ollama, custom endpoints)
- Connect to databases (MySQL, PostgreSQL, SQLite, SQL Server, Snowflake,
  BigQuery, Redshift, MariaDB, CSV files, Excel files, Google Sheets)
- Data visualization with bar, line, pie, scatter, and other charts
- Result summarization and exploration queries
- Custom system prompts, business rules, and table/column descriptions
- Reusable Skills for domain knowledge per conversation
- PDF export of conversations
- Conversation management (pin, archive, duplicate, clear, delete)

The user is asking you a question about the application. Below you will see
what page they are currently viewing and any relevant context about that page.
Answer their question directly and concisely. If they ask about something not
on the current page, use your knowledge of the application's features to help.
Use markdown formatting in your response.
```

**2. Page context** — a structured description of what the user is looking at, captured by the frontend and passed as a string:

```
The user is currently viewing: [Discussions list]
- 12 discussions visible
- Connected to: MySQL (classicmodels)
- Using: OpenAI (gpt-4)
```

Or:

```
The user is currently viewing: [Settings > Data Sources > Edit Connection]
- Editing: "My Production DB" (PostgreSQL)
- Host: db.example.com:5432
- Database: analytics
- Currently viewing the Connection Info section
```

Or:

```
The user is currently viewing: [Conversation: "Monthly Sales Analysis"]
- Data source: MySQL (classicmodels)
- LLM provider: OpenAI (gpt-4)
- 23 messages in the conversation
- Last message was an assistant response with a summary and a 29-row table
- Summarize results: ON
- Data visualization: ON
```

**3. User question** — verbatim from the input field.

### 2.4 Context Capture by Page

The frontend captures context differently depending on the active view:

| View | Context Captured |
|---|---|
| Discussions list | List of discussion titles, count, current default provider and data source |
| Conversation view | Discussion title, provider name, data source name, message count, summary/viz toggles, last few message summaries |
| Settings > Model Configurations | List of configured providers (name, type, model, default status) |
| Settings > Data Sources (list) | List of data sources (name, type, status) |
| Settings > Data Sources (detail) | Connection details (name, type, host, database), exploration settings, schema table count |
| Settings > Skills | List of skills (name, active status) |
| Settings > General | Current UI scale setting |
| About | Application version |

### 2.5 Response Rendering

The LLM's response is rendered as markdown in the help panel. The rendering reuses the existing `renderMarkdown` function from the backend. No charts, no tables, no SQL — just formatted text.

---

## 3. Implementation Plan

### 3.1 Backend — Data Model

Add a helper to `pkg/models/llm_provider.go` to read the `allow_help` config flag. No struct change needed — it's a key in the existing `Config` JSON column.

```go
func (p *LLMProvider) AllowHelp() bool {
    if p.Config == nil || *p.Config == "" {
        return false
    }
    var cfg map[string]interface{}
    if err := json.Unmarshal([]byte(*p.Config), &cfg); err != nil {
        return false
    }
    v, _ := cfg["allow_help"].(bool)
    return v
}
```

### 3.2 Backend — Service

Two new functions in a new file `pkg/services/help.go`:

**`GetHelpProvider() (*models.LLMProvider, error)`**
- Calls `ListLLMProvidersByWorkspace()`
- Filters to those with `allow_help == true`
- Returns the default one, or the first alphabetically, or an error if none enabled

**`AskHelpQuestion(pageContext string, question string) (string, error)`**
1. Calls `GetHelpProvider()` to find the help-enabled model
2. Creates an `LLMClient` for that provider
3. Builds the system prompt (static YourQL description) + page context + user question
4. Calls `client.ChatCompletion(ctx, messages)` — single-shot, no history
5. Returns the response text (markdown)
6. 30-second timeout

The request is a simple three-message array:
```go
messages := []ChatMessage{
    {Role: "system", Content: helpSystemPrompt},
    {Role: "user", Content: fmt.Sprintf("Page context:\n%s\n\nQuestion:\n%s", pageContext, question)},
}
```

### 3.3 Backend — Wails Binding

Add a single binding in `app.go`:

```go
func (a *App) AskHelpQuestion(pageContext string, question string) (string, error) {
    return services.AskHelpQuestion(pageContext, question)
}
```

### 3.4 Frontend — Provider Configuration UI

In `SettingsView.svelte`, on the Model Configurations tab, add a checkbox to the provider card and the create/edit form:

```svelte
<div class="form-group">
  <label>
    <input type="checkbox" bind:checked={helpEnabled} />
    Allow Application Help
  </label>
</div>
```

The `helpEnabled` value is read from `provider.config` (parsed JSON) and saved as part of the provider's config object when the form is submitted.

### 3.5 Frontend — Help Icon & Panel (`HelpChat.svelte`)

A new Svelte 5 component:

**Props:** none — it reads application state from the DOM/page context directly.

**State:**
- `isOpen` — whether the panel is expanded
- `question` — the current input text
- `answer` — the last response from the LLM
- `loading` — whether a query is in flight
- `error` — any error message

**Lifecycle:**
- On mount, calls a backend method to check if any provider has help enabled
- If yes, renders the icon; if no, renders nothing
- When the active view changes (discussions → conversation → settings), the page context string is recalculated

**Context capture:** Each view already knows its own state. The help component can access this via:
- Reading DOM state (document title, visible elements)
- Importing shared state from `App.svelte` (if refactored into a store)
- Or, simplest approach: the `AskHelpQuestion` binding accepts the context as a string, and each view builds its own context string when the help panel is opened. This keeps concerns separated — the help component is generic, each view knows how to describe itself.

**Approach for context capture (recommended):** The `HelpChat.svelte` component is placed inside `App.svelte`. When the user opens the help panel, the component calls a function provided by `App.svelte` that returns a context string based on the current `activeView` and any active conversation/settings state. This avoids coupling the help component to every view's internal state.

```js
// In App.svelte
function getPageContext() {
    if (activeView === 'discussions') {
        return `Discussions list — ${conversations.length} discussions visible`
    }
    if (activeView === 'conversation' && activeConversation) {
        return `Conversation: "${activeConversation.title}" — ${conversationMessages.length} messages — Provider: ${llmProviders.find(...)?.name} — Data source: ${dataSources.find(...)?.name}`
    }
    // ... etc for each view
}
```

The help component receives `getPageContext` as a prop.

### 3.6 Frontend — Styling

The icon and panel use fixed positioning with high z-index. The panel is a card with a drop shadow, max-height with scroll, and a semi-transparent backdrop when open (optional — or just float over content).

```
┌─────────────────────────────┐
│ YourQL Help              ✕  │
├─────────────────────────────┤
│                             │
│  [Previous Q&A or empty]    │
│                             │
│  User: How do I add a DB?   │
│  Help: Go to Settings >     │
│  Data Sources and click...  │
│                             │
├─────────────────────────────┤
│ [Type your question...]  →  │
└─────────────────────────────┘
```

Panel dimensions: 380px wide, 500px tall, positioned 20px from bottom and 20px from right.

---

## 4. Touchpoint Map

| File | Change |
|---|---|
| `pkg/models/llm_provider.go` | Add `AllowHelp()` method |
| `pkg/services/help.go` | New file — `GetHelpProvider()`, `AskHelpQuestion()` |
| `app.go` | Add `AskHelpQuestion` Wails binding |
| `frontend/src/HelpChat.svelte` | New file — icon + panel component |
| `frontend/src/App.svelte` | Add `getPageContext()` function, render `<HelpChat>`, pass context builder |
| `frontend/src/SettingsView.svelte` | Add "Allow Application Help" checkbox to provider form and card |

That's **6 touchpoints**, two of which are new files.

---

## 5. Edge Cases & Considerations

### 5.1 No Help Provider Enabled

If no provider has `allow_help == true`, the help icon does not render. The `AskHelpQuestion` binding returns a clear error: "No LLM provider has been configured for application help. Enable 'Allow Application Help' on a provider in Settings."

### 5.2 LLM Provider Has No API Key / Is Offline

The LLM call fails — the `AskHelpQuestion` binding returns the error, and the panel displays it as "Unable to reach the help model: [error message]. Check your provider configuration in Settings."

### 5.3 Sensitive Data in Context

The page context may include database hostnames, table names, or conversation content. This data is sent to the configured LLM provider. Users should be aware that the help feature sends page context to the LLM. The checkbox label could include a note: "Allow this model to be used for in-app help. Page context (current view, settings) will be sent with your questions."

### 5.4 User Asks About Data (Not App)

The LLM receives a system prompt describing the app, not the user's database. If the user asks "What were my top 3 products?", the LLM should respond that it's a help assistant and can't query the database — suggesting the user create a discussion instead. The system prompt should include this instruction:

```
You are a help assistant for the YourQL application. You cannot query
databases or access the user's data. If the user asks a data question,
politely direct them to create a Discussion where they can query their
database. Only help with application usage, navigation, and features.
```

### 5.5 Multiple Providers With Help Enabled

The `GetHelpProvider()` function returns the default provider with help enabled, or the first alphabetically if no default. This is documented in the UI: the checkbox tooltip says "If multiple providers have this enabled, the default provider is used."

### 5.6 Provider Help Is Disabled Mid-Session

If the user unchecks "Allow Application Help" on the provider that's currently powering the help chat, the icon disappears. If the panel is open, it closes gracefully with a message: "Help is no longer available. Enable 'Allow Application Help' on a provider in Settings."

---

## 6. Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| LLM hallucinates app features that don't exist | Medium | The system prompt describes actual features. Hallucination risk is inherent to LLMs but the prompt grounds it. |
| Page context leaks credentials | Low | Context capture explicitly avoids sensitive fields. No passwords, API keys, or connection strings are included. Only display names, types, and counts. |
| Help LLM call blocks the UI | Low | 30-second timeout; the frontend shows a loading spinner and remains responsive (Wails calls are async). |
| User confuses help chat with discussion | Low | The help panel is visually distinct (smaller, positioned differently, labeled "YourQL Help"). |
| LLM responds with markdown that doesn't render well | Low | Same markdown renderer as discussions — already battle-tested. |

---

## 7. Testing

| Test | Description |
|---|---|
| `TestAllowHelp_NoConfig` | Provider with nil Config → AllowHelp returns false |
| `TestAllowHelp_ConfigTrue` | Config with `{"allow_help": true}` → returns true |
| `TestAllowHelp_ConfigFalse` | Config with `{"allow_help": false}` → returns false |
| `TestAllowHelp_ConfigAbsent` | Config with other keys but no allow_help → returns false |
| `TestAllowHelp_InvalidJSON` | Malformed config → returns false |
| `TestGetHelpProvider_OneEnabled` | One provider with help → returns that provider |
| `TestGetHelpProvider_DefaultPreferred` | Multiple enabled, one is default → returns default |
| `TestGetHelpProvider_NoneEnabled` | No providers with help → returns error |
| `TestAskHelpQuestion_BuildsCorrectPrompt` | Mock LLM, verify system prompt contains app description and page context |
| `TestAskHelpQuestion_ReturnsMarkdown` | Mock LLM returns markdown, function passes it through |
| `TestAskHelpQuestion_LLMFailure` | Mock LLM errors → error returned |
| Frontend: Help icon hidden when no providers enabled | Component test |
| Frontend: Help panel opens/closes | Component test |
| Frontend: Context string changes per view | Component test |
