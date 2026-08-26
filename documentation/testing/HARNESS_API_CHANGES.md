> ⏳ **Point-in-time record** — this document describes work as of its original date. Re-verify all specifics (file paths, line numbers, behavior) against the live source before relying on them. For goals and priorities, `documentation/AGENT_READ_FIRST.md` always wins.

# Headless API Change Notice — Clarification Failure Modes

**Date:** 2026-08-17
**To:** yourql-test-harness maintainers
**From:** YourQL core

---

## 1. What's changing

Currently, when a YourQL conversation produces a clarification — e.g., "I received
an incomplete response. Could you try rephrasing your question?" — the headless
API returns `status: "completed"` with a `Query` record whose `error_category`
and `error_message` fields are `null`. The only way to determine *why* the
clarification happened is to inspect the natural-language message content, and
that prose is identical across multiple distinct failure modes.

We are adding **structured failure-mode signals** to the `Query` record so the
harness can distinguish failure modes without grepping prose.

## 2. What the harness will see — `query.error_category`

The `Query` record (already returned in `messageResponse.query`) will now carry
one of these values in `error_category` when `query.status == "clarification"`:

| `error_category` | Meaning | Typical cause |
|---|---|---|
| `empty_response` | LLM returned 0 tokens and no tool calls | Provider timeout, server crash, transient failure |
| `context_overflow` | Prompt tokens were over 90% of the model's context window and 0 completion tokens were produced | The question + schema + history exceeded the model's capacity. Try breaking into smaller questions. |
| `loop_exhausted` | The agentic loop hit the maximum-round safety cap | Repeated invalid responses, retry loops, or a conversation that couldn't converge |
| `sql_error` | A SQL query was generated and executed but returned an error | Bad SQL, dialect mismatch, reserved-word collision (e.g., `div` as alias) |
| `null` | No clarification occurred — the query completed normally | Not a failure |

> **Note:** `sql_error` is *not new* — it was already reported for some error
> paths. It is included here for completeness because it overlaps with the
> "incomplete response" diagnostic class.

In addition, `query.error_message` may carry a short human-readable string
describing the specific error (e.g., `"model returned 0 tokens"` or
`"Error 1064: syntax error near 'div'"`). This is supplementary — the
`error_category` is the stable value to switch on.

## 3. How to read it

The existing harness code already receives the full `messageResponse` from
the headless API. No new fields are added to the top-level response.

```json
{
  "status": "completed",
  "query": {
    "status": "clarification",
    "error_category": "context_overflow",
    "error_message": "prompt 8192 tokens vs 8192 limit; 0 completion tokens"
  }
}
```

Access pattern (pseudocode):

```python
if message.query and message.query.status == "clarification":
    cat = message.query.error_category or "unknown"
    # "empty_response" / "context_overflow" / "unparseable_output" /
    # "loop_exhausted" / "sql_error"
```

## 4. Grading implications

### Use `error_category` to refine pass/fail decisions

| Old behavior | New behavior |
|---|---|
| All clarifications graded the same — typically "incomplete" or "fail" | Differentiate by root cause: |
| | `context_overflow` → "the question is too complex, not the model's fault" — could be a pass, a warning, or a separate "needs-decomposition" grade |
| | `empty_response` / `unparseable_output` → genuine model/provider failure — likely a fail |
| | `loop_exhausted` → the model couldn't converge — could be a fail or a "needs investigation" flag |
| | `sql_error` → the model tried but the SQL was bad — might be a partial credit depending on how close it got |

### Don't rely on message-content grep anymore

The user-facing prose in `message.content` will also become differentiated per
mode (e.g., "This question is too complex for the current context window" vs.
"The model returned no response"). But the prose is meant for human reading.
Switch your grading logic to `error_category` — it's stable, guaranteed lowercase,
and won't change with phrasing updates.

### `status: "completed"` is still correct

Clarification is a *successful* pipeline completion (the app did its job — it
detected a problem and asked the user for help). The harness should NOT treat
`status != "error"` as "no failure." Instead, check `query.status == "clarification"`
plus `query.error_category` to detect failures the pipeline handled gracefully.

## 5. Backward compatibility

- `messageResponse.status` — unchanged (still `"completed"` for clarifications).
- `messageResponse.error` — unchanged (still `null` for clarifications; only set
  for hard pipeline errors).
- `messageResponse.query` — unchanged shape. Only the *values* of existing
  fields (`error_category`, `error_message`) change from `null` to strings.
- Message content — the prose in `messages_added[].content` will change to
  mode-specific text, but if your grader currently does exact-string matching on
  "I received an incomplete response," it may stop matching. Switch to
  `error_category`.

## 6. Upcoming: summarization signal

A separate change (also in progress) will add `summary_generated: true/false` to
the assistant message metadata. This gives the harness a reliable binary for
whether the LLM summarization call actually ran and produced output — replacing the
current heuristic (content ≥ 200 chars + data-backed) with a direct measurement.

That change will be communicated in a follow-up notice. It uses the same pattern:
an existing metadata field that was always present, now populated with a
meaningful value.

---

*Questions or edge cases? File an issue in the yourql-test-harness repo and tag*
*the core team for clarification.*