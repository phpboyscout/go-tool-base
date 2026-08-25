---
title: "Redact, Credential Stripping at Boundaries"
description: "go-tool-base consumes the standalone go/redact module to strip credential-like content from strings before they reach telemetry, logs, or third-party observability surfaces."
date: 2026-07-13
tags: [component, security, telemetry, redaction]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Redact: Credential Stripping at Boundaries

The credential-redaction helper has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/redact`](https://gitlab.com/phpboyscout/go/redact)
module** (zero dependencies (pure standard library). Its full documentation) 
the `String`/`Error` API, the sensitive-header helpers, the rule catalogue, and
the threat model. Now lives at:

> **[redact.go.phpboyscout.uk](https://redact.go.phpboyscout.uk)**

`redact` is framework-free, so go-tool-base consumes it **directly** (no adapter):
callers import `gitlab.com/phpboyscout/go/redact` and use `redact.String`,
`redact.Error`, `redact.SensitiveHeaderKeys`, and `redact.IsSensitiveHeaderKey`
as before. See the
[migration note](../../reference/migration/v0.x-redact-extracted.md) for the
import-path change.

## How go-tool-base uses it

`redact` remains GTB's single entry point for untrusted-string redaction:

- **Telemetry**: `TrackCommandExtended` applies `redact.String` to `errMsg` and
  every `args` entry before events are buffered; the OTel backend warns on
  caller-supplied sensitive header keys.
- **HTTP middleware**: `pkg/http/logging.go` sources its header-redaction
  catalogue from `redact.SensitiveHeaderKeys`.
- **Diagnostics**: the `doctor report` support bundle redacts config values
  through the same helpers.

Route any tool-owned log line, custom telemetry event, or third-party
observability payload with free-form strings through `redact.String` /
`redact.Error`.

## Why redact exists (threat model)

Error messages, command arguments, and HTTP header values routinely carry
credentials **by accident**: an HTTP client wraps a URL with an embedded token,
a flag like `--api-key=sk-abc123` lands in `os.Args`, a failed OTLP export
quotes an `Authorization` header. The moment that content reaches a third-party
ingest it is outside the operator's control, replicated, indexed, and retained
longer than intended. The right defence is to redact **at the boundary**, in
process, before anything ships, so callers never have to remember to sanitise.
That is exactly why the collector, HTTP middleware, and logger helpers route
untrusted strings through `redact.String` on their way out. Conversely, do
**not** redact local process logs that never leave the host: they may need raw
content for debugging.

Redaction is **best-effort, not a guarantee**. A pattern catalogue never reaches
100% recall: it catches the common shapes (URL userinfo, common credential
query-parameter names, `Authorization`-header tokens, well-known provider
prefixes like `sk-`, `ghp_`, `AIza`, `AKIA`, and a conservative opaque-token
fallback). A credential in a non-standard format will slip through; callers
handling such inputs must redact upstream themselves.

Two deliberate tradeoffs shape the fallback rules:

- **The opaque-token fallback matches at ≥41 characters.** That threshold is
  chosen so it does not false-positive on UUIDs (36 with hyphens), MD5 (32) or
  SHA-1 (40) hashes. SHA-256 digests (64 chars) *will* match, an accepted
  tradeoff, since hashes rarely appear in error strings and losing one to
  `****` is harmless.
- **Patterns are ASCII-only.** Virtually all real-world provider tokens are
  ASCII; UTF-8 credentials in the wild are vanishingly rare, so the added
  matcher complexity is not worth it.

The sensitive-header helpers draw an intentional exact-vs-fuzzy distinction.
`SensitiveHeaderKeys` is the **exact** (case-insensitive) allowlist used to
decide *what to redact*. `IsSensitiveHeaderKey` is deliberately **wider**: it
matches that exact list *or* a fuzzy substring pattern (`auth`, `token`, `key`,
`secret`, `bearer`, `password`, `credential`). Use the exact set when deciding
what to strip; use the fuzzy function only to decide whether to **warn** an
operator that a caller-supplied header name looks credential-bearing, its
question is "did the operator likely put a secret here?", not "is this on my
allowlist?".

## Related

- **Module docs:** [redact.go.phpboyscout.uk](https://redact.go.phpboyscout.uk)
- **Trust model / spec:** [`0063-telemetry-redaction`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0063-telemetry-redaction)
