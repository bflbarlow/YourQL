# Technical Review 2026-08-24 — Finding Documents

One document per finding from [`../TECH_REVIEW_20260824.md`](../TECH_REVIEW_20260824.md).
Each document covers: problem statement, code-level analysis (verified against
source as of commit `5be20a1`), the proposed solution, a step-by-step
implementation plan, risk assessment per the charter's §4 framework, and the
tests that must accompany the change.

| ID | Document | Severity | Effort | Roadmap order |
|----|----------|----------|--------|---------------|
| F-1 | [F-01-query-context-propagation.md](F-01-query-context-propagation.md) | High | Small | 2 |
| F-2 | [F-02-sqlserver-limit-syntax.md](F-02-sqlserver-limit-syntax.md) | High | Medium | 3 |
| F-3 | [F-03-ci-pipeline.md](F-03-ci-pipeline.md) | High (process) | Small | 1 |
| F-4 | [F-04-test-expansion.md](F-04-test-expansion.md) | High | Medium | 6 |
| F-5 | [F-05-graceful-exit-db-switcher.md](F-05-graceful-exit-db-switcher.md) | Med-High | Small | 9 |
| F-6 | [F-06-updater-legacy-version-check.md](F-06-updater-legacy-version-check.md) | Low-Med | Trivial | 5 |
| F-7 | [F-07-updateinfo-type-dedup.md](F-07-updateinfo-type-dedup.md) | Low | Small | 5 |
| F-8 | [F-08-session-read-only-defense.md](F-08-session-read-only-defense.md) | High | Medium | 4 |
| F-9 | [F-09-oauth-scope-audit.md](F-09-oauth-scope-audit.md) | Medium | Small | 11 |
| F-10 | [F-10-headless-bearer-token.md](F-10-headless-bearer-token.md) | Med-High | Small | 7 |
| F-11 | [F-11-keychain-credentials.md](F-11-keychain-credentials.md) | Medium | Large | 10 |
| F-12 | [F-12-slog-driver-logging.md](F-12-slog-driver-logging.md) | Low | Small | 11 |
| F-13 | [F-13-llm-result-truncation.md](F-13-llm-result-truncation.md) | Medium | Medium | 8 |
| F-14 | [F-14-secret-leak-ci-guard.md](F-14-secret-leak-ci-guard.md) | Low | Small | 11 |
| F-15 | [F-15-readme-model-lists.md](F-15-readme-model-lists.md) | Low | Trivial | 11 |
| F-16 | [F-16-app-go-decomposition.md](F-16-app-go-decomposition.md) | Medium | Large | 10 |
| F-17 | [F-17-frontend-componentization.md](F-17-frontend-componentization.md) | Medium | Large | 10 |
| F-18 | [F-18-html-css-class-extraction.md](F-18-html-css-class-extraction.md) | Low-Med | Medium | 10 |
| F-19 | [F-19-dead-code-smells.md](F-19-dead-code-smells.md) | Low | Trivial | 5 |
| F-20 | [F-20-commit-hygiene.md](F-20-commit-hygiene.md) | Low | Trivial | 11 |

**Charter precedence:** every plan here defers to
`documentation/AGENT_READ_FIRST.md`. None of these changes weaken the Data
Source Read-Only Invariant (§0), log secrets, or perform destructive
migrations against `~/.yourql/yourql.db`.
