# F-14 · CI guard against credential leakage into logs and prompts

**Severity:** Low · **Effort:** Small · **Risk categories:** credential protection (charter absolute rule #2)

## Problem Statement

"Never log API keys or passwords" is charter §4.0 absolute rule #2. Today it
is enforced only by human vigilance during review. The codebase has many
places where secrets flow near strings: DSN construction in nine drivers,
LLM request bodies in four providers, OAuth tokens in `google_auth.go`, and
prompt assembly that embeds data-source metadata. One careless
`log.Printf("request: %v", req)` reintroduces the class of bug previously
fixed in `llm_openai.go` (per TECH_REVIEW 2026-07-26 §2.2).

Static review doesn't scale; make the invariant executable.

## Solution Design

Three complementary guards, cheapest first.

### Guard 1 — Test-time source scan (Go test, runs in CI)

`secret_leak_test.go` in `pkg/services`:

```go
func TestNoSecretLoggingPatterns(t *testing.T) {
    forbidden := []struct{ name, re string }{
        {"log password var", `log\.(Print|Printf|Println)\([^)]*[Pp]assword`},
        {"log apikey var",   `log\.(Print|Printf|Println)\([^)]*([Aa]pi[Kk]ey|APIKey)`},
        {"log token var",    `log\.(Print|Printf|Println)\([^)]*[Tt]oken[^s]`},
        {"log raw req body", `log\.(Print|Printf|Println)\([^)]*(req\.Body|requestBody\b)`},
        {"fmt print dsn",    `fmt\.Print(f|ln)?\([^)]*\bdsn\b`},
        {"slog secret attrs", `slog\.\w+\([^)]*"?(password|api_key|apikey)"?[,)]`},
    }
    filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
           strings.HasSuffix(path, "_test.go") { return nil }
        src, _ := os.ReadFile(path)
        for _, f := range forbidden {
            if regexp.MustCompile(f.re).Match(src) {
                t.Errorf("%s: matches forbidden pattern %q", path, f.name)
            }
        }
        return nil
    })
}
```

Tuned to minimize false positives (test files exempted since they contain
the words legitimately). Runs in milliseconds; fails CI loudly.

### Guard 2 — Central redaction helper + mandatory use

Formalize the ad-hoc MySQL redaction into one pure function:

```go
// pkg/engine/sql_errors.go or new pkg/services/redact.go
// RedactSecret scrubs password/key material from connection strings and
// error text. All drivers MUST route DSNs and driver error strings through
// this before logging or embedding in user-visible messages.
func RedactSecret(s string) string
```

Implementation: regex over known schemes
(`password=…`, `:password@host`, `PWD=…`, `token=…`, `?key=…`) replacing
values with `***`. Unit-test against each driver's real DSN shapes (golden
cases per driver — doubles as documentation of DSN formats).

Then sweep drivers to route their (already-safe) logs through it, so future
log additions inherit safety by construction rather than by memory.

### Guard 3 — Runtime tripwire (optional, dev builds only)

In `NewLLMClient` request builders, wrap the outgoing body marshal site:

```go
if dbg := os.Getenv("YOURQL_DEBUG_REQUESTS"); dbg != "" {
    slog.Debug("LLM request", "url", url, "model", model, "messages", len(messages))
}
```

i.e., never log bodies at all — the env-gated line logs only metadata. The
guard is the *policy statement in code*: there is no code path that marshals
secrets into logs, so none can be accidentally enabled.

## Implementation Plan

1. Guard 1 test + wire into CI (F-3's go job picks it up automatically).
2. `RedactSecret` + golden tests; sweep drivers/providers.
3. Audit prompt-assembly paths (`agentic_loop.go` buildToolSystemPrompt) to
   confirm no credential-bearing config reaches prompt text (data-source
   names/types are fine; DSNs/password fields are not) — add assertion test
   feeding a DataSource with hostile values through the prompt builder and
   scanning the output with the same regexes.
4. Charter §3.5 gains one sentence pointing to the guard test.

## Risk Assessment

- False positives annoy developers → keep patterns narrow, allow inline
  `//nolint:secretscan` escape with mandatory comment explaining why.
- Zero runtime impact (tests + refactored formatting only).
