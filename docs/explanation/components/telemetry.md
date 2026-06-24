---
title: Telemetry
description: Opt-in pseudonymous usage analytics with pluggable backends, privacy controls, and GDPR-compliant data deletion.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Telemetry

## Overview

The telemetry package provides an opt-in framework for collecting pseudonymous usage analytics from CLI tools built on GTB. It is designed around three principles:

1. **Explicit consent** — telemetry is never enabled by default. Users must opt in via `telemetry enable`, the `init` prompt, or the `TELEMETRY_ENABLED` environment variable.
2. **Privacy by design** — no personally identifiable information is collected. Machine IDs are derived from multiple system signals and hashed with SHA-256. Command arguments, file contents, and IP addresses are never recorded.
3. **Pluggable backends** — tool authors choose where data goes. The framework ships noop, stdout, file, HTTP, and OpenTelemetry (OTLP) backends, and supports custom implementations.

> **Note:** For a conceptual overview of data collection, delivery modes, gating, and machine IDs, see the [Telemetry Architecture & Concepts](../../explanation/concepts/telemetry.md) guide.

---

## Quick Start

### Enable telemetry for your tool

```go
props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(
        props.Enable(props.TelemetryCmd),
    ),
    Telemetry: props.TelemetryConfig{
        Endpoint: "https://analytics.example.com/events",
    },
}
```

### Emit events from commands

```go
func runMyCommand(p *props.Props) error {
    start := time.Now()

    // ... command logic ...

    p.Collector.TrackCommand("my-command", time.Since(start).Milliseconds(), 0, nil)
    return nil
}
```

### User opt-in

```bash
mytool telemetry enable    # opt in
mytool telemetry status    # check current state
mytool telemetry disable   # opt out (drops all pending events)
mytool telemetry reset     # clear local data + request remote deletion
```

---

## Event Types

```go
props.EventCommandInvocation  // "command.invocation" — a command was run
props.EventCommandError       // "command.error" — a command failed
props.EventFeatureUsed        // "feature.used" — a feature was exercised
props.EventUpdateCheck        // "update.check" — update check performed
props.EventUpdateApplied      // "update.applied" — update was applied
props.EventDeletionRequest    // "data.deletion_request" — GDPR deletion request
```

These constants are defined in both `pkg/props` and `pkg/telemetry`. Since they resolve to plain strings, values from either package are interchangeable.

---

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

```go
type Backend interface {
    Send(ctx context.Context, events []Event) error
    Close() error
}
```

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

## TelemetryConfig

```go
type TelemetryConfig struct {
    Endpoint           string               // HTTP JSON endpoint
    OTelEndpoint       string               // OTLP/HTTP endpoint (takes precedence)
    OTelHeaders        map[string]string    // OTLP auth headers
    OTelInsecure       bool                 // Disable TLS for OTLP
    Backend            func(*Props) any     // Custom backend factory
    DeletionRequestor  func(*Props) any     // Custom GDPR deletion requestor
    ExtendedCollection bool                 // Include args + errors (enterprise only)
    DeliveryMode       DeliveryMode         // at_least_once (default) or at_most_once
    Metadata           map[string]string    // Extra key/value pairs in every event
}
```

Endpoints are set by the tool author at build time and are **not user-configurable**. The user config file only stores consent (`telemetry.enabled`) and mode (`telemetry.local_only`).

---

## Environment Variables

| Variable | Values | Effect |
|----------|--------|--------|
| `TELEMETRY_ENABLED` | `true` / `false` | Bypasses interactive consent; overrides config at runtime |
| `TELEMETRY_LOCAL` | `true` / `false` | Forces local-only mode (file backend) |
| `CI` | `true` | Sets `--skip-telemetry` default to `true` during `init` |

These names are deliberately un-prefixed so tools building on GTB can use them without GTB-specific naming conventions.

---

## Testing

### Unit Tests

Use the noop collector — `Props.Collector` is always non-nil:

```go
p := &props.Props{
    // Collector is nil — telemetry calls are safe but do nothing
}
```

Or create a disabled collector for explicit testing:

```go
c := telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(),
    "test", "1.0.0", nil, logger.NewNoop(), "", props.DeliveryAtLeastOnce)
```

### Verifying Events

Use a spy backend to capture events in tests:

```go
type spyBackend struct {
    events []telemetry.Event
    mu     sync.Mutex
}

func (s *spyBackend) Send(_ context.Context, events []telemetry.Event) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.events = append(s.events, events...)
    return nil
}

func (s *spyBackend) Close() error { return nil }
```

---

## Related Documentation

- [Telemetry Command](../../reference/cli/telemetry.md) — CLI commands for managing telemetry
- [Props](props.md) — dependency injection container (`Collector` field)
- [Create a Custom Telemetry Backend](../../how-to/custom-telemetry-backend.md) — implement your own backend
- [Create a Custom Deletion Requestor](../../how-to/custom-deletion-requestor.md) — GDPR deletion for custom backends
- [Telemetry Specification](../../development/specs/2026-03-21-opt-in-telemetry.md) — full design spec
- [Vendor Backends Specification](../../development/specs/2026-03-30-telemetry-vendor-backends.md) — Datadog and PostHog backends
