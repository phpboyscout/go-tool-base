---
title: Observability
description: OTel-native traces, metrics and logs for GTB services, exported over OTLP, under pkg/telemetry.
date: 2026-06-01
tags: [components, telemetry, observability, opentelemetry, otlp, tracing, metrics, logging]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Observability

GTB services emit the three OpenTelemetry signals — **traces**, **metrics** and **logs** — over **OTLP/HTTP** to a collector. The plumbing lives as subpackages of `pkg/telemetry`, over a shared OTel core, and the per-request instrumentation lives beside the request logging in `pkg/http` and `pkg/grpc`.

This is distinct from the product **analytics** in the same package. Analytics is the vendor learning about users and runs on **informed consent** (opt-in). Observability is the operator instrumenting their own service and runs on **implied consent** (configuration). The two share the `telemetry.*` config root and the export core, but never a consent gate. See [The two consent models](#the-two-consent-models).

## Package layout

| Package | Role |
|---------|------|
| `pkg/telemetry/otelcore` | Shared core: OTLP endpoint parsing, the service `resource`, and `telemetry.*` config resolution. Imports no signal exporters. |
| `pkg/telemetry/tracing` | `TracerProvider` over an OTLP trace exporter; parent-based ratio sampler. |
| `pkg/telemetry/metrics` | `MeterProvider` over an OTLP metric exporter; periodic push. |
| `pkg/telemetry/logs` | `LoggerProvider` over an OTLP log exporter, plus an `otelslog` bridge handler. |
| `pkg/telemetry` (`Setup`) | Builds the enabled providers, installs the OTel globals, registers shutdown on the controller. |
| `pkg/http`, `pkg/grpc` | `OTelMiddleware` / `OTelStatsHandler`: per-request spans + server metrics, reading the global providers. |

## Setup

`telemetry.Setup` is the one call that wires everything from `props.Props`:

```go
func Setup(ctx context.Context, p *props.Props, controller controls.Controllable) (Shutdown, error)
```

It resolves each signal from config, builds only the enabled providers, installs them as the OTel globals, sets the W3C trace-context + baggage propagators, and — when a controller is supplied — registers a `telemetry` service so the providers flush on graceful stop. Call it in `serve`, after the controller exists:

```go
controller := controls.NewController(ctx, controls.WithLogger(p.Logger))

if _, err := telemetry.Setup(ctx, p, controller); err != nil {
	return err
}
```

Signals whose `telemetry.<signal>.enabled` is false are skipped, so an unconfigured service pays nothing.

**Teardown on partial failure.** `Setup` installs each provider's global (tracer, then meter, then logger) in order, accumulating its shutdown func as it goes. If a later signal fails to build, the providers already installed are **not** stranded: their shutdowns are run best-effort, in reverse install order, before `Setup` returns the error. So a failure midway through setup leaves no live global providers (and no orphaned background batch goroutines) behind.

## Transport instrumentation

Spans and the standard server metrics come from the OTel contrib libraries, wrapped as one-line helpers that read the global providers `Setup` installed. `OTelMiddleware` is just one entry in the HTTP server chain (and `OTelStatsHandler` its gRPC analogue) — it composes alongside security-headers, rate-limit, and the rest. See [Transport Middleware & Resilience](../concepts/transport-middleware.md) for the chain pattern and ordering.

gRPC — a stats handler passed to `Register`:

```go
grpcSrv, _ := grpc.Register(ctx, "grpc", controller, p.Config, p.Logger,
	grpc.OTelStatsHandler())
```

HTTP — a `Chain`-compatible middleware. Put it **ahead** of the logging middleware so the access log can read the active span:

```go
chain := http.NewChain(
	http.OTelMiddleware("macguffin"),
	http.LoggingMiddleware(p.Logger),
)
http.Register(ctx, "http", controller, p.Config, p.Logger, mux, http.WithMiddleware(chain))
```

Custom, business-level instrumentation needs no GTB API — use the OTel globals directly:

```go
ctx, span := otel.Tracer("macguffin/store").Start(ctx, "Store.Create")
defer span.End()
```

## Trace correlation in logs

When `OTelMiddleware` / `OTelStatsHandler` precede the request logging, the logging middleware and interceptor pull the active span from the request context and add `trace_id` and `span_id` to the access log. Because they are ordinary log fields, the correlation reaches **both** the human-readable stderr output (so `kubectl logs` line up with traces) and any OTLP log records exported via the `logs` bridge. With no active span the fields are simply absent.

## Configuration

All under the `telemetry.*` root, resolved shared-then-per-signal (the same shared-plus-override style as `pkg/tls`). An empty endpoint is intentional — it lets the OTel SDK read the standard `OTEL_EXPORTER_OTLP_*` environment variables.

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `telemetry.endpoint` | string | — | Shared OTLP/HTTP base URL for all signals. |
| `telemetry.headers` | map | — | Shared exporter headers (e.g. an auth token). |
| `telemetry.insecure` | bool | `false` | Shared: plaintext OTLP (local collectors only). |
| `telemetry.tracing.enabled` | bool | `false` | Enable trace export. |
| `telemetry.tracing.endpoint` | string | shared | Per-signal endpoint override. |
| `telemetry.tracing.sampling` | float | `0.1` | Parent-based ratio. Set `1.0` to record every trace in dev. |
| `telemetry.metrics.enabled` | bool | `false` | Enable metric export. |
| `telemetry.metrics.interval` | duration | `60s` | Periodic export interval. |
| `telemetry.logs.enabled` | bool | `false` | Enable OTLP log export (stderr output is unaffected). |

Per-signal `endpoint`, `headers` and `insecure` override the shared values individually.

Long-lived code that needs to react to reloads should bind a resolved signal
snapshot through `otelcore.ObserveSettingsFromConfig`. The returned value
satisfies `otelcore.SettingsSource`, exposing `Current()` and `Version()` without
coupling the signal package to GTB's config container. A version change means
the full resolved settings snapshot changed; existing exporters are not rebuilt
automatically, so the owning package must explicitly rebuild or restart a
provider when that is the desired behaviour.

## The two consent models

The defining distinction of this package:

- **Informed consent — analytics.** `telemetry.Collector` collects usage data about the user. It is off by default and runs only on opt-in (`telemetry.enabled`), with `TelemetryConfig.ForceEnabled` for enterprise embedded config. Unchanged by observability.
- **Implied consent — observability.** `Setup` collects operational data about the service, emitted by the operator to the operator's own collector. It is gated **only** by `telemetry.<signal>.enabled`, never by `telemetry.enabled`. Enabling tracing does not enable analytics, and disabling analytics does not disable tracing. There is no consent prompt, no machine-ID hashing and no GDPR deletion flow on this path — those are analytics concerns.

The principle: **the kind of data decides the consent model.** Personal/usage data needs informed consent; operational data runs on implied consent. The CLI and the web service are the canonical homes of each.

## Lifecycle

Each provider batches and exports asynchronously, and flushes its buffer on `Shutdown`. `Setup` registers that shutdown as a controller service, so a SIGTERM drains telemetry in the same graceful window that drains in-flight requests — no dropped spans on a clean stop. Callers without a controller get a `Shutdown` return value to defer themselves.
