---
title: Command Middleware
description: Technical reference for the command middleware system in the setup package.
date: 2026-03-24
tags: [components, setup, middleware, cobra]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Command Middleware

The middleware system in `pkg/setup` provides a mechanism for wrapping `cobra.Command` execution with cross-cutting concerns. It uses a functional chain pattern that allows logic to be executed before and after a command's `RunE` function.

## Core API

### Middleware Type

```go
type Middleware func(next cobra.RunEFunc) cobra.RunEFunc
```

A `Middleware` is a higher-order function that takes a `cobra.RunEFunc` and returns a new `cobra.RunEFunc`. This allows for nesting and composition.

### Registration Functions

#### `RegisterGlobalMiddleware`
```go
func RegisterGlobalMiddleware(mw ...Middleware)
```
Adds middleware that will be applied to **all** commands registered via the root command. Global middleware is executed before feature-specific middleware.

#### `RegisterMiddleware`
```go
func RegisterMiddleware(feature props.FeatureCmd, mw ...Middleware)
```
Adds middleware that will be applied only to commands associated with a specific `props.FeatureCmd`.

#### `Seal`
```go
func Seal()
```
Locks the middleware registry. This must be called before `Chain()` is used, typically during root command initialization. Attempting to register middleware after sealing will cause a panic.

### Application Functions

#### `Chain`
```go
func Chain(feature props.FeatureCmd, runE cobra.RunEFunc) cobra.RunEFunc
```
Applies all registered global and feature-specific middleware to the provided `RunE` function, returning the final wrapped function.

## Composed Commands: `setup.Command`

Since v0.5 the canonical integration surface is the composed `setup.Command`
type. (The former `AddCommandWithMiddleware` helper it replaced was deprecated
in v0.5 and removed in v0.20.) A `setup.Command` carries its own
`props.FeatureCmd` key alongside the underlying `*cobra.Command`, and middleware
is wired exactly once at attach time.


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/setup](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/setup) for the full API definition.


#### `setup.Wrap`
```go
func Wrap(feature props.FeatureCmd, cmd *cobra.Command) *Command
```
Produces a `*Command` bound to `feature`. Untyped string literals are
implicitly converted to `props.FeatureCmd`, so call sites typically read
as `setup.Wrap("serve", &cobra.Command{...})`.

#### `Command.Register`
```go
func (c *Command) Register(children ...*Command)
```
Attaches each child to the underlying cobra tree and wraps its `RunE`
with `Chain(child.Feature, child.RunE)` — applying every global
middleware and any feature-specific middleware registered for the
child's key. Each child is wrapped exactly once with **its own** feature;
the parent's feature is not propagated downward.

```go
parent := setup.Wrap("parent", &cobra.Command{Use: "parent"})
parent.Register(
    childA.NewCmdChildA(p),  // returns *setup.Command
    childB.NewCmdChildB(p),
)
```

### Removed helpers

The `AddCommandWithMiddleware(parent, child, feature)` and
`ApplyMiddlewareRecursively(cmd, feature)` helpers — both `// Deprecated:` since
the composed `setup.Command` type landed in v0.5 — were **removed in v0.20**.
Wrap each command with its own feature via `setup.Wrap` and attach it through
`parent.Register(child)`; replace any `AddCommandWithMiddleware(parent, child,
feature)` call with `parent.Register(setup.Wrap(feature, child))`. See the
[migration note](../../../reference/migration/v0.20-deprecated-middleware-helpers-removed.md).

## Built-in Middleware

The `setup` package provides several production-ready middlewares in `middleware_builtin.go`.

### `WithTiming`
```go
func WithTiming(l logger.Logger) Middleware
```
Logs the execution duration of the command.
- **Log Level**: `Info`
- **Fields**: `command`, `duration`, `error` (if any)

### `WithRecovery`
```go
func WithRecovery(l logger.Logger) Middleware
```
Catches panics during command execution and converts them into returned errors.
- **Log Level**: `Error` (on panic)
- **Fields**: `command`, `panic`, `stack`

### `WithAuthCheck`
```go
func WithAuthCheck(keys ...string) Middleware
```
Verifies that the specified configuration keys are set (non-empty) before executing the command. If any key is missing, it returns an error and prevents command execution.

## Implementation Details

### Execution Order
When `Chain` is called, it constructs a sequence:
`Global MW 1` -> `Global MW 2` -> `Feature MW 1` -> `Feature MW 2` -> `Actual Command`

Because each middleware "wraps" the next, the "before" logic executes in the order above, while "after" logic (and `defer` statements) executes in reverse order.

### Thread Safety
The middleware registry uses a `sync.RWMutex` to ensure safe concurrent access, although registration typically happens during single-threaded `init()` phases.

### Error Handling
Middleware should generally return the error from the `next()` call unless they are specifically designed to transform or suppress errors. GTB recommends using `github.com/cockroachdb/errors` for wrapping errors within middleware.

## Example: Custom Middleware

```go
func WithCustomHeader(header string) setup.Middleware {
    return func(next cobra.RunEFunc) cobra.RunEFunc {
        return func(cmd *cobra.Command, args []string) error {
            fmt.Println(header)
            return next(cmd, args)
        }
    }
}
```


## Architecture & Context

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


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/setup](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/setup) for the full API definition.


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
