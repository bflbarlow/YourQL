# YourQL — Release & Deployment Guide

> **Version:** 0.4.7  
> **Last updated:** 2026-08-25

> **Note (v0.4.7):** Per `AGENT_READ_FIRST.md` §3.8, `main.go`'s default
> remains `"dev"` — never hardcode a real version there. The shipped version
> comes exclusively from the ldflags injection of the git tag.

This document is a step-by-step checklist for cutting a release. Follow every
step in order. Do not skip verification steps — a broken release is worse than
a delayed one.

---

## 1. Where the Version Lives

There is exactly one place the version number is stored in source code:

| File | Line | What |
|---|---|---|
| `main.go` | `var appVersion = "0.4.0"` | Default for `wails dev`. Overridden at build time by ldflags. |

There is no version string in the frontend. The About page calls
`GetAppVersion()` on mount, which returns this Go variable.

**At build time**, the build scripts inject the git tag via ldflags:
```
wails build -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"
```
This means the version in the shipped binary comes from the **git tag**, not
from `main.go`. The `main.go` default is only used during `wails dev`.

---

## 2. Pre-Release Checklist

Run through these before you touch any code or tags.

### 2.1 Fundamental Checks (from AGENT_READ_FIRST.md)

- [ ] **Answer accuracy** — Sent a real question against a real database with a
      real LLM provider and got a correct answer.
- [ ] **Read-only invariant** — No code path allows INSERT, UPDATE, DELETE,
      DROP, ALTER, or any write statement against a data source.
- [ ] **No credential leaks** — Grepped for `slog.*api_key`, `slog.*password`,
      `log.*api_key`, `log.*password`, `fmt.*api_key`, `fmt.*password`. Found
      nothing.
- [ ] **Migration safety** — Any schema changes in `models/database.go` are
      ADD COLUMN only, wrapped in `runMigration()`.
- [ ] **Dark mode** — Opened Settings and a conversation in both light and dark
      themes. Nothing is invisible or unreadable.
- [ ] **Existing data survives** — Launched the new binary against an existing
      `~/.yourql/yourql.db`. Conversations, messages, providers, data sources,
      and skills all load correctly.

### 2.2 Build & Packaging

- [ ] `go build ./...` succeeds with zero errors.
- [ ] `cd frontend && npx vite build` succeeds with zero errors.
- [ ] `wails generate module` succeeds (bindings are up to date).
- [ ] `wails build` succeeds on the development machine.
- [ ] Driver registry count verified: `len(driverRegistry) == 10`.

### 2.3 End-to-End Smoke Test

- [ ] Create a new conversation → type a question → get a correct answer.
- [ ] Send a follow-up message → model references prior context correctly.
- [ ] Click column headers in the results table → sorting works.
- [ ] Click "Show all X rows" → all rows appear.
- [ ] Generate a chart (ask "make a bar chart of...") → chart renders.
- [ ] Edit an existing LLM provider → API key is not wiped.
- [ ] Edit an existing data source → password is not wiped.
- [ ] Open an old conversation → messages load and display correctly.
- [ ] Pin, duplicate, rename, archive, and delete a conversation → all work.
- [ ] Cancel an in-progress message → conversation returns to clean state.

---

## 3. The Release (Step by Step)

### Step 1 — Choose the Version

Decide what the new version is. Follow semver:

| Bump | Example | When |
|---|---|---|
| Patch | `0.4.0` → `0.4.1` | Bug fixes, safety improvements |
| Minor | `0.4.0` → `0.5.0` | New features, new drivers, additive schema |
| Major | `0.x.x` → `1.0.0` | First stable public release |

For this document, we'll use `0.4.1` as the example. Replace with your actual
version.

### Step 2 — Update Version in Source

```bash
# Edit main.go: change the default version string
#   var appVersion = "0.4.0"   →   var appVersion = "0.4.1"
```

Also update the version header at the top of this document and in
`README.md`'s About-page description if applicable.

### Step 3 — Update Changelog / Release Notes

Create or update a changelog. At minimum, write the release notes you'll paste
into GitHub Releases. Keep a running `CHANGELOG.md` or write notes now — don't
try to reconstruct them from git commits later.

Template:
```markdown
## What's New in v0.4.1

### 🐛 Fixed
- ...

### ✨ Added
- ...

### 🔧 Changed
- ...
```

### Step 4 — Commit and Tag

```bash
git add main.go
# Add any other files you changed (README, this doc, changelog)
git commit -m "Release v0.4.1"
git tag v0.4.1
```

The tag **must** start with `v` (e.g., `v0.4.1`). The build scripts use
`git describe --tags --abbrev=0` which looks for the most recent `v*` tag.

### Step 5 — Push

```bash
git push origin main
git push origin v0.4.1
```

### Step 6 — Build for macOS

Run on a Mac. This produces the `.app` bundle and packages it as a `.dmg`.

```bash
# Build the universal binary
wails build -platform darwin/universal -clean \
    -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"

# Create the DMG
hdiutil create -volname "YourQL" \
    -srcfolder build/bin/YourQL.app \
    -ov -format UDZO \
    build/bin/YourQL-0.4.1-darwin-universal.dmg
```

**If you have an Apple Developer account (signing + notarization):**

```bash
# Sign the .app
codesign --deep --force --verify --verbose \
    --sign "Developer ID Application: <Your Name> (<Team ID>)" \
    build/bin/YourQL.app

# Sign and notarize the DMG
codesign --force --verify --verbose \
    --sign "Developer ID Application: <Your Name> (<Team ID>)" \
    build/bin/YourQL-0.4.1-darwin-universal.dmg

xcrun notarytool submit build/bin/YourQL-0.4.1-darwin-universal.dmg \
    --apple-id "<apple-id>" \
    --team-id "<Team ID>" \
    --password "<app-specific-password>" \
    --wait

xcrun stapler staple build/bin/YourQL-0.4.1-darwin-universal.dmg
```

**If you don't have an Apple Developer account:** The unsigned DMG will trigger
Gatekeeper. Users can bypass it by right-clicking → Open. Document this in the
release notes.

### Step 7 — Build for Windows

Can be done on macOS (cross-compile) or Windows.

```bash
wails build -platform windows/amd64 -nsis -clean \
    -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"
```

Output: `build/bin/YourQL-amd64-installer.exe`

Optional signing:
```cmd
signtool sign /fd SHA256 /f codesign.pfx /p <password> ^
    /tr http://timestamp.digicert.com ^
    build\bin\YourQL-amd64-installer.exe
```

### Step 7b — Build the MSIX package (Windows only)

Wails v2 does **not** produce MSIX (there is no `-msix` flag). MSIX packaging
and signing use the Windows SDK (`makeappx.exe`, `signtool.exe`) and therefore
must run on Windows. The cross-platform pieces are committed:

- `packaging/msix/AppxManifest.xml` — Desktop Bridge (`runFullTrust`) manifest
- `scripts/build-msix.ps1` — packages the built `YourQL.exe`, generates tile
  icons from `build/appicon.png`, and signs the output

On a Windows machine with the Windows 10/11 SDK installed (and, for public
distribution, a code-signing certificate):

```powershell
# 1. Build the exe (can be copied from the macOS cross-compile, or built here)
wails build -platform windows/amd64 -clean `
    -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"

# 2. Package + sign
cd scripts
.\build-msix.ps1 -Version 0.4.7.0 -Publisher "CN=Your Name" `
    -CertPath C:\certs\yourql.pfx -CertPassword <password>
```

Notes:

- **MSIX must be signed.** Without `-CertPath` the script falls back to a
  self-signed certificate that works for sideloading/dev only — the package
  will not install on other machines until they trust that cert.
- `<Identity Publisher>` and `<Identity Version>` are stamped by the script,
  but must match your certificate's subject (`CN=...`).
- Output: `scripts/YourQL-0.4.7-x64.msix`. Add its SHA256 to `checksums.txt`
  and upload alongside the other artifacts.

### Step 8 — Build for Linux

Uses Docker for a reproducible AppImage. Run from the repo root on any machine
with Docker installed.

```bash
./scripts/build-appimage.sh
```

Output: `build/bin/YourQL-x86_64.AppImage`

The script clones the repo, checks out the latest tag, and builds with the
correct WebKit dependencies. It injects the version via ldflags automatically
(see `scripts/build-linux.sh`).

### Step 9 — Generate Checksums

```bash
cd build/bin
shasum -a 256 \
    YourQL-0.4.1-darwin-universal.dmg \
    YourQL-0.4.1-amd64-installer.exe \
    YourQL-0.4.1-x86_64.AppImage \
    > checksums.txt
cd ../..
```

### Step 10 — Create GitHub Release

1. Go to https://github.com/bflbarlow/YourQL/releases → **Draft a new release**.
2. Choose the `v0.4.1` tag.
3. Title: `YourQL v0.4.1`
4. Paste your release notes.
5. Upload these four files:
   - `build/bin/YourQL-0.4.1-darwin-universal.dmg`
   - `build/bin/YourQL-0.4.1-amd64-installer.exe`
   - `build/bin/YourQL-0.4.1-x86_64.AppImage`
   - `build/bin/checksums.txt`
6. Click **Publish release**.

### Step 11 — Verify the Release

Download each artifact from the GitHub Releases page and test on a clean
machine (or VM):

**macOS:**
- [ ] Download the DMG, mount it, drag `.app` to `/Applications`.
- [ ] Launch the app. If unsigned, verify right-click → Open works.
- [ ] About page shows `Version v0.4.1`.
- [ ] Create a conversation, send a message, get a response.

**Windows:**
- [ ] Download and run the installer.
- [ ] Start Menu shortcut works.
- [ ] About page shows `Version v0.4.1`.
- [ ] Create a conversation, send a message, get a response.

**Linux:**
- [ ] Download the AppImage, `chmod +x`, run it.
- [ ] About page shows `Version v0.4.1`.
- [ ] Create a conversation, send a message, get a response.

---

## 4. If Something Goes Wrong

### Wrong version shows in the About page

The ldflags didn't take effect. Rebuild and verify:
```bash
wails build -ldflags "-X main.appVersion=v0.4.1"
```
Then check the binary:
```bash
strings build/bin/YourQL.app/Contents/MacOS/YourQL | grep "0.4.1"
```

### Critical bug discovered after release

1. Edit the GitHub Release to add a warning at the top of the release notes.
2. Direct users to download the previous version.
3. Cut a patch release (`v0.4.2`) with the fix.
4. `~/.yourql/yourql.db` uses only additive migrations, so the old binary can
   still read the database — it ignores any new columns.

### Forgot to tag before building

The ldflags fall back to the `main.go` default (`0.4.0`). This is wrong — the
binary will show the old version. Rebuild with the correct tag:
```bash
git tag v0.4.1
wails build -ldflags "-X main.appVersion=v0.4.1" ...
```

---

## 5. Quick Reference Card

```bash
# === CUT A RELEASE (example: v0.4.1) ===

# 1. Update version in main.go: var appVersion = "0.4.1"
# 2. Write release notes

# 3. Commit and tag
git add main.go
git commit -m "Release v0.4.1"
git tag v0.4.1
git push origin main --tags

# 4. Build all platforms
# macOS (on a Mac)
wails build -platform darwin/universal -clean \
    -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"
hdiutil create -volname "YourQL" \
    -srcfolder build/bin/YourQL.app \
    -ov -format UDZO \
    build/bin/YourQL-0.4.1-darwin-universal.dmg

# Windows (cross-compile from Mac or on Windows)
wails build -platform windows/amd64 -nsis -clean \
    -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)"

# Linux (any machine with Docker)
./scripts/build-appimage.sh

# 5. Checksums
cd build/bin
shasum -a 256 YourQL-0.4.1-* > checksums.txt
cd ../..

# 6. Create GitHub Release and upload all four files
# 7. Download each artifact and verify on a clean machine
```

---

## 6. CI Automation (Optional)

For teams or frequent releases, the above steps can be automated with GitHub
Actions. A workflow triggered on `v*` tags can build all three platforms,
generate checksums, and draft the release. See the `scripts/build-linux.sh`
pattern — each platform build takes ~10 minutes on GitHub's runners.

If you set up CI, the manual steps become:
1. Update version in `main.go`
2. Write release notes
3. `git commit`, `git tag`, `git push`
4. Wait for CI to finish
5. Download and verify artifacts
6. Publish the draft release