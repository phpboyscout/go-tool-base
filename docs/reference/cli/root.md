---
title: Root Command
description: The entry point for the CLI, orchestrating service initialization and global flags.
date: 2026-02-16
tags: [components, commands, root, entrypoint]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Root Command

The Root command is the entry point for every GTB CLI. It orchestrates the primary lifecycle of the application, including service initialization and global feature registration.

## Usage

```bash
mytool [subcommand] [flags]
```

## Description

The root command provides the base structure for your tool. It manages the persistent state (flags, logging, config) for all subcommands. It does not perform a specific domain action on its own but ensures the environment is correctly set up before any subcommand runs.

## Built-in Commands

The root command automatically registers the following subcommands:

| Command | Description | Can be Disabled? |
| :--- | :--- | :--- |
| `version` | Display version information and check for updates | :material-close: No |
| `init` | Initialize tool configuration and setup | :material-check: Yes |
| `update` | Update the tool to the latest version | :material-check: Yes |
| `docs` | Interactive documentation browser with AI Q&A | :material-check: Yes |
| `mcp` | Expose tool as a Model Context Protocol server | :material-check: Yes |
| `doctor` | Environment and configuration health checks, plus `doctor report` | :material-check: Yes |
| `changelog` | Display the embedded changelog | :material-check: Yes |
| `config` | Programmatic config access (`get`/`set`/`list`/`validate`): opt-in, off by default | :material-check: Yes |
| `telemetry` | Opt-in usage telemetry status and management: off by default | :material-check: Yes |
| `man` | Hidden roff man-page emitter for packaging: opt-in, off by default | :material-check: Yes |

See the [Commands Overview](index.md) for the full list and which commands are default-enabled versus opt-in.

!!! tip "Disabling Built-in Commands"
    Use the `Features` property to remove optional commands from your tool:
    ```go
    Tool: props.Tool{
        Features: props.SetFeatures(
            props.Disable(props.UpdateCmd), // Remove update command
            props.Disable(props.McpCmd),    // Remove MCP server
        ),
    }
    ```

## Global flags

Registered as persistent flags on the root command, so every subcommand carries
them. The exact defaults for a given tool are printed by `<tool> --help`.

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--config` | string array | `/etc/<tool>/config.yaml`, `~/.<tool>/config.yaml` | Config files to use, lowest precedence first. |
| `--debug` | bool | `false` | Force debug-level logging. |
| `--ci` | bool | `false` | Treat the run as a CI invocation. |
| `--output` | string | `text` | Output format for commands that render structured results. |
| `-h`, `--help` | bool | `false` | Print help for the command and exit. |

### `--config`: naming a file replaces the defaults

`--config` does not add to the default list, it replaces it, and it also
suppresses the project-local `.<tool>.yaml` layer. Repeat the flag to declare
several files; the last one wins.

Note the default order: the system-wide `/etc` file is declared *first* so the
per-user file overrides it, and so writes from `config set` land in the file you
can actually write to.

A path that does not exist is skipped rather than failing, with one exception:
the last path in the list is always declared so a first write has somewhere to
land. A path that exists but cannot be read, a permissions problem, a broken
mount: is a hard error, because falling back to defaults would hide a real
fault.

Full detail in the [configuration reference](../config/index.md#which-layer-wins-the-precedence-order).

### `--debug`: outranks the configured log level

`--debug` is applied before `log.level` and wins over it, and a configuration
hot-reload cannot downgrade it. It also raises the log level of an embedded MCP
server.

It has no effect on a tool that injects a plain `*slog.Logger` into Props: the
level setter is a no-op there, because that logger owns its own level.

### `--ci`: what changes in a CI run

`--ci` sets the `ci` config key. The framework also honours a bare `CI=true`
environment variable, so a pipeline that forgets the flag is still recognised.
Either one:

- skips the self-update check entirely,
- suppresses the interactive telemetry-consent prompt,
- removes literal-value storage from the credential wizards, leaving env-var and
  keychain modes.

`CI` is compared against the literal string `true`. `CI=1` does **not** count.
Pass `--ci` on those runners.

### `--output`: supported values, and what a wrong one does

The flag's help text says `text, json`, but the renderer behind it accepts
`text`, `json`, `yaml`, `csv`, `tsv` and `markdown`.

Which of those actually work depends on the command. `text`, `json` and `yaml`
render any result. The tabular formats (`csv`, `tsv`, `markdown`) need a
command whose result type carries table tags; on one that does not, the command
fails with `no table tags found on struct`.

An unrecognised value is **not** an error. It falls back to `text` silently, so
`--output jsonn` prints human-readable output and a script parsing it will break
on the next line, not on the flag.

Only commands that render through the output package honour the flag at all
(`version`, `doctor`, `doctor report`, `config list`, `config path` and others).
A command that prints directly ignores it.

**`--output` cannot be set from configuration.** Commands read the flag directly,
so `output: json` in a config file or `MYTOOL_OUTPUT=json` in the environment
does nothing. `--debug` is the same. `--ci` is the exception: it *is* read back
from the `ci` config key, so a config file or environment variable can set it.

## Signal handling and exit codes

`root.Execute` runs the command tree under a signal-aware context. On SIGINT or
SIGTERM:

1. The first signal cancels `cmd.Context()`, logs `received signal, shutting
   down gracefully (press again to force quit)`, and lets the command unwind.
2. A second signal force-exits immediately, so a hung cleanup can never trap the
   user.
3. Buffered telemetry is flushed on every path, success, error and cancellation.

An interrupted run exits **128 + signum**: `130` for SIGINT, `143` for SIGTERM.
That code is used regardless of what the command tree returned, because an
interrupt is a deliberate user choice rather than a failure, which is also why
the notice is logged at debug rather than error. Any other command failure exits
`1` unless the error carries an explicit code.

A service supervisor such as [`go/controls`](https://controls.go.phpboyscout.uk)
observes the context the framework cancels; that is the intended arrangement and
not a reason to take over signals.

### Taking over signal handling

`root.Execute(rootCmd, props, root.WithoutSignals())` stops the framework
installing its handler. Signal disposition is process-global and `signal.Notify`
is additive, so two owners means two shutdown drivers racing on one Ctrl-C,
which is why this is opt-in rather than composable.

A tool that opts out owns the whole contract the framework otherwise provides:
cancelling the command context, flushing telemetry, and choosing an exit code.

## What the root command does not do

- **It does not perform a domain action.** Running `<tool>` with no subcommand
  prints help.
- **It does not add flags to subcommands beyond the persistent set above.** A
  subcommand's own flags are its own.
- **It does not validate configuration.** A wrong value usually falls back to a
  default rather than failing the run; `config validate` is the check.
- **Global flags are not configurable per subcommand.** `--output` means the same
  thing everywhere it is honoured, and is ignored everywhere it is not.
