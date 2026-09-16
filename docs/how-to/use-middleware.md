---
title: Using Command Middleware
description: How to register and apply built-in middleware to your CLI commands with setup.Command.Register.
date: 2026-05-31
tags: [how-to, middleware, setup, config]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Using Command Middleware

GTB's middleware system lets you add cross-cutting behaviour (logging, timing, authentication checks, telemetry) to your CLI commands without duplicating code in every handler. Since v0.5 middleware is wired automatically when a parent attaches a child via `setup.Command.Register`. There is no separate "wrap with middleware" call.

## Registering global middleware

Global middleware applies to **every** command in your tool. This is typically done by the framework's root constructor:

```go
import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
    setup.RegisterGlobalMiddleware(
        setup.WithRecovery(logger),
        setup.WithTiming(logger),
    )
}
```

The root snapshots the registry when it resolves its feature set, so middleware registered after that is not in the tree it built (and nothing panics). Register at process start (`init()` or before `props.New`).

## Registering feature middleware

Feature middleware only applies to commands whose `Feature` key matches. Register it in the feature package's `init()`:

```go
package chat

import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
    // This middleware ONLY runs for commands wrapped with FeatureID("chat").
    setup.RegisterMiddleware(props.FeatureID("chat"), requireKeys(p, "chat.api_key", "chat.model"))
}

// requireKeys is a Middleware: it refuses the command when any of the keys is
// unset in the live config, before the command runs.
func requireKeys(p *props.Props, keys ...string) setup.Middleware {
    return func(next func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
        return func(cmd *cobra.Command, args []string) error {
            view := p.Config.View()
            for _, k := range keys {
                if view.GetString(k) == "" {
                    return fmt.Errorf("%s is not configured", k)
                }
            }

            return next(cmd, args)
        }
    }
}
```

A command picks up that middleware by carrying the matching feature key:

```go
func NewCmdChat(p *props.Props) *setup.Command {
    return setup.Wrap("chat", &cobra.Command{Use: "chat", RunE: runChat})
}
```

## Built-in middleware

### `WithRecovery`
Catches panics and converts them into errors. Without it, an unhandled panic terminates the process: with it, you get a clean `Error: panic: ...` log line and a non-zero exit.

```go
setup.WithRecovery(logger)
```

### `WithTiming`
Logs the wall-clock duration of every command at `INFO` level.

```go
setup.WithTiming(logger)
```

### `WithTelemetry`
Emits structured command-invocation events through the telemetry collector. Active when the `telemetry` feature is enabled and a backend is configured.

```go
setup.WithTelemetry(props)
```

## Attaching commands

Use `*setup.Command.Register(child...)` from the parent. Middleware is applied at attach time:

```go
func NewCmdMyTool(p *props.Props) *setup.Command {
    rootCmd := root.NewCmdRoot(p) // *setup.Command

    rootCmd.Register(
        chat.NewCmdChat(p),       // picks up chat-feature middleware
        deploy.NewCmdDeploy(p),   // picks up deploy-feature middleware (if any)
    )

    return rootCmd
}
```

Equivalent and more common: pass children to the variadic constructor so the wiring is co-located with construction:

```go
rootCmd := root.NewCmdRoot(p,
    chat.NewCmdChat(p),
    deploy.NewCmdDeploy(p),
)
```

Either form works, `Register` is what runs under the hood for both.

!!! warning "Avoid the raw cobra `AddCommand`"
    Calling `rootCmd.Command.AddCommand(unwrappedCobraCmd)` attaches a command without wrapping its `RunE`. The command runs without timing, recovery, or feature middleware. Always go through `setup.Command.Register` (or pass `*setup.Command` to the variadic root constructor).

## How it works under the hood

`Command.Register` does three things per child:

1. Hand the child the root's `Chainer` and, if the child has a `RunE`, replace it with `chain.Chain(child.Feature, child.RunE)`. The default chain wraps with the root's built-in middleware first, then the enabled features' global contributions, then the middleware contributed for `child.Feature`.
2. Walk the child's descendants and wrap any `RunE` not yet wrapped, each under its own feature (the annotation `gtb.chained` marks a wrapped command, so a subtree built bottom-up before it joined the root is wrapped exactly once).
3. Call the embedded `(*cobra.Command).AddCommand` to splice the child into the cobra tree.

The chain is the root's, built by `NewCmdRoot` from its own `Props` (spec 0199 D3), so two roots in one process share no middleware state and each root's telemetry middleware reports to its own collector. `root.WithChain` replaces it; a test hands in a recording `setup.Chainer`.

The result: every command in the tree is wrapped exactly once with its own feature, regardless of how deep the nesting goes.

## See also

- [Command Middleware System](../explanation/components/setup/middleware.md): chain semantics, execution order.
- [Adding Custom Commands](custom-commands.md): full custom-command walkthrough.
- [Writing Custom Middleware](custom-middleware.md): how to build your own.
