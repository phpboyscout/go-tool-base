---
title: Error Handling Patterns
description: Standards for error management within the GTB library and framework.
tags: [errors, library, patterns, errorhandling]
---

# Error Handling Patterns

As a framework, GTB provides the `errorhandling` package which manages how errors are logged and how applications exit.

## Architecture

The system is split into two concerns:
1. **Creation**: Using `github.com/cockroachdb/errors` for rich error objects with stack traces, hints, and structured context.
2. **Handling**: Using the `errorhandling.ErrorHandler` interface for reporting, routed through a central `Execute()` wrapper.

### Creation & Wrapping

Always import `github.com/cockroachdb/errors`.

```go
import "github.com/cockroachdb/errors"
```

!!! important "Avoid Native Errors"
    Avoid using the standard library `fmt.Errorf("%w", err)` or `errors.New`, as they do not capture stack traces. Use `cockroachdb/errors` instead.

```go
// Create a new error
err := errors.New("static error")

// Create a formatted error
err := errors.Newf("invalid port: %d", port)

// Wrap with a message prefix (prefer this over fmt.Errorf)
if err != nil {
    return errors.Wrap(err, "context about failure")
}

// Wrap to add a stack trace without changing the message
if err != nil {
    return errors.WithStack(err)
}
```

### Command Error Handling with `RunE`

GTB commands use Cobra's `RunE` and return errors idiomatically. All errors — including flag parse errors and `PersistentPreRunE` failures — are routed through the central `Execute()` wrapper in `pkg/cmd/root`, which calls `ErrorHandler.Check` at `LevelFatal`.

```go
// ✅ Correct: use RunE and return errors
func NewMyCommand(props *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "mycommand",
        Short: "Description of my command",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runMyCommand(cmd.Context(), props)
        },
    }
}

func runMyCommand(ctx context.Context, props *props.Props) error {
    result, err := performOperation(ctx)
    if err != nil {
        return errors.Wrap(err, "operation failed")
    }
    props.Logger.Info("Command completed", "result", result)
    return nil
}
```

The `Execute()` wrapper in `pkg/cmd/root` handles all the plumbing:

```go
// pkg/cmd/root/execute.go
func Execute(rootCmd *cobra.Command, props *p.Props) {
    rootCmd.SilenceErrors = true
    rootCmd.SilenceUsage = true

    rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
        return errors.WithHintf(err, "Run '%s --help' for usage.", cmd.CommandPath())
    })

    if err := rootCmd.Execute(); err != nil {
        props.ErrorHandler.Check(err, "", errorhandling.LevelFatal)
    }
}
```

`SilenceErrors` and `SilenceUsage` suppress Cobra's own error output, ensuring all output is controlled by `ErrorHandler`.

### Reporting with `ErrorHandler`

Applications built with GTB use an `ErrorHandler` (typically found in `props.ErrorHandler`).

- **`Check`**: Routes an error through the full handler pipeline at the specified level. Used by the `Execute()` wrapper.
- **`Fatal`**: Logs and calls `os.Exit(1)`. Use for errors that must terminate immediately within non-command code.
- **`Error`**: Logs at Error level. Use for non-terminating failures.
- **`Warn`**: Logs at Warn level.

## Error Bubbling Philosophy

Errors should be **bubbled up** to the `cobra.Command` where they are ultimately handled by the central `Execute()` wrapper.

!!! important "Avoid Early Exit"
    Only use `ErrorHandler.Fatal` or `os.Exit` in very specific cases. Forced exits prevent `defer` functions from executing, which can lead to resource leaks or corrupted state (e.g., unfinished file writes or open database connections).

Lower-level library code should focus on creating and wrapping errors with `cockroachdb/errors`, allowing the consumer to decide on the fatality of the error.

```go
// ✅ Correct: bubble errors up via RunE
RunE: func(cmd *cobra.Command, args []string) error {
    return doWork(cmd.Context(), props)
}

// ❌ Avoid: calling Fatal inside business logic
Run: func(cmd *cobra.Command, args []string) {
    props.ErrorHandler.Fatal(doWork())
}
```

## Debugging and Stack Traces

When the logger is in **Debug** mode, `ErrorHandler` logs a full stack trace for every error. Because `cockroachdb/errors` captures the stack at the point the error is created or wrapped, you can also get a detailed trace at any time using `fmt.Sprintf("%+v", err)`. The `%+v` verb includes the error chain, stack frames, hints, details, and any issue links.

```go
// Log the full trace manually (e.g. in a debug utility)
fmt.Sprintf("%+v", err)
```

No type assertion is needed. Any error produced by `cockroachdb/errors` supports `%+v` directly.

## User-Facing Hints

Rather than embedding multi-line suggestion text directly in error messages, attach hints using `errors.WithHint` or the `errorhandling` convenience wrappers. `ErrorHandler` surfaces hints as a structured `hints` field alongside the error message, which keeps the primary message concise while still giving users actionable guidance.

```go
import (
    "github.com/cockroachdb/errors"
    "github.com/phpboyscout/gtb/pkg/errorhandling"
)

// Attach a hint to a new error
err := errors.WithHint(
    errors.New("database connection failed"),
    "Check that the database server is running and the connection string is correct",
)

// Attach a formatted hint
err = errors.WithHintf(err, "expected port in range 1–65535, got %d", port)

// Convenience wrapper: wrap + hint in one call
err = errorhandling.WrapWithHint(err, "failed to connect", "Verify network connectivity and credentials")
```

The `ErrorHandler` automatically extracts and displays all hints when reporting the error — no extra wiring required.

## Predefined Library Errors

The `errorhandling` package exports common errors that trigger specific behaviors in the handler:

- **`ErrNotImplemented`**: Logged as a warning to indicate a stubbed command.
- **`NewErrNotImplemented(issueURL)`**: Creates a richer not-implemented error that includes a link to the tracking issue. `ErrorHandler` detects this and logs the URL so users know where to follow progress.
- **`ErrRunSubCommand`**: Triggers the `Usage()` output for the command, helpful for parent commands that shouldn't be run directly.
