---
title: "Extract pkg/errorhandling into a standalone go/errorhandling module"
description: "Extract the structured error-reporting layer (hint/detail/stack-trace formatting over cockroachdb/errors, exit-code attachment, log-level routing, and pluggable help channels) out of go-tool-base into gitlab.com/phpboyscout/go/errorhandling. The package has zero go-tool-base coupling; its only framework dependency is a vestigial variadic *cobra.Command parameter on Check, which no production call site uses and which duplicates the existing SetUsage seam. Removing that parameter eliminates the Cobra coupling entirely, so the module ships as one framework-free package with no companion module and no split."
date: 2026-07-18
status: APPROVED
tags:
  - specification
  - errorhandling
  - errors
  - module-extraction
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# Extract `pkg/errorhandling` into a standalone `go/errorhandling` module

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   2026-07-18

Status
:   APPROVED (2026-07-18 — all open questions resolved with user)

Related
:   [module-extraction playbook](2026-07-12-go-module-extraction-playbook.md) (§5 procedure, §10/§11 findings),
    [config extraction](2026-07-18-config-module-extraction.md) (IMPLEMENTED — the immediately-prior clean-break repoint and the published-mocks precedent),
    [credentials extraction](2026-07-18-credentials-module-extraction.md) (IMPLEMENTED),
    [extraction report](../reports/2026-07-07-package-extraction-report.md)

---

## Summary

`pkg/errorhandling` turns an error into useful user-facing output. It wraps
[`cockroachdb/errors`](https://github.com/cockroachdb/errors) with: user hints
(`WithUserHint`/`WrapWithHint`), process exit codes attached to an error
(`WithExitCode`/`ExitCode`), log-level routing (`Fatal`/`Error`/`Warn`, including a
quiet-fatal for expected terminations such as SIGINT), debug-gated stack traces and
details, assertion-failure handling, and a pluggable **help-channel** seam
(`HelpConfig`) that appends "contact X for assistance" to reported errors.

This spec extracts it to `gitlab.com/phpboyscout/go/errorhandling` following the
[playbook](2026-07-12-go-module-extraction-playbook.md). Production code imports
**zero** go-tool-base packages and the logging seam is already a `*slog.Logger`. It is
a small, clean, clean-break repoint — the smallest extraction since the Phase-1 leaves.

Two design questions shape the module's surface, and both are the substance of this
spec (§Seams): how to decouple Cobra — which has a better answer than the extraction
report anticipated — and which side of the boundary the help-channel implementations
belong on.

## Motivation

- **Completes the `go/releases` prerequisite set.** `pkg/vcs` and the release stack
  report errors through this package; with `credentials` and `config` already
  extracted, `errorhandling` is the last prerequisite before the VCS/release tree can
  be extracted cleanly.
- **Genuinely reusable.** Hint-carrying errors, exit codes attached to an error value,
  and debug-gated stack traces are useful to any Go CLI, independent of GTB.
- **Small and low-risk.** ~405 LOC of production code, 7 consumer packages, no
  decoupling work beyond removing one vestigial parameter.

## Target module

- **Module path:** `gitlab.com/phpboyscout/go/errorhandling`.
- **Package name:** stays `errorhandling`.
- **Docs microsite:** `errorhandling.go.phpboyscout.uk`.
- **Local dir:** `~/workspace/phpboyscout/go/errorhandling`.

## Current structure & coupling

Production source ~405 LOC (1279 incl. tests):

| File | LOC | Role |
|---|---|---|
| `handling.go` | 213 | `ErrorHandler` interface, `StandardErrorHandler`, level routing, special-error handling |
| `helpers.go` | 74 | `WithUserHint(f)`, `WrapWithHint`, `NewAssertionFailure`, stack-trace extraction |
| `exitcode.go` | 46 | `WithExitCode` / `ExitCode` |
| `help.go` | 38 | `HelpConfig` interface + `SlackHelp` / `TeamsHelp` — **the interface travels, the two implementations stay in GTB (R4)** |
| `options.go` | 22 | `Option`, `WithExitFunc`, writer override |
| `doc.go` | 12 | package doc |

**External dependencies:** `github.com/cockroachdb/errors` — and, today,
`github.com/spf13/cobra` (see §Seams). `getsentry/sentry-go` appears in the graph as a
pre-existing transitive of `cockroachdb/errors`; it is already present in every sibling
module (`credentials`, `config`, `controls`) and is **not** a new concern.

**Coupling:** production code imports **zero** go-tool-base packages (verified). The
logger seam is already `*slog.Logger`.

### Consumers to repoint

7 non-test packages — `pkg/props` (+`propstest`), `pkg/cmd/root`, `internal/cmd/root`,
`internal/generator/templates`, `cmd/e2e`, `mocks/pkg/props` — for **24 files** total
including tests. `pkg/props` carries `ErrorHandler` as a DI field, which is why the
mocks under `mocks/pkg/props` reference it.

## Seams and decoupling

### The Cobra dependency is vestigial — remove it (R1)

`cobra` appears in exactly three places, all in `handling.go`, and serves exactly one
purpose: printing usage when a parent command is invoked without a subcommand
(`ErrRunSubCommand`).

```go
Check(err error, prefix string, level string, cmd ...*cobra.Command)   // the interface
// …
if len(cmd) > 0 && cmd[0] != nil {
    cmd[0].SetOut(h.Writer)
    _ = cmd[0].Usage()
} else if h.Usage != nil {        // a Cobra-free path already exists
    _ = h.Usage()
}
```

Three findings make removal the obvious answer:

1. **A Cobra-free seam already exists and is already the preferred one.**
   `SetUsage(usage func() error)` is on the `ErrorHandler` interface.
2. **The generator — the canonical pattern for downstream tools — already uses it.**
   `internal/generator/templates/command.go` emits
   `props.ErrorHandler.SetUsage(cmd.Usage)` inside each command's **`PreRunE`**, so the
   stored usage func is re-pointed at the *currently executing* command on every
   invocation. The natural objection to a single stored func — *"won't it print the
   root command's usage instead of the failing subcommand's?"* — therefore does not
   apply.
3. **No production call site passes the variadic.** GTB's only `Check` call sites
   (`pkg/cmd/root/execute.go`) pass no command; the variadic is exercised solely by
   `pkg/errorhandling`'s own tests and a mock's method signature.

**Decision: drop the variadic.** The interface becomes:

```go
Check(err error, prefix string, level string)
```

With that one change the Cobra import disappears and the module is framework-free.

**Consequence for the extraction report's plan.** The report proposed "split Cobra
integration from pure hint/exit-code helpers", implying a core module plus an
`errorhandling-cobra` companion. That split is **unnecessary**: once the vestigial
parameter is gone there is no Cobra integration left to separate. The module ships as
**one package, no companion, no version-compatibility matrix.**

**Behavioural note.** The variadic path also called `cmd[0].SetOut(h.Writer)` before
printing usage, redirecting Cobra's usage output to the handler's writer. The
`SetUsage(cmd.Usage)` path — the one the generator emits and therefore the one in
actual use — never did this, so nothing real changes. A caller who wants the redirect
supplies it in the closure:

```go
props.ErrorHandler.SetUsage(func() error { cmd.SetOut(w); return cmd.Usage() })
```

**Rejected alternative — a structural `UsagePrinter` interface.** The variadic could
have been retyped as `cmd ...UsagePrinter` where
`type UsagePrinter interface { SetOut(io.Writer); Usage() error }`; `*cobra.Command`
satisfies that structurally (both signatures match exactly), so existing callers would
compile unchanged — the same trick as `config`'s `SettingsSource`. Rejected because it
preserves a variadic that **nothing uses** and that duplicates `SetUsage`; keeping it
would carry a vestigial API into a brand-new module's v0.1.0 purely to avoid churn that
does not exist. GTB is pre-1.0 and the clean break is cheap.

### Help channels: the interface travels, the implementations stay (R4)

`HelpConfig` is already an interface — `interface { SupportMessage() string }` — and the
handler consumes nothing else from it (`if h.Help != nil { h.Help.SupportMessage() }`).
The concrete `SlackHelp` and `TeamsHelp` structs beside it are opinionated,
vendor-flavoured formatters carrying `json`/`yaml` tags for a scaffolded tool's manifest.

**Decision: the module keeps the `HelpConfig` interface; the concrete `SlackHelp` /
`TeamsHelp` implementations move to GTB.** The module ships the extension point and no
opinion about *where* a team's support channel lives — exactly the shape the decision log
already established for credentials ("`Backend` is the extension point; vendor adapters
are the tool author's concern").

Nothing in GTB argues against the move: `props.Tool.Help` is already typed as the
**interface**, and the only consumers of the concrete types are the generator — it emits
`SlackHelp{...}`/`TeamsHelp{...}` into a scaffolded `root.go` (driven by
`--help-type slack|teams`) and reads them back for manifest round-tripping.

**Placement in GTB: `pkg/props`.** The scaffolded `root.go` already imports `props` to
build its `props.Tool{…}`, so the generated code gains **no new import** — the emitted
literal simply becomes `props.SlackHelp{...}`. (A dedicated `pkg/help` package was
considered — conceptually tidier than putting formatters in the DI package — but it adds
an import to every scaffold for ~30 LOC of string formatting.) The generator's emitter
(`templates/skeleton_root.go`) and its manifest reader
(`ast_extract_properties.go`) both update to the new qualifier.

No other decoupling is required.

## Migration procedure

Follows playbook §5 in the clean-repoint shape:

1. **Bootstrap** `phpboyscout/go/errorhandling` to playbook §3 (cicd v0.23.0,
   `.golangci.yaml` local prefix, `depfootprint_test.go`, `requirements-lock.txt`,
   `enable_e2e: false`, README header). No goreleaser.
2. **Move code** — the six production files; delete the `cmd ...*cobra.Command`
   parameter from the interface and implementation, and drop the `SetOut` call.
3. **Tests + guards** — carry the tests, swapping `pkg/logger` for the stdlib slog seam
   (`slog.New(slog.DiscardHandler)`, and a text handler where output is asserted) per
   playbook §11.3, and vendoring an integration-gate helper for
   `propagation_integration_test.go` per §11.2. Update the Cobra-passing test cases to
   use `SetUsage`. Keep **≥90% coverage**. Add the depfootprint guard forbidding
   go-tool-base, **Cobra**, Viper, TUI, OTel, and cloud SDKs.
4. **Mocks (R2)** — ship `ErrorHandler` and `HelpConfig` mocks in a published
   **`mocks`** subpackage (`gitlab.com/phpboyscout/go/errorhandling/mocks`); GTB deletes
   `mocks/pkg/errorhandling` and consumes the module's.
5. **Docs (Diátaxis) + Pages** — `errorhandling.go.phpboyscout.uk`: getting-started;
   how-tos (add a user hint, attach an exit code, route by level, wire a help channel,
   print usage via `SetUsage`, test with the mock); explanation (the error-reporting
   model, debug-gated output, and why the module has no CLI-framework dependency).
6. **Cut `v0.1.0`** via the Release-MR flow (human-gated).
7. **GTB cut-over (single MR):** add the dependency; delete `pkg/errorhandling` and
   `mocks/pkg/errorhandling`; repoint the 24 files; update the generator templates;
   run `just ci`; add `docs/reference/migration/v0.x-errorhandling-extracted.md`
   documenting the `Check` signature change.
8. **GTB docs cross-reference** the microsite (§4.1) and sweep per §11.6.

## Test redistribution

Five of the six test files import `pkg/logger` (mechanical slog swap);
`propagation_integration_test.go` additionally uses `internal/testutil` for the
integration gate, which is vendored into the module as a small local helper. Two test
cases pass a `*cobra.Command` to `Check` and are rewritten against `SetUsage` — they are
testing the usage-printing behaviour, which survives, not the parameter. Build the
`Test*`/`Example*` inventory and diff it after the move (§10.3).

## Acceptance criteria

- `go/errorhandling` builds under the new path; **no Cobra** anywhere in the graph;
  depfootprint guard green; coverage ≥90%.
- Docs live at `errorhandling.go.phpboyscout.uk` with a working cert.
- GTB builds and `just ci` is green consuming `go/errorhandling@v0.1.0`;
  `pkg/errorhandling` and `mocks/pkg/errorhandling` deleted; scaffolded projects
  (`gtb generate`) build and still print usage correctly for a parent command.
- Migration note documents the `Check` signature change; component docs point at the
  microsite.

## Resolutions (confirmed with user 2026-07-18)

- **R1 — drop the vestigial `cmd ...*cobra.Command` variadic.** Cobra coupling is
  removed entirely rather than abstracted; `SetUsage` is the single usage seam. The
  structural-interface alternative was considered and rejected (see §Seams). The
  report's "split Cobra integration" plan is superseded: **one module, no companion.**
- **R2 — publish the mocks with the module** (`ErrorHandler`, `HelpConfig`), consumed by
  GTB's tests, following the `go/config` precedent. The subpackage is named **`mocks`**,
  not `errorhandlingmock`: `errorhandling/errorhandlingmock` stutters, and the parent
  path already supplies the context. The generic identifier means a file importing two
  modules' mocks must alias one — which every GTB mock import already does, so it costs
  nothing. **This decision is back-ported to `go/config`** (`configmock` → `mocks`),
  making `<module>/mocks` the toolkit-wide convention.
- **R4 — `HelpConfig` interface travels; `SlackHelp`/`TeamsHelp` stay in GTB.** The
  module owns the extension point and ships no opinion about support channels; the
  concrete formatters move to `pkg/props`, where the scaffolded `root.go` already imports
  from, so generated code needs no new import. See §Seams.
- **R3 — Sentry is not a concern.** `getsentry/sentry-go` is a pre-existing transitive
  of `cockroachdb/errors`, already carried by every sibling module; no build tag or
  guard is warranted.

## Open questions

_All resolved — see Resolutions above._

## Status

APPROVED (2026-07-18) — all open questions resolved (R1–R4). Next: flip to
`IN PROGRESS`, build locally to a green checkpoint, then the human-gated
repo/DNS/publish, then the GTB clean-break cut-over.
