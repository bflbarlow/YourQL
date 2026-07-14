# YourQL — Technical Analysis & Enhancement Plan

This document catalogs concrete improvements that should be applied to the
YourQL codebase (`/Users/bflbarlow/Wails/YourQL`). The goal is to make the app
simpler to maintain, faster to reason about, and cleaner for end users.
Recommendations are grouped by area and each item is written so that it can be
executed as an independent change.

Legend:
- 🗑  = delete (dead code)
- ✂️  = simplify / refactor
- 🐞 = correctness / bug
- 🎨 = UX / UI polish
- 🔒 = security
- ⚡ = performance

---

## 1. High-Level Findings

The project began life as a multi-user, org/workspace-scoped web service (Gin +
JWT + email + Stripe + password/lockout policy, etc.) and was later repurposed
as a single-user Wails desktop app. Roughly **~60% of the Go code is dead**:
none of it is reachable from `main.go` → `App` methods, and the frontend never
exercises it.

Concretely:

| Area | LoC (Go) | Actually used by the Wails app? |
| --- | --- | --- |
| `pkg/controllers/*` (Gin HTTP handlers) | ~3,000 | ❌ **None**. `grep -r "pkg/controllers"` returns zero non-self references. |
| `pkg/services/auth.go`, `mail.go`, `organization.go`, `workspace.go`, `workspace_user.go`, `workspace_context.go`, `user.go` | ~2,400 | ❌ Only invoked by the dead controllers. |
| `pkg/services/integration_logger.go` | 290 | ❌ Not referenced. |
| `pkg/services/json_utils.go` (`isValidJSON`) | 11 | ❌ Only used by dead controller. |
| `pkg/services/llm_tester.go` | 213 | ✅ Used via `TestLLMProvider`. Keep. |
| `pkg/services/llm_local.go` (GGUF CLI mode) | 485 | ⚠️ Only HTTP mode is exercised; `os/exec` code path is dead. |
| `pkg/services/llm_mock.go` | 103 | ⚠️ Reachable via provider type `"mock"`, but the UI only offers openai/anthropic/ollama/local. |
| `pkg/models/*` (login, organization, workspace_*, saved_query, integration_log, user, query…) | ~350 | ⚠️ Tables are still migrated but never read/written by the app flow. |
| `pkg/environment/env.go` | 184 | ❌ Loads JWT/SMTP/Stripe/DB env vars that nothing consumes at runtime. |
| `pkg/configuration/*` (`Lockout`, `Email`, `Password`, `Workspace`, `LLM.MaxTokens`, …) | ~350 | ⚠️ Only `SQLQuery` sub-config is actually read at runtime. |
| `pkg/utils/utils.go` (`ValidatePasswordStrength`, `ValidateEmail`, `GenerateTicketCode`) | 69 | ❌ Not used from the app. |
| `pkg/utils/sanitizer.go` | 303 | ❌ Only used by dead controllers. |
| `pkg/utils/slug.go` | 92 | ❌ Only used by dead workspace service. |
| Wails-app `app.go` methods | 467 | ✅ In use. |
| Discussion engine + LLM clients + SQL execution + introspection | ~4,000 | ✅ In use. Some duplication (see below). |

**Estimated deletion after this analysis: ~7,500 lines of Go**, roughly half
the backend, plus dead migrations, dead env vars, and dead frontend CSS
sections. This alone will make the codebase dramatically simpler to navigate
and audit.

The frontend is small (three Svelte files, ~3,700 lines total) but
`SettingsView.svelte` is monolithic and has multiple half-finished features
(query runner UI, schema-preview second variant, modal + detail-view
duplication) that should be removed or extracted.

---

## 2. Dead Code to Delete

### 2.1 🗑 `pkg/controllers/` (entire package)
- Files: `auth.go`, `db_connection.go`, `discussion.go`, `llm_provider.go`, `organization.go`, `workspace.go` (~3,033 LoC).
- Depends on Gin (`github.com/gin-gonic/gin`), which is only pulled in for these controllers. **`main.go` does not import Gin and never starts an HTTP server** — the Wails runtime is the only front-end transport.
- Action: delete the folder outright, then remove `github.com/gin-gonic/gin` from `go.mod`. Also drop `github.com/gin-contrib/sse`, `github.com/go-playground/validator/v10`, and the other Gin transitive deps.

### 2.2 🗑 `pkg/services/auth.go` (JWT, password login, magic codes, lockout)
- Nothing in `app.go` authenticates a user — the app is single-user local desktop with a hard-coded workspace/user id of `1` (see `app.go:111,141,158,193,262,279`).
- Depends on `pkg/configuration.Lockout`, `pkg/environment.Jwt_secret`, and `golang-jwt/jwt/v5`. Remove all three transitively.

### 2.3 🗑 `pkg/services/mail.go` (`SendConfirmationEmail`, `SendPasswordResetEmail`, `LogEmail`, SMTP dial)
- No caller in `app.go`. Only used by `pkg/controllers/auth.go` (also being deleted).
- Removes ~285 LoC + all SMTP env-var handling.

### 2.4 🗑 Workspace / Organization / User services
- `pkg/services/workspace.go` (808 LoC), `workspace_user.go` (441), `workspace_context.go` (373), `organization.go` (448), `user.go` (169) — all only used by the deleted controllers and by each other.
- The Wails app calls only these workspace-ish functions: `IsWorkspaceMember`, `CheckPermission` (indirectly, from `services/conversation.go`, `db_connection.go`, `llm_provider.go`). See §3.1 for how to remove the whole check.

### 2.5 🗑 `pkg/services/integration_logger.go` + `pkg/models/integration_log.go`
- Zero call sites. The `integration_logs` table migration in `pkg/models/database.go` should also be removed.

### 2.6 🗑 `pkg/services/json_utils.go`
- Only symbol (`isValidJSON`) is used by the dead controller `pkg/controllers/workspace.go`. Delete file.

### 2.7 🗑 `pkg/services/llm_mock.go`
- The `mock` provider is not offered in the UI dropdown (`SettingsView.svelte` lines 583–586: openai/anthropic/ollama/local). `ValidateMockResponse` is never referenced. The `case "mock"` branches in `NewLLMClient` and `TestLLMProvider` can be removed with it.
- If the mock is desired for internal testing, move it to `pkg/services/llm_mock_test.go` (test-only build tag). Otherwise delete.

### 2.8 🗑 `pkg/environment/env.go` (184 LoC)
- Loads JWT, SMTP, Stripe, DB, workspace-encryption, LLM master-key, and org-features env vars. None of these are consumed by the running app.
- The `.env` file at the project root is likewise obsolete. Delete `.env` and the `godotenv` dependency.

### 2.9 🗑 Most of `pkg/configuration/`
- `LockoutConfig`, `PasswordConfig`, `EmailConfig`, `WorkspaceConfig`, and `LLMConfig` (except `TimeoutSeconds`) are not read at runtime. Only `SQLQueryConfig` (`DefaultLimit`, `ExplorationDefaultLimit`, `QueryLengthThreshold`) is used, in `sql_execution.go`.
- Reduce `configuration/config.go` to just the SQL-query defaults, or inline the three constants directly into `sql_execution.go`.
- Delete `pkg/configuration/email.go` (211 LoC of email templates).

### 2.10 🗑 `pkg/utils/`
- `sanitizer.go` (303 LoC): only used by the deleted auth controller. Delete.
- `slug.go` (92 LoC): only used by the deleted workspace service. Delete.
- `utils.go` (69 LoC): `ValidatePasswordStrength`/`ValidateEmail`/`GenerateTicketCode` are all controller-only. Delete file.
- After this, `pkg/utils/` can be removed entirely.

### 2.11 🗑 Dead models
Files with no runtime read/write path (only the DDL in `pkg/models/database.go` uses them, and the corresponding tables aren't queried):
- `login.go` (`logins` table — inserted by deleted `services/auth.go` only)
- `organization.go` (org tables aren't even created in `database.go`)
- `saved_query.go` (never read/written)
- `user.go` (only default admin insert on migrate; whole users table is unused)
- `workspace_invitation.go`, `workspace_role.go`, `workspace_setting.go`, `workspace_user.go`, `workspace_user_role.go`, `workspace.go` (all only used by deleted services)
- Correspondingly, drop the `CREATE TABLE IF NOT EXISTS` blocks for: `workspace_settings`, `workspace_invitations`, `workspace_roles`, `workspace_user_roles`, `users`, `logins`, `saved_queries`, `integration_logs`, and the "add default user"/"add default workspace member" bootstrap in `migrate()`.

### 2.12 🗑 Dead function in `pkg/services/discussion_engine.go`
- `handleSQLQuery` (line ~911) is defined but no code path calls it. `renderSQLResults` and `renderSQLError` replaced it. Delete `handleSQLQuery`.

### 2.13 🗑 Duplicate function in `pkg/services/database_introspection.go`
- `GetSchemaForConnection` is byte-identical to `GetDatabaseSchema` (both delegate to `getMySQLSchema`/`getSQLiteSchema` on the same switch). Zero call sites. Delete `GetSchemaForConnection`.

### 2.14 🗑 Dead frontend query runner
- `SettingsView.svelte` imports `ExecuteQuery` and defines `queryConnectionId`, `queryText`, `queryResults`, `queryLoading`, `queryError`, `handleExecuteQuery`, `clearQueryResults`.
- Corresponding CSS classes exist (`.exploration-section`, `.query-form`, `.query-input`, `.query-results`, `.results-header`, `.results-table`, `.table-header`, `.table-row`, `.table-cell`) — but **no markup uses any of them**.
- Either wire the UI (§4.5) or delete both the state + the Go binding `App.ExecuteQuery` (`app.go:377–427`). Recommended: delete; the same functionality is available through the discussion view.

### 2.15 🗑 Dead frontend CSS
- `frontend/src/style.css` is not imported anywhere (`main.js` only imports `App.svelte`; `index.html` does not include a stylesheet). Delete `style.css`, `frontend/src/assets/fonts/`, `frontend/src/assets/images/logo-universal.png`, and the `vite-env.d.ts` file (project is JS-only, no TS).
- The `.schema-preview`, `.schema-columns`, `.schema-col`, `.sqlite-info` CSS blocks in `SettingsView.svelte` are also unused; remove them alongside the query-runner CSS.

### 2.16 🗑 Unused `App` type field
- `LLMProviderSetting` in `app.go` declares `Model,omitempty` and `BaseURL,omitempty` but the `TestLLMProviderConnection` handler and `UpdateLLMProvider` also take an `apiKey` — the returned struct does **not** contain the api_key, yet `SettingsView.svelte:startEditLLM` reads `provider.api_key`. That field will always be empty. Either remove the code path in `startEditLLM` or add `APIKey` (with a redacted placeholder) to `LLMProviderSetting`.

---

## 3. Simplifications & Refactors

### 3.1 ✂️ Remove permission plumbing from single-user app
Every "workspace" service currently calls:
```go
isMember, _ := IsWorkspaceMember(workspaceID, createdBy)
hasPermission, _ := CheckPermission(workspaceID, createdBy, "can_manage_db")
```
The Wails layer passes hard-coded `1, 1` (see `app.go:141, 262, 279, …`). After deleting `workspace_user.go` / `workspace_context.go` (§2.4), remove these prelude blocks from:
- `pkg/services/db_connection.go` (`CreateDBConnection`, `UpdateDBConnection`)
- `pkg/services/llm_provider.go` (`CreateLLMProvider`, `UpdateLLMProvider`, `DeleteLLMProvider`, `SetDefaultLLMProvider`)
- `pkg/services/conversation.go` (`CreateConversation`, etc.)

That alone removes hundreds of lines and eliminates the `workspace_id`/`created_by` columns' semantic weight. Consider fully dropping `workspace_id` from `db_connections`, `llm_providers`, `conversations`, `queries` — none of them serve any purpose in a single-user desktop app.

### 3.2 ✂️ Drop the "workspace" concept entirely
- `App.CreateConversation(1, 1, …)`, `ListConversations(1, 1)`, `ListLLMProvidersByWorkspace(1)`, `ListDBConnectionsByWorkspace(1)`, etc. all use the literal 1.
- Rewrite the bindings to take no workspace/user id, and remove the parameter from services.
- Front-end callers (`App.svelte:loadData`, `handleCreateDiscussion`, `handleUpdateConversationSettings`) can drop the leading `1, 1`.

### 3.3 ✂️ Simplify migrations
`pkg/models/database.go`:
- After §2.11, migration goes from ~14 tables to **5**: `llm_providers`, `db_connections`, `conversations`, `conversation_messages`, `queries` (and even `queries` is arguably redundant with `conversation_messages` — see §3.4).
- The trailing "ensure workspace_settings exists" block (lines ~305–315) duplicates a `CREATE TABLE IF NOT EXISTS` from earlier in the same function. Remove.
- `addColumnIfNotExists` matches on the substring `"duplicate column"`; the pure-Go SQLite driver returns messages like `duplicate column name: x`, but modernc.org/sqlite can also return `SQLITE_ERROR` codes without that exact string. Prefer `PRAGMA table_info(...)` and check for column presence explicitly — deterministic and driver-agnostic.

### 3.4 ✂️ Fold `queries` table into `conversation_messages`
`queries` is written to on every user message and updated 2–3 times per message
(pending → running → success/error). But no UI ever reads from it. All useful
data (SQL, error, latency) is already stored in `conversation_messages`
(`content`, `llm_content`, `metadata`). Delete the table and every
`CreateQuery`/`UpdateQueryStatus` call site — removes ~120 LoC and half a dozen
DB writes per turn.

### 3.5 ✂️ Consolidate LLM response parsing in `discussion_engine.go`
`ProcessUserMessage` currently contains **three near-identical blocks** that:
1. call `client.ChatCompletionWithPayload`,
2. persist a `[Round N — Full Payload]` conversation message,
3. call `extractJSONFromResponse`,
4. `json.Unmarshal` with fallback to `looksLikeSQL`/clarification.

Extract into one helper:
```go
func (e *engine) call(ctx context.Context, msgs []ChatMessage, label string) (LLMResponse, error)
```
This eliminates ~150 lines of copy-paste (in the exploration loop, the
"exploration-limit-reached" branch, and `executeFinalQueryWithRetry`).

### 3.6 ✂️ `extractJSONFromResponse` is overloaded and fragile
The current implementation tries to unescape via `strconv.Unquote(`"` + …)`,
which is a source of subtle bugs when the LLM emits genuinely-embedded
newlines or backslashes. Replace with:
1. Strip markdown fences (unchanged).
2. Trim `</think>` / "response" prefixes with a single regex.
3. Locate the first balanced `{...}` object with a small brace-counter.
4. Return that substring verbatim — `json.Unmarshal` handles escapes.

This drops ~40 lines and removes the "unescape then re-escape" trap.

### 3.7 ✂️ `sql_execution.go` duplication
- `applyDefaultLimit` walks the entire query character-by-character to find a
  closing paren for CTEs, then also duplicates logic for UNION/plain SELECT
  even though both fall through to the same `cleaned + " LIMIT n"` output.
  Collapse the last two branches.
- `strippedTrailingComments` has two `for` loops that each `break` on the first
  iteration — they're effectively `if`s. Simplify.
- `isRetryableError` has ~70 patterns with heavy duplication (`"truncated"`
  appears twice, `"invalid utf8"` and `"invalid utf8mb4"` both hit the same
  substring, several `column '.*' in ...` regex patterns are identical).
  Consolidate to ~15 patterns.
- `renderSQLResults` and `handleSQLQuery` (the dead one from §2.12) share most
  of their body with the message-composition code. After deleting
  `handleSQLQuery`, extract the shared "compose assistant message" block into
  a helper.

### 3.8 ✂️ HTML generation belongs in the frontend
`sql_execution.go` builds ~250 lines of inline-style HTML (`formatResultsHTML`,
`buildCollapsibleSQLBlockHTML`, `formatExplorationHTML`) that reference
frontend JS functions (`toggleRows`, `exportCSV`, `sortColumn`, `copySQL`,
`toggleAllExploration`) which **do not exist** in the Svelte app. So the
"Show more", "↓ CSV", column-sort, and "Collapse all" buttons are rendered but
non-functional.

Options (pick one):
- **(a) Recommended, simple):** return structured JSON (`AssistantResponse` as
  data, not HTML string) from the backend, and let `ConversationView.svelte`
  render it with real Svelte components (`<ResultsTable>`, `<SQLBlock>`,
  `<ExplorationDetails>`). Removes all the inline styles, XSS risk, and dead
  onclick attributes.
- **(b) Minimal):** keep HTML generation but strip out the non-functional
  buttons and their `data-*` attributes so users don't click dead controls.

### 3.9 ✂️ `looksLikeSQL` complexity
The `WITH` / `(` / `UNION` fallback logic can be replaced with a single case-insensitive regex `^\s*(SELECT|INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|DESCRIBE|SHOW|EXPLAIN|TRUNCATE|WITH|UNION|INTERSECT|EXCEPT|\()`. ~40 lines → 3.

### 3.10 ✂️ `llm_local.go` — pick one mode
485 lines cover two entirely separate transports: an HTTP client (mirrors
OpenAI's payload shape) and a `os/exec` GGUF/llama.cpp runner. The GGUF path
requires shelling out to a `llama` binary present on the host; this is fragile
on macOS/Windows/Linux desktop builds and isn't reachable from the UI (which
only asks for `base_url`). Delete the exec path; keep the HTTP client, which
is 99% redundant with `llm_openai.go` — most local endpoints (LM Studio,
llama-server, text-generation-webui, vllm) already expose an OpenAI-compatible
`/v1/chat/completions`. Consider deleting `llm_local.go` entirely and telling
users to use provider type `openai` with a custom `base_url`.

### 3.11 ✂️ Boilerplate `sql.Null*` → struct mapping
Every `GetXByID`/`ListXByWorkspace` in `db_connection.go`, `llm_provider.go`,
`conversation.go`, `query.go` does the same 20-line dance of
`sql.NullString` → `*string`. Extract a small helper or migrate to
`sqlx`/`sqlc`. Alternative: the project already imports GORM — if we're
keeping it in `go.mod`, use it here for consistency (and delete GORM if we
don't). Currently `gorm.io/gorm` is in `go.mod` but **not used at all** in the
non-dead code. Delete the dependency.

### 3.12 ✂️ Remove unused Go dependencies after cleanup
After the above deletions, prune from `go.mod`:
- `github.com/gin-gonic/gin`, `github.com/gin-contrib/sse`
- `github.com/go-playground/validator/v10`, `github.com/go-playground/*`
- `github.com/golang-jwt/jwt/v5`
- `github.com/joho/godotenv`
- `github.com/samber/lo` (only used in `pkg/services/workspace_user.go`)
- `gorm.io/gorm`
- `github.com/labstack/echo/v4` (never imported in-tree, only transitive)

Run `go mod tidy` afterwards. Expect the binary to shrink noticeably — the
current 26 MB executable is inflated by Gin + Echo + validator + jwt.

---

## 4. UI / UX Issues

### 4.1 🎨 Missing loading, empty and error states
- `App.svelte`'s "status" indicator in the sidebar (`Ready` / `Loading...` / `Loaded successfully` / `Error loading data`) is not a state end users care about — it's developer telemetry. Replace with a subtle toast on error and hide on success.
- The `.error-banner` at the top of the main area uses `color: #ffcdd2` (light pink) on a light-pink background → **the error text is unreadable**. Change to a solid `#b71c1c` on `#ffebee` with an icon.
- The "Loading schema..." indicator inside DB detail view has no spinner. Add one.

### 4.2 🎨 Contrast / theming inconsistencies
- `App.svelte` uses `--sidebar-bg: #f5f5f5` with heading `#4fc3f7` (light-blue on very light gray) — WCAG contrast ~1.7:1. Fails AA. Darken the header to `#0277bd` or `#01579b`.
- `SettingsView.svelte` mixes several palette generations: `.tab-btn` uses `#808080` text, `.form-group label` uses `#a0a0a0`, `.section-desc` uses `#808080`, `.btn-secondary` has `color: #a0a0a0` on `#e0e0e0` (near-invisible). Consolidate into a small set of CSS variables (`--text`, `--text-muted`, `--border`, `--surface`, `--surface-alt`, `--accent`, `--danger`) defined once in `App.svelte:global()`.
- `.btn-primary` in `SettingsView.svelte` uses `color: #000000` on `#4fc3f7`, whereas in `App.svelte` it's `color: #ffffff` on `#4fc3f7`. Pick one.
- `settings-tabs` has `border-bottom: 1px solid #1a1a1a` (near-black) on a white background — jarring hairline. Use `#e0e0e0`.

### 4.3 🎨 Gear popover UX
- The gear popover in `App.svelte` opens as a fixed centered dialog with no backdrop and no way to dismiss by clicking outside. Users get stuck.
- Add an overlay identical to `.modal-overlay` that closes the popover on outside click and on `Escape`.
- Also close it when the user actually changes the LLM/DB via the `<select>` (currently it stays open, requiring the ✕).
- The gear icon shows twice in the discussion list (one per row) *and* in the conversation header. When both are present, clicking either opens the same modal. Consider removing the per-row gear from the list — the settings are only relevant when a conversation is active. Keep the ✕ delete button, add archive/restore to the popover.

### 4.4 🎨 New Discussion modal
- The LLM Provider and DB Connection selects say "Default" for the null option, but there is no visible indication of which provider/db is the current default. Show it: `Default (My GPT-4)`.
- The Enter key submits from the title field but not from the selects. Consider a submit shortcut (`⌘/Ctrl-Enter`) that works anywhere in the modal.
- After creation, the user is dropped into the empty conversation with no cue about what to type. Add a placeholder message (e.g., "Ask a question about your data, or type `/help` for examples.").

### 4.5 🎨 SettingsView is unusually large and confusing
The Databases tab has **two full editors** for the same connection:
1. The `#showDBEditModal` modal (add/edit via list).
2. The `#showDBDetail` full-page editor with schema, business rules, exploration settings.

The list "Edit" button opens the detail view (2), but "+ Add Connection" opens the modal (1). This means creating a new connection uses a lightweight form, then to configure system prompt / business rules / exploration you have to save, click "Edit" again, and enter the second editor.

- **Recommendation:** delete the modal (§2.14-adjacent) and open every add/edit action in the detail view. Support "new (unsaved)" state with a `Create` button in place of `Save`.
- The exploration-config block appears in *both* the modal and the detail view with slightly different fields (`explorationAllowed` vs `dbDetailConfig.exploration_allowed`, `maxExplorationRounds` vs `dbDetailConfig.max_exploration_rounds`). Keep one source of truth (the detail view's `dbDetailConfig`).
- The detail view's `openDBDetail` calls `openDBDetail(updatedConn)` recursively after save (line ~325), which re-loads the form and can flicker; instead just update the reactive `selectedDBConnection` reference.
- Split `SettingsView.svelte` (1,824 lines) into three files: `SettingsLLM.svelte`, `SettingsDB.svelte`, `SettingsGeneral.svelte`, plus a shared `<Modal>` component (currently duplicated in `App.svelte` and `SettingsView.svelte`).

### 4.6 🎨 General Settings tab is a lie
`GetGeneralSettings` returns hard-coded defaults and `UpdateGeneralSettings` returns `nil` without persisting anything. The form pretends to save language, theme, default provider, app name. Either:
- Implement it (store in a small `app_settings` key/value table), or
- Remove the entire tab.

Given the About view already exists and the "app name" / "version" are hard-coded elsewhere, removing the tab is the honest option. Alternatively keep only the *usable* setting: default LLM provider (a dropdown that calls `SetDefaultLLMProvider`).

### 4.7 🎨 Tech Details toggle rendering
- In `ConversationView.svelte` the "Tech" toggle shows/hides `role !== 'user' && role !== 'assistant'` messages, but the payload sections (`showRequest`, `showResponse`, `showMessages`) are **shared state** across all exploration rounds — collapsing round 1's request also collapses round 2's. Make these per-message signals: use a `Map<messageID, {req, resp, msgs}>` or move the payload panel into its own component.
- The exploration icon (🔍) plus the huge JSON dump is intimidating and unstyled. Wrap the JSON in a copy-friendly `<pre>` with a max-height + fade-out; add a "Copy JSON" button (currently only the "Raw LLM Response" block has one, and it doesn't apply to the payload JSON).
- The `.exploration-summary` block at the bottom of the messages list rewrites the same info that's already visible above. Remove.

### 4.8 🎨 Message rendering
- User messages are rendered as plain text (`{message.content}`) — no line breaks preserved. Use `white-space: pre-wrap`.
- Assistant messages are `{@html message.content}` with no sanitization. The content comes from `AssistantResponse.ToHTML()` which does escape via `html.EscapeString`, but if we ever store LLM output verbatim (§3.8 refactor), this becomes an XSS vector. Add DOMPurify or (better) switch to structured rendering.
- Message timestamps use `toLocaleTimeString()` with no date — for messages older than a day, users can't tell when they were sent. Use "10:15 AM" for today, "Yesterday 10:15", "Nov 3 10:15" otherwise.
- No auto-scroll to bottom on new messages. Add `$effect` on `filteredMessages.length` that scrolls the `.messages-container` element.
- The processing indicator ("...") appears at the *end* but the container doesn't auto-scroll to reveal it. Same fix.

### 4.9 🎨 Sidebar footer
- The status text truncates ("Loaded successfully") without ellipsis on narrow window widths and wraps awkwardly. Add `white-space: nowrap; overflow: hidden; text-overflow: ellipsis;`.
- Consider replacing it with a "New Discussion" button — that's what users actually want at the bottom of a sidebar.

### 4.10 🎨 Discussion list interactions
- Clicking anywhere on the row opens the conversation, but the gear button and delete button are children of the same row and rely on `e.stopPropagation()`. Space/keyboard nav doesn't work — the outer element is a `<button class="conversation-item">` but the row contains nested `<button>`s (invalid HTML: buttons cannot contain buttons). Restructure so only the title area is clickable, and actions live in a right-aligned toolbar.
- No visual state for hovering the delete button — just a color change. Add a subtle background transition and `title="Delete"`.
- No confirmation when archiving (which is destructive from the user's POV — the conversation disappears from the list without an obvious way to find archived items again). Add an "Archived" filter above the list.

### 4.11 🎨 Missing UI for archived discussions
`ArchiveConversation` moves conversations to `status = 'archived'`, but `ListConversationsByUser` filters `status != 'deleted'` (see `pkg/services/conversation.go`). If the filter includes archived, they show up mixed in. If it excludes them, restore is unreachable except by re-opening a discussion that's already open. Add a "Show archived" toggle in the discussions view.

### 4.12 🎨 Modal focus/traps
None of the modals (`New Discussion`, `Delete Confirmation`, `Edit LLM`, `Edit DB`, `Gear Popover`) trap focus, restore focus on close, or listen for `Escape`. Add a small `<Modal>` component that:
- Traps Tab within itself
- Closes on `Escape`
- Restores focus to the trigger button on close
- Has `role="dialog"` + `aria-modal="true"` + `aria-labelledby`

### 4.13 🎨 Icon consistency
The app mixes emoji icons (💬 ⚙️ ℹ️ 🔍 📤 📥 💬 ✕ ×) and inline SVG icons (send button, tech toggle chevrons). Pick one. Recommended: swap emoji for `lucide-svelte` (or hand-picked SVGs) — emoji rendering varies wildly across OSes and looks unprofessional at small sizes.

### 4.14 🎨 SSL Mode dropdown values are wrong
`SettingsView.svelte` offers `"false" | "true" | "preferred"` but `buildMySQLDSN` only recognises `"required" | "verify-ca" | "verify-full"`. So none of the UI values actually configure TLS. Align both sides on: `disable | require | verify-ca | verify-full` and document.

### 4.15 🎨 Password field UX
- The DB connection password shows `placeholder="Leave blank to keep current"` in the modal but not in the detail view — users editing from detail have no clue.
- On the LLM edit modal, the api_key field uses the *current* raw key as placeholder text if any was ever populated? No — because `LLMProviderSetting` doesn't include the key. So the user always sees the same generic "sk-..." placeholder and can't tell if the key was already saved.
- Add a "🔑 Key saved" indicator when the backend has a non-empty key; require the user to click "Change key" to edit.

### 4.16 🎨 Number inputs are unbounded
`Max Exploration Rounds`, `Default Limit`, etc. use `<input type="number">` with no `min`, `max`, or `step`. A user can enter `-5` or `999999`. Add sensible bounds and clamp server-side.

### 4.17 🎨 Safety-mode hint duplication
The mile-long safety-mode explanation appears twice (in the modal *and* in the detail view). Extract to a `<SafetyModeHelp mode={value}/>` component; keep the source of truth in one place.

---

## 5. Correctness / Bugs

### 5.1 🐞 `formatSQLResultsForLLM` writes SQL where columns should go
`pkg/services/discussion_engine.go:1104`:
```go
sb.WriteString(fmt.Sprintf("[PREVIOUS QUERY RESULTS]\nSQL: %s\n\n", qr.Columns))
```
`qr.Columns` is `[]string`, but the format string is `%s` (single string). Go prints it as `[col1 col2 col3]`. The label reads "SQL:" — clearly the intent was to include the SQL query, but the value passed is the columns slice. Fix: either drop the "SQL:" line (results don't carry the SQL) or thread the SQL through.

### 5.2 🐞 `LLMResponse` fallback loses the parse error context
Multiple sites do:
```go
finalResp = LLMResponse{
  Action: "sql_query",
  SQLQuery: fmt.Sprintf("-- Exploration limit reached. …"),
}
```
This literally sends a comment-only SQL statement to the DB, which will fail with "no statement executed" and then trigger the retry loop. Instead: convert to `action: "clarification"` and surface the underlying reason to the user.

### 5.3 🐞 Consecutive assistant errors
The deferred `if err != nil { CreateConversationMessage(…, "assistant", errorMsg, …) }` at the top of `ProcessUserMessage` runs *even when* `handleClarification` or `renderSQLError` already inserted an assistant message. The user sees the error twice. Guard the defer with a bool `assistantMessageSaved`.

### 5.4 🐞 `formatResults` uses `strings.Title`, deprecated in Go 1.18+
`humanizeColumnName` calls `strings.Title(col)`. That function is deprecated and unicode-broken. Use `golang.org/x/text/cases` or a manual `unicode.ToTitle` on the first rune.

### 5.5 🐞 `applyDefaultLimit` UNION/CTE logic
The UNION branch's loop searching for `lastSelect` never actually uses `lastSelect` other than the guard — the same trailing `LIMIT` is appended regardless. In MySQL, `SELECT … UNION SELECT … LIMIT 100` only limits the *last* query (unless wrapped in parens). This is a subtle correctness bug: users asking for "the last 100" via a UNION will silently get less than expected.

Fix: wrap the entire query in `SELECT * FROM ( … ) LIMIT n` when a `UNION` is detected — or better, don't apply a default limit to queries whose top-level starts with a `(` or contains `UNION`; leave those to the LLM.

### 5.6 🐞 `validateExplorationQuery` blocks all comments
`dangerousPatterns` contains `"--"`, `"/*"`, `"*/"`. Any SQL containing e.g. `WHERE x > 1` — wait, that's not a comment. But a legitimate query like `WHERE last_updated_at > '2020-01-01' -- last decade` will be blocked. In practice this is fine because the LLM doesn't emit comments; still, the check is a blunt instrument. Consider stripping comments before validation instead of rejecting.

### 5.7 🐞 SQLite migration column-detection race
`addColumnIfNotExists` catches the error string and returns nil. But some `ALTER TABLE ... ADD COLUMN` failures return `SQLITE_ERROR: duplicate column name: X` while others (e.g., generated columns) return different codes. Use `PRAGMA table_info` first — it's a single query and 100% reliable.

### 5.8 🐞 `TestDBConnection` doesn't respect the port
Actually — it does, but `SettingsView.svelte`'s form ports default to `0` when a SQLite connection is edited (since SQLite doesn't have a port). Then when the user switches from SQLite to MySQL, `port` stays `0` and MySQL DSN gets `localhost:0` and fails cryptically. On type change, reset port to the default (`3306` for MySQL, `0`/hidden for SQLite).

### 5.9 🔒 Password stored in plaintext
`db_connections.password` is stored as-is in SQLite. The `WORKSPACE_ENCRYPTION_KEY` env var was clearly intended to encrypt it (see `env.go`) but never wired up. Either:
- Encrypt with a key derived from the OS keyring (`zalando/go-keyring`), or
- Encrypt with a machine-bound key stored in `~/.yourql/.key` (chmod 0600), or
- Document it explicitly ("secrets are stored in cleartext in `~/.yourql/yourql.db`; secure the file yourself").

At minimum, prevent the raw password from ever being sent back to the frontend via `LLMProviderSetting`/`DBConnectionSetting` (currently `DBConnectionSetting` omits it, but `startEditDB` reads `connection.password` from the list response — actually, `ListDBConnections` doesn't include it since `Password *string 'json:"-"'`. Confirm and add tests.)

### 5.10 🔒 SQL injection via `ExecuteQuery`
`app.go:ExecuteQuery` runs whatever string the frontend sends against the target DB. If §2.14 keeps the query runner, the workflow is fine (user is deliberately typing SQL). If not, delete `ExecuteQuery` from `app.go` — it's dead surface area (§2.14 established the UI is not wired). The Wails binding still exposes it, so a malicious page loaded into the webview could invoke it.

### 5.11 🐞 Nil-slice JSON serialization
`ListLLMProviders`/`ListDBConnections` in `app.go` build `[]LLMProviderSetting` / `[]DBConnectionSetting` via `append` on a `nil` slice. When there are 0 providers, the returned JSON is `null`, not `[]`. The Svelte frontend does `llmProviders = llmRes || []` which papers over this. Fix by initialising `settings := make([]LLMProviderSetting, 0)`. (Same applies to `ListConversations`, `ListDiscussions`.)

### 5.12 🐞 `App.startup` doesn't propagate migration errors
`models.ConnectDatabase()` calls `log.Fatalf` on migration failure. In a Wails app, that terminates the process silently for the end user — no UI feedback. Wrap with a recover / return the error and render a user-facing error screen.

### 5.13 🐞 Race between `updateConversationSettings` and `openConversation`
`App.svelte:handleUpdateConversationSettings` mutates `activeConversation.llm_provider_id` in-place, then re-fetches `ListConversations`, then re-assigns `conversations`. But the `activeConversation` reference isn't updated from the new list, so if the returned data differs (e.g., `updated_at`), the header shows stale data. Assign `activeConversation = conversations.find(c => c.id === activeConversation.id) ?? activeConversation` after the reload.

### 5.14 🐞 Deleting a conversation from the list while it's open
The delete flow removes the conv from `conversations` but only navigates back if `activeConversation.id === deleteTargetId`. That check works, but the delete button (per row) is disabled only visually — actually, it isn't disabled at all during `deleting`. Add `disabled={deleting}` on the row's ✕.

---

## 6. Recommended Order of Work

1. **Delete dead code (Section 2)** — safe, mechanical, unlocks all other refactors. Run `go build ./...` after each package deletion.
2. **Remove workspace/permission plumbing (§3.1, §3.2)** — one PR touching every remaining service.
3. **Fold `queries` table into `conversation_messages` (§3.4)** — schema cleanup.
4. **Extract LLM call helper (§3.5, §3.6)** — highest leverage for future maintenance.
5. **Structured assistant responses (§3.8)** — enables real interactive tables and fixes the dead onclick buttons.
6. **Frontend restructure (§4.5, §4.7, §4.12)** — split `SettingsView`, extract `<Modal>`, per-message payload state.
7. **Design-token pass (§4.2)** — one commit to introduce CSS variables and audit contrast.
8. **Correctness fixes (Section 5)** — can be interleaved with the above.
9. **Security hardening (§5.9, §5.10)** — after the surface is smaller.

At the end of this pass, expected shape:

```
YourQL/
├── app.go                        # ~250 lines (down from 467)
├── main.go
├── pkg/
│   ├── models/
│   │   └── database.go           # 5 tables, ~120 lines
│   └── services/
│       ├── discussion_engine.go  # ~700 lines (down from 1174)
│       ├── llm/                  # split by provider
│       │   ├── client.go
│       │   ├── openai.go
│       │   ├── anthropic.go
│       │   └── ollama.go
│       ├── db/
│       │   ├── connection.go
│       │   ├── introspection.go
│       │   └── execution.go
│       └── conversation.go
└── frontend/src/
    ├── App.svelte
    ├── views/
    │   ├── DiscussionsList.svelte
    │   ├── ConversationView.svelte
    │   └── AboutView.svelte
    ├── settings/
    │   ├── SettingsView.svelte
    │   ├── SettingsLLM.svelte
    │   ├── SettingsDB.svelte
    │   └── SafetyModeHelp.svelte
    └── lib/
        ├── Modal.svelte
        ├── Button.svelte
        └── tokens.css
```

Total expected reduction: **~10,000 → ~4,500 lines** across Go + Svelte, with
no loss of user-facing functionality (except the never-wired query runner and
the fake general-settings tab).

---

## 7. Nice-to-haves (post-cleanup)

- **Streaming LLM responses**: today `ChatCompletion` blocks until the entire response is buffered. For long queries the user sees "Processing..." for 20+ seconds. Enable `stream: true` in the OpenAI/Anthropic clients and emit events via Wails runtime `EventsEmit` to progressively render.
- **Postgres support**: schema introspection already has a `postgresql` type hint in `buildSystemPrompt`, but there's no `pgx` driver, no `getPostgresSchema`, and the DB-type dropdown offers only MySQL/SQLite. Ship Postgres to hit the largest user base.
- **Query history / saved queries**: the `saved_queries` table exists in the migration but has no UI. Add it if useful; otherwise remove it (see §2.11).
- **Keyboard shortcuts**: `⌘K` to focus the message input, `⌘N` for new discussion, `⌘,` for settings, `↑` in an empty input to edit the previous user message.
- **Dark mode**: the code contains dark-mode remnants (`background-color: rgba(27, 38, 54, 1)` in `main.go`, `#a0a0a0` labels in Settings, deleted `style.css` dark bg). Finish or forbid; don't leave in a half state.
- **First-run onboarding**: on empty DB, guide the user through creating their first LLM provider and DB connection before the discussions view appears. Currently a first-time user sees an empty discussions list and no hint that they need to configure Settings first.
- **Cross-platform testing**: no CI, no tests. Add at minimum a `go test ./pkg/services/...` job covering `applyDefaultLimit`, `validateExplorationQuery`, `extractJSONFromResponse`, and `isRetryableError` — all pure functions with tricky logic.

---

_End of analysis._
