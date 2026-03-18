---
title: "CockroachDB Errors Migration Specification"
description: "Migrate error handling from github.com/go-errors/errors to github.com/cockroachdb/errors for richer error semantics, user-facing hints, structured safe details, and improved stack trace formatting."
date: 2026-02-18
status: IMPLEMENTED
tags:
  - specification
  - error-handling
  - migration
  - cockroachdb-errors
  - errorhandling
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-4.6-opus)
    role: AI drafting assistant
---

# CockroachDB Errors Migration Specification

Authors
:   Matt Cockayne, Claude (claude-4.6-opus) *(AI drafting assistant)*

Date
:   18 February 2026

Status
:   IMPLEMENTED

---

## 1. Overview

### 1.1 Motivation

GTB currently uses `github.com/go-errors/errors` (v1.5.1) for error creation and wrapping. While this library provides stack traces, it lacks several capabilities that would significantly improve the developer and user experience:

- **No user-facing hints or details**: When errors occur, users see raw error messages with no guidance on how to resolve the issue. The current workaround is embedding multi-line suggestion text directly into error messages via `errors.Errorf` or `errors.WrapPrefix`, mixing user-facing content with programmatic error identity.
- **No structured error metadata**: There is no way to attach machine-readable context (telemetry keys, issue tracker links, safe details) to errors without modifying the error message string.
- **Awkward stack trace access**: Extracting stack traces requires a type assertion to `*errors.Error` followed by calling `.ErrorStack()`. The `cockroachdb/errors` library supports standard `%+v` formatting for verbose error output including stack traces.
- **No multi-error support**: Concurrent operations that can produce multiple errors have no first-class way to combine them.
- **No `errors.Is`/`errors.As` on the error library itself**: The `go-errors` library's `errors.Is` and `errors.As` are wrappers, but `cockroachdb/errors` provides these as first-class citizens that work correctly through all wrapper layers, including across the network.
- **No assertion failure markers**: There is no way to distinguish programming errors (invariant violations) from operational errors (expected failure modes).
- **Limited wrapping API**: `WrapPrefix(err, "msg", 0)` requires a skip parameter that is almost always `0`; `cockroachdb/errors` provides the cleaner `Wrap(err, "msg")`.

### 1.2 Proposed Solution

Replace `github.com/go-errors/errors` with `github.com/cockroachdb/errors` throughout the codebase. This is a like-for-like replacement for basic functionality (error creation, wrapping, stack traces, `Is`/`As`) while unlocking a rich set of additional capabilities:

| Capability | `go-errors` | `cockroachdb/errors` |
|---|---|---|
| `errors.New` / `errors.Errorf` | Yes | Yes |
| `errors.Wrap` with message | `WrapPrefix(err, msg, skip)` | `Wrap(err, msg)` |
| `errors.Is` / `errors.As` | Yes (wrappers) | Yes (first-class, network-portable) |
| Stack traces | `.ErrorStack()` | `%+v` formatting |
| User-facing hints | No | `WithHint(err, "try X")` |
| User-facing details | No | `WithDetail(err, "context")` |
| Assertion failures | No | `AssertionFailedf(...)` / `WithAssertionFailure(err)` |
| Multi-error / Join | No | `CombineErrors(a, b)` / `Join(...)` |
| Issue tracker links | No | `WithIssueLink(err, link)` |
| Telemetry keys | No | `WithTelemetry(err, key)` |
| PII-safe details | No | `WithSafeDetails(err, ...)` / `Safe(v)` |
| Unimplemented markers | Manual sentinel | `UnimplementedError(link, msg)` |
| Secondary errors | No | `WithSecondaryError(err, secondary)` |

### 1.3 Terminology

| Term | Definition |
|---|---|
| **Sentinel error** | A package-level `var Err... = errors.New(...)` used for identity comparison with `errors.Is()`. |
| **Hint** | A user-facing suggestion attached to an error via `errors.WithHint()`, retrieved with `errors.FlattenHints()`. |
| **Detail** | A developer-facing contextual string attached via `errors.WithDetail()`, retrieved with `errors.FlattenDetails()`. |
| **Safe detail** | A PII-free string attached via `errors.WithSafeDetails()`, suitable for telemetry and crash reporting. |
| **Assertion failure** | An error denoting a programming bug (invariant violation), created with `errors.AssertionFailedf()`. |
| **Barrier** | An error wrapper (`errors.Handled()` / `errors.Opaque()`) that hides the cause from `errors.Is()`. |

### 1.4 Design Decisions

**D1: Direct replacement, not abstraction layer.**
We will import `github.com/cockroachdb/errors` directly rather than creating an internal wrapper. The `cockroachdb/errors` package is designed as a drop-in replacement and its API is stable. An abstraction layer would add complexity without benefit.

**D2: Retain the `ErrorHandler` interface.**
The `ErrorHandler` interface (`Check`, `Fatal`, `Error`, `Warn`, `SetUsage`) remains the primary mechanism for error reporting in commands. The `StandardErrorHandler` implementation will be enhanced to leverage hints, details, and improved stack trace formatting from `cockroachdb/errors`.

**D3: Preserve sentinel error identity.**
All existing sentinel errors (`ErrNotImplemented`, `ErrRunSubCommand`, `ErrNoFilesFound`, `ErrCommandProtected`, etc.) will be migrated to `cockroachdb/errors` equivalents. Since both libraries define `errors.New()` that returns types compatible with `errors.Is()`, identity comparisons will continue to work.

**D4: Enhance `ErrNotImplemented` with `UnimplementedError`.**
The `cockroachdb/errors` library provides `UnimplementedError(issueLink, msg)` which is purpose-built for this use case and supports issue tracker links. We will migrate `ErrNotImplemented` to use this.

**D5: Use `WithHint` for user-facing recovery suggestions.**
Error messages that currently embed multi-line suggestion text (e.g. `"failed to connect\n\nSuggestions:\n  - Check X\n  - Try Y"`) will be refactored to use `errors.WithHint()`, separating the error identity from the user guidance.

**D6: Stack traces via `%+v` formatting.**
The `StandardErrorHandler` will use `fmt.Sprintf("%+v", err)` for verbose error output in debug mode instead of the current type-assertion-based `.ErrorStack()` approach. This is cleaner and works with any error in the `cockroachdb/errors` chain.

**D7: Remove `go-errors` dependency entirely.**
After migration, `github.com/go-errors/errors` will be removed from `go.mod`. No dual-dependency period.

---

## 2. Public API

### 2.1 ErrorHandler Interface (unchanged)

The `ErrorHandler` interface signature remains unchanged to preserve backwards compatibility for all downstream consumers:

```go
type ErrorHandler interface {
    Check(err error, prefix string, level string, cmd ...*cobra.Command)
    Fatal(err error, prefixes ...string)
    Error(err error, prefixes ...string)
    Warn(err error, prefixes ...string)
    SetUsage(usage func() error)
}
```

### 2.2 StandardErrorHandler (enhanced)

The `StandardErrorHandler` struct remains structurally the same:

```go
type StandardErrorHandler struct {
    Logger *log.Logger
    Help   HelpConfig
    Exit   ExitFunc
    Writer io.Writer
    Usage  func() error
}
```

**Internal behavioural changes:**

1. **Stack trace extraction** switches from `errors.As(err, &es); es.ErrorStack()` to `fmt.Sprintf("%+v", err)` when debug logging is enabled.
2. **Hint extraction**: When an error carries hints (via `errors.WithHint`), the handler will extract and display them below the error message using `errors.FlattenHints()`.
3. **Detail extraction**: When an error carries details (via `errors.WithDetail`), and the logger is at debug level, the handler will extract and display them using `errors.FlattenDetails()`.
4. **Unimplemented detection**: `ErrNotImplemented` detection will use `errors.HasUnimplementedError()` alongside `errors.Is()` for broader compatibility.
5. **Assertion failure detection**: New logic to detect assertion failures via `errors.HasAssertionFailure()` and log them with elevated severity and a distinct prefix.

### 2.3 New Constructor

```go
func New(logger *log.Logger, help HelpConfig, opts ...Option) ErrorHandler
```

A functional options pattern is introduced to allow future extensibility without breaking the constructor signature:

```go
type Option func(*StandardErrorHandler)

func WithExitFunc(exit ExitFunc) Option
func WithWriter(w io.Writer) Option
```

The existing `New(logger, help)` two-argument constructor will be preserved as a convenience that delegates to the options-based constructor with no options. This maintains full backwards compatibility.

### 2.4 Sentinel Errors (migrated)

Existing sentinels migrate to `cockroachdb/errors` equivalents:

```go
import "github.com/cockroachdb/errors"

var (
    ErrRunSubCommand = errors.New("subcommand required")
)
```

`ErrNotImplemented` is replaced with a function that returns a richer error:

```go
func NewErrNotImplemented(issueURL string) error {
    return errors.UnimplementedError(
        errors.IssueLink{IssueURL: issueURL},
        "command not yet implemented",
    )
}

// ErrNotImplemented is retained as a simple sentinel for backwards compatibility
// with existing errors.Is(err, ErrNotImplemented) checks in downstream consumers.
var ErrNotImplemented = errors.New("command not yet implemented")
```

Downstream consumers that use `errors.Is(err, ErrNotImplemented)` will continue to work. The `NewErrNotImplemented` function is offered for callers that want to attach an issue link.

### 2.5 New Error Construction Helpers

The `errorhandling` package will export convenience functions that standardise common error patterns across the codebase:

```go
// WithUserHint attaches a user-facing recovery suggestion to an error.
func WithUserHint(err error, hint string) error {
    return errors.WithHint(err, hint)
}

// WithUserHintf attaches a formatted user-facing recovery suggestion.
func WithUserHintf(err error, format string, args ...interface{}) error {
    return errors.WithHintf(err, format, args...)
}

// WrapWithHint wraps an error with a message and attaches a user-facing hint.
func WrapWithHint(err error, msg string, hint string) error {
    return errors.WithHint(errors.Wrap(err, msg), hint)
}

// NewAssertionFailure creates an error denoting a programming bug.
func NewAssertionFailure(format string, args ...interface{}) error {
    return errors.AssertionFailedf(format, args...)
}
```

These are thin wrappers that exist primarily for discoverability and to establish project conventions. Direct use of `cockroachdb/errors` functions is also acceptable.

### 2.6 Exported Constants (updated)

```go
const (
    LevelFatal    = "fatal"
    LevelError    = "error"
    LevelWarn     = "warn"
    KeyStacktrace = "stacktrace"
    KeyHelp       = "help"
    KeyHints      = "hints"
    KeyDetails    = "details"
)
```

Two new log keys (`KeyHints`, `KeyDetails`) are added for structured log output.

---

## 3. Internal Implementation

### 3.1 Stack Trace Extraction

**Current implementation** (`handling.go`, `Check` method):

```go
var es *errors.Error
if errors.As(err, &es) {
    stacktrace = es.ErrorStack()
}
```

**New implementation:**

```go
if h.Logger.GetLevel() == log.DebugLevel {
    stacktrace = fmt.Sprintf("%+v", err)
}
```

The `%+v` format on `cockroachdb/errors` produces a structured output containing:
- The full error message chain
- Stack traces for every wrapper that captured one
- Hints, details, safe details, assertion markers, issue links

This is richer than the previous `.ErrorStack()` output and requires no type assertion.

### 3.2 Hint and Detail Display

When the `StandardErrorHandler` processes an error, it will check for attached hints and details:

```go
func (h *StandardErrorHandler) logError(err error, prefix, level string) {
    l := h.Logger
    if len(prefix) > 0 {
        l = l.WithPrefix(prefix)
    }

    kvPairs := []any{}

    // Stack trace in debug mode
    if h.Logger.GetLevel() == log.DebugLevel {
        kvPairs = append(kvPairs, KeyStacktrace, fmt.Sprintf("%+v", err))
    }

    // User-facing hints (always displayed when present)
    if hints := errors.FlattenHints(err); hints != "" {
        kvPairs = append(kvPairs, KeyHints, hints)
    }

    // Developer-facing details (debug mode only)
    if h.Logger.GetLevel() == log.DebugLevel {
        if details := errors.FlattenDetails(err); details != "" {
            kvPairs = append(kvPairs, KeyDetails, details)
        }
    }

    // Help/support information
    if h.Help.Slack.Channel != "" && h.Help.Slack.Team != "" {
        kvPairs = append(kvPairs, KeyHelp,
            fmt.Sprintf(SupportMessageFormat, h.Help.Slack.Team, h.Help.Slack.Channel))
    }

    switch level {
    case LevelFatal:
        l.Error(err, kvPairs...)
        h.Exit(1)
    case LevelError:
        l.Error(err, kvPairs...)
    case LevelWarn:
        l.Warn(err, kvPairs...)
    }
}
```

### 3.3 Special Error Handling

The `handleSpecialErrors` method will be updated to leverage `cockroachdb/errors` introspection:

```go
func (h *StandardErrorHandler) handleSpecialErrors(err error, cmd ...*cobra.Command) bool {
    // Unimplemented errors (covers both sentinel and UnimplementedError)
    if errors.Is(err, ErrNotImplemented) || errors.HasUnimplementedError(err) {
        h.Logger.Warn("Command not yet implemented")
        if links := errors.GetAllIssueLinks(err); len(links) > 0 {
            h.Logger.Info("Track progress", "url", links[0].IssueURL)
        }
        return true
    }

    // Subcommand required
    if errors.Is(err, ErrRunSubCommand) {
        if len(cmd) > 0 && cmd[0] != nil {
            cmd[0].SetOut(h.Writer)
            _ = cmd[0].Usage()
        } else if h.Usage != nil {
            _ = h.Usage()
        }
        h.Logger.Warn("Subcommand required")
        return true
    }

    // Assertion failures: log with elevated visibility
    if errors.HasAssertionFailure(err) {
        h.Logger.Error("Internal error (assertion failure)", "error", err)
        if h.Logger.GetLevel() == log.DebugLevel {
            h.Logger.Debug("Assertion detail", KeyStacktrace, fmt.Sprintf("%+v", err))
        }
        return false // still proceed with normal error logging for exit handling
    }

    return false
}
```

### 3.4 Codebase-Wide Import Replacement

Every file that currently imports `github.com/go-errors/errors` will be updated to import `github.com/cockroachdb/errors`. The following API mappings apply:

| `go-errors` pattern | `cockroachdb/errors` equivalent |
|---|---|
| `errors.New("msg")` | `errors.New("msg")` (identical) |
| `errors.Errorf("fmt", args...)` | `errors.Newf("fmt", args...)` or `errors.Errorf("fmt", args...)` |
| `errors.Wrap(err, 0)` | `errors.WithStack(err)` |
| `errors.WrapPrefix(err, "msg", 0)` | `errors.Wrap(err, "msg")` |
| `errors.Is(err, target)` | `errors.Is(err, target)` (identical) |
| `errors.As(err, &target)` | `errors.As(err, &target)` (identical) |
| `var es *errors.Error; errors.As(err, &es); es.ErrorStack()` | `fmt.Sprintf("%+v", err)` |

### 3.5 Files Requiring Changes

The following non-test source files require import and API updates:

**`pkg/` (public library — highest priority):**

| File | Change Summary |
|---|---|
| `pkg/errorhandling/handling.go` | Core migration: import swap, stack trace logic, hint/detail extraction, enhanced special error handling. |
| `pkg/config/container.go` | Replace `errors.Wrap(err, 0).ErrorStack()` with `fmt.Sprintf("%+v", err)`. |
| `pkg/config/load.go` | Replace `errors.WrapPrefix` → `errors.Wrap`, migrate sentinel `ErrNoFilesFound`. |
| `pkg/config/config.go` | Replace `errors.New` (import swap only — API identical). |
| `pkg/props/assets.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `pkg/chat/client.go` | Import swap. |
| `pkg/chat/claude.go` | Replace `errors.New` inline usages, `errors.Errorf` → `errors.Newf`/`errors.Wrapf`. |
| `pkg/chat/openai.go` | Replace `errors.New`, `errors.WrapPrefix` → `errors.Wrap`, `errors.Errorf` → `errors.Newf`. |
| `pkg/chat/gemini.go` | Replace `errors.New`, `errors.As` (for API error type), `errors.Errorf` → `errors.Newf`. |
| `pkg/docs/ask.go` | Import swap, `errors.Errorf` → `errors.Newf`. |
| `pkg/setup/update.go` | Replace `errors.Wrap(err, 0)` → `errors.WithStack(err)`, `errors.WrapPrefix` → `errors.Wrap`, `errors.Is`. |
| `pkg/setup/init.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `pkg/setup/ai/ai.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `pkg/setup/github/ssh.go` | Replace `errors.Wrap(err, 0)` → `errors.WithStack(err)`, `errors.Is`. |
| `pkg/vcs/github.go` | Replace sentinels, `errors.Wrap(err, 0)` → `errors.WithStack(err)`, `errors.WrapPrefix` → `errors.Wrap`. |
| `pkg/vcs/repo.go` | Replace `errors.Wrap(err, 0)` → `errors.WithStack(err)`, `errors.WrapPrefix` → `errors.Wrap`, `errors.Is`. |
| `pkg/controls/controls_test.go` | Test file — update imports. |

**`cmd/` (built-in commands):**

| File | Change Summary |
|---|---|
| `cmd/root/root.go` | Replace `errors.WrapPrefix` → `errors.Wrap`, `errors.Is`, `errors.New`. |
| `cmd/docs/docs.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `cmd/docs/serve.go` | Import swap. |
| `cmd/docs/ask.go` | Replace `errors.Errorf` → `errors.Newf`. |

**`internal/` (generator and agent — lowest priority):**

| File | Change Summary |
|---|---|
| `internal/generator/commands.go` | Replace sentinels, `errors.New`, `errors.Errorf`, `errors.Is`. |
| `internal/generator/skeleton.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `internal/generator/manifest.go` | Replace `errors.New`, `errors.Errorf` → `errors.Newf`. |
| `internal/generator/docs.go` | Replace sentinels, `errors.Errorf`, `errors.WrapPrefix` → `errors.Wrap`. |
| `internal/generator/generator.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `internal/generator/ast.go` | Import swap. |
| `internal/agent/tools.go` | Replace sentinels, `errors.WrapPrefix` → `errors.Wrap`. |
| `internal/cmd/generate/command.go` | Replace `errors.Errorf` → `errors.Newf`. |
| `internal/cmd/generate/flag.go` | Replace `errors.Errorf` → `errors.Newf`. |

**Test files** (all `*_test.go` files that import `go-errors`):

| File | Change Summary |
|---|---|
| `pkg/errorhandling/handling_test.go` | Rewrite tests for new stack trace and hint/detail behaviour. |
| `pkg/errorhandling/handling_debug_test.go` | Update debug stack trace test to verify `%+v` output. |
| `pkg/controls/controls_test.go` | Import swap. |

**Generated mocks:**

| File | Change Summary |
|---|---|
| `mocks/pkg/errorhandling/ErrorHandler.go` | Regenerate via `mockery` (no manual changes). |

### 3.6 Deprecation Cleanup

The existing deprecated package-level functions and variables will be retained in this migration but updated to use `cockroachdb/errors` internally. They remain deprecated:

- `Logger` (package var)
- `Help` (package var)
- `OutputWriter` (package var)
- `Check()` (package func)
- `CheckFatal()` (package func)
- `CheckError()` (package func)
- `CheckWarn()` (package func)
- `SetHelp()` (package func)

---

## 4. Project Structure

### 4.1 Modified Files

No new packages or directories are created. This is a migration within existing files.

```
pkg/errorhandling/
├── handling.go          # Core changes: import swap, enhanced logError, handleSpecialErrors
├── handling_test.go     # Rewritten tests
├── handling_debug_test.go  # Updated debug tests
├── options.go           # NEW: functional options for New()
└── helpers.go           # NEW: convenience wrappers (WithUserHint, NewAssertionFailure, etc.)
```

### 4.2 Dependency Changes

**Added:**
```
github.com/cockroachdb/errors  (latest stable, currently v1.12.0)
```

**Removed:**
```
github.com/go-errors/errors    v1.5.1
```

**Transitive dependencies:** `cockroachdb/errors` brings in `github.com/cockroachdb/redact`, `github.com/cockroachdb/logtags`, `github.com/getsentry/sentry-go`, and protobuf dependencies. While these increase the dependency tree, they are well-maintained and widely used in production Go systems. The Sentry dependency is optional and will not be used unless explicitly configured in a future enhancement.

---

## 5. Generator Impact

The `internal/generator/` package uses `go-errors` for error creation and wrapping in the scaffolding logic. The migration here is mechanical (import swap + API mapping) and does not affect the generated output for downstream CLI projects.

However, the **skeleton templates** (in `internal/generator/assets/skeleton/`) should be reviewed to ensure that any error handling patterns in generated code reference `cockroachdb/errors` rather than `go-errors`. If the skeleton templates contain `go-errors` imports, they must be updated.

---

## 6. Error Handling Patterns (Post-Migration)

### 6.1 Creating Errors

```go
import "github.com/cockroachdb/errors"

// Sentinel errors (package level)
var (
    ErrConfigNotFound = errors.New("configuration file not found")
    ErrInvalidPort    = errors.New("invalid port: must be between 1 and 65535")
)

// Inline errors with formatting
err := errors.Newf("unsupported provider: %s", provider)

// Wrapping with context
return errors.Wrap(err, "failed to load config")

// Wrapping with formatted context
return errors.Wrapf(err, "failed to process file %s", path)
```

### 6.2 Attaching User-Facing Hints

```go
// Attach a hint to guide the user
err := errors.New("database connection failed")
err = errors.WithHint(err, "Check that the database server is running and the connection string is correct.")

// Or combine wrapping with a hint
return errorhandling.WrapWithHint(err, "database init failed",
    "Verify the DATABASE_URL environment variable is set correctly.")
```

### 6.3 Assertion Failures

```go
// When an invariant is violated
if idx < 0 || idx >= len(items) {
    return errors.AssertionFailedf("index %d out of bounds [0, %d)", idx, len(items))
}

// Using the convenience wrapper
return errorhandling.NewAssertionFailure("unexpected nil config after validation")
```

### 6.4 Wrapping vs WithStack

```go
// When adding context to an error — use Wrap
return errors.Wrap(err, "failed to read manifest")

// When re-returning an error without additional context — use WithStack
// (captures a stack trace at this point for debugging)
return errors.WithStack(err)

// When returning a sentinel with a stack trace
return errors.WithStack(ErrConfigNotFound)
```

### 6.5 Multi-Error Handling

```go
// Combining errors from concurrent operations
errA := operationA()
errB := operationB()
return errors.CombineErrors(errA, errB)
```

### 6.6 Error Handling in Commands (unchanged pattern)

```go
Run: func(cmd *cobra.Command, args []string) {
    props.ErrorHandler.Fatal(doWork(props))
},
```

The command-level error handling pattern is unchanged. The `ErrorHandler` now provides richer output automatically.

---

## 7. Testing Strategy

This migration follows a strict TDD approach. Tests are written first for each phase, then implementation is done to make them pass.

### 7.1 Unit Tests for `pkg/errorhandling`

#### 7.1.1 StandardErrorHandler Core Behaviour

| Test Case | Description |
|---|---|
| `TestCheck_NilError` | Verify `Check` is a no-op when `err == nil`. |
| `TestCheck_ErrorLevel` | Verify error is logged at error level with correct message. |
| `TestCheck_WarnLevel` | Verify error is logged at warn level. |
| `TestCheck_FatalLevel` | Verify error is logged and `Exit(1)` is called. |
| `TestCheck_WithPrefix` | Verify prefix is applied to the logger. |
| `TestCheck_MultiplePrefixes` | Verify multiple prefixes are concatenated. |
| `TestFatal_CallsExit` | Verify `Fatal` calls `Exit(1)` with a non-nil error. |
| `TestFatal_NilError` | Verify `Fatal` does nothing when error is nil. |
| `TestError_NonTerminating` | Verify `Error` logs but does not call `Exit`. |
| `TestWarn_NonTerminating` | Verify `Warn` logs at warn level and does not call `Exit`. |

#### 7.1.2 Stack Trace Behaviour

| Test Case | Description |
|---|---|
| `TestCheck_DebugMode_IncludesStackTrace` | Verify that when logger is at DebugLevel, the log output includes stack trace information extracted via `%+v`. |
| `TestCheck_InfoMode_NoStackTrace` | Verify that at InfoLevel, no stack trace key appears in log output. |
| `TestCheck_DebugMode_WrappedError_ShowsFullChain` | Verify that a `errors.Wrap(errors.New("inner"), "outer")` error produces a stack trace containing both layers. |
| `TestCheck_DebugMode_StandardError_NoStackTrace` | Verify that a plain `fmt.Errorf` error does not produce a stack trace in the structured log output (no `%+v` noise). |

#### 7.1.3 Hint and Detail Display

| Test Case | Description |
|---|---|
| `TestCheck_ErrorWithHint_DisplaysHint` | Verify that an error wrapped with `errors.WithHint(err, "try X")` includes the hint in log output at all log levels. |
| `TestCheck_ErrorWithMultipleHints_DisplaysAll` | Verify multiple hints are flattened and displayed. |
| `TestCheck_ErrorWithDetail_DebugMode` | Verify that details are displayed when logger is at DebugLevel. |
| `TestCheck_ErrorWithDetail_InfoMode_Hidden` | Verify that details are NOT displayed when logger is at InfoLevel. |
| `TestCheck_ErrorWithHintAndHelp_BothDisplayed` | Verify that hints and Slack help information coexist in log output. |

#### 7.1.4 Special Error Handling

| Test Case | Description |
|---|---|
| `TestCheck_ErrNotImplemented` | Verify that `ErrNotImplemented` triggers a warning log and returns early. |
| `TestCheck_UnimplementedError_WithIssueLink` | Verify that an error created with `errors.UnimplementedError(link, msg)` triggers the unimplemented handler and logs the issue URL. |
| `TestCheck_ErrRunSubCommand_WithCmd` | Verify that `ErrRunSubCommand` prints usage via the provided `*cobra.Command`. |
| `TestCheck_ErrRunSubCommand_WithSetUsage` | Verify that `ErrRunSubCommand` uses the `SetUsage` function when no command is provided. |
| `TestCheck_AssertionFailure_ElevatedLogging` | Verify that an assertion failure error is logged with error-level severity and includes the assertion marker. |
| `TestCheck_WrappedErrNotImplemented` | Verify that `errors.Wrap(ErrNotImplemented, "ctx")` is still detected as ErrNotImplemented via `errors.Is`. |

#### 7.1.5 Help Configuration

| Test Case | Description |
|---|---|
| `TestCheck_HelpConfig_DisplaysSupportMessage` | Verify that Slack help info is appended to log output when configured. |
| `TestCheck_HelpConfig_Empty_NoSupportMessage` | Verify that no support message appears when HelpConfig is empty. |
| `TestSetHelp_UpdatesDefaultHandler` | Verify the deprecated `SetHelp` function updates the default handler. |

#### 7.1.6 Constructor and Options

| Test Case | Description |
|---|---|
| `TestNew_DefaultOptions` | Verify `New(logger, help)` creates a handler with `os.Exit` and default writer. |
| `TestNew_WithExitFunc` | Verify `New(logger, help, WithExitFunc(fn))` uses the custom exit function. |
| `TestNew_WithWriter` | Verify `New(logger, help, WithWriter(w))` uses the custom writer. |

#### 7.1.7 Convenience Helpers

| Test Case | Description |
|---|---|
| `TestWithUserHint_AttachesHint` | Verify `WithUserHint(err, "msg")` produces an error where `errors.FlattenHints` returns the hint. |
| `TestWithUserHintf_FormatsHint` | Verify formatted hint with arguments. |
| `TestWrapWithHint_WrapsAndAttachesHint` | Verify the error message is wrapped AND a hint is attached. |
| `TestNewAssertionFailure_MarksAssertion` | Verify `errors.HasAssertionFailure` returns true for the resulting error. |

#### 7.1.8 Backwards Compatibility

| Test Case | Description |
|---|---|
| `TestDeprecated_Check_DelegatesToDefault` | Verify the package-level `Check` function delegates to `DefaultErrorHandler`. |
| `TestDeprecated_CheckFatal_DelegatesToDefault` | Verify `CheckFatal` delegates to `DefaultErrorHandler.Fatal`. |
| `TestDeprecated_CheckError_DelegatesToDefault` | Verify `CheckError` delegates to `DefaultErrorHandler.Error`. |
| `TestDeprecated_CheckWarn_DelegatesToDefault` | Verify `CheckWarn` delegates to `DefaultErrorHandler.Warn`. |
| `TestDeprecated_SetUsage_DelegatesToDefault` | Verify `SetUsage` delegates to `DefaultErrorHandler.SetUsage`. |

### 7.2 Integration Tests

| Test Case | Description |
|---|---|
| `TestErrorHandler_FullChain_WrapWithHintAndHelp` | End-to-end: create an error, wrap it, add a hint, process through `ErrorHandler.Error` with HelpConfig, verify complete log output structure. |
| `TestErrorHandler_CombinedErrors` | Verify that `errors.CombineErrors` produces correct output when processed by the handler. |
| `TestErrorHandler_AssertionInFatal` | Verify that an assertion failure processed via `Fatal` logs the assertion detail and exits. |

### 7.3 Codebase-Wide Regression Tests

After the migration, the full test suite (`go test -race ./...`) must pass. No new test files are needed for the mechanical import-swap changes in `pkg/`, `cmd/`, and `internal/` — the existing tests validate that error creation and wrapping behaviour is preserved.

### 7.4 Test Structure

All new tests follow project conventions:

- Table-driven tests where applicable
- Parallel execution (`t.Parallel()`)
- `bytes.Buffer` capture for log output verification
- Mock exit functions for `Fatal` testing
- `testify/assert` for assertions
- `>90%` coverage target for `pkg/errorhandling`

---

## 8. Migration & Compatibility

### 8.1 Backwards Compatibility

| Concern | Impact | Mitigation |
|---|---|---|
| `errors.Is(err, ErrNotImplemented)` | Still works — `ErrNotImplemented` remains a sentinel created with `errors.New`. | None needed. |
| `errors.Is(err, ErrRunSubCommand)` | Still works — same rationale. | None needed. |
| `ErrorHandler` interface | Unchanged. | None needed. |
| `New(logger, help)` constructor | Preserved. New options-based variant is additive. | None needed. |
| Deprecated package functions | Preserved and still functional. | None needed. |
| Mock: `MockErrorHandler` | Regenerated via `mockery` — interface unchanged, so mock signature is identical. | Run `mockery` after migration. |
| `*errors.Error` type assertions in downstream code | Breaking if downstream code performs `var e *goerrors.Error; errors.As(err, &e)`. | Document in migration guide. This pattern is only used internally in `pkg/config/container.go` and `pkg/errorhandling/handling.go`, both of which are migrated. |

### 8.2 Breaking Changes

This migration introduces **no breaking changes** to the public API surface (`pkg/`). The `ErrorHandler` interface, constructor signatures, sentinel errors, and all exported types remain compatible.

The only potentially breaking change is for downstream consumers who:
1. Import `github.com/go-errors/errors` independently and perform type assertions on errors returned by GTB functions. This is an uncommon pattern and can be addressed in release notes.

### 8.3 Versioning

This is a **minor version bump** (backward-compatible enhancement). The new dependency (`cockroachdb/errors`) is an addition, and the removal of `go-errors` does not affect the public API.

### 8.4 Migration Guide for Downstream Consumers

A section should be added to `docs/upgrading/` with the following guidance:

1. Replace `github.com/go-errors/errors` imports with `github.com/cockroachdb/errors`.
2. Replace `errors.WrapPrefix(err, "msg", 0)` with `errors.Wrap(err, "msg")`.
3. Replace `errors.Wrap(err, 0)` with `errors.WithStack(err)`.
4. Replace `.ErrorStack()` calls with `fmt.Sprintf("%+v", err)`.
5. Replace `errors.Errorf(...)` with `errors.Newf(...)` (or keep `errors.Errorf` — both work).
6. Add `errors.WithHint(err, "hint")` where user-facing suggestions are appropriate.

---

## 9. Documentation Updates

The following documentation files must be updated to reflect the new error library:

| File | Changes |
|---|---|
| `docs/development/error-handling.md` | Replace all `go-errors` references with `cockroachdb/errors`. Add sections on hints, details, assertion failures. Update API examples. |
| `docs/concepts/error-handling.md` | Update history section. Add coverage of new features (hints, details, assertions). |
| `docs/components/error-handling.md` | Update all code examples. Add new patterns section. Update best practices. |
| `docs/development/index.md` | Update "Error Wrapping" example in Architecture Principles section. |
| `docs/concepts/interface-design.md` | Update ErrorHandler section noting new `cockroachdb/errors` integration. |
| `.agent/skills/gtb-dev/SKILL.md` | Update error handling guidance for AI agents. |
| `.agent/workflows/gtb-library-contribution.md` | Update error handling references. |
| `docs/upgrading/` | New migration guide document (see section 8.4). |

---

## 10. Future Considerations

The following items are **out of scope** for this specification but are enabled by the migration:

1. **Sentry integration**: `cockroachdb/errors` has built-in Sentry reporting via `errors.BuildSentryReport()`. A future spec could add opt-in crash reporting.
2. **Error domains**: Attaching package-origin domains to errors for better triage in large systems.
3. **Telemetry keys**: Attaching telemetry metadata to errors for observability pipelines.
4. **Network portability**: Encoding/decoding errors via protobuf for distributed CLI tooling scenarios.
5. **PII-safe reporting**: Leveraging `errors.Safe()` and `GetSafeDetails()` for automated PII stripping in log aggregation.
6. **Custom error types with protobuf encoding**: Building domain-specific error types (e.g. `ValidationError`, `ConfigError`) that are network-portable.

---

## 11. Implementation Phases

### Phase 1: Core `errorhandling` Package Migration

**Scope:** `pkg/errorhandling/` only.

**Steps (TDD):**

1. Write all unit tests from section 7.1 (they will fail — `cockroachdb/errors` is not yet imported).
2. Add `github.com/cockroachdb/errors` to `go.mod`.
3. Create `pkg/errorhandling/options.go` with functional options types.
4. Create `pkg/errorhandling/helpers.go` with convenience functions.
5. Update `pkg/errorhandling/handling.go`:
   - Replace import.
   - Update `Check` / `logError` / `handleSpecialErrors`.
   - Add new `KeyHints` / `KeyDetails` constants.
   - Update `New` constructor to accept options.
6. Verify all tests pass.
7. Regenerate mocks with `mockery`.
8. Run `golangci-lint run --fix`.

**Acceptance criteria:**
- All section 7.1 tests pass.
- `go test -race ./pkg/errorhandling/...` succeeds.
- Coverage >= 90% for `pkg/errorhandling`.

### Phase 2: Public Library Package Migration (`pkg/`)

**Scope:** All `pkg/` packages except `pkg/errorhandling/` (already done in Phase 1).

**Steps (TDD):**

1. For each package (`config`, `props`, `chat`, `docs`, `setup`, `vcs`, `controls`):
   a. Update existing tests to use `cockroachdb/errors` imports.
   b. Run tests — they should fail where `go-errors`-specific APIs are used.
   c. Update source files with the API mappings from section 3.4.
   d. Verify tests pass.
2. Where appropriate, enhance error returns with `errors.WithHint` for user-facing errors (e.g. config loading failures, GitHub authentication errors, AI provider errors).
3. Run `go test -race ./pkg/...`.
4. Run `golangci-lint run --fix ./pkg/...`.

**Acceptance criteria:**
- All existing `pkg/` tests pass with `cockroachdb/errors`.
- No remaining imports of `github.com/go-errors/errors` in `pkg/`.
- User-facing errors in `config`, `setup`, and `chat` packages include hints where appropriate.

### Phase 3: Command and Internal Package Migration (`cmd/`, `internal/`)

**Scope:** `cmd/root/`, `cmd/docs/`, `internal/generator/`, `internal/agent/`, `internal/cmd/`.

**Steps (TDD):**

1. Update existing tests to use `cockroachdb/errors` imports.
2. Update source files with the API mappings from section 3.4.
3. Review skeleton templates in `internal/generator/assets/skeleton/` for `go-errors` references and update.
4. Run `go test -race ./cmd/... ./internal/...`.
5. Run `golangci-lint run --fix ./cmd/... ./internal/...`.

**Acceptance criteria:**
- All existing `cmd/` and `internal/` tests pass with `cockroachdb/errors`.
- No remaining imports of `github.com/go-errors/errors` anywhere in the codebase.

### Phase 4: Cleanup and Documentation

**Scope:** Dependency removal, documentation, migration guide.

**Steps:**

1. Remove `github.com/go-errors/errors` from `go.mod` and run `go mod tidy`.
2. Run `mockery` to regenerate all mocks.
3. Run full verification: `go test -race ./...`, `golangci-lint run --fix`.
4. Update all documentation files listed in section 9.
5. Create migration guide in `docs/upgrading/`.
6. Update `.agent/skills/` and `.agent/workflows/` files.

**Acceptance criteria:**
- `github.com/go-errors/errors` does not appear in `go.mod` or `go.sum`.
- Full test suite passes: `go test -race ./...`.
- Linter passes: `golangci-lint run`.
- All documentation references `cockroachdb/errors`.
- Migration guide exists in `docs/upgrading/`.
- The [Verification Checklists](../verification-checklists.md) are fully satisfied.
