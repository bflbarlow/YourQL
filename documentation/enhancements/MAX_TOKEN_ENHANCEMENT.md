> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Max Token Configuration — Technical Enhancement

> Per `AGENT_READ_FIRST.md` §0: *"Does this help the user get the right answer
> from their data, faster and more reliably?"* When the LLM runs out of tokens
> before finishing its response, the user gets an error instead of an answer.
> This document defines a configurable `maxTokens` setting per LLM provider to
> eliminate that failure mode.

---

## 1. Problem Statement

### 1.1 The Incident

A user with the qwen/qwen3.7-plus provider received the error:

> *"I received a response I couldn't understand. Could you try rephrasing your
> question?"*

Investigation of the API response payloads revealed:

**Round 1:**

| Detail | Value |
|---|---|
| `finish_reason` | `length` (hit token budget) |
| Reasoning tokens consumed | 1,969 |
| Remaining for JSON output | 31 tokens |
| `content` | `"answer": "This query is a step in the right direction because it successfully introduces"` — truncated mid-sentence |
| JSON valid? | No — cuts off before closing braces |

**Round 2 (retry):**

| Detail | Value |
|---|---|
| `finish_reason` | `length` (hit token budget again) |
| Reasoning tokens consumed | 2,000 (all of them) |
| Remaining for JSON output | 0 |
| `content` | `null` — model never produced a single byte of output |

After two consecutive failures, the discussion engine's retry handler
surfaced the generic error message. The error handling itself was graceful
and correct — the user saw a clean message rather than a stack trace or raw
JSON. The issue is that the token budget was too small for the model to
succeed.

### 1.2 Root Cause

Every LLM client in the codebase hardcodes `MaxTokens: 2000`:

| File | Line | Location |
|---|---|---|
| `pkg/services/llm_openai.go` | 92 | `openAIChatRequest.MaxTokens` |
| `pkg/services/llm_anthropic.go` | 102 | `anthropicChatRequest.MaxTokens` |
| `pkg/services/llm_anthropic.go` | 200 | `anthropicChatRequest.MaxTokens` (payload capture) |

Ollama (`llm_ollama.go`) and Local (`llm_local.go`) do not set `MaxTokens`
at all — they rely on the server's default, which varies.

The qwen/qwen3.7-plus model is a **reasoning model**. It generates extensive
chain-of-thought reasoning *before* producing its JSON response. Reasoning
tokens count against the `maxTokens` budget. With only 2,000 tokens total,
the model exhausts its budget on reasoning and never reaches the answer.

### 1.3 Why It's Intermittent

Not every response fails. The model succeeds when:

- The question is simple and reasoning is short (e.g., 600 tokens of
  reasoning + 1,400 tokens of JSON = fits in 2,000)
- The action is `sql_query` (response is short: `{"action":"sql_query",
  "sql_query":"SELECT ..."}`)
- The model happens to produce a concise answer

It fails when:

- The question requires complex multi-join analysis
- The action is `answer` (long markdown with code blocks)
- Reasoning consumes 1,900+ tokens before the model even starts its response

This is a **resource starvation bug**, not a logic bug. The model *can*
answer correctly — it just isn't given enough budget to do so.

### 1.4 Impact on the Fundamental Goal

Per the charter's priority order (§0):

| Priority | Impact |
|---|---|
| **#1 Correctness** | **Degraded.** Users receive error messages instead of answers for complex questions. A wrong answer is worse than no answer, but *no answer at all* when the model could have answered is a failure of the fundamental goal. |
| **#2 Safety** | None. No data or credential exposure. |
| **#3 Reliability** | **Degraded.** Users can't predict which questions will succeed. |
| **#4 Appeal/Comfort** | **Degraded.** Repeated errors erode trust. |

---

## 2. Design

### 2.1 Per-Provider Setting

Each LLM provider gets a configurable `maxTokens` value. It lives alongside
the existing provider fields (name, type, model, base URL, API key) and
follows the same patterns for creation, update, and persistence.

**Why per-provider rather than global:** Different models have different
context windows and reasoning behaviors. A user with GPT-4o may want 4,096
tokens while a user with a local Llama 3B on 4K context may need 512. A
global setting forces the lowest common denominator.

### 2.2 Context Window as Upper Limit

The `maxTokens` setting must never exceed the model's context window. The
context window is the total token budget for both input and output. If
`maxTokens` exceeds the context window, the API will reject the request or
truncate the input.

**Implementation:**

1. **Store `contextWindow` per provider** — a new column in `llm_providers`
2. **Clamp `maxTokens` to `contextWindow`** — in `effectiveMaxTokens()`:
   ```go
   func effectiveMaxTokens(provider *models.LLMProvider) int {
       configured := 2000
       if provider.MaxTokens != nil && *provider.MaxTokens > 0 {
           configured = *provider.MaxTokens
       }
       // Clamp to context window if known
       if provider.ContextWindow != nil && *provider.ContextWindow > 0 {
           if configured > *provider.ContextWindow {
               configured = *provider.ContextWindow
           }
       }
       // Also clamp to detected model max if available
       if provider.ModelMaxTokens != nil && *provider.ModelMaxTokens > 0 {
           if configured > *provider.ModelMaxTokens {
               configured = *provider.ModelMaxTokens
           }
       }
       return configured
   }
   ```
3. **UI enforcement** — the `maxTokens` input field has `max` attribute set to
   `contextWindow` when known, preventing the user from entering an invalid value

### 2.3 Context Window as Upper Limit

The `maxTokens` setting must never exceed the model's context window. The
context window is the total token budget for both input and output. If
`maxTokens` exceeds the context window, the API will reject the request or
truncate the input.

**Implementation:**

1. **Store `contextWindow` per provider** — a new column in `llm_providers`
2. **Clamp `maxTokens` to `contextWindow`** — in `effectiveMaxTokens()`:
   ```go
   func effectiveMaxTokens(provider *models.LLMProvider) int {
       configured := 2000
       if provider.MaxTokens != nil && *provider.MaxTokens > 0 {
           configured = *provider.MaxTokens
       }
       // Clamp to context window if known
       if provider.ContextWindow != nil && *provider.ContextWindow > 0 {
           if configured > *provider.ContextWindow {
               configured = *provider.ContextWindow
           }
       }
       // Also clamp to detected model max if available
       if provider.ModelMaxTokens != nil && *provider.ModelMaxTokens > 0 {
           if configured > *provider.ModelMaxTokens {
               configured = *provider.ModelMaxTokens
           }
       }
       return configured
   }
   ```
3. **UI enforcement** — the `maxTokens` input field has `max` attribute set to
   `contextWindow` when known, preventing the user from entering an invalid value
4. **Display context window** — the provider list shows context window when available

### 2.4 Default Values

When the user creates a new provider without specifying `maxTokens`, the
application uses a sensible default. The default can be per-provider-type
to accommodate different model families:

| Provider type | Default `maxTokens` | Rationale |
|---|---|---|
| `openai` | 4096 | GPT-4o / GPT-4 Turbo have 128K context; 4096 is conservative |
| `anthropic` | 4096 | Claude 3.5 has 200K context; 4096 matches OpenAI parity |
| `ollama` | 2048 | Local models vary; 2048 is a safe floor |
| `local` | 2048 | Custom endpoints vary widely; 2048 is a safe floor |

Existing providers (pre-migration) retain 2000 to avoid changing behavior
for users who haven't tuned their settings.

### 2.5 Model Max Detection

Rather than letting the user set an arbitrary value that could exceed the
model's actual context window, the application attempts to detect the
model's maximum output tokens from the provider's API. The detected value
acts as a **hard ceiling** on the user's configurable `maxTokens`.

#### 2.5.1 Detection Strategy by Provider

| Provider | Detection method | Reliability |
|---|---|---|
| **OpenAI-compatible** | `GET /v1/models` or `GET /v1/models/{model}` — inspect response for `max_tokens`, `context_length`, `context_window`, or `max_output_tokens` | Low. Most proxies (Alibaba, OpenRouter) do not expose this metadata. Standard OpenAI does not include it. |
| **Anthropic** | No programmatic detection available. Anthropic's API does not expose a `/models` endpoint with context limits. | None. |
| **Ollama** | `POST /api/show` with `{"name": model}` — inspect `model_info` for context parameters like `num_ctx` | Moderate. Works on recent Ollama versions. |
| **Local / custom** | No standard detection. Custom endpoints have no metadata contract. | None. |

#### 2.5.2 Detection Lifecycle

Detection is attempted at two points:

1. **On "Test Connection"** — the existing test handler also queries model
   metadata. If detected, the result is returned alongside the connection
   success message. The user sees: *"Connection successful. Model max
   output: 4,096 tokens."*
2. **On manual "Detect" button** — a small button next to the `maxTokens`
   field in Settings triggers a targeted detection request without
   re-testing the full connection.

The detected value is stored in a new `model_max_tokens` column on
`llm_providers` (nullable — `NULL` means "not detected").

#### 2.5.3 When Detection Succeeds

The UI shows the detected model max as an informational label next to the
`maxTokens` input:

> Model max: **4,096 tokens**
> Your setting (max 4,096): [____]

The input's HTML `max` attribute is set to the detected value. The user
cannot enter a number higher than what the model supports.

#### 2.5.4 When Detection Fails

The UI shows a warning with a conservative default:

> ⚠ Could not determine model's token limit.
> Using conservative default of 2,048 tokens to avoid truncation.
> Your setting: [____]

The input has no hard ceiling (the user can increase it), but the warning
and low default discourage overly aggressive values. The user can still set
a higher value based on their own knowledge of the model (e.g., "I know
this model supports 8K").

The detection result (`model_max_tokens = NULL`) is persisted so the
warning reappears whenever the user edits this provider — they don't have
to re-discover the limitation on every visit.

#### 2.5.5 The Ceiling Rule

When `model_max_tokens` is known, the effective `maxTokens` sent to the
API is:

```go
effectiveMax := min(userMaxTokens, modelMaxTokens)
```

This prevents the user from accidentally setting a value the model can't
honor. If detection later succeeds for a provider that previously had
`model_max_tokens = NULL`, the stored `model_max_tokens` is updated and
the user's setting is clamped to it on the next save.

### 2.6 Where the Value Flows

```
Settings UI (number input + Detect button)
    ↓
app.go TestLLMProviderConnection / DetectModelLimits bindings
    ↓
services.TestLLMProvider / services.DetectModelMaxTokens
    ↓  (API call to provider's /models or /api/show endpoint)
    ↓
models.LLMProvider.ModelMaxTokens (detected ceiling, nullable)
    +
models.LLMProvider.MaxTokens (user setting, clamped to ceiling)
    ↓
services.NewLLMClient(provider) → effectiveMax = min(maxTokens, modelMaxTokens)
    ↓
OpenAIClient / AnthropicClient struct .maxTokens field
    ↓
openAIChatRequest.MaxTokens = c.maxTokens  (was hardcoded 2000)
```

Ollama and Local clients do not currently send `maxTokens` to their
servers. They will gain the ability to do so, making them consistent with
OpenAI and Anthropic.

### 2.7 UI Placement

In Settings → Model Configurations, each provider row already has fields
for name, provider type dropdown, model, base URL, and API key. `Max Tokens`
fits as a number input after the model field, before the base URL.

**When model max is detected:**

> Model max: **4,096 tokens**  
> Max Tokens: [____] ↑↓ (max 4,096)

**When context window is known but model max is unknown:**

> Context window: **131,072 tokens**  
> ⚠ Could not determine model's max output tokens.  
> Max Tokens: [____] ↑↓ (max 131,072)

**When neither is known:**

> ⚠ Could not determine model's token limits.  
> Using conservative default of 2,048 tokens.  
> Max Tokens: [____]

A small "Detect" link/button next to the field re-attempts detection
without requiring a full connection test. The existing "Test Connection"
button also triggers detection as a side effect.

---

## 3. Implementation Plan

### 3.1 Data Model — `models.LLMProvider`

**File:** `pkg/models/llm_provider.go`

Add three new fields:

```go
type LLMProvider struct {
    // ... existing fields ...
    MaxTokens      *int  `json:"max_tokens,omitempty"`       // user setting
    ModelMaxTokens *int  `json:"model_max_tokens,omitempty"` // detected ceiling
    ContextWindow  *int  `json:"context_window,omitempty"`   // model's total context
}
```

- `MaxTokens` — the user's configured value. Pointer to distinguish "not
  set" from "explicitly set to zero."
- `ModelMaxTokens` — the detected ceiling from the provider's API.
  `NULL` means detection failed or hasn't been attempted. When set, the
  effective `maxTokens` sent to the API is `min(userSetting, modelMax)`.
- `ContextWindow` — the model's total context window (input + output).
  `NULL` means unknown. When set, `maxTokens` is clamped to this value to
  prevent invalid API requests.

### 3.2 Database Migration

**File:** `pkg/models/database.go`

Per the migration safety rules in `AGENT_READ_FIRST.md` §3.2:

```go
ensureColumn("llm_providers", "max_tokens", "INTEGER DEFAULT 2000")
ensureColumn("llm_providers", "model_max_tokens", "INTEGER")  // NULL = not detected
ensureColumn("llm_providers", "context_window", "INTEGER")     // NULL = unknown
```

- **Additive only** — no DROP, RENAME, or destructive ALTER
- **Safe default** — existing providers keep 2000 (current behavior)
- **`ensureColumn`** — idempotent, won't re-run if column already exists
- **No data loss** — existing rows are untouched

### 3.3 Detection Service

**File:** `pkg/services/llm_provider.go` (new function)

A new function attempts to detect a model's maximum output tokens:

```go
func DetectModelMaxTokens(provider *models.LLMProvider) (*int, error)
```

It dispatches to provider-specific detection logic:

| Provider type | Strategy |
|---|---|
| `openai` | `GET /v1/models` — search response for model with matching name, inspect `max_tokens`, `context_length`, or `context_window` |
| `anthropic` | Returns `nil, nil` — no detection available |
| `ollama` | `POST /api/show` with `{"name": model}` — inspect `model_info` for context params |
| `local` | Returns `nil, nil` — no detection available |

When detection succeeds, the caller updates `provider.ModelMaxTokens` in
the database. When it fails (or returns nil), `ModelMaxTokens` remains
`NULL`.

This function is called from two places:

1. **`TestLLMProviderConnection`** (existing) — as a side effect of
   testing the connection, also attempt detection. The test result message
   includes the detected max when available.
2. **A new `DetectModelMaxTokens` Wails binding** — a lightweight call
   that only does detection, no full connection test. Used by the
   "Detect" button in Settings.

### 3.4 Wails Bindings — `app.go`

**File:** `app.go`

**`LLMProviderSetting` struct** (line 174) — add all three token fields:

```go
type LLMProviderSetting struct {
    // ... existing fields ...
    MaxTokens      int `json:"maxTokens"`
    ModelMaxTokens int `json:"modelMaxTokens"`  // 0 = not detected
    ContextWindow  int `json:"contextWindow"`   // 0 = unknown
}
```

**`ListLLMProviders`** (line 184) — populate all three fields from the provider
model, defaulting to 0 when `NULL`.

**`CreateLLMProvider`** (line 213) — add `maxTokens int` parameter.

**`UpdateLLMProvider`** (line 218) — add `maxTokens int` parameter.

**New binding: `DetectModelMaxTokens`** — triggers detection for a
specific provider and returns the result:

```go
func (a *App) DetectModelMaxTokens(id uint) (int, error)
```

Returns the detected value or 0 if detection failed. The frontend uses
this to populate the "Model max" label and set the input ceiling.

### 3.5 Service Layer — `pkg/services/llm_provider.go`

**File:** `pkg/services/llm_provider.go`

`CreateLLMProvider` and `UpdateLLMProvider` accept and persist
`maxTokens`. The existing function signatures accept individual parameters
for name, model, base URL, and API key. Add `maxTokens` as an `*int`
parameter following the same nullable pattern as `model` and `baseURL`.

When `ModelMaxTokens` or `ContextWindow` is detected, the effective value
sent to the LLM API is clamped at client construction time:

```go
effectiveMax := configuredMaxTokens
if contextWindow != nil && *contextWindow > 0 {
    effectiveMax = min(effectiveMax, *contextWindow)
}
if modelMaxTokens != nil && *modelMaxTokens > 0 {
    effectiveMax = min(effectiveMax, *modelMaxTokens)
}
```

This clamping happens in `NewLLMClient`, not in persistence — the user's
configured value is stored as-is, and the ceilings are applied at
construction time. This preserves the user's intent even if detection
later succeeds for a previously-undetected model.

### 3.6 LLM Clients

Each client struct gains a `maxTokens` field, populated from the provider
config at construction time. The hardcoded `2000` literals are replaced with
`c.maxTokens`.

#### 3.6.1 OpenAI — `llm_openai.go`

**Struct (line 18):**
```go
type OpenAIClient struct {
    baseURL    string
    apiKey     string
    model      string
    maxTokens  int       // new
    httpClient *http.Client
}
```

**Constructor (line 25):** Read `provider.MaxTokens`, default to 4096 if nil.

**`ChatCompletionWithPayload` (line 92):**
```go
// Before:
MaxTokens: 2000,
// After:
MaxTokens: c.maxTokens,
```

#### 3.6.2 Anthropic — `llm_anthropic.go`

**Struct (line 16):** Add `maxTokens int`.

**Constructor (line 54):** Read `provider.MaxTokens`, default to 4096 if nil.

**`ChatCompletion` (line 102) and `ChatCompletionWithPayload` (line 200):**
Replace `MaxTokens: 2000` with `MaxTokens: c.maxTokens`.

#### 3.6.3 Ollama — `llm_ollama.go`

Ollama currently has no `maxTokens` field in its request struct. The
`ollamaChatRequest` struct and the `ChatCompletion` method need to be
updated to optionally include `max_tokens` (Ollama's API parameter name)
when a value is configured.

**Struct (line 16):** Add `maxTokens int`.

**Constructor (line 40):** Read `provider.MaxTokens`, default to 2048 if nil.

#### 3.6.4 Local — `llm_local.go`

Same pattern as Ollama. The local client sends OpenAI-compatible requests
but currently does not include `maxTokens`.

**Struct (line 18):** Add `maxTokens int`.

**Constructor (line 26):** Read `provider.MaxTokens`, default to 2048 if nil.

### 3.7 Frontend — `SettingsView.svelte`

**File:** `frontend/src/SettingsView.svelte`

Add a number input for `maxTokens` in the provider form, between the model
field and the base URL field. The existing pattern for other fields (model,
base URL) uses local state bound to the form, with save/create handlers that
pass values through the Wails bindings.

Label: *"Max Tokens"* with hint: *"Maximum tokens per response. Increase for
reasoning models (Qwen, o1, DeepSeek-R1). Default: 4096 for OpenAI/Anthropic,
2048 for local/Ollama."*

The `ListLLMProviders` binding already returns a typed struct — the new
`maxTokens` field will be available to the frontend automatically after
regenerating Wails bindings.

### 3.8 Wails Bindings Regeneration

After changing any Go-side binding signature (adding a parameter), the
Wails bindings must be regenerated:

```bash
~/go/bin/wails generate module
```

This updates `frontend/wailsjs/go/main/App.js` to reflect the new function
signatures.

---

## 4. Migration Safety

Per `AGENT_READ_FIRST.md` §3.2, all migrations follow strict rules. This
change is fully compliant:

| Rule | Compliance |
|---|---|
| Only ADD COLUMN | ✓ — `ensureColumn("llm_providers", "max_tokens", ...)`, `ensureColumn("llm_providers", "model_max_tokens", ...)`, and `ensureColumn("llm_providers", "context_window", ...)` |
| Never DROP, RENAME, or ALTER | ✓ — no existing columns touched |
| Never `CREATE TABLE … AS SELECT` | ✓ — no table recreation |
| Use `ensureColumn()` for additive changes | ✓ — follows established pattern |
| Default value preserves existing behavior | ✓ — `DEFAULT 2000` matches current hardcoded value |

No `runMigration()` wrapper is needed — `ensureColumn()` already records
its success in `schema_migrations` and will not re-run.

---

## 5. Risk Assessment (per `AGENT_READ_FIRST.md` §4)

### 5.1 Risk Categories

| Category | Assessment |
|---|---|
| **Answer accuracy** | **Improves.** Reasoning models get the budget they need to complete answers. Users receive correct responses instead of errors. |
| **Answer delivery** | Low risk. Token budget change is a request parameter, not a pipeline change. |
| **Data safety** | None. No credential or data exposure. |
| **Data integrity** | None. Additive migration with safe default. |
| **User trust** | **Improves.** Eliminates unexplained failures on complex questions. |
| **Regression** | Very low. Existing providers keep 2000 and `model_max_tokens = NULL` until the user explicitly tests or detects. No behavior change for untouched configurations. |
| **Detection complexity** | Low-Medium. Detection is best-effort. When it fails (common for OpenAI/Anthropic), the user sees a clear warning and a conservative default. The core feature (configurable `maxTokens`) works without detection. |
| **UX/Comfort** | **Improves.** Transparent setting with sensible defaults. |

### 5.2 Cost Implications

Increasing `maxTokens` may increase per-request costs for paid APIs
(OpenAI, Anthropic). This is:
- **User-visible and configurable** — the user chooses the value
- **Documented in the UI** — the hint text explains that higher values use
  more tokens
- **Defaulted conservatively** — 4096 is a modest increase from 2000, not
  the model's full context window

### 5.3 Absolute Rules Compliance

Per §4.0, no absolute rules are violated:
- No write statements against data sources
- No API key or password logging
- No destructive schema changes

### 5.4 Decision

**Proceed.** The change is purely additive, fully configurable, and
addresses a concrete failure mode that prevents users from getting answers.
Default-preserving migration ensures zero impact on existing configurations.

---

## 6. Pre-Implementation Checklist (per `AGENT_READ_FIRST.md` §6)

- [x] **Fundamental goal alignment** — directly improves answer delivery for
      complex questions with reasoning models
- [x] **Risk/reward documented** — this document serves as the analysis
- [x] **All files identified** — `llm_provider.go` (model), `database.go`,
      `app.go`, `llm_provider.go` (service), `llm_client.go`, `llm_openai.go`,
      `llm_anthropic.go`, `llm_ollama.go`, `llm_local.go`,
      `SettingsView.svelte`
- [x] **SQL execution safety preserved** — no query execution changes
- [x] **LLM prompt intact** — no prompt modification
- [x] **Migration safety** — additive `ensureColumn` with safe default
- [x] **Wails bindings** — identified all changed function signatures;
      regeneration step documented

---

## 7. Graceful Degradation Note

The incident that motivated this enhancement demonstrated that the existing
error handling is correct. When the LLM fails to produce a parseable
response (due to truncation or null content), the discussion engine:

1. Retries with explicit JSON formatting instructions
2. Falls back to a user-friendly clarification message
3. Never exposes raw JSON or stack traces to the user

This behavior is preserved and is the correct response to genuine model
failures. The `maxTokens` setting reduces the *frequency* of these failures
by giving the model enough budget, but the graceful degradation remains
intact for cases where the model genuinely cannot answer.

---

## 8. Appendix: Incident Trace

### Round 1

```
POST /chat/completions
  model: qwen/qwen3.7-plus
  max_tokens: 2000
  messages: [system prompt + conversation history + user question]

Response:
  finish_reason: "length"
  usage.completion_tokens: 2002
    reasoning_tokens: 1969
    output_tokens: 33 (truncated mid-sentence)
  content: {"action":"answer","answer":"This query is a step in
           the right direction because it successfully introduces"

→ json.Unmarshal fails (incomplete JSON)
→ extractJSONFromResponse + parseLenientJSON fail
→ system retries with: "Your previous response was not valid JSON..."
```

### Round 2

```
POST /chat/completions
  model: qwen/qwen3.7-plus
  max_tokens: 2000
  messages: [... + retry instruction]

Response:
  finish_reason: "length"
  usage.completion_tokens: 2002
    reasoning_tokens: 2000 (all of them)
    output_tokens: 2
  content: null

→ content is nil → no JSON to parse
→ discussion engine shows: "I received a response I couldn't
  understand. Could you try rephrasing your question?"
```

### What would have succeeded

With `maxTokens: 4096`:

```
POST /chat/completions
  model: qwen/qwen3.7-plus
  max_tokens: 4096
  messages: [...]

Response:
  finish_reason: "stop"
  usage.completion_tokens: ~3500
    reasoning_tokens: ~2000
    output_tokens: ~1500 (complete, valid JSON)
  content: {"action":"answer","answer":"Yes, this query is
           better because..."}  ← complete, valid JSON

→ json.Unmarshal succeeds
→ handleAnswer renders the answer
→ user sees a formatted response
```