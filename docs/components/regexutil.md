---
title: "Regexutil — Bounded Regex Compilation"
description: "pkg/regexutil provides DoS-safe wrappers around regexp.Compile for every call path that takes a user- or config-supplied pattern. Enforces a byte-length cap and a wall-clock compile timeout to mitigate Regular Expression Denial of Service."
date: 2026-04-17
tags: [component, security, regex, denial-of-service]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Regexutil — Bounded Regex Compilation

`pkg/regexutil` is the single entry point for compiling regex patterns that originate outside the binary — config files, CLI flags, TUI input, HTTP payloads, message queues. Every such call site in GTB — and in tools built on GTB — must route through it rather than calling `regexp.Compile` directly.

Literal patterns known at build time should keep using `regexp.MustCompile` — the helper is for *untrusted* input only.

## Threat Model

Go's regexp engine is RE2-based and does not suffer classical catastrophic backtracking at match time. Compilation, however, is not guaranteed linear: a pathological pattern (`(a+)+b`, deeply nested alternation, huge bounded repetitions) can take measurable wall-clock time inside `regexp.Compile` and allocate an automaton of many thousands of states. That is enough to hang a CLI's update flow or freeze an interactive TUI.

| Vector | Impact |
|--------|--------|
| Config-supplied regex with pathological alternation | Flow that compiles the pattern hangs; automated workflows stall |
| User types a large or pathological pattern in the docs TUI | TUI becomes unresponsive until process is killed |
| New `regexp.Compile(userInput)` added to the codebase without going through the helper | Reintroduces the same class of issue |

The helper addresses all three vectors with a single shared discipline.

## Invariants

Two defences are applied to every call:

1. **Byte-length cap** — patterns longer than `MaxPatternLength = 1024` bytes are rejected before any compile work begins.
2. **Wall-clock compile timeout** — `DefaultCompileTimeout = 100 ms` caps the worst case for anything that slips past the length cap.

A pattern is accepted only if both layers pass.

## API

```go
import "github.com/phpboyscout/go-tool-base/pkg/regexutil"

// When a context.Context is already in hand:
re, err := regexutil.CompileBounded(ctx, pattern)

// When one isn't (e.g. in a Bubble Tea TUI loop):
re, err := regexutil.CompileBoundedTimeout(pattern, regexutil.DefaultCompileTimeout)
```

### Error discrimination

Each rejection wraps one of three exported sentinels so callers can switch on kind via `errors.Is`:

| Failure | Error |
|---------|-------|
| Pattern length > `MaxPatternLength` | `ErrPatternTooLong` |
| Compile does not finish within the timeout | `ErrPatternCompileTimeout` |
| Pattern has a syntax error | `ErrPatternInvalid` |

```go
re, err := regexutil.CompileBounded(ctx, userPattern)
switch {
case errors.Is(err, regexutil.ErrPatternTooLong):
    return fmt.Errorf("pattern too long (max %d bytes)", regexutil.MaxPatternLength)
case errors.Is(err, regexutil.ErrPatternCompileTimeout):
    return fmt.Errorf("pattern too complex — simplify it")
case errors.Is(err, regexutil.ErrPatternInvalid):
    return fmt.Errorf("pattern has a syntax error: %w", err)
case err != nil:
    return err
}
```

## Logging Invariant

**Rejection errors never include the offending pattern.** Length and kind are sufficient for diagnosis; logging the pattern itself would let an attacker fill logs with content of their choosing — a low-grade attack but avoidable at zero cost.

Callers that want to surface the pattern to a trusted operator should do so from a context where log amplification is not a concern; the helper does not make that decision for you.

## Goroutine Leak Tradeoff

`regexp.Compile` is not context-aware, so the timeout path launches the compile in a goroutine and returns on timeout while that goroutine keeps running until the compile eventually finishes (or forever, for truly pathological inputs).

This is a **bounded leak** — acceptable because:

- The number of distinct pathological patterns a single process sees is small.
- The goroutine holds only a single compile's working set (one regex automaton).
- The caller gets their typed error back promptly and can continue.

If a future Go version exposes a context-aware regex compile, this package will migrate to the native API.

## Patterns to Avoid

These shapes are compile-bounded by the helper, but they are bad style in any codebase:

```regex
(a+)+b          # Nested quantifiers — RE2 handles them, but they obscure intent
(a|a)*b         # Overlapping alternation — always prefer `a*b`
(x|x|x)*y       # Duplicated alternatives — collapse to `x*y`
a{1,999}b{1,999}c{1,999}   # Very large bounded repetitions — cap at realistic sizes
```

Prefer explicit character classes and anchored matches over clever alternation.

## Call-Site Discipline

The helper exists to be **the** entry point for untrusted regex compilation. When adding a new code path:

- If the pattern is a build-time constant → `regexp.MustCompile` is fine.
- If the pattern originates from a config file, CLI flag, TUI input, HTTP payload, or message queue → `regexutil.CompileBounded` or `CompileBoundedTimeout`.

Failing to route through the helper reintroduces the ReDoS vector this package exists to close.
