# Headless vs GUI Result Divergence - Analysis

**Date:** 2026-08-19
**Revision:** v4 - added explicit success criterion, continued investigation.

---

## Success Criterion

**The results from headless and GUI must be absolutely indistinguishable.**
An observer looking at an answer must not be able to tell whether it came from
the GUI or headless mode.

This means:
- Same answer quality, same level of detail, same tone.
- Same query behavior (exploration rounds, retries, error recovery).
- Same rendering of results (tables, charts, summaries).
- Same behavior for edge cases (empty results, errors, clarifications).

This is not aspirational - it is a hard requirement. Headless mode is the
programmatic face of the same engine. Divergence means the engine is not
actually shared, even if the code path appears to be.

---

## Investigation v4 - Full Code-Path Trace (2026-08-19)

### Method

Every file in the call chain was read and compared side-by-side:
`main.go` → `headless.go` / `app.go` → `ProcessUserMessageWithContext` →
`AgenticLoop.Run` → `ChatCompletionWithTools` (OpenAI as representative provider).

### Finding: The code paths are provably identical

#### Entry point → `ProcessUserMessageWithContext`

| Concern | GUI (`app.go`) | Headless (`headless_handlers.go`) | Same? |
|---|---|---|---|
| Context creation | `context.WithCancel(context.Background())` | `context.WithCancel(context.Background())` | ✅ |
| `onStream` callback | `runtime.EventsEmit(..."llm:stream"...)` (always non-nil) | `events = append(events, ev)` (non-SSE) / `writeEvent("stream", ...)` (SSE) (always non-nil) | ✅ |
| `onPhase` callback | `runtime.EventsEmit(..."processingPhase"...)` | `nil` (non-SSE) / `writeEvent("phase", ...)` (SSE) | ✅ (unused by pipeline) |
| Pipeline call | `services.ProcessUserMessageWithContext(ctx, id, msg, onPhase, onStream)` | Same call via `processMessage` | ✅ |

#### Inside `ProcessUserMessageWithContext`

| Concern | Behavior | Same for both? |
|---|---|---|
| `conversation.StreamingEnabled` check | If false → `streamCallback = nil` | ✅ |
| `LoopInput.OnStream` | Equals `streamCallback` (nil when streaming disabled) | ✅ |
| `LoopConfig` | Same `safetyMode`, `maxRounds`, `maxRetries`, `contextWindow`, `agentConfig` | ✅ |
| `LoopInput.Messages` / `LoopInput.Tools` | Built by same `buildToolLlmMessages` / `buildTools` | ✅ |
| LLM client | Same `NewLLMClient(provider)` call | ✅ |
| Pipeline timeout | Same `GetTimeoutSetting("pipeline_timeout_seconds", 180)` | ✅ |

#### Inside `AgenticLoop.Run` (when streaming disabled)

| Concern | Behavior | Same? |
|---|---|---|
| LLM call | `a.LLMClient.ChatCompletionWithTools(ctx, messages, tools)` (blocking) | ✅ |

#### Inside `ChatCompletionWithTools` (OpenAI representative)

| Concern | Behavior | Same? |
|---|---|---|
| HTTP client | `http.Client{Timeout: 300 * time.Second}` | ✅ |
| Request construction | Same JSON body (messages, tools, temperature, max_tokens) | ✅ |
| API endpoint | Same `c.baseURL + "/chat/completions"` | ✅ |
| Headers | Same `Authorization`, `Content-Type` | ✅ |
| Response parsing | Same `json.Unmarshal(body, &response)` | ✅ |

### Conclusion

**There is zero code-path divergence.** Every function, every if-branch, every
parameter is the same between GUI and headless for a fresh conversation with
default settings. The only remaining variables are **outside the code**:

1. **The LLM provider itself** - same code, but the provider may respond
   differently to an HTTP request from a Wails process vs a standalone Go
   process (different TLS fingerprint, different source port range,
   different timing, rate-limiting per-IP or per-process).

2. **Environment** - `godotenv.Load()` uses the current working directory;
   the working directory differs between GUI (app bundle) and headless
   (terminal). System env vars (`HTTP_PROXY`, `SSL_CERT_FILE`, etc.) may
   also differ.

3. **Test methodology** - a subtle difference in how the test was conducted
   (e.g., the GUI conversation may have been created with different default
   settings, or a provider was cached differently).

### Next Step: Diagnostic instrumentation

Since the code paths are identical, the **only productive code change** is to
instrument both paths to capture the exact LLM request and response. This
doesn't change behavior - it only adds observability. Once we have the byte-
for-byte request/response from both paths, we'll see exactly where they
diverge.

Minimal instrumentation needed:
1. Write the full `requestJSON` and `responseJSON` to log files (gated behind
   env var) - these are already captured inside `ChatCompletionWithTools`.
2. Include the raw HTTP response body for non-OK status codes.
3. Compare the two log files for the same question.

This is a ~10-line change to `llm_client.go` or each provider, not a pipeline
refactor.

---

## Investigation v5 — Test Harness Analysis (2026-08-19)

### Scope

Full analysis of the test harness at `/Users/bflbarlow/Go/yourql-test-harness`,
including every internal package (`main.go`, `driver`, `db`, `config`, `fixture`,
`eval`, `grade`, `report`), all testdata (model profiles, data source profiles,
scenarios, conversation logs), and comparison against the YourQL code responsible
for LLM communication.

### Root Cause Found

**The divergence is caused by a missing fallback in the `local` provider's
blocking path, combined with the harness's inability to use streaming mode.**

#### How it works

The harness uses `provider: "local"` for LM Studio models. The `local` provider
does NOT use native tool calling — it injects tool instructions into the system
prompt as a text-based "fallback protocol" (`injectFallbackInstructions` /
`fallbackInstructions` in `llm_ollama.go`, called by `llm_local.go`).

There are two paths for the `local` provider to call the LLM:

**Blocking path** (`ChatCompletionWithTools` → `ChatCompletionWithPayload`):
```go
// llm_local.go, lines 91-97
if resp.StatusCode == http.StatusOK {
    var response struct { ... }
    json.Unmarshal(body, &response)
    reply := response.Choices[0].Message.Content
    if reply != "" {
        return reply, ...
    }
}
return "", string(jsonData), string(body), nil  // *** DROPS CONTENT ***
```

When the LLM returns HTTP 200 but `choices[0].message.content` is empty or
missing, the response body is returned in `responseJSON` (third return value)
but is **ignored** by `ChatCompletionWithTools`, which only passes `content`
to `parseFallbackResponse`. Empty content → empty `ChatMessage` → loop emits
"I received an incomplete response."

**Streaming path** (`ChatCompletionWithToolsStreaming`):
```go
// llm_local.go, lines 265-268
msg := parseFallbackResponse(contentBuf.String())
msg.FinishReason = "stop"
// Fallback: surface raw output when parsing produced nothing.
if msg.Content == "" && len(msg.ToolCalls) == 0 && contentBuf.Len() > 0 {
    msg.Content = truncateString(contentBuf.String(), 2000)  // *** HAS FALLBACK ***
}
```

The streaming path has a raw-output fallback: if the parser produces nothing
but the model emitted text, the raw SSE text is surfaced as `Content`. This
means partial, malformed, or non-OpenAI-format responses still reach the
agentic loop.

#### Why the harness hits this

The harness always sets `StreamingEnabled: false` — it's hardcoded in
`modelRun`:
```go
convSettings := config.ConversationSettings{
    Summarize: false, VizEnabled: false, StreamingEnabled: false,
    ...
}
```

And the `ModelScenario` struct has no `streaming_enabled` field:
```go
type ModelScenario struct {
    ...
    Summarize   bool `json:"summarize"`
    VizEnabled  bool `json:"viz_enabled"`
    // NO StreamingEnabled field
    ...
}
```

If the GUI user has `StreamingEnabled: true` for their LM Studio conversations,
the streaming path is used — which has the raw-output fallback and can handle
the LLM's response. The harness has no way to test streaming because the field
doesn't exist in either `ModelScenario` or `ModelProfile`.

#### Evidence from test output

```
lm-studio-qwen3.6-35b -- classicmodels-sales-rep-scorecard
Status: completed | Latency: 140912ms | Tokens: 0
Reply: "I received an incomplete response. Could you try rephrasing your question?"
Failure mode: clarification
```

141 seconds elapsed (the model DID produce a response — otherwise it would
have returned much faster), 0 tokens recorded (the content was dropped by the
parser), and the loop emitted `ResponseEmptyTruncated`.

Compare with OpenRouter models (which use native tool calling via the `openai`
provider, NOT the fallback protocol): they produce rich, successful results
with exploration rounds and substantive responses.

### Concrete Gaps in the Test Harness

These are the **specific, actionable things missing from the harness** that
prevent it from producing results indistinguishable from the GUI:

#### 1. `StreamingEnabled` not supported (CRITICAL)

- `ModelScenario` has no `streaming_enabled` field
- `ModelProfile` has no `streaming_enabled` default
- `ConversationSettings` hardcodes `StreamingEnabled: false` with no override
- The `local` provider's streaming path has a raw-output fallback; blocking
  does not. Without streaming, local models hit the empty-content path.

**Fix:** Add `streaming_enabled` to `ModelScenario` and `ModelProfile`;
honor it in `modelRun`.

#### 2. `context_window` not supported (HIGH)

- `ModelProfile` has no `context_window` field
- `LLMProvider` struct has no `ContextWindow` field
- `SeedProvider` doesn't seed `context_window`
- The agentic loop can't detect context overflow without it
- Local models (7B–35B) have limited windows; overflow causes silent empty
  responses that look identical to the parse-failure case above.

**Fix:** Add `context_window` to `ModelProfile`; seed it in `SeedProvider`.

#### 3. `max_tokens` not supported (MEDIUM)

- `ModelProfile` has no `max_tokens` field
- Always defaults to 2000 via `effectiveMaxTokens`
- Complex scenarios may need higher limits for the fallback protocol's
  multi-round text responses

**Fix:** Add `max_tokens` to `ModelProfile`; seed it in `SeedProvider`.

#### 4. No `agent_loop_config` seeding (MEDIUM)

- The harness never seeds `agent_loop_config` overrides
- The GUI user may have custom prompts for local models that improve
  protocol compliance

**Fix:** Add optional `agent_loop_config` block to `ModelProfile`.

#### 5. No raw LLM response logging (HIGH)

- When the `local` provider returns empty content, `responseJSON` is discarded
- The harness has no visibility into what the LLM actually returned
- Debugging requires instrumenting the YourQL binary itself

**Fix:** Log the raw `requestJSON`/`responseJSON` from `ChatCompletionWithTools`
when the harness detects a 0-token result. Or add a `YOURQL_DIAGNOSTIC=1` env
var to YourQL itself (as recommended in Investigation v4).

#### 6. `Summarize` and `VizEnabled` hardcoded in `modelRun` defaults

- `Summarize: false` is the base, overridden by scenario
- But if a scenario wants `Summarize: true`, the summarization timeout needs
  to be high enough (harness already handles this via
  `SummarizationTimeoutSeconds`)

### Recommended Fix Path

**For the harness (immediate):**
1. Add `streaming_enabled` to `ModelScenario` JSON schema and `modelRun`
2. Add `context_window` to `ModelProfile` JSON schema and `SeedProvider`
3. Add `max_tokens` to `ModelProfile` JSON schema and `SeedProvider`
4. Add raw response capture when results are 0-token

**For the OpenRouter divergence (explained below):**
5. Add `agent_loop_config` seeding to `ModelProfile` so the harness can match the
   GUI's custom prompts (especially the confidence instruction)
6. Add data source table descriptions, business rules, and custom system prompt
   to `DataSourceProfile`

**Multi-turn conversations are out of scope.** The harness is single-turn by
design — one question, one response. The configuration parity items above
must be sufficient to close the divergence without follow-ups.

**For YourQL (separate fix):**
1. ~~Add the same raw-output fallback to the `local` provider's blocking
   `ChatCompletionWithPayload` that the streaming path already has
   (surfacing `body` as `Content` when `choices[0].message.content` is empty)~~
   **✅ Fixed 2026-08-19** — `llm_local.go` now surfaces raw body text when
   the OpenAI-format parse succeeds but `content` is empty, matching the
   streaming path's fallback.

---

## Investigation v6 — OpenRouter Divergence (2026-08-19)

### The Pattern

OpenRouter models (deepseek-v4-pro and deepseek-v4-flash) do NOT produce the
same results as the GUI. Unlike local models (which return 0 tokens), OpenRouter
models successfully use the tool-calling protocol — they explore the schema,
run queries, and call `respond_to_user`. But their responses are fundamentally
different from the GUI:

| Run | Model | Behavior | Verdict |
|---|---|---|---|
| pro-run01 | deepseek-v4-pro | 3 exploration rounds, then asked 4 clarification questions about methodology | **Wrong** — clarification instead of answer |
| pro-run02 | deepseek-v4-pro | 4 exploration rounds, then asked 2 clarification questions about "judgment calls" | **Wrong** — clarification instead of answer |
| flash-run01 | deepseek-v4-flash | 1 exploration, then final query: `SELECT status, COUNT(*) FROM orders GROUP BY status` (trivial/wrong) | **Wrong** — wrong query, model acknowledged it didn't answer the question |
| flash-run02 | deepseek-v4-flash | 0 tokens — empty truncated response | **Wrong** — no response |

None of these resemble the correct scorecard the GUI produces.

### Root Cause: The Default Confidence Instruction

The system prompt includes this instruction (from `agent_loop_config.go`,
`instructions.4`, used verbatim when no overrides are seeded):

> "4. **Confidence rule — before your final answer, check that you are
> confident in every part of it.** You must be confident that: (a) you
> understand what the user means, (b) your methodology and metric definitions
> are correct, (c) you have correctly interpreted your query results, and (d)
> you can complete the task within your exploration budget. **If you are
> uncertain about ANY of these, use `respond_to_user` to ask the user a
> targeted question. Never guess — asking one question is always better than
> delivering a wrong answer.**"

The scorecard question asks for genuinely ambiguous metrics:
- "average payment collection days" — the `payments` table isn't linked to
  individual orders; the model must make a methodological choice
- "percentage of orders shipped on time" — requires defining "on time"
  (shippedDate vs requiredDate? Do cancelled orders count?)
- "product line diversification" — how to count distinct product lines per rep?

Faced with these ambiguities, models following the confidence instruction
**correctly choose to ask rather than guess**. This is not a bug — it's the
instruction working as designed.

In the GUI, this is fine: the model asks a question, the user answers it,
and the conversation continues. In the headless test harness, **the
conversation ends after one message** — the clarification is the final
output, not a stepping stone to the answer.

### Why the GUI Produces the Answer

The GUI user likely has one or more of these configuration differences:

1. **Custom `instructions.4` override** — after finding the default confidence
   instruction too conservative, they may have softened or removed it (e.g.,
   "If uncertain, state your assumptions and proceed" instead of "ask the user").

2. **Business rules on the data source** — definitions like
   "On-time shipping: shippedDate <= requiredDate, exclude Cancelled orders"
   resolve the ambiguities before the model sees them.

3. **Table/column descriptions** — human-readable descriptions like
   "The `payments` table records payments grouped by customer, not by individual
   order. Payment collection days should be averaged per customer."

Multi-turn conversation (answering clarifications) is explicitly NOT a factor
here — the harness is single-turn by design. The divergence must be closed
through configuration parity alone.

### Concrete Gaps for OpenRouter Parity

#### 1. `agent_loop_config` seeding (CRITICAL)

- `ModelProfile` has no `agent_loop_config` field
- The harness seeds a blank `agent_loop_config` table
- Default instruction "Never guess" makes models ask questions on ambiguous
  questions
- The GUI user's overrides (which make the model decisive) are not replicated

**Fix:** Add optional `agent_loop_config` map to `ModelProfile`; seed it
before creating conversations.

#### 2. Data source table descriptions & business rules (HIGH)

- `DataSourceProfile` has no fields for `system_prompt`, `business_rules`,
  `table_descriptions`, or `column_descriptions`
- These are stored in the `config` JSON column of `data_sources`
- The harness seeds a minimal config (exploration safety + rounds only)
- Without these, the model lacks domain knowledge that resolves ambiguities

**Fix:** Add `system_prompt`, `business_rules`, `table_descriptions`,
and `column_descriptions` to `DataSourceProfile`; include them in
`buildConfigJSON`.

#### 3. Multi-turn conversation NOT in scope (stated 2026-08-19)

- The harness is intentionally single-turn only — one question, one response
- Follow-up conversations (clarification → user reply → answer) are out of
  scope for the current harness design
- This means the confidence instruction MUST be tuned so that models produce
  answers, not clarifications, on a single pass
- The harness should NOT implement follow-up turns; instead, the
  `agent_loop_config` and data source configuration must bridge the gap

## Executive Summary

The headless HTTP API and the GUI Wails bindings **share the exact same core
pipeline** (`ProcessUserMessageWithContext` → `AgenticLoop.Run` →
`executeSQLWithMode`). Both entry points call the same service function with
the same arguments. Every behavioral decision (streaming vs blocking, tool
definitions, context window, summarization, chart rendering) is made **inside
that shared function**, not in the caller. There is **no code-path
divergence** in the pipeline.

Nevertheless, fresh conversations in both modes - same **data source**, same LLM
provider, same question - produce **wildly different
results**. The GUI produces good, relevant answers. The headless mode
consistently produces `"I received an incomplete response. Could you try
rephrasing your question?"` and other non-answers. This is a systematic
divergence, not LLM non-determinism, and its cause has **not yet been
isolated** in this analysis.

---

## Important Corrections from Prior Revisions

### v1 → v2: Streaming/blocking divergence RETRACTED

v1 claimed the GUI checks `conversation.StreamingEnabled` before passing
`onStream` while headless always streams. **This was incorrect.** Both paths
always pass `onStream`, and the `StreamingEnabled` check lives inside
`ProcessUserMessageWithContext` (`pkg/services/discussion_engine.go`). The
streaming vs blocking decision is centralized and identical for both entry
points. No divergence.

### v2 → v3: History-as-cause DISPROVEN

v2's Finding 1 attributed divergence to the GUI accumulating conversation
history while headless starts fresh. **Testing has disproven this:**
fresh conversations in both GUI and headless were used, yet results still
diverge wildly. This explanation is retracted. See Finding 1 below for
the actual observed behavior.

---

## Finding 1 - Systematic LLM Failure in Headless Mode (CRITICAL, unresolved)

### Test conditions (confirmed by tester)

- Same **data source** (the user's database being queried - e.g. PostgreSQL, MySQL, etc.).
- Same LLM provider and model.
- Same **application database** (`~/.yourql/yourql.db` - no `-db-path` divergence).
- Same user question.

> **Terminology note:** The *data source* is the user's external database (the one
> being queried). The *application database* is the local SQLite file that stores
> conversations, settings, and credentials. The LLM prompt, schema, and behavior
> depend on the **data source**, not the application database.
- **Fresh conversations in both GUI and headless** (zero prior history).
- Pipeline timeout configured identically for both paths.

### Observed result

- **GUI:** Produces good, relevant answers.
- **Headless:** Consistently produces `"I received an incomplete response.
  Could you try rephrasing your question?"` and other non-answers that do
  not address the question.

### What the error message means

The message `"I received an incomplete response. Could you try rephrasing
your question?"` is the `response.empty_truncated` configurable text
(`pkg/services/agent_loop_config.go` line 76). It is emitted when the
agentic loop receives an LLM response with **0 completion tokens and no
tool calls** (see `pkg/engine/loop.go`):

```go
// Empty response - truncated or malformed.
if len(response.ToolCalls) == 0 {
    if pendingFinal != nil { ... return }
    category := "empty_response"
    detail := "model returned 0 tokens and no tool calls"
    clarMsg := cfg.ResponseEmptyTruncated
    // If PromptTokens >= contextWindow * 90%, it's "context_overflow" instead
    ...
    return a.emitClarification(category, detail, clarMsg, input)
}
```

This means the LLM is returning **no content at all** - zero completion
tokens, no tool calls - when called from headless mode, but returns
good results when called from GUI mode with the same prompt.

### Why this is puzzling

For fresh conversations querying the **same data source**, the prompt sent to the LLM
should be **identical** between GUI and headless. Both paths construct it
via the same `buildToolLlmMessages` → `buildToolSystemPrompt` chain, using
the same schema, skills (none for fresh), data source config, agent loop
config, and conversation settings.

The fact that GUI consistently succeeds while headless consistently fails
means **something** differs in how the LLM client is invoked. Yet the code
path (streaming/blocking, timeout, tool definitions) is shared:

- `conversation.StreamingEnabled` is checked centrally in
  `ProcessUserMessageWithContext` (see retraction above).
- Both paths always pass a non-nil `onStream` callback.
- The timeout (`pipeline_timeout_seconds`) is read from the same DB.
- The LLM client (`NewLLMClient`) is created identically from the same
  provider config.

### Potential but unverified causes

1. **LLM provider behavior difference between streaming and blocking**:
   The conversation's `streaming_enabled` setting (schema default: `0` =
   false) determines whether `ChatCompletionWithToolsStreaming` or
   `ChatCompletionWithTools` is used. Both paths should respect this
   identically, but if the *provider implementation* handles streaming vs
   blocking tool calls differently (Anthropic streaming accumulates tool
   calls incrementally; OpenAI streams partial JSON; Ollama may have bugs),
   the *same* conversation setting could produce different tool-call
   parsing between provider code paths. This needs provider-level
   investigation.

2. **Headless server HTTP timeout**: The `http.Server` in `headless.go`
   has no `ReadTimeout`/`WriteTimeout` set (uses Go defaults, which may
   interact with long-running SSE responses).

3. **Connection/pooling difference**: The headless server is a long-running
   process, while the GUI runs inside Wails. The LLM client's HTTP
   transport or connection pooling could behave differently.

4. **Environment variable difference**: `.env` loading (`godotenv.Load()`)
   happens in both modes, but system environment variables (e.g.,
   `HTTP_PROXY`, `SSL_CERT_FILE`) could differ between a GUI launch and a
   CLI launch.

5. **Race condition in headless state management**: The `headlessServer`
   uses mutex-guarded `inflight` and `cancels` maps. A subtle concurrency
   issue could cause unexpected behavior.

### Severity

**Critical** - this is a systematic failure that makes headless mode
unusable for testing, since its results do not match the GUI's behavior.

---

## Finding 2 - Database Path Divergence (MEDIUM IMPACT, confirmed)

### What happens

- **GUI** (`app.go` `startup`): always `models.ConnectDatabase()` - the
  pointer-file-aware default (`~/.yourql/yourql.db` or the pointer target).
- **Headless** (`headless.go` `runHeadless`): can use
  `models.ConnectDatabaseAt(dbPath)` when `-db-path` is provided.

### Consequence

If headless is launched with `-db-path /other.db`, it has different
`discussion_defaults`, `agent_loop_config`, `skills`, LLM providers, and
data sources - any of which changes the prompt sent to the LLM.

### Severity

**Medium** - genuine divergence but controllable via flag.

### Mitigation

Use the same **application database** (`~/.yourql/yourql.db`). The user confirmed this was the case in their
testing, so it is not the cause of the current divergence.

---

## Finding 3 - `MaxContextMessages` Default Is Always Overwritten (LOW IMPACT, bug)

### What happens

`CreateConversationWithDefaults` has a bug. The system-floor check reads
the **in-memory** `conv.MaxContextMessages` (always `0` because
`CreateConversation` does not populate it), rather than the **database**
value. The floor of `5` always fires, overwriting any user-configured
`discussion_defaults.max_context_messages`.

```go
conv, err := CreateConversation(...)  // conv.MaxContextMessages = 0

if defaults.MaxContextMessages != nil {
    UpdateConversationMaxContextMessages(conv.ID, *defaults.MaxContextMessages)  // DB updated, not conv
}

if conv.MaxContextMessages == 0 {  // STILL 0 - in-memory never updated
    UpdateConversationMaxContextMessages(conv.ID, 5)  // ALWAYS fires
}
```

### Severity

**Low** - affects GUI and headless equally (both use
`CreateConversationWithDefaults`). Not a divergence between them, but the
`discussion_defaults.max_context_messages` setting has no effect.

### Fix direction

Track the effective value before the floor check rather than reading the
stale in-memory struct.

---

## Finding 4 - `Summarize`/`VizEnabled` Types & Schema Defaults (LOW IMPACT, corrected)

### Correction

v1 stated these are `*bool`. They are plain `bool` (`pkg/models/conversation.go`).

Schema defaults (`pkg/models/database.go`):

| Column | Default |
|---|---|
| `summarize` | `0` (false) |
| `viz_enabled` | `1` (true) |
| `streaming_enabled` | `0` (false) |

Note `viz_enabled` defaults to **true** - charts are enabled for new
conversations by default.

### Consequence

Consistent between GUI and headless. No divergence.

---

## Finding 5 - `MaxMessages` Is Never Read by the Pipeline (confirmed)

`conversations.max_messages` is persisted and has Wails/headless bindings,
but `ProcessUserMessageWithContext` and `AgenticLoop.Run` never read it.
Only `MaxContextMessages` is used for context-window limiting.

This is dead configuration - a bug, but not a divergence between paths.

---

## Summary Table

| Finding | Impact | Diverges GUI↔headless? | Status |
|---|---|---|---|
| 1. Systematic LLM failure in headless | **Critical** | **Yes - unresolved** | Active |
| 2. Database path | Medium-High | Yes (if `-db-path` differs) | Confirmed |
| 3. MaxContextMessages overwritten to 5 | Low | No | Bug |
| 4. Summarize/Viz types & defaults | Low | No | Corrected |
| 5. MaxMessages unused | Low | No | Confirmed |

---

## Root Cause Assessment (revised)

The shared pipeline (`ProcessUserMessageWithContext` → `AgenticLoop.Run`)
is code-path identical for both entry points. With the same **data source**, provider,
schema, and question, the prompt sent to the LLM should be
identical. Yet the LLM returns content for GUI calls and `0 tokens` for
headless calls.

This points to a difference in **how the LLM client communicates with the
model**, not in what prompt is sent. The most likely areas to investigate
are:

1. **Provider-level streaming vs blocking** - while both paths resolve to
   the same `conversation.StreamingEnabled` value, the underlying provider
   implementation may behave differently between `ChatCompletionWithTools`
   and `ChatCompletionWithToolsStreaming` in ways that affect tool-call
   parsing for subsequent rounds.

2. **HTTP transport and timeouts** - the headless server's HTTP handling
   or the process lifecycle may differ from the Wails app's goroutine
   model.

3. **LLM provider connection pooling** - long-running headless process
   may accumulate stale connections or hit provider rate limits in ways
   the GUI's per-message connection model does not.

---

## Recommended Next Steps (v4)

1. **Instrument the LLM call** — Add a flag (env var `YOURQL_DIAGNOSTIC=1`) that
   writes the full request JSON and full response JSON to
   `~/.yourql/diag/` with timestamps. Run the same question through both
   GUI and headless, then compare the files byte-for-byte. The divergence
   point will be visible immediately.

2. **Test with the diagnostic output** — Once the instrumentation is in
   place, the root cause will be one of:
   - **Same request, different response** → the LLM provider is behaving
     differently (rate limiting, model version, TLS fingerprinting).
   - **Different request, same provider** → there's a hidden difference in
     prompt construction (unlikely given the trace above, but the
     instrumented log will catch it).
   - **Network error swallowed** → the LLM call is failing but the error
     is not being surfaced properly.

3. **Once the root cause is identified** — The fix will be targeted and
   minimal. Most likely candidates:
   - Add explicit HTTP transport configuration (disable connection pooling
     for LLM calls, set consistent TLS config).
   - Ensure `godotenv.Load()` uses an absolute path, not cwd.
   - Add retry-with-backoff for transient provider failures.

4. **After the fix** — Run the full test harness (see
   `documentation/testing/`) in both GUI and headless modes and confirm
   results are indistinguishable.