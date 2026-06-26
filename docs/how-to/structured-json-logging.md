---
title: Switch to Structured JSON Logging for Containers
description: How to replace the charmbracelet terminal logger with a slog JSON backend for daemon and container deployments.
date: 2026-03-25
tags: [how-to, logging, slog, json, containers, kubernetes, observability]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Switch to Structured JSON Logging for Containers

GTB defaults to `logger.NewCharm` for beautiful terminal output. When you deploy your tool as a daemon or container, you want structured JSON logs instead — one JSON object per line, readable by Datadog, Loki, CloudWatch, or any other log aggregator.

This is a one-line change in `main.go`.

---

## Step 1: Replace the Logger Backend in `main.go`

```go
import (
    "log/slog"
    "os"

    "gitlab.com/phpboyscout/go-tool-base/pkg/logger"
    "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func main() {
    // Detect whether we're running interactively or as a daemon
    var l logger.Logger
    if isTerminal(os.Stderr) {
        // Interactive CLI: coloured, styled output
        l = logger.NewCharm(os.Stderr,
            logger.WithLevel(logger.InfoLevel),
            logger.WithTimestamp(false),
        )
    } else {
        // Daemon/container: structured JSON
        l = logger.NewSlog(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
            Level: slog.LevelInfo,
        }))
    }

    p := &props.Props{
        Logger: l,
        // ...
    }
}
```

`isTerminal` can be implemented with `golang.org/x/term`:

```go
import "golang.org/x/term"

func isTerminal(f *os.File) bool {
    return term.IsTerminal(int(f.Fd()))
}
```

---

## Step 2: Configure the Log Level

`SetLevel` works on all backends:

```go
level, err := logger.ParseLevel(os.Getenv("LOG_LEVEL"))
if err == nil {
    l.SetLevel(level)
}
```

Or wire it through config (after the config is loaded in `PersistentPreRunE`):

```go
levelStr := p.Config.GetString("log.level")
if level, err := logger.ParseLevel(levelStr); err == nil {
    p.Logger.SetLevel(level)
}
```

Valid level strings: `debug`, `info`, `warn`, `error`.

---

## JSON Output Format

With `slog.NewJSONHandler`, each log call produces one JSON object:

```json
{"time":"2026-03-25T14:23:01Z","level":"INFO","msg":"starting gRPC server","addr":":8080"}
{"time":"2026-03-25T14:23:01Z","level":"INFO","msg":"service registered","id":"grpc"}
{"time":"2026-03-25T14:23:05Z","level":"WARN","msg":"health check failed","service":"database","error":"connection refused"}
```

Structured fields passed to `Info`, `Warn`, etc. appear as top-level JSON keys:

```go
p.Logger.Info("request completed",
    "method", "POST",
    "path", "/api/v1/deploy",
    "status", 201,
    "duration_ms", 42,
)
```

```json
{"time":"...","level":"INFO","msg":"request completed","method":"POST","path":"/api/v1/deploy","status":201,"duration_ms":42}
```

---

## Using OpenTelemetry

Replace `slog.NewJSONHandler` with an OTEL handler:

```go
import "go.opentelemetry.io/contrib/bridges/otelslog"

otelHandler := otelslog.NewHandler("mytool",
    otelslog.WithLoggerProvider(loggerProvider),
)
l = logger.NewSlog(otelHandler)
```

The rest of your code is unchanged — all calls to `p.Logger.Info(...)` etc. flow through to the OTEL exporter.

---

## Bridging to Third-Party Libraries

Some libraries require a `*slog.Logger` directly. Use `l.Handler()` to get the underlying handler:

```go
slogLogger := slog.New(p.Logger.Handler())

// Pass to libraries that need *slog.Logger
grpcserver.SetLogger(slogLogger)
someSDK.WithLogger(slogLogger)
```

---

## Contextual Fields

Add fields that appear on every subsequent log call from a given logger:

```go
// Request-scoped logger (create per-request)
reqLogger := p.Logger.With(
    "request_id", requestID,
    "user", userID,
    "service", "api",
)
reqLogger.Info("processing")
reqLogger.Warn("validation failed", "field", "email")
```

```json
{"level":"INFO","msg":"processing","request_id":"abc123","user":"matt","service":"api"}
{"level":"WARN","msg":"validation failed","request_id":"abc123","user":"matt","service":"api","field":"email"}
```

---

## Differences from the Charm Backend

| Behaviour | `NewCharm` | `NewSlog` |
|-----------|-----------|-----------|
| `SetFormatter(JSONFormatter)` | Switches to JSON | No-op (format set at construction) |
| `Print(msg)` | Unlevelled output (no level prefix) | Emits at `INFO` level |
| `WithPrefix(prefix)` | Adds `[prefix]` visually | Adds `"prefix": "..."` JSON field |
| Timestamp | Configurable via `WithTimestamp` | Controlled by `slog.HandlerOptions` |

---

## Testing

Tests should always use `logger.NewNoop()` — it discards all output with zero allocations:

```go
p := &props.Props{
    Logger: logger.NewNoop(),
}
```

To assert specific log calls in tests, use the generated mock:

```go
import mock_logger "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/logger"

ml := mock_logger.NewMockLogger(t)
ml.EXPECT().Info("server started", "addr", ":8080").Once()
```

---

## Related Documentation

- **[Logger component](../explanation/components/logger.md)** — all backends, `CharmOption` functions, `Handler()` interop
- **[Logging concepts](../explanation/components/logger.md)** — when to use each backend
