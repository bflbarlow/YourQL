# F-10 · Bearer-token authentication for the headless HTTP API

**Severity:** Medium-High (when headless mode runs) · **Effort:** Small · **Risk categories:** data safety, credential protection, user trust

## Problem Statement

`headless.go` starts an HTTP server:

```go
srv := &http.Server{
    Addr:    fmt.Sprintf("127.0.0.1:%d", port),
    Handler: mux,
}
```

Loopback binding correctly prevents remote access. But **every local
process** — and several classes of browser-driven attack — can reach an
unauthenticated localhost API:

- **DNS rebinding**: a malicious page resolves a hostname to `127.0.0.1`,
  then `fetch("http://evil.example:PORT/api/conversations")` succeeds
  because Origin checks don't exist and Host headers aren't validated.
  Modern browsers' private-network-access protections mitigate partially but
  inconsistently across engines.
- **CSRF-style POSTs**: any page can attempt cross-origin POSTs; without
  CORS headers the *response* is hidden, but the *side effect*
  (creating conversations, sending messages → triggering paid LLM calls and
  queries against production databases) still executes. Simple requests
  (`POST` with form/text content types) avoid preflight.
- **Malicious local processes**: lower concern (same-user compromise is
  game over), but multi-user macOS machines share nothing by default —
  loopback binds are per-host, fine — yet scripts/cron jobs reading
  conversation data becomes trivially scriptable.

What's exposed: full conversation content (i.e., query results over the
user's business data), message sending (LLM spend, DB reads), settings.
Settings endpoints deliberately exclude provider/data-source configs — good
— but everything else is open.

## Solution Design

Shared-secret bearer auth, generated per server start.

### Token lifecycle

```
runHeadless(port, dbPath):
    token := generateToken()          // crypto/rand, 32 bytes, base64url
    printOnce(token, port)            // stdout, single structured line
    persistTokenFile(token)           // ~/.yourql/headless_token (0600) for scripts
```

```go
func generateToken() string {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil { /* fatal: cannot secure */ }
    return base64.RawURLEncoding.EncodeToString(b)
}
```

### Middleware

Wrap the mux:

```go
func authMiddleware(next http.Handler, token string) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Timing-safe compare
        got := r.Header.Get("Authorization")
        ok := len(got) > 8 && strings.HasPrefix(got, "Bearer ") &&
              subtle.ConstantTimeCompare([]byte(got[7:]), []byte(token)) == 1
        // 2. Hardening against DNS-rebinding/CSRF regardless of token:
        if h := r.Header.Get("Origin"); h != "" && h != "null" {
            writeError(w, http.StatusForbidden, "origin_forbidden",
                "headless API does not accept browser origins")
            return
        }
        if !ok {
            w.Header().Set("WWW-Authenticate", `Bearer realm="yourql-headless"`)
            writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
            return
        }
        next.ServeHTTP(w, r)
    })
}
mux := authMiddleware(mux, token)
```

Notes:

- Rejecting any non-null `Origin` header kills rebinding pages even if the
  token leaks into a browser context; legitimate CLI clients (curl, scripts)
  send no Origin.
- `subtle.ConstantTimeCompare` avoids timing oracles.
- Health endpoint: keep `/api/health` **outside** auth (uptime probes) but
  reduce its payload to `{"status":"ok"}` — no version/db info to
  unauthenticated callers.

### Compatibility

- `HEADLESS_YOURQL.md` updated with `Authorization: Bearer $TOKEN` examples
  for curl and a `YOURQL_HEADLESS_TOKEN` env-var convention for scripts
  reading the token file.
- Optional `--headless-no-auth` escape hatch? **No** — a flag that disables
  auth recreates today's risk with a footgun. If a genuine need emerges,
  make it bind `127.0.0.1` + random ephemeral port + token printed anyway.

## Implementation Plan

1. Refactor route registration into `func routes(hs *headlessServer, token string) http.Handler`
   returning the wrapped mux (also unlocks the F-4/T9 integration tests).
2. Token generation/print/persist as above; chmod 0600; document file
   location in HEADLESS_YOURQL.md.
3. Middleware incl. Origin rejection; health endpoint carve-out with reduced
   payload.
4. Tests (httptest): no token → 401; wrong token → 401; correct token → 200;
   Origin-bearing request with valid token → 403; constant-time compare
   present (source assertion is fine); health reachable unauthenticated.
5. Update `documentation/operations/HEADLESS_YOURQL.md` and the
   `AGENT_READ_FIRST.md` quick reference if it mentions headless.

## Risk Assessment

- **Failure mode:** breaks existing user scripts. Mitigation: version-note
  the breaking change in release notes; the migration is one env var /
  header. Acceptable for a security fix.
- **Lock-out risk:** losing the printed token — recovery is restarting the
  process (token regenerates) or reading the 0600 file. Documented.
- Charter §4.3 case 2 (improves safety, reduces no capability). Because this
  touches headless entry points, log the decision in RISK_ANALYSIS_LOG.md.
