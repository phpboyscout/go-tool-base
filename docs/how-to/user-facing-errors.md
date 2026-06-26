---
title: Write User-Facing Errors with Hints
description: How to use cockroachdb/errors and GTB's ErrorHandler to produce actionable error messages with recovery hints.
date: 2026-03-25
tags: [how-to, errors, error-handling, hints, cockroachdb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Write User-Facing Errors with Hints

GTB uses `github.com/cockroachdb/errors` throughout. It provides richer error metadata than the standard library: stack traces, hints (user-facing recovery suggestions), details (debug information), and special error classes (unimplemented, assertion failure).

The `ErrorHandler` in `Props` bridges these errors to the logger with the right level and output format.

---

## Basic Error Creation and Wrapping

Use `cockroachdb/errors` everywhere — not `fmt.Errorf`, not `errors.New` from the standard library:

```go
import "github.com/cockroachdb/errors"

// Create a new error
return errors.New("config file not found")

// Wrap with context (adds stack frame)
return errors.Wrap(err, "failed to open config file")

// Wrap with formatted context
return errors.Wrapf(err, "failed to open config file at %s", path)

// New with format
return errors.Newf("unsupported provider: %s", provider)
```

---

## Attaching User-Facing Hints

A hint is a short, actionable suggestion shown to the user alongside the error message. Use it to tell the user *what to do next*, not to describe the error:

```go
import (
    "github.com/cockroachdb/errors"
    "gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
)

// Attach a hint to an existing error
err = errorhandling.WithUserHint(err,
    "Run 'mytool init' to create a default configuration file.")

// Attach a formatted hint
err = errorhandling.WithUserHintf(err,
    "Set the %s environment variable or run 'mytool init --token'.", "GITHUB_TOKEN")

// Wrap and hint in one call
err = errorhandling.WrapWithHint(err,
    "failed to authenticate",
    "Check that your GITHUB_TOKEN is valid and has the required scopes.")
```

When `ErrorHandler` logs the error, hints appear as a `hints=` key in the log output:

```
ERROR failed to authenticate  error=401 Unauthorized  hints=Check that your GITHUB_TOKEN is valid and has the required scopes.
```

---

## Attaching Debug Details

Details appear only in debug mode (`--debug` / `log.level: debug`). Use them for information that helps developers diagnose problems but would confuse end users:

```go
err = errors.WithDetail(err,
    fmt.Sprintf("HTTP response body: %s", body))
```

---

## Using `ErrorHandler` in Commands

`Props.ErrorHandler` provides three severity levels:

```go
// Fatal: logs the error and exits with code 1
p.ErrorHandler.Fatal(err)
p.ErrorHandler.Fatal(err, "database")  // with prefix: "ERROR [database] ..."

// Error: logs but continues execution
p.ErrorHandler.Error(err)

// Warn: logs at warning level, execution continues
p.ErrorHandler.Warn(err, "config")
```

`Check` is the lower-level method used when you want to choose the level dynamically:

```go
p.ErrorHandler.Check(err, "setup", errorhandling.LevelFatal, cmd)
```

The typical command pattern:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    result, err := doWork(args)
    if err != nil {
        // Add a hint before handing to ErrorHandler
        err = errorhandling.WithUserHint(err,
            "Ensure the service is running: mytool service start")
        p.ErrorHandler.Fatal(err, "work")
        return nil  // Fatal already exits; return nil to satisfy cobra
    }
    // ...
},
```

---

## Sentinel Errors and `errors.Is`

Define package-level sentinel errors for conditions your callers need to distinguish:

```go
var (
    ErrNotConfigured = errors.New("feature is not configured")
    ErrTokenExpired  = errors.New("authentication token has expired")
)

// Wrap preserves Is() compatibility
return errors.Wrap(ErrTokenExpired, "GitHub API request failed")

// Callers can test
if errors.Is(err, ErrTokenExpired) {
    return errorhandling.WithUserHint(err, "Run 'mytool init --github' to re-authenticate.")
}
```

---

## Unimplemented Commands

For commands that are planned but not yet built:

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"

RunE: func(cmd *cobra.Command, args []string) error {
    return errorhandling.NewErrNotImplemented("https://github.com/my-org/mytool/issues/42")
},
```

`ErrorHandler` recognises this error class and logs a friendly "Command not yet implemented" message with the issue tracker link, instead of printing a stack trace.

---

## Assertion Failures (Programming Bugs)

For conditions that indicate a bug in your code rather than a user error:

```go
if len(items) == 0 {
    return errorhandling.NewAssertionFailure(
        "processItems called with empty slice; this is a bug")
}
```

`ErrorHandler` logs these at error level and includes the stack trace regardless of the log level setting.

---

## Configuring the Help Channel

If your tool has a support channel (Slack, Teams, email), configure it on the `Tool.Help` field in `main.go`. It appears automatically on every error log:

```go
tool := props.Tool{
    // ...
    Help: errorhandling.SlackHelp{
        Team:    "my-team",
        Channel: "#my-tool-support",
    },
}
```

All errors will then include:

```
ERROR something went wrong  error=...  help=For assistance, contact my-team via Slack channel #my-tool-support
```

---

## Stack Traces in Debug Mode

Full stack traces are only shown when `log.level: debug`. This keeps normal output clean while giving developers the full context they need:

```bash
mytool --debug mycommand
# ERROR failed to load config  error=...  stacktrace=(full trace)
```

---

## Testing

Verify that errors have the expected hints:

```go
err := errorhandling.WithUserHint(
    errors.New("token missing"),
    "Set the GITHUB_TOKEN environment variable",
)

hints := errors.FlattenHints(err)
assert.Contains(t, hints, "GITHUB_TOKEN")
```

Use `WithExitFunc` to prevent `Fatal` from actually calling `os.Exit` in tests:

```go
var exitCode int
handler := errorhandling.New(logger.NewNoop(), nil,
    errorhandling.WithExitFunc(func(code int) {
        exitCode = code
    }),
)

handler.Fatal(someErr)
assert.Equal(t, 1, exitCode)
```

---

## Related Documentation

- **[Error Handling component](../explanation/components/error-handling.md)** — `ErrorHandler` interface and `StandardErrorHandler`
- **[Centralized Error Handling](../explanation/components/error-handling.md)** — architectural rationale
- **[Sentinel errors](../explanation/components/errors.md)** — catalogue of framework sentinel errors
