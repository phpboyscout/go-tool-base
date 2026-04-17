---
title: "Browser — Safe URL Opening"
description: "pkg/browser provides the single validated entry point for opening URLs in the user's default browser or mail client. Enforces scheme allowlist, URL-length bound, and control-character rejection before invoking the OS URL handler."
date: 2026-04-17
tags: [component, security, browser, url]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Browser — Safe URL Opening

`pkg/browser` is the single entry point for opening URLs in the user's default browser or mail client. Every URL-opening code path in GTB — and in tools built on GTB — must route through it rather than invoking the OS handler directly (via `github.com/cli/browser.OpenURL`, `exec.Command("open"|"xdg-open"|"rundll32")`, or equivalent).

The package exists to guarantee four invariants that cannot be enforced after a URL has reached the OS:

1. **Scheme allowlist** — only `https`, `http`, `mailto`. Dangerous schemes (`file:`, `javascript:`, `data:`, `vbscript:`, custom protocol handlers) are rejected.
2. **Length bound** — `MaxURLLength = 8192` bytes, below the command-line length limit of every supported platform.
3. **Hygiene** — ASCII control characters (0x00–0x1F, 0x7F) and NUL bytes are rejected.
4. **No user logging** — the URL is never written to any log surface above DEBUG. Callers surfacing errors to users should reconstruct only the scheme or host from their own copy of `rawURL`.

## API

```go
import "github.com/phpboyscout/go-tool-base/pkg/browser"

err := browser.OpenURL(ctx, "https://example.com")
```

### Validation order

Fail-fast in this order:

1. Length ≤ `MaxURLLength`
2. No ASCII control characters (0x00–0x1F, 0x7F)
3. `net/url.Parse` succeeds
4. Scheme matches `AllowedSchemes` (case-insensitive per RFC 3986)
5. Context not cancelled

Each failure returns a typed sentinel:

| Failure | Error |
|---------|-------|
| Empty, too long, control chars, parse failure | [`ErrInvalidURL`](https://pkg.go.dev/github.com/phpboyscout/go-tool-base/pkg/browser#ErrInvalidURL) |
| Disallowed scheme | [`ErrDisallowedScheme`](https://pkg.go.dev/github.com/phpboyscout/go-tool-base/pkg/browser#ErrDisallowedScheme) |
| Context cancelled before opener invoked | `ctx.Err()` |
| OS URL handler failed | Wrapped underlying error |

Callers can distinguish via `errors.Is`.

## Options

`OpenURL` accepts zero or more `Option` values. The primary use is testing:

```go
err := browser.OpenURL(ctx, url, browser.WithOpener(func(raw string) error {
    // Record the URL for the test to verify.
    return nil
}))
```

`WithOpener(nil)` is a no-op — the default opener (`github.com/cli/browser.OpenURL`) is retained. When multiple `WithOpener` options are supplied, the last non-nil one wins.

`WithOpener` is also the extension point for tools that need a custom OS integration (e.g. a sandboxed browser on a kiosk device).

## `mailto:` and caller responsibility

`OpenURL` validates only the scheme and the URL's overall shape. It does not protect against `mailto:` header injection — an attacker-controlled subject or body containing `&cc=` or CR/LF sequences that, if not properly encoded, would add unintended recipients or headers to the resulting email.

**Callers constructing `mailto:` URLs from user-influenced data must `url.QueryEscape` every parameter value.** See `EmailDeletionRequestor` in `pkg/telemetry` for the canonical pattern, and the accompanying `TestEmailDeletionRequestor_CannotInjectHeaders` test for the caller-contract assertion.

## Why this package

Before this package, two call sites in the codebase opened URLs inconsistently:

- `pkg/telemetry/deletion.go` had a private `openURL` that shelled out to `open`/`xdg-open`/`rundll32` with no scheme validation.
- `pkg/cmd/docs/serve.go` delegated to `github.com/cli/browser.OpenURL`, also with no scheme validation.

The security audit (2026-04-02, M-5) flagged both paths. This package consolidates them behind a single validated entry point. The underlying OS invocation is still delegated to `github.com/cli/browser` — it has robust cross-platform coverage — but validation happens in GTB before the URL reaches it.

## Threat model

| Threat | Mitigation |
|--------|------------|
| Arbitrary scheme → arbitrary handler (`file:`, `javascript:`, etc.) | Scheme allowlist |
| Control characters confusing a platform URL handler | Control-char rejection |
| Oversize URL exceeding OS command-line limit | `MaxURLLength` check |
| Credential-bearing URL logged via error messages | Package never logs rawURL above DEBUG |
| `mailto:` header injection | Caller contract (documented + tested in callers) |
| Command injection via `exec.Command` | N/A — `exec.Command` in `cli/browser` does not invoke a shell |

## Fuzz testing

`FuzzOpenURL` feeds arbitrary bytes into `OpenURL` and asserts every outcome falls into one of the documented categories — accepted (opener invoked with exact URL), `ErrInvalidURL`, `ErrDisallowedScheme`, or a context-cancel error. Panics fail the fuzz. Run locally with:

```bash
go test -fuzz=FuzzOpenURL -fuzztime=60s ./pkg/browser/
```

## See also

- Spec: [`docs/development/specs/2026-04-02-url-scheme-validation.md`](../development/specs/2026-04-02-url-scheme-validation.md)
- Security audit: [`docs/development/reports/security-audit-2026-04-02.md`](../development/reports/security-audit-2026-04-02.md) (finding M-5)
