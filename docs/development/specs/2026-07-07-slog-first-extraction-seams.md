---
title: "Slog-first logging and config adapter seams for module extraction"
description: "Refactor GTB's logging boundary so reusable packages and future extracted modules can depend on log/slog or package-local narrow seams instead of pkg/logger. Define the companion config strategy: extracted modules accept typed options or local lookup interfaces, while GTB keeps pkg/config as the framework runtime configuration adapter."
date: 2026-07-07
status: APPROVED
tags:
  - specification
  - logging
  - config
  - module-extraction
  - dependency-inversion
  - slog
  - reuse
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Codex
    role: AI drafting assistant
---

# Slog-first logging and config adapter seams for module extraction

Authors
:   Matt Cockayne, Codex *(AI drafting assistant)*

Date
:   7 July 2026

Status
:   APPROVED

Builds on
:   [`2026-07-05-chat-module-extraction.md`](2026-07-05-chat-module-extraction.md)

Related
:   [`2026-07-07-package-extraction-report.md`](../reports/2026-07-07-package-extraction-report.md),
    [`2026-07-07-config-section-adapters-for-extraction.md`](2026-07-07-config-section-adapters-for-extraction.md)

---

## 0. Approved design decisions (2026-07-10 review)

This spec was reviewed and approved on 2026-07-10. It is delivered **jointly with
the config-section-adapters spec** on a single large branch, so the logging and
config seams land as one coherent cut-over. The decisions below are authoritative
and override any earlier wording in the body sections; the body is retained as
design context.

### D1 — `Props.Logger` becomes a GTB-owned interface that mirrors `*slog.Logger`

There is **no new `Props.Slog` field**. Instead, `pkg/logger.Logger` is
**redefined** as an interface whose method set mirrors `*slog.Logger` exactly, and
`Props.Logger` keeps its name but takes that interface as its type. A real
`*slog.Logger` therefore satisfies `Props.Logger` and can be passed in directly,
with no concrete dependency and room for consumers to supply their own
implementation later.

```go
// Logger mirrors *slog.Logger's method set so a *slog.Logger satisfies it
// directly. Full mirror: With/WithGroup return *slog.Logger by design.
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
    DebugContext(ctx context.Context, msg string, args ...any)
    InfoContext(ctx context.Context, msg string, args ...any)
    WarnContext(ctx context.Context, msg string, args ...any)
    ErrorContext(ctx context.Context, msg string, args ...any)
    Log(ctx context.Context, level slog.Level, msg string, args ...any)
    LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr)
    With(args ...any) *slog.Logger
    WithGroup(name string) *slog.Logger
    Enabled(ctx context.Context, level slog.Level) bool
    Handler() slog.Handler
}
```

GTB's default `Props.Logger` is simply `slog.New(logger.NewCharmHandler(...))`.
This is a **breaking change** to the `Logger` interface and to `Props.Logger`'s
type. That is acceptable pre-1.0 (see `AGENTS.md`), ships as a minor bump, and
must **not** carry a `BREAKING CHANGE:` footer (that would wrongly cut v1.0.0 via
releaser-pleaser). A migration note is required.

Consequences (drop-outs from the interface, since `*slog.Logger` lacks them):

- `Debugf`/`Infof`/`Warnf`/`Errorf` — migrate call sites to structured logging
  (hybrid: structured key/values where the format maps cleanly, otherwise
  `Debug(fmt.Sprintf(...))`). ~191 production call sites.
- `Fatal`/`Fatalf` — move to command/`pkg/errorhandling` exit paths (~7 sites).
- `Print` — move to Cobra writers / `pkg/output` (~25 sites).
- `SetLevel`/`GetLevel`/`SetFormatter` — become root-only concerns driven through
  a shared `slog.LevelVar` and the Charm handler (~10 sites); packages branch on
  `log.Enabled(ctx, slog.LevelDebug)`.

### D2 — Charm stays the default via a `slog.LevelVar`-gated handler

`NewCharmHandler(w, opts...) slog.Handler` and `NewCharmSlog(w, opts...) *slog.Logger`
are added. Runtime `--debug`/`log.level` changes are driven by a shared
`*slog.LevelVar` via a `WithSlogLevel(*slog.LevelVar)` option. Implementation
note: the existing `slogLevelHandler` in `pkg/logger/slog.go` already gates an
inner handler on a `LevelVar`, so `NewCharmHandler` wraps the Charm handler in
that gate (Charm's own level set permissive). The typed `logger.Config` from the
config-adapters spec supplies format/timestamp/caller via `CharmOptions()`.

### D3 — Capturing slog handler for tests

`ToSlog(bufferLogger)` would discard records because `bufferLogger.Handler()`
returns a no-op. A **capturing `slog.Handler`** (e.g. `logger.NewBufferHandler()`
exposing recorded records, or a real capturing `Handler()` on the buffer logger)
must be added in Phase 1 so migrated packages can assert on emitted records.

### D4 — Deprecate legacy surface immediately

Once the slog-first constructors and compatibility helpers exist, the legacy
facade methods that leave the interface (`Debugf`, `Fatal`, `Print`, `SetLevel`,
etc., and `NewCharm`) are annotated `// Deprecated:` immediately, pointing at the
slog-first replacements. (Deprecation is advisory; it does not break builds.)

### D5 — Generator auto-migrates existing projects

Regeneration **rewrites** existing scaffolded projects' root logger construction
to the slog-first pattern, not just new scaffolds. This requires idempotency
tests (regenerating twice produces no second diff) alongside the new-scaffold
tests.

### D6 — Config seam defers to the config-adapters spec

The config strategy in §7 below is **superseded** by
`2026-07-07-config-section-adapters-for-extraction.md`: typed `UnmarshalSection`
structs are the primary boundary, and `pkg/config` is an acceptable lightweight
dependency (it is itself an early planned extraction). The binding extraction
constraint is severing GTB **main-module/`props`** weight, not `pkg/config`. Read
§7 as historical context; follow the config-adapters spec for the config seam.

---

## 1. Context

GTB currently uses `pkg/logger.Logger` as the common logging dependency across
framework packages. That interface has served the framework well: it gives GTB
a default Charm-powered CLI experience, no-op and buffer loggers for tests, and
an `slog.Handler` escape hatch for interoperability.

For extraction, however, the interface is too framework-shaped:

- It requires downstream modules to import `gitlab.com/phpboyscout/go-tool-base/pkg/logger`.
- It includes GTB-specific runtime controls (`SetFormatter`, `SetLevel`,
  `GetLevel`) that most reusable libraries should not own.
- It includes process-exiting methods (`Fatal`, `Fatalf`) that are inappropriate
  in library code and should remain in command/error-handling layers.
- It includes `Print`, which is direct user output rather than logging.
- It makes extracted modules inherit GTB's logging abstraction even though Go now
  has a standard structured logging package in `log/slog`.

The chat extraction spec already proposes local seams for logging and config.
This spec generalises that approach for GTB's reusable packages so extraction
does not repeat bespoke decoupling work for each package.

## 2. Problem Statement

Reusable packages need observability hooks without depending on the GTB
framework. Today they often accept `logger.Logger`, even when they only need one
or two debug/error log calls. This couples packages such as `chat`, `controls`,
`http`, `grpc`, `config`, `telemetry`, and `vcs` to GTB's logger package and
complicates standalone module extraction.

The same question exists for config. `pkg/config.Containable` is a powerful GTB
runtime configuration abstraction, but many extractable packages only need a few
typed values or a provider-specific options struct. If extracted modules depend
on `pkg/config`, they still pull in Viper, afero, fsnotify, pflag, and GTB's
runtime config semantics.

## 3. Goals

- Make `log/slog` the default logging boundary for reusable library packages.
- Retain Charm's human-friendly output as GTB's default application logger by
  using Charm's `slog.Handler` support.
- Keep existing public APIs working during the transition, with deprecations and
  wrappers where required by GTB's API stability policy.
- Remove `pkg/logger` imports from packages targeted for extraction, starting
  with `pkg/chat` and then `pkg/controls`, transports, telemetry, and VCS.
- Establish a consistent config rule: extracted modules accept typed options or
  package-local lookup interfaces; GTB adapts `pkg/config.Containable` at the
  framework boundary.
- Avoid making `pkg/config` a mandatory dependency of extracted modules unless a
  module's purpose is specifically configuration management.

## 4. Non-goals

- This spec does not extract `pkg/logger` or `pkg/config` into separate modules.
- This spec does not change GTB's default visual log output.
- This spec does not remove `pkg/logger.Logger` immediately.
- This spec does not implement the chat extraction itself; it prepares shared
  logging/config conventions that the chat extraction can use.
- This spec does not replace Cobra output, `pkg/output`, or user-facing command
  rendering with `slog`.

## 5. Current Logging Surface

`pkg/logger.Logger` currently exposes:

```go
type Logger interface {
    Debug(msg string, keyvals ...any)
    Info(msg string, keyvals ...any)
    Warn(msg string, keyvals ...any)
    Error(msg string, keyvals ...any)
    Fatal(msg string, keyvals ...any)

    Debugf(format string, args ...any)
    Infof(format string, args ...any)
    Warnf(format string, args ...any)
    Errorf(format string, args ...any)
    Fatalf(format string, args ...any)

    Print(msg any, keyvals ...any)
    With(keyvals ...any) Logger
    WithPrefix(prefix string) Logger
    SetLevel(level Level)
    GetLevel() Level
    SetFormatter(f Formatter)
    Handler() slog.Handler
}
```

This mixes at least four concepts:

- structured application logging,
- process termination,
- direct terminal/user output,
- mutable application logger configuration.

The extraction-friendly boundary should be smaller and standard-library based.

## 6. Design

### 6.1 Logging direction

GTB should keep `pkg/logger` as the application logging package, but package
authors should not treat it as the default library dependency. The preferred
library logging boundary becomes one of:

1. `*slog.Logger` for packages that need normal structured logging.
2. `slog.Handler` for constructors that are explicitly creating loggers.
3. A package-local minimal interface only when a package needs to abstract over
   behaviour not represented well by `*slog.Logger`.

This is a deliberate use of the standard library's logging design, not a
rejection of the Go idiom to accept interfaces where they buy useful
decoupling. `slog`'s abstraction point is `slog.Handler`; the standard library
does not define a `slog.Logger` interface. `*slog.Logger` is the small, stable,
concurrency-safe facade that application and library code pass around to emit
records, while `slog.Handler` is the interface used when wiring or replacing
the backend. This mirrors common Go boundaries that accept concrete runtime
handles such as `*http.Client` or `*sql.DB` because those concrete types already
encapsulate the extensibility seam.

For most reusable packages, `*slog.Logger` is enough. Package-local interfaces
remain appropriate when a module genuinely needs only one or two methods and
does not need `Enabled`, `With`, groups, structured attributes, or standard
handler interoperability. Those interfaces should be defined at the point of
use rather than exported as a shared GTB logging abstraction.

```go
func WithLogger(log *slog.Logger) Option {
    return func(o *options) {
        o.log = log
    }
}
```

Nil loggers should resolve to a package-local no-op logger:

```go
func defaultLogger(log *slog.Logger) *slog.Logger {
    if log != nil {
        return log
    }
    return slog.New(discardHandler{})
}
```

Packages must not call `os.Exit`, `Fatal`, or `Fatalf`. Fatal decisions belong
to command execution and `pkg/errorhandling`.

### 6.2 Charm remains the GTB default

GTB keeps the same user experience by constructing its default logger from a
Charm `slog.Handler`.

Proposed additions to `pkg/logger`:

```go
func NewCharmHandler(w io.Writer, opts ...CharmOption) slog.Handler
func NewCharmSlog(w io.Writer, opts ...CharmOption) *slog.Logger
```

`NewCharm` remains for compatibility, but internally it should be treated as the
legacy GTB facade. New GTB composition code should prefer:

```go
level := new(slog.LevelVar)
level.Set(slog.LevelInfo)

handler := logger.NewCharmHandler(os.Stderr,
    logger.WithTimestamp(true),
    logger.WithSlogLevel(level),
)
log := slog.New(handler)
```

The exact option names may change during implementation, but the design
requirement is that GTB can retain:

- colored/styled human output,
- runtime log level changes from `--debug` and `log.level`,
- JSON/logfmt/text formatting where those remain framework features,
- OTel bridge support where needed.

### 6.3 Compatibility facade

`pkg/logger.Logger` should remain available during the transition. It becomes a
compatibility facade for GTB application code and downstream tools that already
compile against it.

Required compatibility helpers:

```go
func FromSlog(log *slog.Logger, opts ...CompatOption) Logger
func ToSlog(log Logger) *slog.Logger
func Handler(log Logger) slog.Handler
```

Behaviour:

- `ToSlog(nil)` returns a no-op slog logger.
- `ToSlog` uses `log.Handler()` when given a legacy `Logger`.
- `FromSlog` maps `Debug`/`Info`/`Warn`/`Error` to slog levels.
- `Fatal`/`Fatalf` on the compatibility wrapper log at error level and then use
  the configured exit function. Tests must be able to override this exit
  function.
- `Print` remains compatibility-only and must not be used in reusable packages.

### 6.4 Deprecation plan

The following should be marked deprecated after `*slog.Logger` alternatives are
available:

```go
// Deprecated: use *slog.Logger or logger.NewCharmSlog for new code.
type Logger interface { ... }

// Deprecated: use logger.NewCharmSlog or logger.NewCharmHandler for new code.
func NewCharm(w io.Writer, opts ...CharmOption) Logger

// Deprecated: use logger.FromSlog only at GTB compatibility boundaries.
func NewSlog(handler slog.Handler) Logger
```

Deprecation does not mean immediate removal. These APIs remain until the next
major version window or until the project's API stability policy allows removal.

### 6.5 Package migration pattern

Each reusable package should move from:

```go
func WithLogger(l logger.Logger) Option
```

to:

```go
func WithLogger(l *slog.Logger) Option
```

Where backwards compatibility is required:

```go
// Deprecated: use WithLogger with *slog.Logger.
func WithGTBLogger(l logger.Logger) Option {
    return WithLogger(logger.ToSlog(l))
}
```

If Go does not allow both overloads because of identical names, keep the old
name temporarily and add a new explicit name:

```go
func WithSlogLogger(l *slog.Logger) Option

// Deprecated: use WithSlogLogger.
func WithLogger(l logger.Logger) Option
```

The implementation phase should choose the least disruptive name per package.
Packages being extracted should expose only the slog-based API in the extracted
module.

### 6.6 Log levels and formatting

Runtime level and formatter controls are application concerns. Extractable
packages should not expose `SetLevel`, `GetLevel`, or `SetFormatter`.

GTB root command remains responsible for:

- reading `--debug`, `log.level`, and `log.format`,
- mutating the shared application `slog.LevelVar`,
- choosing the application handler format,
- wiring the logger into `props` and command execution.

Packages that need to branch on debug mode should use:

```go
if log.Enabled(ctx, slog.LevelDebug) {
    ...
}
```

instead of `logger.GetLevel() == logger.DebugLevel`.

### 6.7 Direct user output is not logging

`logger.Print` is currently used by some commands for direct terminal output.
That should stay in command/UI layers and gradually move to `pkg/output`, Cobra
writers, or explicit `io.Writer` parameters.

Reusable libraries should never require a logger solely to print user-facing
content.

## 7. Config Strategy

### 7.1 Recommendation

Extracted modules should not depend on `pkg/config.Containable` by default.

Instead:

- use explicit typed options for module behaviour,
- use package-local lookup interfaces only for adapter seams,
- keep GTB's `pkg/config` as the framework runtime configuration loader,
- map `config.Containable` to extracted module options in GTB adapter code.

This is the same direction as the chat extraction spec's `ConfigLookup`, but
generalised across modules.

### 7.2 Why not require `pkg/config` everywhere?

`pkg/config` is a high-value GTB component, but it carries framework semantics:

- Viper-backed precedence,
- env prefix binding,
- file/reader/embed loading,
- hot reload and observers,
- schema validation,
- pflag binding,
- `GetViper` escape hatch.

Most extracted modules do not need that surface. If they import it, they inherit
both dependency weight and GTB runtime assumptions.

For example:

- `chat` mostly needs provider names, base URLs, models, and credential
  references.
- `http`/`grpc` need typed server/client options.
- `vcs/release` needs provider source and optional auth/host values.
- `tls` needs a typed TLS config struct.
- `controls` should not need config at all.

### 7.3 Package-local lookup interfaces

When lookup is genuinely better than a fully materialised options struct, define
the interface in the consuming module:

```go
type StringLookup interface {
    GetString(key string) string
}

type BoolLookup interface {
    GetBool(key string) bool
}
```

Do not export one global "GTB config lite" interface for every extracted module.
Interfaces should be defined at the point of use so each module depends only on
the methods it needs.

### 7.4 Typed options first

For new extracted APIs, prefer:

```go
type ClientOptions struct {
    BaseURL string
    Timeout time.Duration
    Token string
    Logger *slog.Logger
}

func NewClient(opts ClientOptions) (*Client, error)
```

GTB then provides an adapter:

```go
func clientOptionsFromConfig(cfg config.Containable, log *slog.Logger) chat.ClientOptions {
    return chat.ClientOptions{
        BaseURL: cfg.GetString("openai.base_url"),
        Timeout: cfg.GetDuration("ai.timeout"),
        Logger:  log,
    }
}
```

This keeps config schema decisions in GTB while leaving the extracted module
usable by any application config system.

### 7.5 Should `pkg/config` be extracted?

This spec recommends not using `pkg/config` as the shared abstraction for
extracted modules.

`pkg/config` may still be extracted later as a standalone, opinionated
configuration module. That should be a separate decision with a separate spec.
If extracted, it should be positioned as:

- "a Viper/afero-backed runtime config package for CLI tools",
- not "the required config interface for all PHPBoyscout modules".

Before any config extraction, the package should complete hot-reload and
observer hardening and remove its dependency on `pkg/logger` by accepting
`*slog.Logger` or no logger.

## 8. Impacted Packages

### First wave

- `pkg/chat`: align with the chat extraction spec. Replace logger dependency
  with `*slog.Logger` or a minimal local interface; replace config with typed
  options and `ConfigLookup` only at GTB adapter boundaries.
- `pkg/controls`: move to `*slog.Logger`; remove `logger.Logger` from
  controller options and interfaces where compatible wrappers can preserve API.
- `pkg/config`: stop depending on `pkg/logger`; use `*slog.Logger` internally
  for diagnostics.

### Second wave

- `pkg/http` and `pkg/grpc`: move middleware/server logging to `*slog.Logger`;
  use `slog.Level` instead of `logger.Level`; keep config adaptation in GTB or
  explicit server options.
- `pkg/telemetry`: accept `*slog.Logger`; keep product telemetry config mapping
  in GTB.
- `pkg/vcs/release` and providers: replace `config.Containable` provider
  factories with explicit provider option structs or package-local lookup
  interfaces.
- `pkg/errorhandling`: preserve existing constructor, add `NewSlog` or
  `WithSlogLogger` and replace debug-level checks with `slog.Enabled`.

### Command/UI packages

Command packages may continue to use the GTB application logger while migration
is in progress. Direct user output should move toward Cobra writers and
`pkg/output`, not `slog`.

## 9. Generator Updates

This refactor affects scaffolded tools, so `internal/generator` must be updated
as part of the implementation. The generator is the source of truth for the
recommended GTB application composition pattern; it must not continue emitting
new code that depends on the legacy logger facade once the slog-first
construction APIs exist.

Current generated root code creates the logger with:

```go
l := logger.NewCharm(os.Stderr,
    logger.WithTimestamp(true),
    logger.WithLevel(logger.InfoLevel),
)
```

The generated root template should move to the new slog-first construction
pattern. The exact final API names may change during implementation, but the
generated code should be equivalent to:

```go
logLevel := new(slog.LevelVar)
logLevel.Set(slog.LevelInfo)

slogLogger := logger.NewCharmSlog(os.Stderr,
    logger.WithTimestamp(true),
    logger.WithSlogLevel(logLevel),
)

compatLogger := logger.FromSlog(slogLogger,
    logger.WithLevelVar(logLevel),
)
```

The generated `props.Props` wiring must preserve compatibility with the current
framework until `Props` itself grows a slog-native field:

```go
p := &props.Props{
    Logger: compatLogger,
    // future, if approved by this spec's open questions:
    // Slog: slogLogger,
}
```

Required generator changes:

- Update `internal/generator/templates/skeleton_root.go` so new scaffolded
  tools use the slog-first Charm construction.
- Add any required imports to generated root files, especially `log/slog` if a
  `slog.LevelVar` is emitted directly.
- Keep generated code compatible with the public `props.Props` shape available
  at that implementation phase.
- Update any generated tests, examples, or command templates that currently
  recommend `logger.NewNoop()` as a default for reusable library code. Command
  tests may still use compatibility loggers where `props.Props` requires them.
- If `Props` gains a slog-native field, update `skeleton_root.go`,
  `skeleton_main.go`, generated command examples, and any manifest regeneration
  logic that preserves root composition.
- Add a migration note for existing generated projects explaining that the old
  `logger.NewCharm` root code still works, but new scaffolds use
  `logger.NewCharmSlog` / `logger.FromSlog`.

Generator testing requirements:

- Unit tests for `internal/generator/templates/skeleton_root.go` must assert the
  generated root includes the slog-first logger construction and no longer emits
  `logger.NewCharm(` as the primary logger path once Phase 2 is implemented.
- Existing skeleton compile tests must be updated so generated projects compile
  with the new imports and logger wiring.
- A regenerate/project dry-run fixture should verify that existing projects are
  not rewritten unexpectedly unless the logger migration is explicitly enabled
  or the manifest/template version requires it.
- If the generator includes an automatic migration for root logger construction,
  add idempotency tests: applying regeneration twice must produce no second diff.

## 10. Public API Plan

Because GTB now treats stable public APIs more strictly, this refactor should be
additive first.

General rule:

1. Add slog-based APIs.
2. Convert internal implementations to slog.
3. Keep old `logger.Logger` APIs as wrappers.
4. Mark old APIs deprecated after equivalent replacements exist.
5. Remove only in an approved major-version migration window.

Example:

```go
func NewController(ctx context.Context, opts ...ControllerOpt) *Controller

func WithSlogLogger(log *slog.Logger) ControllerOpt

// Deprecated: use WithSlogLogger.
func WithLogger(log logger.Logger) ControllerOpt {
    return WithSlogLogger(logger.ToSlog(log))
}
```

For packages already scheduled for extraction, the extracted module may start
with the clean slog API while GTB retains local compatibility wrappers at the
old import path.

## 11. TDD / BDD Strategy

Implementation must follow GTB's TDD workflow. Each phase starts by adding or
updating tests that fail against the current code, then implementing the minimum
change to pass them.

TDD requirements by phase:

- Phase 1 logger package additions: write unit tests for `NewCharmHandler`,
  `NewCharmSlog`, `ToSlog`, `FromSlog`, nil logger behaviour, level filtering,
  and fatal compatibility exit hooks before implementation.
- Phase 2 root wiring: write tests that prove `--debug`, `log.level`, and
  `log.format` still drive the application logger. Include coverage for both
  legacy `props.Logger` compatibility and any new slog-native field if added.
- Phase 2 generator updates: write or update generator tests first so the
  scaffolded root output requires the slog-first construction.
- Phase 3 package migrations: for each package, add tests showing nil slog
  loggers are safe, injected slog loggers receive expected records, and legacy
  wrapper options still work.
- Phase 4 transport/telemetry migration: add request/interceptor/backend tests
  that assert log records are emitted through `slog` and that no package-level
  `logger.Logger` dependency remains where the phase says it should be removed.
- Phase 5 config adapter cleanup: add tests for typed option mapping from
  `config.Containable` into extracted-module option structs.

BDD requirements:

- Add or update E2E scenarios only where behaviour is user-visible. This refactor
  is mostly library/internal, so broad BDD coverage is not required for every
  package migration.
- Root command logging is user-visible and should have an E2E or existing CLI
  scenario updated if the emitted log format, debug visibility, or scaffolded
  CLI behaviour changes.
- Generator behaviour is user-facing. If generated root code changes, add or
  update generator E2E scenarios that scaffold a project, build it, and confirm
  the generated CLI starts with the new logger wiring.
- If no new BDD scenario is added for a phase, the implementation notes should
  state why unit/integration coverage is sufficient.

## 12. Testing Strategy

Unit tests:

- `pkg/logger`: `NewCharmHandler` returns an enabled handler; `NewCharmSlog`
  logs through the Charm handler; `ToSlog(nil)` returns a no-op logger; `FromSlog`
  maps levels and honours test exit hooks for fatal compatibility.
- Migrated packages: nil logger defaults to no-op; slog logger is honoured;
  debug-only branches use `Enabled`.
- Compatibility wrappers: old `logger.Logger` options still affect logs via
  `logger.ToSlog`.

Integration tests:

- Root command logging still respects `--debug`, `log.level`, and `log.format`.
- Existing E2E scenarios keep human-friendly Charm output where applicable.

Regression tests:

- Reusable packages do not call `os.Exit` through logging paths.
- Extractable packages do not import `pkg/logger` or `pkg/config` after their
  migration phase completes. This can be enforced with a small package-import
  test or documented as a review checklist item.
- Generator output contains the slog-first root logger wiring and continues to
  compile.

## 13. Documentation

Update:

- `docs/explanation/components/logger.md`
- `docs/explanation/concepts/architecture.md`
- `docs/development/dependency-management.md`
- `docs/reference/migration/v0.x-to-v1.0.md` or a new migration note, depending
  on release/version policy at implementation time.
- Chat extraction spec, if this spec changes its final logging/config seam names.

Documentation should clearly distinguish:

- application logging in GTB,
- library logging in reusable/extracted modules,
- direct user output via `pkg/output` or command writers,
- configuration loading in GTB,
- typed options in extracted modules.

## 14. Implementation Phases

### Phase 1: Logger package additions

- Add `NewCharmHandler`.
- Add `NewCharmSlog`.
- Add `ToSlog`, `FromSlog`, and compatibility options.
- Add no-op/discard slog handler helpers if needed.
- Add tests for compatibility and Charm handler construction.

### Phase 2: Root application logger wiring

- Update GTB root construction to create and store a slog-first application
  logger while preserving `props.Logger` compatibility.
- Make log level changes update a shared `slog.LevelVar`.
- Keep existing Charm output and formatter behaviour.
- Update generator templates to use the new construction pattern for newly
  scaffolded tools, while preserving compatibility for existing tools.
- Update generator tests before changing the template, then make the tests pass.

### Phase 3: First-wave package migration

- Migrate `pkg/chat` in line with the chat extraction spec.
- Migrate `pkg/controls`.
- Migrate `pkg/config` diagnostics to `*slog.Logger`.
- Keep deprecated wrappers for old public APIs.

### Phase 4: Transport and telemetry migration

- Migrate `pkg/http`, `pkg/grpc`, `pkg/gateway`, and telemetry packages to
  slog-based logging.
- Replace `logger.Level` with `slog.Level` or local level types where needed.
- Promote any internal support abstractions needed for extraction.

### Phase 5: Config adapter cleanup

- Introduce typed option constructors for release providers, transports, TLS,
  and chat adapters.
- Keep `pkg/config.Containable` usage inside GTB composition/setup layers.
- Remove config imports from packages being prepared for extraction.

## 15. Open Questions

_All resolved in the 2026-07-10 review; see §0 for the authoritative decisions._

1. **Props.Logger vs Props.Slog** — **Resolved (D1):** no `Props.Slog`. Redefine
   `logger.Logger` as an interface mirroring `*slog.Logger` and keep `Props.Logger`
   typed as it, so a `*slog.Logger` passes in directly. Breaking, pre-1.0
   acceptable.
2. **Deprecation timing** — **Resolved (D4):** deprecate the legacy facade
   immediately once slog-first replacements exist.
3. **`*slog.Logger` vs narrow interface for extracted modules** — **Resolved:**
   `*slog.Logger` by default; a narrow package-local interface only when a module
   needs ≤2 methods and no slog features.
4. **`pkg/config` as its own module** — **Resolved:** yes, an early standalone
   extraction and an acceptable lightweight dependency, gated on `pkg/config`
   first dropping its `pkg/logger` dependency (accepting `*slog.Logger`). Aligns
   with the config-adapters spec reframe.
5. **`logger.Print`** — **Resolved:** keep on the compatibility surface for
   command convenience; forbidden in reusable packages; command migration to
   Cobra writers / `pkg/output` is opportunistic, not blocking.
6. **Generator auto-migration** — **Resolved (D5):** regeneration auto-migrates
   existing projects' root logger construction, with idempotency tests.

## 16. Acceptance Criteria

- New reusable package APIs can be written without importing `pkg/logger`.
- GTB still defaults to Charm-styled human log output.
- Existing public `logger.Logger`-based APIs continue to compile during the
  transition.
- `pkg/chat` extraction can use the shared logging/config guidance without
  inventing a GTB-specific logger dependency.
- Extracted module design docs state that config is supplied as typed options or
  package-local lookup interfaces, not `pkg/config.Containable`.
- New generated projects use the slog-first logger construction and compile.
- The implementation is test-first, with unit/integration coverage for each
  phase and BDD coverage for user-visible root/generator behaviour where
  behaviour changes.
