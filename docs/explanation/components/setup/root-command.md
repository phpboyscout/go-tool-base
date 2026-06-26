---
title: Root Command
description: Root command architecture, PersistentPreRunE lifecycle hooks, signal handling, and implementation.
date: 2026-02-16
tags: [components, setup, initialization, bootstrapping]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Root Command

## Root Command Architecture

## Lifecycle Hooks (PersistentPreRunE)

Before any subcommand is executed, the root command performs the following automated steps:

1. **Flag Extraction**: Validates and parses the global flags.
2. **Configuration Loading**: Merges embedded assets with local filesystem configuration.
3. **Logging Setup**: Configures the global `props.Logger` level and format based on flags and config.
4. **Update Checking**: Optionally performs a background check for newer versions (unless `--ci` is set or the check was done in the last 24 hours).

## Signal Handling

`root.Execute` runs the command tree with a **signal-aware execution context**: it derives a cancellable context watching `os.Interrupt` (SIGINT/Ctrl-C) and `syscall.SIGTERM`, and passes it to Cobra via `ExecuteContext`, so every command's `cmd.Context()` is cancelled on interruption.

The lifecycle mirrors `kubectl`/`docker`:

1. **First signal** — cancels `cmd.Context()`. Long-running commands observing `ctx.Done()` unwind gracefully; the deferred telemetry flush still runs (on a bounded background context, so cancellation cannot abort the flush itself).
2. **Second signal** — force-exits immediately, so a hung cleanup can never trap the user.
3. **Exit code** — a signal-terminated run exits `128 + signum` (`130` for SIGINT, `143` for SIGTERM), threaded through the `ErrorHandler`'s exit path via `errorhandling.WithExitCode` so it never conflicts with normal error exits.

An interrupt is a deliberate user choice, not a failure, so the `interrupted by signal: …` notice is logged at **debug**, not error (it routes through `errorhandling.LevelFatalQuiet`, which exits like `LevelFatal` but logs at debug). End users see a clean exit with the conventional code; `--debug` still surfaces the notice. The non-zero exit code is the signal.

On Windows only `os.Interrupt` is deliverable; the SIGTERM registration is harmless there, so no build tags are needed.

!!! note "Interactive prompts own Ctrl-C"
    While a TUI prompt (Huh/Bubble Tea) is active, the terminal is in raw mode, so Ctrl-C arrives as a *keystroke* — it aborts the current prompt and never raises SIGINT. The outer signal context therefore only acts when no TUI is reading the keyboard. An external `kill -INT`/`kill -TERM` still cancels the whole run, which is the desired semantic for supervisors.

Commands should treat `cmd.Context()` as the single cancellation source:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    select {
    case <-cmd.Context().Done():
        return cmd.Context().Err() // graceful unwind on Ctrl-C / SIGTERM
    case result := <-work:
        return handle(result)
    }
},
```

## Implementation

The root command is implemented in `cmd/root/root.go` and created via the `root.NewCmdRoot(props)` entry point.

For more information on the dependency injection pattern used here, see the **[Props Documentation](../props.md)**.
