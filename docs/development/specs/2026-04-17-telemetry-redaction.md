---
title: "Telemetry Redaction for Errors and Headers"
description: "Prevent credentials and sensitive strings from leaking to telemetry vendors. Closes M-5 and M-6 from the 2026-04-17 audit by adding a shared redactor used automatically for TrackCommandExtended error messages, and by warning when OTel-header values look like credentials."
date: 2026-04-17
status: APPROVED
tags:
  - specification
  - security
  - telemetry
  - redaction
audit-findings:
  - security-audit-2026-04-17.md#m-5
  - security-audit-2026-04-17.md#m-6
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude
    role: AI drafting assistant
---

# Telemetry Redaction for Errors and Headers

Authors
:   Matt Cockayne, Claude *(AI drafting assistant)*

Date
:   17 April 2026

Status
:   APPROVED

---

## Overview

Two telemetry paths can leak credentials to third-party vendors:

**M-5 — `TrackCommandExtended` error messages.** When `ExtendedCollection` is enabled, the `errMsg` argument is shipped verbatim to the configured backend (Datadog, PostHog, OTel). A typical error such as `failed to POST https://api.example.com/?apikey=sk-abc123: connection refused` embeds an API key in telemetry that the user consented to but not an agreement to ship their credentials with it.

**M-6 — OTel exporter headers.** `WithOTelHeaders(map[string]string)` accepts arbitrary headers. Tool authors commonly place bearer tokens or API keys in `Authorization` / `X-API-Key` headers for authenticated OTel endpoints. If the HTTP client's DEBUG-level request logger ever records request headers, those credentials appear in logs. Separately, mis-configuring transport (forgotten TLS) would leak them on the wire.

This spec delivers a shared redactor used automatically on the ingest side, plus an advisory warning when header keys look credential-bearing.

---

## Threat Model

| Vector | Impact |
|--------|--------|
| Command error wraps a URL with credentials in a query parameter | Credentials flow to the configured telemetry backend |
| Command error includes a bearer token (e.g. `Authorization: Bearer …: 401 Unauthorized`) | Same |
| Command error quotes a file path inside a user's home with a rotated-token name | Low-grade PII leak |
| OTel header `Authorization` carries a bearer token; HTTP middleware logs headers at DEBUG | Credentials in log stream |
| OTel header key is mis-named (`X-API-Secret`) so current redaction logic doesn't catch it | Future leak vector |

All five are addressed by: redact at the ingest boundary (M-5), warn on apparently-credential header keys at configuration boundary (M-6), and ensure the HTTP middleware redacts known-sensitive header names consistently.

---

## Design Decisions

**Automatic redaction in `TrackCommandExtended`, not caller-side.** Callers who forget to sanitise are the most common failure mode. Applying redaction unconditionally inside the collector is the only safe default. Callers who need the raw error for local logging can continue to use `Logger.Error` or `fmt.Errorf` before handing it to telemetry.

**Shared package `pkg/redact`.** One redactor is used for telemetry errors AND by the HTTP middleware's header redaction AND by anywhere else the codebase needs to strip credentials from strings. Centralises the pattern catalogue and the tests.

**Pattern-based redaction, not deny-listed keys.** Credentials appear in error messages as text — there is no structured key to target. Patterns match:

- URLs with `userinfo` component (`https?://[^/\s]+:[^@\s]+@...`) — replaced with `https://<redacted>@.../`.
- Common query-parameter names (`apikey`, `api_key`, `key`, `access_token`, `token`, `secret`, `password`, `auth`, `authorization`, `signature`) — value replaced with `***`.
- `Authorization: <scheme> <value>` header tokens (Bearer, Basic, etc.) in free text — value replaced with `***`.
- Long base64url- or hex-looking strings (≥ 32 chars) — replaced with `<redacted-token>`. This is the fuzziest rule; it's deliberately conservative (only very long strings) to avoid false positives on commit hashes, UUIDs, or file hashes.
- Provider-specific prefixes: `sk-`, `ghp_`, `gho_`, `ghs_`, `github_pat_`, `xoxb-`, `xoxp-`, `AIza`, `AKIA` — these are unambiguous credential prefixes and are redacted aggressively (value after prefix replaced with `***`).

**Redaction is a function, not a middleware wrapper.** It operates on a string and returns a string. Simpler to test, compose, and reason about than a stream-wrapping design.

**Advisory warning on OTel header keys.** If a header key matches `/(?i)auth|token|key|secret|bearer|password/`, log a WARN at `WithOTelHeaders` registration time pointing at `docs/components/telemetry.md` for guidance. Does not reject — some operators have internal conventions that embed sensitive strings in header keys intentionally — but makes the risk visible.

**HTTP middleware header-redaction alignment.** `pkg/http/client_middleware.go` already redacts `Authorization` by default. Extend the default redaction list to cover `X-API-Key`, `X-Auth-Token`, `Cookie`, `Set-Cookie`, and the same case-insensitive pattern. This is a small hardening, aligned with the telemetry work.

**Do not invert the model: no "audit mode" that captures raw strings.** Some telemetry libraries offer "debug mode" that ships unredacted payloads. This spec explicitly does not — the redaction invariant holds in all builds. Debugging production issues uses local logging, not telemetry.

---

## Public API Changes

### New package: `pkg/redact`

```go
package redact

import "regexp"

// String applies all redaction patterns to s and returns the sanitised
// result. Safe to call on any string; idempotent; returns unchanged
// output for inputs with no sensitive patterns.
func String(s string) string

// Error is a convenience wrapper equivalent to String(err.Error()).
// Returns "" for nil error.
func Error(err error) string

// SensitiveHeaderKeys is the default set of header names whose values
// are redacted by HTTP middleware and telemetry. Case-insensitive
// match.
var SensitiveHeaderKeys = []string{
    "Authorization",
    "X-API-Key",
    "X-API-Token",
    "X-Auth-Token",
    "X-Access-Token",
    "Cookie",
    "Set-Cookie",
    "Proxy-Authorization",
}

// IsSensitiveHeaderKey reports whether header name matches one of the
// sensitive keys (case-insensitive exact match, or substring match
// against the same fuzzy pattern used for advisory warnings).
func IsSensitiveHeaderKey(name string) bool
```

### Stability tier

`pkg/redact` enters at **Beta** tier.

### Existing API — telemetry behaviour change

`TrackCommandExtended` now applies `redact.String` to `errMsg` automatically. Callers do not need to change anything. The behaviour change is that telemetry events previously carrying raw error strings will now carry redacted ones.

### New OTel option variant — unchanged signature, new warning

`WithOTelHeaders` signature is unchanged. At registration time, a WARN is logged if any header key matches the sensitive pattern.

---

## Internal Implementation

### `pkg/redact/redact.go`

```go
package redact

import (
    "regexp"
    "strings"
)

var (
    // URL userinfo: scheme://user:pass@host/...
    urlUserinfoPattern = regexp.MustCompile(
        `(https?://)[^/\s:@]+:[^/\s@]+@`,
    )

    // Query-string credential parameters.
    queryCredPattern = regexp.MustCompile(
        `(?i)\b(apikey|api_key|key|access_token|token|secret|password|auth|authorization|signature)=([^&\s]+)`,
    )

    // Authorization header tokens in free text.
    authHeaderPattern = regexp.MustCompile(
        `(?i)(authorization:\s*)(bearer|basic|digest|apikey)\s+([A-Za-z0-9._~\-+/=]+)`,
    )

    // Well-known credential prefixes.
    prefixPatterns = []*regexp.Regexp{
        regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),
        regexp.MustCompile(`ghp_[A-Za-z0-9]{30,}`),
        regexp.MustCompile(`gho_[A-Za-z0-9]{30,}`),
        regexp.MustCompile(`ghs_[A-Za-z0-9]{30,}`),
        regexp.MustCompile(`github_pat_[A-Za-z0-9_]{30,}`),
        regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
        regexp.MustCompile(`AIza[A-Za-z0-9_\-]{30,}`),
        regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
    }

    // Fuzzy fallback: long base64url- or hex-looking runs.
    // Threshold 32 chars to avoid false positives on UUIDs (36 with hyphens)
    // and commit hashes (40 hex chars — which are legitimate to leak in
    // non-security contexts, so we allow up to 40; from 41+ we treat as
    // suspicious).
    longOpaqueTokenPattern = regexp.MustCompile(
        `\b[A-Za-z0-9_\-]{41,}\b`,
    )
)

func String(s string) string {
    if s == "" {
        return s
    }

    s = urlUserinfoPattern.ReplaceAllString(s, "${1}<redacted>@")
    s = queryCredPattern.ReplaceAllString(s, "$1=***")
    s = authHeaderPattern.ReplaceAllString(s, "${1}${2} ***")
    for _, p := range prefixPatterns {
        s = p.ReplaceAllStringFunc(s, func(match string) string {
            // Keep the prefix so the redaction hint remains useful for debugging.
            if i := strings.Index(match, "-"); i > 0 && i < len(match)-1 {
                return match[:i+1] + "***"
            }
            return match[:4] + "***"
        })
    }
    s = longOpaqueTokenPattern.ReplaceAllString(s, "<redacted-token>")

    return s
}

func Error(err error) string {
    if err == nil {
        return ""
    }
    return String(err.Error())
}
```

### `pkg/telemetry/telemetry.go` — `TrackCommandExtended` update

```go
func (c *Collector) TrackCommandExtended(
    name string, args []string, durationMs int64, exitCode int,
    errMsg string, extra map[string]string,
) {
    if !c.extendedCollection {
        // Existing behaviour: drop args and errMsg silently.
        c.TrackCommand(name, durationMs, exitCode, extra)
        return
    }

    // Redact both errMsg and args values. args may contain credentials
    // if a tool is invoked with --token=<secret> style flags.
    redactedMsg := redact.String(errMsg)
    redactedArgs := make([]string, len(args))
    for i, a := range args {
        redactedArgs[i] = redact.String(a)
    }

    // ... existing event construction using redactedMsg/redactedArgs ...
}
```

The `args` redaction is a bonus — the audit did not specifically flag `args`, but the same risk applies: a tool invoked with `--api-token=sk-abc123` would leak the token via `args[i]`.

### `pkg/telemetry/backend_otel.go` — header advisory

```go
func WithOTelHeaders(headers map[string]string) OTelOption {
    return func(c *otelConfig) {
        for k := range headers {
            if redact.IsSensitiveHeaderKey(k) {
                c.pendingWarnings = append(c.pendingWarnings, fmt.Sprintf(
                    "OTel header %q appears to carry credentials. "+
                    "Ensure the exporter uses TLS and HTTP middleware "+
                    "redacts the header at DEBUG level. "+
                    "See docs/components/telemetry.md.", k,
                ))
            }
        }
        c.headers = headers
    }
}
```

Warnings are emitted at logger construction time (when the collector is wired) rather than at `WithOTelHeaders` time — `WithOTelHeaders` may run before the logger is available.

### `pkg/http/client_middleware.go` — align with `pkg/redact`

Replace the existing hard-coded `Authorization` redaction with the shared `redact.SensitiveHeaderKeys` / `redact.IsSensitiveHeaderKey` helpers. This broadens the set of redacted headers and centralises the list.

---

## Project Structure

| File | Action |
|------|--------|
| `pkg/redact/redact.go` | **New** — `String`, `Error`, `SensitiveHeaderKeys`, `IsSensitiveHeaderKey`, patterns |
| `pkg/redact/redact_test.go` | **New** — golden-input tests for each pattern; idempotence tests |
| `pkg/redact/redact_fuzz_test.go` | **New** — fuzz the `String` function; assert no panic and output is ≤ input in length |
| `pkg/redact/doc.go` | **New** — package doc explaining threat model and caveats (fuzzy long-token pattern) |
| `pkg/telemetry/telemetry.go` | Modify — `TrackCommandExtended` routes `errMsg` and `args` through `redact.String` |
| `pkg/telemetry/telemetry_test.go` | Modify — tests assert redaction applied in various error shapes |
| `pkg/telemetry/backend_otel.go` | Modify — `WithOTelHeaders` records advisory warnings for sensitive-looking header keys |
| `pkg/telemetry/backend_otel_test.go` | Modify — test that sensitive header name produces a warning |
| `pkg/http/client_middleware.go` | Modify — use `pkg/redact` for the middleware's header redaction list |
| `pkg/http/client_middleware_test.go` | Modify — test that the broadened set of headers is redacted in request-log output |
| `docs/components/telemetry.md` | Modify — new "Credential redaction" subsection documenting behaviour and caveats |

---

## Error Handling

Redaction is infallible by design — invalid input returns the input unchanged (no panics, no errors). The library never refuses to run.

| Scenario | Behaviour |
|----------|-----------|
| Empty string input | Returned unchanged |
| String with no sensitive patterns | Returned unchanged (identity) |
| String with multiple patterns | All patterns applied; order is stable |
| String with a pattern inside a larger context | The matched span is replaced; surrounding context preserved |
| UTF-8 input with non-ASCII credentials | Patterns are ASCII-only; non-ASCII credentials are left alone. Documented limitation; acceptable because essentially all provider credentials are ASCII |

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 95 % for `pkg/redact/` |
| Branch coverage | ≥ 95 % |
| Race detector | `go test -race ./pkg/redact/... ./pkg/telemetry/... ./pkg/http/...` passes |
| Fuzz testing | **Required**. `FuzzRedactString` runs ≥ 60 s in CI; asserts no panic, idempotence (`String(String(s)) == String(s)`), and that output length ≤ input length |
| Golden-file tests | At least 30 golden samples covering each pattern type with positive and negative cases |
| Performance test | Benchmark asserting `String` on a 4 KiB mixed-content string completes in < 200 µs |
| Idempotence test | For each golden sample, `String(String(s)) == String(s)` |
| Telemetry integration | Test: `TrackCommandExtended` with `errMsg` containing an OpenAI key → telemetry event's `errMsg` field contains `sk-***` not the raw key |
| HTTP middleware | Test: a request with `X-API-Key: foo` logs `<redacted>` at DEBUG, not `foo` |
| OTel advisory | Test: `WithOTelHeaders({"Authorization": "Bearer foo"})` produces a WARN log entry at collector init |

### Documentation Deliverables

| Artefact | Scope |
|----------|-------|
| `docs/components/telemetry.md` | New "Credential redaction" subsection: what is redacted, what isn't, fuzzy-long-token caveat, how to verify redaction in your own test suite |
| `docs/components/redact.md` (new) | Reference doc for `pkg/redact`: purpose, API, guidelines, known limitations (ASCII-only, fuzzy long-token false positives on very long filenames) |
| Package doc comment on `pkg/redact/doc.go` | Top-of-file block describing threat model and the "redact-at-ingest" discipline |
| Migration notes | Not required for downstream tool authors — the behaviour improvement is silent |
| CLAUDE.md | Add one line under Testing: "Use `pkg/redact` when writing data to logs, telemetry, or error surfaces that may contain credentials." |
| `docs/how-to/write-a-telemetry-backend.md` (if exists) | Note that custom backends inherit automatic error-field redaction from the collector |

### Observability

| Event | Level | Fields |
|-------|-------|--------|
| `WithOTelHeaders` registered with sensitive-looking key | WARN | `header_name` (not the value) |
| Redaction applied in `TrackCommandExtended` | Not logged | Hot path; no value in logging |
| Redaction tool invocation — if a debug endpoint is ever added | DEBUG | Hash of redacted vs original length, never the strings |

**Redaction invariants**:
1. The redactor never records the original string anywhere.
2. The `pendingWarnings` for OTel headers include only the header NAME, never the value.
3. Pattern matching is constant-time relative to input length; no backtracking (Go's regexp is RE2).

### Performance Bounds

| Metric | Bound |
|--------|-------|
| `String` on a 4 KiB input | ≤ 200 µs |
| `String` on an empty input | Immediate return |
| Memory | O(n) — a single new string of the same or smaller length |
| Pattern compilation | Happens once at package init via `MustCompile` on package-level `var`s |

### Security Invariants

1. `TrackCommandExtended` never sends `errMsg` or `args` to a backend without running them through `redact.String`.
2. HTTP middleware never logs headers in `redact.SensitiveHeaderKeys` with their values.
3. OTel sensitive-looking header keys generate a WARN at collector-init so operators are aware of the risk.
4. The redactor is a pure function: same input → same output; no side effects; safe for concurrent use.
5. New patterns are added via PRs that update the golden-file test suite — the suite acts as a specification of what "is and is not redacted".

---

## Migration & Compatibility

**Behaviour change**: telemetry events previously containing raw error strings now contain redacted ones. This is a strict improvement. No breaking API changes.

**Risk of over-redaction**: the fuzzy long-token pattern could redact legitimate content (e.g. a file hash in a log line). The ≥ 41-char threshold was chosen to avoid common hashes (32-char MD5, 40-char SHA-1/git-commit, 36-char UUID with hyphens) but a future tightening could introduce false positives. The golden test suite captures current behaviour; any change to redaction rules requires updates there.

**API stability**: `pkg/redact` is new at Beta. `pkg/telemetry` and `pkg/http` changes are internal; public interfaces unchanged.

---

## Implementation Phases

Single phase.

| Step | Description |
|------|-------------|
| 1 | Create `pkg/redact/` with `redact.go`, `doc.go`, unit tests, fuzz tests |
| 2 | Wire `redact.String` into `TrackCommandExtended` for `errMsg` and `args` |
| 3 | Wire the sensitive-header advisory into `WithOTelHeaders` |
| 4 | Switch `pkg/http/client_middleware.go` to the shared `redact.SensitiveHeaderKeys` |
| 5 | Update `docs/components/telemetry.md` and add `docs/components/redact.md` |
| 6 | `just ci` green |

Estimated effort: **one day**.

---

## Resolved Decisions

1. **Automatic redaction, not caller-side** — callers forget; defaults matter.
2. **One shared redactor** — `pkg/redact` used by telemetry AND HTTP middleware AND any future surface. Centralises the pattern catalogue.
3. **Pattern catalogue with golden tests** — the tests are the spec of "what is redacted". Pattern additions require review.
4. **OTel header warning is advisory, not rejecting** — some operators intentionally embed sensitive-looking strings in header names. Visibility over restriction.
5. **Long-opaque-token threshold at 41 chars** — avoids false positives on MD5/SHA-1/UUID; still catches most real credentials.
6. **ASCII-only patterns** — provider credentials are universally ASCII; avoids UTF-8 edge cases.

---

## Future Considerations

- **Structured redaction inside JSON payloads**: if custom backends serialise free-form JSON blobs containing credentials, a content-aware redactor might be useful. Out of scope; current patterns already catch most issues in serialised text.
- **Per-tool redaction allowlist**: a tool author who operates in a trusted network and wants raw errors for diagnosis could supply a `TelemetryConfig.RedactionMode = "minimal"` switch. Explicitly NOT adding this in Phase 1 — opt-out is a security anti-pattern.
- **Expand `SensitiveHeaderKeys`** over time as new conventions appear. Each addition is a small, auditable PR.
