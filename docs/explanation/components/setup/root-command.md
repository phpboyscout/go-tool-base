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
2. **Configuration Loading**: Builds the layered `config.Store` — embedded defaults, config files, env, and changed flags — and pins it on `props.Config`.
3. **Logging Setup**: Configures the global `props.Logger` level and format based on flags and config.
4. **Update Checking**: Optionally performs a background check for newer versions (unless `--ci` is set or the check was done in the last 24 hours).

### The auxiliary fast path

Not every command should pay the bootstrap. Before configuration is even
loaded, the pre-run returns early — skipping config load, telemetry consent,
collector wiring, and the update check, while still honouring `--debug` for
logging — for:

- **The `init` subtree.** `init` is what *creates* the configuration, and its
  provider subcommands (`init github`, `init bitbucket`, …) must equally run on
  a configless machine. The subtree is identified by walking up the command
  tree for the `InitCmd` feature annotation stamped by `setup.Wrap` — never the
  fragile `Use` string.
- **Cobra's own generated auxiliary commands** — `help`, `completion` (and its
  shell subcommands), and the hidden `__complete` used by shell tab-completion.
  On a fresh install they must not fail the missing-config gate, and
  tab-completion must never pay the bootstrap (least of all a network update
  check) on every keystroke. Only cobra's *own* instances are exempted: they
  are direct children of the root carrying no annotations, whereas every
  GTB-wrapped command is stamped by `setup.Wrap` — so a downstream feature
  command that happens to be named `help` or `completion` still gets the
  normal bootstrap.
- **Commands listed in `Tool.Bootstrap.AuxiliaryCommands`**, so a downstream
  tool can extend the set (e.g. for its own bootstrap-free plumbing commands)
  without a framework release. Entries match a command's `Name()` or full
  `CommandPath()`, like `Bootstrap.SkipConfigCheck`. Note this is stronger
  than `SkipConfigCheck`: an auxiliary command runs with `props.Config` left
  nil, so it must read nothing from configuration.

### Update-check exemptions

The pre-run update check is likewise annotation-driven, not name-driven.
`setup.SkipUpdateCheck` exempts a command when it — or any ancestor, so a
stamped group covers its subtree — carries the `setup.MarkSkipUpdateCheck`
annotation or is wrapped with the `UpdateCmd`/`InitCmd` feature. Built-ins
stamp themselves (`version` prints its own check, `doctor` diagnoses the
install it has, `mcp` must keep protocol stdout clean); a downstream command
opts out with `setup.MarkSkipUpdateCheck(cmd)`.

### The missing-config gate

When the `init` feature is enabled, configuration is treated as required: if no
config file exists, loading returns `ErrNoConfigFile` rather than running the
command against bare defaults. Two escape hatches relax it:

- **Auto-initialise** — with `Tool.Bootstrap.AutoInitialise` set, the pre-run
  heals a missing config by running a non-interactive `init` (writing the
  default config) and then loading it for real, so the command proceeds.
- **Config-independent commands skip the gate** — `version`, `changelog`,
  `man`, and `docs` read nothing from configuration, so requiring one before
  they run would be a fresh-install papercut. They carry the
  `setup.SkipConfigCheck` annotation, which relaxes the gate to a tolerant load
  (embedded defaults, no error) — `props.Config` is still populated, so any
  incidental read is safe. The `init` subtree and cobra's generated
  help/completion commands skip config loading entirely (see the auxiliary
  fast path above). A command that owns its own bootstrap can opt in with
  `setup.SkipConfigCheck(cmd)` or `Tool.Bootstrap.SkipConfigCheck`.

When the gate does fire — a config-gated command on a machine with no config
file — the resulting error carries a hint naming the fix:
`Run '<tool> init' to create a configuration.`

## Signal Handling

`root.Execute` runs the command tree with a **signal-aware execution context**: it derives a cancellable context watching `os.Interrupt` (SIGINT/Ctrl-C) and `syscall.SIGTERM`, and passes it to Cobra via `ExecuteContext`, so every command's `cmd.Context()` is cancelled on interruption.

The lifecycle mirrors `kubectl`/`docker`:

1. **First signal** — cancels `cmd.Context()`. Long-running commands observing `ctx.Done()` unwind gracefully; the deferred telemetry flush still runs (on a bounded background context, so cancellation cannot abort the flush itself).
2. **Second signal** — force-exits immediately, so a hung cleanup can never trap the user.
3. **Exit code** — a signal-terminated run exits `128 + signum` (`130` for SIGINT, `143` for SIGTERM), threaded through the `ErrorHandler`'s exit path via `errorhandling.WithExitCode` so it never conflicts with normal error exits.

An interrupt is a deliberate user choice, not a failure, so the `interrupted by signal: …` notice is logged at **debug**, not error (it routes through `errorhandling.LevelFatalQuiet`, which exits like `LevelFatal` but logs at debug). End users see a clean exit with the conventional code; `--debug` still surfaces the notice. The non-zero exit code is the signal.

On Windows only `os.Interrupt` is deliverable; the SIGTERM registration is harmless there, so no build tags are needed.

### The framework owns signals, and why that matters

Signal disposition is **process-global**: `signal.Notify` is additive, so every registered channel receives a copy of every signal. Whatever registers a handler becomes an *owner* of that global, and two owners means two shutdown drivers racing on a single Ctrl-C.

The framework claims that ownership because it has framework-wide work to do on interruption — flushing buffered telemetry, cleaning up a half-written self-update — that individual commands know nothing about.

The corollary is that **a component beneath the framework must not register its own handler**. A service supervisor such as [`go/controls`](https://controls.go.phpboyscout.uk) observes the context the framework cancels; it does not compete for the signal. That is the intended arrangement and needs no configuration.

### Opting out

A tool that genuinely needs to own signal disposition itself can decline the framework's handler:

```go
gtbRoot.Execute(rootCmd, p, gtbRoot.WithoutSignals())
```

Having opted out, the tool owns the whole contract the framework otherwise provides: cancelling the command context, flushing telemetry before exit, and choosing an exit code. This is deliberately a rare need — reach for it only when something in the tool must genuinely be the signal owner.

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
