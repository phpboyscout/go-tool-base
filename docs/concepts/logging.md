---
title: Logging
description: How GTB abstracts logging with a unified interface, backend selection guidance, and ecosystem integration patterns.
date: 2026-03-25
tags: [concepts, logging, logger, slog, charmbracelet, observability]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Logging

GTB provides a `logger.Logger` interface rather than using `*slog.Logger` or
any concrete logging library directly. This keeps every package backend-agnostic
and testable.

---

## Why a Logger Interface?

Go's `log/slog` is the standard library logger and is excellent for server-side
code, but CLI tools have different requirements:

- **Coloured, styled terminal output** — `slog` produces plain text or JSON; CLI
  users expect styled output
- **Dynamic level changes** — `slog.Logger` has no built-in dynamic level control
  without careful handler wiring
- **Printf-style convenience** — `slog` has no `Infof`, `Errorf` etc.
- **Unlevelled output** — `slog` always attaches a level; CLI tools need to print
  version strings, release notes, and prompts without a level prefix

The `logger.Logger` interface exposes all of these without coupling any package
to a specific implementation. Backends are swapped at the `Props` construction
point in `main.go` — no other code changes.

---

## Choosing a Backend

| Scenario | Backend | Factory |
|----------|---------|---------|
| CLI tool with terminal output | charmbracelet | `logger.NewCharm(os.Stderr, ...)` |
| Headless daemon / server | slog | `logger.NewSlog(handler)` |
| OpenTelemetry / Datadog | slog | `logger.NewSlog(otelslog.NewHandler(...))` |
| Zap or Zerolog | slog | `logger.NewSlog(zapslog.NewHandler(...))` |
| Unit tests | noop | `logger.NewNoop()` |

**Rule of thumb:** if the binary has a terminal user, use charmbracelet. If it
runs in a container or as a background service, use slog.

---

## The charmbracelet Backend

The default backend for GTB-generated CLI tools. Produces coloured, styled
terminal output via `charmbracelet/log`.

```go
import (
    "os"
    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

l := logger.NewCharm(os.Stderr,
    logger.WithLevel(logger.InfoLevel),
    logger.WithTimestamp(false), // suppress timestamp for interactive CLIs
    logger.WithCaller(false),
)
```

The formatter can be changed at runtime — useful for switching to JSON when a
`--output json` flag is set:

```go
if outputJSON {
    l.SetFormatter(logger.JSONFormatter)
}
```

---

## The slog Backend

Wraps any `slog.Handler`. Appropriate for services, daemons, and pipelines
that feed structured logs to an aggregator.

```go
import (
    "log/slog"
    "os"
    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// Standard library JSON (for container logs)
l := logger.NewSlog(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
```

`SetFormatter` is a no-op on the slog backend — format is determined by
the handler at construction time. `SetLevel` works via an internal
`slog.LevelVar` wrapper.

---

## slog Ecosystem Integration

All three backends expose an `slog.Handler` via `l.Handler()`. Use this to
bridge to libraries that require `*slog.Logger`:

```go
// Pass to a library that needs *slog.Logger
slogLogger := slog.New(l.Handler())
thirdPartyLib.SetLogger(slogLogger)

// OpenTelemetry log bridge
otelHandler := otelslog.NewHandler(logExporter)
l := logger.NewSlog(otelHandler)
```

---

## Dynamic Level Control

The log level can be changed at runtime without recreating the logger:

```go
// Enable verbose output for a debug flag
if debug {
    l.SetLevel(logger.DebugLevel)
}

// Inspect current level
currentLevel := l.GetLevel() // logger.Level
```

`ParseLevel` converts a config string to a level, returning `ErrInvalidLevel`
on unknown values:

```go
level, err := logger.ParseLevel(cfg.GetString("log.level"))
if err != nil {
    // cfg has an invalid level string
}
l.SetLevel(level)
```

---

## Structured vs Printf-Style

Both styles are available on the same logger:

```go
// Structured — preferred for machine-parseable fields
l.Info("request completed", "method", "GET", "path", "/api/v1", "status", 200)

// Printf-style — convenient for simple messages
l.Infof("server listening on :%d", port)
```

Prefer structured logging for anything that may be consumed by log aggregators.
Use printf-style for simple, human-readable messages where key-value pairs add
no value.

---

## Unlevelled Output: `Print`

`Print` writes a message that is not filtered by the log level. Use it for
direct user-facing output that is not a log entry — version strings, release
notes, prompts, or any output the user explicitly requested:

```go
l.Print(props.Version.String())  // "v1.2.3 (abc1234)" — always shown
l.Debug("checking version")      // filtered by level
```

---

## Contextual Logging

Add fields that appear on all subsequent calls:

```go
// Structured fields — appears on every log call
reqLogger := l.With("request_id", reqID, "user", userID)
reqLogger.Info("processing")
// INFO processing request_id=abc123 user=matt

// Message prefix
dbLogger := l.WithPrefix("db")
dbLogger.Error("connection failed", "host", host)
// ERROR [db] connection failed host=postgres:5432
```

Use `With` for request-scoped fields in handlers. Use `WithPrefix` for
subsystem-scoped loggers that should be visually distinct in output.

---

## Testing

Use `NewNoop()` in all unit tests — it discards all output with zero
allocations and no race conditions:

```go
p := &props.Props{
    Logger: logger.NewNoop(),
}
```

If you need to assert specific log calls, use the generated mock:

```go
import mock_logger "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/logger"

ml := mock_logger.NewMockLogger(t)
ml.EXPECT().Warn("low disk space", "free_gb", 1).Once()
```

---

## Logger in Props

The logger is always injected through `Props`. Packages that only need logging
declare the narrow `LoggerProvider` interface rather than taking a full `*Props`:

```go
type logProvider interface {
    GetLogger() logger.Logger
}

func NewMyService(p logProvider) *MyService {
    return &MyService{log: p.GetLogger()}
}
```

---

## Related Documentation

- **[Logger component](../components/logger.md)** — full API reference and backend options
- **[Props](../components/props.md)** — how Logger is injected via Props
- **[Interface Design](interface-design.md)** — Logger in the interface hierarchy
