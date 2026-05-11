---
title: Using Command Middleware
description: How to register and apply built-in middleware to your CLI commands.
date: 2026-03-24
tags: [how-to, middleware, setup, config]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Using Command Middleware

GTB's middleware system allows you to add cross-cutting behaviors like logging, timing, and authentication checks to your CLI commands without duplicating code in every handler.

## Registering Global Middleware

Global middleware applies to **every** command in your tool. This is typically done during the initialization of your root command.

```go
package root

import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func registerFeatureCommands(rootCmd *cobra.Command, props *props.Props) {
    // 1. Register global middleware
    setup.RegisterGlobalMiddleware(
        setup.WithRecovery(props.Logger),
        setup.WithTiming(props.Logger),
    )

    // 2. Seal the registry before applying it to commands
    setup.Seal()

    // ... command registration continues
}
```

## Registering Feature Middleware

Feature-specific middleware only applies to commands belonging to a particular feature. This is ideal for domain-specific checks like verifying API keys.

You typically register these in the `init()` function of your feature package:

```go
package chat

import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/setup"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func init() {
    // This middleware will ONLY run for commands associated with FeatureCmdChat
    setup.RegisterMiddleware(props.FeatureCmdChat,
        setup.WithAuthCheck("chat.api_key", "chat.model"),
    )
}
```

## Using Built-in Middleware

### WithRecovery
Ensures your CLI doesn't crash on panics. Instead, it logs the panic and stack trace and returns a clean error.

```go
setup.WithRecovery(props.Logger)
```

### WithTiming
Logs how long each command took to execute.

```go
setup.WithTiming(props.Logger)
```

### WithAuthCheck
Validates that required configuration settings are present before running the command. This prevents commands from failing midway because of missing credentials.

```go
setup.WithAuthCheck("github.token")
```

## How it Works Under the Hood

When you register a command using GTB's standard root command pattern, the `Chain()` function is called:

1.  It collects all **Global Middleware**.
2.  It collects all **Feature Middleware** for the current command's feature.
3.  It wraps the command's `RunE` function in a nested chain.

If a middleware fails (e.g., `WithAuthCheck` finds a missing key), it returns an error, and the actual command handler is **never executed**.

## Manual Command Registration

If you are adding commands to your CLI tree manually (e.g., in `main.go` after calling `root.NewCmdRoot`), use the `AddCommandWithMiddleware` helper from `pkg/cmd/root`.

Using standard cobra `root.AddCommand(myCmd)` will **bypass** the middleware chain.

```go
package main

import (
    "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func main() {
    p := &props.Props{...}
    rootCmd := root.NewCmdRoot(p)

    // INCORRECT: myCmd will NOT have middleware (no timing, no recovery)
    // rootCmd.AddCommand(myCmd)

    // CORRECT: myCmd and its children will have middleware applied
    root.AddCommandWithMiddleware(rootCmd, myCmd, props.FeatureCmdMyFeature)

    rootCmd.Execute()
}
```

This helper handles:
1.  Wrapping the `RunE` of `myCmd`.
2.  Recursively wrapping all subcommands of `myCmd`.
3.  Registering `myCmd` with `rootCmd`.
