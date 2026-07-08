---
name: centralize-security-footguns
description: Route every value crossing a trust boundary through one hardened, tested helper — bounded regex compilation, a URL scheme allowlist, credential redaction, endpoint validation — instead of re-checking ad hoc at each call site. Use when handling untrusted patterns, URLs, secrets, or external endpoints.
---

# Centralise the security footguns

Use this when code handles something from outside the binary — a regex from
config, a URL to open, a string about to be logged, an endpoint to call. The
principle: **each trust-boundary check lives in exactly one hardened, tested
helper, and every call site routes through it.** Scattering ad-hoc `if`s at call
sites guarantees one of them is wrong, and the next person copies the wrong one.

These are small-surface, easy-to-get-subtly-wrong primitives — the exact class of
thing a foundation should own once so no tool re-implements (and re-breaks) it.
go-tool-base centralises four; the *pattern* is what transfers.

## 1. Bounded regex compilation (ReDoS)

A regex whose *pattern* comes from config, a flag, an HTTP payload, or TUI input
can hang the process at **compile** time. Don't hand it to `regexp.Compile`
directly. Route it through a bounded helper:

- `regexutil.CompileBounded(ctx, pattern)` — caps the pattern at **1024 bytes**
  and compilation at **100 ms** (or the ctx deadline, whichever is first),
  returning distinguishable errors (`ErrPatternTooLong`,
  `ErrPatternCompileTimeout`, `ErrPatternInvalid`).
- `CompileBoundedTimeout(pattern, timeout)` for non-ctx call sites — the
  effective timeout is `min(timeout, 100ms)`; the bound can be tightened, never
  widened.

Literal patterns known at build time keep `regexp.MustCompile` — they aren't a
trust boundary.

## 2. URL scheme allowlist (before handing a URL to the OS)

Opening a URL via the OS handler is a command-execution surface. Route every
open through one helper with a **fixed allowlist** — `browser.OpenURL(ctx, url)`
permits only `https`, `http`, `mailto`, after checking length (≤ 8192) and
rejecting ASCII control characters, and never logs the URL. No call site should
shell out to `open`/`xdg-open`/`rundll32` itself. (Constructing a `mailto:` from
user data? URL-escape every parameter value first.)

## 3. Credential redaction (before anything leaves the process)

Any free-form string headed for a log, telemetry, or a third-party observability
surface goes through one redactor first. `redact.String(s)` strips URL userinfo,
common credential query params, `Authorization` headers, JWTs, and well-known
provider token prefixes (`sk-`, `ghp_`, `AIza`, `AKIA`, Slack `xox…`), with a
fuzzy fallback for long opaque runs. Two properties make it safe to apply
liberally: it never panics, and it's **idempotent** (`String(String(s)) ==
String(s)`), so double-redaction is harmless.

## 4. Endpoint validation at the boundary (fail fast, before credentials fly)

When a tool can be pointed at an arbitrary endpoint (a provider `BaseURL`),
validate it at the one construction boundary, not deep in a request. GTB's
`chat.ValidateBaseURL` rejects non-HTTPS, any embedded userinfo
(`user:pass@host` — credentials belong in a separate field), over-length, control
chars, and placeholder hosts (`example.com` and subdomains) — and `chat.New`
calls it so a misconfiguration fails immediately rather than after the key hits
the wire.

## The transferable rule

When you add a new untrusted-input surface, ask: *is there one helper this must
route through?* If yes, use it. If no — and the input crosses a trust boundary —
**write that helper** (bounded, tested, with a fuzz test where the input is
adversarial), and make it the only path. The win isn't any single check; it's
that there's exactly one place to get each check right, audit, and harden.
