---
title: Dependency Injection (Props)
description: The Props dependency injection container for global state and services.
date: 2026-02-16
tags: [concepts, props, dependency-injection, architecture]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Dependency Injection (Props)

The `Props` struct is the "Context Object" of GTB. It acts as a dependency injection container that carries global state, service interfaces, and tool metadata throughout your application's lifecycle.

!!! note "What's in a Name?"
    The name **Props** is not merely a shorthand for 'properties' (though we do shove plenty of those in there). It’s a direct reference to a **prop**—the heavy-duty timber or steel beam that prevents a structure from an embarrassing collapse. For the sports fans, it’s also a lovingly crafted nod to the rugby position: the broad-shouldered stalwarts who provide the primary structural support for the scrum. Much like its on-field namesake, our `Props` struct isn't here to score the flashy tries; it's here to do the unsung heavy lifting that keeps the entire framework from falling over.

## The Props Struct

Defined in `pkg/props`, the `Props` object typically contains:

- **`Tool`**: Metadata about your CLI (Name, Summary, GitHub Org/Repo).
- **`Logger`**: A unified logger abstraction (`logger.Logger`) with charmbracelet as the default backend for consistent output.
- **`Config`**: The loaded configuration container.
- **`FS`**: An abstraction of the file system (`spf13/afero`), allowing for easy mocking.
- **`Assets`**: A manager for embedded filesystem resources.
- **`Version`**: Current build information (Version, Commit, Date).
- **`ErrorHandler`**: A centralized handler for fatal and non-fatal errors.

## Why use Props?

Instead of using global variables or passing dozens of arguments to every function, you pass the `Props` object. This provides several key benefits:

1. **Testability**: You can easily swap out the `FS` for an in-memory filesystem or provide a mock `Logger` during tests.
2. **Consistency**: Global flags like `--debug` automatically update the `Logger` level inside `Props`, ensuring all components behave consistently.
3. **Extensibility**: Adding new global services to the framework only requires adding them to the `Props` struct.

## Usage in Commands

Every command constructor in GTB accepts `*props.Props`:

```go
func NewCmdExample(props *props.Props) *cobra.Command {
    return &cobra.Command{
        Use: "example",
        Run: func(cmd *cobra.Command, args []string) {
            props.Logger.Info("Hello from Props!")
            // Use props.FS to read a file
            // Use props.Config to get a value
        },
    }
}
```
