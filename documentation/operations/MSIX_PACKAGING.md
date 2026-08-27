# YourQL — MSIX Packaging Guide

> **Version:** 0.4.7  
> **Last updated:** 2026-08-26

This document records exactly how the Windows **MSIX** package for YourQL is
produced, so it can be repeated for future releases without rediscovering the
pitfalls. The build is fully automated via GitHub Actions on a
`windows-latest` runner.

---

## 1. Why This Exists

Wails v2 (v2.13.0 as of this writing) can produce a Windows `.exe` and an
NSIS installer (`wails build -nsis`), but it has **no MSIX output**. MSIX is a
Microsoft packaging format that requires the Windows SDK tooling:

- `makeappx.exe` — packages the app + manifest into a `.msix`
- `makepri.exe` — (optional) generates resource files
- `signtool.exe` — **mandatory** signing step

These tools are Windows-only, and MSIX **must be signed** to install (there is
no "Run Anyway" path like an unsigned `.exe`). This is why MSIX is built on a
Windows machine / CI runner rather than cross-compiled from macOS like the
exe and DMG.

---

## 2. The Pieces (all committed)

| File | Purpose |
|---|---|
| `.github/workflows/build-msix.yml` | CI workflow: checkout → build exe → package → sign → upload |
| `packaging/msix/AppxManifest.xml` | Desktop Bridge (`runFullTrust`) package manifest |
| `scripts/build-msix.ps1` | Packages `YourQL.exe`, generates tile icons, stamps version/publisher, signs |
| `build/appicon.png` | 1024×1024 source icon (tracked in git) → tile assets |

---

## 3. The Manifest (`packaging/msix/AppxManifest.xml`)

Key facts about the committed manifest:

- **`<Identity Version="0.4.7.0">`** — the 4-part MSIX version. **Must be
  numeric `a.b.c.d`.** The workflow derives this from the git tag
  (`v0.4.7` → `0.4.7.0`).
- **`<Identity Publisher="CN=YourQL">`** — must equal the code-signing
  certificate's subject. Stamped by the script from the `MSIX_PUBLISHER`
  repo variable for real releases.
- **`runFullTrust` capability lives ONLY in the package-level
  `<Capabilities>` block.** It is *not* valid inside `<Application><Extensions>`
  — that was a schema error caught during development (`C00CE014`).
- **`<TargetDeviceFamily MinVersion>` must be `>= 10.0.19041.0`** (Windows 10
  2004 / 20H1). Partner Center **rejects** packages targeting
  `MinVersion <= 10.0.17134.0` at upload. We ship `10.0.19041.0` with
  `MaxVersionTested="10.0.22621.0"`.
- The manifest has **no BOM** and a standard `<?xml version="1.0" ...?>` prolog
  on line 1. `makeappx` rejects any deviation.

---

## 4. The Script (`scripts/build-msix.ps1`)

`build-msix.ps1` does the following, in order:

1. **Locates** `makeappx.exe` and `signtool.exe` under the Windows SDK.
2. **Stages** a clean `msix-stage/` directory.
3. **Copies** `YourQL.exe` (from `build/bin/`) and the manifest.
4. **Generates tile icons** (`StoreLogo.png` 50×50, `Square44x44Logo.png`,
   `Square150x150Logo.png`, `Wide310x150Logo.png`) from `build/appicon.png`
   using `System.Drawing`.
5. **Stamps** the 4-part version and publisher into the manifest using
   **case-sensitive** regex (`-creplace`) targeting only the numeric
   `Version="a.b.c.d"` attribute — a case-insensitive `-replace` on
   `Version="…"` clobbered the XML prolog's `version="1.0"`, which is the
   subtle bug that must not regress.
6. **Writes the manifest as UTF-8 without a BOM** (`UTF8Encoding($false)`).
7. **Packages** with `makeappx pack /d msix-stage /p YourQL-<ver>-x64.msix`.
8. **Signs** with `signtool sign /fd SHA256`.

Parameters:

| Param | Default | Meaning |
|---|---|---|
| `-ExePath` | `build/bin/YourQL.exe` (repo-root resolved) | The Wails-built exe |
| `-Version` | `0.4.7.0` | 4-part MSIX version |
| `-Publisher` | `CN=YourQL` | Cert subject / manifest Identity Publisher |
| `-CertPath` | *(empty)* | `.pfx` for real signing |
| `-CertPassword` | *(empty)* | `.pfx` password |

---

## 5. The CI Workflow (`build-msix.yml`)

Triggers: `push` of a `v*` tag, or manual `workflow_dispatch`.

Job steps (on `windows-latest`):

1. Checkout with `fetch-depth: 0` (so `git describe --tags` resolves).
2. Go 1.25.7 + Node 20 + `wails@v2.13.0`.
3. `npm ci && npm run build` in `frontend/`.
4. `wails generate module`.
5. `wails build -clean` (native Windows build — **no `-platform` flag**; the
   cross-compile flag relocates the exe unpredictably on a Windows host).
   The step then **verifies `build/bin/YourQL.exe` exists**, and if not,
   lists `build/bin` and throws — so failures are self-diagnosing.
6. Decode `MSIX_CERT_BASE64` secret → `.pfx` (if set).
7. Run `build-msix.ps1` with explicit named parameters.
8. Compute SHA256, upload the `.msix` as an artifact, and (on tag push)
   attach it to the GitHub release via `softprops/action-gh-release`.

### Required repo settings

| Name | Type | When needed |
|---|---|---|
| `MSIX_CERT_BASE64` | Secret | Real (public) releases — `.pfx` as base64 |
| `MSIX_CERT_PASSWORD` | Secret | With the cert above |
| `MSIX_PUBLISHER` | Variable | Real releases — `CN=Your Name` matching the cert |

Without the cert secrets, the workflow **still succeeds** but produces a
self-signed MSIX valid only for sideloading/dev.

---

## 6. How to Run a Build

### Automated (recommended)

```bash
# The workflow file must be present on the DEFAULT BRANCH (main) at the
# commit the tag points to — otherwise a tag push will NOT trigger it.
git checkout main
git tag -f -a v0.4.7 -m "YourQL v0.4.7"
git push origin --delete v0.4.7
git push origin v0.4.7
```

Then: GitHub → Actions → *Build MSIX* → watch the run. Output:

- Artifact: `yourql-msix` (in the run summary)
- SHA256: printed by the **Compute SHA256** step
- Release attachment: auto-attached to the `v0.4.7` release if it exists

> ⚠️ **Why re-tag?** GitHub evaluates a tag-push workflow from the workflow
> file *as committed at the tagged commit*. If the tag predates the workflow,
> nothing runs. The tag must point at a commit containing the workflow. If in
> doubt, trigger manually with `workflow_dispatch` (branch `main`).

### Manual (on a Windows machine)

```powershell
wails build -clean -ldflags "-X main.appVersion=v0.4.7"
.\scripts\build-msix.ps1 -Version 0.4.7.0 -Publisher "CN=Your Name" `
    -CertPath C:\certs\yourql.pfx -CertPassword <password>
```

---

## 7. Pitfalls Encountered (do not reintroduce)

1. **`$args` is reserved in PowerShell.** The workflow originally splatted a
   variable named `$args`; it silently broke. Use explicit named parameters
   or a hashtable splat (`@{}`), never a bare array splat for named args.
2. **Array splat binds positionally.** `@('-Version','0.4.7.0')` bound
   `-Version` to `$ExePath`. Use `-Name value` explicitly.
3. **Cross-compile flag on a native host.** `-platform windows/amd64` on the
   Windows runner moved the exe; drop it for native builds.
4. **Relative paths.** Early versions used `$PSScriptRoot\..\..\` which was
   one level too deep. All repo paths now use
   `Split-Path $PSScriptRoot -Parent` (the repo root).
5. **UTF-8 BOM.** `Set-Content` emits a BOM; `makeappx` rejects a BOM before
   the XML prolog. Write with `[System.IO.File]::WriteAllText` +
   `UTF8Encoding($false)`.
6. **Case-insensitive regex clobbered the XML prolog.** Stamping
   `Version="…"` with `-replace` also rewrote `<?xml version="1.0"…?>`.
   Use `-creplace` on a numeric-only pattern.
7. **`runFullTrust` in the wrong element.** It belongs only under the
   package-level `<Capabilities>`, never inside `<Application><Extensions>`.
8. **Empty dirs aren't tracked by git.** (Affected the Linux AppImage, not
   MSIX, but the same class of issue.) Create required directories in the
   build step rather than relying on git-tracked folders.
9. **Store MinVersion too low.** Partner Center rejects MSIX uploads with
   `MinVersion <= 10.0.17134.0`. Use `10.0.19041.0` or higher in
   `<TargetDeviceFamily>`.
10. **`runFullTrust` submission warning.** Partner Center flags `runFullTrust`
    as a restricted capability. This is *expected* and standard for
    Win32/Wails apps; it's auto-approved during certification. In the Partner
    Center submission (App properties / Submission options / Restricted
    capabilities), provide a justification such as:
    > "YourQL is a native Win32 desktop application (built with Go/Wails)
    > that requires full trust execution to perform database connections,
    > standard file I/O, and native desktop functionality."

---

## 8. End-to-End Release Flow (all platforms)

1. Commit work; push to the release branch (`NNN_YYYYMMDD`).
2. Merge into `main`.
3. Write `release-notes/vN.N.N.md`.
4. Tag: `git tag -f -a vN.N.N -m "..."` on `main`, then delete + re-push so
   the tag points at a commit containing `.github/workflows/`.
5. Build macOS DMG + Windows NSIS installer on the Mac.
6. Build Linux AppImage via Docker (`scripts/build-appimage.sh`).
7. The MSIX builds automatically from the tag push; attach the resulting
   `.msix` (or download the artifact) and add its SHA256 to
   `release-notes/vN.N.N.md` and `build/bin/checksums.txt`.
8. Create the GitHub release for the tag, paste release notes, upload the
   DMG, installer exe, AppImage, MSIX, and `checksums.txt`.
9. Verify each artifact on a clean machine (About page shows the version).
