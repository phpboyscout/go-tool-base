---
title: Telemetry
description: Opt-in anonymous usage analytics with pluggable backends, privacy controls, and GDPR-compliant data deletion.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Telemetry

## Overview

The telemetry package provides an opt-in framework for collecting anonymous usage analytics from CLI tools built on GTB. It is designed around three principles:

1. **Explicit consent** — telemetry is never enabled by default. Users must opt in via `telemetry enable`, the `init` prompt, or the `TELEMETRY_ENABLED` environment variable.
2. **Privacy by design** — no personally identifiable information is collected. Machine IDs are derived from multiple system signals and hashed with SHA-256. Command arguments, file contents, and IP addresses are never recorded.
3. **Pluggable backends** — tool authors choose where data goes. The framework ships noop, stdout, file, HTTP, and OpenTelemetry (OTLP) backends, and supports custom implementations.

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

## Two-Level Gating

Telemetry requires two conditions to be active:

| Level | Who controls it | How |
|-------|----------------|-----|
| Feature flag | Tool author | `props.Enable(props.TelemetryCmd)` in code |
| User consent | End user | `telemetry enable` command or `TELEMETRY_ENABLED=true` env var |

Both must be active for data to be collected. If either is missing, the collector is a silent noop.

---

## What Is Collected

Every telemetry event contains:

| Field | Example | Description |
|-------|---------|-------------|
| `event.type` | `command.invocation` | Event category |
| `event.name` | `generate` | Specific command or feature |
| `tool.name` | `mytool` | Tool identity |
| `tool.version` | `1.2.3` | Tool version |
| `os.type` | `linux` | Operating system |
| `os.version` | `6.8.0-106-generic` | OS/kernel version |
| `host.arch` | `amd64` | CPU architecture |
| `go.version` | `go1.26.1` | Go runtime version |
| `machine.id` | `4a3f8c1d9e2b6f70` | Anonymised machine identifier (16 hex chars) |
| `command.duration_ms` | `142` | Execution time (command events only) |
| `command.exit_code` | `0` | Exit status (command events only) |

Tool authors can add custom metadata via `TelemetryConfig.Metadata` (included in every event) or the `extra` parameter on `Track`/`TrackCommand` (per-event).

### What Is NOT Collected

- Command arguments or flags
- File paths or file contents
- Environment variables
- IP addresses
- Usernames, hostnames, or any raw PII
- Authentication tokens or credentials

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

## Machine ID

The machine ID is a privacy-preserving identifier derived from four system signals:

1. **OS machine ID** — `/etc/machine-id` (Linux), `IOPlatformUUID` (macOS), `MachineGuid` (Windows)
2. **MAC address** — first non-loopback network interface
3. **Hostname**
4. **Username**

All four are concatenated and hashed with SHA-256. The first 8 bytes (16 hex chars) are used. Each signal degrades gracefully if unavailable. The hash cannot be reversed to recover any input value.

The machine ID is computed fresh on every invocation — it is not persisted to config.

```bash
$ mytool telemetry status
Telemetry: enabled
Machine ID: 4a3f8c1d9e2b6f70
```

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

POSTs events as a JSON array to an endpoint. Network errors are silently dropped. Non-2xx responses are logged at debug level.

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
    Endpoint          string               // HTTP JSON endpoint
    OTelEndpoint      string               // OTLP/HTTP endpoint (takes precedence)
    OTelHeaders       map[string]string    // OTLP auth headers
    OTelInsecure      bool                 // Disable TLS for OTLP
    Backend           func(*Props) any     // Custom backend factory
    DeletionRequestor func(*Props) any     // Custom GDPR deletion requestor
    DeliveryMode      DeliveryMode         // at_least_once (default) or at_most_once
    Metadata          map[string]string    // Extra key/value pairs in every event
}
```

Endpoints are set by the tool author at build time and are **not user-configurable**. The user config file only stores consent (`telemetry.enabled`) and mode (`telemetry.local_only`).

---

## Buffer and Spill

Events are buffered in memory (capped at 1000) and flushed on process exit via Cobra's `OnFinalize` callback.

When the buffer is full, events are spilled to disk:

- **Location**: config directory (if available and writable), otherwise `/tmp`
- **File size cap**: 1 MB per spill file
- **File count cap**: 10 files — oldest deleted when exceeded
- **Recovery**: every `Flush` checks for spill files first, sends them before the current buffer

The shared `telemetry.ResolveDataDir(p)` helper determines the data directory for both spill files and local-only logs.

---

## Delivery Modes

| Mode | Behaviour | Trade-off |
|------|-----------|-----------|
| `DeliveryAtLeastOnce` (default) | Spill files deleted **after** successful send | Possible duplicates if ack is lost; no data loss |
| `DeliveryAtMostOnce` | Spill files deleted **before** send | Possible data loss; no duplicates |

```go
Telemetry: props.TelemetryConfig{
    DeliveryMode: props.DeliveryAtMostOnce,
},
```

---

## Environment Variables

| Variable | Values | Effect |
|----------|--------|--------|
| `TELEMETRY_ENABLED` | `true` / `false` | Bypasses interactive consent; overrides config at runtime |
| `TELEMETRY_LOCAL` | `true` / `false` | Forces local-only mode (file backend) |
| `CI` | `true` | Sets `--skip-telemetry` default to `true` during `init` |

These names are deliberately un-prefixed so tools building on GTB can use them without GTB-specific naming conventions.

---

## GDPR Data Deletion

The `telemetry reset` command:

1. Drops all buffered events and deletes spill files
2. Sends a deletion request via the configured `DeletionRequestor`
3. Clears the local-only telemetry log (if present)
4. Disables telemetry

### Built-in Deletion Requestors

| Requestor | How it works |
|-----------|-------------|
| `NewHTTPDeletionRequestor(url, logger)` | POSTs `{"machine_id": "..."}` to the endpoint |
| `NewEmailDeletionRequestor(address, toolName)` | Opens a pre-filled `mailto:` link |
| `NewEventDeletionRequestor(backend)` | Sends a `data.deletion_request` event through the existing backend |

If no requestor is configured, the event-based requestor is used as the universal fallback.

### Custom Requestor

```go
Telemetry: props.TelemetryConfig{
    DeletionRequestor: func(p *props.Props) any {
        return telemetry.NewHTTPDeletionRequestor(
            "https://analytics.example.com/deletion",
            p.Logger,
        )
    },
},
```

---

## Consent Withdrawal

When the user runs `telemetry disable`:

1. Config is updated to `telemetry.enabled: false`
2. All buffered events are **immediately dropped**
3. All spill files are **deleted**
4. The `OnFinalize` flush re-checks the enabled state and no-ops

No events are sent after an explicit disable, even if they were collected while consent was active.

---

## Init Integration

When `TelemetryCmd` is enabled and the tool has `InitCmd` enabled, the `TelemetryInitialiser` registers with the setup system. During `init`, the user is prompted to opt in:

```
? Anonymous usage telemetry
  Help improve mytool by sending anonymous usage statistics.
  No personally identifiable information is collected.
  You can change this at any time with `mytool telemetry enable/disable`.
  > Yes / No
```

The `--skip-telemetry` flag (default `true` when `CI=true`) suppresses the prompt in non-interactive environments. The `TELEMETRY_ENABLED` env var pre-answers the consent question.

### Tools Without Init

For tools that disable `InitCmd` (like the GTB binary itself), the `telemetry enable` command auto-creates the config file in the default config directory (`~/.toolname/config.yaml`) if one doesn't exist.

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

- [Telemetry Command](commands/telemetry.md) — CLI commands for managing telemetry
- [Props](props.md) — dependency injection container (`Collector` field)
- [Telemetry Specification](../development/specs/2026-03-21-opt-in-telemetry.md) — full design spec
- [Vendor Backends Specification](../development/specs/2026-03-30-telemetry-vendor-backends.md) — Datadog and PostHog backends
