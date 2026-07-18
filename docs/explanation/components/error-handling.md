---
title: Error Handling
description: go-tool-base's integration of the standalone go/errorhandling module — the Execute wrapper, signal-aware exits, help-channel implementations, and the generated command patterns.
date: 2026-07-18
tags: [components, error-handling, errors, logging]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Error Handling

The error-reporting layer has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/errorhandling`](https://gitlab.com/phpboyscout/go/errorhandling)
module**. Its full documentation — the `ErrorHandler` interface, hints, exit codes
carried on the error value, `LevelFatalQuiet`, debug-gated stack traces, assertion
failures, the sentinels, and `HelpConfig` — now lives at:

> **[errorhandling.go.phpboyscout.uk](https://errorhandling.go.phpboyscout.uk)**

API reference: **[pkg.go.dev/gitlab.com/phpboyscout/go/errorhandling](https://pkg.go.dev/gitlab.com/phpboyscout/go/errorhandling)**.
See the [migration note](../../reference/migration/v0.x-errorhandling-extracted.md) for
the import-path change and the `Check` signature change.

GTB imports the module directly (no adapter package). This page documents only what
**GTB layers on top**.

## The `Execute` wrapper — one funnel for every error

GTB's commands use Cobra's `RunE` and return errors idiomatically. A single wrapper in
`pkg/cmd/root` is where they all land:

```go
func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())
    pkgRoot.Execute(rootCmd, p)
}
```

`Execute` does four things the module cannot do for you:

1. Sets `SilenceErrors` and `SilenceUsage` so **Cobra never prints errors itself** — all
   output comes from the structured logger.
2. Adds a `--help` hint to flag-parse errors via `SetFlagErrorFunc`.
3. Runs the command tree under a **signal-aware context** (see below).
4. Routes whatever comes back through `ErrorHandler.Check` at `LevelFatal`.

The result: runtime errors, flag-parse errors, and `PersistentPreRunE` failures are all
reported the same way, and there is exactly one place in the process that exits.

## Signal handling

`Execute` runs the command tree under a context cancelled by `SIGINT`/`SIGTERM`. The
first signal cancels gracefully; a **second forces an immediate exit** so a hung cleanup
cannot trap the user. The run exits `128+signum` (130 for SIGINT, 143 for SIGTERM)
through the module's `LevelFatalQuiet` path — the correct code, logged at debug rather
than as an error, because an interrupt is a deliberate choice, not a failure.

!!! warning "Flush before the fatal call, not in a `defer`"
    `Check(..., LevelFatal)` exits the process, so **deferred cleanup in the calling
    frames never runs**. GTB's telemetry flush is therefore `sync.Once`-guarded and
    invoked *explicitly* before the fatal call, with its own bounded background context
    — a cancelled context would abort the flush itself. Any pre-exit work you add must
    follow the same pattern.

## Help channels

The module defines the `HelpConfig` interface and deliberately ships no
implementations — where a team's support channel lives is a framework concern, not an
error library's. GTB provides the two common ones in `pkg/props`:

```go
props.Tool{
    Help: props.SlackHelp{Team: "Platform", Channel: "#platform-help"},
    // or props.TeamsHelp{Team: "Platform", Channel: "Support"}
}
```

Scaffolded projects get this wired from `gtb generate project --help-type slack|teams`,
and the value round-trips through the project manifest.

## Command patterns

Generated commands follow a fixed shape:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    return runMyCommand(cmd.Context(), props)
},
```

- **Return errors; don't report them in place.** `Execute` reports. A `Fatal` buried in
  business logic skips deferred cleanup — see the module's
  [reporting model](https://errorhandling.go.phpboyscout.uk/explanation/reporting-model/).
- **`SetUsage` is set per command.** Generated commands call
  `props.ErrorHandler.SetUsage(cmd.Usage)` in their `PreRunE`, so a parent command that
  returns `ErrRunSubCommand` prints *its own* usage, not the root's.
- **A stubbed command returns `ErrNotImplemented`** (or `NewErrNotImplemented(issueURL)`
  to point at a tracking issue), which reports as a warning rather than a crash.

## Related

- **Module docs:** [errorhandling.go.phpboyscout.uk](https://errorhandling.go.phpboyscout.uk)
- **How-to:** [Write user-facing errors](../../how-to/user-facing-errors.md),
  [Custom commands](../../how-to/custom-commands.md)
- **Patterns:** [Error handling patterns](../../development/error-handling.md)
