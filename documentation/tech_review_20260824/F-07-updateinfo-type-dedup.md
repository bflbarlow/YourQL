# F-7 · Consolidate `UpdateInfo` type duplicated between `app.go` and `updater.go`

**Severity:** Low · **Effort:** Small · **Risk categories:** regression, maintainability

## Problem Statement

`pkg/services/updater.go` header:

```go
// ---------------------------------------------------------------------------
// Types (repeated from app.go for the service layer; kept in sync manually)
// ---------------------------------------------------------------------------
type UpdateInfo struct { ... }
```

The identical struct is declared in `app.go` (the Wails-bound surface).
"Kept in sync manually" is a standing invitation for drift — and the charter
explicitly forbids this pattern in §1.6:

> *"When adding a type, define it canonically in `pkg/engine` and alias it
> where needed — don't create two structurally-identical-but-distinct
> types."*

While that rule names `pkg/engine`, the principle applies to any duplicated
struct: if a future field is added to only one copy (e.g., a
`MandatoryUpdate bool` or `DownloadProgress float64`), the Go compiler will
not complain — the Wails-bound copy simply won't carry the data, or worse,
the JSON shape the frontend expects diverges silently between the two.

## Solution Design

Follow the codebase's established alias pattern (used for engine types):

```go
// pkg/services/updater.go — canonical definition stays here,
// next to the logic that produces the value.
type UpdateInfo struct {
    CurrentVersion  string `json:"current_version"`
    LatestVersion   string `json:"latest_version"`
    UpdateAvailable bool   `json:"update_available"`
    ReleaseNotes    string `json:"release_notes"`
    DownloadURL     string `json:"download_url"`
    AssetChecksum   string `json:"asset_checksum"`
    PublishedAt     string `json:"published_at"`
}
```

```go
// app.go
import "YourQL/pkg/services"

// UpdateInfo is re-exported from pkg/services where it is canonically
// defined alongside CheckForUpdate/DownloadUpdate. Do not add fields here.
type UpdateInfo = services.UpdateInfo
```

Because it is a **type alias** (`=`), not a defined type:

- All existing method signatures on `*App` (`CheckForUpdate() *UpdateInfo`,
  etc.) compile unchanged.
- The Wails bindings generator serializes identically (JSON tags preserved;
  aliases are transparent).
- The frontend contract is untouched byte-for-byte.

Delete the "kept in sync manually" comment block; replace with a pointer to
this finding so history explains the layout.

## Implementation Plan

1. Delete the struct from `app.go`; add the alias line near the other
   service imports/usages.
2. `go build ./...` + regenerate/verify frontend bindings still typecheck
   (`wails dev` once; check the About/update UI renders update info).
3. Grep for any remaining structural duplicates of the same nature while in
   there: `grep -rn "current_version" --include='*.go' .` should hit exactly
   one declaration. Fix any siblings found the same way.
4. Add a note to `AGENT_READ_FIRST.md` §1.6's alias guidance? Not needed —
   the existing text already covers it; instead cite this doc from
   RISK_ANALYSIS_LOG.md.

## Risk Assessment

- **Failure mode:** none plausible beyond a typo'd import — compiler-enforced.
- **Behavioral change:** zero. JSON wire format identical.
- Charter §4.3 case 3 (additive refactor, isolated). Proceed; bundle with F-6.
