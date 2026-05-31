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
type rather than the legacy `AddCommandWithMiddleware` helper. A
`setup.Command` carries its own `props.FeatureCmd` key alongside the
underlying `*cobra.Command`, and middleware is wired exactly once at
attach time.

```go
type Command struct {
    *cobra.Command
    Feature props.FeatureCmd
}
```

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

### Deprecated helpers

`AddCommandWithMiddleware(parent, child, feature)` and
`ApplyMiddlewareRecursively(cmd, feature)` are retained as `// Deprecated:`
shims for one release. The shim no longer recurses into descendants — only
the immediate child's `RunE` is wrapped. New code should always use
`parent.Register(child)`. Planned removal: v1.0.

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
