---
title: Bind CLI Flags to Config
description: Wire command-line flags into the configuration precedence so a flag overrides env vars and file config.
date: 2026-06-12
tags: [how-to, config, flags, cobra, viper, precedence]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Bind CLI Flags to Config

GTB resolves configuration through a single precedence chain:

> **flags > env > file > embedded > defaults**

For a CLI flag to take part in that chain, it must be **bound** to a configuration key. Once bound, `props.Config.GetInt("server.port")` returns the flag value when the user passed `--server-port`, the environment value when they didn't but `MYTOOL_SERVER_PORT` is set, and the file value otherwise — with no manual `if flag == ""` plumbing.

## Bind a flag explicitly

Use `WithBoundFlags` to map a config key to a flag. Create the flag in a `*pflag.FlagSet`; the option registers it on the root command for you.

```go
import (
    "github.com/spf13/pflag"
    "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
)

func newRoot(props *props.Props) *setup.Command {
    flags := pflag.NewFlagSet("server", pflag.ContinueOnError)
    flags.Int("server-port", 8080, "server port")

    return root.NewCmdRootWithOptions(props,
        root.WithBoundFlags(map[string]*pflag.Flag{
            "server.port": flags.Lookup("server-port"),
        }),
    )
}
```

Now `mytool --server-port 9090` makes `props.Config.GetInt("server.port")` return `9090`, overriding both `MYTOOL_SERVER_PORT` and `server.port` in the config file.

## Bind by convention (zero boilerplate)

If your flag names already mirror your config keys, `WithConventionBoundFlags` binds an entire flag set, deriving each key by replacing hyphens with dots (`--server-port` → `server.port`):

```go
return root.NewCmdRootWithOptions(props,
    root.WithConventionBoundFlags(flags),
)
```

`root.ConventionKey("server-port")` exposes the same mapping if you need to compute keys yourself.

## Per-command flags

A subcommand's **own local flags** are bound automatically when that command runs, using the same hyphen-to-dot convention. No extra wiring is needed:

```go
serve := &cobra.Command{Use: "serve", RunE: runServe}
serve.Flags().Int("server-port", 8080, "server port for serve")
```

`mytool serve --server-port 9090` overrides `server.port` for the `serve` command's `RunE`.

## What gets bound

- **Only flags the user explicitly set.** A flag left at its default is filtered out (`flag.Changed == false`) and never overrides config. This avoids viper's default-clobber footgun, where binding a defaulted flag silently masks file/env values.
- **Built-ins `--debug` and `--ci`** are folded through the same path, so `Config.GetBool("ci")` reflects `--ci`. `--debug` also retains its immediate effect on the log level.

## When to bind directly

For advanced cases you can bind onto the container directly with `Containable.BindPFlag` — but remember to filter by `flag.Changed` yourself:

```go
if flag.Changed {
    if err := props.Config.BindPFlag("server.port", flag); err != nil {
        return err
    }
}
```

The `RootOption`s above are preferred because they handle registration, `flag.Changed` filtering, and per-command binding consistently.

## Related

- [Configuration System](../explanation/components/config.md) — precedence and the observer pattern.
- [Config component reference](../explanation/components/config.md#binding-cli-flags-to-config) — full API.
