---
title: "Handover: config-section-adapters (remaining work)"
description: "State of the config decoupling / slog-first branch and what a follow-up session needs to finish the config migration."
date: 2026-07-10
tags: [handover, config, slog, module-extraction]
---

# Handover: config migration (config-section-adapters)

This note hands off the `refactor/config-section-adapters` branch (MR !200) to a
follow-up session. It records what is **done**, the **patterns** to reuse, and
the **remaining** config-migration work.

## Status

- **slog-first logging** (`docs/development/specs/2026-07-07-slog-first-extraction-seams.md`, APPROVED):
  **COMPLETE**. `logger.Logger` is now a `*slog.Logger` mirror, and every
  extraction-candidate package (`chat`, `controls`, `config`, `http`, `grpc`,
  `gateway`, `telemetry` + `datadog`/`posthog`, `errorhandling`) has been
  migrated off `pkg/logger` to `*slog.Logger`. Only GTB adapter files
  (`*config_adapter.go`, `*FromProps`) and the composition layer (`pkg/cmd`,
  `pkg/setup`, `pkg/props`, `pkg/docs`, `pkg/utils`) still import `pkg/logger`.
- **config-section-adapters** (`docs/development/specs/2026-07-07-config-section-adapters-for-extraction.md`, IN PROGRESS):
  **first wave DONE** (`chat`, `tls`, `http`, `grpc`, `gateway`,
  `telemetry/otelcore`, `vcs` + providers + `repo`). Later waves remain (below).

## Patterns to reuse

### Logger boundary
- Packages accept `*slog.Logger`; a nil logger resolves to
  `slog.New(slog.DiscardHandler)` (quiet by default — the app wires logging).
- GTB adapters convert `props.Logger` (the `logger.Logger` interface) to
  `*slog.Logger` with **`logger.ToSlog(props.Logger)`** at the boundary. `ToSlog`
  is nil-safe, returns a `*slog.Logger` unchanged, else builds one over the
  logger's `Handler()` (so buffer capture is preserved in tests).
- Runtime level/format live only in the GTB root via `logger.SetLevel` /
  `logger.SetFormatter` helpers (the optional `Leveller`/`Reformatter`
  interfaces). A plain `*slog.Logger` no-ops for them.
- Tests: wrap logger args in `logger.ToSlog(...)`; replace custom capture
  loggers with `logger.NewCaptureHandler()` + `slog.New(capture)` and assert on
  `capture.Records()` / `Messages()` / `Contains()`.

### Config typed-struct adapters
- Extracted packages own typed config structs (`mapstructure` tags). GTB's
  `config_adapter.go` does the config reads: `config.UnmarshalSection[T]` (one
  shot) or `config.ObserveSection[T]` (long-lived, reload-aware), merges package
  defaults, validates, resolves credentials.
- Choose the mechanism by shape (see the spec's §12 note):
  - **`UnmarshalSection`** for self-contained sections (`chat`, `http`/`grpc`
    server settings).
  - **Narrow lookup interface** (`release.Config`/`vcs.TokenConfig`) for provider
    registries with dynamic, provider-specific keys (`vcs`) — do NOT force typed
    decode there; it already imports zero GTB packages.
  - **Manual `IsSet`-override** for shared↔transport/signal fallback (`tls`,
    `telemetry/otelcore`).
  - **Composition** across multiple prefixes (`gateway`).
- `config_adapter.go` is the GTB adapter file that will NOT travel with the
  extracted module. Keep all `pkg/config` + `pkg/props` coupling there; package
  cores stay clean.

## Remaining config-migration work

Per `config-section-adapters` §8 (later waves) and §9 Phase 6:

1. **Packages that currently need no config** — add typed structs only if/when
   GTB introduces config-driven behaviour (deferred, not speculative):
   `authn` (API-key/JWT/mTLS config in the transports), `output.Config`
   (deferred — output resolves format from the `--output` flag, not a config
   section; adding it now would be dead surface), `browser`, `workspace`,
   `forms`, `changelog`, `redact`, `regexutil`, `openapi`, `errorhandling`
   `HelpConfig`.
2. **Root `pkg/telemetry`** — the `pkg/props` **type** coupling is now **DONE**
   (`docs/development/specs/2026-07-10-telemetry-props-decoupling.md`,
   IMPLEMENTED): `EventType`/`DeliveryMode` + constants moved to the
   dependency-free `pkg/telemetrytypes` leaf; `pkg/props` keeps them as type
   aliases (zero downstream break); `pkg/telemetry` core no longer imports
   `pkg/props`. **Still remaining before extraction**: the larger
   product-analytics/consent ↔ observability split (see the extraction report's
   telemetry section) — a separate effort, not a config/props-type concern.
3. **Phase 6 — automated import-boundary check**: **DONE**.
   `test/architecture/boundary_test.go` is now a table-driven guard over
   `pkg/props` **and** `pkg/logger`, covering all first-wave cores plus
   `pkg/telemetry`. No separate CI job is needed — `go test ./...` (the cicd
   `go-test` MR component) already runs `test/architecture`; the earlier
   `just ci` worry was moot.
4. **Open questions still deferred**: Q1 `SectionInConfig` (not shipped —
   `SectionExists` + `IsSet` cover the telemetry-consent case); Q5 the extracted
   `pkg/config` module name (defer to the config-extraction spec).
5. **`vcs/release`** received a registry-signature narrowing rather than a full
   typed adapter (judged adequate since its providers own the typed settings) —
   revisit if/when extracting the releases module.

## Gotchas & conventions

- **Mockery churn**: `mockery` / `just mocks` regenerates the whole `mocks/`
  tree with import-order churn. After an interface change, regenerate then keep
  ONLY the affected package's mocks (`git checkout`/`git clean` the rest).
- **slog `int`→`int64`**: slog normalises integer attributes to `int64`; tests
  asserting on captured integer key/values must use `assert.EqualValues`.
- **`contextcheck`**: request/RPC logging uses `l.Log(ctx, level, msg)` — thread
  the request/RPC context through, never `context.Background()`.
- **Adding imports**: prefer `goimports -w`; ad-hoc `sed` insertion can misorder
  the group or leave a stray blank line (run `gofmt -w` after).
- **`internal/agent` test**: `TestGoBuildTool_SuccessOnTrivialModule` fails with
  `error obtaining VCS status: exit status 128` — an environmental Go
  build-stamp failure in this sandbox, pre-existing on `main` and unrelated to
  this work. Exclude it: `go test $(go list ./... | grep -v internal/agent)`.

## Verification

```
go build ./...
go test $(go list ./... | grep -v internal/agent)
golangci-lint run ./...
go test ./test/architecture/...   # props import-boundary guard
```
