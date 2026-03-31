---
title: "Telemetry Vendor Backends: Datadog & PostHog"
description: "Dedicated telemetry backend implementations for Datadog and PostHog, extending the opt-in telemetry framework."
date: 2026-03-30
status: IMPLEMENTED
tags:
  - specification
  - telemetry
  - backends
  - datadog
  - posthog
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
depends_on:
  - 2026-03-21-opt-in-telemetry.md
---

# Telemetry Vendor Backends: Datadog & PostHog

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   30 March 2026

Status
:   DRAFT

Depends on
:   [Opt-in Telemetry Specification](2026-03-21-opt-in-telemetry.md)

---

## Overview

The [opt-in telemetry spec](2026-03-21-opt-in-telemetry.md) defines a pluggable `Backend` interface and ships noop, stdout, file, HTTP, and OTLP backends. The GTB binary itself uses the OTLP backend with Grafana Cloud.

This spec adds two vendor-specific backends for tool authors whose organisations use Datadog or PostHog. These backends map the framework's `telemetry.Event` struct to each vendor's native ingestion format, providing a zero-configuration experience — tool authors supply their API key and region, and the backend handles the rest.

Both backends:

- Implement the existing `telemetry.Backend` interface — no changes to the core telemetry framework
- Use only HTTP APIs — no vendor SDK dependencies
- Follow the same patterns as the existing HTTP backend (logger for debug output, silent network error handling, short timeouts)
- Are optional — tool authors opt in by importing the backend package and wiring it into `TelemetryConfig.Backend`

---

## Design Decisions

**No vendor SDK dependencies**: Both Datadog and PostHog offer Go SDKs, but importing them would add transitive dependencies to every tool built on GTB. Since both vendors provide simple HTTP JSON APIs, we use `pkg/http.NewClient` directly. This keeps the dependency tree clean and the backends lightweight.

**Separate package per vendor**: Each backend lives in its own sub-package (`pkg/telemetry/datadog`, `pkg/telemetry/posthog`) to avoid importing vendor-specific code unless explicitly used. Tool authors who don't use these vendors pay no cost.

**Event mapping is opinionated but overridable**: Each backend defines a default mapping from `telemetry.Event` to the vendor's format. The mapping is designed to make events immediately useful in each platform's UI (Datadog Log Explorer, PostHog Events) without custom parsing rules.

**Region-aware endpoints**: Both vendors operate multi-region. The backends accept a region parameter and resolve the correct endpoint automatically, falling back to a sensible default (US for both).

---

## Datadog Backend

### API Overview

Datadog's HTTP Logs Intake API accepts JSON payloads at regional endpoints:

| Region | Endpoint |
|--------|----------|
| US1 (default) | `https://http-intake.logs.datadoghq.com/v1/input` |
| US3 | `https://http-intake.logs.us3.datadoghq.com/v1/input` |
| US5 | `https://http-intake.logs.us5.datadoghq.com/v1/input` |
| EU1 | `https://http-intake.logs.datadoghq.eu/v1/input` |
| AP1 | `https://http-intake.logs.ap1.datadoghq.com/v1/input` |
| AP2 | `https://http-intake.logs.ap2.datadoghq.com/v1/input` |
| GOV | `https://http-intake.logs.ddog-gov.com/v1/input` |

Authentication: `DD-API-KEY` header with the Datadog API key.

Payload: JSON array of log objects. Maximum 5 MB uncompressed per request; individual logs truncated at 1 MB.

### Event Mapping

Each `telemetry.Event` maps to a Datadog log entry:

```json
{
  "message": "command.invocation: generate",
  "ddsource": "gtb",
  "ddtags": "event_type:command.invocation,tool_version:1.2.3,os:linux,arch:amd64",
  "hostname": "4a3f8c1d9e2b6f70",
  "service": "mytool",
  "timestamp": "2026-03-30T10:15:30.000Z",
  "level": "info",
  "metadata": {
    "command": "generate",
    "duration_ms": "245"
  }
}
```

| Event field | Datadog field | Notes |
|-------------|---------------|-------|
| `Type + ": " + Name` | `message` | Human-readable summary |
| `ToolName` | `service` | Maps to Datadog's service dimension |
| `"gtb"` | `ddsource` | Constant; identifies the source integration |
| `MachineID` | `hostname` | Hashed ID — Datadog uses hostname for grouping |
| `Timestamp` | `timestamp` | ISO 8601 format |
| `Type, Version, OS, Arch` | `ddtags` | Comma-separated key:value pairs |
| `Metadata` | `metadata` | Nested object for custom dimensions |

### Implementation

```go
// pkg/telemetry/datadog/datadog.go

package datadog

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"

    "github.com/cockroachdb/errors"
    gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
    "github.com/phpboyscout/go-tool-base/pkg/logger"
    "github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

// Region identifies a Datadog data center region.
type Region string

const (
    RegionUS1 Region = "us1" // default
    RegionUS3 Region = "us3"
    RegionUS5 Region = "us5"
    RegionEU1 Region = "eu1"
    RegionAP1 Region = "ap1"
    RegionAP2 Region = "ap2"
    RegionGOV Region = "gov"
)

var regionEndpoints = map[Region]string{
    RegionUS1: "https://http-intake.logs.datadoghq.com/v1/input",
    RegionUS3: "https://http-intake.logs.us3.datadoghq.com/v1/input",
    RegionUS5: "https://http-intake.logs.us5.datadoghq.com/v1/input",
    RegionEU1: "https://http-intake.logs.datadoghq.eu/v1/input",
    RegionAP1: "https://http-intake.logs.ap1.datadoghq.com/v1/input",
    RegionAP2: "https://http-intake.logs.ap2.datadoghq.com/v1/input",
    RegionGOV: "https://http-intake.logs.ddog-gov.com/v1/input",
}

// Option configures the Datadog backend.
type Option func(*config)

type config struct {
    region Region
    source string
}

// WithRegion sets the Datadog region. Default: RegionUS1.
func WithRegion(region Region) Option {
    return func(c *config) { c.region = region }
}

// WithSource overrides the ddsource tag. Default: "gtb".
func WithSource(source string) Option {
    return func(c *config) { c.source = source }
}

type backend struct {
    endpoint string
    apiKey   string
    source   string
    client   *http.Client
    log      logger.Logger
}

// NewBackend creates a Datadog telemetry backend.
// apiKey is the Datadog API key (not an application key).
func NewBackend(apiKey string, log logger.Logger, opts ...Option) telemetry.Backend {
    cfg := &config{
        region: RegionUS1,
        source: "gtb",
    }
    for _, o := range opts {
        o(cfg)
    }

    endpoint, ok := regionEndpoints[cfg.region]
    if !ok {
        endpoint = regionEndpoints[RegionUS1]
    }

    return &backend{
        endpoint: endpoint,
        apiKey:   apiKey,
        source:   cfg.source,
        client:   gtbhttp.NewClient(gtbhttp.WithTimeout(5 * time.Second)),
        log:      log,
    }
}

// datadogEntry is the JSON structure for a single Datadog log entry.
type datadogEntry struct {
    Message   string            `json:"message"`
    DDSource  string            `json:"ddsource"`
    DDTags    string            `json:"ddtags"`
    Hostname  string            `json:"hostname"`
    Service   string            `json:"service"`
    Timestamp string            `json:"timestamp"`
    Level     string            `json:"level"`
    Metadata  map[string]string `json:"metadata,omitempty"`
}

func (b *backend) Send(ctx context.Context, events []telemetry.Event) error {
    entries := make([]datadogEntry, 0, len(events))

    for _, e := range events {
        tags := []string{
            fmt.Sprintf("event_type:%s", e.Type),
            fmt.Sprintf("tool_version:%s", e.Version),
            fmt.Sprintf("os:%s", e.OS),
            fmt.Sprintf("arch:%s", e.Arch),
        }

        entries = append(entries, datadogEntry{
            Message:   fmt.Sprintf("%s: %s", e.Type, e.Name),
            DDSource:  b.source,
            DDTags:    strings.Join(tags, ","),
            Hostname:  e.MachineID,
            Service:   e.ToolName,
            Timestamp: e.Timestamp.UTC().Format(time.RFC3339Nano),
            Level:     "info",
            Metadata:  e.Metadata,
        })
    }

    body, err := json.Marshal(entries)
    if err != nil {
        return errors.Wrap(err, "marshalling datadog entries")
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
    if err != nil {
        return errors.Wrap(err, "creating datadog request")
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("DD-API-KEY", b.apiKey)

    resp, err := b.client.Do(req)
    if err != nil {
        return nil // silently drop — telemetry must never block the user
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        b.log.Debug("datadog endpoint returned non-success status",
            "status", resp.StatusCode)
    }

    return nil
}

func (b *backend) Close() error { return nil }
```

### Tool Author Usage

```go
import "github.com/phpboyscout/go-tool-base/pkg/telemetry/datadog"

props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        Backend: func(p *props.Props) any {
            return datadog.NewBackend(
                os.Getenv("DD_API_KEY"),
                p.Logger,
                datadog.WithRegion(datadog.RegionEU1),
            )
        },
    },
}
```

---

## PostHog Backend

### API Overview

PostHog's Capture API accepts event payloads at:

| Instance | Endpoint |
|----------|----------|
| US Cloud (default) | `https://us.i.posthog.com/capture/` |
| EU Cloud | `https://eu.i.posthog.com/capture/` |
| Self-hosted | `https://<your-instance>/capture/` |

Authentication: project API key included in the JSON payload body (`api_key` field).

Payload: JSON with `api_key`, `event` (name), `properties` (key-value pairs), and `distinct_id` (unique identifier). Supports batch mode with an array of events.

### Event Mapping

Each `telemetry.Event` maps to a PostHog event:

```json
{
  "api_key": "phc_...",
  "batch": [
    {
      "event": "command.invocation",
      "distinct_id": "4a3f8c1d9e2b6f70",
      "timestamp": "2026-03-30T10:15:30.000Z",
      "properties": {
        "event_name": "generate",
        "tool_name": "mytool",
        "tool_version": "1.2.3",
        "$os": "linux",
        "arch": "amd64",
        "command": "generate",
        "duration_ms": "245"
      }
    }
  ]
}
```

| Event field | PostHog field | Notes |
|-------------|---------------|-------|
| `Type` | `event` | PostHog event name — maps to the Events tab |
| `MachineID` | `distinct_id` | PostHog's unique actor identifier |
| `Timestamp` | `timestamp` | ISO 8601 format |
| `Name` | `properties.event_name` | The specific event name within the type |
| `ToolName` | `properties.tool_name` | Custom property |
| `Version` | `properties.tool_version` | Custom property |
| `OS` | `properties.$os` | PostHog's standard OS property (uses `$` prefix) |
| `Arch` | `properties.arch` | Custom property |
| `Metadata` | `properties.*` | Merged into properties as additional key-value pairs |

### Implementation

```go
// pkg/telemetry/posthog/posthog.go

package posthog

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "time"

    "github.com/cockroachdb/errors"
    gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
    "github.com/phpboyscout/go-tool-base/pkg/logger"
    "github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

// Instance identifies a PostHog deployment.
type Instance string

const (
    InstanceUS   Instance = "us"   // default
    InstanceEU   Instance = "eu"
)

var instanceEndpoints = map[Instance]string{
    InstanceUS: "https://us.i.posthog.com/capture/",
    InstanceEU: "https://eu.i.posthog.com/capture/",
}

// Option configures the PostHog backend.
type Option func(*config)

type config struct {
    instance Instance
    endpoint string // custom endpoint for self-hosted; overrides instance
}

// WithInstance sets the PostHog cloud instance. Default: InstanceUS.
func WithInstance(instance Instance) Option {
    return func(c *config) { c.instance = instance }
}

// WithEndpoint sets a custom endpoint for self-hosted PostHog.
// Takes precedence over WithInstance.
func WithEndpoint(endpoint string) Option {
    return func(c *config) { c.endpoint = endpoint }
}

type backend struct {
    endpoint   string
    projectKey string
    client     *http.Client
    log        logger.Logger
}

// NewBackend creates a PostHog telemetry backend.
// projectKey is the PostHog project API key (starts with "phc_").
func NewBackend(projectKey string, log logger.Logger, opts ...Option) telemetry.Backend {
    cfg := &config{
        instance: InstanceUS,
    }
    for _, o := range opts {
        o(cfg)
    }

    endpoint := cfg.endpoint
    if endpoint == "" {
        var ok bool
        endpoint, ok = instanceEndpoints[cfg.instance]
        if !ok {
            endpoint = instanceEndpoints[InstanceUS]
        }
    }

    return &backend{
        endpoint:   endpoint,
        projectKey: projectKey,
        client:     gtbhttp.NewClient(gtbhttp.WithTimeout(5 * time.Second)),
        log:        log,
    }
}

// posthogBatch is the top-level batch capture payload.
type posthogBatch struct {
    APIKey string         `json:"api_key"`
    Batch  []posthogEvent `json:"batch"`
}

// posthogEvent is a single PostHog event within a batch.
type posthogEvent struct {
    Event      string            `json:"event"`
    DistinctID string            `json:"distinct_id"`
    Timestamp  string            `json:"timestamp"`
    Properties map[string]string `json:"properties"`
}

func (b *backend) Send(ctx context.Context, events []telemetry.Event) error {
    batch := make([]posthogEvent, 0, len(events))

    for _, e := range events {
        props := map[string]string{
            "event_name":   e.Name,
            "tool_name":    e.ToolName,
            "tool_version": e.Version,
            "$os":          e.OS,
            "arch":         e.Arch,
        }

        // Merge custom metadata into properties
        for k, v := range e.Metadata {
            props[k] = v
        }

        batch = append(batch, posthogEvent{
            Event:      string(e.Type),
            DistinctID: e.MachineID,
            Timestamp:  e.Timestamp.UTC().Format(time.RFC3339Nano),
            Properties: props,
        })
    }

    payload := posthogBatch{
        APIKey: b.projectKey,
        Batch:  batch,
    }

    body, err := json.Marshal(payload)
    if err != nil {
        return errors.Wrap(err, "marshalling posthog batch")
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
    if err != nil {
        return errors.Wrap(err, "creating posthog request")
    }

    req.Header.Set("Content-Type", "application/json")

    resp, err := b.client.Do(req)
    if err != nil {
        return nil // silently drop — telemetry must never block the user
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        b.log.Debug("posthog endpoint returned non-success status",
            "status", resp.StatusCode)
    }

    return nil
}

func (b *backend) Close() error { return nil }
```

### Tool Author Usage

```go
import "github.com/phpboyscout/go-tool-base/pkg/telemetry/posthog"

props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        Backend: func(p *props.Props) any {
            return posthog.NewBackend(
                os.Getenv("POSTHOG_PROJECT_KEY"),
                p.Logger,
                posthog.WithInstance(posthog.InstanceEU),
            )
        },
    },
}
```

#### Self-hosted PostHog

```go
posthog.NewBackend(
    os.Getenv("POSTHOG_PROJECT_KEY"),
    p.Logger,
    posthog.WithEndpoint("https://posthog.internal.example.com/capture/"),
)
```

---

## Project Structure

```
pkg/telemetry/
├── datadog/
│   ├── datadog.go           ← Backend implementation, Region type, event mapping
│   └── datadog_test.go      ← Unit tests
├── posthog/
│   ├── posthog.go           ← Backend implementation, Instance type, event mapping
│   └── posthog_test.go      ← Unit tests
```

No changes to `pkg/telemetry/` core, `pkg/props/`, or any other existing package. These are additive, opt-in imports.

---

## Testing Strategy

### Datadog

| Test | Scenario |
|------|----------|
| `TestDatadog_Send` | Mock server → correct JSON structure, `DD-API-KEY` header present |
| `TestDatadog_EventMapping` | Verify `message`, `ddsource`, `ddtags`, `hostname`, `service`, `timestamp`, `metadata` fields |
| `TestDatadog_Regions` | Each `Region` constant resolves to the correct endpoint |
| `TestDatadog_InvalidRegion` | Unknown region falls back to US1 |
| `TestDatadog_Non2xx` | 400/403 → debug log emitted, no error returned |
| `TestDatadog_NetworkError` | Timeout/connection refused → nil returned (silent drop) |
| `TestDatadog_WithSource` | Custom `ddsource` value applied |
| `TestDatadog_MetadataMerge` | Event metadata appears in `metadata` field |
| `TestDatadog_Close` | Close returns nil |

### PostHog

| Test | Scenario |
|------|----------|
| `TestPostHog_Send` | Mock server → correct JSON batch structure, `api_key` in payload |
| `TestPostHog_EventMapping` | Verify `event`, `distinct_id`, `timestamp`, `properties` fields |
| `TestPostHog_Instances` | Each `Instance` constant resolves to the correct endpoint |
| `TestPostHog_InvalidInstance` | Unknown instance falls back to US |
| `TestPostHog_CustomEndpoint` | `WithEndpoint` overrides instance-based endpoint |
| `TestPostHog_Non2xx` | 400/403 → debug log emitted, no error returned |
| `TestPostHog_NetworkError` | Timeout/connection refused → nil returned (silent drop) |
| `TestPostHog_MetadataMerge` | Event metadata merged into `properties` |
| `TestPostHog_OsProperty` | OS field mapped to `$os` (PostHog convention) |
| `TestPostHog_Close` | Close returns nil |

### Integration Tests

- **Datadog round-trip**: `httptest.Server` mimicking Datadog intake → `NewBackend` → `Send` → verify full payload structure and headers.
- **PostHog round-trip**: `httptest.Server` mimicking PostHog capture → `NewBackend` → `Send` → verify batch payload structure.
- Gate with `testutil.SkipIfNotIntegration(t, "telemetry")`.

### Coverage

- Target: 95%+ for both `pkg/telemetry/datadog/` and `pkg/telemetry/posthog/`.

---

## Manifest Integration

The `generate project` command and interactive form gain a `--telemetry-backend` option that accepts `http`, `otel`, `datadog`, or `posthog`. When `datadog` or `posthog` is selected, the generator scaffolds the appropriate import and `TelemetryConfig.Backend` factory in the generated root command.

### Manifest extension

```yaml
properties:
  telemetry:
    backend: datadog          # or "posthog", "http", "otel"
    endpoint: ""              # used for http/otel backends
    otel_endpoint: ""         # used for otel backend
    datadog_region: eu1       # used when backend is "datadog"
    posthog_instance: eu      # used when backend is "posthog"
    posthog_endpoint: ""      # used for self-hosted posthog
```

### `ManifestTelemetry` update

```go
type ManifestTelemetry struct {
    Backend         string `yaml:"backend,omitempty"`          // "http", "otel", "datadog", "posthog"
    Endpoint        string `yaml:"endpoint,omitempty"`
    OTelEndpoint    string `yaml:"otel_endpoint,omitempty"`
    DatadogRegion   string `yaml:"datadog_region,omitempty"`
    PostHogInstance string `yaml:"posthog_instance,omitempty"`
    PostHogEndpoint string `yaml:"posthog_endpoint,omitempty"` // self-hosted override
}
```

The generator uses the `Backend` field to determine which import and factory to emit. API keys are always referenced via `os.Getenv(...)` in the generated code — never hardcoded.

---

## Documentation

- `docs/components/telemetry.md` gains a **Vendor Backends** section covering:
  - Datadog: setup, region configuration, event mapping, API key management
  - PostHog: setup, instance/self-hosted configuration, event mapping, project key management
  - How to choose between HTTP, OTLP, Datadog, and PostHog backends
- Each backend package includes full godoc with usage examples.

---

## Backwards Compatibility

- **No breaking changes**. These are new, additive packages. Existing code is entirely unaffected.
- Tool authors who don't import `pkg/telemetry/datadog` or `pkg/telemetry/posthog` incur no dependency cost.

---

## Open Questions

*No open questions.*

---

## Implementation Phases

### Phase 1 — Datadog backend
1. Create `pkg/telemetry/datadog/datadog.go` — `Backend`, `Region`, `Option`, event mapping
2. Create `pkg/telemetry/datadog/datadog_test.go` — full test suite
3. Integration test with `httptest.Server`

### Phase 2 — PostHog backend
1. Create `pkg/telemetry/posthog/posthog.go` — `Backend`, `Instance`, `Option`, event mapping, self-hosted support
2. Create `pkg/telemetry/posthog/posthog_test.go` — full test suite
3. Integration test with `httptest.Server`

### Phase 3 — Generator and manifest integration
1. Update `ManifestTelemetry` with `Backend`, `DatadogRegion`, `PostHogInstance`, `PostHogEndpoint` fields
2. Update generator templates to emit vendor backend imports and factories
3. Add `--telemetry-backend` flag and interactive form option to `generate project`
4. Tests for generated code with each backend type

### Phase 4 — Documentation
1. Add vendor backends section to `docs/components/telemetry.md`
2. Godoc for both packages

---

## Verification

```bash
go build ./...
go test -race ./pkg/telemetry/datadog/... ./pkg/telemetry/posthog/...
golangci-lint run

# Generate a tool with Datadog backend
go run . generate project --name myapp --repo org/myapp \
  --telemetry-backend datadog --datadog-region eu1

# Generate a tool with PostHog backend
go run . generate project --name myapp --repo org/myapp \
  --telemetry-backend posthog --posthog-instance eu

# Generate a tool with self-hosted PostHog
go run . generate project --name myapp --repo org/myapp \
  --telemetry-backend posthog --posthog-endpoint https://posthog.internal.example.com/capture/
```
