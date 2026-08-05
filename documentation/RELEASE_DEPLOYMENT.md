# YourQL — Release & Deployment Guide

> **Version:** 0.3.0  
> **Last updated:** 2026-08-03

This document prescribes the release process for YourQL. Following these steps
ensures every release is consistent, tested, and correctly packaged for all
supported operating systems.

---

## 1. Versioning

YourQL follows [Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`.

| Bump | When |
|---|---|
| **Patch** (`0.3.0` → `0.3.1`) | Bug fixes, safety improvements, dependency updates |
| **Minor** (`0.3.0` → `0.4.0`) | New features, new driver/LLM support, additive schema changes |
| **Major** (`0.x.x` → `1.0.0`) | Stable public release with no further breaking changes planned |

### Where the version lives

| Location | Purpose |
|---|---|
| `frontend/src/App.svelte` — `const appVersion = '0.3.0'` | Displayed in the About page; used by the planned auto-updater |
| `git tag v0.3.0` | GitHub Releases use this tag; must match `appVersion` with a `v` prefix |

**Both must be updated in the same commit.** A mismatch between the tag and
`appVersion` will cause the auto-updater to misreport the current version.

---

## 2. Pre-Release Checklist

Before cutting a release, verify the following:

### 2.1 Fundamental Goal Checks (from `AGENT_READ_FIRST.md`)

- [ ] **Answer accuracy** — tested with at least one real database + one LLM provider
- [ ] **Read-only invariant** — no code path allows writes to a data source (`SELECT` only)
- [ ] **API keys / passwords** — no new `slog` or `log` calls that could leak credentials
- [ ] **Migration safety** — any schema changes are ADD COLUMN only, recorded via `runMigration()`
- [ ] **Dark mode** — all new UI elements verified in both light and dark themes
- [ ] **Data safety** — `~/.yourql/yourql.db` schema changes preserve existing user data

### 2.2 Build & Packaging

- [ ] `wails build` succeeds with no warnings on the primary development machine
- [ ] Frontend builds clean: `cd frontend && npm run build` (no errors)
- [ ] Driver registry count verified: `len(driverRegistry) == 10`
- [ ] Wails bindings (`frontend/wailsjs/`) are up to date (`wails generate module`)

### 2.3 Regression

- [ ] Existing conversations open and display messages correctly
- [ ] Creating a new conversation, sending a message, and receiving a response works end-to-end
- [ ] LLM provider settings can be edited without wiping the API key
- [ ] Data source settings can be edited without wiping the password

---

## 3. Build Process

All builds are triggered from the repository root. The working directory must be
clean (no uncommitted changes). The build process follows this sequence:

### 3.1 Pre-Build Steps (all platforms)

```bash
# 1. Ensure the frontend is built
cd frontend
npm install
npm run build
cd ..

# 2. Update version in App.svelte (example: bumping to 0.3.1)
# Edit frontend/src/App.svelte: change const appVersion = '0.3.0' to '0.3.1'

# 3. Regenerate Wails bindings (if any Go structs changed)
wails generate module

# 4. Commit the version bump
git add frontend/src/App.svelte frontend/wailsjs/
git commit -m "Bump version to 0.3.1"
git tag v0.3.1
```

### 3.2 macOS Build

**Prerequisites:** macOS with Xcode Command Line Tools installed.

```bash
# Build universal binary (Intel + Apple Silicon)
wails build -platform darwin/universal -clean

# Output: build/bin/YourQL.app (universal .app bundle)
```

**Packaging as DMG:**

```bash
# Create a DMG for distribution
hdiutil create -volname "YourQL" \
    -srcfolder build/bin/YourQL.app \
    -ov -format UDZO \
    build/bin/YourQL-0.3.1-darwin-universal.dmg
```

**Signing & Notarization (required for distribution outside the App Store):**

```bash
# Sign the .app bundle
codesign --deep --force --verify --verbose \
    --sign "Developer ID Application: <Your Name> (<Team ID>)" \
    build/bin/YourQL.app

# Sign the DMG
codesign --force --verify --verbose \
    --sign "Developer ID Application: <Your Name> (<Team ID>)" \
    build/bin/YourQL-0.3.1-darwin-universal.dmg

# Notarize the DMG
xcrun notarytool submit build/bin/YourQL-0.3.1-darwin-universal.dmg \
    --apple-id "<your-apple-id>" \
    --team-id "<Team ID>" \
    --password "<app-specific-password>" \
    --wait

# Staple the notarization ticket to the DMG
xcrun stapler staple build/bin/YourQL-0.3.1-darwin-universal.dmg
```

> **Note:** If you don't have an Apple Developer account, unsigned builds will
> trigger Gatekeeper warnings. Users can still open the app by right-clicking →
> Open. Signing and notarization are recommended for public releases.

### 3.3 Windows Build

**Prerequisites:** Windows 10+ or cross-compilation from macOS/Linux with
`wails build -platform windows/amd64`. For NSIS installers, `makensis` must be
installed.

```bash
# Build Windows binary + NSIS installer
wails build -platform windows/amd64 -nsis -clean

# Output:
#   build/bin/YourQL.exe                    (standalone binary)
#   build/bin/YourQL-amd64-installer.exe    (NSIS installer)
```

**Signing (optional):**

```cmd
signtool sign /fd SHA256 /f codesign.pfx /p <password> /tr http://timestamp.digicert.com build\bin\YourQL-amd64-installer.exe
```

> **Note:** The NSIS installer (`project.nsi`) registers file associations,
> creates Start Menu shortcuts, and installs the WebView2 runtime if needed.
> The installer places the app in `%LOCALAPPDATA%\Programs\YourQL` (per-user)
> or `%PROGRAMFILES%\YourQL` (system-wide).

### 3.4 Linux Build

**Prerequisites:** Docker (for reproducible AppImage builds).

Two options: Docker-based (recommended for consistency) or native.

#### Option A: Docker-based (recommended)

```bash
# Build the AppImage via Docker (reproducible, no local WebKit deps)
./scripts/build-appimage.sh

# Output: build/bin/YourQL-x86_64.AppImage
```

The Dockerfile (`Dockerfile.appimage`) uses Ubuntu 24.04, installs
WebKit2GTK 4.1, builds the Go binary with CGO, and packages it as an AppImage
using `linuxdeploy` + `linuxdeploy-plugin-gtk`.

#### Option B: Native build

```bash
# Requires: libgtk-3-dev, libwebkit2gtk-4.1-dev (Ubuntu 24.04+) or
#           libwebkit2gtk-4.0-dev (Ubuntu 22.04/20.04)
./scripts/build-linux.sh

# Output: YourQL-*.AppImage in the current directory
```

The script auto-detects the Ubuntu version and selects the correct WebKit
package. For non-Ubuntu distributions, install the equivalent packages manually
and run:
```bash
wails build -platform linux/amd64 -clean
```

**Post-build for AppImage:**

```bash
chmod +x YourQL-0.3.1-x86_64.AppImage
```

### 3.5 Build Summary

| OS | Command | Artifact |
|---|---|---|
| macOS | `wails build -platform darwin/universal -clean` | `YourQL.app` → packaged as `.dmg` |
| Windows | `wails build -platform windows/amd64 -nsis -clean` | `YourQL-amd64-installer.exe` |
| Linux | `./scripts/build-appimage.sh` | `YourQL-x86_64.AppImage` |

---

## 4. Release Artifacts

Each GitHub Release should include the following files:

| File | OS | Description |
|---|---|---|
| `YourQL-0.3.1-darwin-universal.dmg` | macOS | Universal DMG (Intel + Apple Silicon) |
| `YourQL-0.3.1-amd64-installer.exe` | Windows | NSIS installer (64-bit) |
| `YourQL-0.3.1-x86_64.AppImage` | Linux | AppImage (64-bit) |
| `checksums.txt` | All | SHA256 checksums of all artifacts |

Generate checksums:

```bash
cd build/bin
shasum -a 256 YourQL-*.dmg YourQL-*.exe YourQL-*.AppImage > checksums.txt
```

---

## 5. GitHub Release Process

### 5.1 Create the Release

1. Push the version bump commit and tag to GitHub:
   ```bash
   git push origin main
   git push origin v0.3.1
   ```

2. Go to [GitHub Releases](https://github.com/yourorg/yourql/releases) →
   **Draft a new release**.

3. Choose the `v0.3.1` tag.

4. Title: `YourQL v0.3.1`

5. Upload all artifacts from `build/bin/`:
   - `YourQL-0.3.1-darwin-universal.dmg`
   - `YourQL-0.3.1-amd64-installer.exe`
   - `YourQL-0.3.1-x86_64.AppImage`
   - `checksums.txt`

6. Write release notes (see §6).

7. Click **Publish release**.

### 5.2 Release Notes Template

```markdown
## What's New in v0.3.1

### 🐛 Fixed
- Fixed API key being wiped when editing LLM provider settings
- Fixed "Thinking..." message appearing on all conversations
- Discussion list now sorts by most recently updated

### ✨ Added
- Archived conversations now show a visual indicator (accent-colored left border)
- Password field now shows "(leave blank to keep current)" hint on edit

### 🔧 Changed
- Conversation list refreshes automatically after sending a message

---

## Installation

### macOS
Download the `.dmg`, open it, and drag `YourQL.app` into `/Applications`.
Replace any existing copy.

### Windows
Download and run the installer. It will replace any existing installation.

### Linux
Download the `.AppImage`, make it executable (`chmod +x`), and run it.
Replace any existing AppImage.

## Upgrading
Replace your current binary with the new one. All your conversations, settings,
and credentials are stored in `~/.yourql/yourql.db` and will be preserved
automatically. See `documentation/VERSION_UPGRADE.md` for details.
```

---

## 6. Post-Release Verification

After publishing, verify the release on each platform:

### 6.1 macOS
- [ ] Download the DMG from GitHub Releases
- [ ] Mount, drag `YourQL.app` to `/Applications`
- [ ] Launch — verify no Gatekeeper warning (or right-click → Open works)
- [ ] Check that existing conversations and settings load from `~/.yourql/yourql.db`
- [ ] Send a test message and verify response

### 6.2 Windows
- [ ] Download and run the installer from GitHub Releases
- [ ] Verify Start Menu shortcut and desktop shortcut work
- [ ] Launch — check that existing data loads from `%USERPROFILE%\.yourql\yourql.db`
- [ ] Send a test message and verify response
- [ ] Uninstall via Add/Remove Programs — verify clean removal (data remains)

### 6.3 Linux
- [ ] Download the AppImage from GitHub Releases
- [ ] `chmod +x` and run
- [ ] Check that existing data loads from `~/.yourql/yourql.db`
- [ ] Send a test message and verify response
- [ ] Verify that `--appimage-extract` works for debugging

---

## 7. Continuous Integration (Recommended)

For teams or frequent releases, automate the build pipeline with GitHub Actions.

### 7.1 Workflow Overview

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build-macos:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: cd frontend && npm ci && npm run build
      - run: go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
      - run: wails build -platform darwin/universal -clean
      - run: |
          hdiutil create -volname "YourQL" \
            -srcfolder build/bin/YourQL.app \
            -ov -format UDZO \
            build/bin/YourQL-${{ github.ref_name }}-darwin-universal.dmg
      - uses: actions/upload-artifact@v4
        with:
          name: macos-dmg
          path: build/bin/*.dmg

  build-windows:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: cd frontend && npm ci && npm run build
      - run: go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
      - run: wails build -platform windows/amd64 -nsis -clean
      - uses: actions/upload-artifact@v4
        with:
          name: windows-installer
          path: build/bin/*-installer.exe

  build-linux:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - run: ./scripts/build-appimage.sh
      - uses: actions/upload-artifact@v4
        with:
          name: linux-appimage
          path: build/bin/*.AppImage

  release:
    needs: [build-macos, build-windows, build-linux]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
      - run: |
          shasum -a 256 */* > checksums.txt
      - uses: softprops/action-gh-release@v1
        with:
          files: |
            macos-dmg/*
            windows-installer/*
            linux-appimage/*
            checksums.txt
```

### 7.2 CI Notes

- **macOS runner** can produce universal binaries but **cannot notarize** without
  Apple Developer credentials stored as GitHub Secrets.
- **Windows runner** needs `makensis` (pre-installed on `windows-latest`).
- **Linux runner** should use `ubuntu-24.04` for `libwebkit2gtk-4.1-dev`. For
  older Ubuntu, switch to `ubuntu-22.04` and `libwebkit2gtk-4.0-dev`.
- **Signing** requires platform-specific secrets:
  - macOS: `APPLE_DEVELOPER_CERTIFICATE`, `APPLE_NOTARY_USER`, `APPLE_NOTARY_PASSWORD`
  - Windows: `WINDOWS_CODESIGN_PFX`, `WINDOWS_CODESIGN_PASSWORD`

---

## 8. Rollback Procedure

If a release introduces a critical bug:

1. **GitHub:** Edit the release to mark it as a pre-release or add a warning at
   the top of the release notes.
2. **Users:** Direct affected users to download the previous version from the
   Releases page. Since `~/.yourql/yourql.db` uses only additive migrations, the
   previous binary can still read the database (it will ignore any new columns).
3. **Fix:** Cut a patch release (`v0.3.2`) with the fix and publish as normal.

---

## 9. Environment Variables

These can be set in `.env` (not committed) or as CI secrets:

| Variable | Used by | Purpose |
|---|---|---|
| `APPLE_DEVELOPER_CERTIFICATE` | CI (macOS) | Base64-encoded signing certificate |
| `APPLE_NOTARY_USER` | CI (macOS) | Apple ID for notarization |
| `APPLE_NOTARY_PASSWORD` | CI (macOS) | App-specific password for notarization |
| `WINDOWS_CODESIGN_PFX` | CI (Windows) | Base64-encoded PFX for Authenticode signing |
| `WINDOWS_CODESIGN_PASSWORD` | CI (Windows) | PFX password |
| `GH_TOKEN` | CI (all) | GitHub token for creating releases |

---

## 10. Quick Reference

### Single-command release (manual)

```bash
# 1. Bump version
#    Edit frontend/src/App.svelte: change appVersion
#    Edit this file: update the version in the header

# 2. Build all platforms
# macOS (on a Mac)
wails build -platform darwin/universal -clean

# Windows (on a Mac, cross-compile)
wails build -platform windows/amd64 -nsis -clean

# Linux (on a Mac, via Docker)
./scripts/build-appimage.sh

# 3. Package macOS as DMG
hdiutil create -volname "YourQL" \
    -srcfolder build/bin/YourQL.app \
    -ov -format UDZO \
    build/bin/YourQL-0.3.1-darwin-universal.dmg

# 4. Generate checksums
cd build/bin
shasum -a 256 YourQL-*.dmg YourQL-*.exe YourQL-*.AppImage > checksums.txt

# 5. Commit, tag, push
git add -A
git commit -m "Release v0.3.1"
git tag v0.3.1
git push origin main --tags

# 6. Create GitHub Release manually or via gh CLI
gh release create v0.3.1 \
    --title "YourQL v0.3.1" \
    --notes-file /tmp/release-notes.md \
    build/bin/YourQL-*.dmg \
    build/bin/YourQL-*.exe \
    build/bin/YourQL-*.AppImage \
    build/bin/checksums.txt
```
