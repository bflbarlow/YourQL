# Risk/Reward Analysis Log

> Companion to `AGENT_READ_FIRST.md`. Agents should document every
> non-trivial risk/reward decision here, most recent entry first, so the
> project accumulates a real history of trade-off decisions.

This log uses the template defined in `AGENT_READ_FIRST.md` §4.4.

---

<!-- Add new entries above this line -->

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
