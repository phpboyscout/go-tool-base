---
title: "OpenTelemetry Observability Specification"
description: "OTel-native traces, metrics and logs for GTB services, exported over OTLP, organised as subpackages of pkg/telemetry over a shared OTel core, with two distinct consent models: informed consent for CLI analytics and implied consent for web-service observability."
date: 2026-06-01
status: IMPLEMENTED
tags:
  - specification
  - telemetry
  - observability
  - opentelemetry
  - otlp
  - tracing
  - metrics
  - logging
  - http
  - grpc
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude Opus 4.8
    role: AI drafting assistant
---

# OpenTelemetry Observability Specification

Authors
:   Matt Cockayne, Claude Opus 4.8 *(AI drafting assistant)*

Date
:   1 June 2026

Status
:   IMPLEMENTED

---

## Overview

GTB already gives a long-running service request *logging* (`pkg/http.LoggingMiddleware`,
`pkg/grpc.LoggingInterceptor`, see the
[Transport Middleware and Logging spec](2026-03-26-transport-logging-middleware.md))
and a consent-gated product *analytics* pipeline (`pkg/telemetry`, exporting
OTel log records over OTLP). It has **no** request **metrics** and **no**
distributed **tracing** for the HTTP and gRPC servers. An operator running a GTB
service today can read its logs but cannot see request rates, latency
distributions, error ratios, or a trace of a request as it crosses the gateway
into the gRPC backend.

This spec adds the two missing observability pillars and unifies all three
(traces, metrics, logs) on a single OpenTelemetry pipeline exported over
**OTLP/HTTP** (push, not pull). It introduces no new top-level package: the new
code lives as subpackages of `pkg/telemetry` over a shared OTel core that the
existing analytics backend can also adopt.

### Motivation

- **Operational visibility.** Logs answer "what happened"; metrics answer "how
  often and how fast"; traces answer "where the time went, across services". A
  service that bridges REST → gateway → gRPC (the shape the web-service tutorial
  builds) is exactly where a trace earns its keep.
- **One standard, not three.** The framework already commits to OpenTelemetry
  for the analytics log backend. Doing metrics and tracing the OTel way keeps a
  single mental model, a single exporter family, and a single resource
  description across every signal.
- **Push, not pull.** A scrapeable `/metrics` endpoint (Prometheus pull) couples
  collection to scrape interval and service discovery, and suits neither
  short-lived processes nor egress-restricted networks. OTLP push matches how the
  analytics pipeline already ships, and lets every signal leave through one
  collector.
- **Two audiences, two consent models.** CLI analytics is the *vendor* learning
  about *users* — it demands informed, opt-in consent. Web-service observability
  is the *operator* instrumenting *their own* service for *their own* collector —
  consent is implied by deployment. Conflating the two is a footgun; this spec
  keeps them on separate, clearly-named paths under one package.

### Terminology

| Term | Definition |
|------|-----------|
| **Signal** | One of the three OTel data types: traces, metrics, logs. |
| **OTLP** | OpenTelemetry Protocol; here always OTLP/HTTP (`:4318`). |
| **Provider** | An OTel SDK `TracerProvider` / `MeterProvider` / `LoggerProvider` — the thing instrumentation reads from and the SDK exports from. |
| **Resource** | The `service.name`/`service.version`/`deployment.environment` attributes identifying who is emitting (semconv). |
| **Instrumentation** | The per-request code that creates spans / records metrics — here the OTel contrib libraries `otelhttp` and `otelgrpc`. |
| **Analytics** | The existing `pkg/telemetry.Collector` product-usage pipeline. |
| **Observability** | The new traces/metrics/logs pillars for a running service. |
| **Informed consent** | Off by default; the user must opt in. The analytics model. |
| **Implied consent** | Enabled by the operator's configuration; no end-user prompt. The observability model. |

### Design decisions

1. **OTel-only, OTLP push.** Every signal uses the OpenTelemetry SDK and exports
   over OTLP/HTTP. No Prometheus `/metrics` endpoint, no `client_golang`, no
   vendor SDKs in the core. (Vendor specifics stay possible via standard OTLP
   collector routing, not framework code.)
2. **No new top-level package.** All new code is subpackages of `pkg/telemetry`,
   over a shared `pkg/telemetry/otel` core (resource builder + OTLP exporter
   factory + endpoint/header/insecure config). The existing `backend_otel.go`
   is refactored onto this core in the same branch, so the analytics path and the
   observability path share one exporter/resource implementation with no
   duplication.
3. **`telemetry.*` config root.** Observability reads `telemetry.tracing.*`,
   `telemetry.metrics.*`, `telemetry.logs.*`, inheriting shared OTLP settings
   from `telemetry.*`, in the same shared-plus-override style as `pkg/tls`.
   Standard `OTEL_*` environment variables are honoured (and take the precedence
   the OTel SDK defines).
4. **Two consent models, one package.** The analytics `Collector` keeps its
   informed-consent gate (`telemetry.enabled`, opt-in, `ForceEnabled` override).
   Observability runs on implied consent: it is enabled by the operator setting
   `telemetry.<signal>.enabled` / an endpoint, never routed through the user
   opt-in prompt, never a noop because a *user* didn't consent. See
   [The consent model](#the-consent-model).
5. **Standard instrumentation, not hand-rolled.** Spans and server metrics come
   from the OTel contrib libraries (`otelhttp`, `otelgrpc`), which implement the
   HTTP/RPC semantic conventions. The framework supplies thin one-line wiring and
   a clean hook for custom instrumentation; it does not reimplement what contrib
   already does (decision 1c from design review).
6. **Global providers, zero transport coupling.** `Setup` installs the providers
   as the OTel globals (`otel.SetTracerProvider`, …). `otelhttp`/`otelgrpc` read
   the globals, so `pkg/http` and `pkg/grpc` need not import `pkg/telemetry/*` —
   the only new coupling is on the contrib libraries.
7. **Lifecycle on the Controller.** Provider shutdown (which flushes batched
   spans/metrics/logs) registers as a `controls` service, so a SIGTERM drains
   telemetry the same way it drains in-flight requests.
8. **`props.Props` is the foundation.** `Setup` takes `*props.Props` and reads
   logger, config and version from it, continuing the pattern by which
   `p.Collector` already rides props for both CLI and web service.

---

## The consent model

This is the load-bearing distinction of the spec and must be explicit in code,
config and docs.

### Informed consent — CLI analytics (unchanged)

`pkg/telemetry.Collector` collects *usage data about the user* (hashed machine
ID, command, exit code, optionally redacted args). It is **off by default** and
returns a noop until the user opts in via the telemetry command
(`telemetry.enabled = true`). `TelemetryConfig.ForceEnabled` lets an enterprise
tool author override the prompt through embedded config where collection is a
contractual requirement. This path is untouched by this spec.

### Implied consent — web-service observability (new)

Observability collects *operational data about the service*, emitted by the
operator to a collector the operator runs. There is no end-user to prompt:
consent is implied by the operator configuring an OTLP endpoint and enabling a
signal. Therefore:

- Observability is gated **only** by `telemetry.<signal>.enabled` (and/or a
  resolvable endpoint), set by the operator. It is **never** gated by
  `telemetry.enabled` (the analytics opt-in), and disabling analytics does not
  disable observability, nor vice versa.
- There is **no** consent prompt, **no** machine-ID hashing, **no** GDPR
  deletion flow on this path. Those are analytics concerns and stay on the
  analytics path.
- The principle underneath: **the kind of data decides the consent model.**
  Personal/usage data → informed consent. Operational data → implied consent.
  CLI and web service are the canonical homes of each, but the axis is the data,
  not the surface.

Both paths share the `telemetry.*` config root and the `pkg/telemetry/otel`
export core; they do not share a gate.

---

## Package structure

```
pkg/telemetry/
    (existing analytics: Collector, Event, spill, machine, backends, posthog/, datadog/)
    otel/            shared OTel core
        resource.go      service.name/version/environment from props + semconv
        exporter.go      OTLP/HTTP exporter factory (endpoint, headers, insecure, OTEL_* env)
        config.go        telemetry.* shared + per-signal override resolution
    tracing/
        tracing.go       TracerProvider setup over telemetry/otel; sampler config
    metrics/
        metrics.go       MeterProvider setup (PeriodicReader → OTLP metric exporter)
    logs/
        logs.go          slog → OTel LoggerProvider bridge (otelslog) + OTLP log exporter
    observability.go     Setup(ctx, p, controller): build enabled providers, set globals,
                         register shutdown on the controller; returns a Shutdown func

pkg/http/
    otel.go              OTelMiddleware(server, opts...) — thin otelhttp.NewMiddleware wrapper (Chain-compatible)
pkg/grpc/
    otel.go              OTelStatsHandler(opts...) grpc.ServerOption — thin otelgrpc.NewServerHandler wrapper
```

No new top-level package; `pkg/http`/`pkg/grpc` gain one small file each.

---

## Public API

### `pkg/telemetry/otel` — shared core

```go
// Resource builds the OTel resource (service.name/version/environment) from props.
func Resource(p *props.Props) (*resource.Resource, error)

// Settings is the resolved OTLP target for one signal: endpoint, headers, TLS.
type Settings struct {
    Enabled  bool
    Endpoint string            // OTLP/HTTP base URL, e.g. https://collector:4318
    Headers  map[string]string // exporter headers (auth); sensitive values redacted in logs
    Insecure bool              // plaintext OTLP — local collectors only
}

// Resolve reads telemetry.<signal>.* overlaid on telemetry.* shared defaults,
// then applies standard OTEL_* environment precedence.
func Resolve(cfg config.Containable, signal string) Settings
```

### `pkg/telemetry/tracing`, `/metrics`, `/logs`

Each exposes a constructor returning a provider plus its shutdown:

```go
// tracing
func NewProvider(ctx context.Context, res *resource.Resource, s otel.Settings,
    opts ...Option) (*sdktrace.TracerProvider, error)

// metrics
func NewProvider(ctx context.Context, res *resource.Resource, s otel.Settings,
    opts ...Option) (*sdkmetric.MeterProvider, error)

// logs
func NewProvider(ctx context.Context, res *resource.Resource, s otel.Settings,
    opts ...Option) (*sdklog.LoggerProvider, error)
// plus a slog.Handler bridge so the GTB logger also writes OTel log records.
func Handler(lp *sdklog.LoggerProvider, name string) slog.Handler
```

### `pkg/telemetry` — the one-line entrypoint

```go
// Setup builds every enabled observability provider from p.Config, installs them
// as the OTel globals, and registers a shutdown service on the controller so the
// providers flush on graceful stop. Signals that are not enabled are skipped.
// Returns a Shutdown func for callers without a controller (e.g. CLIs).
//
//   shutdown, err := telemetry.Setup(ctx, p, controller)
func Setup(ctx context.Context, p *props.Props, controller controls.Controllable) (Shutdown, error)

type Shutdown func(context.Context) error
```

### Transport wiring helpers

```go
// pkg/http — Chain-compatible; emits both server spans and server metrics via
// the global providers. One line in the reader's existing middleware chain.
func OTelMiddleware(server string, opts ...otelhttp.Option) Middleware

// pkg/grpc — a stats handler that emits both server spans and server metrics.
// Pass straight to Register's variadic ServerOptions.
func OTelStatsHandler(opts ...otelgrpc.Option) grpc.ServerOption
```

Reader-facing wiring, end to end:

```go
// in the serve command, after the controller exists:
shutdown, err := telemetry.Setup(ctx, p, controller) // builds + installs providers, flushes on stop
if err != nil { return err }
_ = shutdown // controller owns it; kept for non-controller callers

// gRPC: spans + metrics for every RPC
grpcSrv, _ := gtbgrpc.Register(ctx, "grpc", controller, p.Config, p.Logger,
    gtbgrpc.OTelStatsHandler())

// HTTP/gateway: spans + metrics for every request, alongside logging
chain := gtbhttp.NewChain(
    gtbhttp.OTelMiddleware("macguffin"),
    gtbhttp.LoggingMiddleware(p.Logger),
)
_, _ = gtbhttp.Register(ctx, "http", controller, p.Config, p.Logger, mux,
    gtbhttp.WithMiddleware(chain))
```

Custom, business-level instrumentation uses the OTel globals directly — no
framework API to learn:

```go
tracer := otel.Tracer("macguffin/store")
ctx, span := tracer.Start(ctx, "Store.Create")
defer span.End()
```

---

## Configuration

All under the `telemetry.*` root, resolved shared-then-override like `pkg/tls`:

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `telemetry.endpoint` | string | — | Shared OTLP/HTTP base URL for all signals. |
| `telemetry.headers` | map | — | Shared exporter headers (e.g. auth token). |
| `telemetry.insecure` | bool | `false` | Shared: plaintext OTLP (local collectors only). |
| `telemetry.tracing.enabled` | bool | `false` | Enable trace export. |
| `telemetry.tracing.endpoint` | string | shared | Per-signal endpoint override. |
| `telemetry.tracing.sampling` | float | `0.1` | Parent-based ratio sampler (production-safe; set `1.0` to see every trace in dev). |
| `telemetry.metrics.enabled` | bool | `false` | Enable metric export. |
| `telemetry.metrics.endpoint` | string | shared | Per-signal endpoint override. |
| `telemetry.metrics.interval` | duration | `60s` | PeriodicReader export interval. |
| `telemetry.logs.enabled` | bool | `false` | Enable OTLP log export (stderr logs stay regardless). |
| `telemetry.logs.endpoint` | string | shared | Per-signal endpoint override. |

- `telemetry.enabled` (analytics opt-in) is **independent** of these and gates
  only the analytics `Collector`.
- Standard `OTEL_*` env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`,
  `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_TRACES_SAMPLER`, …) are read by the
  SDK and take their defined precedence; the `telemetry.*` keys are the friendly
  front door for tools that prefer GTB config files.
- Per-signal `headers`/`insecure` override the shared values individually.

---

## Dependencies

Added, all OTel, version-aligned to the pinned `go.opentelemetry.io/otel v1.43.0`
SDK (the `otel/log` signal stays on its `v0.x` line, as the analytics backend
already uses):

- `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`
- `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp`
- `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc`
- `go.opentelemetry.io/contrib/bridges/otelslog`
- `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` (promote from indirect)

Already present: `otel`, `otel/metric`, `otel/trace`, `otel/sdk`, `otel/log`,
`otel/sdk/log`, `otlploghttp`. No Prometheus, no `client_golang`, no vendor SDKs.

---

## Validation (gap-first, against the Macguffin harness)

Per the initiative's method, the implementation is proven on the `widgetsvc`
reference service before a word of the article is written:

1. Run a local OTel collector (or Jaeger all-in-one + an OTLP metrics/logs sink).
2. Wire `telemetry.Setup` + the two transport helpers into `serve`.
3. Drive REST and gRPC traffic; confirm:
   - a single trace spans gateway HTTP → gRPC handler → `Store` custom span;
   - server metrics (`http.server.request.duration`, `rpc.server.duration`,
     request counts, error counts) arrive at the collector;
   - logs arrive as OTel records correlated by `trace_id`/`span_id`, while the
     human-readable stderr log is unchanged;
   - a SIGTERM flushes all three before exit (no dropped spans).
4. Record any framework friction the way the v0.6.0 spike notes did; fix in-tree.

---

## Testing strategy

- **`telemetry/otel`**: resolution precedence (shared vs per-signal vs `OTEL_*`),
  resource attributes, exporter option building, header redaction in logs.
- **`tracing`/`metrics`/`logs`**: provider construction with a stub exporter;
  shutdown flushes; disabled signal yields a noop provider.
- **`Setup`**: only enabled signals are built; globals installed; a shutdown
  service is registered on the controller; idempotent shutdown.
- **Transport helpers**: `OTelMiddleware` produces a server span and a duration
  metric (in-memory exporter) and composes in a `Chain` with logging;
  `OTelStatsHandler` does the same over `bufconn` for unary and stream.
- **Consent isolation**: observability runs with `telemetry.enabled=false`;
  analytics noop with observability enabled — neither gate touches the other.
- **Coverage target**: 90% on new files, matching the logging middleware spec.

---

## Documentation and article outputs

This work feeds three artefacts, all from one validated implementation:

- `docs/components/observability.md` — the framework component reference.
- **Tutorial — web-service part 6**: actionable "add traces, metrics and logs to
  your service" using the helpers above, validated on the Macguffin service,
  dated to the next slot in the series (2026-05-31).
- **Technical deep-dive** (standalone): "OpenTelemetry the right way in a Go
  service", built around the informed-vs-implied consent distinction, the
  one-pipeline-three-signals design, and push-vs-pull.

---

## Implementation phases

### Phase 1 — shared OTel core (`pkg/telemetry/otel`)
Resource builder, OTLP/HTTP exporter factory, `telemetry.*` resolution with
`OTEL_*` precedence and per-signal override. Unit tests. Refactor
`backend_otel.go` onto the core in the same phase and re-run the analytics tests.

### Phase 2 — tracing
`tracing.NewProvider` (batch processor, parent-based ratio sampler, OTLP trace
exporter). `pkg/grpc.OTelStatsHandler` and `pkg/http.OTelMiddleware`. Validate a
cross-transport trace on the harness.

### Phase 3 — metrics
`metrics.NewProvider` (PeriodicReader + OTLP metric exporter). Confirm
`otelhttp`/`otelgrpc` server metrics flow from the same handlers; add a custom
business metric example. Validate on the harness.

### Phase 4 — logs
`logs.NewProvider` + `otelslog` bridge so the GTB logger also emits OTel records.
`trace_id`/`span_id` correlation on **both** paths: automatic on the OTLP export
via the bridge, and added to the **stderr** lines via a small `pkg/logger` change
that pulls span context from the request `ctx` when present. Human-readable
stderr output is otherwise preserved. Validate correlation on the harness
(`kubectl logs`-style stderr shows trace ids without a collector).

### Phase 5 — `Setup`, lifecycle, config, docs
`telemetry.Setup` wiring all enabled signals, global install, controller-managed
shutdown. Full config surface + `OTEL_*` precedence. Component doc. End-to-end
harness run with a collector; capture the screenshots/notes the articles need.

---

## Resolved decisions

These four were settled in design review (2026-06-01); recorded here so the
implementation has no ambiguity:

1. **`backend_otel.go` refactor — DO IT NOW.** Fold the analytics OTLP exporter
   onto `pkg/telemetry/otel` in this branch. One exporter/resource implementation
   serves both the analytics and observability paths; no duplication. The
   analytics consent gate is unaffected — only the export plumbing is shared.
2. **stderr trace correlation — IN SCOPE.** As well as the automatic correlation
   on OTLP-exported logs, the stderr logger gains `trace_id`/`span_id` when a span
   is active in context (a small `pkg/logger` change), so local `kubectl logs`
   show correlation without a collector.
3. **Sampler default — `0.1`, parent-based.** Production-safe out of the box. The
   tutorial tells the reader to set `telemetry.tracing.sampling = 1.0` to see
   every trace while following along.
4. **Generator impact — NONE.** Server and observability wiring stay
   hand-written, consistent with the web-service initiative's decision not to
   scaffold `serve`. The tutorial guides the reader through the wiring.
