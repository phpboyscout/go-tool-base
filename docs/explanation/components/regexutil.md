---
title: "Regexutil — Bounded Regex Compilation"
description: "go-tool-base consumes the standalone go/regexutil module for DoS-safe compilation of user- or config-supplied regex patterns."
date: 2026-07-13
tags: [component, security, regex, denial-of-service]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Regexutil — Bounded Regex Compilation

The bounded regex-compilation helper has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/regexutil`](https://gitlab.com/phpboyscout/go/regexutil)
module**. Its full documentation — the `CompileBounded`/`CompileBoundedTimeout`
API, the length cap and compile timeout, and the ReDoS threat model — now lives
at:

> **[regexutil.go.phpboyscout.uk](https://regexutil.go.phpboyscout.uk)**

`regexutil` is framework-free, so go-tool-base consumes it **directly** (no
adapter): callers import `gitlab.com/phpboyscout/go/regexutil` and use
`CompileBounded` / `CompileBoundedTimeout` as before. See the
[migration note](../../reference/migration/v0.x-regexutil-extracted.md) for the
import-path change.

## How go-tool-base uses it

Any `regexp.Compile` call whose pattern originates outside the binary (config
file, CLI flag, TUI input, HTTP payload) routes through
`regexutil.CompileBounded` / `CompileBoundedTimeout` — e.g. the docs TUI search
and the Bitbucket release-matching. Literal patterns known at build time continue
to use `regexp.MustCompile`.

## What "ReDoS" actually means here (threat model)

The naïve ReDoS worry is catastrophic backtracking at **match time** — but Go's
regexp engine is RE2-based and does not backtrack, so that class of attack
simply does not apply. The real risk the helper guards is **compile-time
blow-up**. `regexp.Compile` is not guaranteed linear: a pathological pattern
(`(a+)+b`, deeply nested alternation, or just very long repetition chains) can
burn measurable wall-clock time and allocate an automaton of many thousands of
states. That is enough to **hang a CLI's update flow or freeze an interactive
TUI** — a denial of service driven entirely by an attacker-supplied pattern
reaching `Compile`. The helper applies two defences uniformly: a byte-length cap
that rejects oversize patterns before any compile work begins, and a wall-clock
timeout on the compile itself that bounds the worst case for anything slipping
past the cap.

The timeout carries a deliberate **bounded goroutine-leak tradeoff**.
`regexp.Compile` is not context-aware, so `CompileBoundedTimeout` runs the
compile in a goroutine and returns on timeout while that goroutine keeps running
until the compile finishes (or forever, for truly pathological input). This is
accepted because the leak is bounded: a single process ever sees only a handful
of distinct pathological patterns, each goroutine holds just one compile's
working set, and the caller gets its error immediately and can proceed. If a
future Go release exposes a context-aware compile, migrate to it.

Finally, **never log the offending pattern** on rejection — surface only its
length and the rejection kind. Echoing the pattern into logs would hand an
attacker a log-amplification primitive, letting them fill the operator's logs
with content of their choosing. Only surface a pattern from a trusted-source
context where log amplification is not a concern.

## Related

- **Module docs:** [regexutil.go.phpboyscout.uk](https://regexutil.go.phpboyscout.uk)
- **Trust model / spec:** [`0061-regex-hardening`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0061-regex-hardening)
