# Risk/Reward Analysis Log

> Companion to `AGENT_READ_FIRST.md`. Agents should document every
> non-trivial risk/reward decision here, most recent entry first, so the
> project accumulates a real history of trade-off decisions.

This log uses the template defined in `AGENT_READ_FIRST.md` §4.4.

---

<!-- Add new entries above this line -->

## 2026-08-18 — Engine/Agentic Loop Extraction Plan (pkg/engine/ black-boxing)

- **Change:** A three-document plan (`FUNCTIONALITY_SILO_DEFINITIONS.md`,
  `FUNCTIONALITY_SILO_TARGET.md`, `FUNCTIONALITY_SILO_PLAN.md`) proposing to
  extract the discussion engine and agentic tool-calling loop
  (`discussion_engine.go`, `agentic_loop.go`, plus the pure-function halves of
  `sql_execution.go`) into a new `pkg/engine/` package, isolated behind 8
  interfaces (`LLMClient`, `QueryExecutor`, `OutputHandler`,
  `ConversationStore`, `LLMProviderStore`, `DataSourceStore`,
  `SchemaIntrospector`, `ConfigurationProvider`). Implementation is staged
  across 9 phases (types → pure-function moves → adapters → prompts/tools →
  new loop → new orchestrator → cutover → delete old code → tests), with the
  explicit intent of zero behavior change — a mechanical refactor, not a
  feature change. This entry documents the risk/reward analysis required by
  `AGENT_READ_FIRST.md` §4.2 before any phase is executed, because the plan
  touches `discussion_engine.go`, `agentic_loop.go`, and `sql_execution.go`
  directly (all three are named §4.2 pause triggers) and rewrites the tool
  definitions and system-prompt construction.

- **Fundamental goal impact:**
  **No intended change to answer accuracy or delivery.** This is explicitly
  a structural refactor, not a feature or prompt change — every function
  that produces LLM-facing text, SQL, or rendered output is required to move
  character-for-character. The *reward* is indirect: once isolated, the loop
  becomes unit-testable with mocked LLM/DB dependencies, which the project
  currently has almost none of (5 tests total, in `sql_execution_test.go`).
  This directly serves the stated pain point ("timeouts, errors, unexpected
  model behavior") by making the loop's round logic, retry logic, and
  one-shot finality rule testable without a live LLM or database — today
  those can only be observed by running the full GUI or headless pipeline.
  **If executed incorrectly, this carries real risk to answer accuracy and
  delivery** — see failure modes below — which is exactly why this plan is
  staged so heavily and why this analysis exists before Phase 0 starts.

- **Risk category(ies):** Answer accuracy (prompt/tool construction moves),
  Answer delivery (the loop's round/retry/streaming logic moves), Data safety
  (the exploration complexity-mode safety control moves call sites), Data
  integrity (none — no schema/migration changes), Regression (existing
  conversations must replay identically), Reliability (Phase 7 is a single
  cutover point with no gradual rollout).

- **Failure mode(s):**
  1. **Exploration safety mode (§1.4) enforcement gap.** Identified during
     doc review: `validateExplorationQuery` (complexity-mode gate) is called
     by `agentic_loop.go` *before* `executeSQLWithMode`, not from inside it.
     A naive extraction that only wraps `executeSQLWithMode` in a
     `QueryExecutor` would silently drop the complexity-mode check once
     `agentic_loop.go` is deleted in Phase 8 — while leaving the read-only
     invariant (§0) intact, since that check *is* inside `executeSQLWithMode`.
     **Mitigated:** `FUNCTIONALITY_SILO_TARGET.md`'s `QueryExecutor` contract
     and `FUNCTIONALITY_SILO_PLAN.md` Phase 3/6 now require
     `QueryExecutorAdapter` to carry an explicit `SafetyMode` (never
     zero-valued) and call `engine.ValidateExplorationQuery` itself. Phase 9
     adds `TestQueryExecutorAdapter_ExplorationSafetyMode` as a named
     regression gate. This must be verified against the live source at
     implementation time, not assumed from the docs.
  2. **Provider-specific tool-call parsing regression (§1.3).** The agentic
     loop's response parsing has been tuned per-provider (OpenAI, Anthropic,
     Ollama, local). Moving this code into `engine/loop.go` risks subtly
     changing behavior for one provider while looking correct for another.
     **Mitigated:** Phase 7 decision gates now require re-running §5.2's
     Multi-Provider Verification (all 4 providers) and §5.3's Multi-Database
     Verification (SQLite + one other driver + one NativeQuerier driver)
     before the cutover commit, not just a generic smoke test.
  3. **One-shot finality or streaming regression.** The loop's one-shot
     finality rule and the streaming/blocking dual code path are exactly the
     kind of behavior that's easy to subtly break during a large copy-paste.
     **Mitigated:** Phase 9 test list explicitly includes
     `TestOneShotFinality` and `TestStreamingVsBlocking` (identical input,
     with/without `OnStream`, asserting identical `LoopOutput`).
  4. **Credential leakage via new adapter code.** New adapter files in
     `services/adapters/` handle `DataSourceMeta`/`LLMProviderMeta` structs
     that carry `Password`/`APIKey` fields as plain strings. New code is
     exactly where a stray debug `log.Printf("%+v", ...)` could violate §3.5
     ("never log API keys or passwords"). **Mitigated:** Phase 3 must include
     an explicit prohibition on logging Meta structs verbatim; code review
     for Phase 3 must grep new adapter files for `log.` / `slog.` calls that
     take a Meta struct as an argument.
  5. **Cutover has no gradual rollout.** Phase 7 swaps `app.go` and
     `headless_handlers.go` to the new orchestrator in one commit; there is
     no feature flag or side-by-side comparison mode. **Mitigated:** Phases
     1–6 are purely additive (old code path stays live and callable) so
     `git revert` of the single Phase 7 commit fully restores the old
     behavior with no cleanup; Phase 0 captures a baseline conversation for
     manual content-parity comparison.
  6. **Charter documentation goes stale.** `AGENT_READ_FIRST.md` §1.2/§1.3/§7
     name `discussion_engine.go` and `agentic_loop.go` by path; both are
     deleted in Phase 8. **Mitigated:** Phase 8 now includes an explicit task
     to update those references in `AGENT_READ_FIRST.md` in the same change,
     per that document's own closing instruction ("a stale charter is worse
     than no charter").

- **Reward:** Enables fast, mocked, deterministic tests of the tool-calling
  loop's hardest-to-reproduce behaviors (round exhaustion, error retry,
  context overflow, one-shot finality, provider-specific parsing) without a
  live LLM API key or database connection. This is the most direct path
  available to reducing the "timeouts, errors, and unexpected model
  behavior" pain point that motivated the request, because those failure
  modes can be scripted and asserted on instead of only observed live.
  Secondary reward: the loop's configuration (rounds, retries, safety mode,
  tool descriptions, prompts) becomes a single serializable `LoopConfig`
  value, opening the door to per-data-source tuning without code changes.

- **Mitigations:** See failure modes above — each has a specific, named test
  or documentation fix. Additionally: every phase is a single, revertible
  commit; no phase before Phase 7 changes any caller's behavior; Phase 8
  (deletion) only happens after Phase 7's manual + automated verification
  passes; the plan explicitly forbids logic changes during the moves
  ("character-for-character" requirement) to keep the diff review tractable.

- **Decision:** **Proceed with changes.** The plan is sound in structure and
  unusually disciplined about zero-behavior-change extraction, but it must
  not be executed as originally drafted — the exploration-safety-mode gap
  (failure mode 1) is a real regression risk against a named §1.4 safety
  control and has been corrected in `FUNCTIONALITY_SILO_TARGET.md` and
  `FUNCTIONALITY_SILO_PLAN.md` as part of this same review. Do not begin
  Phase 0 until an implementer has re-verified failure mode 1's fix against
  the live source (not just the docs), since "code moves faster than docs"
  per `AGENT_READ_FIRST.md`'s own document-precedence note.

---

## 2026-08-16 — Headless Mode (HTTP API) for the External Testing Harness

- **Change:** Add a `-headless -port -db-path` launch mode and a minimal
  localhost-only HTTP API (`documentation/HEADLESS_YOURQL.md`) that reuses
  the existing agentic-loop pipeline. Two new additive Go functions were
  introduced: `models.ConnectDatabaseAt(path)` (open an arbitrary DB file and
  migrate it, bypassing the pointer-file resolution) and
  `services.GetQueriesByConversation(id)` (read the query log back by
  conversation — previously no code did this). The GUI launch path (`wails
  dev`, `wails build`, release binaries) is unchanged; `-headless` defaults to
  `false`.

- **Fundamental goal impact:**
  **None for GUI users.** The GUI code path is untouched — no change to the
  LLM prompt, SQL execution, schema introspection, or result rendering.
  **Indirectly positive** for the fundamental goal: this is the transport the
  external test harness (`TESTING_EXECUTION.md`) needs to exercise the real
  pipeline across a config matrix and surface answer-quality regressions.

- **Risk category(ies):** Data safety (a new open network listener, even on
  loopback), Regression (new startup branch in `main.go`), Reliability (a
  long-lived HTTP server introduces hung-request / port-conflict / panic
  surfaces).

- **Failure mode(s):**
  1. **Listener bound to `0.0.0.0`.** Mitigated by hardcoding `127.0.0.1` in
     `runHeadless`; there is deliberately no bind-address flag, so it cannot
     be widened without a code edit.
  2. **Pipeline panic inside an HTTP handler crashing the process.**
     Mitigated by `recover()` in the message handler returning
     `500 internal_error` rather than letting the panic unwind the server.
  3. **`-headless` reaching a production/GUI build.** Mitigated by the flag
     defaulting to `false`; it is opt-in and never set by `wails dev`/`wails
     build`/release paths.
  4. **`-db-path` pointing at a real `~/.yourql/yourql.db`.** Not fully
     mitigated here — it is the harness's responsibility to only ever pass
     disposable paths; flagged as a loud warning in the harness's own docs.
  5. **Concurrent messages on one conversation interleaving state.**
     Mitigated by a per-conversation single-flight guard (`409
     processing_already_active`); the harness runs scenarios sequentially
     anyway.

- **Reward:** Deterministic, scriptable, config-matrix testing of the real
  pipeline (vs. UI automation) — the primary blocker identified when deciding
  the harness architecture. Also a small diagnostic surface for local
  debugging.

- **Mitigations:** loopback-only bind (no flag to change it); per-handler
  panic recovery; single-flight guard per conversation; no shutdown/kill
  endpoint; `-headless` off by default everywhere; all pipeline work flows
  through the unchanged `ProcessUserMessageWithContext` path (never
  short-circuited to `executeSQLWithMode`).

- **Decision:** Proceed. Low risk given the loopback-only, opt-in, additive
  design; does not touch any `AGENT_READ_FIRST.md` §4.0 absolute rule.

## 2026-08-16 — Discovery: `window.confirm()` / `window.alert()` Are No-Ops in Wails macOS Webview

- **Change:** None — this is a discovery, not a code change. Verified that
  Wails v2.13.0's macOS `WKWebView` does not implement the `WKUIDelegate`
  methods `runJavaScriptConfirmPanelWithMessage` / `runJavaScriptAlertPanelWithMessage`,
  so `window.confirm()` returns falsy and `window.alert()` does nothing.
  Documented in `documentation/SMALL_ISSUES.md` §5.

- **Fundamental goal impact:**
  **None on answer correctness**, but **directly violates the comfort/trust
  secondary goal** (charter §3.9): users click a destructive button
  (delete provider, delete connection, clear all messages, reset agent loop
  settings) and *nothing happens* — no dialog, no action, no error. The
  action is silently not executed, which reads as a broken button and erodes
  trust.

- **Risk category(ies):** User trust, UX/Comfort.

- **Failure mode(s):**
  - **Silent no-op on destructive actions.** Every `if (!confirm(...)) return`
    guard exits early because `confirm()` returns falsy. Four call sites are
    affected (`SettingsView.svelte:431/720/1128`, `App.svelte:495`). The
    positive side: this is fail-safe — it blocks the action rather than
    executing it without confirmation, so no data is lost.
  - **Future call sites re-introducing the bug.** Mitigated by documenting the
    pattern in `SMALL_ISSUES.md` with the custom-modal replacement (already
    implemented for the DB-switcher feature).

- **Reward:** None directly — this is a framework limitation, not a feature.
  Documenting it prevents future code from relying on `confirm()`/`alert()`
  and gives the four broken call sites a clear, copy-pasteable fix path.

- **Mitigations:** DB-switcher already switched to a custom Svelte modal
  (`{#if dbConfirm}` reusing `skill-editor-overlay` CSS). The four
  pre-existing call sites remain broken until individually migrated to the
  same pattern (tracked in `SMALL_ISSUES.md`).

- **Decision:** Document and defer the four pre-existing call-site fixes (they
  are fail-safe, not data-loss bugs). New code must not use
  `window.confirm()`/`window.alert()`.

## 2026-08-11 — Diagnostic Logging Feature

- **Change:** Add optional file-based logging (`~/.yourql/yourql.log`) with a
  General Settings toggle and an Export & Clear button. Both `log.Printf` and
  `slog` are redirected via `io.MultiWriter(os.Stderr, logFile)` during
  `startup()` when enabled. Changes take effect after app restart.

- **Fundamental goal impact:**
  **Preserves answer quality.** Logging is purely observational — it captures
  diagnostic output that already exists (stderr) and writes a copy to disk.
  Zero effect on the query pipeline, prompt construction, or SQL execution.
  Answer accuracy is unchanged.

- **Risk category(ies):** Data safety, Regression, UX/Comfort

- **Failure mode(s):**
  1. **Disk fill.** If logging is enabled and the app runs for weeks/months
     without clearing, the log file grows unboundedly. On a full disk, Go's
     file writes will fail gracefully (return error from `Write()`), and the
     app continues running — logs are non-critical. The `MultiWriter` to
     stderr still works. Mitigation: Export & Clear button provides manual
     rotation; the log is plain text (tens of KB per session, not MB).
  2. **Credential leak.** If any `log.Printf` call in `pkg/` includes API keys
     or database passwords, they would be written to the log file in plain
     text. This is the same risk as stderr except now it's persisted. A grep
     of all 47 `log.Printf`/`log.Println`/`log.Print` calls shows none log
     credentials (they log query results, model names, DSN components without
     passwords, row counts, error messages from providers). The existing
     `AGENT_READ_FIRST.md` §3.5 rule "Never log API keys or passwords" already
     covers this. Risk is pre-existing, not introduced.
  3. **File handle leak.** If `logFile` is not closed in `shutdown()`, the FD
     leaks. Mitigation: single `logFile` var, closed in `shutdown()` in a
     `defer`-style pattern.
  4. **Race on export.** `ExportAndClearLog` reads then truncates the file
     while the logger may be writing. Mitigation: use `logFile.Seek(0,0)` +
     `io.ReadAll` then `logFile.Truncate(0)` + `logFile.Seek(0,0)`, all under
     a mutex. In-flight log writes between read and truncate may be lost;
     this is acceptable (the user is explicitly choosing to export-and-clear).

- **Reward:** Users can diagnose issues without launching from Terminal.
  Support/development flow improves: "send me your log file" replaces "run
  from Terminal and tell me what you see." The feature is opt-in, default-off,
  and visually marked as a diagnostic tool.

- **Mitigations:**
  - Default-off (no logging without explicit user action)
  - Export & Clear button prevents unbounded growth
  - `io.MultiWriter` to stderr ensures terminal users still see logs
  - Restart-required for toggle changes (avoids runtime complexity)
  - No new dependencies or schema migrations

- **Decision:** Proceed.

<!-- Add new entries above this line -->

## 2026-08-10 — Discovery: Conversation History Context Exhaustion

- **Observation:** After a turn with 9 exploration queries (NPV analysis),
  all subsequent messages in the same conversation produced empty responses.
  The tool result tables (up to 50 formatted rows each, ~3KB per result)
  consumed the model's entire context window, leaving no room for the new
  user question.
- **Impact:** The conversation is bricked — user must start a new thread.
  Current mitigations (`maxContextMessages` cap, 50-row tool result limit)
  are insufficient because they don't control per-message size.
- **Document:** See `DISCUSSION_CONTEXT_ISSUES.md` for full analysis and
  solution options (digest tool results, row cap reduction, context budget,
  auto-retry on empty).
- **Status:** ✅ Implemented 2026-08-10. Both changes in `agentic_loop.go`:
  `formatToolResult` now produces a digest (row count, columns, sample rows,
  numeric stats); `stripReasoningFromArgs` trims replayed reasoning in
  `buildToolMessages`. See `DISCUSSION_CONTEXT_ISSUES.md` for details.

## 2026-08-10 — Prevent `respond_to_user` After Query Errors (Summarization)

- **Change:**
  - Track `lastQueryHadError` in the agentic loop. When summarization is on,
    `respond_to_user` is rejected if the previous `query_database` returned
    an error — the model must retry with corrected SQL before it can respond.
    Feedback is injected and the loop continues.
  - Augment MySQL "Unknown column" errors with schema context: detect the
    error pattern, look up the real column location, and suggest the correct
    JOIN in the error message fed back to the model.
  - New config field `ResponseQueryErrorRequiresRetry`.

- **Fundamental goal impact:**
  **Improves answer correctness.** The model was observed claiming results
  it never retrieved ("Here are the results..." with no query executed).
  Now it's forced to fix and retry or exhaust the error budget.

- **Risk category(ies):** Answer accuracy (positive), User trust (positive).

- **Failure mode(s):**
  - Model could exhaust the error budget without ever fixing the query.
    Mitigation: the existing `ResponseLoopExhausted` fallback fires — user
    sees "I wasn't able to complete this request." That's better than a
    false claim of success.
  - Schema hint augmentation could produce misleading suggestions for
    complex multi-table errors. Mitigation: conservative pattern matching —
    only augments Error 1054 (Unknown column), not arbitrary errors.

- **Reward:** Eliminates the "confident non-answer" failure mode for
  summarization users. Schema hints help small/local models recover from
  trivial JOIN errors they currently can't self-correct.

- **Mitigations:** No schema changes. No data source interaction beyond
  read-only queries. Additive only — new config field, new error augmentation
  function, one new boolean in the loop state.

- **Decision:** Proceed. A model claiming non-existent results is a direct
  violation of the fundamental goal (answer correctness). The guard is
  narrow (summarization + error-state) and non-summarization users are
  unaffected.
- **Resolution:** Implemented and verified 2026-08-10. Three-part fix: (A) `lastQueryHadError` guard blocks `respond_to_user` after query errors when summarization is on, (B) `augmentSQLError()` appends schema hints to MySQL Error 1054 messages, (C) `ResponseQueryErrorRequiresRetry` config message guides the model back to correction. Non-summarization users are unaffected.

## 2026-08-10 — Fix: Collapsible Table Lost After Summarization Fix

- **Change:**
  - Revert the `assistantResp.Summary = nil` approach from the previous fix.
    Instead, when a summary exists, let `AssistantResponse.ToHTML()` own the
    rendering entirely — it renders the summary text AND wraps the results
    table in a `<details>` collapsible block. `textAboveTableHTML` is only
    used for `respondText` when no summary is present.

- **Fundamental goal impact:**
  **Restores correct UX.** The previous fix cleared `Summary` to prevent
  duplicate rendering, but `ToHTML()` uses `Summary != nil` as a switch for
  wrapping the results table in `<details>`. Clearing it caused the table
  to render inline, fully visible, instead of behind "View raw results".

- **Risk category(ies):** UX/Comfort (regression fix).

- **Failure mode(s):**
  - None. This is a return to the pre-existing `ToHTML()` behavior, which
    has been in production since the tool-calling protocol was introduced.
    The duplicate-rendering problem is already solved by the `textAboveTableHTML`
    conditional — when summary exists, textAboveTableHTML stays empty and
    ToHTML() handles everything once.

- **Reward:** Summary text appears once above the table, table is collapsible
  behind "View raw results." Exactly the original design intent.

- **Mitigations:** No new code paths. One-line deletion (`assistantResp.Summary = nil`)
  and one conditional change (summary branch skips textAboveTableHTML).

- **Decision:** Proceed. Direct regression fix.

## 2026-08-10 — Fix: Duplicate Summary Rendering (ToHTML + textAboveTable)

- **Change:**
  - When `summary` is rendered as `textAboveTableHTML` (the summarization
    toggle path), clear `assistantResp.Summary` before calling `ToHTML()`
    so the summary is not rendered a second time inside the results section.

- **Fundamental goal impact:**
  **Fixes a regression from the previous fix.** The prior change moved the
  summary into `textAboveTableHTML` but left `AssistantResponse.Summary`
  populated, causing `ToHTML()` to render the identical text again. Users
  saw the same summary twice in one bubble — text-above-table and inside
  the results block.

- **Risk category(ies):** UX/Comfort (positive — eliminates duplicate text).

- **Failure mode(s):** None. The nil check before `ToHTML()` already handles
  `Summary == nil` gracefully. The only change is when—not whether—the
  summary appears.

- **Reward:** Single, clean summary above the results table. No duplicate.

- **Mitigations:** One-line change. No schema, no data source, no prompt
  modification.

- **Decision:** Proceed. Obvious regression fix.

## 2026-08-10 — Prevent Bare `respond_to_user` When Summarization Is On

- **Change:**
  - When `conversation.Summarize` is enabled and the model has already run
    at least one exploration query, reject a bare `respond_to_user` (i.e.,
    one not paired with `query_database is_exploration: false`) and inject
    feedback telling the model it must deliver a final query. The loop
    continues — the model gets another round to do the right thing.
  - Applies to both the explicit `respond_to_user` tool-call path and the
    plain-text response path (when the model emits text with no tool calls).
  - New configurable feedback message:
    `ResponseSummarizationRequiresFinalQuery`.

- **Fundamental goal impact:**
  **Improves answer correctness.** Previously, when summon was on, the
  model could promise "I'll run the final query" via `respond_to_user` and
  never actually deliver data. The user got a chatty non-answer. Now the
  loop injects corrective feedback and forces resolution.

- **Risk category(ies):** Answer accuracy (positive), User trust (positive).

- **Failure mode(s):**
  - Injected feedback could loop indefinitely if the model truly cannot
    produce a final query. Mitigation: the existing `totalRoundCap` still
    applies — if the model exhausts all rounds, `ResponseLoopExhausted`
    fires as before.
  - Could reject a legitimate clarification (e.g., "That data isn't
    available, would you like me to look at something else?") after
    explorations. Mitigation: the feedback message explicitly says "If you
    cannot answer with data, use respond_to_user to ask a clarifying
    question" — the model can still clarify on the next round.

- **Reward:** Eliminates silent non-answer delivery. Summarization toggle
  users get actual data + summary every time. Non-summarization users are
  unaffected (the guard only fires when `Summarize` is true).

- **Mitigations:** No schema changes. No data source interaction. Additive
  only — one new config field, one new default, one conditional before
  existing `handleRespond` calls. The plain-text path (line ~850) and
  tool-call path (line ~1053) both receive the same guard.

- **Decision:** Proceed. The model was observed delivering a naked promise
  without data for a summarization-enabled user. The charter's priority #1
  is correctness of the answer — a promise without delivery is incorrect.
  The guard is narrow (summarization + explorations-run + no final query)
  and leaves legitimate clarifications intact.

## 2026-08-10 — Fix: Double Answer in Single Chat Bubble (respond_to_user + Summarization)

- **Change:**
  - In `renderToolQueryResults`, when both a `respondText` (from the LLM's
    batched `respond_to_user` tool call) and a `summary` (from
    `summarizeResults`) are present, use the summary as the text-above-table
    instead of concatenating both. When only one is present, use whichever
    is available.
  - The summarization call now includes a system message instructing the LLM
    to produce a standalone answer — not a follow-up or supplement — since
    the summary now replaces `respondText` in the chat bubble.

- **Fundamental goal impact:**
  **Improves answer delivery.** Previously the user saw two competing
  analyses in one bubble (the model's inline `respond_to_user` text and the
  post-query summarization), both of which signed off per the active skill.
  This confused users and degraded the primary goal of serving a clear,
  trustworthy answer. Now the user sees one coherent answer.

- **Risk category(ies):** Answer accuracy (positive), UX/Comfort (positive).

- **Failure mode(s):**
  - If summarization fails, the user sees only the fallback text and loses
    the inline `respond_to_user` commentary that the model produced.
    Mitigation: `respondText` is already preserved as a separate field;
    when summary is nil, `respondText` is still shown as before.
  - If summarization produces a lower-quality answer than the model's
    inline `respond_to_user`, the user sees the worse version. Mitigation:
    the summarization prompt already instructs the LLM to reference specific
    numbers and produce a thorough analysis; quality should be comparable.
    If this becomes a recurring issue, a future enhancement could use
    `respondText` as a fallback when summarization returns empty.

- **Reward:** Eliminates the confusing double-message-in-one-bubble UX.
  Summarization now truly *replaces* the inline commentary with a polished,
  post-query analysis, which is exactly what the toggle is meant to do.

- **Mitigations:** No schema changes. No data source interaction. Change is
  isolated to one conditional in `renderToolQueryResults` and one additional
  system message in `summarizeResults`. The `respondText` field is not
  removed — it remains available for future use (e.g., tech-details toggle).

- **Decision:** Proceed. The current behavior is unambiguously broken — two
  signed messages in one bubble with overlapping content. The summarization
  toggle's purpose is to give the user a single, polished analysis; this
  change makes it do exactly that.

## 2026-08-10 — LLM Finish Reason & Stream Diagnostics

- **Change:**
  - Add `FinishReason` field to `ChatMessage` struct so LLM clients can pass the
    real API-level stop reason ("stop", "length", "tool_calls", "max_tokens",
    "end_turn", "content_filter") through to the agentic loop.
  - Plumb the actual finish/stop reason from each LLM client
    (OpenAI non-streaming + streaming, Anthropic non-streaming + streaming,
    Ollama, Local) into `ChatMessage.FinishReason`.
  - In the agentic loop, populate `TechDetail.Response.FinishReason` from the
    real value instead of hard-coding `"stop"` / `"tool_calls"`.
  - Wrap the streaming callback in `runAgenticLoop` to capture `StreamDone`
    events and populate `TechDetail.Stream` (chunk count, byte count).

- **Fundamental goal impact:**
  **None — purely additive diagnostics.** Does not touch the LLM prompt,
  SQL pipeline, schema introspection, or answer delivery. The only behavioral
  change is a tech-details `finish_reason` field that was previously
  hard-coded to `"stop"` now reflects the real API value.

- **Risk category(ies):** None triggered. This is an additive observability
  change. No existing code path is modified to behave differently.

- **Failure mode(s):**
  - If `FinishReason` is empty in the message (e.g. Ollama fallback path
    doesn't set it), the agentic loop falls back to `"stop"`. Mitigation:
    explicit fallback `if response.FinishReason == ""` → `"stop"`.
  - Streaming callback wrapper could error and prevent UI updates.
    Mitigation: wrapper is a thin pass-through; all original events are
    forwarded unmodified.

- **Reward:** Agents and developers can definitively diagnose why an LLM
  response stopped early (token limit, content filter, normal stop). This
  directly answers "why did it cut off?" without guesswork. Stream stats
  (chunk/byte count) provide additional context for incomplete responses.

- **Mitigations:** No schema changes. No data source interaction. All LLM
  client changes are additive (one new field set). Agentic loop change is a
  one-line replacement plus a thin callback wrapper.

- **Decision:** Proceed. Zero risk to the fundamental goal pipeline. The
  hard-coded `"stop"` has already caused at least one undiagnosable
  mid-thought truncation (see Round 2 of sales-rep classification).
  Immediate diagnostic value with no downside.

## 2026-08-07 — Export Enhancement (Print, HTML, Markdown)

- **Change:**
  - Replace the single print button with a three-option dropdown: Print to PDF, Export as
    HTML, Export as Markdown.
  - HTML export generates a self-contained `.html` file from the conversation messages
    (already stored as rendered HTML in the DB).
  - Markdown export converts assistant message HTML to Markdown using a targeted converter
    for YourQL's known HTML patterns.
  - Existing `ExportConversationPDF()` is preserved unchanged; small `@media print` CSS
    improvements reduce page waste.

- **Fundamental goal impact:**
  **None.** Export is a post-answer, read-only operation on `conversation_messages`. It
  does not touch the LLM pipeline, SQL execution, or schema introspection.

- **Risk category(ies):** User trust (positive — more export options). No negative risk
  categories triggered.

- **Failure mode(s):**
  - HTML export could produce invalid HTML for edge-case message content (e.g., unclosed
    tags from truncated streaming output). Mitigation: use the same `content` column the
    frontend already renders via `{@html}` — if it renders in the app, it exports.
  - Markdown converter could mishandle nested HTML (e.g., a `<table>` inside a
    `<details>`). Mitigation: targeted patterns only; fall back to stripping tags and
    emitting plain text for unrecognized patterns.
  - File save dialog cancellation must not leave partial state. Mitigation: check for
    empty path return before writing.

- **Reward:** Users get a single-page, searchable, portable conversation export. Fills a
  clear gap — the print dialog alone is insufficient for archiving or sharing.

- **Mitigations:** No schema changes. No data source interaction. Additive-only (existing
  print path untouched). Light-mode-only CSS in HTML export (avoids dark-mode print
  issues).

- **Decision:** Proceed. Zero risk to the fundamental goal pipeline. Clear user value.
  Minimal code surface (~280 lines total across 3 files).

## 2026-08-05 — Summarization Digest & Context Truncation

- **Change:**
  - Summarization now receives a compact digest (column stats + 10 representative rows) instead of the full result set.
  - `formatSQLResultsForLLM` (the `[PREVIOUS QUERY RESULTS]` context replay) now uses a similar compact digest instead of dumping up to 200 rows.
  - Summarization failures are surfaced as a visible "⚠️ Summary unavailable" chip instead of being silently swallowed.
  - Tool transcript replay (`formatToolResult`, 50-row cap) is left unchanged.
  - `MaxContextMessages` defaults to 5 (was 0/unlimited) with a hard cap of 15.

- **Fundamental goal impact:**
  - **Improved correctness.** Context-window exhaustion was causing empty/incomplete LLM responses.
    The digest keeps the LLM aware of prior results without blowing tokens. If the LLM genuinely
    needs a specific prior row, it can re-query — which is more reliable than trusting a stale
    cached result.
  - **Improved delivery.** Summarization failures are now visible to the user. The digest makes
    summarization calls more reliable by reducing token load.

- **Risk category(ies):** Answer accuracy, answer delivery, user trust.

- **Failure mode(s):**
  - The LLM could lose fine detail about a prior result and produce a less-informed follow-up answer.
    Mitigation: the tool transcript replay still carries 50 rows; most follow-ups only need schema-level
    awareness.
  - A user asking "what was in row 47?" may not get an answer from context alone. Mitigation: this is
    better answered by re-querying the database than by trusting a stale cache.

- **Reward:** Eliminates the primary cause of empty/incomplete LLM responses (context exhaustion).
  Makes summarization reliable for large result sets. Reduces per-conversation token costs.

- **Mitigations:** Tool transcript replay unchanged. Digest includes column stats for structure
  awareness. Hard cap of 15 context messages prevents even worst-case bloat.

- **Decision:** Proceed. The risk of lost detail is outweighed by the reliability improvement —
  incomplete responses and silent summarization failures are worse outcomes than the LLM needing
  to re-query for a specific row.

## 2026-08-17 — Clarification Failure-Mode Classification (Finding 3)

- **Change:**
  - Added structured `error_category` signals to the clarification path so the
    headless harness (and any automated grader) can distinguish failure modes
    without parsing prose. Three categories: `empty_response`, `context_overflow`
    (heuristic: prompt tokens ≥ 90% of context window + 0 completion), and
    `loop_exhausted`.
  - `handleClarification` now writes `error_category` and `error_message` to the
    `queries` tracking table.
  - User-facing prose now differs per mode (`response.empty_truncated` for
    transient empty responses, `response.context_overflow` for capacity
    exceedance, `response.loop_exhausted` for non-convergence).
  - Added `contextWindow *int` parameter to `runAgenticLoop` for accurate
    context-overflow detection.
  - `LLMResponse` gained `FailureCategory` / `FailureDetail` fields (additive).

- **Fundamental goal impact:**
  - **Improved delivery.** Previously 17% of conversations ended with the same
    opaque "incomplete response" message. Now the user gets a mode-specific
    hint and the harness gets a machine-readable signal.
  - **No impact on answer accuracy.** Only the already-failed path is touched;
    successful answers are untouched.

- **Risk category(ies):** Answer delivery (improved), user trust (improved),
  regression (managed — see below).

- **Failure mode(s):**
  - `context_overflow` classification is a heuristic (prompt tokens ≥ 90% of
    context window). A transient empty response on a high-token prompt could be
    misclassified as context overflow. Mitigation: the message text is a
    suggestion ("try breaking into smaller parts"), not a definitive diagnosis.
    Harness should treat the category as a strong signal, not absolute.
  - The new `LLMResponse` fields could be nil/empty from pre-existing callers.
    Mitigation: `handleClarification` only writes them when non-empty; all
    existing calls remain zero-value-safe.

- **Reward:** The single most common error path (17% of harness conversations)
  now gives the user, the developer, and the harness actionable signal about
  what went wrong. Directly addresses Finding 3 of the 2026-08-16 test harness
  analysis.

- **Mitigations:**
  - All changes are additive (new fields, new config keys, new query-record
    writes). No control flow changed.
  - `go vet` clean across all touched packages.
  - Existing `LLMResponse` callers (renderSQLError, etc.) unaffected — they
    zero-value the new fields.

- **Decision:** Proceed. Small, additive change with clear testing signal value.
