---
title: "URL Scheme Validation Specification"
description: "Add URL scheme validation before passing URLs to system commands, preventing command injection via dangerous URI schemes."
date: 2026-04-02
status: DRAFT
tags:
  - specification
  - security
  - telemetry
  - url-validation
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# URL Scheme Validation Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   02 April 2026

Status
:   DRAFT

---

## Overview

A security audit identified that `pkg/telemetry/deletion.go` contains an `openURL()` function that passes URLs directly to platform-specific system commands (`open`, `xdg-open`, `rundll32`) without validating the URL scheme. While the immediate caller (`EmailDeletionRequestor.RequestDeletion`) constructs a well-formed `mailto:` URL using `url.QueryEscape()`, the `openURL()` function itself accepts any string. If reused elsewhere or if the input were ever influenced by external data, dangerous schemes such as `file://`, `javascript:`, or `data:` could be passed to the OS, leading to local file access, arbitrary command execution, or other unintended behaviour.

A separate call site in `pkg/cmd/docs/serve.go` uses the third-party `github.com/cli/browser` package, which also lacks scheme validation.

This specification proposes a small, reusable URL-opening utility in `pkg/browser/` with built-in scheme allowlisting, replacing both existing call sites.

### Problem

1. **No scheme validation**: `openURL()` in `pkg/telemetry/deletion.go` accepts any string and passes it to `exec.Command` without checking the URI scheme.
2. **Potential for misuse**: The function signature (`func openURL(ctx context.Context, rawURL string) error`) invites reuse without any guardrails.
3. **Inconsistent URL-opening patterns**: The codebase has two different mechanisms for opening URLs -- a hand-rolled `openURL()` in telemetry and the `github.com/cli/browser` package in the docs command. Neither validates schemes.
4. **No input sanitisation beyond scheme**: Control characters, null bytes, newlines, or extremely long URLs are passed through unchanged. `rundll32 url.dll,FileProtocolHandler` on Windows and some browser URL parsers can interpret such characters in surprising ways.
5. **`mailto:` header injection**: URL schemes like `mailto:` allow header injection (`?cc=attacker@evil.com&bcc=...`). Callers that construct `mailto:` URLs from user-influenced data can accidentally leak recipients unless every parameter is `url.QueryEscape`'d.

### Threat Model

| Threat | Vector | Impact |
|--------|--------|--------|
| Arbitrary scheme → arbitrary handler | `file:///etc/passwd`, `javascript:...`, `vbscript:...`, `data:...`, custom protocol | Local file read, arbitrary code execution via registered handlers |
| Control character injection | URL containing `\r`, `\n`, `\x00` | Argument parser confusion in platform shells/handlers, log forgery |
| Extreme URL length | Oversized URL (>32 KB) | OS command-line length limit exceeded (Windows ~32 KB, Linux ~128 KB); DoS or partial argument truncation |
| `mailto:` header injection | `mailto:target@x.com?cc=attacker@y.com&body=...` from unescaped caller input | Exfiltration of subject/body to third parties |
| Command injection via `exec.Command` args | URL containing shell metacharacters | NOT APPLICABLE — `exec.Command` does not invoke a shell, so args are passed directly to the child process |

The goal is defence-in-depth: even if a single threat is unreachable today, the library must not invite reintroduction through misuse.

### Goals

- Validate URL schemes against an explicit allowlist before invoking system commands.
- Provide a single reusable utility for opening URLs across the entire codebase.
- Reject dangerous schemes (`file://`, `javascript:`, `data:`, custom protocol handlers, etc.).
- Reject URLs containing control characters, null bytes, or exceeding a reasonable length.
- Keep the change minimal and focused on the security fix.

### Non-Goals

- Replacing the `github.com/cli/browser` dependency entirely (it may still be useful as a fallback implementation detail).
- URL validation beyond scheme and hygiene checks (e.g. hostname allowlisting, SSRF prevention).
- Supporting custom scheme allowlists at call sites (can be added later if needed).

---

## Design Decisions

**Allowlist over denylist**: Dangerous schemes are open-ended (custom protocol handlers, `vbscript:`, etc.), so an allowlist of known-safe schemes is the only robust approach. The allowed set is `https`, `http`, and `mailto`.

**New `pkg/browser/` package**: Rather than adding validation inline in telemetry, a dedicated package provides a single canonical entry point. This avoids duplicating validation logic and gives other packages a safe import path. The package name `browser` aligns with the existing third-party dependency name and clearly communicates its purpose.

**Wrap `github.com/cli/browser` internally**: The `cli/browser` package handles cross-platform URL opening reliably. Rather than maintaining our own `exec.Command` logic, delegate to it after validation. This removes the hand-rolled platform switching in `deletion.go`.

**Context-aware API**: The existing `openURL()` already accepts a `context.Context`. The new public API preserves this for cancellation support.

**Hygiene validation before scheme check**: URLs are rejected if they contain ASCII control characters (`0x00`–`0x1F`, `0x7F`), if they exceed `MaxURLLength` (8 KiB — well under OS command-line limits, generous for legitimate `mailto:` with long bodies), or if `url.Parse` rejects them. Hygiene checks run _before_ scheme lookup so malformed schemes do not sneak through via parser quirks.

**Scheme comparison uses `strings.EqualFold`**: RFC 3986 declares schemes case-insensitive. Lowercasing via `strings.ToLower` is equivalent for ASCII schemes, which are the only ones on the allowlist.

**Fixed allowlist, no call-site configuration**: Every known legitimate use is covered by `{http, https, mailto}`. A configurable allowlist is deferred until a concrete use case (`future-considerations`). Configurability invites misuse and a broadened attack surface for no current benefit.

**`mailto:` caller contract**: Callers constructing `mailto:` URLs must escape every parameter using `url.QueryEscape`, documented as the caller's responsibility. The browser package cannot transparently protect against header injection in URLs it receives as an opaque string.

---

## Public API Changes

### New: `pkg/browser`

```go
package browser

import "context"

// MaxURLLength is the maximum accepted length for a URL (bytes).
// Set conservatively below OS command-line limits (Windows: ~32 KB,
// Linux: ~128 KB ARG_MAX, macOS: ~256 KB) to protect all supported
// platforms with a single constant.
const MaxURLLength = 8192

// ErrDisallowedScheme is returned when a URL's scheme is not in the allowlist.
var ErrDisallowedScheme = errors.New("disallowed URL scheme")

// ErrInvalidURL is returned when a URL fails hygiene validation (parse
// failure, empty, too long, or contains control characters).
var ErrInvalidURL = errors.New("invalid URL")

// AllowedSchemes lists the URI schemes that OpenURL permits.
// Scheme comparison is case-insensitive per RFC 3986.
// The list is not configurable; extending it requires a code change and
// a security-review sign-off, documented in a follow-up spec.
var AllowedSchemes = []string{"https", "http", "mailto"}

// OpenURL validates that rawURL passes hygiene checks and uses an allowed
// scheme, then opens it in the user's default browser or mail client.
//
// Validation order (fail-fast):
//   1. Length ≤ MaxURLLength
//   2. No ASCII control characters (0x00–0x1F, 0x7F)
//   3. net/url.Parse succeeds
//   4. Scheme matches AllowedSchemes (case-insensitive)
//   5. Context not cancelled
//
// Returns ErrInvalidURL for hygiene failures, ErrDisallowedScheme for
// scheme failures, or a wrapped error from the underlying opener.
func OpenURL(ctx context.Context, rawURL string) error
```

### Stability Tier

`pkg/browser` is a new package. It enters at **Beta** stability tier per the API stability policy, meaning backward-compatible changes only after the initial release.

---

## Internal Implementation

### `pkg/browser/browser.go`

The implementation is straightforward:

1. Parse `rawURL` with `net/url.Parse`.
2. Normalise the scheme to lowercase.
3. Check the scheme against `AllowedSchemes`.
4. If disallowed, return `ErrDisallowedScheme` wrapped with the actual scheme for diagnostics.
5. If allowed, delegate to `github.com/cli/browser.OpenURL` (or the context-aware equivalent).

```go
func OpenURL(ctx context.Context, rawURL string) error {
    // 1. Length check (cheap, eliminates most malicious payloads).
    if len(rawURL) == 0 {
        return errors.WithHint(ErrInvalidURL, "URL is empty.")
    }
    if len(rawURL) > MaxURLLength {
        return errors.WithHintf(ErrInvalidURL,
            "URL exceeds maximum length of %d bytes.", MaxURLLength)
    }

    // 2. Reject ASCII control characters and null bytes. strings.ContainsFunc
    //    avoids a regex dependency and is constant-time in worst case.
    if strings.ContainsFunc(rawURL, func(r rune) bool {
        return r < 0x20 || r == 0x7F
    }) {
        return errors.WithHint(ErrInvalidURL,
            "URL contains control characters.")
    }

    // 3. Parse. Parser quirks are handled before the scheme check so a
    //    malformed scheme cannot slip past.
    parsed, err := url.Parse(rawURL)
    if err != nil {
        return errors.Wrap(ErrInvalidURL, "parsing URL")
    }

    // 4. Scheme allowlist. Case-insensitive per RFC 3986.
    if !isAllowedScheme(parsed.Scheme) {
        return errors.WithHintf(
            ErrDisallowedScheme,
            "scheme %q is not permitted; allowed: %v", parsed.Scheme, AllowedSchemes,
        )
    }

    // 5. Check context before invoking the opener. cli/browser.OpenURL is
    //    not context-aware; we cannot cancel once it returns.
    if err := ctx.Err(); err != nil {
        return err
    }

    return clibrowser.OpenURL(rawURL)
}

func isAllowedScheme(scheme string) bool {
    for _, s := range AllowedSchemes {
        if strings.EqualFold(scheme, s) {
            return true
        }
    }
    return false
}
```

### Migration of existing call sites

**`pkg/telemetry/deletion.go`**:
- Remove the private `openURL()` function entirely.
- Replace `openURL(ctx, mailto)` with `browser.OpenURL(ctx, mailto)`.

**`pkg/cmd/docs/serve.go`**:
- Replace `browser.OpenURL(url)` (from `github.com/cli/browser`) with `browser.OpenURL(cmd.Context(), url)` (from `pkg/browser`).
- The import alias changes from `github.com/cli/browser` to the new internal package.

After migration, if no other code imports `github.com/cli/browser` directly, it becomes a transitive dependency of `pkg/browser/` only.

---

## Project Structure

```
pkg/
  browser/
    browser.go          # OpenURL with scheme validation
    browser_test.go     # Unit tests
```

No new directories beyond the single package.

---

## Generator Impact

None. This change does not affect templates, scaffolded output, or the generator pipeline.

---

## Error Handling

| Condition | Error | User hint |
|-----------|-------|-----------|
| Empty URL | `ErrInvalidURL` | "URL is empty." |
| URL exceeds `MaxURLLength` | `ErrInvalidURL` | "URL exceeds maximum length of %d bytes." |
| URL contains control characters | `ErrInvalidURL` | "URL contains control characters." |
| URL cannot be parsed | `ErrInvalidURL` wrapping `url.Parse` error | "The URL could not be parsed. Check the URL format." |
| Disallowed scheme | `ErrDisallowedScheme` with detail | "URL scheme %q is not permitted. Allowed schemes: https, http, mailto." |
| System command fails | Wrapped error from `cli/browser` | "Failed to invoke the default URL handler." (platform-specific details logged at debug level only — never surface raw OS error messages to users, as they may leak path information) |
| Context cancelled | `ctx.Err()` | None (caller handles cancellation) |

All errors use `cockroachdb/errors` for wrapping. Error messages must never echo the URL back to the user at anything above DEBUG level — URLs can contain sensitive query parameters (tokens, IDs). Log the scheme only, not the full URL.

---

## Testing Strategy

### Unit Tests (`pkg/browser/browser_test.go`)

Table-driven tests covering:

| Test case | Input | Expected |
|-----------|-------|----------|
| Valid HTTPS URL | `https://example.com` | No error (mock system call) |
| Valid HTTP URL | `http://localhost:8080` | No error |
| Valid mailto URL | `mailto:user@example.com?subject=Test` | No error |
| Uppercase scheme | `HTTPS://example.com` | No error (normalised) |
| Mixed case scheme | `Https://example.com` | No error |
| file:// scheme | `file:///etc/passwd` | `ErrDisallowedScheme` |
| javascript: scheme | `javascript:alert(1)` | `ErrDisallowedScheme` |
| data: scheme | `data:text/html,<h1>hi</h1>` | `ErrDisallowedScheme` |
| vbscript: scheme | `vbscript:MsgBox("hi")` | `ErrDisallowedScheme` |
| Custom scheme | `myapp://callback` | `ErrDisallowedScheme` |
| Empty string | `""` | `ErrInvalidURL` |
| No scheme | `example.com` | `ErrDisallowedScheme` (empty scheme) |
| Unparseable URL | `://bad` | `ErrInvalidURL` |
| Cancelled context | Any valid URL | `context.Canceled` |
| URL with NUL byte | `"https://x.com/\x00"` | `ErrInvalidURL` |
| URL with CR/LF | `"https://x.com/\r\npath"` | `ErrInvalidURL` |
| URL with control char | `"https://x.com/\x1f"` | `ErrInvalidURL` |
| URL at length limit | `https://x.com/` + 8178×`a` | No error (= `MaxURLLength`) |
| URL over length limit | `https://x.com/` + 8179×`a` | `ErrInvalidURL` |
| Percent-encoded scheme | `h%74tps://example.com` | `ErrDisallowedScheme` (scheme = `h%74tps`) |
| Scheme-only URL | `https:` | No error (scheme matches) |
| Leading whitespace | `"  https://example.com"` | `ErrInvalidURL` if whitespace is control char; else parse check catches it |

The actual system command invocation must be tested via a function variable (`openerFunc`) to avoid opening real browsers in CI. Production code delegates to `clibrowser.OpenURL`; tests swap in a fake.

### Fuzz Test

`FuzzOpenURL` feeds random bytes into `OpenURL` and asserts that the function never panics and always returns one of `ErrInvalidURL`, `ErrDisallowedScheme`, a wrapped parse error, or — for valid URLs with allowed schemes — the mock opener's return value. Fuzzing catches edge cases in control character handling and scheme normalisation that table-driven tests may miss.

### Existing Tests

`pkg/telemetry/deletion_test.go` should continue to pass after the migration. Verify that `EmailDeletionRequestor` still constructs a valid `mailto:` URL.

---

## Migration & Compatibility

This is a **non-breaking change**. No public API is modified; a new package is added and internal call sites are updated.

- `pkg/telemetry/deletion.go`: The removed `openURL()` function is unexported, so no external consumers are affected.
- `pkg/cmd/docs/serve.go`: The import swap is internal to the command package.
- The `github.com/cli/browser` dependency remains in `go.mod` as it is used internally by the new package.

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 95 % for `pkg/browser/` (small package; high coverage is feasible) |
| Branch coverage | ≥ 90 % for validation paths |
| Race detector | `go test -race ./pkg/browser/...` must pass |
| Fuzz testing | `FuzzOpenURL` runs in CI for ≥ 60 s; corpus committed under `pkg/browser/testdata/fuzz/` |
| Golangci-lint | No new findings; no `//nolint` directives |
| BDD scenarios | Not required — this is an internal hardening change with no user-facing workflow |
| Mock injection | An `openerFunc` function variable (or equivalent DI) must be swappable from tests to avoid opening real browsers in CI |
| Regression test | The existing `pkg/telemetry/deletion_test.go` must continue to pass unchanged |

Testing details are in the [Testing Strategy](#testing-strategy) section above.

### Documentation Deliverables

The following artefacts must be produced or updated before the implementing PR is merged:

| Artefact | Scope |
|----------|-------|
| `docs/components/browser.md` | New. Package reference: purpose, `OpenURL` contract, allowlist rationale, `mailto:` caller contract, list of validation failure modes. |
| `docs/about/security.md` | Update. Add "Opening external URLs" subsection describing the allowlist, `MaxURLLength`, and control-character rejection. |
| Package doc comment (`pkg/browser/browser.go`) | New. A top-of-file comment that explains threat model in ~10 lines — so anyone grepping the package understands the guardrails without reading the spec. |
| Migration guide | **Not required.** No downstream consumers break. |
| CLAUDE.md | Update. Add a one-line reference under "Linting / Security" noting that URL-opening must go through `pkg/browser`, not `exec.Command` directly. |
| BDD / Gherkin | **Not required.** |

### Observability

| Event | Level | Fields (never the URL body) |
|-------|-------|----------------------------|
| URL accepted and opener invoked | DEBUG | `scheme`, `host` (only), `length` |
| Hygiene failure (length, control chars, parse) | DEBUG | `reason`, `length` |
| Scheme rejected | WARN | `scheme` (lowercased), `allowed_schemes` |
| Opener command failed | ERROR | Wrapped `cli/browser` error at DEBUG; user-facing message omits full URL |

**Redaction invariant**: the full URL is never logged above DEBUG. URLs can contain tokens, session identifiers, or personal data; the scheme and host are sufficient for incident diagnosis.

### Performance Bounds

| Metric | Bound | Notes |
|--------|-------|-------|
| Wall-clock validation | ≤ 1 ms per call for inputs ≤ `MaxURLLength` | Linear scan for control characters; `url.Parse` is the dominant cost |
| Memory | O(1) beyond the input URL | No buffer allocations in the hot path |
| Input size limit | `MaxURLLength = 8 KiB` | Protects all platforms with a single constant |
| Goroutine use | None | Synchronous call only |

### Security Invariants

Summarised from the [Threat Model](#threat-model) and [Resolved Decisions](#resolved-decisions):

1. Scheme allowlist is fixed at `{http, https, mailto}`.
2. Length cap of 8 KiB is enforced before any parse.
3. Control characters and NUL bytes are rejected.
4. `mailto:` header injection is the caller's responsibility, documented in the package doc and enforced by the `EmailDeletionRequestor` test.
5. URLs must never be logged above DEBUG level.
6. The allowlist is **not** user- or runtime-configurable; expanding it requires a code change, spec update, and security sign-off.

---

## Resolved Decisions

1. **`AllowedSchemes` is NOT configurable.** Every known legitimate use case is covered by `{http, https, mailto}`. Configurability invites misuse — a future library consumer could disable the check or expand the list without security review. If a new scheme is needed, it must be added to the allowlist in a change that goes through code review and updates this spec.

2. **`cli/browser` dependency is retained.** Wrapping it is simpler and leverages its maintained cross-platform coverage. The alternative (hand-rolled platform switching in our package) would duplicate well-tested code and create an ongoing maintenance burden. `cli/browser` is a well-known GitHub-maintained library, regularly updated, and uses `exec.Command` (no shell interpolation). The validation layer in `pkg/browser` provides the security guarantees; `cli/browser` handles the OS-specific invocation.

3. **`mailto:` header injection is the caller's responsibility.** The browser package cannot introspect arbitrary `mailto:` URLs to detect injection. Callers constructing `mailto:` URLs must `url.QueryEscape` every parameter. This is documented in the package comment and enforced via a test in `EmailDeletionRequestor` that supplies a crafted subject/body and asserts the final URL has no unescaped `?`, `&`, `\r`, or `\n` in parameter values.

4. **Windows `rundll32` is delegated to `cli/browser`.** The `cli/browser` library uses `exec.Command` with literal arguments (no shell), so command injection via the URL is not possible. `rundll32 url.dll,FileProtocolHandler <url>` interprets the URL via the registered protocol handler, which is the intended behaviour. Our scheme allowlist + hygiene validation prevents handing dangerous schemes to `rundll32`.

---

## Future Considerations

- **Hostname allowlisting**: For more sensitive contexts, a future enhancement could validate the hostname against an allowlist (e.g. only open URLs pointing to the tool's own domain).
- **Configurable schemes via `Option` pattern**: If other parts of the codebase need custom protocol handlers, the API could accept `WithScheme("custom")` options.
- **Telemetry consent URL**: The telemetry consent flow may also need to open URLs in the future; having `pkg/browser` ready avoids repeating the same pattern.

---

## Implementation Phases

### Phase 1: Core implementation (single phase)

This is a small, focused change that fits in a single implementation phase.

1. Create `pkg/browser/browser.go` with `OpenURL` and scheme validation.
2. Create `pkg/browser/browser_test.go` with full table-driven test coverage.
3. Migrate `pkg/telemetry/deletion.go` to use `browser.OpenURL`, remove `openURL()`.
4. Migrate `pkg/cmd/docs/serve.go` to use `browser.OpenURL`.
5. Run `just ci` to verify all tests pass.
6. Update `docs/components/` with a brief entry for the new package.

Estimated effort: small (1-2 hours).
