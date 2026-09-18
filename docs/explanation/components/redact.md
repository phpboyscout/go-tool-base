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
module** (zero dependencies, pure standard library). Its full documentation,
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

## Why redact exists

Error messages, command arguments, and HTTP header values routinely carry
credentials by accident: an HTTP client wraps a URL with an embedded token, a
flag like `--api-key=sk-abc123` lands in `os.Args`, a failed OTLP export quotes
an `Authorization` header. The right defence is to redact **at the boundary**,
in process, before anything ships, which is why the collector, HTTP
middleware, and logger helpers route untrusted strings through `redact.String`
on their way out; local process logs that never leave the host are not
redacted, since they may need raw content for debugging.

Redaction is **best-effort, not a guarantee**: a pattern catalogue never
reaches 100% recall, and a credential in a non-standard format will slip
through. The rule catalogue, the opaque-token length threshold, and the
exact-vs-fuzzy distinction between `SensitiveHeaderKeys` and
`IsSensitiveHeaderKey` are on the module's own
[threat model page](https://redact.go.phpboyscout.uk/explanation/threat-model/).

## Related

- **Module docs:** [redact.go.phpboyscout.uk](https://redact.go.phpboyscout.uk)
- **Trust model / spec:** [`0063-telemetry-redaction`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0063-telemetry-redaction)
