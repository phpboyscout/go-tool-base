---
title: "redact: scheme-agnostic URL userinfo stripping"
description: "redact.String only strips URL userinfo for http/https URLs, so connection-string passwords (postgres://, redis://, amqp://, mongodb://) pass through unredacted into telemetry and error surfaces that trust it as the last line of defence. Widen the scheme group to the RFC 3986 scheme shape, and add a JSON-form credential rule as secondary scope."
date: 2026-07-23
status: DRAFT
tags:
  - specification
  - redact
  - security
  - telemetry
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Fable 5
    role: AI drafting assistant
---

# redact: scheme-agnostic URL userinfo stripping

Authors
:   Matt Cockayne, Claude Fable 5 *(AI drafting assistant)*

Date
:   2026-07-23

Status
:   DRAFT — pending review

Related
:   [architectural review](../reports/2026-07-23-architectural-review.md) (HIGH finding, cross-cutting leaf modules section)

---

## 1. Problem

`go/redact/redact.go:22-24` anchors the URL-userinfo rule to `(https?://)`:

```go
urlUserinfoPattern = regexp.MustCompile(
    `(https?://)[^/\s:@]+:[^/\s@]+@`,
)
```

Verified empirically: `postgres://admin:s3cretpw@db.internal:5432/app` and
`redis://:hunter2@cache.internal:6379` pass through `redact.String`
**unchanged**, while the `https://` equivalent is redacted. The module doc
(`doc.go`) and GTB's `docs/components/redact.md` both claim generic "URL
userinfo" stripping — the implementation is narrower than the contract.

Concrete failure scenario: a failed database / AMQP / Redis / MongoDB
connection produces an error string embedding the full connection URL
(`dial tcp ... postgres://user:pass@host refused`). That string flows through
exactly the surfaces that trust `redact.String` as the last line of defence:

- `TrackCommandExtended`'s `errMsg` (`pkg/telemetry/telemetry.go:210`)
- event metadata redacted at the ingest boundary (`telemetry.go:126`)
- doctor-report messages and any free-form string routed per the
  "Credential Redaction" guidance in AGENTS.md

The password reaches the telemetry vendor verbatim. Connection strings are one
of the *most* common shapes of leaked credential in error telemetry, and
non-HTTP schemes are the norm for them.

A related LOW finding: JSON-form credentials are also uncovered. Verified:
`{"access_token":"shorttok123"}` passes through unchanged — `queryCredPattern`
requires `=`, and the fallback rules only catch tokens ≥ 41 chars, JWTs, or
known prefixes. OAuth token-endpoint JSON bodies quoted in HTTP client errors
are a common shape at these same surfaces.

## 2. Proposed change

**Primary — widen the userinfo scheme group** to the RFC 3986 scheme shape:

```go
urlUserinfoPattern = regexp.MustCompile(
    `([A-Za-z][A-Za-z0-9+.\-]*://)[^/\s:@]+:[^/\s@]+@`,
)
```

Group 1 (the scheme + `://`) remains preserved verbatim so the URL stays
recognisable after redaction, exactly as today. The existing tail
(`[^/\s:@]+:[^/\s@]+@` — a `user:pass@` pair with no whitespace) already keeps
false positives low; the scheme prefix requirement means bare `a:b@c` text
without `://` is still untouched. The pattern stays RE2 (no backtracking), so
the linear-time guarantee documented at the top of `redact.go` holds.

**Secondary — add a JSON-form credential rule** mirroring the query-param key
list, applied before the long-token fallback:

```go
jsonCredPattern = regexp.MustCompile(
    `(?i)("(?:apikey|api_?key|access_token|refresh_token|token|secret|password|auth|authorization|signature)"\s*:\s*")([^"]+)"`,
)
```

Group 1 (the key and opening quote) is preserved; the value is replaced with
`***` and the closing quote restored, keeping the JSON well-formed in the
redacted output. The key list intentionally matches `queryCredPattern`'s so the
two rules stay reviewable side by side.

Both changes must preserve the module's stated invariants: idempotence
(re-redacting redacted output is a no-op — the `***` value must not itself
match), rule ordering (specific rules before the long-token fallback), and the
existing fuzz targets extended to cover the new rules.

## 3. Scope & release plan

- **Module:** `gitlab.com/phpboyscout/go/redact` only. No API change — both
  fixes are pattern additions/widenings behind the existing `redact.String`.
- **Release:** conventional `fix(redact):` commit(s); releaser-pleaser opens
  the Release MR; merging cuts a patch release.
- **Downstream pickup:** GTB consumes redact directly (`pkg/telemetry`); a
  routine Renovate dep bump picks up the fix — no GTB code change needed.
  `go/transit` HTTP logging has its own header redaction and is unaffected.
- **Docs:** update `doc.go`'s rule inventory and GTB's
  `docs/components/redact.md` to list the scheme-agnostic userinfo rule and
  the JSON rule.

## 4. Acceptance criteria

- Connection-string redaction cases (table-driven, in `redact` module tests):
  `postgres://user:pass@host/db`, `redis://:pass@host:6379` (empty user),
  `amqp://guest:guest@mq:5672/`, `mongodb+srv://u:p@cluster.example` (scheme
  with `+`), embedded mid-sentence in a realistic dial-error string — all
  redact the password while preserving scheme and host.
- Negative cases: `https://host/path`, bare `user:pass@host` without `://`,
  `key=value` timestamps, and Windows paths (`C:\...`) remain untouched.
- JSON cases: `{"access_token":"tok"}`, `{"password": "x"}` (space after
  colon), mixed-case keys redact; `{"token_type":"bearer"}` and non-credential
  keys pass through.
- Idempotence: `redact.String(redact.String(s)) == redact.String(s)` holds for
  every new fixture; fuzz targets extended to assert it.
- Existing test suite and `depfootprint_test.go` unchanged and green (the
  module stays stdlib + regexp only).

## 5. Open questions

1. Should the JSON rule's key list include `client_secret` and
   `private_key`(PEM bodies excluded)? Recommendation: yes for
   `client_secret`; PEM handling is out of scope for this spec.
2. Is there any consumer relying on non-HTTP userinfo *surviving* redaction
   (e.g. displaying a full connection string back to the user)? None known in
   GTB — but worth a maintainer confirmation before release.
