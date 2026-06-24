---
title: Command Middleware System
description: Understanding the middleware chain pattern for cross-cutting CLI command concerns.
date: 2026-03-24
tags: [concepts, middleware, extensibility, cobra]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Command Middleware System

!!! note "CLI commands, not HTTP/gRPC"
    This page covers middleware for the **cobra command tree** (wrapping command
    `RunE` execution). For cross-cutting concerns on the **HTTP/gRPC transports**
    — logging, auth, rate limiting, retry, circuit breaking — see
    [Transport Middleware & Resilience](transport-middleware.md). The two share a
    philosophy but are entirely separate systems.

The Command Middleware System provides a powerful way to inject shared behavior across your CLI command tree. Instead of duplicating logic in every command's `RunE` function, you can define "middlewares" that wrap your commands to handle cross-cutting concerns.

## The Chain Pattern

GTB uses a functional "Chain" pattern for middleware, similar to those found in modern web frameworks like Gin or Echo. A middleware is a function that receives the "next" handler in the chain and returns a new handler.

```go
type Middleware func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error
```

This pattern is superior to simple lifecycle hooks (like `PreRun`) because it allows the middleware to "wrap" the entire execution. A middleware can:
1.  Execute logic **before** the command runs.
2.  Execute logic **after** the command runs.
3.  **Recover** from panics within the command.
4.  **Decide** whether to call the next handler at all (e.g., for auth checks).
5.  **Transform** the error returned by the command.

## Global vs. Feature Middleware

GTB distinguishes between two scopes of middleware:

**Global Middleware**
: Applied to **every** command in the tool. Ideal for universal concerns like panic recovery, execution timing, and global telemetry.

**Feature Middleware**
: Applied only to commands belonging to a specific **Feature**. Ideal for domain-specific concerns like verifying API keys for AI commands or checking for a valid git repository.

## Execution Order

Middleware is applied in a deterministic order to ensure predictable behavior:

1.  **Global Middleware** executes first, in the order they were registered.
2.  **Feature Middleware** executes second, in the order they were registered.
3.  **The Command Handler** executes last.

Because it is a wrapping chain, the "before" logic runs in registration order (outermost to innermost), and the "after" logic (and `defer` blocks) runs in reverse registration order (innermost to outermost).

## The Registry Lifecycle

To ensure thread safety and architectural consistency, the middleware registry follows a strict lifecycle:

1.  **Registration**: Occurs during the `init()` phase of your packages.
2.  **Sealing**: The registry is "sealed" during the root command registration. No further middleware can be added once the command tree is being built.
3.  **Execution**: When a parent attaches a child via `parent.Register(child)`, `Chain()` wraps the child's `RunE` exactly once — with that child's own feature key. Every command in the tree picks up its own middleware at attach time; nothing is wrapped twice.

## How wrapping is wired

Each `*setup.Command` carries a `Feature` field (`props.FeatureCmd`) that the wrapping path uses as the middleware-registry key:

```go
type Command struct {
    *cobra.Command
    Feature props.FeatureCmd
}
```

When the root constructor (or any parent) calls `Register`, the framework:

1. Iterates each child you pass in.
2. Wraps the child's `RunE` with `Chain(child.Feature, child.RunE)` — applying global middleware then any middleware registered against the child's feature.
3. Calls the embedded `(*cobra.Command).AddCommand` to splice the child into the cobra tree.

Because each command owns its own feature, a parent never needs to know which middleware its descendants need. Siblings can have different feature keys and pick up entirely different middleware stacks.

## Manual / late attachment

If you build your CLI tree *after* the root has been initialised (dynamic plugins, conditional commands, late discovery), still use `Register`:

```go
rootCmd := root.NewCmdRoot(p) // *setup.Command

if pluginEnabled {
    rootCmd.Register(plugin.NewCmdPlugin(p))
}
```

The `Register` call is idempotent against double-attachment (the underlying cobra parent rejects duplicates) and always wires middleware correctly, regardless of when it fires relative to `setup.Seal()`.

!!! warning "Deprecated: `AddCommandWithMiddleware`"
    The legacy `setup.AddCommandWithMiddleware(parent, child, feature)` helper is still exported but marked `// Deprecated:`. It now delegates to `Command.Register`, no longer recurses into descendants (the recursive re-wrap with the *parent's* feature was always semantically wrong), and will be removed in v1.0. Migrate to `parent.Register(child)` — the [v0.4-to-v0.5 migration guide](../../reference/migration/v0.4-to-v0.5.md) has the diff.

## Hooks vs. the framework bootstrap

The framework's own bootstrap — config loading, log-level setup, feature-flag resolution, telemetry collector construction, and the update check — runs in the **root command's `PersistentPreRunE`**. By default cobra runs only the *closest* `PersistentPreRunE` in the command chain, so a subcommand that defines its own would silently shadow the root bootstrap for that subtree (a footgun where `props.Config` ends up nil at runtime).

GTB removes this footgun: `NewCmdRoot` sets `cobra.EnableTraverseRunHooks = true`, so cobra runs **every** `PersistentPreRunE` from root to leaf. The guarantee is:

1. The framework bootstrap (root hook) **always runs first**.
2. Your subcommand's `PersistentPreRunE` runs **after** it — never instead of it.

This means a downstream `PersistentPreRunE` can safely rely on `props.Config`, `props.Collector`, and the resolved log level already being populated. You do **not** need to (and should not) call the framework bootstrap yourself. When the tree contains a downstream `PersistentPreRunE`, GTB emits a one-time debug log noting this bootstrap-then-child ordering.

---

!!! tip "Middleware vs. Hooks"
    Use **Hooks** (`PersistentPreRunE`) for environmental setup like loading config files. Use **Middleware** for operational concerns that need to wrap the execution, like timing, logging, or error recovery.

    A subcommand-level `PersistentPreRunE` does **not** replace the framework bootstrap — `NewCmdRoot` enables `cobra.EnableTraverseRunHooks`, so the root bootstrap always runs first and your hook runs after it (root→leaf). Your hook can rely on `props.Config` already being loaded.
