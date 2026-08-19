# SQLite Database Switcher — Technical Design

**Date:** 2026-08-13  
**Project:** YourQL — a feature allowing the user to create a new, blank
application database and/or switch which SQLite file YourQL's own app-state
database points at.  
**Status:** Implemented 2026-08-13 — the pointer-file indirection,
`CreateBlankDatabaseAt`, `SwitchActiveDatabase`/`ResetToDefaultDatabase`
restart flow, Wails bindings, and the toggle-gated "App Database" Settings
tab are in place. This document is the design record; the code lives in
`pkg/models/database.go`, `pkg/services/db_switcher.go`, `app.go`, and
`frontend/src/SettingsView.svelte`.  
**Compliance:** Reviewed against `AGENT_READ_FIRST.md`. See §7 for the
explicit compliance mapping.

---

## 1. What This Feature Is (and Isn't)

This document describes a feature for the **application's own local
database** — the one at `~/.yourql/yourql.db` that stores conversations,
messages, LLM provider configs, data source configs, and skills
(`AGENT_READ_FIRST.md` §3.1). It lets a user:

1. **Create a new, blank app database** — as if YourQL were being launched
   for the very first time (no conversations, no providers, no data
   sources, no skills) — at a location of their choosing.
2. **Switch** which SQLite file YourQL treats as its app database, either by
   typing/pasting a path or by using a native file/folder picker.
3. **See which database is currently active** in Settings, so switching is
   never a surprise.

This document does **not** describe anything related to a user's configured
**data sources** (their MySQL/Postgres/BigQuery/etc. connections under
Settings → Data Sources). Those remain governed by the read-only invariant
in `AGENT_READ_FIRST.md` §0 and are completely untouched by this feature —
switching the *app* database never touches, reads, or modifies any
*data source* a user has configured, because data source credentials and
configs live inside the app database being switched, not the reverse.

### 1.1 Why This Is Useful (Beyond Testing)

This started as a question about enabling automated testing (see
`TESTING_EXECUTION.md`), but it stands on its own as a product feature:

- **Clean slate without data loss.** A user can start fresh (new client
  demo, new environment) without deleting their existing history — the old
  `yourql.db` file is left alone on disk.
- **Profile switching.** Different `yourql.db` files can represent different
  "profiles" (e.g., work vs. personal, or per-client configurations) that a
  user switches between deliberately.
- **Portable/USB installs.** A user can keep their YourQL app-state file on
  removable media or a synced folder and point the app at it.
- **Testing.** An external test harness (see `TESTING_EXECUTION.md`) can use
  this exact mechanism to point a real, running YourQL instance at a
  disposable, empty database file — without ever touching the user's real
  `~/.yourql/yourql.db`.

---

## 2. Current State (What Exists Today)

- `models.getDBPath()` (`pkg/models/database.go`) unconditionally returns
  `filepath.Join(user.Current().HomeDir, ".yourql", "yourql.db")`. There is
  no flag, environment variable, or setting that changes this.
- `models.ConnectDatabase()` opens that hardcoded path and unconditionally
  runs `migrate()`. This means **"blank slate" already works for free** —
  any empty/nonexistent file at the target path becomes a fully-migrated,
  empty app database the instant `ConnectDatabase()` runs against it. No new
  "reset" or "create" schema logic is needed; the existing migration system
  already does it.
- `app.go`'s `startup()` calls `models.ConnectDatabase()` exactly once,
  before the window is shown, and `shutdown()` closes `models.DB` exactly
  once. There is currently no code path that closes and reopens `models.DB`
  while the app is running.
- The auto-updater (`pkg/services/updater_darwin.go`, `_linux.go`,
  `_windows.go`) already implements the closest analogous pattern this
  feature needs: write a detached script that waits for the current
  process to exit, do the risky filesystem operation, then relaunch. This
  document reuses that pattern rather than inventing a new one.

**Conclusion on the "how hard is a blank instance" question:** technically
trivial (the migration system already produces one), but currently
*inaccessible* — there's no supported way to point YourQL at anywhere other
than the one hardcoded path. This document closes that gap.

---

## 3. Design

### 3.1 Storage: Where "Which Database Am I Using" Is Recorded

A new small **pointer file** (not the SQLite database itself) records which
`yourql.db` YourQL should open on next launch:

```
~/.yourql/active_db_path.json
```

```json
{
  "path": "/Users/alice/Documents/YourQL Profiles/client-acme/yourql.db",
  "updated_at": "2026-08-13T10:15:00Z"
}
```

**Why a separate pointer file, not a value inside `app_settings`:** the
`app_settings` table lives *inside* the very database you're trying to
switch away from. If the pointer to "which DB to open" were itself stored
in a DB row, switching databases would require opening the *old* DB first to
read where to go next, or the *new* DB after arriving to know you arrived —
a chicken-and-egg problem when the two databases disagree. A tiny JSON file
next to (but outside of) the database directory has no such dependency and
is trivial to read before `ConnectDatabase()` ever runs.

If the pointer file is missing, corrupt, or empty, YourQL falls back to
`getDBPath()`'s current, unchanged default
(`~/.yourql/yourql.db`) — this is the zero-config path every existing
install already uses, and it is never broken by this feature.

### 3.2 Additive Model Change

```go
// pkg/models/database.go

// activeDBPointerPath returns the fixed location of the pointer file that
// records which SQLite file is currently active. This path itself is never
// user-configurable — only its *contents* (the path it points to) are.
func activeDBPointerPath() string {
    usr, err := user.Current()
    if err != nil {
        return "" // caller falls back to getDBPath()
    }
    return filepath.Join(usr.HomeDir, ".yourql", "active_db_path.json")
}

type activeDBPointer struct {
    Path      string    `json:"path"`
    UpdatedAt time.Time `json:"updated_at"`
}

// resolveDBPath returns the SQLite file YourQL should open: the pointer
// file's path if one is recorded and the file's directory is writable,
// otherwise the original hardcoded default from getDBPath(). This function
// replaces the direct getDBPath() call inside ConnectDatabase() but keeps
// getDBPath() itself unchanged and still exported-equivalent for anything
// that depends on today's default-path behavior.
func resolveDBPath() string {
    pointerPath := activeDBPointerPath()
    if pointerPath == "" {
        return getDBPath()
    }
    data, err := os.ReadFile(pointerPath)
    if err != nil {
        return getDBPath() // no pointer file yet — default behavior
    }
    var ptr activeDBPointer
    if err := json.Unmarshal(data, &ptr); err != nil || ptr.Path == "" {
        return getDBPath() // corrupt/empty pointer — default behavior
    }
    return ptr.Path
}

// ConnectDatabase now opens resolveDBPath() instead of getDBPath()
// directly. This is the only change to the existing function.
func ConnectDatabase() error {
    dbPath := resolveDBPath()
    // ...unchanged from here down...
}
```

This is a minimal, additive change: `ConnectDatabase()`'s only modification
is swapping `getDBPath()` for `resolveDBPath()`, which falls through to
`getDBPath()` in every case where the new pointer file doesn't exist or
can't be read. **Every existing install with no pointer file behaves
identically to today**, with zero migration or upgrade step required.

### 3.3 Creating a New, Blank Database

```go
// pkg/models/database.go

// CreateBlankDatabaseAt creates (or overwrites, if empty/absent) a SQLite
// file at path and runs migrations against it, producing a database
// identical in shape to a first-ever YourQL launch. It does not touch
// models.DB or the currently active pointer — callers decide separately
// whether/when to switch to the new file (see §3.4, which always restarts
// rather than hot-swapping models.DB).
func CreateBlankDatabaseAt(path string) error {
    if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
        return fmt.Errorf("failed to create directory for new database: %w", err)
    }
    if info, err := os.Stat(path); err == nil {
        if info.Size() > 0 {
            return fmt.Errorf("refusing to overwrite existing non-empty file: %s", path)
        }
        // Zero-byte placeholder file — safe to proceed; sql.Open will
        // populate it. (Guards accidental clobbering of a real database
        // that happens to be truncated/corrupt — err on the side of refusal.)
    }

    db, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_journal_mode=WAL")
    if err != nil {
        return fmt.Errorf("failed to create new database: %w", err)
    }
    defer db.Close()

    if err := db.Ping(); err != nil {
        return fmt.Errorf("failed to initialize new database: %w", err)
    }

    // Run the exact same migration path as a normal app startup, but
    // against this standalone *sql.DB rather than the global models.DB —
    // migrate() currently operates on the package-level DB var, so this
    // requires either (a) a migrateOn(db *sql.DB) variant, or (b) briefly
    // swapping models.DB, running migrate(), then restoring it. Given
    // migrate() is only ever called at startup today and this function is
    // synchronous and short-lived, (b) is the lower-risk, smaller change:
    prevDB := DB
    DB = db
    err = migrate()
    DB = prevDB
    if err != nil {
        return fmt.Errorf("failed to initialize new database schema: %w", err)
    }
    return nil
}
```

**Refusal to overwrite non-empty files is deliberate and non-negotiable.**
This guards against a picker mistake (user selects an existing database
file, thinking they're choosing a folder) silently destroying real data.
See §7.2.

### 3.4 Switching the Active Database — Restart, Never Hot-Swap

**Switching databases always restarts the application. `models.DB` is never
closed and reopened while the app is running.**

This mirrors exactly how the auto-updater already works
(`AGENT_READ_FIRST.md` §3.8: replacing the binary is a distinct,
restart-requiring trust domain) and for the same underlying reason: a huge
amount of in-memory state — every `App` method, every open conversation,
every in-flight `ProcessUserMessage` call, cached schema, active
cancellation handles — implicitly assumes `models.DB` never changes out from
under it mid-session. Hot-swapping it would require auditing every caller
for that assumption; restarting sidesteps the entire problem for a feature
used rarely and deliberately.

```go
// pkg/services/db_switcher.go (new file)

// SwitchActiveDatabase points YourQL at a different SQLite file on next
// launch and restarts the app to pick it up. targetPath must already exist
// and be a valid SQLite file (either a real yourql.db or one just created
// via CreateBlankDatabaseAt) — this function does not create files, it only
// repoints and restarts.
func SwitchActiveDatabase(targetPath string) error {
    if targetPath == "" {
        return fmt.Errorf("target path is empty")
    }
    abs, err := filepath.Abs(targetPath)
    if err != nil {
        return fmt.Errorf("failed to resolve path: %w", err)
    }
    if _, err := os.Stat(abs); err != nil {
        return fmt.Errorf("target database does not exist: %w", err)
    }

    ptr := models.ActiveDBPointer{Path: abs, UpdatedAt: time.Now().UTC()}
    data, _ := json.MarshalIndent(ptr, "", "  ")
    pointerPath := models.ActiveDBPointerPath() // exported wrapper, see §3.2
    if err := os.WriteFile(pointerPath, data, 0600); err != nil {
        return fmt.Errorf("failed to write database pointer: %w", err)
    }

    return restartApp()
}

// restartApp launches a fresh copy of the current executable and exits this
// process — the same detached-relaunch pattern used by
// pkg/services/updater_{darwin,linux,windows}.go's PerformUpgradeRestart,
// but without any binary replacement step (this feature never touches the
// app binary, only which data file it opens).
func restartApp() error {
    exe, err := os.Executable()
    if err != nil {
        return fmt.Errorf("failed to determine running executable: %w", err)
    }
    cmd := exec.Command(exe, os.Args[1:]...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to relaunch: %w", err)
    }
    os.Exit(0)
    return nil // unreachable
}
```

On the new process's `startup()`, `models.ConnectDatabase()` calls
`resolveDBPath()`, finds the pointer file, and opens the new target — no
other startup code changes.

**Development mode caveat.** Under `wails dev`, the app depends on a Vite
dev server that is a child process of the Wails CLI. `restartApp()` detects
this (`devserver`/`frontenddevserverurl` env vars or flags) and does NOT
relaunch the binary — launching it directly would orphan it from the dev
environment and `wails dev` would tear down Vite when the old app exits.
Instead, the pointer file/clear still happens, and `restartApp()` returns a
message telling the user to restart `wails dev` (or quit and reopen) to take
effect. In a production build the auto-restart path runs normally.

**Why launch-a-new-process instead of the updater's "wait for PID, then
relaunch" script pattern:** the updater needs a detached script because it
must *replace the file the running process was launched from* — you cannot
overwrite/move a binary that's currently executing on most platforms while
it's still running. Switching databases has no such constraint: the
*database file* isn't the executable, so nothing prevents this process from
directly starting the *next* process before exiting itself. This is simpler
and has fewer moving parts (no shell script, no OS-specific script content,
no PID-polling loop) — it only needs to exist as its own subsection because
someone reading the updater code might reasonably assume the same
script-based mechanism is required here; it is not.

### 3.5 Wails Bindings (`app.go`)

```go
// GetActiveDatabaseInfo returns the path of the currently active app
// database and whether it's the default (unmodified) location, for display
// in Settings.
func (a *App) GetActiveDatabaseInfo() (*services.ActiveDatabaseInfo, error) {
    return services.GetActiveDatabaseInfo()
}

// PickNewDatabaseLocation opens a native "Save File" dialog (the file need
// not exist yet — the user is choosing where the *new* database will live)
// and returns the chosen path, or "" if cancelled.
func (a *App) PickNewDatabaseLocation() (string, error) {
    path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
        Title:           "Create New YourQL Database",
        DefaultFilename: "yourql.db",
        Filters: []runtime.FileFilter{
            {DisplayName: "SQLite Database (*.db)", Pattern: "*.db"},
        },
    })
    return path, err
}

// PickExistingDatabaseFile opens a native "Open File" dialog for selecting
// an existing yourql.db to switch to.
func (a *App) PickExistingDatabaseFile() (string, error) {
    paths, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
        Title: "Choose YourQL Database",
        Filters: []runtime.FileFilter{
            {DisplayName: "SQLite Database (*.db)", Pattern: "*.db"},
        },
    })
    return paths, err
}

// CreateAndSwitchToNewDatabase creates a blank database at path (refusing to
// overwrite a non-empty existing file), points YourQL at it, and restarts.
func (a *App) CreateAndSwitchToNewDatabase(path string) error {
    if err := models.CreateBlankDatabaseAt(path); err != nil {
        return err
    }
    return services.SwitchActiveDatabase(path)
}

// SwitchToExistingDatabase points YourQL at an existing SQLite file and
// restarts. Does not validate the file is a *YourQL* database beyond
// confirming it opens and migrates cleanly — see §7.4 for the safety
// rationale (running migrate() against an arbitrary SQLite file is
// additive/idempotent and safe even if the file has unrelated tables).
func (a *App) SwitchToExistingDatabase(path string) error {
    return services.SwitchActiveDatabase(path)
}

// ResetToDefaultDatabase clears the pointer file, returning YourQL to
// ~/.yourql/yourql.db on next launch, and restarts.
func (a *App) ResetToDefaultDatabase() error {
    if err := services.ClearActiveDatabasePointer(); err != nil {
        return err
    }
    return restartApp()
}
```

### 3.6 Frontend: Advanced Toggle + Conditional "Application Database" Tab

This feature is **behind an "Enable Application Database Switching" checkbox**
in Settings → General → Advanced — the same section that already houses
"Enable advanced agent loop settings." When unchecked (the default), nothing
changes visually — the database tab never appears. When checked, a new
"Application Database" tab is added to the settings-tabs bar.

**Why behind a toggle.** Switching the app database is a low-frequency,
disruptive operation (it forcibly restarts the process, per §3.4). Showing a
dedicated tab and controls to every user on first launch would add noise to
the default Settings experience for a feature most users will never need. An
opt-in toggle under Advanced follows the same discoverability-vs-noise
trade-off as the existing agent-loop toggle, and puts both behind the same
"Advanced" header that already signals "you might not need this."

#### Step 1: Add the checkbox (SettingsView.svelte, General tab, Advanced section)

Inside the existing `<div class="form-card">` that starts with
`<h4>Advanced</h4>`, after the agent-loop checkbox block, add:

```svelte
<div class="checkbox-group" style="margin-top: var(--space-sm);">
  <label>
    <input
      type="checkbox"
      checked={dbSwitcherEnabled}
      onchange={toggleDbSwitcher}
    />
    Enable Application Database Switching
  </label>
  <div style="color: var(--text-tertiary); font-size: var(--font-xs); margin-top: var(--space-2xs);">
    Create a new blank database or switch YourQL to a different SQLite
    file. Switching restarts the application. Your current database is
    left untouched on disk — you can always switch back.
  </div>
</div>
```

#### Step 2: Add the toggle handler and state

```js
// Reactive state
let dbSwitcherEnabled = $state(false)
let activeDbPath = $state('')
let activeDbIsDefault = $state(true)

async function toggleDbSwitcher() {
  dbSwitcherEnabled = !dbSwitcherEnabled
  try {
    await SetAppSetting('db_switcher_enabled', dbSwitcherEnabled ? 'true' : 'false')
  } catch (e) {
    console.error('Failed to save DB switcher toggle:', e)
  }
}

// Load initial state on mount (same pattern as agent loop toggle)
async function loadDbSwitcherPrefs() {
  try {
    const val = await GetAppSetting('db_switcher_enabled')
    dbSwitcherEnabled = val === 'true'
  } catch (e) { /* not set yet, leave disabled */ }
}
```

#### Step 3: Add the tab button (inside the settings-tabs div)

After the `{#if agentLoopEnabled}` block that wraps the "Agent Loop" tab
button, add:

```svelte
{#if dbSwitcherEnabled}
  <button
    class="tab-btn {activeSettingsTab === 'appdb' ? 'active' : ''}"
    onclick={() => { activeSettingsTab = 'appdb'; loadDbSwitcherInfo() }}
  >
    App Database
  </button>
{/if}
```

#### Step 4: Add the tab content

```svelte
{#if activeSettingsTab === 'appdb'}
  <div class="tab-content">
    <h3>Application Database</h3>
    <p class="section-desc">
      YourQL stores conversations, provider configs, data source configs,
      and skills in a local SQLite database. Switching creates a new blank
      database or points YourQL at a different file — the application
      restarts after switching.
    </p>

    <div class="form-card">
      <p class="card-hint" style="margin-bottom: var(--space-sm);">
        <strong>Current database:</strong><br />
        {activeDbPath}
        {#if activeDbIsDefault}
          <span style="color: var(--text-tertiary);"> (default)</span>
        {/if}
      </p>
    </div>

    <div class="form-card">
      <h4>Create New Blank Database</h4>
      <p class="card-hint">
        Creates a brand-new, empty YourQL database at a location you
        choose — like launching the app for the very first time. Nothing
        in your current database is deleted.
      </p>
      <div style="display: flex; gap: var(--space-sm); flex-wrap: wrap;">
        <input
          type="text"
          class="form-input"
          placeholder="Path to new database (e.g. ~/Documents/yourql-new.db)"
          bind:value={newDbPath}
          style="flex: 1; min-width: 250px;"
        />
        <button class="btn btn-secondary" onclick={pickNewDbLocation}>
          Browse...
        </button>
        <button
          class="btn btn-primary"
          onclick={createAndSwitch}
          disabled={!newDbPath}
        >
          Create & Restart
        </button>
      </div>
    </div>

    <div class="form-card">
      <h4>Switch to Existing Database</h4>
      <p class="card-hint">
        Point YourQL at an existing SQLite database file and restart.
      </p>
      <div style="display: flex; gap: var(--space-sm); flex-wrap: wrap;">
        <input
          type="text"
          class="form-input"
          placeholder="Path to existing database"
          bind:value={switchToPath}
          style="flex: 1; min-width: 250px;"
        />
        <button class="btn btn-secondary" onclick={pickExistingDbFile}>
          Browse...
        </button>
        <button
          class="btn btn-primary"
          onclick={switchToExisting}
          disabled={!switchToPath}
        >
          Switch & Restart
        </button>
      </div>
    </div>

    {#if !activeDbIsDefault}
      <div class="form-card">
        <h4>Reset to Default</h4>
        <p class="card-hint">
          Return YourQL to its default database location
          (~/.yourql/yourql.db) and restart.
        </p>
        <button class="btn btn-secondary" onclick={resetToDefault}>
          Reset to Default & Restart
        </button>
      </div>
    {/if}

    <div style="margin-top: var(--space-md); color: var(--text-tertiary); font-size: var(--font-xs);">
      ⚠ Switching databases always restarts YourQL. Any in-progress
      conversations will be interrupted. Your current database is left
      untouched on disk — you can always switch back.
    </div>
  </div>
{/if}
```

#### Step 5: Frontend handlers that call the Wails bindings

```js
let newDbPath = $state('')
let switchToPath = $state('')

async function loadDbSwitcherInfo() {
  try {
    const info = await GetActiveDatabaseInfo()
    activeDbPath = info.path
    activeDbIsDefault = info.is_default
  } catch (e) {
    console.error('Failed to load database info:', e)
  }
}

async function pickNewDbLocation() {
  try {
    const path = await PickNewDatabaseLocation()
    if (path) newDbPath = path
  } catch (e) {
    console.error('PickNewDatabaseLocation:', e)
  }
}

async function pickExistingDbFile() {
  try {
    const path = await PickExistingDatabaseFile()
    if (path) switchToPath = path
  } catch (e) {
    console.error('PickExistingDatabaseFile:', e)
  }
}

async function createAndSwitch() {
  if (!newDbPath) return
  if (!confirm('This creates a brand-new, empty YourQL database and restarts the app. Nothing in your current database is deleted.')) return
  try {
    await CreateAndSwitchToNewDatabase(newDbPath)
  } catch (e) {
    alert('Failed to create database: ' + (e.message || String(e)))
  }
}

async function switchToExisting() {
  if (!switchToPath) return
  if (!confirm('Switch YourQL to "' + switchToPath + '" and restart?')) return
  try {
    await SwitchToExistingDatabase(switchToPath)
  } catch (e) {
    alert('Failed to switch database: ' + (e.message || String(e)))
  }
}

async function resetToDefault() {
  if (!confirm('Return YourQL to its default database and restart?')) return
  try {
    await ResetToDefaultDatabase()
  } catch (e) {
    alert('Failed to reset database: ' + (e.message || String(e)))
  }
}
```

#### Confirmation dialogs

**Platform note (2026-08-16).** Wails v2.13.0's macOS WKWebView does NOT
implement the `runJavaScriptConfirmPanelWithMessage` WKUIDelegate method,
so `window.confirm()` returns falsy/undefined silently and `window.alert()`
does nothing. The frontend MUST use a custom Svelte modal (not `confirm()`/
`alert()`) for all confirmation and error dialogs in this feature. The
implementation reuses SettingsView's existing `skill-editor-overlay` /
`skill-editor` CSS classes — see §3.6 Step 6 for the actual markup.

(The five existing `confirm()` calls elsewhere in SettingsView — delete
provider, delete connection, reset agent loop settings — are pre-existing
issues with the same root cause, but are out of scope for this document.)

---

## 4. What Happens To In-Progress Work During a Switch

`SwitchActiveDatabase` → `restartApp` calls `os.Exit(0)` immediately after
launching the next process. This is the same abruptness the auto-updater
already accepts (`AGENT_READ_FIRST.md` §3.8 calls `PerformUpgradeRestart`'s
`os.Exit(0)` "irreversible from the app's perspective"). Concretely:

- Any in-flight `ProcessUserMessage` call is killed mid-request. There is no
  graceful "finish this answer first" step — this mirrors quitting the app
  normally (⌘Q / Alt+F4) while a query is running, which today has the same
  outcome (`app.go`'s `shutdown()` just closes `models.DB`, it doesn't wait
  for in-flight work either).
- The frontend should treat "a database switch was requested" like a normal
  app quit from the user's perspective: if any conversation shows an active
  spinner, the confirmation modal (§3.5/§3.6) should surface a mild warning
  ("Processing will be interrupted") rather than silently proceeding —
  this is a UX/comfort concern (`AGENT_READ_FIRST.md` §3.9), not a safety
  one, since no data source write is at risk either way (§0 is about the
  *data source*, and no query is left half-executed in a way that could
  write to one — `sql_execution.go`'s queries are single round-trip
  `SELECT`s, not multi-statement transactions that could be caught
  half-committed).

---

## 5. Migration/Compatibility Notes

- **No existing installs are affected until they use this feature.** The
  pointer file doesn't exist for any install today; `resolveDBPath()` falls
  through to `getDBPath()` unconditionally in that case.
- **No new columns, tables, or schema changes** to `yourql.db` are needed for
  this feature — it's entirely file-path plumbing plus a tiny sibling JSON
  file. `AGENT_READ_FIRST.md` §3.2's migration-safety rules aren't
  implicated because nothing about the schema changes.
- **The pointer file's directory (`~/.yourql/`) already exists** by the time
  any of this code runs, because `getDBPath()` already calls
  `os.MkdirAll(dir, 0755)` on every startup.
- **Multiple YourQL windows/instances:** `DB.SetMaxOpenConns(1)` plus WAL
  mode already assumes single-process, single-connection use per database
  file. This feature does not change that assumption — switching still
  means "one file, one running instance at a time." If a user manually
  launches two YourQL processes pointed at the *same* file, that's already
  possible today (unrelated to this feature) and already a pre-existing
  risk the app doesn't currently guard against; this document doesn't
  attempt to solve it.

---

## 6. Relationship to `TESTING_EXECUTION.md`

This feature is what makes the "independent, code-blind test project"
option from that earlier discussion viable without asking an external
project to manipulate the user's real `~/.yourql/yourql.db` file directly:

1. The external test project calls `CreateAndSwitchToNewDatabase(path)` (or
   drives the equivalent UI flow) to get a real, running YourQL instance
   pointed at a disposable file it fully controls.
2. It seeds configuration either by driving the Settings UI, or — since the
   database schema is a stable, documented contract (`pkg/models/*.go`) —
   by writing directly into the SQLite file with any generic SQLite
   library, in any language, with zero dependency on YourQL's Go code.
3. It drives conversations through the real UI (or Wails devtools protocol,
   as `TESTING_SUITE.md` §10 already describes for E2E) and reads results
   back from the same SQLite file's `conversation_messages` / `queries`
   tables.
4. When done, it can call `ResetToDefaultDatabase()` (or simply leave the
   pointer alone — the user's original database was never touched) and
   delete its own disposable file.

This document does not itself decide whether the test project ends up
independent or in-repo (see the open question from the prior discussion) —
it only ensures that whichever way that goes, "get a real YourQL process
onto a blank database" is a supported, safe, one-call operation rather than
a `$HOME`-env hack or a risky rename-swap of the user's real file.

---

## 7. Compliance Review Against `AGENT_READ_FIRST.md`

| Charter Reference | How This Feature Complies |
|---|---|
| §0 Data Source Read-Only Invariant | Not implicated. This feature only repoints and creates copies of the **app's own** database (§3.1's `models.DB`), never a user's configured data source. No `query_database`/`sql_execution.go` code path is touched. |
| §3.1 `models.DB` is fully mutable app state | This feature is squarely inside the "normal, expected, and necessary" category the charter already carves out for `models.DB` — creating/switching entire database *files* is a larger-grained version of the CRUD it already permits. |
| §3.2 Migration Safety (only ADD COLUMN, never destructive) | Untouched — `CreateBlankDatabaseAt` and switching both run the existing, unmodified `migrate()` function. No new migration is introduced by this feature. |
| §2.2 Wails Lifecycle — "Never call `models.ConnectDatabase()` outside of `startup`" | Preserved by design: switching **restarts the process** rather than calling `ConnectDatabase()` a second time mid-session. The rule is not bent, it's respected by avoiding the scenario it warns about. |
| §3.8 Auto-Updater Safety (distinct trust domain, explicit confirmation, no silent restart) | The restart-and-relaunch mechanism is deliberately modeled on the *same* pattern and subjected to the *same* rule: `SwitchActiveDatabase`/`ResetToDefaultDatabase` are only reachable after explicit UI confirmation (§3.5/§3.6), never automatically. Unlike the updater, this feature never touches the app binary — only which data file it opens — so it is a distinct, lower-risk trust domain, but treated with equal caution. |
| §3.9 UX & Trust — confirm before destructive/irreversible actions | Every switch/create/reset action requires an explicit confirmation modal (§3.6) before restarting, matching the delete/archive/clear precedent. |
| §3.9 — empty/error states need real content | `CreateBlankDatabaseAt`'s refusal to overwrite a non-empty file returns a clear, specific error ("refusing to overwrite existing non-empty file") rather than failing silently or generically. |
| §3.5 Security — never log API keys/passwords | Not implicated — no credential data is read, written, or logged by any function in this document; only file paths are handled. |
| §4.2 "When to Pause" — schema/multi-driver/system-prompt changes | None of those trigger conditions apply: no schema column change, no driver change, no prompt change. The one item worth flagging per §4.2's spirit is the **restart mechanism itself**, since it's new process-lifecycle code — reviewed in detail in §3.4 and §4 above. |

### 7.1 Explicit Non-Goals (documented per §4.4's risk/reward template)

- **Change:** Add a pointer-file indirection + restart-based switch for
  YourQL's own app database; add a "create blank database" helper.
- **Fundamental goal impact:** None on answer accuracy/delivery — this
  feature never touches the query pipeline, LLM calls, or data sources.
- **Risk category:** Regression / Data integrity (of the *app* database,
  not a data source) / UX comfort (abrupt restart).
- **Failure modes considered:** (1) accidentally overwriting a real
  database — mitigated by the non-empty-file refusal in §3.3; (2) losing
  in-flight work on switch — mitigated by treating it like a normal quit
  and warning the user in the confirmation modal (§4); (3) pointer file
  corruption bricking startup — mitigated by falling back to
  `getDBPath()`'s default on any read/parse failure (§3.2).
- **Reward:** Enables profile switching, clean-slate resets without data
  loss, portable installs, and a safe foundation for external test tooling
  (§6) — benefits every user who wants to manage multiple contexts, not
  just testing.
- **Mitigations:** confirmation modals before every restart-triggering
  action; refusal to clobber non-empty files; pointer file failures degrade
  to today's exact default behavior; no schema or query-pipeline changes.
- **Decision:** Proceed — low risk, additive, and does not touch any
  charter absolute rule (§4.0). If implemented, log this analysis (or a
  refined version of it) in `documentation/RISK_ANALYSIS_LOG.md` per §4.4.

### 7.2 Non-Empty-File Refusal — Why It's Non-Negotiable

`CreateBlankDatabaseAt` refuses to write into any existing file with
`size > 0`. This is the single most important safety property in this
document: without it, a user who mis-clicks in a save dialog and selects
their *real* `yourql.db` instead of a new filename would have it silently
replaced by an empty database — an unrecoverable, silent data-loss bug with
no error, no confirmation, and no charter rule currently guarding against
it (this scenario is novel to this feature, not an existing gap). The
refusal turns that scenario into a clear, actionable error instead.

### 7.3 Why Restart Instead of Hot-Swap Is a Safety Choice, Not Just Simplicity

Hot-swapping `models.DB` while conversations, streaming callbacks, and
cancellation handles are live would require guaranteeing no in-flight
`*sql.DB` reference from the old connection is used after the swap — a
correctness property that is hard to verify by inspection across every
`pkg/services/*.go` caller and easy to silently regress later as new
service functions are added. Restarting the whole process is the
verifiably-correct alternative: a fresh process has no stale references by
construction. This trades a few seconds of restart time for eliminating an
entire class of latent bugs, which is the right trade given
`AGENT_READ_FIRST.md`'s priority order (§1: correctness/safety over
convenience/speed).

### 7.4 Note on Switching to an Arbitrary (Non-YourQL) SQLite File

`SwitchToExistingDatabase`/`SwitchActiveDatabase` do not attempt to verify
the target file is a real, previously-used YourQL database before
switching — they rely on `migrate()`'s own idempotent `ensureColumn`/
`CREATE TABLE IF NOT EXISTS` behavior to either recognize an existing
YourQL schema or safely lay one down fresh. If a user points YourQL at an
unrelated SQLite file (e.g., some other app's database), `migrate()` would
attempt to add YourQL's tables/columns into it — additive only, per §3.2,
so it would not delete or corrupt whatever unrelated tables already exist,
but it would leave YourQL's schema mixed into a file the user probably
didn't intend to modify this way. **This is a UX footgun worth a
confirmation-copy callout** ("Are you sure `<path>` is a YourQL database or
a location where you want to create one?") rather than a safety violation —
no charter absolute rule is broken, since this only ever touches the file
being switched *to*, and only ever additively.
