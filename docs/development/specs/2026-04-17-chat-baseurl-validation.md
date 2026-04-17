---
title: "Chat Provider BaseURL Validation"
description: "Reject malformed or credential-bearing BaseURL values in the OpenAI-compatible and Gemini chat providers. Closes M-3 from the 2026-04-17 audit by refusing non-HTTPS schemes, URLs with embedded userinfo, and URLs outside a configurable allowlist for production deployments."
date: 2026-04-17
status: IMPLEMENTED
tags:
  - specification
  - security
  - chat
  - url-validation
audit-finding: security-audit-2026-04-17.md#m-3
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude
    role: AI drafting assistant
---

# Chat Provider BaseURL Validation

Authors
:   Matt Cockayne, Claude *(AI drafting assistant)*

Date
:   17 April 2026

Status
:   APPROVED

---

## Overview

`pkg/chat.Config.BaseURL` is accepted by the OpenAI-compatible (`ProviderOpenAICompatible`) and Gemini (`cfg.BaseURL`) paths without validation. An operator who can influence config — via a tampered config file, a compromised setup wizard, a misconfigured environment variable — can redirect API traffic to an attacker-controlled host. The hardened HTTP client refuses plaintext HTTP, but an attacker can still specify `https://evil.example.com` (a valid HTTPS host) and harvest credentials sent in `Authorization` headers.

A separate concern: URLs of the form `https://user:pass@host/` parse cleanly in `net/url`. Some HTTP libraries propagate the `userinfo` segment as an `Authorization: Basic` header, others log the full URL at DEBUG, and a few even expose it in error messages. Rejecting `URL.User` at the boundary avoids all three paths.

This is identified as **M-3** in `docs/development/reports/security-audit-2026-04-17.md`.

---

## Threat Model

| Vector | Impact |
|--------|--------|
| Config file tampered to set `openai.base_url: "https://evil.example.com"` | API requests (and their `Authorization: Bearer <token>` headers) flow to attacker |
| Env var `${TOOL}_OPENAI_BASE_URL` set by malicious shell profile, CI job, or container orchestrator | Same |
| User mis-copies a URL containing userinfo (`https://user:pass@api...`) during setup | Credentials may appear in logs or be incorporated into HTTP Basic by the client library |
| Downstream tool author writes their own `BaseURL`-reading config key without validation | Reintroduces the same issue |

A defence at the `pkg/chat` boundary catches all four: validation happens once at provider construction, regardless of how `BaseURL` was configured.

---

## Design Decisions

**Validation at provider construction, not at each request.** The `BaseURL` is set once when the client is constructed and does not change per request. Validation therefore belongs in `newClaudeLocal` / `newGemini` / the `OpenAICompatible` branch of the OpenAI factory. Validating at request time would be redundant and would pay the check cost on every API call.

**Five rejection rules, ordered from cheapest to most expensive:**

1. **Reject empty-BaseURL for providers that require it** (`ProviderOpenAICompatible` cannot resolve without one). Empty is permitted for `ProviderOpenAI` and `ProviderGemini` where a BaseURL override is optional.
2. **Reject length > `MaxBaseURLLength` = 2 KiB.** Normal BaseURLs are under 200 bytes.
3. **Reject URLs containing control characters** (`0x00`–`0x1F`, `0x7F`). Same defensive measure as `pkg/browser`.
4. **Reject unless `url.Parse` succeeds and `Scheme == "https"`.** HTTP-only for local testing is explicit opt-in via a separate `AllowInsecureBaseURL` flag (see below).
5. **Reject `URL.User != nil`.** URLs with embedded userinfo are rejected unconditionally. Credentials belong in the `Authorization` header, not the URL.

**Local-testing opt-out: `Config.AllowInsecureBaseURL`.** Tests running against an `httptest.Server` need to accept `http://127.0.0.1:PORT`. Rather than making validation configurable at runtime from config, we add a dedicated bool flag on `Config` that tests can set explicitly. Config-from-YAML cannot set this flag (it is not serialised) — so a production config cannot bypass HTTPS enforcement even by accident.

**No hostname allowlist by default.** The audit raised whether to restrict `BaseURL` to a hardcoded list of known provider hosts (`api.openai.com`, `generativelanguage.googleapis.com`, etc.). That is too strict — legitimate proxy deployments (LiteLLM, Kong, corporate egress gateways) use arbitrary private hostnames. Instead, we log an INFO-level message at client construction naming the BaseURL host so operators have an audit trail of "which host is this tool talking to".

**Reject `example.com` and other placeholder hosts in production.** Config scaffolding sometimes ships with `https://api.example.com/v1` placeholders. Reject `example.com`, `example.net`, `example.org`, `localhost.localdomain` at config-validation time so a placeholder never silently "works" against a typosquatted real domain.

---

## Public API Changes

### New sentinel errors

```go
package chat

// ErrInvalidBaseURL is returned when Config.BaseURL fails validation.
var ErrInvalidBaseURL = errors.New("invalid chat provider base URL")
```

### New Config field

```go
type Config struct {
    // ... existing fields ...

    // AllowInsecureBaseURL permits HTTP (non-HTTPS) BaseURLs. Used
    // exclusively by tests with httptest.Server. Production callers
    // must leave this false. This field is not serialised to config
    // (json:"-") so config files cannot enable it.
    AllowInsecureBaseURL bool `json:"-"`
}
```

### Existing API — no signature changes

`chat.New` and each provider factory retain their signatures. The behaviour change is that `chat.New` returns `ErrInvalidBaseURL` (wrapped) for inputs that previously would have succeeded silently.

### Optional: exported validator

```go
// ValidateBaseURL returns nil if baseURL is acceptable for use as a
// chat provider endpoint, or an error wrapping ErrInvalidBaseURL
// otherwise. Pass allowInsecure=true ONLY from tests.
//
// Downstream tool authors should call this at the boundary where they
// accept BaseURL input (e.g. their own setup wizard) so errors surface
// early rather than from chat.New.
func ValidateBaseURL(baseURL string, allowInsecure bool) error
```

---

## Internal Implementation

### `pkg/chat/baseurl.go` — new file

```go
package chat

import (
    "net/url"
    "strings"

    "github.com/cockroachdb/errors"
)

// MaxBaseURLLength caps the length of a provider BaseURL.
const MaxBaseURLLength = 2048

// placeholderHosts are rejected to prevent accidental use of
// template/example values in production config.
var placeholderHosts = map[string]bool{
    "example.com":         true,
    "example.net":         true,
    "example.org":         true,
    "localhost.localdomain": true,
}

func ValidateBaseURL(baseURL string, allowInsecure bool) error {
    if baseURL == "" {
        return nil // empty is permitted; caller decides whether to require
    }

    if len(baseURL) > MaxBaseURLLength {
        return errors.WithHintf(ErrInvalidBaseURL,
            "base URL exceeds maximum length of %d bytes", MaxBaseURLLength)
    }

    for _, r := range baseURL {
        if r < 0x20 || r == 0x7F {
            return errors.WithHint(ErrInvalidBaseURL,
                "base URL contains control characters")
        }
    }

    parsed, err := url.Parse(baseURL)
    if err != nil {
        return errors.Wrap(ErrInvalidBaseURL, "parsing base URL")
    }

    if parsed.User != nil {
        return errors.WithHint(ErrInvalidBaseURL,
            "base URL must not contain credentials. Use the Token field instead.")
    }

    scheme := strings.ToLower(parsed.Scheme)
    switch scheme {
    case "https":
        // ok
    case "http":
        if !allowInsecure {
            return errors.WithHint(ErrInvalidBaseURL,
                "base URL must use https. Set AllowInsecureBaseURL=true in tests only.")
        }
    default:
        return errors.WithHintf(ErrInvalidBaseURL,
            "base URL scheme %q is not supported; use https", scheme)
    }

    if parsed.Host == "" {
        return errors.WithHint(ErrInvalidBaseURL, "base URL must include a host")
    }

    hostname := strings.ToLower(parsed.Hostname())
    if placeholderHosts[hostname] {
        return errors.WithHintf(ErrInvalidBaseURL,
            "base URL host %q is a placeholder — replace it with your provider's real endpoint", hostname)
    }

    return nil
}
```

### Call site updates

**`pkg/chat/client.go` — central validation in `New`:**

```go
func New(ctx context.Context, p *props.Props, cfg Config) (ChatClient, error) {
    if err := ValidateBaseURL(cfg.BaseURL, cfg.AllowInsecureBaseURL); err != nil {
        return nil, err
    }
    // ... existing provider dispatch ...
}
```

Centralising in `New` means every provider gets the check without per-provider changes. Providers that do not use BaseURL simply ignore the field (validation of an empty string returns nil).

**`pkg/chat/openai.go`** — additional check that `ProviderOpenAICompatible` requires a non-empty BaseURL:

```go
if cfg.Provider == ProviderOpenAICompatible && cfg.BaseURL == "" {
    return nil, errors.WithHint(ErrInvalidBaseURL,
        "ProviderOpenAICompatible requires BaseURL to be set")
}
```

**Audit logging:**

After successful validation, at INFO level, log the host (not the full URL):

```go
log.Info("chat provider initialised",
    "provider", cfg.Provider,
    "endpoint_host", parsedHost)
```

Knowing "this tool is sending chat traffic to host X" is operationally useful and aids detection of misconfiguration. The URL path and query are not logged (they may contain API-specific identifiers).

---

## Project Structure

| File | Action |
|------|--------|
| `pkg/chat/baseurl.go` | **New** — `ValidateBaseURL`, `MaxBaseURLLength`, `ErrInvalidBaseURL`, placeholder-host set |
| `pkg/chat/baseurl_test.go` | **New** — unit tests for each rejection rule; golden accept/reject cases |
| `pkg/chat/baseurl_fuzz_test.go` | **New** — fuzz test asserting no panic and consistent rejection/acceptance |
| `pkg/chat/client.go` | Modify — call `ValidateBaseURL` in `New` before provider dispatch; log endpoint host at INFO |
| `pkg/chat/openai.go` | Modify — reject empty BaseURL for `ProviderOpenAICompatible` |
| `pkg/chat/client_test.go` | Modify — adjust tests that used `http://` BaseURLs to set `AllowInsecureBaseURL: true` |
| `pkg/chat/claude_local_test.go`, `gemini_test.go`, etc. | Modify — same as above where tests construct test HTTP servers |
| `docs/components/chat.md` | Modify — add "Provider endpoint security" subsection |
| `docs/how-to/configure-chat.md` (if present) | Modify — note that BaseURL must be HTTPS; placeholders rejected |

---

## Error Handling

| Scenario | Error | Hint |
|----------|-------|------|
| Empty BaseURL + required by provider | `ErrInvalidBaseURL` | `ProviderOpenAICompatible requires BaseURL to be set` |
| Length > 2 KiB | `ErrInvalidBaseURL` | Byte-count hint |
| Control characters in URL | `ErrInvalidBaseURL` | `base URL contains control characters` |
| `url.Parse` failure | `ErrInvalidBaseURL` wrapping parse error | `parsing base URL` |
| `URL.User` populated (`user:pass@`) | `ErrInvalidBaseURL` | `base URL must not contain credentials. Use the Token field instead.` |
| Non-HTTPS scheme and `AllowInsecureBaseURL=false` | `ErrInvalidBaseURL` | `base URL must use https. Set AllowInsecureBaseURL=true in tests only.` |
| Missing host | `ErrInvalidBaseURL` | `base URL must include a host` |
| Placeholder host (e.g. `example.com`) | `ErrInvalidBaseURL` | `base URL host is a placeholder — replace it with your provider's real endpoint` |

All errors are wrapped via `cockroachdb/errors` with `WithHint` for user-facing guidance.

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 95 % for `pkg/chat/baseurl.go` |
| Branch coverage | 100 % — every rejection rule hit by a dedicated test |
| Race detector | `go test -race ./pkg/chat/...` passes |
| Fuzz testing | `FuzzValidateBaseURL` runs ≥ 60 s in CI; corpus seeded with canonical URLs, malformed URLs, userinfo-bearing URLs, non-HTTPS schemes, control characters, oversized inputs |
| Security-specific tests | Credentials-in-URL test: `https://user:pass@api.openai.com` → `ErrInvalidBaseURL` |
| Security-specific tests | Downgrade test: `http://evil.com` with `AllowInsecureBaseURL=false` → error |
| Security-specific tests | Placeholder test: `https://api.example.com` → error |
| Regression tests | Existing tests that point at `httptest.Server` updated to set `AllowInsecureBaseURL: true`; all must pass |
| Integration test | End-to-end test with a misconfigured BaseURL confirms the user sees a clear error at `chat.New`, not a cryptic HTTP failure later |

### Documentation Deliverables

| Artefact | Scope |
|----------|-------|
| `docs/components/chat.md` | Add "Provider endpoint security" subsection listing the rejection rules, the audit-log behaviour, and the `AllowInsecureBaseURL` test-only flag |
| Package doc comment on `pkg/chat/baseurl.go` | Top-of-file block explaining the threat model and the rationale for each rule |
| Migration notes | Not required — the only behaviour change is rejecting previously-accepted-but-insecure URLs. Tests that use `http://` need one-line updates |
| CLAUDE.md | Add one line under Architecture (Chat section): "BaseURL values must pass `ValidateBaseURL`; test-only HTTP requires `AllowInsecureBaseURL=true`." |

### Observability

| Event | Level | Fields |
|-------|-------|--------|
| Validation rejected | Returned error only; not logged by the package | Caller may log with their own context |
| Chat provider initialised successfully | INFO | `provider`, `endpoint_host` (hostname only — never path or query) |
| Audit-log the endpoint at INFO for every `chat.New` call | INFO | Ensures every process startup leaves a record of which host the tool talks to |

**Redaction invariant**: the URL path and query are never logged — they may contain provider-specific identifiers, per-tool routing slugs, or session identifiers. The hostname alone is the useful operational signal.

### Performance Bounds

| Metric | Bound |
|--------|-------|
| `ValidateBaseURL` wall-clock | ≤ 10 µs — single `url.Parse` on a bounded-length string |
| Memory | O(1) above the input |
| Frequency | Once per `chat.New` call; not in the hot path |

### Security Invariants

1. Non-HTTPS BaseURLs are rejected in production (`AllowInsecureBaseURL=false`).
2. URLs with `URL.User` (userinfo) are rejected unconditionally.
3. Placeholder hostnames (`example.com`, etc.) are rejected.
4. Validation runs before provider construction, so invalid config fails fast.
5. The hostname (not full URL) is logged at INFO for every provider initialisation.
6. `AllowInsecureBaseURL` is not config-serialisable — config files cannot turn off HTTPS enforcement.

---

## Migration & Compatibility

**Behaviour change**: callers with misconfigured BaseURL values will now receive an error at `chat.New` time. In exchange, they previously would have got an obscure network failure or, worse, a successful request to a wrong host.

**Test migration**: test files that use `httptest.Server` (which serves HTTP) need `AllowInsecureBaseURL: true` in the `Config`. This is a one-line addition per test file. All existing Claude/Gemini/OpenAI tests will need this update.

**No public API signature change**. New `AllowInsecureBaseURL` field is additive; new `ValidateBaseURL` function and `ErrInvalidBaseURL` sentinel are additive.

**API stability**: `pkg/chat` is Beta-tier. All changes are additive.

---

## Implementation Phases

Single phase — small, focused change.

| Step | Description |
|------|-------------|
| 1 | Create `pkg/chat/baseurl.go` with `ValidateBaseURL`, sentinel error, constants, placeholder set |
| 2 | Add `AllowInsecureBaseURL` to `Config` (with `json:"-"`) |
| 3 | Call `ValidateBaseURL` in `chat.New`; reject empty BaseURL for `ProviderOpenAICompatible` |
| 4 | Add endpoint-host INFO log at successful provider init |
| 5 | Update all test files that use `httptest.Server` to set `AllowInsecureBaseURL: true` |
| 6 | Unit + fuzz tests for `ValidateBaseURL` |
| 7 | Update `docs/components/chat.md` and CLAUDE.md |
| 8 | `just ci` green |

Estimated effort: **half a day** (most of which is mechanical test updates).

---

## Resolved Decisions

1. **Validation centralised at `chat.New`** rather than per-provider — avoids drift if new providers are added.
2. **HTTPS-only by default, opt-out via `AllowInsecureBaseURL`** — the opt-out is not config-serialisable, so config files cannot downgrade security.
3. **No hostname allowlist** — too restrictive for legitimate proxy deployments. INFO-level audit log of the host is the compensating control.
4. **`URL.User` rejected unconditionally** — no legitimate reason to use HTTP Basic for chat APIs; all three supported providers use `Authorization: Bearer`.
5. **Placeholder hostnames rejected** — catches scaffolding mistakes before they reach the wire.
6. **Exported `ValidateBaseURL` helper** — downstream tools that accept BaseURL in their own config surface can use the same rules.

---

## Future Considerations

- **Hostname allowlist as opt-in**: A tool author who wants to restrict their build to `api.openai.com` only could set `chat.AllowedHosts = []string{"api.openai.com"}`. This is an additive feature and does not affect the default behaviour.
- **Certificate pinning**: Pin the TLS certificate of known provider hosts so a MITM against the public PKI cannot succeed. Out of scope — CA compromises are rare and the attack model here is primarily config-tampering.
- **Redact BaseURL path in error messages**: if `provider initialised` logs ever expand to include the full URL, the redaction rule above should be enforced.
