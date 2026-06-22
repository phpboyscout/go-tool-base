---
title: "config coverage hardening via huh accessible-mode form tests"
description: "Lift pkg/cmd/config unit coverage from a thin 90.6% to a comfortable ~93–94% with test-only changes. The two lowest-covered functions are interactive huh forms in migrate.go (resolveEnvVarName, instructAndVerifyEnvVar); rather than refactoring them to inject seams, drive the real forms headlessly through huh's built-in accessible mode (auto-enabled by TERM=dumb), feeding scripted answers via os.Stdin. Complemented by pure error-path tests for runEdit, persistUnset, applyPlan, and writeConfigAtomic. No production-code or public-API change."
status: IMPLEMENTED
date: 2026-06-22
tags:
  - specification
  - config
  - cli
  - testing
  - coverage
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
---

# config coverage hardening via huh accessible-mode form tests

Authors
:   Matt Cockayne

Date
:   22 June 2026

Status
:   IN PROGRESS

---

## Summary

`pkg/cmd/config` sits at **90.6%** unit coverage — only 0.6 points over the
advisory per-package threshold in `scripts/coverage-policy.sh` (the enforcement
half of [coverage-gap-closure](2026-06-20-coverage-gap-closure.md)). The thin
margin comes from two interactive `huh` forms in `migrate.go`:

- `resolveEnvVarName` (63.6%) — `huh` input form for the env var name.
- `instructAndVerifyEnvVar` (57.1%) — `huh` confirm form + env-var verification.

The policy measures **unit tests only** (`go test ./... -cover`, no `INT_TEST*`
gates); integration tests are skipped and Godog/E2E run the binary
out-of-process, so neither can attribute coverage to library packages. These
forms therefore need a way to run headlessly under plain `go test`.

**Key finding (validated by probe):** `huh` already provides this. Forms
auto-switch to **accessible mode** when `TERM=dumb` (`form.go:129`), and in that
mode each field runs `RunAccessible(w, r)` — plain line-based prompts reading
from `f.input` or, by default, **`os.Stdin`** (`form.go:677`). Setting
`TERM=dumb` and feeding `os.Stdin` therefore drives the **real** production
`.Run()` forms with **no production change**. A throwaway probe confirmed both
functions run to completion and return the scripted values this way.

This spec hardens `pkg/cmd/config` coverage to **~93–94%** entirely in test
code, using that mechanism plus a handful of pure error-path tests.

## Motivation

At a 0.6-point margin, a single new uncovered branch in a future `migrate.go`
edit re-trips the advisory `coverage-policy` gate. The durable fix is to make
the genuinely-interactive code testable. We considered refactoring the two
functions to inject form seams (the established `WithEditorRunner` / `WithForm`
pattern used by `config edit`, `setup/bitbucket`, `setup/ai`), but the probe
showed the refactor is **unnecessary**: huh's accessibility feature already
exposes a headless path, and driving the real forms is higher-fidelity than
stubbing them. We therefore keep production code untouched.

## Design

### Approach: accessible-mode form driving (test-only)

A single test helper encapsulates the global-state juggling:

```go
// withScriptedStdin runs fn with huh in accessible mode (TERM=dumb) and os.Stdin
// fed from script, restoring both afterwards. Tests using it must NOT call
// t.Parallel() — they mutate process-global os.Stdin/os.Stdout and TERM.
func withScriptedStdin(t *testing.T, script string, fn func()) {
    t.Helper()
    t.Setenv("TERM", "dumb")              // huh → accessible mode

    r, w, err := os.Pipe()
    require.NoError(t, err)
    origIn, origOut := os.Stdin, os.Stdout
    os.Stdin = r
    devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
    os.Stdout = devnull
    t.Cleanup(func() { os.Stdin, os.Stdout = origIn, origOut; _ = devnull.Close() })

    go func() { _, _ = io.WriteString(w, script); _ = w.Close() }()
    fn()
}
```

Field input formats in accessible mode: `Input` reads one line (the value);
`Confirm` reads `y`/`n` via `accessibility.PromptBool`. So a name prompt is fed
`"MY_VAR\n"`, a confirm `"y\n"` or `"n\n"`.

### Why serial (no `t.Parallel`)

The helper swaps process-global `os.Stdin`/`os.Stdout` and `TERM`. `t.Setenv`
already forbids `t.Parallel`, which is the safety interlock: these tests run in
`go test`'s sequential phase, during which the package's parallel tests are
paused, so there is no concurrent `os.Stdin` reader. Restoration happens via
`t.Cleanup`. This is a contained, test-only use of global state — not a
production mocking hook — so it does not conflict with the project's
no-package-level-hooks rule (which targets `t.Parallel`-racing production seams).

### Test matrix

`resolveEnvVarName` (combined with existing override / AssumeYes / DryRun tests
→ ~100%):

- interactive form, scripted name → returns that name.
- interactive form, scripted name failing `ValidateEnvVarName` then a valid one
  (optional; the validator re-prompts) — or simply assert the happy path, since
  the validation loop is huh's, not ours.

`instructAndVerifyEnvVar` (→ ~100%):

- confirm `y` with the env var exported (`t.Setenv`) → `nil` (proceeds).
- confirm `y` with the env var **unset** → mismatch error + documented hint.
- confirm `n` → "migration aborted by user".
- dual-credential (Bitbucket) candidate → exercises the partner export line.

### Part B — pure error-path tests (also test-only)

| Function | Uncovered branch | How to hit it |
|----------|------------------|---------------|
| `runEdit` | shlex parse failure | `--editor "'unbalanced"` |
| `runEdit` | read-edited failure | injected runner deletes the temp file |
| `persistUnset` / `applyPlan` | reload failure after write | bound viper whose file is removed pre-reload |
| `writeConfigAtomic` | rename failure | afero wrapper allowing write, failing `Rename` |

These reuse the existing `WithEditorRunner` seam and the internal `fileBoundProps`
helper; no new production code.

## Testing strategy

All additions are `package config` test files. The accessible-mode tests are
serial (no `t.Parallel`); the error-path tests are parallel and use memmap
`afero.Fs` + `logger.NewNoop()`. Acceptance:

- `go test -cover ./pkg/cmd/config` ≥ 93%.
- `scripts/coverage-policy.sh` reports OK (gate green).
- `just lint` clean, `go test -race ./pkg/cmd/config` clean.

## Decision log

- **Accessible mode over an injectable seam.** The probe proved huh's
  `TERM=dumb` accessible path drives the real forms headlessly. That avoids any
  production change and tests genuine form behaviour (rendering, validation,
  parsing) rather than a stub. The seam refactor is therefore rejected as
  unnecessary complexity.
- **Serial tests accepted.** The `os.Stdin` swap forces these few tests to run
  serially; that is an acceptable, contained cost versus changing production
  code. `t.Setenv` enforces the no-parallel constraint automatically.
- **No `.coverage-policy.yaml` exclusion.** The code is testable, so we test it
  rather than exclude it.

## Out of scope

- Any change to migration behaviour, flags, output, or the public `Migrate` API
  (this spec adds **no** production code).
- The Windows-only `resolveEditor` `notepad` branch (unreachable on Linux CI).
- Broadening the coverage policy to include integration/E2E runs.

## Resolved questions

1. **Refactor vs. accessible mode** → accessible mode (probe-validated; no
   production change).
2. **One MR or two** → one MR; it is entirely test-only, so there is no
   production risk to isolate.
