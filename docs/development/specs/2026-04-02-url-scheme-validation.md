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

### Goals

- Validate URL schemes against an explicit allowlist before invoking system commands.
- Provide a single reusable utility for opening URLs across the entire codebase.
- Reject dangerous schemes (`file://`, `javascript:`, `data:`, custom protocol handlers, etc.).
- Keep the change minimal and focused on the security fix.

### Non-Goals

- Replacing the `github.com/cli/browser` dependency entirely (it may still be useful as a fallback implementation detail).
- URL validation beyond scheme checks (e.g. hostname allowlisting, SSRF prevention).
- Supporting custom scheme allowlists at call sites (can be added later if needed).

---

## Design Decisions

**Allowlist over denylist**: Dangerous schemes are open-ended (custom protocol handlers, `vbscript:`, etc.), so an allowlist of known-safe schemes is the only robust approach. The allowed set is `https`, `http`, and `mailto`.

**New `pkg/browser/` package**: Rather than adding validation inline in telemetry, a dedicated package provides a single canonical entry point. This avoids duplicating validation logic and gives other packages a safe import path. The package name `browser` aligns with the existing third-party dependency name and clearly communicates its purpose.

**Wrap `github.com/cli/browser` internally**: The `cli/browser` package handles cross-platform URL opening reliably. Rather than maintaining our own `exec.Command` logic, delegate to it after validation. This removes the hand-rolled platform switching in `deletion.go`.

**Context-aware API**: The existing `openURL()` already accepts a `context.Context`. The new public API preserves this for cancellation support.

---

## Public API Changes

### New: `pkg/browser`

```go
package browser

import "context"

// ErrDisallowedScheme is returned when a URL's scheme is not in the allowlist.
var ErrDisallowedScheme = errors.New("disallowed URL scheme")

// AllowedSchemes lists the URI schemes that OpenURL permits.
// Currently: "https", "http", "mailto".
var AllowedSchemes = []string{"https", "http", "mailto"}

// OpenURL validates that rawURL uses an allowed scheme, then opens it
// in the user's default browser or mail client.
//
// Returns ErrDisallowedScheme if the scheme is not in AllowedSchemes.
// Returns an error if the URL cannot be parsed or the system command fails.
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
    parsed, err := url.Parse(rawURL)
    if err != nil {
        return errors.Wrap(err, "parsing URL")
    }

    scheme := strings.ToLower(parsed.Scheme)
    if !isAllowedScheme(scheme) {
        return errors.WithDetailf(
            ErrDisallowedScheme,
            "scheme %q is not permitted; allowed: %v", scheme, AllowedSchemes,
        )
    }

    // Delegate to cli/browser for cross-platform opening.
    // Note: cli/browser.OpenURL is not context-aware, so we check
    // context cancellation before invoking.
    if err := ctx.Err(); err != nil {
        return err
    }

    return clibrowser.OpenURL(rawURL)
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
| URL cannot be parsed | Wrapped `url.Parse` error | "The URL could not be parsed. Check the URL format." |
| Disallowed scheme | `ErrDisallowedScheme` with detail | "URL scheme %q is not permitted. Allowed schemes: https, http, mailto." |
| System command fails | Wrapped error from `cli/browser` | None (platform-specific; logged at debug level) |
| Context cancelled | `ctx.Err()` | None (caller handles cancellation) |

All errors use `cockroachdb/errors` for wrapping and detail attachment.

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
| Empty string | `""` | `ErrDisallowedScheme` (empty scheme) |
| No scheme | `example.com` | `ErrDisallowedScheme` (empty scheme) |
| Unparseable URL | `://bad` | Parse error |
| Cancelled context | Any valid URL | `context.Canceled` |

The actual system command invocation should be tested via an interface or function variable to avoid opening real browsers in CI.

### Existing Tests

`pkg/telemetry/deletion_test.go` should continue to pass after the migration. Verify that `EmailDeletionRequestor` still constructs a valid `mailto:` URL.

---

## Migration & Compatibility

This is a **non-breaking change**. No public API is modified; a new package is added and internal call sites are updated.

- `pkg/telemetry/deletion.go`: The removed `openURL()` function is unexported, so no external consumers are affected.
- `pkg/cmd/docs/serve.go`: The import swap is internal to the command package.
- The `github.com/cli/browser` dependency remains in `go.mod` as it is used internally by the new package.

---

## Open Questions

1. **Should `AllowedSchemes` be configurable?** The current design uses a fixed allowlist. A future iteration could accept `Option` functions to add schemes, but this adds complexity for no current need.
2. **Should the `cli/browser` dependency be replaced entirely?** It would remove one dependency but requires maintaining platform-specific `exec.Command` logic. Wrapping it is simpler and leverages its existing cross-platform coverage.

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
