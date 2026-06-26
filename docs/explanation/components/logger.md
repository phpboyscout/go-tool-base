---
title: Logger
description: Unified logging abstraction with charmbracelet, slog, and noop backends.
date: 2026-03-25
tags: [components, logger, logging, slog, charmbracelet]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Logger

`pkg/logger` provides a unified logging interface for all GTB packages. Every
component accepts `logger.Logger` rather than a concrete type, keeping the
framework backend-agnostic and fully testable.

## Overview

All GTB packages receive a `logger.Logger` through the `Props` container.
Three built-in backends are provided:

| Backend | Factory | Best For |
|---------|---------|----------|
| charmbracelet | `NewCharm(w, opts...)` | CLI applications — coloured, styled terminal output |
| slog | `NewSlog(handler)` | Observability stacks — OpenTelemetry, Datadog, Zap, Zerolog |
| noop | `NewNoop()` | Tests — discards all output |

---

## Why a Logger Interface?

Go's `log/slog` is the standard library logger and is excellent for server-side
code, but CLI tools have different requirements:

- **Coloured, styled terminal output** — `slog` produces plain text or JSON; CLI
  users expect styled output
- **Testable by construction** — `slog` ships no first-class test double; CLI
  tests need to silence logs or capture them for assertions. `logger.NewNoop()`
  discards output, and `NewCharm(w, …)` writes to any `io.Writer` you inject
  (e.g. a `bytes.Buffer`)
- **Printf-style convenience** — `slog` has no `Infof`, `Errorf` etc.
- **Unlevelled output** — `slog` always attaches a level; CLI tools need to print
  version strings, release notes, and prompts without a level prefix

The `logger.Logger` interface exposes all of these without coupling any package
to a specific implementation. Backends are swapped at the `Props` construction
point in `main.go` — no other code changes.

---

## The Logger Interface


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/logger](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/logger) for the full API definition.


---

## Log Levels

```go
const (
    DebugLevel Level = iota  // Most verbose
    InfoLevel                // Default
    WarnLevel                // Potentially harmful
    ErrorLevel               // Error conditions
    FatalLevel               // Fatal — terminates the process
)
```

Parse a level from a string (e.g., config or flag):

```go
level, err := logger.ParseLevel("debug")
if err != nil {
    // err wraps logger.ErrInvalidLevel
}
```

---

## Output Formatters

```go
const (
    TextFormatter   Formatter = iota  // Human-readable (default for charmbracelet)
    JSONFormatter                     // Machine-readable JSON
    LogfmtFormatter                   // logfmt key=value pairs
)
```

`SetFormatter` is fully supported by the charmbracelet backend.
The slog backend ignores it — the format is set by the `slog.Handler`
at construction time.

---

## Backends

### charmbracelet (default for CLI)

Produces coloured, styled terminal output via `charmbracelet/log`.
This is the default for all GTB-generated CLI tools.

```go
import (
    "os"
    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

l := logger.NewCharm(os.Stderr,
    logger.WithLevel(logger.InfoLevel),
    logger.WithTimestamp(false),  // disable timestamps for CLI output
    logger.WithCaller(false),     // disable caller location
    logger.WithPrefix("myapp"),
)
```

**CharmOption functions:**

| Option | Effect |
|--------|--------|
| `WithLevel(level)` | Sets the initial log level |
| `WithTimestamp(bool)` | Show/hide timestamp in output |
| `WithCaller(bool)` | Show/hide caller file:line |
| `WithPrefix(string)` | Prepend a prefix to all messages |

The underlying `charmbracelet/log.Logger` can be accessed via the `Handler()`
method if you need charm-specific features (e.g., custom styles). Since the
charm implementation is unexported, use the slog handler bridge:

```go
slogLogger := slog.New(l.Handler())
```

### slog (observability integration)

Wraps any `slog.Handler` — use this for OpenTelemetry, Datadog, structured
JSON pipelines, or any slog ecosystem library.

```go
import (
    "log/slog"
    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// Standard library JSON handler
jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
l := logger.NewSlog(jsonHandler)

// With Zap (using zapslog bridge)
// l := logger.NewSlog(zapslog.NewHandler(zapCore))

// With OpenTelemetry
// l := logger.NewSlog(otelslog.NewHandler(exporter))
```

`SetLevel` is supported on the slog backend via an internal `slog.LevelVar`.
`SetFormatter` is a no-op — the format is determined by the handler.

### noop (tests)

Discards all output with zero allocations. Use in tests where log output is
irrelevant.

```go
l := logger.NewNoop()
props := &props.Props{Logger: l, ...}
```

---

## slog Interoperability

All backends expose an `slog.Handler` via `l.Handler()`. Use this when a
third-party library requires `*slog.Logger`:

```go
slogLogger := slog.New(l.Handler())
thirdPartyLib.SetLogger(slogLogger)
```

---

## Integration with Props

The logger is injected through `Props`:

```go
func NewMyCommand(p *props.Props) *cobra.Command {
    return &cobra.Command{
        RunE: func(cmd *cobra.Command, args []string) error {
            p.Logger.Info("running", "args", args)
            return nil
        },
    }
}
```

For packages that only need logging, declare the narrow provider interface:

```go
type logProvider interface {
    GetLogger() logger.Logger
}

func doWork(p logProvider) {
    l := p.GetLogger()
    l.Info("working")
}
```

---

## Dynamic Level Control

The log level can be changed at runtime, useful for toggling debug output
in response to a signal or config change:

```go
l.SetLevel(logger.DebugLevel)  // enable verbose output
// ... do work
l.SetLevel(logger.InfoLevel)   // restore default
```

---

## Contextual Logging

Add fields that appear on every subsequent log call:

```go
// Structured key-value fields
reqLogger := l.With("request_id", reqID, "user", userID)
reqLogger.Info("processing request")
// → INFO processing request request_id=abc123 user=matt

// Message prefix
subLogger := l.WithPrefix("db")
subLogger.Error("connection failed", "host", host)
// → ERROR [db] connection failed host=postgres:5432
```

---

## Testing

Use `NewNoop()` in all unit tests:

```go
func TestMyCommand(t *testing.T) {
    p := &props.Props{
        Logger: logger.NewNoop(),
        // ...
    }
    // ...
}
```

Mocks are available if you need to assert specific log calls:

```go
import mock_logger "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/logger"

func TestWithLogAssertions(t *testing.T) {
    ml := mock_logger.NewMockLogger(t)
    ml.EXPECT().Warn("low disk space", "free_gb", 1).Once()
    // ...
}
```

---

## Related Documentation

- **[Props](props.md)** — how Logger is injected via the Props container
- **[Interface Design](../concepts/interface-design.md)** — Logger interface in the interface hierarchy
- **[Error Catalogue](errors.md)** — `ErrInvalidLevel` from `ParseLevel`
