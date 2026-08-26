# F-6 · Remove hardcoded legacy `"0.4.0"` version check from the updater

**Severity:** Low-Medium · **Effort:** Trivial · **Risk categories:** user trust, reliability, doc drift

## Problem Statement

`pkg/services/updater.go`, `CheckForUpdate`:

```go
// CheckForUpdate queries the GitHub Releases API ... If the app was built
// without a release tag (appVersion is "0.4.0"), it returns immediately —
// dev builds should not self-update. ...
func CheckForUpdate(appVersion string) (*UpdateInfo, error) {
    info := &UpdateInfo{CurrentVersion: appVersion}

    if appVersion == "dev" || appVersion == "0.4.0" {
        return info, nil
    }
```

Three distinct problems, all verified in source:

1. **It contradicts the charter.** `AGENT_READ_FIRST.md` §3.8 states:
   *"`appVersion` … must default to `"dev"` and only ever be set via
   `-ldflags` … `CheckForUpdate` treats `"dev"` as a signal to skip itself
   entirely."* `main.go` complies (`var appVersion = "dev"`). The `"0.4.0"`
   clause is a fossil from pre-convention builds.
2. **It is a latent functional bug.** Any production binary legitimately
   reporting version `0.4.0` — e.g., a release built with a broken ldflags
   invocation, or a distro-packaged build that stamps `0.4.0` — would
   silently *never receive updates*. Self-update silence is indistinguishable
   from "up to date," the worst kind of failure for an updater (§3.8's core
   concern is precisely that users not lose update capability).
3. **The comment lies.** The doc-comment says dev builds report `"0.4.0"`.
   Future agents trusting the comment over the code (or vice versa) waste
   time; stale comments adjacent to security-adjacent code are exactly the
   drift the charter warns about ("A stale charter is worse than no
   charter").

## Solution Design

```go
// CheckForUpdate queries the GitHub Releases API and compares the latest tag
// against the running version. Development builds (appVersion == "dev",
// set as the default in main.go and overridden only via -ldflags at
// release-build time) skip the network call entirely so they can never
// self-update. See AGENT_READ_FIRST.md §3.8.
func CheckForUpdate(appVersion string) (*UpdateInfo, error) {
    info := &UpdateInfo{CurrentVersion: appVersion}

    if appVersion == "dev" {
        return info, nil
    }
    ...
```

Add a regression test pinning the contract (fits F-4's suite):

```go
func TestCheckForUpdateDevBuildSkipsNetwork(t *testing.T) {
    // Must not perform HTTP: point at an unreachable transport.
    // (If fetchLatestRelease is refactored to accept an *http.Client — see
    // plan step 3 — inject a client whose Transport returns an error, then
    // assert err == nil and UpdateAvailable == false for "dev".)
}
```

Plus a negative test: a parseable fake version *does* attempt the network
call (proving removal of `"0.4.0"` restored normal behavior).

## Implementation Plan

1. Delete `|| appVersion == "0.4.0"`; rewrite the doc comment.
2. Grep for other occurrences of the literal: `grep -rn '"0\.4\.0"' --include='*.go' .`
   and in docs; correct each (expected hits: updater.go only).
3. Optional hardening while touching the file: extract
   `var githubBaseURL = "https://api.github.com"` /
   `httpClient = &http.Client{Timeout: 15s}` as package vars so tests can
   inject fakes. Small, additive, enables the tests above properly.
4. Land with F-7 (same file) in one housekeeping PR; append a line to
   `documentation/RISK_ANALYSIS_LOG.md` noting trivial risk.

## Risk Assessment

- **Failure mode:** some environment genuinely relies on reporting `0.4.0`
  to suppress updates. Search suggests no such build path exists (ldflags
  injects real tags per `RELEASE_DEPLOYMENT.md`); risk negligible.
- **Worst case:** a `0.4.0`-stamped install starts receiving update offers —
  the desired behavior for any released version.
- Charter §4.3 case 3: additive/corrective, isolated, testable. Proceed.
