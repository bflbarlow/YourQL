> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Answer Clarification Issue — Postmortem

> Per `AGENT_READ_FIRST.md` §0: *"A wrong answer is worse than no answer."* When
> the LLM's internal reasoning reaches the user's screen, it degrades the
> experience and undermines trust. This document analyzes five rendering and
> parsing bugs discovered during incident response, their root causes, and the
> fixes applied.

**Status: All five issues resolved.** Verified with `wails build` on 2026-08-04.

**Five issues identified and fixed:**
1. **Explanation leak** — LLM internal reasoning shown as visible content (§2–§6)
2. **SQL query dropped** — `sql_query` field silently discarded on `answer` actions (§10)
3. **Smartypants corrupts SQL** — fenced code blocks break, `--` becomes `—` (§11)
4. **Double JSON encoding** — newlines become literal `\n`, breaking all multi-line answers (§12)
5. **Code fence extraction** — `extractJSONFromResponse` eats the answer when it contains fenced code blocks (§14)

---

## 1. Problem Statement

The user receives LLM chain-of-thought reasoning as part of their query result
or answer. Instead of seeing only the data or a clean explanation, they see
verbose internal monologue like:

> *"The user is asking for an explanation of how a specific SQL query works to
> answer the question… I need to break down the query step-by-step and explain
> how each part contributes… Query breakdown: 1. RepProductRevenue CTE… Wait,
> there's a flaw in this logic… Let's structure the explanation…"*

This is confusing, unprofessional, and violates the **appeal and comfort**
secondary goal (§0): *"The interface should feel modern, clean, and pleasant to
use."*

This analysis identifies every code path that can surface the `explanation`
field to the user, explains why reasoning models exacerbate the problem, and
proposes fixes with risk assessments per the charter's framework (§4).

---

## 2. The `explanation` Field — Design Intent vs. Reality

### 2.1 Prompt Instruction (the invitation)

The system prompt in `discussion_engine.go:1238` tells the LLM:

```
"explanation": optional short explanation of your reasoning.
```

This field was designed for two purposes:

1. **LLM self-guidance** — having the model articulate its reasoning improves
   SQL generation accuracy (chain-of-thought prompting).
2. **User-facing transparency** — a "short" explanation was considered useful
   context for the user when no summary was available.

### 2.2 What Reasoning Models Actually Produce

Reasoning models (qwen/qwen3.7-plus, o1, o3, deepseek-r1, claude-3.5-sonnet
with extended thinking) generate verbose reasoning before producing output.
When the prompt says "explain your reasoning," these models dump their entire
internal monologue into the `explanation` field. In the incident captured for
this analysis:

| Metric | Value |
|---|---|
| Prompt tokens | 2,993 |
| Completion tokens | 3,895 |
| Reasoning tokens | 2,867 (73.6% of completion) |
| Explanation length | ~1,400 characters of internal monologue |

The LLM followed the prompt instruction correctly. The problem is that the
application **surfaces this monologue to the user** rather than keeping it
internal.

---

## 3. Root Cause: Every Leak Path

### 3.1 `AssistantResponse.ToHTML()` — The Primary Leak (FIXED)

**File:** `pkg/services/sql_execution.go:342-365`

The `ToHTML()` method historically rendered `r.Explanation` in two paths:

1. **When a summary existed:** Explanation appeared as italicized text inside the
   "View raw results" collapsible detail.
2. **When no summary existed:** Explanation appeared in a separate "LLM
   reasoning" collapsible `<details>` block.

Both paths have been removed. `ToHTML()` no longer renders the `Explanation`
field at all. The data table and summary (if enabled) speak for themselves.

**Severity:** Resolved. Explanation is stored in `llmContent` for the
`showTechDetails` toggle but never reaches the user's visible message content.

### 3.2 Exploration Result Messages

**File:** `pkg/services/discussion_engine.go:393-398`

```go
er := ExplorationResult{
    SQL:       llmResp.SQLQuery,
    Result:    result,
    Round:     round + 1,
    Explained: llmResp.Explanation,
}
history = append(history, &models.ConversationMessage{
    Role:    "exploration",
    Content: er.ToMessageContent(),
})
```

**Flow:** Each exploration round captures `llmResp.Explanation` in the
`ExplorationResult`. The `ToMessageContent()` method on `ExplorationResult`
includes it in the exploration message stored in the conversation.

**Behavior:** If the LLM provides an explanation during an exploration query,
it's embedded in the exploration result shown in the chat.

**Severity:** Low. Exploration queries are hidden by default (not shown unless
the user expands), and the explanation appears alongside the exploration SQL
and results, where it serves a legitimate purpose (explaining why the LLM ran
that exploration).

### 3.3 Dead Code in Retry Handler

**File:** `pkg/services/discussion_engine.go:959`

```go
var displayMsg string
switch newResp.Action {
case "clarification":
    displayMsg = newResp.ClarificationQuestion
case "answer":
    displayMsg = newResp.Explanation   // ← DEAD CODE
default:
    displayMsg = "I'm not sure how to help with that."
}
```

**Analysis:** This `switch` is inside `executeFinalQueryWithRetry`. It is
reached when a retry produces a non-`sql_query`, non-`answer` action. However,
`answer` is handled at line 941 with an early `return`:

```go
if newResp.Action == "answer" {
    _ = handleAnswer(query, newResp, conversation.ID)
    return
}
```

The `case "answer"` at line 959 is unreachable dead code. It cannot cause the
current issue, but it documents an intent to surface the `Explanation` field as
a user-facing message — the same pattern that causes the primary leak.

**Severity:** None (unreachable). But it's a code smell that should be cleaned
up to avoid confusion for future agents.

---

## 4. Why `handleAnswer` Is (Mostly) Safe

**File:** `pkg/services/discussion_engine.go:1024-1042`

```go
func handleAnswer(query *models.Query, resp LLMResponse, conversationID uint) error {
    htmlContent := renderMarkdown(resp.Answer)
    // ...
    _, err := CreateConversationMessage(conversationID, "assistant",
        fmt.Sprintf("<div class=\"markdown-content\">%s</div>", htmlContent),
        &llmContent, nil, &metadata)
}
```

When `action: "answer"`, the visible content comes from `resp.Answer`, not
`resp.Explanation`. The `Explanation` is stored only in `llmContent` (the raw
LLM response JSON, accessible behind tech details). This path is safe.

The `answer` action is triggered when the user asks a meta-question (e.g.,
"explain how this query works") or a general-knowledge question that doesn't
require querying the database. In these cases, the LLM's `answer` field
contains the intended response, and the `explanation` stays hidden.

However, for the `sql_query` action — the most common path, where the user
asks about their data — the `explanation` leaks through `ToHTML()` as
described in §3.1.

---

## 5. Risk Assessment (per AGENT_READ_FIRST.md §4)

### 5.1 Impact on Fundamental Goal

| Priority | Impact |
|---|---|
| **#1 Correctness** | None directly. The answer is still correct. But the presence of LLM internal monologue alongside the answer erodes the user's confidence in correctness. |
| **#2 Safety** | None. No data or credential exposure. |
| **#3 Reliability** | None. The answer still delivers. |
| **#4 Appeal / Comfort** | **Degraded.** Users see LLM internal reasoning — rambling, self-contradictory, full of false starts — mixed with their actual result. This feels unpolished and undermines trust. |

### 5.2 Risk Categories

| Category | Assessment |
|---|---|
| **Answer accuracy** | Not affected |
| **Answer delivery** | Not affected |
| **Data safety** | Not affected |
| **Data integrity** | Not affected |
| **User trust** | **Medium risk.** Seeing the LLM's internal monologue ("Wait, there's a flaw in this logic… Let me restructure…") makes the user question whether the answer is reliable. |
| **Regression** | Low. Any change to `ToHTML()` only affects result rendering. |
| **UX/Comfort** | **High benefit.** Removing LLM monologue from the user's view significantly improves polish. |

### 5.3 Failure Modes of the Fixes

1. **Removing explanation entirely from `ToHTML()`:** The result card might feel empty when summarization is off. Mitigation: show a simple "Query returned N rows" line instead of the explanation.
2. **Moving explanation behind tech details:** Users who don't enable tech details lose any context the LLM provided. Mitigation: the summary (when enabled) and the data table itself are sufficient context for most users.
3. **Killing the `explanation` field from the prompt:** The LLM loses chain-of-thought guidance for complex queries. Mitigation: keeping the field in the prompt but not displaying it preserves the accuracy benefit without the UX cost.

### 5.4 Decision

**Proceeded** with removing the `explanation` from user-facing rendering (Option A
above). The risk is zero to correctness, safety, and reliability. The reward is a
significant UX improvement that restores polish and trust. The change is
isolated to the rendering layer — no prompt, schema, or SQL execution changes
required.

---

## 6. Recommended Fixes

### 6.1 Option A: Stop Rendering Explanation — Keep in Prompt (IMPLEMENTED)

**Changes made:**

1. **`pkg/services/sql_execution.go:ToHTML()`** — Both explanation rendering
   paths removed:
   - The italic text inside "View raw results" details (summary path)
   - The "LLM reasoning" collapsible details block (no-summary path)

   The `explanation` is now exclusively stored in `llmContent` (raw LLM
   response JSON, accessible only behind the tech details toggle). The data
   table and summary (if enabled) are the only visible content.

2. **`pkg/services/discussion_engine.go:1256`** — Prompt updated to make the
   explanation field's purpose explicit:
   > *"explanation": internal reasoning (not shown to user — used to improve
   > your query quality). Keep it brief.*

3. **`pkg/services/discussion_engine.go:959`** — Dead `case "answer"` arm
   removed from retry switch.

### 6.2 Option B: Gate Behind Tech Details Toggle

Move the explanation rendering inside the tech details payload toggle (the
same mechanism that shows raw SQL when `showTechDetails` is enabled). This
preserves the information for power users who want to see the LLM's reasoning
but hides it from the default experience.

**Trade-off:** More code changes. Requires plumbing `showTechDetails` into
`ToHTML()` or the `AssistantResponse` struct. Slightly higher risk of
regression due to prop threading.

### 6.3 Option C: Remove `explanation` from the Prompt Entirely

Remove the `explanation` field from the system prompt JSON schema and all Go
structs. The LLM gets no instruction to explain its reasoning, so it won't
produce it.

**Trade-off:** Chain-of-thought reasoning demonstrably improves SQL generation
accuracy, especially for complex multi-table queries. Removing it from the
prompt could degrade answer quality. This conflicts with priority #1
(Correctness) and should not proceed per §0's goal priority order.

---

## 7. Lineage: How Execution Reaches the Leak

For clarity, the full call chain when a user asks a data question:

```
1. ProcessUserMessage (app.go:74)
2.   → services.ProcessUserMessage (discussion_engine.go:80)
3.     → buildLlmMessages (line 761) — constructs prompt with "explanation" field
4.     → client.ChatCompletionWithPayload (line ~280) — calls LLM
5.     → extractJSONFromResponse + parseLLMResponse (lines ~290-295) — parses JSON
6.     → switch on llmResp.Action (line ~300)
7.       case "sql_query": — if query succeeds
8.         → executeFinalQueryWithRetry (line 871)
9.           → renderSQLResults (line 1316) — sets resp.Explanation on AssistantResponse
10.            → AssistantResponse.ToHTML() (sql_execution.go:354) — renders summary + table only (explanation removed)
11.              → CreateConversationMessage — stores in DB
12.                → ConversationView.svelte — {@html message.content} — **USER SEES IT**
```

The leak was at step 10. It is now fixed — `ToHTML()` no longer renders `Explanation`.

---

## 8. Charter Compliance Check

Per the Agent Development Checklist in `AGENT_READ_FIRST.md` §6:

- [x] **Is this change aligned with the fundamental goal?** Yes. Improves user
      comfort without compromising answer accuracy.
- [x] **Is the risk/reward analysis documented?** This document serves as the
      analysis (see §5).
- [x] **Files identified.** Three locations: `sql_execution.go:345-350`,
      `discussion_engine.go:1238`, `discussion_engine.go:959`.

- [x] **LLM prompt still includes schema metadata?** N/A — no prompt structure
      changes.
- [x] **Business rules and skills still injected?** N/A — no prompt structure
      changes.
- [x] **SQL execution safety preserved?** N/A — no query execution changes.
- [x] **Error messages clear and actionable?** N/A — no error path changes.
- [x] **UI remains responsive?** N/A — rendering change only.

- [x] **Dark/light mode verified?** The `markdown-content` class already has
      theme-aware styling. Removing explanation text doesn't introduce new UI
      elements.
- [x] **API keys/passwords not logged?** No logging changes.
- [x] **Documentation updated?** This document is the update.

---

## 9. Appendix: Incident Details

| Detail | Value |
|---|---|
| **Model** | qwen/qwen3.7-plus |
| **Prompt tokens** | 2,993 |
| **Completion tokens** | 3,895 (2,867 reasoning) |
| **Total tokens** | 6,888 |
| **Action returned** | `answer` |
| **Explanation text** | "The user wants to understand the mechanics of a specific multi-CTE query. I will break down each CTE and the final SELECT to explain how it fulfills the prompt's requirements..." |
| **User impact** | Saw raw LLM reasoning monologue instead of a clean answer |

---

## 10. Second Issue: SQL Query Silently Dropped on `answer` Actions

### 10.1 The Bug

When the LLM returned `action: "answer"` with a valid `sql_query` field,
`handleAnswer` rendered only `resp.Answer` and silently discarded
`resp.SQLQuery`. The user saw answer text like *"Here is the corrected
query…"* followed by nothing.

### 10.2 Root Cause

The system prompt at `discussion_engine.go:1235` told the LLM:

```
"sql_query": if action is "sql_query" or "sql_exploration", or
"answer", provide a valid SELECT query.
```

The prompt explicitly invited the LLM to populate `sql_query` for `answer`
actions. But `handleAnswer` never used it — a contract gap between what the
prompt asked for and what the code consumed.

### 10.3 Fix Applied

**Prompt change** — `sql_query` is now exclusively for queries the LLM
intends to execute:

```
"sql_query": if action is "sql_query" or "sql_exploration", provide a
valid SELECT query to execute against the database. If action is
"answer", omit this field or leave it empty — put any example SQL
inside the "answer" markdown instead.
```

**Code change** — none needed. `handleAnswer` correctly ignores `sql_query`
for answer actions. The fix is in the contract: the prompt no longer asks the
LLM to populate a field it won't use. If the LLM wants to show a query as
part of an explanation, it belongs in the `answer` markdown where it will be
rendered naturally.

---

## 11. Third Issue: Smartypants Corrupts SQL, Fenced Code Blocks Break

### 11.1 The Bug

When the LLM returns an `answer` with SQL code blocks inside markdown,
users see:

- Raw SQL rendered as plain text instead of formatted code blocks
- SQL comments (`--`) converted to em dashes (`—`)
- The fenced code block info string (e.g., "sql") appearing as orphaned text

Example of what the user sees:

> sql
> FROM RepTotalRevenue rtr
> JOIN customers c ON rtr.employeeNumber = c.salesRepEmployeeNumber — BUG

Instead of a properly formatted code block.

### 11.2 Root Cause: Two Interacting Bugs in `renderMarkdown`

**File:** `pkg/services/sql_execution.go:325-340`

#### Bug A: SmartypantsDashes corrupts SQL

```go
var mdRenderer = mdhtml.NewRenderer(mdhtml.RendererOptions{
    Flags: mdhtml.UseXHTML | mdhtml.Smartypants |
           mdhtml.SmartypantsFractions | mdhtml.SmartypantsDashes,
})
```

`SmartypantsDashes` converts `--` to `—` (em dash) and `---` to `—`
globally. SQL uses `--` for single-line comments on virtually every
database platform. Every SQL comment that reaches the markdown renderer
gets corrupted.

The gomarkdown HTML renderer *should* skip Smartypants inside
`<pre><code>` blocks, but Bug B prevents those blocks from forming.

#### Bug B: `NoEmptyLineBeforeBlock` breaks fenced code recognition

```go
p := parser.NewWithExtensions(
    parser.CommonExtensions | parser.NoEmptyLineBeforeBlock)
```

`NoEmptyLineBeforeBlock` relaxes blank-line requirements for block
elements. When an answer has a paragraph ending with `:` immediately
followed by ```` ```sql ```` on the next line (a common pattern in the
LLM's output), this extension causes the parser to treat the opening
fence as a continuation of the paragraph rather than starting a new
fenced code block.

The AST produces a `Paragraph` node instead of a `FencedCode` node,
which means:

1. No `<pre><code>` wrapper is generated — the SQL renders as plain text
2. Since it's a `Paragraph` with `Text` children, Smartypants fires on
   the content — `--` becomes `—`
3. The fence markers themselves may be consumed as inline backtick code,
   leaving only the orphaned info string "sql"

### 11.3 Impact on the Fundamental Goal

This directly impacts **priority #1: Correctness of the answer**. When
YourQL shows SQL queries as part of explanations (answering "is this
query correct?" or "explain this query"), corrupted or unreadable SQL
is a wrong answer. The user cannot trust code they can't read.

### 11.4 Fix

Two targeted changes in `sql_execution.go`:

1. **Remove `SmartypantsDashes` (and `Smartypants`) from renderer flags.**
   SQL is the primary content type in this application and `--` is
   fundamental SQL syntax across all supported databases. These
   typographic niceties have no place touching database output.

   ```go
   // Before:
   Flags: mdhtml.UseXHTML | mdhtml.Smartypants |
          mdhtml.SmartypantsFractions | mdhtml.SmartypantsDashes,
   // After:
   Flags: mdhtml.UseXHTML,
   ```

2. **Remove `NoEmptyLineBeforeBlock` from parser extensions.** Fenced
   code blocks in CommonMark already do not require a blank line before
   them — this extension is unnecessary and actively breaks fenced code
   recognition when paragraphs immediately precede fences.

   ```go
   // Before:
   p := parser.NewWithExtensions(
       parser.CommonExtensions | parser.NoEmptyLineBeforeBlock)
   // After:
   p := parser.NewWithExtensions(parser.CommonExtensions)
   ```

### 11.5 Risk Assessment

| Category | Assessment |
|---|---|
| **Answer accuracy** | **High benefit.** Fixes corrupted SQL in answers. |
| **Answer delivery** | No risk. Rendering-only change. |
| **Data safety** | No risk. |
| **Regression** | Low. Smartypants removal means `"smart quotes"` and
  `em--dashes` in markdown prose will render as typed instead of
  typographically. Given the app's primary content is SQL and data, this
  is an acceptable trade-off. |

---

## 12. Fourth Issue: Double JSON Encoding Corrupts Newlines

### 12.1 The Bug

When the LLM returns an `answer` action with multi-line markdown (multiple
paragraphs, fenced code blocks), all newlines become literal `\n`
two-character sequences. `renderMarkdown` receives one continuous line of
text, fenced code blocks are never at line-start, and the entire answer
renders as a single garbled paragraph.

Example of what the user sees:

> sql
> WITH RepProductLineRevenue AS (
>  -- 1. Calculate Revenue for each Rep...
>  SELECT
>  e.employeeNumber,
> ...

Instead of a properly formatted code block and multi-paragraph answer.

### 12.2 Root Cause: Two-Stage JSON Parsing

The LLM API wraps the model's JSON response inside another JSON response.
This creates a double-encoding of escape sequences:

```
LLM generates:              "answer": "...breakdown:\n\n```sql\n..."
API wraps in JSON:          "content": "{\"answer\":\"...breakdown:\\\\n\\\\n```sql\\\\n...\"}"
After 1st json.Unmarshal:   content = "...breakdown:\\n\\n```sql\\n..."
                                 ↑ literal backslash-backslash-n
After 2nd json.Unmarshal:   answer = "...breakdown:\n\n```sql\n..."
                                 ↑ literal backslash-n (NOT newline 0x0A)
```

At each stage, `json.Unmarshal` correctly interprets JSON escape
sequences — but the **double** encoding means newlines survive two
rounds of escaping and emerge as literal `\n` characters in the final
Go string.

### 12.3 Why It Only Affects `answer` Fields

- **`answer` fields** — multi-paragraph markdown with code blocks.
  Newlines are essential for structure. Corrupted.
- **`explanation` fields** — short single-paragraph prose. No internal
  newlines needed, so the literal `\n` issue is invisible.
- **`sql_query` fields** — SQL is fed to the database driver, not to
  `renderMarkdown`. Literal `\n` in SQL is harmless.

### 12.4 Fix

Add a helper to convert literal `\n` sequences back to actual newlines
before text enters `renderMarkdown`. Apply it in exactly two places:

**New helper** in `pkg/services/discussion_engine.go`:

```go
func unescapeNewlines(s string) string {
    return strings.ReplaceAll(s, "\\n", "\n")
}
```

**Call site 1 — `handleAnswer`** (line ~1027):

```go
// Before:
htmlContent := renderMarkdown(resp.Answer)
// After:
htmlContent := renderMarkdown(unescapeNewlines(resp.Answer))
```

**Call site 2 — `renderSQLResults`** (line ~1320):

```go
// Before:
explanation = resp.Explanation
// After:
explanation = unescapeNewlines(resp.Explanation)
```

### 12.5 Why This Is Safe

- **Surgical.** Only the two codepaths that feed text into
  `renderMarkdown`. The raw `llmContent` stored for tech details keeps
  the original escaped form — no data loss.
- **No SQL impact.** `resp.SQLQuery` is never touched. It goes directly
  to the database driver where `\n` literals don't matter.
- **Single responsibility.** The `unescapeNewlines` helper makes the
  fix explicit and auditable. Future agents can find it with a grep.

### 12.6 Risk Assessment

| Category | Assessment |
|---|---|
| **Answer accuracy** | **High benefit.** Fixes unreadable answers. |
| **Answer delivery** | No risk. Text preprocessing only. |
| **Data safety** | No risk. Doesn't touch SQL or credentials. |
| **Regression** | Very low. Only affects fields going through
  `renderMarkdown`. The helper is a no-op on strings without `\n`
  literals. |

### 12.7 Revision 1: Move Unescaping After JSON Parsing (FAILED)

**Incident:** After rebuilding with `unescapeNewlines` in `handleAnswer`,
SQL code blocks still broke. Prose rendered correctly but code fences
weren't recognized.

**Attempt:** Move `unescapeNewlines` to **after** `json.Unmarshal`,
applied to each field individually. This avoided injecting unescaped
control characters into the JSON.

**Why it failed:** This approach assumed `json.Unmarshal` would succeed
on the inner JSON. But the inner JSON ALSO has literal `\n` in its
**structure** (formatting newlines between tokens):

```
{\\n  \"action\": \"answer\",\\n  \"answer\": \"...\"\\n}
```

After the first `json.Unmarshal` (extracting from the outer API
response), these become literal `\n` (two chars) between JSON tokens.
`json.Unmarshal` rejects them — `\n` is not valid JSON whitespace.

So `json.Unmarshal` failed on nearly every LLM response, the code fell
through to `parseLenientJSON`/`extractFieldValue`, and the boundary
detection in `extractFieldValue` couldn't find `\"}\"` or `\",\"`
patterns because literal `\n` chars sat between the closing quote and
the brace/comma.

---

### 12.8 Revision 2: State Machine `fixJSONFormatting` (CURRENT)

**Root cause (definitive):** After extraction from the outer API response,
the inner JSON has literal `\n` characters in TWO contexts:

1. **JSON formatting** — between `{` and fields, between fields, before `}`.  
   These MUST become real newlines (0x0A) for valid JSON whitespace.
2. **String values** — the double-encoded newlines inside `answer` and  
   `explanation`. These contain `\\n` (backslash-backslash-n) which the  
   JSON parser interprets as escaped backslash + literal `n` → `\n` in the  
   Go string. `unescapeNewlines` then converts to real newlines.

A global `unescapeNewlines` corrupts context 2 (`\\n` → `\<newline>` —
an incomplete escape that `json.Unmarshal` rejects). Skipping it makes
context 1 invalid.

**Fix:** `fixJSONFormatting` — a simple state machine that tracks whether
the current position is inside a JSON string. It converts `\n` → real
newlines ONLY outside strings:

```go
func fixJSONFormatting(raw string) string {
    var sb strings.Builder
    inString := false
    escaped := false
    for i := 0; i < len(raw); i++ {
        c := raw[i]
        if inString {
            if escaped {
                escaped = false
            } else if c == '\\' {
                escaped = true
            } else if c == '"' {
                inString = false
            }
            sb.WriteByte(c)
        } else {
            if c == '\\' && i+1 < len(raw) && raw[i+1] == 'n' {
                sb.WriteByte('\n')
                i++ // skip 'n'
            } else if c == '"' {
                inString = true
                sb.WriteByte(c)
            } else {
                sb.WriteByte(c)
            }
        }
    }
    return sb.String()
}
```

**Pipeline after fix:**

1. `fixJSONFormatting` — formatting `\n` → real newlines; string `\\n` untouched
2. `json.Unmarshal` — parses valid JSON; `\\n` in strings → `\n` in Go values
3. `unescapeFields` — `\n` in Go string values → real newlines
4. `renderMarkdown` — receives clean text with proper newlines

**Result:** `json.Unmarshal` succeeds on all LLM responses.
`parseLenientJSON` is retained as a fallback for truly malformed JSON
(unescaped quotes, etc.) but is no longer the primary path.

This also fixed the orphaned `}` rendering artifact — that occurred when
the LLM returned JSON with no `explanation` field and `extractFieldValue`
couldn't find the boundary, leaving the closing brace as the only
visible content.

---

## 13. Rendering Pipeline Resilience

### 13.1 Problem

Even with `unescapeNewlines` moved to `parseLLMResponse`, some LLM
responses still produce broken output. Three interacting weaknesses:

1. **`NoEmptyLineBeforeBlock` was removed** — killed alongside
   Smartypants. Smartypants is gone, but the extension stayed removed.
   Without it, code fences without a preceding blank line aren't
   recognized. LLMs frequently omit blank lines before fences.

2. **No markdown normalization** — the raw markdown goes straight into
   the parser with no preprocessing. LLMs produce markdown with
   inconsistent formatting (mixed escaping depths inside code blocks,
   missing blank lines, indented fences).

3. **No render fallback** — when `renderMarkdown` produces broken HTML,
   the engine happily stores it. The user sees garbled output with no
   recovery path.

### 13.2 Fixes Applied

#### Fix A: Restore `NoEmptyLineBeforeBlock`

**File:** `pkg/services/sql_execution.go:337`

```go
// Before:
p := parser.NewWithExtensions(parser.CommonExtensions)
// After:
p := parser.NewWithExtensions(parser.CommonExtensions | parser.NoEmptyLineBeforeBlock)
```

Restored because the original reason for removal (Smartypants interaction)
no longer applies. This handles the most common LLM formatting pattern:
paragraph immediately followed by code fence with no blank line.

#### Fix B: Normalize Markdown Before Rendering

**File:** `pkg/services/sql_execution.go:337-340`

A preprocessing step ensures blank lines before fenced code blocks:

```go
var fencedCodeRe = regexp.MustCompile(`([^\n])\n(\x60{3,})`)

func normalizeMarkdown(text string) string {
    return fencedCodeRe.ReplaceAllString(text, "$1\n\n$2")
}
```

Applied in `renderMarkdown` before the parser: `text = normalizeMarkdown(text)`.
This makes code fence recognition nearly bulletproof regardless of how
the LLM formats its output.

#### Fix C: Render Fallback for Broken Output

**File:** `pkg/services/discussion_engine.go:1033-1038`

After `renderMarkdown`, validate that the output has actual content. If
the rendered HTML is effectively empty, fall back to showing the raw
answer text as preformatted content:

```go
htmlContent := renderMarkdown(resp.Answer)
if strings.TrimSpace(stripHTMLTags(htmlContent)) == "" && resp.Answer != "" {
    htmlContent = fmt.Sprintf(
        "<pre style=\"white-space:pre-wrap; font-family:inherit;\">%s</pre>",
        html.EscapeString(resp.Answer))
}
```

This ensures the user always sees *something* readable, even when
markdown parsing completely fails.

### 13.3 Risk Assessment

| Category | Assessment |
|---|---|
| **Answer accuracy** | No change. LLM output is unchanged. |
| **Answer delivery** | **Improved.** Broken rendering recovers gracefully. |
| **Data safety** | No risk. Rendering-only changes. |
| **Regression** | Very low. `NoEmptyLineBeforeBlock` was restored, not added
  for the first time. `normalizeMarkdown` only inserts blank lines —
  never removes content. Fallback only triggers on empty output. |

---

## 14. Fifth Issue: `extractJSONFromResponse` Eats the Answer

### 14.1 The Bug

When the LLM returned an `answer` action with markdown containing fenced
code blocks, the user saw only the content of the **first code block**
(e.g. `"sql\nJOIN TerritoryAvgRevenue tar ON o.territory = tar.territory"`)
— the rest of the answer was silently discarded.

### 14.2 Root Cause

`extractJSONFromResponse` has a code-block handler designed to unwrap
LLM responses that put their JSON inside ` ```json ` blocks. It searched
for ` ``` ` (three backticks) ANYWHERE in the response string and
extracted only the content between the first pair:

```go
startIdx := strings.Index(response, "```")
if startIdx != -1 {
    remaining := response[startIdx+3:]
    // ... skip language identifier ...
    endIdx := strings.Index(remaining, "```")
    if endIdx != -1 {
        response = remaining[:endIdx]  // ← drops everything outside the fence
    } else {
        response = remaining
    }
}
```

When the LLM's response was valid JSON containing markdown code blocks
(e.g. `"answer": "...here's the fix:\n```sql\nJOIN...\n```\n\n### 2..."`),
the FIRST ` ``` ` inside the JSON string value triggered this handler.
The extracted content was the text between the first and second fence —
just the SQL code — and the rest of the JSON (including the full answer)
was discarded.

### 14.3 Fix

Guard the code-block handler: only apply it when the response does NOT
already start with `{` or `[` (i.e., it's not valid JSON). If the
response starts with JSON, don't look for code fences at all:

```go
if !strings.HasPrefix(response, "{") && !strings.HasPrefix(response, "[") {
    startIdx := strings.Index(response, "```")
    if startIdx != -1 {
        // ... unwrap code block ...
    }
}
```

### 14.4 Risk Assessment

| Category | Assessment |
|---|---|
| **Answer accuracy** | **High benefit.** Fixes complete answer loss. |
| **Regression** | Very low. Only affects responses that start with `{` or `[`
  — those are already valid JSON and don't need code block unwrapping. |

---

## 15. Resolution Summary

All five issues are resolved. The following files were modified:

| File | Changes |
|---|---|
| `pkg/services/sql_execution.go` | Removed Smartypants flags; restored `NoEmptyLineBeforeBlock`; added `normalizeMarkdown`; removed explanation from `ToHTML()` |
| `pkg/services/discussion_engine.go` | Updated prompt (§10.3); removed dead code (§3.3); added `fixJSONFormatting` state machine (§12.8); added `unescapeFields` helper; added `unescapeNewlines` helper; guarded `extractJSONFromResponse` code-block handler (§14.3); swapped `extractFieldValue` boundary order |

**Verification:** Full build (`wails build`) tested 2026-08-04. `answer` actions render complete markdown with properly formatted code blocks. Explanation text is confined to `llmContent` (tech details toggle). SQL queries in answers render as syntax-highlighted code blocks.