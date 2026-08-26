# F-11 · Migrate credentials to OS keychain storage (opt-in, additive)

**Severity:** Medium · **Effort:** Large · **Risk categories:** credential protection, user trust

## Problem Statement

Charter §3.5 accepts plaintext-at-rest credentials: DB passwords and LLM API
keys live in rows of `~/.yourql/yourql.db`. Threats this exposes:

- Any process running as the user (malware, curious scripts, backup agents)
  reads the SQLite file directly — no app involvement.
- Cloud-synced home directories (iCloud Desktop/Dropbox users who symlink or
  relocate dotfiles) replicate secrets to third-party clouds unencrypted.
- Accidental disclosure: support/debug flows that zip `~/.yourql`, screen
  shares of `sqlite3` sessions, etc.

Keychain-backed storage raises the bar substantially: extraction requires
the same user session *plus* keychain ACL approval, and macOS/Windows sync
keychain items nowhere.

## Solution Design

Indirection model — strictly additive, charter §3.2-compliant:

### Storage format

Keep existing columns (`Password *string`, `APIKey *string`). Introduce a
distinguishable marker prefix:

```
keyring:yourql/<sourceType>/<recordID>/<field>
```

Resolution order wherever a credential is consumed (`BuildDSN`, LLM client
construction):

```go
func resolveSecret(ref string) (string, error) {
    if !strings.HasPrefix(ref, "keyring:") { return ref, nil } // legacy plaintext
    return keyring.Get("YourQL", strings.TrimPrefix(ref, "keyring:"))
}
```

Library: `github.com/zalando/go-keyring` (macOS Keychain via CGO-free
Security framework bindings, Windows Credential Manager, Linux libsecret;
pure-Go fallback errors on unsupported platforms).

### Migration UX (comfort goal, §0 secondary goals)

Settings page gains a single action per provider/data-source row:
*"Store password in system keychain."* On activation:

1. read plaintext value, write to keyring, rewrite column to marker,
2. offer *"Remove stored credentials for all entries"* batch action,
3. show status badges (🔒 keychain / ⚠️ plaintext) so users can see state.

No silent auto-migration: moving secrets between trust domains deserves
explicit consent, and failure handling (locked keychain on Linux without
libsecret) must be visible, not magical (§3.9 transparency).

### Failure handling

`resolveSecret` errors (keychain locked, item deleted) surface as the
existing friendly connection-error path with a distinct message:
"Credential could not be read from the system keychain — reopen it in
Settings." Never fall back silently to empty-string credentials (would
produce confusing auth failures downstream).

### New-entry flow

New providers/data sources get a checkbox "Store securely (recommended)",
default ON where keyring is available (`keyring.Available()` probe at
startup, cached). Where unavailable, hide the toggle entirely.

## Implementation Plan

1. Add dependency; wrap in `pkg/services/secrets.go` with the
   `resolveSecret` / `storeSecret` / `deleteSecret` trio + interface seam for
   tests (in-memory map mock).
2. Thread resolution through: `BuildDSN` implementations (all drivers),
   `NewLLMClient` paths, `llm_tester.go`, export paths that embed creds (audit
   `grep -rn "\.Password\|\.APIKey" pkg/ --include='*.go'`).
3. Settings UI: badge column + per-row store/unstore actions + batch action;
   both themes verified (§3.6).
4. Docs: README security section, AGENT_READ_FIRST §3.5 amendment describing
   the hybrid scheme (same-commit rule).
5. Tests: round-trip store/resolve/delete with mocked keyring; marker
   parsing edge cases (a literal password that begins with `keyring:` —
   escape rule: exact-prefix `keyring:` followed by valid record path shape;
   legacy values that collide get re-stored as plaintext with documented
   limitation, or migrate them too during their next edit).

## Risk Assessment

- **Failure mode:** keyring unavailability bricks connections → resolution
  errors are actionable and reversible (un-store restores plaintext).
- **Lock-in:** exported configs (`total_export.go`) must exclude markers or
  resolve-then-redact; audit exports explicitly in step 2.
- **Effort risk:** largest item in the roadmap; ship per-trust-domain
  (providers first, then data sources) in separate releases.
- Charter: §4.3 case 3 additive; improves §4.1 "data safety" without touching
  answer path. Requires RISK_ANALYSIS_LOG entry (multi-file, schema-visible).
