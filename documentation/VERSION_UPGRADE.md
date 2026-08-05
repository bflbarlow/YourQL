# YourQL — Version Upgrade Guide

> **Version:** 0.3.0  
> **Last updated:** 2026-08-03

This document describes how users upgrade YourQL to a new version. There are two
paths: **manual upgrade** (works today on every OS) and **automatic upgrade**
(planned, requires implementation).

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

## Path 2: Automatic Upgrade (Planned)

The goal is a "Check for Updates" button on the **About** page that detects,
downloads, and installs a new version without leaving the application.

### User Flow

1. User opens **About** page (sidebar → About).
2. Current version is displayed (e.g., "Version 0.3.0").
3. A **"Check for Updates"** button is available.
4. On click:
   - App queries the GitHub Releases API for the latest version tag.
   - Compares it against the running version.
   - **If up to date:** shows "You're on the latest version."
   - **If update available:** shows the new version number, release notes snippet, and a **"Download & Install"** button.
5. On "Download & Install":
   - The new binary is downloaded in the background with a progress indicator.
   - Once downloaded, the app prompts the user to restart.
   - On restart, the new binary launches and picks up `~/.yourql/yourql.db` as usual.

### Technical Design

#### Version Detection

Query the GitHub Releases API:

```
GET https://api.github.com/repos/yourorg/yourql/releases/latest
```

Parse the `tag_name` field (e.g., `v0.4.0`) and compare it against the running
version (stored as `appVersion` in `frontend/src/App.svelte`). Use semver
comparison to determine if the remote version is newer.

#### Binary Download

The release assets include per-OS binaries. The app detects the current OS at
runtime (`runtime.GOOS`) and selects the matching asset:

| OS | Asset pattern |
|---|---|
| macOS | `YourQL-darwin-*.dmg` or `YourQL.app` |
| Windows | `YourQL-windows-*.exe` |
| Linux | `YourQL-linux-*.AppImage` |

The download streams to a temp directory with progress events emitted to the
frontend via `runtime.EventsEmit`.

#### Binary Replacement

Replacing the running binary requires different approaches per OS:

**macOS — Recommended approach:**
- Download the new `.dmg`.
- Mount it programmatically (`hdiutil attach`).
- Copy `YourQL.app` to a temporary location (e.g., `/tmp/YourQL-new.app`).
- Write a small shell script to `/tmp/yourql-upgrade.sh` that:
  1. Waits for the current app to exit.
  2. Replaces `/Applications/YourQL.app` with the new version.
  3. Launches the new version.
- Execute the script in the background and call `os.Exit(0)`.

**Windows — Recommended approach:**
- Download the new `.exe` to a temp location.
- Write a small PowerShell/batch script that:
  1. Waits for the current process to exit.
  2. Moves the new `.exe` over the old one.
  3. Launches the new `.exe`.
- Execute the script and exit.

**Linux — Recommended approach:**
- Download the new `.AppImage` to `~/.local/share/YourQL/`.
- Write a small shell script that:
  1. Waits for the current process to exit.
  2. Replaces the AppImage.
  3. Launches the new version.
- Execute the script and exit.

#### Implementation Plan

| Phase | What | Effort |
|---|---|---|
| 1 | **Backend:** `CheckForUpdate()` Wails binding — hits GitHub Releases API, returns `{latestVersion, currentVersion, updateAvailable, releaseNotes, downloadUrl}` | Small |
| 2 | **Backend:** `DownloadUpdate()` Wails binding — downloads the binary, emits progress events, replaces and relaunches | Medium |
| 3 | **Frontend:** "Check for Updates" button on About page, download progress bar, restart prompt | Small |
| 4 | **Testing:** Per-OS smoke tests for the full download → replace → relaunch cycle | Medium |

#### Go Dependencies (suggested)

- [`github.com/Masterminds/semver/v3`](https://github.com/Masterminds/semver) — semver parsing and comparison
- Standard library `net/http` — GitHub API calls
- Standard library `io` + `os` — file download and binary replacement
- Standard library `os/exec` — running the upgrade script

No heavy external dependencies are needed. The entire feature can be implemented
with the Go standard library plus a semver package.

#### Security Considerations

- **Verify the GitHub API response** — check the `Authorization` header isn't
  needed for public repos; if the repo goes private later, the API call will
  fail gracefully (no crash).
- **Checksum verification** — GitHub Releases include `checksums.txt`. Verify
  the downloaded binary's SHA256 before executing it. This prevents corrupted
  downloads and MITM attacks.
- **HTTPS only** — all API calls and downloads must use HTTPS.
- **Never run the upgrade script as root** — the script runs as the current
  user, replacing only the YourQL binary.
- **Atomic replacement** — write the new binary to a temp location first, verify
  the checksum, then move it into place. Never overwrite the running binary
  in-place.

#### Edge Cases

| Scenario | Behavior |
|---|---|
| No internet connection | "Check for Updates" shows a friendly "Unable to reach update server" message |
| GitHub API rate-limited | Cache the last check timestamp; don't retry for 1 hour |
| Download interrupted | Resume from byte offset if the server supports `Range` headers; otherwise restart |
| Insufficient disk space | Check available space before downloading; show a clear error if insufficient |
| Version downgrade | The semver check only triggers for `remote > current`. Downgrades require manual download |
| Running from source (`wails dev`) | Skip the binary replacement step; show a message: "You're running a development build. Use `git pull` to update." |

---

## Migration Safety

YourQL's database migrations follow strict rules (see `AGENT_READ_FIRST.md` §3.2):

- **Only ADD COLUMN** — never DROP, RENAME, or ALTER existing columns.
- **`runMigration()` tracks every migration** — nothing runs twice.
- **Schema is backward-compatible** — an old binary can still read a database
  that was opened by a newer version (it just ignores new columns).

This means upgrades are safe and reversible. If a new version has a bug, you can
download the previous version and your data will still work.

---

## Current Status

| Feature | Status |
|---|---|
| Manual upgrade (all OSes) | ✅ Works today |
| "Check for Updates" button | ❌ Not yet implemented |
| In-app download & install | ❌ Not yet implemented |
| Auto-update on launch | ❌ Not yet implemented (future enhancement) |

The manual upgrade path is already functional because of the clean separation
between the application binary and `~/.yourql/yourql.db`. The automatic upgrade
path is a planned enhancement with a clear, low-dependency implementation plan.