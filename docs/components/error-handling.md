---
title: Error Handling
description: Centralized error handling system with structured logging, stack traces, and user-friendly messages.
date: 2026-02-16
tags: [components, error-handling, errors, logging]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Error Handling

The Error Handling component provides a centralized, structured approach to error management throughout GTB applications. It emphasizes consistent error handling patterns, proper logging integration, and user-friendly error messages — all routed through a single `Execute()` entry point that calls `ErrorHandler.Check`.

## Overview

GTB uses a custom error handling system built around the `errorhandling` package, which provides enhanced error handling capabilities including stack traces, structured logging, user-facing hints, and consistent error reporting. The system is powered by `github.com/cockroachdb/errors`, which captures stack traces automatically, supports user-facing hints and developer details, and produces rich diagnostic output via `fmt.Sprintf("%+v", err)`.

## Core Philosophy

GTB commands use Cobra's `RunE` and return errors idiomatically. A central `Execute()` wrapper in `pkg/cmd/root` silences Cobra's own error output, adds a `--help` hint to flag parse errors, and routes any returned error through `ErrorHandler.Check` at `LevelFatal`. This ensures all errors — runtime, flag parse, and pre-run failures — are handled consistently.

## The errorhandling Package

### Core Interface

```go
type ErrorHandler interface {
    Check(err error, prefix string, level string, cmd ...*cobra.Command)
    Fatal(err error, prefixes ...string)
    Error(err error, prefixes ...string)
    Warn(err error, prefixes ...string)
    SetUsage(usage func() error)
}
```

### Creating an ErrorHandler

```go
import "github.com/phpboyscout/gtb/pkg/errorhandling"

// No help channel
props.ErrorHandler = errorhandling.New(logger, nil)

// With Slack support channel
props.ErrorHandler = errorhandling.New(logger, errorhandling.SlackHelp{
    Team:    "Platform",
    Channel: "#platform-help",
})

// With Microsoft Teams support channel
props.ErrorHandler = errorhandling.New(logger, errorhandling.TeamsHelp{
    Team:    "Platform",
    Channel: "Support",
})
```

### Usage Patterns

#### 1. Command Error Handling

The standard pattern for command implementation:

```go
import "github.com/cockroachdb/errors"

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
    if len(args) == 0 {
        return ErrInsufficientArgs
    }

    result, err := performOperation(ctx, args[0])
    if err != nil {
        return errors.Wrap(err, "operation failed")
    }

    props.Logger.Info("Command completed", "result", result)
    return nil
}

var ErrInsufficientArgs = errors.New("at least one argument is required")
```

#### 2. Non-Fatal Error Handling

For errors that should be logged but not terminate the program:

```go
func performBackgroundTasks(props *props.Props) {
    // Log errors but continue execution
    props.ErrorHandler.Error(updateCache(), "cache-update")
    props.ErrorHandler.Warn(cleanupTempFiles(), "cleanup")
}
```

#### 3. The Execute Wrapper

Your generated `main.go` uses `pkgRoot.Execute` which routes all `RunE` errors through `ErrorHandler`:

```go
func main() {
    rootCmd, p := root.NewCmdRoot(version.Get())
    pkgRoot.Execute(rootCmd, p)
}
```

`Execute` sets `SilenceErrors` and `SilenceUsage` on the root command so Cobra never prints errors itself, and adds a `--help` hint to all flag parse errors.

## Advanced Features

### Stack Trace Support

When debug logging is enabled, the errorhandling package automatically includes formatted stack traces:

```go
// Enable debug logging to see stack traces
props.Logger.SetLevel(log.DebugLevel)

// This error will include a clean stack trace in debug mode
props.ErrorHandler.Error(errors.New("something went wrong"))

// Render a full trace manually at any time
fmt.Sprintf("%+v", err)
```

- Stack captured automatically on error creation and wrapping
- Only shown in the structured log when debug logging is enabled
- Rich `%+v` formatting includes hints, details, and issue links

### User-Facing Hints

Attach hints using `errors.WithHint` or `errorhandling.WrapWithHint`. `ErrorHandler` surfaces hints as a dedicated `hints` field in the structured log output.

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

// Convenience wrapper: wrap an error with a message and a hint in one call
err = errorhandling.WrapWithHint(err, "failed to connect", "Verify network connectivity and credentials")
```

Hints are always displayed when present, regardless of log level.

### Help Integration

The `HelpConfig` interface allows plugging in a support channel message that is appended to every error output:

```go
type HelpConfig interface {
    SupportMessage() string
}
```

Two built-in implementations are provided:

**`SlackHelp`** — directs users to a Slack channel:

```go
errorhandling.SlackHelp{
    Team:    "DevOps Team",
    Channel: "#support",
}
// Output: "For assistance, contact DevOps Team via Slack channel #support"
```

**`TeamsHelp`** — directs users to a Microsoft Teams channel:

```go
errorhandling.TeamsHelp{
    Team:    "DevOps Team",
    Channel: "Support",
}
// Output: "For assistance, contact DevOps Team via Microsoft Teams channel Support"
```

Pass `nil` when no help channel is configured:

```go
props.ErrorHandler = errorhandling.New(logger, nil)
```

## Best Practices

Always import and use `cockroachdb/errors` for error creation and wrapping in GTB applications:

```go
import "github.com/cockroachdb/errors"
```

### 1. Error Wrapping

```go
func loadConfig(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return errors.Wrap(err, fmt.Sprintf("failed to read config file %s", path))
    }

    var config Config
    if err := yaml.Unmarshal(data, &config); err != nil {
        return errors.Wrap(err, fmt.Sprintf("failed to parse config file %s", path))
    }

    return nil
}
```

**Why cockroachdb/errors over standard library:**

- **Stack Traces**: Automatic stack trace capture at error creation points
- **Better Debugging**: Stack traces available via `%+v` and in debug log output
- **Consistent Integration**: Works seamlessly with the `errorhandling` package
- **Rich Error Context**: Preserves the full error chain with hints, details, and stack information

**Concrete Errors vs fmt.Errorf:**

```go
// ✅ Preferred: Predefined concrete error variables
var (
    ErrInputEmpty     = errors.New("input cannot be empty")
    ErrInvalidPort    = errors.New("invalid port: must be between 1 and 65535")
    ErrConfigNotFound = errors.New("configuration file not found")
)

// ✅ Preferred: Use concrete errors with Wrap for dynamic content
func validatePort(port int) error {
    if port < 1 || port > 65535 {
        return errors.Wrap(ErrInvalidPort, fmt.Sprintf("port %d", port))
    }
    return nil
}

// ❌ Avoid: fmt.Errorf doesn't provide stack traces
func badValidation(input string) error {
    if input == "" {
        return fmt.Errorf("input cannot be empty") // No stack trace
    }
    return nil
}
```

### 2. Contextual Error Messages

```go
func connectToDatabase(config DatabaseConfig) error {
    conn, err := sql.Open(config.Driver, config.ConnectionString)
    if err != nil {
        return errorhandling.WrapWithHint(
            err,
            "failed to connect to database",
            "Check that the database server is running, the connection string is correct, and network connectivity is available",
        )
    }
    defer conn.Close()

    if err := conn.Ping(); err != nil {
        return errors.WithHint(
            errors.Wrap(err, "database connection test failed"),
            "The connection was established but the database is not responding — check server health",
        )
    }

    return nil
}
```

### 3. Error Message Guidelines

- **Be Specific**: Include relevant details like file paths, URLs, or configuration keys
- **Be Actionable**: Use `errors.WithHint` to suggest concrete steps the user can take
- **Be Consistent**: Use consistent formatting and terminology across all error messages
- **Wrap, don't replace**: Always add context when propagating errors up the call stack

**Error Creation Hierarchy:**

- **First Choice**: `errors.New("simple message")` for static error messages
- **Second Choice**: `errors.Newf("formatted %s", value)` for dynamic error messages
- **For Wrapping**: `errors.Wrap(err, "context")` when adding context to existing errors
- **For Stack Only**: `errors.WithStack(err)` when you only need to capture the stack without changing the message
- **For Hints**: `errors.WithHint(err, "hint")` or `errorhandling.WrapWithHint(err, "msg", "hint")`
- **Never Use**: `fmt.Errorf()` — doesn't provide stack traces and breaks consistency

## Integration with Built-in Commands

The built-in commands (`init`, `version`, `update`, `docs`) all use `RunE` and return errors:

```go
// pkg/cmd/initialise/init.go
RunE: func(cmd *cobra.Command, _ []string) error {
    location, err := setup.Initialise(props, setup.InitOptions{...})
    if err != nil {
        return errors.Wrap(err, "failed to initialise configuration")
    }
    props.Logger.Infof("Configuration initialised in %s", location)
    return nil
},
```

## Testing Error Handling

### Testing Error Conditions

```go
func TestLoadConfig_InvalidFile(t *testing.T) {
    err := loadConfig("/nonexistent/file.yaml")

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "failed to read config file")

    // Verify the stack trace is available via %+v
    stackTrace := fmt.Sprintf("%+v", err)
    assert.NotEmpty(t, stackTrace)
}
```

### Testing Error Handler Integration

```go
func TestCommandErrorHandling(t *testing.T) {
    var logBuffer bytes.Buffer
    logger := log.NewWithOptions(&logBuffer, log.Options{Level: log.ErrorLevel})

    h := errorhandling.New(logger, nil)

    testErr := errors.New("test error with stack trace")
    h.Error(testErr)

    assert.Contains(t, logBuffer.String(), "test error with stack trace")
}
```

### Testing Help Message Output

```go
func TestSlackHelp_AppearsInOutput(t *testing.T) {
    var buf bytes.Buffer
    logger := log.NewWithOptions(&buf, log.Options{Level: log.InfoLevel, Formatter: log.TextFormatter})

    h := errorhandling.New(logger, errorhandling.SlackHelp{Team: "Platform", Channel: "#alerts"})
    h.Error(errors.New("something went wrong"))

    assert.Contains(t, buf.String(), "For assistance, contact Platform via Slack channel #alerts")
}
```

## Summary

The GTB error handling system provides:

1. **Consistent Patterns**: All commands use `RunE` and return errors; the `Execute()` wrapper handles fatal routing
2. **Better User Experience**: Errors include context, hints, and optional help channel information
3. **Developer Friendly**: Stack traces and structured logging for debugging
4. **Pluggable Help**: `HelpConfig` interface supports Slack, Microsoft Teams, or custom implementations
5. **Integration Ready**: Works seamlessly with the logging and configuration systems
