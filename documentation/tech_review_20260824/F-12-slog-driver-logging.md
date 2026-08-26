# F-12 · Standardize driver logging on structured `slog` with redaction

**Severity:** Low · **Effort:** Small · **Risk categories:** credential protection, user trust, maintainability

## Problem Statement

Two logging regimes coexist:

- `log/slog` — used by `main.go`, `headless.go` (structured, leveled)
- stdlib `log.Printf` — used across all nine DB drivers and `pkg/engine/loop.go`

Driver lines verified in source emit usernames/hosts/DSNs:

```
db_redshift.go:93   log.Printf("[redshift] Built DSN for database %s, user %s, host %s:%d", …)
db_mysql.go:87      log.Printf("[mysql] Built DSN: %s", redactedDSN)   // good pattern
db_snowflake.go:98  log.Printf("[snowflake] Built DSN for account %s, database %s, user %s", …)
db_postgres.go:95 / db_mariadb.go:56 / db_sqlserver.go:97 / db_bigquery.go:55 …
```

Issues:

1. **No passwords are logged** (verified; mysql even redacts the DSN) — but
   usernames, hosts, accounts, and project IDs are emitted *unconditionally*
   at whatever the global log level is. These are mildly sensitive
   (usernames are half of credential pairs; internal hostnames leak
   topology into shared logs/ticket attachments).
2. **Inconsistency**: two regimes means no single place to set verbosity;
   headless deployments can't silence driver chatter without hijacking the
   stdlib default logger.
3. **Redaction is per-driver ad hoc** — only mysql has a `redactedDSN`;
   correctness depends on each future driver author remembering.

## Solution Design

### 1. One structured logger convention

Drivers use package-level:

```go
// pkg/services/logging.go
var dbLog = slog.Default().WithGroup("db")
```

Convert each site:

```go
dbLog.Debug("dsn built", "driver", "mysql", "database", database,
            "host", host, "port", port)          // user dropped entirely at Debug
```

Rules:
- Anything identifying (user, host, account, project/dataset) → `Debug`
  only. Default `Info` level shows nothing identifying.
- Connection success/failure outcomes → `Info` with non-identifying fields
  (`driver`, `ok`, latency).
- `pkg/engine/loop.go`'s `[AgenticLoop] log.Printf` lines → `slog.Debug`
  as well (loop telemetry is developer-facing). Note the engine silo rule:
  `log/slog` is stdlib, so importing it does not violate the black-box
  constraint (no models/services/Wails imports involved).

### 2. Central redaction (pairs with F-14 Guard 2)

All DSN-shaped strings pass through `services.RedactSecret(s)` before any
logging; mysql's bespoke `redactedDSN` becomes a call to the shared helper.

### 3. Level configuration

Headless gains `--log-level debug|info` flag mapping to
`slog.SetLogLoggerLevel` / handler options; GUI keeps defaults. This makes
the existing "check any new slog or log calls" charter rule (§3.5) easy to
honor: identifying data simply never compiles into Info-level paths.

## Implementation Plan

1. Add `logging.go` helper + RedactSecret (if not yet from F-14).
2. Mechanical sweep of the nine driver files + loop.go; ~20 call sites.
3. Grep gates: CI test asserting `log.Printf(` absent from
   `pkg/services/db_*.go` and `pkg/engine/*.go` (extend F-14's scanner).
4. Manual check: run `wails dev`, connect MySQL, confirm console shows no
   host/user at default level, everything at `--log-level debug`.
5. Charter §3.5 unchanged in substance; add pointer comment in code.

## Risk Assessment

Minimal — log-line rewrites only. Risk of losing debugging detail is
mitigated by the debug level retaining strictly more than today.
