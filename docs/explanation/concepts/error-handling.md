---
title: Centralized Error Handling
description: How GTB manages errors, logging, and user-facing help.
date: 2026-02-17
tags: [concepts, errors, logic, logging]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Centralized Error Handling

GTB implements a centralized error handling strategy to ensure consistent terminal output, standardized logging, and helpful support information across all CLI tools.

## History & Rationale

The design of the `ErrorHandler` was driven by two primary requirements:

1. **Observability**: We needed a way to display detailed debugging information, specifically full stack traces, whenever an error occurs in a development or troubleshooting context. This led to the adoption of `github.com/cockroachdb/errors` for error creation/wrapping — providing stack traces, user-facing hints, and structured details — alongside the unified `logger` package (with charmbracelet as the default backend) for rich, structured terminal output.
2. **Consistent Output**: We route all errors — runtime errors, flag parse errors, and pre-run failures — through a single `Execute()` wrapper that calls `ErrorHandler.Check`. This suppresses Cobra's own error printing and ensures all output is produced by GTB's structured logger.

## The ErrorHandler Interface

At the core of this pattern is the `ErrorHandler` interface (found in `pkg/errorhandling`):

```go
type ErrorHandler interface {
    Check(err error, prefix string, level string, cmd ...*cobra.Command)
    Fatal(err error, prefixes ...string)
    Error(err error, prefixes ...string)
    Warn(err error, prefixes ...string)
    SetUsage(usage func() error)
}
```

### Key Methods

- **`Check`**: The primary engine for error processing. It handles special error types, extracts stack traces, hints, and details (via `cockroachdb/errors`), and logs the result. Called by the `Execute()` wrapper for all errors returned from `RunE`.
- **`Fatal`**: Logs an error at the fatal level and terminates the application with an exit code of 1.
- **`Error` / `Warn`**: Logs errors at their respective levels without terminating the process.

## The Execute Wrapper

GTB commands use Cobra's `RunE` and return errors idiomatically. The `Execute()` function in `pkg/cmd/root` acts as the central dispatcher — it silences Cobra's own error output, adds a `--help` hint to flag parse errors, and routes any error returned from `RunE` through `ErrorHandler.Check` at `LevelFatal`.

```go
// In your generated main.go:
func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())
    pkgRoot.Execute(rootCmd, p)
}
```

This means commands simply return errors:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    return runMyCommand(cmd.Context(), props)
}
```

## Special Error Types

The framework defines several "sentinel" errors that trigger specific cross-cutting behaviors:

- **`ErrNotImplemented`**: Automatically logs a warning indicating that the command is still under development.
- **`ErrRunSubCommand`**: Triggered when a parent command is run without a required subcommand. The framework automatically prints the command's usage instructions.

## Help Integration

The `errorhandling` package supports pluggable help configuration through the `HelpConfig` interface. When an error occurs, `ErrorHandler` calls `HelpConfig.SupportMessage()` and appends the result to the error output, directing users to the appropriate support channel.

Two built-in implementations are provided:

```go
// Slack support channel
props.Tool.Help = errorhandling.SlackHelp{
    Team:    "Engineering",
    Channel: "#mytool-support",
}

// Microsoft Teams support channel
props.Tool.Help = errorhandling.TeamsHelp{
    Team:    "Engineering",
    Channel: "Support",
}
```

When `Tool.Help` is `nil`, no support message is shown.

## Usage in Commands

Commands return errors via `RunE`; the `Execute()` wrapper routes them through `ErrorHandler`:

```go
func NewMyCommand(props *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "mycommand",
        Short: "Does work",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runMyCommand(cmd.Context(), props)
        },
    }
}

func runMyCommand(ctx context.Context, props *props.Props) error {
    if err := props.ErrorHandler.SetUsage(cmd.Usage); err != nil { ... }
    // return errors directly — Execute() handles fatal routing
    return doWork(ctx)
}
```

For non-fatal errors (log and continue), call `ErrorHandler.Error` or `ErrorHandler.Warn` inside the function body and return `nil`.

## Best Practices

- **Wrap Errors**: Use `github.com/cockroachdb/errors` to wrap errors with stack traces and user-facing hints. The `ErrorHandler` extracts and logs traces, hints, and details automatically when debug mode is enabled.
- **Return from RunE**: Return errors from `RunE` instead of calling `os.Exit` directly. The `Execute()` wrapper ensures they reach `ErrorHandler`.
- **Avoid os.Exit**: Do not call `os.Exit` directly in your business logic. Only `ErrorHandler.Fatal` should call `os.Exit`, and only when there is no other option (e.g., early termination before Cobra has set up).
- **Consistent Prefixes**: Use descriptive prefix/wrap messages (e.g., `errors.Wrap(err, "git clone")`) to help users identify where the failure occurred.
- **Attach Hints**: Use `errors.WithHint` to attach actionable recovery suggestions that will be surfaced by `ErrorHandler` regardless of log level.
