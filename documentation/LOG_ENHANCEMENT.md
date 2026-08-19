# Log Enhancement

**Date:** 2026-08-11
**Project:** YourQL — Add file-based diagnostic logging with UI controls

---

## 1. Motivation

YourQL currently writes all logs (`log.Printf`, `slog.Info`, `slog.Error`, etc.) to
`os.Stderr`. As a macOS GUI app launched from Finder, stderr is invisible — there is
no terminal, no Console.app integration, and no log file. This means:

- **Errors are silently lost.** The `[AgenticLoop] Summarization failed: ...` message
  and every other diagnostic log line vanish into nothing.
- **Debugging requires a terminal launch.** Users and developers must run `./YourQL`
  from the command line to see what happened, which is inconvenient for casual users.
- **No audit trail.** There is no record of provider errors, SQL failures, or other
  issues that could help diagnose problems after the fact.

### What Users Want

- A simple toggle to enable file-based logging when diagnosing a problem
- A way to export the log file without hunting through `~/.yourql/`
- A way to clear the log to start fresh

---

## 2. Design

### 2.1 Log File

- **Path:** `~/.yourql/yourql.log`
- **Format:** Plain text. Each line is one log entry.
- **Rotation:** None — the export-and-clear button serves as manual rotation.
- **Max size:** Unbounded while logging is enabled. The file grows until the user
  exports and clears it, or disables logging and manually deletes the file.

### 2.2 Setting Key

A new entry in the `app_settings` table:

| Key | Values | Default |
|---|---|---|
| `logging_enabled` | `"true"` / `"false"` | `"false"` |

### 2.3 Startup Behavior

In `main()`, after the `.env` load but before `wails.Run()`:

1. Check `~/.yourql/` exists (already created by `models.ConnectDatabase()` during
   `startup` — but `main()` runs first, so we create it here too if needed).
2. Read `logging_enabled` from `app_settings`. Since this runs **before** the database
   is connected, we check the SQLite file directly or read a simple flag file.
   
   **Design decision:** The `app_settings` table isn't available in `main()` (before
   `startup`), so we need an alternate mechanism. Options:
   
   a. **Defer to `startup()`** — Open the log file during `startup()` instead of `main()`.
      This is cleaner and uses the existing DB. Logs before `startup()` go to stderr
      as before (this is fine — those are only a few lines).
   
   b. **Use a separate flag file** — `~/.yourql/logging_enabled` (empty marker file).
      More work, two sources of truth.
   
   **Chosen: (a) Defer to `startup()`.** The handful of log lines emitted between
   `main()` and `startup()` are not worth the complexity of a pre-DB flag file.

3. If logging is enabled, open `~/.yourql/yourql.log` in append mode and configure:
   - `log.SetOutput(io.MultiWriter(os.Stderr, logFile))` — both stderr and file
   - `slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), nil)))`

4. The log file handle is stored in a package-level variable so it can be closed
   cleanly in `shutdown()`.

### 2.4 Wails Bindings (app.go)

Three new methods on `*App`:

| Method | Purpose |
|---|---|
| `GetLoggingEnabled() (bool, error)` | Read the `logging_enabled` setting |
| `SetLoggingEnabled(enabled bool) error` | Toggle logging on/off |
| `ExportLog() (string, error)` | Returns the log content as a string for the frontend to save |
| `ClearLog() error` | Truncates the log file |

**`SetLoggingEnabled` semantics:** When toggled, persist the setting to `app_settings`
and instruct the frontend to prompt the user to restart the app for the change to
take effect. Runtime log redirection is possible but complex (closing/reopening file
handles, thread safety with in-flight log calls); a restart is simpler and safer.

**`ExportLog` semantics:** Reads the log file and returns its contents as a string.
The frontend opens a native save dialog to let the user choose where to save.
Does not modify the log file.

**`ClearLog` semantics:** Truncates the log file to zero bytes. If the file is
currently open for writing (logging enabled), uses the open handle for safe
truncation. Otherwise writes an empty file.

### 2.5 Frontend (SettingsView.svelte)

A new "Diagnostic Logging" form card in the General tab, below the Advanced toggle:

```
┌─ Diagnostic Logging ─────────────────────────────────┐
│                                                        │
│  ☑ Enable diagnostic logging                          │
│  Logs application activity to ~/.yourql/yourql.log    │
│  for troubleshooting. Changes take effect after       │
│  restart.                                              │
│                                                        │
│  [Export Log]  [Clear Log]                             │
│                                                        │
└────────────────────────────────────────────────────────┘
```

- Checkbox bound to `loggingEnabled` state
- On toggle: call `SetLoggingEnabled()`, update state, show restart notice
- Export button: call `ExportAndClearLog()`, open save dialog with `runtime.SaveFileDialog`
- Export button disabled state: frontend checks if log file exists/non-empty via a
  new check binding, or always enabled (simpler — if empty, the export will just save
  an empty file which is harmless)

---

## 3. Files Changed

| File | Change |
|---|---|
| `main.go` | Add `logFile *os.File` var, configure logging in `startup()`, close in `shutdown()` |
| `app.go` | Add `GetLoggingEnabled`, `SetLoggingEnabled`, `ExportAndClearLog` bindings + helper |
| `frontend/src/SettingsView.svelte` | Add "Diagnostic Logging" card to General tab |
| `pkg/models/database.go` | No changes (uses existing `app_settings` table) |
| `pkg/services/app_settings.go` | No changes (existing functions suffice) |

**No schema migration needed.** The `app_settings` key-value table already exists
and `SetAppSetting` handles upserts.

---

## 4. Risk Assessment

See [`RISK_ANALYSIS_LOG.md` §2026-08-11 — Diagnostic Logging Feature](RISK_ANALYSIS_LOG.md).

---

## 5. Testing Checklist

- [ ] Logging disabled by default on fresh install
- [ ] Enable logging → setting persists across restart
- [ ] Log file created at `~/.yourql/yourql.log` after restart with logging enabled
- [ ] Both `log.Printf` and `slog.*` output appear in the log file
- [ ] Log file continues to grow with app usage (append mode)
- [ ] Export & Clear button saves log content, truncates file
- [ ] Disable logging → logs stop going to file after restart
- [ ] Log file handle closed cleanly on shutdown (no leaked FDs)
- [ ] Both light and dark themes verified for new UI elements
- [ ] No API keys or passwords in log output (existing rule, now visible in file)