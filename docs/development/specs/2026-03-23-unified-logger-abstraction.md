---
title: "Unified Logger Abstraction Specification"
description: "Replace dual charmbracelet/log and slog usage with a single Logger interface backed by pluggable implementations, enabling consistent logging across all packages and future telemetry integration."
date: 2026-03-23
status: IMPLEMENTED
tags:
  - specification
  - logging
  - architecture
  - refactor
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Unified Logger Abstraction Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   23 March 2026

Status
:   IMPLEMENTED

---

## Overview

The GTB codebase uses two distinct logging libraries:

- **`charmbracelet/log`** — 72 files across Props, Config, Chat, ErrorHandling, Setup, Commands, and the internal generator.
- **`log/slog`** — 12 files across Controls, Controls/HTTP, Controls/gRPC, and MCP server integration.

This split creates friction at every boundary. A manual `mapLogLevel` bridge in `pkg/cmd/root/root.go` converts charmbracelet levels to slog levels for MCP. A `logAdapter` in `pkg/docs/ask.go` wraps charmbracelet output as an `io.Writer` for callback-based logging. The Controls package cannot accept a logger from Props without type conversion. None of this is necessary.

This spec introduces a unified `Logger` interface in a new `pkg/logger` package with two backend implementations: a **charmbracelet backend** (preserving the current CLI experience) and an **slog backend** (for ecosystem interoperability). All packages migrate to the interface. The charmbracelet backend remains the default, users and library consumers can swap backends, and the interface provides a natural integration point for the telemetry spec (`2026-03-21-opt-in-telemetry`).

---

## Design Decisions

**Interface, not wrapper**: The `Logger` interface defines the contract. Backends implement it directly rather than wrapping one library to look like another. This avoids layered indirection and keeps each backend's native performance characteristics.

**slog.Handler as the ecosystem bridge**: The interface exposes a `Handler() slog.Handler` method, which serves two purposes. First, any library that requires `*slog.Logger` (MCP/ophis, OpenTelemetry) can obtain one via `slog.New(logger.Handler())`. Second, and critically for adoption, `NewSlog(handler)` accepts any `slog.Handler` — which means zap (22k+ stars), zerolog (10k+ stars), logrus (24k+ stars), and OpenTelemetry all integrate without GTB taking a dependency on any of them. The user brings their preferred library; GTB just needs the handler.

**charmbracelet as default backend**: GTB is a CLI framework. Terminal aesthetics matter. The charmbracelet backend provides colour, formatters (JSON, logfmt, text), and styled output. It remains the default for all `NewCmdRoot` consumers. The slog backend is available for library consumers, headless environments, and telemetry pipelines. Crucially, charmbracelet/log's `*Logger` already natively implements `slog.Handler`, so the charm backend's `Handler()` method simply returns the inner logger — no custom bridge or adapter is needed.

**Level type owns the abstraction**: A custom `Level` type with constants (`DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel`) avoids leaking either library's level types into the interface. Conversion functions map to/from both charmbracelet and slog levels.

**Incremental migration**: Props, Config, Controls, and other packages migrate one at a time. At each phase the build must pass. No big-bang rewrite.

**Printf-style methods included**: With 77+ non-test call sites using `Infof`/`Warnf`/`Errorf` and 2 using `Print`, the printf-style methods are a genuine part of the logging API, not a legacy wart. Forcing `Info(fmt.Sprintf(...))` everywhere would add noise without benefit. The interface includes both structured `(msg, keyvals...)` and printf-style `(format, args...)` variants, plus `Print` for unlevelled output (version info, release notes).

**Print for unlevelled output**: charmbracelet/log's `Print` method writes output that is not filtered by log level. This is used for direct user-facing content like version strings and styled release notes. The interface preserves this behaviour.

---

## Public API Changes

### New Package: `pkg/logger`

#### Logger Interface

```go
// Logger is the unified logging interface for GTB. All packages accept this
// interface instead of a concrete logger type.
//
// Logger is NOT safe for concurrent use unless the underlying backend
// documents otherwise. The charmbracelet and slog backends provided by
// this package are both safe for concurrent use.
type Logger interface {
    // Structured logging methods. keyvals are alternating key/value pairs.
    Debug(msg string, keyvals ...any)
    Info(msg string, keyvals ...any)
    Warn(msg string, keyvals ...any)
    Error(msg string, keyvals ...any)
    Fatal(msg string, keyvals ...any)

    // Printf-style logging methods. These exist because 77+ call sites in the
    // codebase use format strings for log messages (e.g., Infof("generating %s", name)).
    // Wrapping every call in fmt.Sprintf would add noise without benefit.
    Debugf(format string, args ...any)
    Infof(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
    Fatalf(format string, args ...any)

    // Print writes an unlevelled message. Used for direct user-facing output
    // that should not be filtered by log level (e.g., version info, release notes).
    // keyvals are optional structured key-value pairs.
    Print(msg any, keyvals ...any)

    // With returns a new Logger with the given key-value pairs prepended
    // to every subsequent log call.
    With(keyvals ...any) Logger

    // WithPrefix returns a new Logger with the given prefix prepended to
    // every message.
    WithPrefix(prefix string) Logger

    // SetLevel changes the minimum log level dynamically.
    SetLevel(level Level)

    // GetLevel returns the current minimum log level.
    GetLevel() Level

    // SetFormatter changes the output format (text, json, logfmt).
    // Backends that do not support a given formatter silently ignore the call.
    SetFormatter(f Formatter)

    // Handler returns an slog.Handler for interoperability with libraries
    // that require *slog.Logger. Usage: slog.New(logger.Handler())
    Handler() slog.Handler
}
```

#### Level Type

```go
type Level int

const (
    DebugLevel Level = iota
    InfoLevel
    WarnLevel
    ErrorLevel
    FatalLevel
)

// ParseLevel parses a level string ("debug", "info", "warn", "error", "fatal").
func ParseLevel(s string) (Level, error)

// String returns the level name.
func (l Level) String() string
```

#### Formatter Type

```go
type Formatter int

const (
    TextFormatter Formatter = iota
    JSONFormatter
    LogfmtFormatter
)
```

#### Backend Constructors

```go
// NewCharm returns a Logger backed by charmbracelet/log with the given
// options. This is the default backend for CLI applications.
func NewCharm(w io.Writer, opts ...CharmOption) Logger

// CharmOption configures the charmbracelet backend.
type CharmOption func(*charmLogger)

func WithTimestamp(enabled bool) CharmOption
func WithCaller(enabled bool) CharmOption
func WithLevel(level Level) CharmOption

// NewSlog returns a Logger backed by an slog.Handler. Use this when you
// need ecosystem integration (OpenTelemetry, Datadog, custom handlers).
// This is the primary integration point for third-party logging libraries.
// Any library that implements or bridges to slog.Handler works here:
//
//   Zap:     logger.NewSlog(zapslog.NewHandler(zapCore))
//   Zerolog: logger.NewSlog(slogzerolog.Option{Logger: &zl}.NewHandler())
//   OTEL:    logger.NewSlog(otelslog.NewHandler(exporter))
//
func NewSlog(handler slog.Handler) Logger

// NewNoop returns a Logger that discards all output. Useful for tests.
func NewNoop() Logger
```

#### Third-Party Library Integration

The `NewSlog(handler slog.Handler)` constructor is the universal integration point. Since `slog.Handler` has become the Go ecosystem's standard logging interface, most production logging libraries either implement it natively or provide an official bridge:

| Library | Integration | Dependency |
|---------|------------|------------|
| **`uber-go/zap`** | `zapslog.NewHandler(core)` (official since zap v1.27) | `go.uber.org/zap/exp/zapslog` |
| **`rs/zerolog`** | `slogzerolog.Option{Logger: &zl}.NewHandler()` | `github.com/samber/slog-zerolog` |
| **OpenTelemetry** | `otelslog.NewHandler(exporter)` | `go.opentelemetry.io/contrib/bridges/otelslog` |
| **`sirupsen/logrus`** | `sloglogrus.Option{Logger: entry}.NewHandler()` | `github.com/samber/slog-logrus` |
| **Datadog** | Via OTEL handler or `slogdd` | Varies |
| **Loki/Grafana** | Via OTEL handler | `go.opentelemetry.io/...` |

**Example: Zap in production, Charm in development**

```go
func setupLogger(env string) logger.Logger {
    if env == "production" {
        zapLogger, _ := zap.NewProduction()
        return logger.NewSlog(zapslog.NewHandler(zapLogger.Core()))
    }
    return logger.NewCharm(os.Stderr, logger.WithTimestamp(true))
}
```

**Example: Logrus migration path**

```go
// Existing logrus users can bridge immediately without rewriting call sites
entry := logrus.NewEntry(logrus.StandardLogger())
handler := sloglogrus.Option{Logger: entry}.NewHandler()
l := logger.NewSlog(handler)
```

This approach avoids adding direct dependencies on third-party logging libraries to GTB's `go.mod` while still providing first-class support. The `slog.Handler` contract is the only thing GTB needs to know about — the user brings their own backend dependency.

### Modified: `Props`

```go
// Before:
Logger *log.Logger // charmbracelet/log

// After:
Logger logger.Logger // pkg/logger interface
```

### Modified: `LoggerProvider` Interface

```go
// Before:
type LoggerProvider interface {
    GetLogger() *log.Logger
}

// After:
type LoggerProvider interface {
    GetLogger() logger.Logger
}
```

### Modified: Config `Container`

```go
// Before:
logger *log.Logger // charmbracelet/log

// After:
logger logger.Logger // pkg/logger interface
```

### Modified: Config Factory Functions

```go
// Before:
func NewFilesContainer(l *log.Logger, ...) *Container
func NewReaderContainer(l *log.Logger, ...) *Container
func LoadFilesContainer(l *log.Logger, ...) (*Container, error)

// After:
func NewFilesContainer(l logger.Logger, ...) *Container
func NewReaderContainer(l logger.Logger, ...) *Container
func LoadFilesContainer(l logger.Logger, ...) (*Container, error)
```

### Modified: Controls `Controller`

```go
// Before:
logger *slog.Logger

// After:
logger logger.Logger
```

### Modified: Controls `StateAccessor`

```go
// Before:
GetLogger() *slog.Logger

// After:
GetLogger() logger.Logger
```

### Modified: Controls `Configurable`

```go
// Before:
SetLogger(logger *slog.Logger)

// After:
SetLogger(l logger.Logger)
```

### Modified: Controls `WithLogger`

```go
// Before:
func WithLogger(logger *slog.Logger) ControllerOpt

// After:
func WithLogger(l logger.Logger) ControllerOpt
```

### Modified: Controls HTTP/gRPC Functions

```go
// Before:
func Start(cfg config.Containable, logger *log.Logger, srv *http.Server) controls.StartFunc
func Stop(logger *log.Logger, srv *http.Server) controls.StopFunc

// After:
func Start(cfg config.Containable, l logger.Logger, srv *http.Server) controls.StartFunc
func Stop(l logger.Logger, srv *http.Server) controls.StopFunc
```

### Modified: ErrorHandling

```go
// Before:
func New(logger *log.Logger, help HelpConfig) ErrorHandler

// After:
func New(l logger.Logger, help HelpConfig) ErrorHandler
```

### Removed

- `mapLogLevel` function in `pkg/cmd/root/root.go`
- `logAdapter` struct in `pkg/docs/ask.go`
- Direct imports of `charmbracelet/log` from all packages except `pkg/logger/charm.go`
- Direct imports of `log/slog` from Controls packages (replaced by `logger.Handler()`)

---

## Internal Implementation

### Charmbracelet Backend

```go
type charmLogger struct {
    inner *log.Logger
}

func (c *charmLogger) Info(msg string, keyvals ...any) {
    c.inner.Info(msg, keyvals...)
}

func (c *charmLogger) Infof(format string, args ...any) {
    c.inner.Infof(format, args...)
}

func (c *charmLogger) Print(msg any, keyvals ...any) {
    c.inner.Print(msg, keyvals...)
}

func (c *charmLogger) SetLevel(level Level) {
    c.inner.SetLevel(toCharmLevel(level))
}

func (c *charmLogger) SetFormatter(f Formatter) {
    switch f {
    case JSONFormatter:
        c.inner.SetFormatter(log.JSONFormatter)
    case LogfmtFormatter:
        c.inner.SetFormatter(log.LogfmtFormatter)
    default:
        c.inner.SetFormatter(log.TextFormatter)
    }
}

func (c *charmLogger) Handler() slog.Handler {
    // charmbracelet/log *Logger natively implements slog.Handler.
    // No custom bridge needed.
    return c.inner
}

func (c *charmLogger) With(keyvals ...any) Logger {
    return &charmLogger{inner: c.inner.With(keyvals...)}
}

func (c *charmLogger) WithPrefix(prefix string) Logger {
    return &charmLogger{inner: c.inner.WithPrefix(prefix)}
}
```

### slog Backend

```go
type slogLogger struct {
    handler slog.Handler
    logger  *slog.Logger
    level   *slog.LevelVar
}

func (s *slogLogger) Info(msg string, keyvals ...any) {
    s.logger.Info(msg, keyvals...)
}

func (s *slogLogger) Infof(format string, args ...any) {
    s.logger.Info(fmt.Sprintf(format, args...))
}

func (s *slogLogger) Print(msg any, keyvals ...any) {
    // slog has no unlevelled output; emit at Info level.
    s.logger.Info(fmt.Sprint(msg), keyvals...)
}

func (s *slogLogger) SetLevel(level Level) {
    s.level.Set(toSlogLevel(level))
}

func (s *slogLogger) SetFormatter(f Formatter) {
    // No-op for slog: formatter is determined by the handler at construction time.
}

func (s *slogLogger) Handler() slog.Handler {
    return s.handler
}

func (s *slogLogger) With(keyvals ...any) Logger {
    return &slogLogger{
        handler: s.handler,
        logger:  s.logger.With(keyvals...),
        level:   s.level,
    }
}
```

### Level Conversion

```go
func toCharmLevel(l Level) log.Level {
    switch l {
    case DebugLevel: return log.DebugLevel
    case InfoLevel:  return log.InfoLevel
    case WarnLevel:  return log.WarnLevel
    case ErrorLevel: return log.ErrorLevel
    case FatalLevel: return log.FatalLevel
    default:         return log.InfoLevel
    }
}

func toSlogLevel(l Level) slog.Level {
    switch l {
    case DebugLevel: return slog.LevelDebug
    case InfoLevel:  return slog.LevelInfo
    case WarnLevel:  return slog.LevelWarn
    case ErrorLevel, FatalLevel: return slog.LevelError
    default: return slog.LevelInfo
    }
}
```

### MCP Integration Update

In `pkg/cmd/root/root.go`, the current MCP setup creates a separate `slog.Logger` with `slog.LevelVar` and uses `mapLogLevel` to sync levels. After this spec:

```go
// Before:
mcpLogLevel := &slog.LevelVar{}
slogLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: mcpLogLevel}))
// ... later:
mcpLogLevel.Set(mapLogLevel(level))

// After:
mcpLogger := slog.New(props.Logger.Handler())
// Level changes propagate automatically through the handler bridge.
```

The `mapLogLevel` function and `mcpLogLevel` variable are eliminated entirely.

### GTB CLI (`internal/cmd/root/root.go`)

The GTB CLI's own root command currently imports `charmbracelet/log` directly:

```go
// Before:
import "github.com/charmbracelet/log"

logger := log.NewWithOptions(os.Stderr, log.Options{
    ReportCaller:    false,
    ReportTimestamp: true,
    Level:           log.InfoLevel,
})

// After:
import "github.com/phpboyscout/gtb/pkg/logger"

l := logger.NewCharm(os.Stderr,
    logger.WithTimestamp(true),
    logger.WithLevel(logger.InfoLevel),
)
```

### Generator Templates (`internal/generator/templates/`)

The skeleton root template (`skeleton_root.go`) uses `jen` to generate code that imports `charmbracelet/log` and calls `log.NewWithOptions`. This generated code is what new GTB-based tools ship with, so it must be updated to emit `logger.NewCharm()` instead.

```go
// Before (skeleton_root.go:64):
jen.Id("logger").Op(":=").Qual("github.com/charmbracelet/log", "NewWithOptions").Call(
    jen.Qual("os", "Stderr"),
    jen.Qual("github.com/charmbracelet/log", "Options").Values(jen.Dict{
        jen.Id("ReportCaller"):    jen.False(),
        jen.Id("ReportTimestamp"): jen.True(),
        jen.Id("Level"):           jen.Qual("github.com/charmbracelet/log", "InfoLevel"),
    }),
),

// After:
jen.Id("logger").Op(":=").Qual("github.com/phpboyscout/gtb/pkg/logger", "NewCharm").Call(
    jen.Qual("os", "Stderr"),
    jen.Qual("github.com/phpboyscout/gtb/pkg/logger", "WithTimestamp").Call(jen.True()),
    jen.Qual("github.com/phpboyscout/gtb/pkg/logger", "WithLevel").Call(
        jen.Qual("github.com/phpboyscout/gtb/pkg/logger", "InfoLevel"),
    ),
),
```

The `isRedundantImport` function in `command.go` filters AI-hallucinated imports. Update it to recognise both the old and new logger paths:

```go
// Before (command.go:754):
if imp == "github.com/phpboyscout/logger" || imp == "github.com/charmbracelet/log" {

// After:
if imp == "github.com/phpboyscout/logger" || imp == "github.com/charmbracelet/log" || imp == "github.com/phpboyscout/gtb/pkg/logger" {
```

### Internal Generator Package

All generator files access the logger through `g.props.Logger`. Since `Props.Logger` changes from `*log.Logger` to `logger.Logger`, and the `Logger` interface includes all the `Infof`/`Warnf`/`Errorf` methods used by the generator, **no call-site changes are needed** in the 14 generator files — only the import path for the type changes if any file references the concrete type directly.

---

## Project Structure

```
pkg/logger/
├── logger.go          ← NEW: Logger interface, Level, Formatter types
├── charm.go           ← NEW: charmbracelet/log backend (Handler() returns inner *log.Logger directly)
├── slog.go            ← NEW: slog backend
├── noop.go            ← NEW: no-op backend for tests
├── logger_test.go     ← NEW: interface contract tests
├── charm_test.go      ← NEW: charmbracelet backend tests
├── slog_test.go       ← NEW: slog backend tests
├── doc.go             ← NEW: package godoc

pkg/props/
├── props.go           ← MODIFIED: Logger field type
├── interfaces.go      ← MODIFIED: LoggerProvider return type

pkg/config/
├── container.go       ← MODIFIED: logger field type, factory params
├── config.go          ← MODIFIED: factory params
├── load.go            ← MODIFIED: factory params

pkg/controls/
├── controls.go        ← MODIFIED: interface logger types
├── controller.go      ← MODIFIED: logger field type

pkg/controls/http/
├── server.go          ← MODIFIED: logger param type

pkg/controls/grpc/
├── server.go          ← MODIFIED: logger param type

pkg/errorhandling/
├── handling.go        ← MODIFIED: logger param type

pkg/cmd/root/
├── root.go            ← MODIFIED: remove mapLogLevel, simplify MCP setup

pkg/docs/
├── ask.go             ← MODIFIED: remove logAdapter

pkg/chat/
├── tools.go           ← MODIFIED: logger param type

pkg/setup/
├── update.go          ← MODIFIED: logger field type

internal/cmd/root/
├── root.go            ← MODIFIED: replace charmbracelet/log with logger.NewCharm()

internal/generator/templates/
├── skeleton_root.go   ← MODIFIED: generated code uses logger.NewCharm() instead of charmbracelet/log
├── command.go         ← MODIFIED: update isRedundantImport filter for new import path

internal/generator/
├── docs.go            ← MODIFIED: logger param type (Infof/Warnf calls unchanged)
├── commands.go        ← MODIFIED: logger param type
├── skeleton.go        ← MODIFIED: logger param type
├── files.go           ← MODIFIED: logger param type
├── removal.go         ← MODIFIED: logger param type
├── pipeline.go        ← MODIFIED: logger param type
├── regenerate.go      ← MODIFIED: logger param type
├── stubs.go           ← MODIFIED: logger param type
├── hash.go            ← MODIFIED: logger param type
├── generator.go       ← MODIFIED: logger param type
├── manifest_scan.go   ← MODIFIED: logger param type
├── verifier/legacy.go ← MODIFIED: logger param type
```

---

## Error Handling

- Backend constructors do not return errors — invalid options fall back to sensible defaults.
- `ParseLevel` returns an error for unrecognised level strings.
- `Fatal` calls `os.Exit(1)` in both backends, consistent with current charmbracelet behaviour. Test code should use `NewNoop()` to avoid process termination.

---

## Testing Strategy

### Unit Tests

| Test | Scenario |
|------|----------|
| `TestCharmBackend_StructuredOutput` | Key-value pairs appear in output |
| `TestCharmBackend_PrintfMethods` | `Infof`/`Warnf`/`Errorf` format strings correctly |
| `TestCharmBackend_Print` | `Print` writes unlevelled output regardless of level |
| `TestCharmBackend_LevelFiltering` | Messages below current level are suppressed |
| `TestCharmBackend_SetFormatter` | JSON/logfmt/text formatting switches correctly |
| `TestCharmBackend_Handler` | `Handler()` returns native `slog.Handler`; `slog.New(logger.Handler())` produces valid slog logger |
| `TestCharmBackend_With` | `With` returns new logger with prepended keyvals |
| `TestCharmBackend_WithPrefix` | `WithPrefix` prepends prefix to messages |
| `TestSlogBackend_StructuredOutput` | Key-value pairs route through handler |
| `TestSlogBackend_PrintfMethods` | Format strings emit via `slog.Info(fmt.Sprintf(...))` |
| `TestSlogBackend_Print` | `Print` emits at Info level |
| `TestSlogBackend_LevelFiltering` | Dynamic level changes via `SetLevel` |
| `TestSlogBackend_Handler` | Returns the underlying handler unchanged |
| `TestSlogBackend_SetFormatter` | No-op, does not panic |
| `TestNoopBackend` | All methods (including Printf/Print) are callable without panic |
| `TestParseLevel` | Valid strings parse correctly, invalid returns error |
| `TestLevelConversion` | Round-trip through charm and slog level mapping |

### Integration Tests

| Test | Scenario |
|------|----------|
| `TestCharmHandler_SlogIntegration` | slog.Logger from `Handler()` logs through charm output |
| `TestCharmHandler_LevelSync` | `SetLevel` on Logger propagates to slog.Handler.Enabled |
| `TestProps_LoggerInterface` | Props with charm backend works across Config, Controls, Chat |
| `TestControls_UnifiedLogger` | Controller accepts `logger.Logger`, MCP gets slog via `Handler()` |

### Generator Tests

| Test | Scenario |
|------|----------|
| `TestSkeletonRoot_GeneratesLoggerImport` | Generated root command imports `pkg/logger`, not `charmbracelet/log` |
| `TestSkeletonRoot_UsesNewCharm` | Generated code calls `logger.NewCharm()` with correct options |
| `TestIsRedundantImport_NewLoggerPath` | `pkg/logger` import path is correctly filtered from AI output |

### Migration Tests

Each migration phase must pass:
```bash
go build ./...
go test ./...
go test -race ./pkg/logger/... ./pkg/props/... ./pkg/config/... ./pkg/controls/...
```

### Coverage

- Target: 95%+ for `pkg/logger/` (new package, full control).
- Target: 90%+ maintained for all modified packages.

---

## Linting

- `golangci-lint run --fix` must pass after all changes.
- No new `nolint` directives.
- After final phase, `charmbracelet/log` should only be imported in `pkg/logger/charm.go`. A custom lint rule or grep check can verify this.
- After final phase, `log/slog` should only be imported in `pkg/logger/` and files that need `slog.Handler` for external library integration.

---

## Documentation

### New Documentation

- **`docs/concepts/logging.md`** — New concept page covering:
    - Why GTB provides a logger abstraction (dual-library history, ecosystem interop)
    - Backend selection guide (charm for CLI, slog for headless/telemetry, noop for tests)
    - Third-party integration guide with examples for zap, zerolog, logrus, and OpenTelemetry
    - Level management and dynamic level changes
    - Formatter configuration (text, JSON, logfmt)
    - slog interop via `Handler()` for MCP and OpenTelemetry
    - Printf-style vs structured logging guidance
    - `Print` for unlevelled user-facing output
    - Migration guide for existing projects switching from charmbracelet/log or slog
- **Package godoc** for `pkg/logger` explaining the interface, backends, and when to use each.

### Updated Documentation

| File | Changes |
|------|---------|
| `docs/concepts/props.md` | Replace `charmbracelet/log` reference with `pkg/logger`, describe unified logger |
| `docs/components/props.md` | Update Logger Configuration section to show `logger.NewCharm()` |
| `docs/components/config.md` | Update `Container` struct and factory function signatures to `logger.Logger` |
| `docs/components/controls.md` | Replace `*slog.Logger` references with `logger.Logger` in interfaces and examples |
| `docs/concepts/service-orchestration.md` | Update logger best practice to reference unified logger |
| `docs/concepts/error-handling.md` | Update `charmbracelet/log` reference to `pkg/logger` |
| `docs/index.md` | Update getting started logger import to `pkg/logger` |
| `docs/getting-started.md` | Update logger initialisation example to `logger.NewCharm()` |
| `docs/installation.md` | Update logger import in quickstart example |
| `docs/cli/skeleton.md` | Update skeleton output description to reference `logger.NewCharm()` |
| `docs/components/setup/index.md` | Update logger import in setup examples |
| `docs/development/index.md` | Update logger import in developer guide examples |

---

## Backwards Compatibility

- **Props.Logger type change**: Breaking change. All consumers that type-assert or directly access `*log.Logger` methods not on the interface must update. The interface covers all commonly used methods, so most code only needs an import path change.
- **Config factory parameter type change**: Breaking change. Callers pass `logger.Logger` instead of `*log.Logger`. Since `NewCharm()` returns `logger.Logger`, the migration at call sites is mechanical.
- **Controls logger type change**: Breaking change. `*slog.Logger` → `logger.Logger`. Since `NewCharm().Handler()` produces an `slog.Handler`, and `NewSlog()` wraps any handler, migration is straightforward.
- **Printf-style and Print methods preserved**: The interface includes `Debugf`/`Infof`/`Warnf`/`Errorf`/`Fatalf` and `Print`, so existing call sites require no changes beyond the import path.
- **`mapLogLevel` removal**: Internal function, no external impact.
- **`logAdapter` removal**: Internal type, no external impact.
- **Generator template change**: New projects generated after this change will import `pkg/logger` instead of `charmbracelet/log`. Existing generated projects continue to work but will use the old import until regenerated. This is consistent with how generator template changes are handled — regeneration picks up the latest templates.

---

## Future Considerations

- **Telemetry integration**: The slog backend's `Handler()` can wrap an OpenTelemetry log handler, feeding structured logs to the telemetry pipeline defined in `2026-03-21-opt-in-telemetry`. The Logger interface does not need to change.
- **Sampling**: High-volume debug logging could benefit from a sampling handler. The slog backend supports this natively via handler middleware.
- **Context-aware logging**: A future `DebugContext(ctx, msg, keyvals...)` method family could extract trace/span IDs from context for correlation. Deferred to avoid interface bloat now.
- **Log output capture in tests**: `NewCharm(buf)` with a `*bytes.Buffer` already supports this. A dedicated test helper could be added for convenience.

---

## Implementation Phases

### Phase 1 — Define `pkg/logger`
1. Create `logger.go` with `Logger` interface, `Level`, `Formatter` types
2. Create `charm.go` with charmbracelet backend (note: `Handler()` returns the inner `*log.Logger` directly — it natively implements `slog.Handler`)
3. Create `slog.go` with slog backend
4. Create `noop.go` with no-op backend
5. Add comprehensive tests for all backends
6. Add `doc.go`

### Phase 2 — Migrate Props & Config
1. Change `Props.Logger` to `logger.Logger`
2. Change `LoggerProvider` and compound interfaces to return `logger.Logger`
3. Change Config factory functions and `Container.logger` to `logger.Logger`
4. Update `internal/cmd/root/root.go` to use `logger.NewCharm()`
5. Update all callers of `Props.Logger` that use charmbracelet-specific methods
6. Regenerate mocks

### Phase 3 — Migrate Controls
1. Change `Controller.logger` to `logger.Logger`
2. Change `StateAccessor.GetLogger()` and `Configurable.SetLogger()` to use `logger.Logger`
3. Change `WithLogger` option
4. Update HTTP and gRPC server functions
5. Regenerate Controls mocks
6. Remove `slog` imports from Controls packages

### Phase 4 — Migrate Remaining Packages
1. Migrate `pkg/errorhandling`
2. Migrate `pkg/chat` (tools.go logger parameter)
3. Migrate `pkg/setup` (update.go, init.go)
4. Migrate `pkg/docs` (remove `logAdapter`)
5. Migrate `pkg/utils`

### Phase 5 — Migrate Generator & CLI
1. Update `internal/cmd/root/root.go` to use `logger.NewCharm()` instead of `charmbracelet/log`
2. Update `internal/generator/templates/skeleton_root.go` to emit `logger.NewCharm()` in generated code
3. Update `internal/generator/templates/command.go` `isRedundantImport` to include new import path
4. Verify all 14 generator files compile with the `logger.Logger` type from Props
5. Run generator tests: `go test ./internal/generator/...`
6. Generate a test skeleton project and verify it compiles with the new logger import

### Phase 6 — Cleanup & Documentation
1. Remove `mapLogLevel` from `pkg/cmd/root/root.go`
2. Simplify MCP logger setup to use `props.Logger.Handler()`
3. Verify `charmbracelet/log` is only imported in `pkg/logger/`
4. Verify `log/slog` is only imported in `pkg/logger/` and MCP integration
5. Create `docs/concepts/logging.md`
6. Update all documentation files listed in the Documentation section
7. Run full verification suite

---

## Verification

```bash
# Build
go build ./...

# Full test suite with race detector
go test -race ./...

# Logger package specifically
go test -race -cover ./pkg/logger/...

# Lint
golangci-lint run --fix

# Generator tests
go test ./internal/generator/...

# Verify charmbracelet/log is contained to pkg/logger
grep -rn 'charmbracelet/log' --include='*.go' pkg/ internal/ | grep -v 'pkg/logger/' | grep -v '_test.go' | grep -v 'vendor/'
# Should return no results after Phase 6

# Verify no mapLogLevel remains
grep -rn 'mapLogLevel' pkg/ internal/
# Should return no results after Phase 6

# Verify no logAdapter remains
grep -rn 'logAdapter' pkg/ internal/
# Should return no results after Phase 6

# Verify generated skeleton uses new logger import
grep -n 'charmbracelet/log' internal/generator/templates/skeleton_root.go
# Should return no results after Phase 5
```
