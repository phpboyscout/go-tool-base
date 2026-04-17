---
title: "Regex Hardening Against ReDoS"
description: "Harden every code path that compiles user- or config-supplied regex patterns against Regular Expression Denial of Service. Closes H-2 and H-3 from the 2026-04-17 audit by adding a length cap, a bounded-time compile helper, and applying both at call sites in pkg/vcs/bitbucket/release.go and pkg/docs/tui.go."
date: 2026-04-17
status: APPROVED
tags:
  - specification
  - security
  - regex
  - denial-of-service
audit-findings:
  - security-audit-2026-04-17.md#h-2
  - security-audit-2026-04-17.md#h-3
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude
    role: AI drafting assistant
---

# Regex Hardening Against ReDoS

Authors
:   Matt Cockayne, Claude *(AI drafting assistant)*

Date
:   17 April 2026

Status
:   APPROVED

---

## Overview

Two call sites in the codebase compile user- or config-supplied patterns via `regexp.Compile` without length or complexity bounds. A pathological input (`(a+)+b`, `(x|x)*z`, etc.) can drive the regex engine into worst-case behaviour, hanging the process.

Go's `regexp` uses RE2, which has guaranteed linear-time matching and is **not** vulnerable to classical catastrophic backtracking. Nonetheless:

- **Compilation time** is not guaranteed linear for pathological inputs — very large or deeply nested patterns can take measurable wall-clock time during `regexp.Compile`.
- **Match time**, while linear in the combined length of text and automaton, can still blow up when the automaton is enormous (millions of states) for a contrived pattern.
- **Resource cost** — memory for a large compiled pattern — is unbounded without a cap.

In short: Go is more resilient than PCRE-style engines, but "resilient" is not "immune". A 10 KiB pattern with heavy alternation and repetition can still make the tool unresponsive.

This spec addresses:

- **H-2** (`pkg/vcs/bitbucket/release.go:87, 114`) — `filename_pattern` from `ReleaseSourceConfig.Params` is compiled without bounds. Risk: a tool's config can DoS its own `update` flow. Higher risk if configs are ever loaded from less-trusted sources.
- **H-3** (`pkg/docs/tui.go:309, 451`) — the user's search query is compiled when regex mode is active. Risk: self-inflicted DoS in the TUI, plus a latent vector if an interactive tool ever accepts search queries from external inputs.

Both findings are addressed uniformly by a single new helper.

---

## Threat Model

| Vector | Impact |
|--------|--------|
| Config-supplied `filename_pattern` with pathological alternation | `update` flow hangs; automated update workflows stall indefinitely |
| User types a large or pathological pattern in the docs TUI | TUI becomes unresponsive until process is killed |
| Future code path adds a new `regexp.Compile(userInput)` without going through the helper | Reintroduces the same class of issue |

The third vector is the reason the fix is a **shared helper** rather than inline guards at each site. Centralising the discipline makes it auditable and reusable.

---

## Design Decisions

**New helper: `pkg/regexutil.CompileBounded`.** A thin wrapper around `regexp.Compile` that enforces a length cap, runs the compile in a bounded-time goroutine, and returns typed errors. Every user/config-supplied pattern in the codebase goes through this helper; calls to `regexp.Compile` with hard-coded literal patterns remain untouched.

**Length cap at `MaxPatternLength = 1024` bytes.** Legitimate filename patterns and search queries are short (tens of characters). 1 KiB is generous; 1 MiB would not be. A short cap also bounds compile time as a side effect.

**Compile timeout at 100 ms.** Normal compile time for a 1 KiB pattern is sub-millisecond. A pattern that takes longer than 100 ms to compile is either pathological or on hardware so constrained that the user has bigger problems. 100 ms is long enough to avoid spurious failures on slow CI runners and short enough to be imperceptible for legitimate inputs.

**Sentinel errors.** `ErrPatternTooLong`, `ErrPatternCompileTimeout`, `ErrPatternInvalid` — so callers can distinguish and the shared helper surfaces structured diagnostics.

**Package `pkg/regexutil` is new and public.** Downstream tools accepting patterns in their own config should use the same helper. Exposing it as a public package encourages this.

**No attempt to detect "dangerous" patterns statically.** Heuristics like "reject `(x+)+`" are brittle and produce false positives. The bounded-time compile is the authoritative test: if a pattern is fast to compile, we accept it; if not, we reject.

**Run the compile in a goroutine with `context.WithTimeout`.** `regexp.Compile` is not context-aware natively, so we cannot cancel it — the goroutine will run to completion. However, the calling code can return early from the timeout case and continue with the rest of its work while the stuck goroutine eventually terminates. For pathological inputs that never terminate within any reasonable time, the goroutine remains until process exit. This is acceptable because: (1) the caller gets their `ErrPatternCompileTimeout` error back promptly, (2) the process memory overhead is bounded (one goroutine, one stuck compile), (3) the number of distinct pathological patterns a single process sees is small. If future Go versions add context-aware regex compilation, we migrate to that.

---

## Public API Changes

### New package: `pkg/regexutil`

```go
package regexutil

import (
    "context"
    "regexp"

    "github.com/cockroachdb/errors"
)

// MaxPatternLength is the maximum accepted pattern length in bytes.
// Patterns longer than this are rejected without being compiled.
const MaxPatternLength = 1024

// DefaultCompileTimeout is the wall-clock timeout for CompileBounded.
// Patterns whose compile takes longer than this are rejected.
const DefaultCompileTimeout = 100 * time.Millisecond

// ErrPatternTooLong is returned when a pattern exceeds MaxPatternLength.
var ErrPatternTooLong = errors.New("regex pattern exceeds maximum length")

// ErrPatternCompileTimeout is returned when regex compilation does not
// complete within the configured timeout.
var ErrPatternCompileTimeout = errors.New("regex pattern compile timed out")

// ErrPatternInvalid is returned when regex compilation fails for
// reasons other than length or timeout (syntax errors).
var ErrPatternInvalid = errors.New("regex pattern is invalid")

// CompileBounded compiles pattern with ctx's deadline or the default
// timeout, whichever is shorter. It rejects patterns longer than
// MaxPatternLength. The returned error wraps one of the Err* sentinels
// so callers can distinguish the failure mode via errors.Is.
//
// Use this at every call site that compiles a user- or config-supplied
// pattern. Compiling literal patterns known at build time should
// continue to use regexp.MustCompile or regexp.Compile directly.
func CompileBounded(ctx context.Context, pattern string) (*regexp.Regexp, error)

// CompileBoundedTimeout is a convenience wrapper that applies a
// timeout via context.WithTimeout(context.Background(), timeout).
// Equivalent to CompileBounded with a fresh context.
func CompileBoundedTimeout(pattern string, timeout time.Duration) (*regexp.Regexp, error)
```

### Stability tier

`pkg/regexutil` enters at **Beta** tier per the API stability policy.

---

## Internal Implementation

### `pkg/regexutil/compile.go`

```go
func CompileBounded(ctx context.Context, pattern string) (*regexp.Regexp, error) {
    if len(pattern) > MaxPatternLength {
        return nil, errors.WithHintf(ErrPatternTooLong,
            "pattern has %d bytes; max is %d", len(pattern), MaxPatternLength)
    }

    ctx, cancel := context.WithTimeout(ctx, DefaultCompileTimeout)
    defer cancel()

    type result struct {
        re  *regexp.Regexp
        err error
    }
    done := make(chan result, 1)

    go func() {
        re, err := regexp.Compile(pattern)
        done <- result{re: re, err: err}
    }()

    select {
    case r := <-done:
        if r.err != nil {
            return nil, errors.Wrap(ErrPatternInvalid, r.err.Error())
        }
        return r.re, nil
    case <-ctx.Done():
        return nil, errors.WithHint(ErrPatternCompileTimeout,
            "The pattern is too complex to compile safely. Simplify it or use a different match strategy.")
    }
}
```

The goroutine leak on the timeout path is intentional (see Design Decisions). The `done` channel is buffered to size 1 so the goroutine does not block forever when it eventually returns after the caller has moved on.

### Call site updates

**`pkg/vcs/bitbucket/release.go`** (line 114):

```go
// Before:
re, err := regexp.Compile(patternStr)

// After:
re, err := regexutil.CompileBounded(ctx, patternStr)
```

The existing function already has access to a `ctx` (passed in from the caller's update flow). If not, construct one with `context.Background()` — but the preferred form is to use the caller's ctx so an outer timeout propagates.

**`pkg/docs/tui.go`** (lines 309, 451):

```go
// Before:
re, err := regexp.Compile("(?i)" + query)

// After:
re, err := regexutil.CompileBoundedTimeout("(?i)"+query, 100*time.Millisecond)
```

The TUI's Bubble Tea loop does not naturally carry a `context.Context` around, so `CompileBoundedTimeout` is cleaner here. A user whose pattern is rejected sees an inline TUI error (rendered in the status line) and can amend their query.

---

## Project Structure

| File | Action |
|------|--------|
| `pkg/regexutil/compile.go` | **New** — `CompileBounded`, `CompileBoundedTimeout`, constants, sentinels |
| `pkg/regexutil/compile_test.go` | **New** — unit tests covering length cap, timeout path, valid compile, ctx cancellation, ReDoS-ish patterns |
| `pkg/regexutil/compile_fuzz_test.go` | **New** — fuzz test that feeds random bytes and asserts the function never hangs beyond the timeout |
| `pkg/regexutil/doc.go` | **New** — package doc explaining threat model and when to use vs `regexp.Compile` |
| `pkg/vcs/bitbucket/release.go` | Modify — replace `regexp.Compile(patternStr)` with `regexutil.CompileBounded(ctx, patternStr)` |
| `pkg/vcs/bitbucket/release_test.go` | Modify — add tests that oversized / timeout patterns fail without hanging the test |
| `pkg/docs/tui.go` | Modify — replace the two `regexp.Compile` sites with `regexutil.CompileBoundedTimeout` |
| `pkg/docs/tui_test.go` | Modify — add test that a large query shows the error status line and does not hang |
| `docs/components/regexutil.md` | **New** — small reference doc |

---

## Error Handling

| Scenario | Error | User surface |
|----------|-------|--------------|
| Pattern longer than 1 KiB | `ErrPatternTooLong` wrapped with byte count | Bitbucket: update aborts with clear error. TUI: status-line message. |
| Pattern compile exceeds 100 ms | `ErrPatternCompileTimeout` with hint | Same behaviours. |
| Pattern has syntax error | `ErrPatternInvalid` wrapping the underlying `syntax.Error` | Same behaviours. |
| `ctx` cancelled before compile starts | `ctx.Err()` | Caller-initiated cancel path. |

---

## Non-Functional Requirements

### Testing & Quality Gates

| Requirement | Target |
|-------------|--------|
| Line coverage | ≥ 95 % for `pkg/regexutil/` (small package) |
| Branch coverage | 100 % — every control-flow branch hit |
| Race detector | `go test -race ./pkg/regexutil/...` passes |
| Fuzz testing | **Required**. `FuzzCompileBounded` runs ≥ 60 s in CI; corpus seeded with canonical patterns, known ReDoS-ish patterns, oversized inputs, UTF-8 edge cases |
| ReDoS regression tests | Table-driven test with 10+ known-pathological patterns asserts each returns an error (length or timeout) within 200 ms |
| Timing assertion | A benchmark/test asserts `CompileBoundedTimeout(<500-char normal pattern>, 100ms)` succeeds in under 10 ms on standard CI hardware |
| Leak containment | A test starts 100 concurrent timeouts and asserts the process's goroutine count returns to baseline within a reasonable window (goroutines that eventually complete will not leak indefinitely) |
| Call-site coverage | Grep test (or CI check) verifies no new `regexp.Compile` on user-input paths without going through `regexutil` |
| Golangci-lint | No new findings; no `//nolint` directives |

### Documentation Deliverables

| Artefact | Scope |
|----------|-------|
| `docs/components/regexutil.md` | Purpose, threat model summary, when to use vs `regexp.Compile`, API reference, patterns-to-avoid examples |
| Package doc comment on `pkg/regexutil/doc.go` | Top-of-file block explaining the design decisions (goroutine leak rationale, bounded timeout, no static analysis of patterns) |
| `docs/about/security.md` | Short subsection "Regex inputs" describing the class of issue and the mitigation |
| CLAUDE.md | One-line entry under Testing or Linting: "User-supplied regex patterns must use `pkg/regexutil.CompileBounded`, not `regexp.Compile`." |
| Lint rule | Optional — a `golangci-lint` custom rule or `gocritic` configuration that warns when `regexp.Compile` is called with a non-literal argument. Tracked as a nice-to-have in Future Considerations rather than required. |

### Observability

| Event | Level | Fields |
|-------|-------|--------|
| Compile rejected (length/timeout) | DEBUG | `kind` (`too_long`/`timeout`/`invalid`), `pattern_length`; **never the pattern itself** to avoid log amplification via attacker-controlled input |
| Compile succeeded | Not logged | Hot path; no value in logging success |
| Timeout-path goroutine still running at process exit | Not logged | Expected behaviour; not a leak worth alerting on |

**Redaction invariant**: the offending pattern is never logged at any level. Length and kind are sufficient for diagnosis; including the pattern would let an attacker fill logs with content of their choosing (a low-grade attack but avoidable at zero cost).

### Performance Bounds

| Metric | Bound |
|--------|-------|
| Normal-pattern compile wall-clock | ≤ 10 ms on typical hardware |
| Pathological-pattern rejection | ≤ 100 ms wall-clock (the timeout) |
| Memory per call | O(1) above the compile itself — one channel, one goroutine, one context |
| Concurrent calls | Unbounded; each call is independent with its own timeout |

### Security Invariants

1. Every user-/config-supplied regex in the codebase goes through `CompileBounded` or `CompileBoundedTimeout`.
2. Compile time is bounded by `DefaultCompileTimeout` regardless of input.
3. Pattern length is bounded by `MaxPatternLength` regardless of input.
4. Patterns are never logged above DEBUG; kind and length are the diagnostic surface.
5. Goroutine leaks from pathological inputs are bounded per process by the finite number of distinct malicious inputs the process sees.

---

## Migration & Compatibility

**Behaviour change**: callers submitting patterns longer than 1 KiB or that take longer than 100 ms to compile will now receive an error instead of hanging. This is a strict improvement.

**No API signature change**. The two call sites retain their outer function signatures; only the internal `regexp.Compile` call is swapped.

**API stability**: `pkg/regexutil` is new at Beta tier. No existing public symbols change.

---

## Implementation Phases

Single phase — small, focused change.

| Step | Description |
|------|-------------|
| 1 | Create `pkg/regexutil/` with `compile.go`, `doc.go`, `compile_test.go`, `compile_fuzz_test.go` |
| 2 | Swap `pkg/vcs/bitbucket/release.go` call site |
| 3 | Swap both `pkg/docs/tui.go` call sites |
| 4 | Add `docs/components/regexutil.md`; update `docs/about/security.md` and CLAUDE.md |
| 5 | CI run: `just test-race`, fuzz invocation, golangci-lint |

Estimated effort: **half a day**.

---

## Resolved Decisions

1. **Bounded-time compile over static pattern analysis** — any heuristic classifier is brittle and produces false positives. The bounded compile is the authoritative oracle.
2. **Shared helper in a new public package** rather than inlined guards, so the discipline is visible, reusable by downstream tools, and easy to audit.
3. **Accept goroutine leak on pathological inputs** — bounded by the finite attacker-input space in a single process; Go has no context-aware `regexp.Compile`. If a future Go version adds one, migrate.
4. **Length cap of 1 KiB** — generous for legitimate inputs, strict for malicious ones. Configurable per-call would invite misuse.
5. **Timeout at 100 ms** — two orders of magnitude above normal compile time; imperceptible for legitimate use.
6. **No logging of the pattern** — prevents attacker-controlled log content.

---

## Future Considerations

- Add a `golangci-lint` custom linter or `gocritic` rule to flag `regexp.Compile` / `regexp.MustCompile` calls with non-literal arguments. Would make the invariant enforceable at CI time.
- If Go adds a context-aware regex compile in a future version, replace the goroutine dance with the native API.
- Consider a TUI-specific helper that debounces compiles while the user is still typing, to avoid compiling partial patterns.
