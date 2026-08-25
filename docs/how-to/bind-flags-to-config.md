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

For a CLI flag to take part in that chain, it must be **bound** to a configuration key. Once bound, `props.Config.GetInt("server.port")` returns the flag value when the user passed `--server-port`, the environment value when they didn't but `MYTOOL_SERVER_PORT` is set, and the file value otherwise, with no manual `if flag == ""` plumbing.

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

- **Only flags the user explicitly set.** A flag left at its default never
  contributes: the flags layer walks pflag's `Visit`, which covers changed
  flags only, so a defaulted flag can never silently mask file/env values.
- **Every changed flag participates.** The dispatched command's full flag set
  (local and inherited) becomes the store's highest-precedence layer at
  construction time, with names mapped by the hyphen-to-dot convention unless a
  `WithBoundFlags` mapping says otherwise. Built-ins like `--ci` are therefore
  visible as ordinary config keys (`Config.View().GetBool("ci")`); `--debug`
  also retains its immediate effect on the log level.

## How it is wired

There is no post-hoc binding step: flags are declared as a store layer when the
root pre-run builds the configuration (`config.WithFlags`), so the precedence
is a property of layer order rather than a sequence of bind calls. The
`RootOption`s above are the whole author-facing surface, they feed the
flag-name-to-key mappings that construction uses.

## Related

- [Configuration System](../explanation/components/config/index.md): precedence and the observer pattern.
- [Bind CLI flags](https://config.go.phpboyscout.uk/how-to/bind-cli-flags/): the module guide.
