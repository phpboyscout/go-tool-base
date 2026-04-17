---
title: "Redact — Credential Stripping at Boundaries"
description: "pkg/redact provides pattern-based credential redaction for strings shipped to telemetry vendors, distributed logs, and other third-party observability surfaces. Applied automatically inside the telemetry collector and HTTP middleware so callers cannot forget to sanitise."
date: 2026-04-17
tags: [component, security, telemetry, redaction]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Redact — Credential Stripping at Boundaries

`pkg/redact` is the shared redactor for any surface in GTB — and in tools built on GTB — that writes free-form strings outside the trust boundary of the local process. It is applied automatically by the telemetry collector for `TrackCommandExtended` error and args fields, and by HTTP middleware for known-sensitive request headers. Callers that write their own logs or telemetry events should route untrusted strings through it too.

Local-process logs that never leave the host do not need to go through this package — those may need raw content for debugging.

## Threat Model

Credentials reach observability surfaces by accident more often than by design:

| Vector | Example |
|--------|---------|
| HTTP client error wrapping a URL with a token | `failed GET https://u:pass@api.example.co/?apikey=sk-abc...: 401` |
| Command invoked with `--api-token=<secret>` | `os.Args = ["--api-token=sk-proj-abc..."]` |
| OTel exporter configured with `Authorization` header | middleware that logs headers dumps the token |
| Provider error message quoting back a bearer token | `Authorization: Bearer abc123...` appears in `errMsg` |

Once that content reaches a third-party ingest it is outside the operator's control — replicated, indexed, and retained longer than intended. Redacting at the boundary is the last controllable step.

## Invariants

1. **Idempotent** — `String(String(s)) == String(s)`. Middlewares that might double-redact cannot corrupt output.
2. **Never panics** — any input, including zero-length or control-byte strings, is safe to pass.
3. **Bounded growth** — replacements are fixed-length (`***`, `<redacted>`, `<redacted-token>`). Output never grows the input unboundedly.
4. **Pure function** — same input → same output; no side effects; safe for concurrent use.
5. **Original string never retained** — the redactor does not log, cache, or transmit the input anywhere.

Both invariants 1 and 2 are enforced by `FuzzRedactString` in CI.

## API

```go
import "github.com/phpboyscout/go-tool-base/pkg/redact"

// Clean a free-form string before shipping.
clean := redact.String(userInput)

// Convenience wrapper over err.Error() — returns "" for nil.
cleanErr := redact.Error(err)

// Decide whether a header name looks like it carries a credential.
if redact.IsSensitiveHeaderKey(name) {
    // warn the operator, redact the value, etc.
}

// The default redaction list used by HTTP middleware and telemetry.
for _, k := range redact.SensitiveHeaderKeys { /* ... */ }
```

## What Gets Redacted

| Shape | Match | Replacement |
|-------|-------|-------------|
| URL userinfo | `https?://user:pass@host/...` | `https://<redacted>@host/...` |
| Query cred params | `?apikey=`, `?api_key=`, `?token=`, `?secret=`, `?password=`, `?auth=`, `?authorization=`, `?signature=`, `?access_token=`, `?refresh_token=`, `?key=` | value replaced with `***` |
| Authorization header in free text | `Authorization: <scheme> <token>` where scheme is Bearer/Basic/Digest/ApiKey | `Authorization: <scheme> ***` |
| OpenAI-family prefix | `sk-[A-Za-z0-9_-]{16,}` | `sk-***` |
| GitHub tokens | `ghp_`, `gho_`, `ghs_`, `github_pat_` | prefix + `***` |
| Slack tokens | `xoxb-`, `xoxp-`, `xoxa-`, etc. | prefix + `***` |
| Google API key | `AIza[A-Za-z0-9_-]{30,}` | `AIza***` (or more, up to the first `-` / `_`) |
| AWS access key ID | `AKIA[A-Z0-9]{16}` | `AKIA***` |
| Fuzzy long token | any ≥ 41-char alphanumeric/`_`/`-` run not already caught | `<redacted-token>` |

## Known Limitations

- **ASCII-only patterns.** Virtually all provider credentials are ASCII; UTF-8 edge cases are out of scope.
- **False negatives on unusual credential formats.** A credential that doesn't match any catalogued pattern and is under 41 characters will slip through. Add a pattern PR if a vendor you care about has a unique shape.
- **Fuzzy pattern threshold at 41 characters.** Chosen to avoid false positives on UUIDs (32/36), MD5 (32), and git SHAs (40). SHA-256 hashes (64) will match — acceptable; hashes rarely appear in error strings and over-redaction is safer than under-redaction.
- **Pattern order matters.** Earlier-matching patterns claim spans first. A string like `?token=` + long-opaque-value is redacted as `token=***` (by the query-param rule), not `token=<redacted-token>` (the fuzzy fallback). Both outcomes are redactions; the difference is cosmetic.

## Call-Site Discipline

`pkg/redact` is **the** entry point for untrusted-string redaction. When adding a new code path that writes to telemetry, distributed logs, or a third-party observability surface:

- Route any free-form string that may contain user- or environment-derived content through `redact.String` or `redact.Error`.
- Use `redact.IsSensitiveHeaderKey` when deciding whether to log a header value or emit an operator advisory.
- When in doubt, redact — the cost is negligible (a few regex passes on a bounded string); the cost of missing is a credential in a vendor log.

Failing to route through the helper reintroduces the leakage class this package exists to close.
