---
title: Command Constructor Pattern
description: Rationale and implementation details for the NewCmd* constructor pattern in CLI commands.
date: 2026-02-17
tags: [concepts, command, constructor, dependency-injection, cobra]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Command Constructor Pattern

In GTB, we consistently use the `NewCmd*` constructor pattern for instantiating `cobra.Command` structs. This architectural choice is fundamental to the framework's goals of testability, modularity, and explicit dependency management.

## The Pattern

A typical command constructor in GTB looks like this:

```go
func NewCmdExample(props *props.Props) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "example",
        Short: "An example command",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation logic using props
            props.Logger.Info("Executing example command")
            return nil
        },
    }

    // Add flags or subcommands
    return cmd
}
```

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

This pattern is natively supported by the [Framework CLI](../cli/index.md) and its generation logic. When you add a new command via the manifest, the generator automatically scaffolds the `NewCmd*` constructor, ensuring your project remains aligned with framework standards.

## Best Practices

- **Avoid Global State**: Do not use `init()` functions to register commands globally. Use the constructor and register the command in the parent's constructor or the `Root` command.
- **Minimal Logic in Run**: Keep the `Run()` function focused on parsing arguments and calling service methods. Business logic should ideally reside in the `pkg/` directory, making it independently testable.
- **Pass Props Down**: If a command has subcommands, pass the `Props` pointer down to their respective constructors.
