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
`logger.Logger`'s method set mirrors the standard library's `*slog.Logger`
exactly, so a `*slog.Logger` satisfies it directly — you can assign one straight
to `Props.Logger`. Several constructors are provided:

| Constructor | Returns | Best For |
|-------------|---------|----------|
| `NewCharm(w, opts...)` | slog-mirror Logger (also `Leveller`/`Reformatter`) | CLI applications — coloured, styled terminal output; GTB's default |
| `NewCharmSlog(w, opts...)` / `NewCharmHandler(w, opts...)` | `*slog.Logger` / `slog.Handler` | slog-native construction over GTB's Charm output |
| `NewSlog(handler)` | `*slog.Logger` | Observability stacks — OpenTelemetry, Datadog, Zap, Zerolog |
| `NewNoop()` | discarding `*slog.Logger` | Tests — discards all output |
| `NewBuffer()` / `NewCaptureHandler()` | in-memory capture | Tests — assert on captured records |

---

## Why a Logger Interface?

Go's `log/slog` is the standard library logging boundary, and GTB embraces it:
`logger.Logger` mirrors `*slog.Logger` exactly, so any slog-compatible logger
drops in and structured, levelled logging is the norm across the framework. What
`pkg/logger` adds on top is CLI-shaped construction and testing:

- **Coloured, styled terminal output** — `slog` produces plain text or JSON;
  `NewCharm` gives CLI users the styled output they expect while still being a
  `*slog.Logger` under the hood
- **Testable by construction** — `slog` ships no first-class test double.
  `logger.NewNoop()` discards output, `NewBuffer()` / `NewCaptureHandler()`
  capture records for assertions, and `NewCharm(w, …)` writes to any `io.Writer`
  you inject (e.g. a `bytes.Buffer`)
- **Runtime level/format control** — a bare `*slog.Logger` owns its level via its
  handler, but GTB's default logger also implements `Leveller`/`Reformatter` so
  `--debug`, `log.level`, and `log.format` can take effect after construction

Because the interface is just the `*slog.Logger` method set, backends are swapped
at the `Props` construction point in `main.go` — no other code changes, and you
can inject a plain `*slog.Logger` from anywhere in the ecosystem.

---

## The Logger Interface

`logger.Logger` mirrors the `*slog.Logger` method set exactly — the levelled
methods (`Debug`/`Info`/`Warn`/`Error` and their `…Context` variants), `Log` /
`LogAttrs`, `With` / `WithGroup`, `Enabled`, and `Handler`. Nothing else is on
the interface, which is precisely why a `*slog.Logger` satisfies it:

```go
var _ logger.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
```

Runtime level/format control is *not* on the interface (see
[Dynamic Level Control](#dynamic-level-control) below); process termination and
unlevelled user output are the command / `go/output` layers' job, not the
logging boundary's.


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

Reformatting at runtime goes through the `logger.SetFormatter(log, f)` helper.
It succeeds (returning `true`) on GTB's Charm-backed logger, which implements
`Reformatter`; for a plain `*slog.Logger` it is a no-op returning `false`, since
the format is fixed by the `slog.Handler` at construction time.

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

`NewCharm` returns a `logger.Logger`; its `Handler()` method exposes the
backing `slog.Handler`, which you can wrap in `slog.New` for any library that
wants a `*slog.Logger`:

```go
slogLogger := slog.New(l.Handler())
```

If you need slog-native construction over the same Charm output — for example to
hand a `*slog.Logger` or `slog.Handler` directly to another component — use
`NewCharmSlog(w, opts...)` or `NewCharmHandler(w, opts...)` instead.

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

`NewSlog` returns a plain `*slog.Logger`, which owns its level and format through
its handler. It does not implement `Leveller`/`Reformatter`, so
`logger.SetLevel` and `logger.SetFormatter` are no-ops (returning `false`) for
it. For runtime level control, build the handler at its most permissive level
and wrap it with `NewLevelGate(handler, levelVar)`; mutate the shared
`*slog.LevelVar` to raise or lower the threshold at runtime.

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

## Structured values, and why the Charm backend is wrapped

The Charm backend's handler is wrapped by `logger.NewResolvingHandler`. That
wrapper exists to work around a specific upstream defect, and it is meant to be
deleted, so it is worth writing down what it is for.

### The defect

`log/slog` requires a handler to call `Value.Resolve()` on every attribute. That
is the call which turns a `slog.LogValuer` into the value it wants logged.

`charmbracelet/log` does this in **one of its three formatters**. Its JSON
formatter resolves structured values and renders them as nested objects; its
text and logfmt formatters do neither, because `Handle` passes the raw
`slog.Value` on and leaves each formatter to decide. The same record therefore
carries less information as text than as JSON:

```
text    err="no config file found"
json    {"err":{"msg":"no config file found","kind":"...","hint":["Run 'gtb init' ..."]}}
```

This became load-bearing when `errorhandling` v0.2.0 stopped taking errors apart
and began handing them to slog whole, expecting the handler to resolve them.
Everything an error carries, **including the hints that tell a user what to do
next**, arrives through `LogValue`. Against the text formatter it silently
disappeared.

### Watch this issue

<https://github.com/charmbracelet/log/issues/96>

**When it is fixed, delete `pkg/logger/resolve.go` and its wiring in `NewCharm`.**
Nothing else depends on it.

The asymmetry looks accidental rather than deliberate: PR #127 ("support slog
attributes") touched `json.go` and the handler, not the two text formatters. A
patch exists and was deliberately **not** proposed upstream, because resolving
properly changes output for anyone pinning the old rendering, and that is the
maintainers' call to make. The findings are recorded on the issue, and the patch
is kept at `~/patches/charmbracelet-log/` in case they ask for it.

### Why Charm was kept rather than swapped out

The obvious alternative was to drop Charm and standardise on a conformant
`slog.Handler`. The ecosystem was surveyed and none of the alternatives carried
the features GTB actually relies on:

| Candidate | Outcome |
|---|---|
| `lmittmann/tint` | Escapes multi-line values onto one line. A stack trace becomes unreadable |
| `golang-cz/devslog` | Renders multi-line, but with no gutter the value bleeds into the left margin |
| `phuslu/log` | Its only `slog.Handler` is JSON. No text handler exists |
| `coder/slog` (`cdr.dev/slog`) | Predates the standard library and implements its own `slog.Sink`, not `log/slog.Handler` |
| `phsym/console-slog` | Unmaintained since January 2024 |
| `dpotapov/slogpfx` | The only dedicated prefix handler, unmaintained since 2023 |

What Charm provides and the alternatives do not:

- **Multi-line values with a gutter.** A stack trace stays readable and stays
  visibly attached to its key, with the following attribute correctly outdented.
  Nothing else in the ecosystem does this.
- **Prefixes**, for labelling a logger by subsystem.
- **`Helper()`**, which skips a wrapper frame in caller reporting the way
  `testing.TB.Helper()` does.

One apparent requirement turned out not to be one: an adapter to the older
`*log.Logger` API is in the standard library as `slog.NewLogLogger`, and works
with any handler.

So Charm is kept. It is the most actively maintained of the options and the
defect is one code path, against a wrapper of about fifty lines that is tested
and reversible.

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

`SetLevel`/`SetFormatter` are not interface methods — they are package helpers
that apply only when the logger implements the optional `Leveller`/`Reformatter`
capabilities. GTB's default `NewCharm` logger implements both:

```go
type Leveller interface{ SetLevel(level slog.Level) }
type Reformatter interface{ SetFormatter(f logger.Formatter) }
```

Use the helpers to change the level at runtime, useful for toggling debug output
in response to a signal or config change. They report whether the change was
applied:

```go
logger.SetLevel(l, slog.LevelDebug)  // true on NewCharm; false (no-op) on a plain *slog.Logger
// ... do work
logger.SetLevel(l, slog.LevelInfo)   // restore default
```

To branch on whether a level is active — for example to skip expensive
diagnostics — call `Enabled` on the interface itself:

```go
if l.Enabled(ctx, slog.LevelDebug) {
    l.Debug("expensive dump", "state", buildExpensiveState())
}
```

---

## Config-driven construction

A config-driven host does not hand-assemble `CharmOption`s. It unmarshals its
logging config section into the typed, config-system-agnostic `logger.Config`
(fields `Level`, `Format`, `Timestamp`, `Caller`) and bridges it to `NewCharm`
via `Config.CharmOptions()`:

```go
cfg := logger.Merge(logger.DefaultConfig(), decoded) // decoded from the host's config layer
l := logger.NewCharm(os.Stderr, cfg.CharmOptions()...)
```

`DefaultConfig` gives the package baseline (info level, text format, no
timestamp, no caller); `Merge` overlays the host's decoded section onto it
(empty string fields preserve the base; booleans take the overlay value); and
`CharmOptions()` renders the result as the `WithLevel`/`WithFormatter`/
`WithTimestamp`/`WithCaller` options `NewCharm` expects.

### Why the bridge is the only route for two of the four fields

The four `Config` fields do **not** all have the same reach:

| Field | Construction | Runtime |
|-------|:---:|:---:|
| `Level` | ✓ (`WithLevel`) | ✓ (`SetLevel` / `Leveller`) |
| `Format` | ✓ (`WithFormatter`) | ✓ (`SetFormatter` / `Reformatter`) |
| `Timestamp` | ✓ (`WithTimestamp`) | ✗ — no runtime setter |
| `Caller` | ✓ (`WithCaller`) | ✗ — no runtime setter |

`Level` and `Format` are reachable **both** at construction and at runtime, so a
host can build a logger however it likes and still let `--debug`, `log.level`,
and `log.format` take effect afterwards. `Timestamp` and `Caller` are
**construction-time only** — the `Logger` interface exposes no runtime setter for
them. A host that wants those two config-driven therefore *must* build its logger
from `Config` through `CharmOptions()`; there is no later hook to apply them, so
setting them after construction is impossible rather than merely inconvenient.

---

## Contextual Logging

Add fields that appear on every subsequent log call with `With` (which returns a
`*slog.Logger`, itself a `logger.Logger`):

```go
// Structured key-value fields
reqLogger := l.With("request_id", reqID, "user", userID)
reqLogger.Info("processing request")
// → INFO processing request request_id=abc123 user=matt

// A component/prefix is just another structured attribute
subLogger := l.With("component", "db")
subLogger.Error("connection failed", "host", host)
// → ERROR connection failed component=db host=postgres:5432
```

There is no `WithPrefix` on the interface — a prefix is carried as a structured
attribute via `With`. (A construction-time `logger.WithPrefix(...)` `CharmOption`
still exists to set a fixed prefix on a `NewCharm` logger.)

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
