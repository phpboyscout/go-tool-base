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
