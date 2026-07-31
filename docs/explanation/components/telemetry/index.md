---
title: Telemetry
description: Opt-in, consent-gated product analytics with pluggable backends and GDPR controls.
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

## Related Documentation

- [Telemetry Command](../../../reference/cli/telemetry.md) — CLI commands for managing telemetry
- [Props](../props.md) — dependency injection container (`Collector` field)
- [Create a Custom Telemetry Backend](../../../how-to/custom-telemetry-backend.md) — implement your own backend
- [Create a Custom Deletion Requestor](../../../how-to/custom-deletion-requestor.md) — GDPR deletion for custom backends
- [Telemetry Specification](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0012-opt-in-telemetry) — full design spec
- [Vendor Backends Specification](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0046-telemetry-vendor-backends) — Datadog and PostHog backends

## In this section

- **[What's Collected](collection.md)** — Event types, the data collected, and machine identification.
- **[Backends](backends.md)** — Pluggable backends, selection precedence, delivery modes, and buffering.
- **[Configuration](configuration.md)** — TelemetryConfig, environment variables, and initialiser integration.
- **[Privacy & Consent](privacy.md)** — Two-level gating, GDPR deletion, consent withdrawal, and known limitations.
- **[Testing](testing.md)** — Testing telemetry collection and backends.
