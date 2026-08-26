# Documentation

## Structure

```
documentation/
├── AGENT_READ_FIRST.md          ← Read this first. Project charter & risk framework.
├── AGENT_LOOP_DETAILS.md        ← Agentic loop: every path, tool, limit, prompt.
├── RISK_ANALYSIS_LOG.md         ← Log of risk/reward analyses.
├── TECH_REVIEW_20260824.md      ← Full-codebase technical review (2026-08-24).
├── README.md                    ← You are here.
│
├── architecture/                ← Architecture & design documents
│   ├── FUNCTIONALITY_SILO_DEFINITIONS.md
│   ├── FUNCTIONALITY_SILO_TARGET.md
│   ├── FUNCTIONALITY_SILO_PLAN.md
│   └── FINAL_MESSAGE.md
│
├── enhancements/                ← Feature design specs & enhancement proposals
│   └── (one file per feature/enhancement, dated)
│
├── issues/                      ← Bug reports & issue analyses
│   └── (one file per issue, dated)
│
├── testing/                     ← Test suite & harness documentation
│   └── (test design, execution framework, harness analysis)
│
└── operations/                  ← Release, deployment, setup, technical review
    ├── RELEASE_DEPLOYMENT.md
    ├── VERSION_UPGRADE.md
    ├── TECH_REVIEW.md
    └── HEADLESS_YOURQL.md
```

## Guidelines

- **Precedence:** `AGENT_READ_FIRST.md` is authoritative for goals, priorities,
  and risk tolerance; documents in subdirectories are point-in-time records.
  If a record conflicts with the charter, the charter wins (§ Document
  Precedence). Always re-verify specifics against live source.
- **`AGENT_READ_FIRST.md`** is the project charter — every agent must read it first.
- **`AGENT_LOOP_DETAILS.md`** is the living reference for the agentic tool-calling loop.
- **`RISK_ANALYSIS_LOG.md`** captures risk/reward analyses for non-trivial changes.
- New docs go in the appropriate subdirectory. Name files descriptively with dates when relevant (e.g., `FEATURE_X_20260819.md`).
- When a doc is superseded or implemented, add a status line at the top:
  `> **Status:** Implemented / Superseded / Pending`
