---
title: Backends
description: Pluggable backends, selection precedence, delivery modes, and buffering.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Backends

## Backends

### Noop (disabled state)

Used when telemetry is disabled or no backend is configured. Silently discards all events.

### Stdout (debugging)

Writes events as pretty-printed JSON. Useful for development.

```go
telemetry.NewStdoutBackend(os.Stdout)
```

### File (local-only mode)

Appends events as newline-delimited JSON to a local file. Activated when the user sets `telemetry.local_only: true` in config or `TELEMETRY_LOCAL=true`.

```go
telemetry.NewFileBackend("/path/to/telemetry.log")
```

### HTTP

POSTs events as a JSON array to an endpoint. `Send` **returns** transport-level failures — a refused connection, a timeout, or a non-2xx status — rather than swallowing them. This is deliberate: under `DeliveryAtLeastOnce` the spill/retry layer must see the failure to retain the batch instead of treating it as delivered. The error does **not** block the user: `Flush` and the spill replay log it (at warn/debug) and continue; the error only informs the retain-vs-delete decision. Non-2xx responses are additionally logged at debug level.

```go
telemetry.NewHTTPBackend("https://analytics.example.com/events", logger)
```

### OpenTelemetry (OTLP)

Exports events as OTel log records via OTLP/HTTP. Compatible with Grafana Cloud, OpenTelemetry Collector, Datadog Agent, and any OTel-capable backend.

```go
backend, err := telemetry.NewOTelBackend(ctx,
    "https://otlp-gateway.example.com/otlp",
    telemetry.WithOTelHeaders(map[string]string{
        "Authorization": "Basic " + authToken,
    }),
    telemetry.WithOTelService("mytool", "1.2.3"),
    telemetry.WithOTelLogger(logger),
)
```

The endpoint URL is parsed into host and path components. The SDK appends `/v1/logs` to the path automatically.

**Endpoint validation.** `otelcore.ParseEndpoint` validates the OTLP endpoint fail-fast (mirroring `chat.ValidateBaseURL`): an empty, over-long (> `MaxEndpointLength`, 2 KiB), control-character-bearing, unparseable, schemeless, or hostless URL is rejected, as is any URL carrying userinfo (`http://user:pass@host` — credentials belong in headers). Only `http` and `https` schemes are accepted; `http` is plaintext and marks the endpoint insecure. Rejections wrap `otelcore.ErrInvalidEndpoint` (matchable with `errors.Is`). A malformed endpoint therefore fails at `NewOTelBackend`/provider-construction time rather than silently failing later at export. A signal provider (`tracing`/`metrics`/`logs`) with an empty endpoint falls back to the `OTEL_EXPORTER_OTLP_*` environment variables and is not subject to this validation.

**OTel Options:**

| Option | Description |
|--------|-------------|
| `WithOTelHeaders(map)` | HTTP headers for every request (e.g. auth) |
| `WithOTelInsecure()` | Disable TLS (local collectors only) |
| `WithOTelLogger(l)` | Route OTel SDK errors to GTB logger |
| `WithOTelService(name, ver)` | Set `service.name` and `service.version` resource attributes |

!!! note "OTel SDK errors"
    The OTel SDK's `logger.Emit()` is fire-and-forget. Errors surface asynchronously through the SDK's error handler, not through `Backend.Send()`. Use `WithOTelLogger` to route these to your GTB logger at debug level.

### Custom Backend

Tool authors can supply any implementation of the `Backend` interface:


> [!NOTE]
> See [pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/telemetry](https://pkg.go.dev/gitlab.com/phpboyscout/go-tool-base/pkg/telemetry) for the full API definition.


Wire it in via `TelemetryConfig.Backend`:

```go
Telemetry: props.TelemetryConfig{
    Backend: func(p *props.Props) any {
        return myanalytics.NewBackend(p.Config.GetString("analytics.key"))
    },
},
```

The factory returns `any` to avoid an import cycle. The returned value must implement `telemetry.Backend` — a failed type assertion falls back to noop with a warning.

### Datadog

The `pkg/telemetry/datadog` package provides a backend that sends events to Datadog's HTTP Logs Intake API. Events are mapped to Datadog's native log format with `ddsource`, `ddtags`, `service`, and `hostname` fields — they appear immediately in Log Explorer without custom parsing.

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/datadog"

Telemetry: props.TelemetryConfig{
    Backend: func(p *props.Props) any {
        return datadog.NewBackend(
            os.Getenv("DD_API_KEY"),
            p.Logger,
            datadog.WithRegion(datadog.RegionEU1),
        )
    },
},
```

**Regions:** `RegionUS1` (default), `RegionUS3`, `RegionUS5`, `RegionEU1`, `RegionAP1`, `RegionAP2`, `RegionGOV`.

**Options:**

| Option | Description |
|--------|-------------|
| `WithRegion(region)` | Datadog region (resolves to the correct intake endpoint) |
| `WithSource(source)` | Override the `ddsource` tag (default: `"gtb"`) |

**Event mapping:**

| Event field | Datadog field |
|-------------|---------------|
| `Type: Name` | `message` |
| `ToolName` | `service` |
| `MachineID` | `hostname` |
| `Type, Version, OS, Arch` | `ddtags` (comma-separated) |
| `Metadata` | `metadata` (nested object) |

### PostHog

The `pkg/telemetry/posthog` package provides a backend that sends events to PostHog's Capture API using batch mode. Events map directly to PostHog's event model — they appear in the Events tab with all properties queryable.

```go
import "gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/posthog"

Telemetry: props.TelemetryConfig{
    Backend: func(p *props.Props) any {
        return posthog.NewBackend(
            os.Getenv("POSTHOG_PROJECT_KEY"),
            p.Logger,
            posthog.WithInstance(posthog.InstanceEU),
        )
    },
},
```

**Self-hosted PostHog:**

```go
posthog.NewBackend(
    os.Getenv("POSTHOG_PROJECT_KEY"),
    p.Logger,
    posthog.WithEndpoint("https://posthog.internal.example.com/capture/"),
)
```

**Options:**

| Option | Description |
|--------|-------------|
| `WithInstance(instance)` | PostHog cloud instance: `InstanceUS` (default), `InstanceEU` |
| `WithEndpoint(url)` | Custom endpoint for self-hosted (overrides `WithInstance`) |

**Event mapping:**

| Event field | PostHog field |
|-------------|---------------|
| `Type` | `event` |
| `MachineID` | `distinct_id` |
| `Name` | `properties.event_name` |
| `ToolName` | `properties.tool_name` |
| `Version` | `properties.tool_version` |
| `OS` | `properties.$os` |
| `Arch` | `properties.arch` |
| `Metadata` | `properties.*` (merged) |

### Choosing a Backend

| Backend | Best for | Auth | Protocol |
|---------|----------|------|----------|
| **OTLP** | Grafana Cloud, any OTel collector, enterprise observability | Basic auth via headers | OTLP/HTTP (protobuf) |
| **Datadog** | Teams already using Datadog for infrastructure monitoring | `DD-API-KEY` header | HTTP JSON |
| **PostHog** | Product analytics, feature adoption tracking, funnels | Project key in payload | HTTP JSON |
| **HTTP** | Simple custom endpoints, webhooks | None (bring your own) | HTTP JSON |
| **Custom** | Any other platform | Defined by implementation | Any |

The OTLP backend is the default recommendation for new deployments — it works with any OTel-compatible collector and avoids vendor lock-in. The Datadog and PostHog backends are provided for teams that want native integration with those platforms without writing a custom backend.

---

## Backend Selection Precedence

When the collector is constructed in `PersistentPreRunE`, backends are selected in this order:

1. **Custom backend** — `TelemetryConfig.Backend` factory (if set)
2. **Local-only** — file backend (if `telemetry.local_only` is true in config)
3. **OTLP** — `TelemetryConfig.OTelEndpoint` (if set)
4. **HTTP** — `TelemetryConfig.Endpoint` (if set)
5. **Noop** — no backend configured

---

## Delivery Modes

| Mode | Behaviour | Trade-off |
|------|-----------|-----------|
| `DeliveryAtLeastOnce` (default) | Spill files deleted **after** successful send; a failed in-memory `Flush` batch is re-spilled for retry | Possible duplicates if ack is lost; no data loss |
| `DeliveryAtMostOnce` | Spill files deleted **before** send | Possible data loss; no duplicates |

The at-least-once guarantee relies on backends **surfacing** delivery failures from `Send`. When a backend returns an error, `flushSpillFiles` retains the spill file, and a failed in-memory `Flush` batch is re-spilled to disk so the next flush retries it — without this, a backend that swallowed transport errors would silently drop batches while reporting success, defeating the guarantee.

```go
Telemetry: props.TelemetryConfig{
    DeliveryMode: props.DeliveryAtMostOnce,
},
```

## Buffer and Spill

Events are buffered in memory (capped at 1000) and flushed on process exit via Cobra's `OnFinalize` callback.

When the buffer is full, events are spilled to disk:

- **Location**: config directory (if available and writable), otherwise `/tmp`
- **File size cap**: 1 MB per spill file
- **File count cap**: 10 files — oldest deleted when exceeded
- **Recovery**: every `Flush` checks for spill files first, sends them before the current buffer

The shared `telemetry.ResolveDataDir(p)` helper determines the data directory for both spill files and local-only logs.
