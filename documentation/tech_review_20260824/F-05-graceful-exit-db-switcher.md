# F-5 · Graceful shutdown before `os.Exit(0)` in db_switcher restart

**Severity:** Medium-High · **Effort:** Small · **Risk categories:** data integrity (app DB), reliability

## Problem Statement

`pkg/services/db_switcher.go` `restartApp()` ends with:

```go
cmd.Start()
...
os.Exit(0)
return nil // unreachable
```

`os.Exit(0)` terminates the process immediately. Wails' `OnShutdown`
hook (`app.shutdown` in `main.go`), whose documented responsibility is
`models.DB.Close()` (charter §2.2, §3.1), **never runs**.

The application's own SQLite database at `~/.yourql/yourql.db` (or the
user-chosen switch target) is opened by `modernc.org/sqlite`. Skipping
`Close()` means:

1. Any pooled connections with open read/write transactions are dropped;
   WAL/journal files are left for recovery-on-next-open. SQLite recovery is
   robust, but "robust recovery" is not the guarantee the charter demands —
   §4.0 rule 3 protects app-data integrity absolutely, and this path can in
   principle lose the last committed-in-flight writes or leave hot journals
   on abnormal setups (network homes, sync clients like Dropbox watching
   `~/.yourql`).
2. In-flight exports (`export.go`, `total_export.go`) writing files are cut
   off mid-stream — a half-written export file left behind with no error
   shown.
3. Any buffered log writers lose their tail.

Contrast with the updater's `os.Exit(0)` sites (`updater_{darwin,windows,linux}.go`):
those replace *the binary*, run detached scripts that wait on the PID, and
the charter (§3.8) accepts the hard-exit trade-off explicitly after verified
download + confirmation. The DB-switch path replaces *state the process
still holds open* — a different situation requiring different hygiene.

## Solution Design

Perform the critical shutdown work explicitly before exiting:

```go
func restartApp() error {
    ...
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to relaunch: %w", err)
    }

    // os.Exit skips Wails OnShutdown — do the integrity-critical part here.
    // (Charter §3.1: models.DB must be closed on quit; §4.0 rule 3.)
    if err := models.CloseDatabase(); err != nil {
        slog.Error("db_switcher: close database before restart", "err", err)
    }
    os.Exit(0)
    return nil // unreachable
}
```

If `models` currently lacks an exported close function (close logic lives in
the App method wired to OnShutdown), add one:

```go
// pkg/models/database.go
func CloseDatabase() error {
    if DB != nil {
        err := DB.Close()
        DB = nil
        return err
    }
    return nil
}
```

and have `App.shutdown(ctx)` delegate to it, so there is exactly one close
implementation (avoids the duplication smell flagged in F-7).

Additionally: guard against concurrent pipelines. If a conversation is
mid-processing when the user switches databases, closing `models.DB` under
it produces confusing errors. Check the discussion engine's activity state
(or reuse the headless-style inflight map if GUI code tracks one) and either
refuse the switch while processing is active ("Finish or cancel the current
question first") or cancel contexts and wait with a short deadline before
exiting. Refusing is simpler and more honest; recommend that.

## Implementation Plan

1. Add `models.CloseDatabase()`; refactor `App.shutdown` to call it.
2. In `restartApp()`: call `CloseDatabase()` before `os.Exit(0)`; log errors
   but proceed (the relaunch command is already running).
3. Add an active-processing check in the switch-database binding
   (`app.go` → `db_switcher.go` entry point); return a friendly refusal if a
   pipeline is running.
4. Comment each remaining `os.Exit(0)` site (updater files) stating which
   shutdown responsibilities were handled and why hard-exit is acceptable
   there (per §3.8) — prevents future readers from "fixing" or copying
   blindly.
5. Test manually: switch DB while idle → reopen → verify latest conversation
   edits present; switch during active message → verify refusal toast.

## Risk Assessment

- **Failure mode:** `CloseDatabase()` hangs on a stuck connection → app
  appears frozen instead of restarting. Mitigation: `modernc.org/sqlite`
  Close is non-blocking once queries finish; combined with step 3's
  refuse-while-active rule, no query should be mid-flight. Optionally wrap
  in a 2s watchdog goroutine that proceeds to exit anyway.
- **Regression risk:** minimal; strictly adds work before an existing exit.
- Charter §4.3: fixes a data-integrity edge case; isolated and testable.
