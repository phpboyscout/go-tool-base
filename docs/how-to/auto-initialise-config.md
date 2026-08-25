---
title: Auto-initialise Configuration on First Run
description: How to make a tool write its default config automatically on first run, or hand config bootstrap to a specific command, using props.Tool.Bootstrap.
date: 2026-07-06
tags: [how-to, config, init, bootstrap, first-run]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Auto-initialise Configuration on First Run

When your tool enables the `init` feature, the framework treats a **missing config file as a hard error** ("please run init") in the root pre-run. Because the framework bootstrap always runs first, that error aborts the invocation *before* any subcommand's own `PreRunE` can run. This guide shows the two opt-ins on `props.Tool.Bootstrap` that let a tool control first-run behaviour instead.

Neither option skips the framework bootstrap itself (config load, telemetry, update check). They relax only the *missing-config outcome*, so `props.Config` is always populated.

## Option 1: Auto-initialise (framework-driven)

Set `AutoInitialise` to have the framework run a **non-interactive** `init` (no credential wizards) that writes the default config when none is found, then continue with it:

```go
props.Tool{
    Name:     "myapp",
    Features: props.SetFeatures(props.Enable(props.InitCmd)),
    Bootstrap: props.BootstrapPolicy{
        AutoInitialise: true,
    },
}
```

On the first run with no config, the tool writes `~/.myapp/config.yaml` from the embedded default and proceeds. No "please run init" error, no prompts. The user can still run the full interactive `init` later to configure credentials.

`AutoInitialise` is honoured only when the `init` feature is enabled (a tool with `init` disabled already tolerates an empty config).

## Option 2: Skip the config check for specific commands

If a command should own its own bootstrap (for example, it runs a guided setup in its own `PreRunE`), relax the missing-config gate for it. The pre-run then falls back to embedded defaults instead of erroring, so control reaches the command.

Name the command declaratively, by its `Name()` or full `CommandPath()`:

```go
props.Tool{
    Name:     "myapp",
    Features: props.SetFeatures(props.Enable(props.InitCmd)),
    Bootstrap: props.BootstrapPolicy{
        SkipConfigCheck: []string{"studio"},       // or "myapp tools studio" to disambiguate
    },
}
```

Or mark the command robustly (rename-safe) with the `setup.SkipConfigCheck` annotation where it is defined:

```go
studioCmd := &cobra.Command{Use: "studio", /* … */}
setup.SkipConfigCheck(studioCmd)
```

Either mechanism relaxes the gate. Use the full `CommandPath()` form when two commands share a `Name()` at different levels of the tree, to avoid an unintended match.

## Which to use

- **`AutoInitialise`**: the batteries-included path. The framework materialises the config; your commands need no bootstrap code.
- **`SkipConfigCheck`**: the escape hatch. Your command's own `PreRunE`/`RunE` decides what to do about the missing config.

If a command is in `SkipConfigCheck` *and* `AutoInitialise` is set, `SkipConfigCheck` wins for that command. It declared that it owns bootstrap, so the framework does not auto-initialise on its behalf.

## Generator support

Both settings round-trip through the project manifest and are re-emitted by `gtb regenerate`:

```yaml
properties:
  bootstrap:
    auto_initialise: true
    skip_config_check:
      - studio
```
