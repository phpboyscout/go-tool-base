---
title: Command Constructor Pattern
description: Rationale and implementation details for the NewCmd* constructor pattern in CLI commands.
date: 2026-02-17
tags: [concepts, command, constructor, dependency-injection, cobra]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Command Constructor Pattern

In GTB, we consistently use the `NewCmd*` constructor pattern for instantiating commands. Since v0.5 the constructor returns `*setup.Command` — a typed wrapper around `*cobra.Command` that also carries the command's middleware feature key. This architectural choice is fundamental to the framework's goals of testability, modularity, and explicit dependency management, and it removes a class of regressions where the parent had to know how to wrap each child.

## The Pattern

A typical command constructor in GTB looks like this:

```go
func NewCmdExample(props *props.Props) *setup.Command {
    cmd := setup.Wrap("example", &cobra.Command{
        Use:   "example",
        Short: "An example command",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation logic using props
            props.Logger.Info("Executing example command")
            return nil
        },
    })

    // Add flags or subcommands
    return cmd
}
```

`setup.Wrap(feature, cobraCmd)` returns a `*setup.Command` that **embeds** `*cobra.Command`, so every cobra method — `cmd.Flags()`, `cmd.MarkFlagsMutuallyExclusive(…)`, `cmd.SetContext(…)` — keeps working through the embedded pointer. The `"example"` literal is implicitly converted to `props.FeatureCmd` (a named string type) by Go, so call sites stay readable.

## Composing the tree

Parents attach children by calling `Register(...)` on their own `*setup.Command`. There is **no** separate "wrap with middleware" step:

```go
func NewCmdRoot(p *props.Props) *setup.Command {
    cmd := setup.Wrap("", &cobra.Command{Use: "myapp"})

    cmd.Register(
        example.NewCmdExample(p),
        other.NewCmdOther(p),
    )

    return cmd
}
```

`Register` wires global middleware *and* any feature-specific middleware registered for the child's key. Each command is wrapped exactly once with **its own** feature — the parent's feature is never propagated downward.

## Rationale

### 1. Explicit Dependency Injection

By passing the `Props` container directly to the constructor, we make the command's dependencies explicit. The command has immediate access to core services like logging, configuration, and the filesystem without relying on global state or hidden package-level variables.

### 2. Improved Testability

Because dependencies are injected, they can be easily mocked during unit testing. For example, you can pass a `Props` object with an in-memory `afero.Fs` to verify file operations without touching the actual disk.

```go
func TestExampleCommand(t *testing.T) {
    mockFS := afero.NewMemMapFs()
    p := &props.Props{
        FS: mockFS,
        // ... other mocked props
    }

    cmd := NewCmdExample(p)
    // Execute command and assert on mockFS state
}
```

### 3. Encapsulation

The constructor provides a single place to define the command URI, description, flags, and execution logic. This encapsulation makes the codebase easier to navigate and maintain, as everything related to a specific command is contained within its own package and constructor.

### 4. Consistency Across the Framework

Using a standardized pattern ensures that all commands in a project behaving similarly. Whether it's a built-in framework command like `version` or a custom-implemented feature, the lifecycle and dependency management remain identical.

### 5. Seamless Generation

This pattern is natively supported by the [Framework CLI](../../reference/cli/index.md) and its generation logic. When you add a new command via the manifest, the generator automatically scaffolds the `NewCmd*` constructor, ensuring your project remains aligned with framework standards.

## Best Practices

- **Avoid Global State**: Do not use `init()` functions to register commands globally. Use the constructor and register the command in the parent's constructor or the `Root` command.
- **Minimal Logic in Run**: Keep the `Run()` function focused on parsing arguments and calling service methods. Business logic should ideally reside in the `pkg/` directory, making it independently testable.
- **Pass Props Down**: If a command has subcommands, pass the `Props` pointer down to their respective constructors.
- **Wrap once, at the top of the constructor**: assign `cmd := setup.Wrap(...)` immediately so every later mutation (`cmd.Flags()`, `cmd.Register(child)`, …) operates on the composed type.
- **Use the empty feature `""` for non-feature-gated commands**: root and pure command-group containers (with no feature-specific middleware) typically pass `setup.Wrap("", &cobra.Command{...})`. Feature keys are middleware lookup keys, not display labels.

## Why the typed return matters

Returning `*setup.Command` (rather than `*cobra.Command`) is what lets `parent.Register(child)` be the single, idiomatic attachment call:

- It can wrap the child's `RunE` with the child's own feature middleware.
- It can stay idempotent on regeneration — re-running the generator does not double-wrap.
- It is type-checked at compile time: a caller cannot accidentally attach an unwrapped `*cobra.Command` and skip middleware.

The previous API exposed this via a free function (`setup.AddCommandWithMiddleware(parent, child, props.<Name>Cmd)`) that required the parent to know the child's feature key. That coupling is removed; the child owns its own identity. See the [v0.4-to-v0.5 migration guide](../../reference/migration/v0.4-to-v0.5.md) for the before/after diff and the [command middleware concept](command-middleware.md) for how the wrapping interacts with the middleware registry.
