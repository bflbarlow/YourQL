# YourQL — Version Upgrade Guide

> **Version:** 0.5.0
> **Last updated:** 2026-08-07

This document describes how users upgrade YourQL to a new version. There are two
paths: **manual upgrade** (works today on every OS) and **automatic upgrade**
(planned, fully specified below, ready for implementation).

---

## Upgrade Philosophy

YourQL separates the application binary from user data:

| What | Where | Survives upgrade? |
|---|---|---|
| Application binary | Downloaded `.dmg` / `.exe` / `.AppImage` | Replaced |
| User data (conversations, settings, credentials, skills) | `~/.yourql/yourql.db` | **Yes — untouched** |
| Database schema migrations | Run automatically on first launch of new version | Handled by `models/database.go` |

Because all user data lives in `~/.yourql/yourql.db` — completely separate from
the application binary — upgrading is as simple as replacing the binary. The new
version detects the existing database, runs any pending additive migrations, and
picks up right where the old version left off. No import, export, or migration
wizard is needed.

---

## Path 1: Manual Upgrade (Works Today)

### macOS

1. Download the latest `.dmg` from the [GitHub Releases page](https://github.com/yourorg/yourql/releases).
2. Open the `.dmg` and drag `YourQL.app` into `/Applications`, replacing the existing copy.
3. Launch YourQL. It reads `~/.yourql/yourql.db` automatically — all conversations, settings, and credentials are preserved.

### Windows

1. Download the latest `.exe` installer or portable `.exe` from the [GitHub Releases page](https://github.com/yourorg/yourql/releases).
2. Run the installer, or replace the existing portable `.exe` with the new one.
3. Launch YourQL. It reads `%USERPROFILE%\.yourql\yourql.db` automatically.

### Linux

1. Download the latest `.AppImage` from the [GitHub Releases page](https://github.com/yourorg/yourql/releases).
2. Make it executable: `chmod +x YourQL-*.AppImage`
3. Replace your existing AppImage with the new one.
4. Launch it. It reads `~/.yourql/yourql.db` automatically.

**Alternative (from source):**

```bash
git pull
wails build
# Replace the binary in your PATH with build/bin/YourQL
```

---

## Path 0 (Prerequisite): A Single Source of Truth for the Version Number

**This must be implemented before any "Check for Updates" work begins.** It is
currently broken and blocks everything else in this document.

### Current State (Bug)

The running app version is hardcoded in **three separate places**, already out
of sync:

| Location | Value |
|---|---|
| `frontend/src/App.svelte:34` | `const appVersion = '0.5.0'` |
| `app.go` `GetGeneralSettings()` → `GeneralSettings.AppVersion` | `"0.3.0"` |
| This document's header (previous revision) | `0.3.0` |

None of these are derived from a build artifact, a git tag, or each other. An
auto-updater cannot reliably compare "current version" against "latest
release" if "current version" is a string manually typed into a `.svelte`
file. This must be fixed first.

### Fix: Inject Version at Build Time via ldflags

1. Add a Go variable to `main.go`, set only via linker flags, defaulting to a
   dev marker:

   ```go
   // main.go
   var appVersion = "dev" // overridden at build time via -ldflags
   ```

2. Update the build commands (`wails build`, and the equivalents in
   `scripts/build-linux.sh` / `scripts/build-appimage.sh` / the CI/release
   workflow) to inject the real version from the git tag:

   ```bash
   wails build -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"
   ```

   For local `wails dev`, `appVersion` stays `"dev"` — this is also the signal
   the auto-updater uses to skip itself (see Edge Cases below).

3. Expose it to the frontend via a Wails binding instead of a hardcoded
   constant:

   ```go
   // app.go
   func (a *App) GetAppVersion() string {
       return appVersion
   }
   ```

4. Update `GeneralSettings.AppVersion` in `app.go` to read from `appVersion`
   instead of the hardcoded `"0.3.0"` literal.

5. Update `frontend/src/App.svelte` to call `GetAppVersion()` on mount instead
   of hardcoding `const appVersion = '0.5.0'`. Keep a `'dev'` fallback for the
   `wails dev` case so the About page still renders something sensible before
   the binding resolves.

6. **Tag releases consistently.** Git tags must be the single source that
   `git describe` reads and that GitHub Releases uses as `tag_name` (e.g.,
   `v0.5.0`). The release workflow, the ldflags injection, and the GitHub
   Releases API all key off the same tag — this is what makes semver
   comparison in Path 2 possible at all.

Once this is done, "current version" is a single fact — the git tag baked
into the binary at build time — read in exactly one place in Go and exposed
to the frontend through exactly one binding. Everything in Path 2 below
depends on this being true.

---

## Path 2: Automatic Upgrade (Planned — Implementation-Ready)

The goal is a "Check for Updates" button on the **About** page that detects,
downloads, and installs a new version without leaving the application.

### User Flow

1. User opens **About** page (sidebar → About).
2. Current version is displayed (via `GetAppVersion()`, e.g., "Version 0.5.0").
3. A **"Check for Updates"** button is available. The app may also
   auto-check once per launch (throttled — see Edge Cases).
4. On click (manual check — never throttled):
   - App calls `CheckForUpdate()`.
   - App queries the GitHub Releases API for the latest version tag.
   - Compares it against the running version using semver.
   - **If up to date:** shows "You're on the latest version."
   - **If update available:** shows the new version number, release notes
     snippet, and a **"Download & Install"** button.
5. On "Download & Install":
   - The new binary/asset is downloaded in the background with a progress
     indicator (`DownloadUpdate()`).
   - The downloaded asset's checksum is verified against `checksums.txt`
     from the same release **before** anything is executed or installed.
   - Once verified, the app prompts the user to restart ("Restart Now" /
     "Later").
   - On restart, the replacement script runs, the new binary launches, and
     it picks up `~/.yourql/yourql.db` as usual.

### Technical Design

#### Version Detection

Query the GitHub Releases API:

```
GET https://api.github.com/repos/yourorg/yourql/releases/latest
```

Parse the `tag_name` field (e.g., `v0.6.0`) and compare it against the running
version (`appVersion`, from Path 0 above) using semver comparison.

**Wails binding contract:**

```go
type UpdateInfo struct {
    CurrentVersion  string `json:"current_version"`
    LatestVersion   string `json:"latest_version"`
    UpdateAvailable bool   `json:"update_available"`
    ReleaseNotes    string `json:"release_notes"`
    DownloadURL     string `json:"download_url"`
    AssetChecksum   string `json:"asset_checksum"`
    PublishedAt     string `json:"published_at"`
}

func (a *App) CheckForUpdate() (*UpdateInfo, error)
```

- If `appVersion == "dev"`, `CheckForUpdate()` returns immediately with
  `UpdateAvailable: false` and a message indicating this is a dev build (see
  Edge Cases). It does not hit the network.
- Semver comparison only ever treats `remote > current` as "update
  available." Equal or lower versions report "up to date."
- If the GitHub tag is malformed or unparseable as semver, log a warning and
  report "up to date" rather than erroring the UI — never block the About
  page on a parsing failure.

#### Binary Download

The release assets include per-OS binaries. The app detects the current OS at
runtime (`runtime.GOOS`) and selects the matching asset:

| OS | Asset pattern |
|---|---|
| macOS | `YourQL-darwin-universal.dmg` |
| Windows | `YourQL-windows-amd64.exe` |
| Linux | `YourQL-linux-amd64.AppImage` |

Every release **must** also publish a `checksums.txt` (SHA256, one line per
asset, `sha256sum` format) as a release asset. The CI/release workflow is
responsible for generating this — it is not optional, since checksum
verification below is not a "nice to have," it's the gate before any code
is executed.

The download streams to a temp directory (e.g.,
`os.TempDir()/yourql-update/`) with progress events emitted to the frontend
via `runtime.EventsEmit("update:progress", pct)`.

```go
func (a *App) DownloadUpdate(downloadURL, expectedSHA256 string) error
```

`DownloadUpdate` performs, in this exact order:

1. Download the asset to `<tempdir>/yourql-update/<asset-name>.download`
   (temp suffix, not the final name).
2. Compute SHA256 of the downloaded file.
3. Compare against `expectedSHA256` (fetched from `checksums.txt` as part of
   `CheckForUpdate`, passed back in `UpdateInfo.AssetChecksum`).
4. **If checksum mismatch:** delete the temp file immediately, return an
   error ("Downloaded file failed verification — please try again"). Do not
   proceed to step 5 under any circumstances.
5. Only after verification passes: rename the temp file to its final name
   (still in the temp directory) and proceed to the OS-specific replacement
   step below.

This ordering — download → hash → compare → only-then-touch-anything-else —
is the concrete sequencing that was previously missing from this document.
No step past #4 runs on unverified bytes.

#### Binary Replacement

Replacing the running binary requires different approaches per OS, and each
approach must account for code signing so the replaced binary is not flagged
or blocked by the OS on next launch.

**macOS:**

- Download the new `.dmg` (already verified above).
- Mount it programmatically (`hdiutil attach -nobrowse -quiet <path>.dmg`).
- **Code signing / Gatekeeper requirement:** the `.app` inside the official
  `.dmg` release asset must already be signed with a valid Apple Developer
  ID and notarized as part of the release build (this is a release-pipeline
  requirement, not something the updater does at runtime). The updater
  itself must **never** re-sign, modify, or repackage the `.app` bundle —
  it only copies the already-signed, already-notarized bundle from the
  mounted `.dmg` into place. As long as the bundle is copied byte-for-byte
  and its signature isn't touched, Gatekeeper accepts it exactly as if the
  user had manually dragged it in.
- Copy `YourQL.app` from the mounted `.dmg` to a temporary location (e.g.,
  `/tmp/yourql-update/YourQL.app`) using `cp -R`, preserving extended
  attributes and the code signature (`cp -R` preserves both; `ditto` is an
  acceptable, more robust alternative — prefer `ditto --rsrc` for
  correctness with resource forks and xattrs).
- Unmount the `.dmg` (`hdiutil detach`).
- Write a small shell script to `/tmp/yourql-update/upgrade.sh` that:
  1. Waits for the current app process (by PID, passed as an argument) to
     exit.
  2. Removes `/Applications/YourQL.app` and moves the new copy into place
     (`rm -rf` + `mv`, not `cp` over the running path — never modify a
     bundle in place while any part of it could still be mapped).
  3. Launches the new version (`open /Applications/YourQL.app`).
  4. Cleans up the temp directory.
- Execute the script in the background (`exec.Command("/bin/sh",
  "/tmp/yourql-update/upgrade.sh", strconv.Itoa(os.Getpid())).Start()`),
  then call `os.Exit(0)` in the Go process so the script's "wait for exit"
  step resolves quickly.

**Windows:**

- Download the new `.exe` to a temp location (already verified above).
- **Code signing requirement:** the release `.exe` must be Authenticode
  signed with a valid certificate as part of the release build. An
  unsigned `.exe` will trigger SmartScreen warnings on first run regardless
  of how it was delivered — this is a release-pipeline requirement, same as
  macOS notarization, and must be budgeted as part of the release process,
  not the updater itself.
- Write a small batch/PowerShell script to
  `%TEMP%\yourql-update\upgrade.ps1` that:
  1. Waits for the current process (by PID) to exit.
  2. Moves the new `.exe` over the old one (`Move-Item -Force`).
  3. Launches the new `.exe`.
  4. Cleans up the temp directory.
- Execute the script detached (`Start-Process powershell -WindowStyle
  Hidden -ArgumentList ...`) and then exit the current process.
- If YourQL is installed via an installer (NSIS/MSI) rather than portable
  `.exe`, the "Download & Install" button should instead download and
  silently run the installer's updater mode (e.g., `installer.exe /S`) and
  skip manual file replacement entirely — installer-based installs and
  portable installs need two distinct code paths, and the app must know
  which one it is (detectable via presence of an uninstaller registry key
  or an installer-written marker file).

**Linux:**

- Download the new `.AppImage` to `~/.local/share/YourQL/` (already
  verified above).
- AppImages are self-contained and unsigned by convention in this
  ecosystem; checksum verification (already performed) is the primary
  integrity control here — there is no OS-level Gatekeeper/SmartScreen
  equivalent to satisfy.
- Write a small shell script that:
  1. Waits for the current process (by PID) to exit.
  2. Replaces the old AppImage with the new one (`mv`, then
     `chmod +x`).
  3. Launches the new version.
- Execute the script detached and exit.

**Shared principle across all three OSes:** the running process never
overwrites its own binary/bundle in place. It always downloads to a temp
location, verifies, and delegates the actual replacement to a short-lived
external script that waits for the process to fully exit first. This is
what makes the replacement atomic from the user's perspective and avoids
"file in use" errors.

#### Implementation Plan

| Phase | What | Effort |
|---|---|---|
| 0 | **Single source of truth for version** — ldflags injection, `GetAppVersion()` binding, remove hardcoded literals from `App.svelte` and `app.go` (see Path 0) | Small |
| 1 | **Backend:** `CheckForUpdate()` Wails binding — hits GitHub Releases API, parses `checksums.txt`, returns `UpdateInfo` | Small |
| 2 | **Backend:** `DownloadUpdate()` Wails binding — downloads the asset, verifies SHA256, emits progress events | Medium |
| 3 | **Backend:** per-OS replacement scripts + launch (`internal/updater` package: `updater_darwin.go`, `updater_windows.go`, `updater_linux.go`, build-tag separated) | Medium |
| 4 | **Frontend:** "Check for Updates" button on About page, download progress bar, restart prompt | Small |
| 5 | **Release pipeline:** ensure every release publishes `checksums.txt`; ensure macOS build is signed + notarized; ensure Windows build is Authenticode signed | Medium (one-time CI setup) |
| 6 | **Testing:** per-OS smoke tests for the full check → download → verify → replace → relaunch cycle, plus the installer-vs-portable branch on Windows | Medium |

Phase 5 is a hard dependency for Phases 1–4 shipping safely — an auto-updater
that delivers unsigned/unnotarized binaries will actively make the upgrade
experience worse than manual download (OS-level warnings, or outright
blocked launches on macOS with strict Gatekeeper settings). This phase
should be scheduled first or in parallel, not last.

#### Go Dependencies (suggested)

- [`github.com/Masterminds/semver/v3`](https://github.com/Masterminds/semver) — semver parsing and comparison
- Standard library `net/http` — GitHub API calls and asset download
- Standard library `crypto/sha256` — checksum verification
- Standard library `io`, `os`, `os/exec` — file download, temp file handling, script execution
- Standard library `runtime` — OS detection

No heavy external dependencies are needed, and no third-party self-update
library (e.g., `minio/selfupdate`) is required — the per-OS replacement logic
above is small enough (roughly 3 short OS-specific files) that a dependency
would add more surface area than it removes. Revisit this decision only if
the hand-rolled scripts prove fragile in practice across OS versions.

#### Security Considerations

- **HTTPS only** — all API calls and downloads must use HTTPS. This is
  already guaranteed by using the `api.github.com` and
  `github.com/.../releases/download/...` hosts, both HTTPS-only.
- **Checksum verification is mandatory and blocking**, not advisory — see
  the exact sequencing in "Binary Download" above. No downloaded byte is
  executed, mounted, or copied into a launchable location before its SHA256
  matches `checksums.txt`.
- **Code signing and notarization are release-pipeline requirements, not
  runtime updater features.** The updater's job is to faithfully relocate an
  already-signed artifact; it must never attempt to sign, re-sign, or patch
  binaries at runtime. See Phase 5.
- **Public API, no auth required.** The GitHub Releases API is called
  unauthenticated for a public repo. If the repository is ever made
  private, `CheckForUpdate()` must fail gracefully (network/4xx error →
  "Unable to reach update server"), never crash the About page.
- **Never run the upgrade script as root/Administrator.** All scripts run
  as the current user, touching only paths the user already owns
  (`/Applications/YourQL.app` owned by the user who dragged it there,
  `%LOCALAPPDATA%`/`%PROGRAMFILES%` per the install mode, or
  `~/.local/share/YourQL/`). If a Windows install lives in
  `Program Files` and requires elevation, the "Download & Install" flow
  must surface a UAC prompt explicitly (via the installer's own elevation
  request) rather than attempting to silently write to a
  permission-restricted path — silent failure there would leave the user
  stuck on the old version with no explanation.
- **Atomic replacement.** The new binary is fully downloaded and verified in
  a temp location first; the actual swap into the live install path happens
  in one `mv`/`Move-Item` operation performed by the external script after
  the running process has exited — never a partial overwrite of a running
  binary.
- **Update check caching is not a security control but a courtesy** — see
  Edge Cases for the throttling rule and where it's stored.

#### Edge Cases

| Scenario | Behavior |
|---|---|
| No internet connection | `CheckForUpdate()` returns an error; UI shows "Unable to reach update server" |
| GitHub API rate-limited (HTTP 403 with rate-limit headers) | Show "Unable to check for updates right now — try again later." Cache the failure timestamp the same as a successful check, so rapid retries aren't hammered |
| Download interrupted | No resume support in v1 — restart the download from scratch on retry. (Resumable `Range` downloads are a possible future enhancement, not required for launch) |
| Checksum mismatch | Delete the partial/corrupt download, surface "Downloaded file failed verification — please try again," never proceed to replacement |
| Insufficient disk space | Check available space in the temp directory before downloading (`syscall.Statfs`-equivalent per OS, or a simple pre-flight write-test of the expected asset size); show a clear error if insufficient, before starting the download |
| Version downgrade | Semver check only triggers `UpdateAvailable: true` for `remote > current`. Downgrades are never offered through this flow and require manual download per Path 1 |
| Running from source (`wails dev`) | `appVersion == "dev"`; `CheckForUpdate()` short-circuits with a message: "You're running a development build. Use `git pull` to update." No network call is made |
| Windows: installed via installer vs. portable `.exe` | Detected at check time (see Windows replacement notes); installer-based installs re-run the installer, portable installs replace the file directly. The wrong path is a broken update, so this branch must be tested explicitly on both install modes |
| macOS: app not in `/Applications` (user ran it from `~/Downloads`) | Detect the running app's actual bundle path (`os.Executable()` walked up to the `.app` root) and replace **that** path, not a hardcoded `/Applications/YourQL.app` |
| Auto-check throttling vs. manual click | Auto-check on launch is throttled to once per hour, using a `last_update_check` timestamp stored via `SetAppSetting`/`GetAppSetting` in `app_settings` (the existing key-value settings table — no schema change needed). **Manual "Check for Updates" clicks always hit the network immediately and are never throttled** — a user who clicks the button expects a fresh answer |
| Update available but user dismisses / clicks "Later" | No nagging — the next signal is either the next auto-check window (throttled as above) or the user opening the About page again and clicking manually |
| Restart prompt declined | The verified, ready-to-install update stays in the temp directory. Track its state (`UpdateInfo` cached in memory for the session) so re-opening the About page can offer "Restart to finish installing" instead of re-downloading |

---

## Migration Safety

YourQL's database migrations follow strict rules (see `AGENT_READ_FIRST.md` §3.2):

- **Only ADD COLUMN** — never DROP, RENAME, or ALTER existing columns.
- **`runMigration()` tracks every migration** — nothing runs twice.
- **Schema is backward-compatible** — an old binary can still read a database
  that was opened by a newer version (it just ignores new columns).

This means upgrades are safe in the common case, and the manual-download
downgrade path in Path 1 works for schema reasons: an older binary reading a
newer, additively-migrated database will simply not see the new columns it
doesn't know about.

**Caveat:** "safe and reversible" is a statement about schema shape, not
about data content. If a newer version's `runMigration()` step *transforms*
existing data (e.g., backfilling a new column from an old one, or
normalizing a value) rather than purely adding a new empty column, that
transformation is not automatically undone by downgrading the binary. Anyone
adding a `runMigration()` step that writes to existing rows (not just to a
newly-added column) should note in the migration's comment whether the
change is downgrade-safe, and this document should not be read as an
unconditional guarantee that any migration path is losslessly reversible —
only that additive `ensureColumn()` changes are.

---

## Current Status

| Feature | Status |
|---|---|
| Manual upgrade (all OSes) | ✅ Works today |
| Single source of truth for version (Path 0) | ❌ Not yet implemented — currently three hardcoded, drifting values |
| "Check for Updates" button (`CheckForUpdate`) | ❌ Not yet implemented |
| In-app download & verify (`DownloadUpdate`) | ❌ Not yet implemented |
| Per-OS binary replacement | ❌ Not yet implemented |
| Release pipeline: `checksums.txt`, macOS signing/notarization, Windows Authenticode signing | ❌ Not yet implemented — hard prerequisite for the above |
| Auto-update on launch (throttled check, not silent install) | ❌ Not yet implemented (future enhancement, distinct from manual "Download & Install") |

The manual upgrade path is already functional because of the clean separation
between the application binary and `~/.yourql/yourql.db`. The automatic
upgrade path is fully specified above (Path 0 through Path 2) and is
implementation-ready — Phase 0 (version source of truth) and Phase 5 (release
signing pipeline) should be scheduled first, since Phases 1–4 depend on them
to deliver a trustworthy result rather than just a functional one.
