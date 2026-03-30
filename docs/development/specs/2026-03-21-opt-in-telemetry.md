---
title: "Opt-in Telemetry Specification"
description: "Opt-in usage analytics framework with pluggable backends, privacy controls, and CLI management commands."
date: 2026-03-21
status: IN PROGRESS
tags:
  - specification
  - telemetry
  - analytics
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-sonnet-4-6)
    role: AI drafting assistant
---

# Opt-in Telemetry Specification

Authors
:   Matt Cockayne, Claude (claude-sonnet-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Overview

Understanding how GTB-based tools are used helps maintainers prioritise features, identify common errors, and measure adoption. However, telemetry must be explicitly opt-in, privacy-preserving, and transparent.

This spec defines an opt-in telemetry framework with:

- Explicit opt-in via a first-run `init` prompt or `telemetry enable` command (never enabled by default)
- Non-interactive bypass via environment variable for CI/CD environments
- Pluggable backend interface — tool authors can supply their own; the framework ships noop, stdout, file, HTTP, and OpenTelemetry (OTLP) backends
- Telemetry definition embedded in `props.Tool` so tool authors declare their config alongside other tool metadata
- Defined event types for command invocations, errors, and feature usage
- Privacy controls — anonymised machine IDs, no PII, no command arguments
- CLI management commands (`telemetry enable`, `telemetry disable`, `telemetry status`, `telemetry reset`)
- A `TelemetryInitialiser` registered via `setup.Register` that prompts the user on first `init`
- Manifest integration so `generate project` scaffolds telemetry configuration
- Pluggable `DeletionRequestor` interface for GDPR-compliant data deletion requests
- Configurable delivery guarantees (at-least-once or at-most-once)
- Bounded in-memory buffer with disk spill for resilient event delivery
- Enabled by default for the GTB binary itself to track framework adoption

---

## Design Decisions

**Opt-in, not opt-out**: Telemetry is disabled by default and requires explicit user action or tool-author pre-configuration to enable. This is a firm ethical and legal requirement (GDPR, user trust). The feature flag `TelemetryCmd` must be enabled by the tool author, AND the user must opt in — either interactively during `init` or via environment variable.

**Two-level gating**: Tool authors control telemetry availability at two points:
1. The `TelemetryCmd` feature flag — enables the `telemetry` command group and wires up collection. Default **disabled**.
2. `props.Tool.Telemetry.Enabled` — the user's runtime opt-in stored in config. Default **false**.

Both must be active for data to be collected.

**Telemetry config lives on `props.Tool`**: The `TelemetryConfig` struct is embedded in `props.Tool`, not scattered across multiple config keys. Tool authors declare their endpoint, backend factory, delivery mode, deletion requestor, and any custom metadata alongside the tool's name and release source. This makes it clear that telemetry is a tool-author concern, not a framework concern.

**Endpoints are tool-author only**: The telemetry endpoint (HTTP or OTLP) is set by the tool author in `props.TelemetryConfig` at build time. Users cannot override it via config — the user config file only stores consent (`telemetry.enabled`) and mode (`telemetry.local_only`). This prevents abuse and misconfiguration.

**Pluggable backends**: The `Backend` interface allows tool authors to supply their own analytics platform. A custom backend is passed in via `TelemetryConfig.Backend` (a factory function). The framework provides noop, stdout, file, HTTP, and OpenTelemetry (OTLP) backends. All backend constructors follow the `NewXxxBackend(...)` pattern. The OTLP backend is the recommended choice for enterprises with existing observability infrastructure.

**`init` integration via `TelemetryInitialiser`**: The telemetry package registers a `TelemetryInitialiser` against `props.TelemetryCmd` via `setup.Register`. When `TelemetryCmd` is enabled and the user runs `init`, they are prompted to opt in. The `--skip-telemetry` flag (defaulting to `true` in CI, via `os.Getenv("CI") == "true"`) suppresses the prompt in non-interactive environments. The skip flag is stored on the `TelemetryInitialiser` struct (not a package-level variable) to ensure test isolation.

**Non-interactive bypass via environment variable**: Setting `TELEMETRY_ENABLED=true` before running `init` (or any command) pre-answers the consent question and writes `telemetry.enabled: true` to the config. This is the canonical path for CI/CD pipelines and provisioning scripts. `TELEMETRY_ENABLED=false` (or any falsy value) explicitly disables. The env var is deliberately not tool-prefixed — it is a generic, tool-name-agnostic name that tool authors building on GTB can reuse seamlessly without framework-specific naming conventions bleeding into their users' environment.

**Collector on `Props`**: `props.Props` gains a `Collector TelemetryCollector` field populated by the root command's `PersistentPreRunE`. Commands that want to emit events call `p.Collector.Track(...)`. The collector is always non-nil — when telemetry is disabled it is a noop collector.

**`OnFinalize` for flush**: Telemetry flush is registered via Cobra's `OnFinalize` callback rather than `PersistentPostRunE`. This ensures flush always runs regardless of whether subcommands define their own `PostRunE` hooks — Cobra does not chain `PersistentPostRunE` when a subcommand has `PostRunE`. `OnFinalize` re-checks the enabled state before flushing, so if the user runs `telemetry disable` mid-session, no events are sent.

**Config persistence via Viper**: CLI commands that change the telemetry state (`enable`, `disable`, `reset`) call `props.Config.Set(key, value)` followed by `v.WriteConfig()` / `v.SafeWriteConfig()` — matching the pattern established by `pkg/cmd/config/set.go`.

**Anonymisation by default**: No personally identifiable information is collected. Machine IDs are derived from multiple signals — OS machine ID, first non-loopback MAC address, hostname, and username — hashed with SHA-256 (first 8 bytes, 16 hex chars). Each signal degrades gracefully if unavailable. IP addresses are not stored. Command arguments are never recorded. The machine ID is computed fresh on every invocation (not persisted to config) so it tracks the machine, not a stored identity.

**Bounded buffer with disk spill**: The in-memory event buffer is capped at 1000 events. When the cap is reached, events are spilled to disk in the telemetry data directory (config dir if available and writable, `/tmp` fallback). Spill files are capped at 1MB each with a maximum of 10 files — oldest deleted when the limit is reached. Every `Flush` checks for spill files first, attempting to send those before the current buffer. Spill files are cleaned up after successful delivery.

**Configurable delivery guarantee**: Tool authors choose between `DeliveryAtLeastOnce` (default) and `DeliveryAtMostOnce` via `TelemetryConfig.DeliveryMode`. At-least-once deletes spill files after successful send (possible duplicates if ack is lost). At-most-once deletes spill files before sending (possible loss, no duplicates). At-least-once is the default because data has value and deduplication is easier to solve than data loss.

**Immediate opt-out**: When the user runs `telemetry disable`, all buffered events are dropped immediately and any spill files are deleted. The user's right to withdraw consent is paramount — no events are flushed after an explicit disable, even if they were collected while consent was active. The `OnFinalize` flush re-checks `telemetry.enabled` and no-ops if disabled.

**GDPR data deletion via `telemetry reset`**: The `telemetry reset` command clears all local telemetry data (buffer, spill files, local-only log), sends a deletion request to the remote backend via a pluggable `DeletionRequestor`, disables telemetry, and reports what happened to the user. The `DeletionRequestor` interface mirrors the `Backend` pattern — tool authors can supply a custom implementation or use built-in requestors (HTTP, email, event-based). If no requestor is configured, the framework falls back to sending a `data.deletion_request` event through the existing backend, which works with any backend type.

**Local-only mode**: Users can enable telemetry in local-only mode (`TELEMETRY_LOCAL=true` or `telemetry.local_only: true`) where events are written to a file but never transmitted. Useful for tool authors debugging their own usage patterns.

**GTB binary uses telemetry for framework adoption tracking**: GTB itself enables `TelemetryCmd` in `internal/cmd/root/root.go` to collect anonymous usage data about GTB command usage. This serves as the reference implementation.

**Manifest integration for `generate project`**: The `.gtb/manifest.yaml` `properties` section gains a `telemetry` block that scaffolded tools can populate, supporting both `endpoint` and `otel_endpoint`. When present, the generator emits a `props.TelemetryConfig` initialisation in the generated root command. Telemetry configuration is also exposed as CLI flags (`--telemetry-endpoint`, `--telemetry-otel-endpoint`) and interactive form prompts during project generation. The `telemetry` feature in the manifest's `features` list remains in the `optInFeatures` set — it must be explicitly enabled.

**EventType constants in both packages**: `EventType` and its constants are defined in both `pkg/props` (for clean consumer imports) and `pkg/telemetry` (for internal use). Since `EventType` resolves to a plain `string`, values from either package are interchangeable at the interface boundary. Keeping both sets in sync is a maintenance responsibility verified by tests.

---

## Public API Changes

### `pkg/props` — new telemetry interface and config

To avoid an import cycle between `pkg/props` and `pkg/telemetry`, the boundary is drawn as follows:

- `pkg/props` defines the **`TelemetryCollector` interface**, **`EventType` constants**, and **`TelemetryConfig`** — the surface commands use to emit events and that tool authors use to configure telemetry.
- `pkg/telemetry` defines the **`Backend` interface, `DeletionRequestor` interface, and all concrete implementations** (noop, stdout, file, HTTP, OTLP backends; HTTP, email, event deletion requestors).
- `pkg/props.Props.Collector` is typed as `props.TelemetryCollector` (interface), not `*telemetry.Collector`.
- `pkg/telemetry` imports `pkg/props` (for `*Props` in factory signatures); `pkg/props` does **not** import `pkg/telemetry`. No cycle.
- `TelemetryConfig.Backend` is typed `func(*Props) any` in `pkg/props` — the `any` avoids referencing `telemetry.Backend`. `pkg/cmd/root` (which imports both packages) performs the type assertion to `telemetry.Backend` when constructing the collector.
- `TelemetryConfig.DeletionRequestor` follows the same `func(*Props) any` pattern.

```go
// In pkg/props/telemetry.go (new file)

// EventType identifies the category of telemetry event.
// Defined here alongside TelemetryCollector so that commands can reference
// event type constants without importing pkg/telemetry.
// The same constants are mirrored in pkg/telemetry — since EventType is a
// string typedef, values from either package are interchangeable.
type EventType string

const (
    EventCommandInvocation EventType = "command.invocation"
    EventCommandError      EventType = "command.error"
    EventFeatureUsed       EventType = "feature.used"
    EventUpdateCheck       EventType = "update.check"
    EventUpdateApplied     EventType = "update.applied"
    EventDeletionRequest   EventType = "data.deletion_request"
)

// TelemetryCollector is the interface through which commands emit telemetry events.
// Defined here (not in pkg/telemetry) to avoid an import cycle.
// The concrete implementation is *telemetry.Collector from pkg/telemetry.
// Props.Collector is always non-nil — when telemetry is disabled it is a noop.
type TelemetryCollector interface {
    // Track records a telemetry event. No-op when the collector is disabled.
    Track(eventType EventType, name string, extra map[string]string)
    // Flush sends all buffered events to the backend and clears the buffer.
    // Checks for and sends spill files before flushing the current buffer.
    Flush(ctx context.Context) error
    // Drop clears all buffered events and deletes any spill files without sending.
    Drop() error
}

// DeliveryMode controls the delivery guarantee for telemetry events.
type DeliveryMode string

const (
    // DeliveryAtLeastOnce deletes spill files after successful send.
    // Possible duplicates if the ack is lost; no data loss.
    DeliveryAtLeastOnce DeliveryMode = "at_least_once"
    // DeliveryAtMostOnce deletes spill files before sending.
    // Possible data loss; no duplicates.
    DeliveryAtMostOnce DeliveryMode = "at_most_once"
)

// TelemetryConfig holds tool-author telemetry declarations.
// It is embedded in Tool and specifies where and how to send telemetry.
// The end-user's opt-in state is stored in the config file under
// telemetry.enabled and telemetry.local_only — endpoints are not
// user-configurable.
type TelemetryConfig struct {
    // Endpoint is the HTTP JSON endpoint to POST events to.
    // Ignored when OTelEndpoint is set or when Backend is non-nil.
    Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`

    // OTelEndpoint is the OTLP/HTTP base URL (e.g. "https://collector:4318").
    // Takes precedence over Endpoint when set.
    OTelEndpoint string `json:"otel_endpoint,omitempty" yaml:"otel_endpoint,omitempty"`

    // OTelHeaders are HTTP headers sent with every OTLP request (e.g. auth tokens).
    OTelHeaders map[string]string `json:"otel_headers,omitempty" yaml:"otel_headers,omitempty"`

    // OTelInsecure disables TLS for the OTLP endpoint — use only for local collectors.
    OTelInsecure bool `json:"otel_insecure,omitempty" yaml:"otel_insecure,omitempty"`

    // Backend is an optional factory for a custom telemetry backend.
    // Typed as func(*Props) any to avoid importing pkg/telemetry from pkg/props.
    // The returned value must implement telemetry.Backend — pkg/cmd/root performs
    // the type assertion. Takes precedence over Endpoint and OTelEndpoint when set.
    // Not serialisable — set programmatically in tool setup.
    Backend func(*Props) any `json:"-" yaml:"-"`

    // DeletionRequestor is an optional factory for a custom GDPR deletion requestor.
    // Typed as func(*Props) any to avoid importing pkg/telemetry from pkg/props.
    // The returned value must implement telemetry.DeletionRequestor — pkg/cmd/root
    // performs the type assertion. If nil, falls back to sending a
    // data.deletion_request event through the existing backend.
    // Not serialisable — set programmatically in tool setup.
    DeletionRequestor func(*Props) any `json:"-" yaml:"-"`

    // DeliveryMode controls the delivery guarantee. Default: DeliveryAtLeastOnce.
    DeliveryMode DeliveryMode `json:"delivery_mode,omitempty" yaml:"delivery_mode,omitempty"`

    // Metadata is additional key/value pairs included in every event.
    // Useful for custom dimensions like environment name or deployment tier.
    Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Tool gains a Telemetry field:
type Tool struct {
    Name          string                   `json:"name"           yaml:"name"`
    Summary       string                   `json:"summary"        yaml:"summary"`
    Description   string                   `json:"description"    yaml:"description"`
    Features      []Feature                `json:"features"       yaml:"features"`
    Help          errorhandling.HelpConfig `json:"-"              yaml:"-"`
    ReleaseSource ReleaseSource            `json:"release_source" yaml:"release_source"`
    Telemetry     TelemetryConfig          `json:"telemetry"      yaml:"telemetry"`
}
```

### New Feature Flag

```go
// In pkg/props/tool.go — default disabled
TelemetryCmd = FeatureCmd("telemetry")

// isDefaultEnabled: TelemetryCmd returns false (not in defaults list)
```

### New Package: `pkg/telemetry`

`pkg/telemetry` imports `pkg/props` (for `*props.Props` in factory signatures). `pkg/props` does **not** import `pkg/telemetry`. The `*telemetry.Collector` concrete type implements `props.TelemetryCollector` — satisfying the interface is verified at compile time in `pkg/cmd/root` via:

```go
var _ props.TelemetryCollector = (*telemetry.Collector)(nil)
```

```go
// EventType identifies the category of telemetry event.
// Mirrored from pkg/props — since both resolve to string, values are
// interchangeable at the interface boundary.
type EventType string

const (
    EventCommandInvocation EventType = "command.invocation"
    EventCommandError      EventType = "command.error"
    EventFeatureUsed       EventType = "feature.used"
    EventUpdateCheck       EventType = "update.check"
    EventUpdateApplied     EventType = "update.applied"
    EventDeletionRequest   EventType = "data.deletion_request"
)

// Event represents a single telemetry event.
type Event struct {
    Timestamp time.Time         `json:"timestamp"`
    Type      EventType         `json:"type"`
    Name      string            `json:"name"`
    MachineID string            `json:"machine_id"` // hashed, never raw
    ToolName  string            `json:"tool_name"`
    Version   string            `json:"version"`
    OS        string            `json:"os"`
    Arch      string            `json:"arch"`
    Metadata  map[string]string `json:"metadata,omitempty"`
}

// Backend is the interface for telemetry data sinks.
// Implementations must be safe for concurrent use.
// Send must be non-blocking or short-timeout to avoid impacting CLI performance.
type Backend interface {
    Send(ctx context.Context, events []Event) error
    Close() error
}

// DeletionRequestor sends a GDPR data deletion request for a given machine ID.
// Implementations should be best-effort — deletion cannot be guaranteed for all
// backend types.
type DeletionRequestor interface {
    RequestDeletion(ctx context.Context, machineID string) error
}

// Config holds runtime telemetry configuration read from the user's config file.
// Endpoints are not included — they are tool-author concerns set in
// props.TelemetryConfig, not user-configurable.
type Config struct {
    Enabled   bool `yaml:"enabled"`
    LocalOnly bool `yaml:"local_only"`
}

// Collector accumulates events and flushes to the backend.
// All methods are safe for concurrent use. When disabled, all operations
// are no-ops — callers do not need to check whether telemetry is enabled.
type Collector struct {
    backend      Backend
    config       Config
    toolName     string
    version      string
    machineID    string
    metadata     map[string]string
    buffer       []Event
    mu           sync.Mutex
    log          logger.Logger
    dataDir      string
    deliveryMode props.DeliveryMode
    maxBuffer    int // default 1000
}
```

---

## Internal Implementation

### Collector

```go
const defaultMaxBuffer = 1000

// NewCollector creates a Collector. When cfg.Enabled is false, returns a noop
// collector so callers never need to nil-check.
func NewCollector(cfg Config, backend Backend, toolName, version string, metadata map[string]string, log logger.Logger, dataDir string, deliveryMode props.DeliveryMode) *Collector {
    if !cfg.Enabled {
        return &Collector{backend: NewNoopBackend(), log: log, maxBuffer: defaultMaxBuffer}
    }

    if deliveryMode == "" {
        deliveryMode = props.DeliveryAtLeastOnce
    }

    return &Collector{
        backend:      backend,
        config:       cfg,
        toolName:     toolName,
        version:      version,
        machineID:    HashedMachineID(),
        metadata:     metadata,
        log:          log,
        dataDir:      dataDir,
        deliveryMode: deliveryMode,
        maxBuffer:    defaultMaxBuffer,
    }
}

// Track records a telemetry event. No-op when collector is disabled.
// When the in-memory buffer reaches maxBuffer, events are spilled to disk.
func (c *Collector) Track(eventType props.EventType, name string, extra map[string]string) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.backend == nil {
        return
    }

    merged := make(map[string]string, len(c.metadata)+len(extra))
    for k, v := range c.metadata {
        merged[k] = v
    }

    for k, v := range extra {
        merged[k] = v
    }

    c.buffer = append(c.buffer, Event{
        Timestamp: time.Now().UTC(),
        Type:      EventType(eventType),
        Name:      name,
        MachineID: c.machineID,
        ToolName:  c.toolName,
        Version:   c.version,
        OS:        runtime.GOOS,
        Arch:      runtime.GOARCH,
        Metadata:  merged,
    })

    if len(c.buffer) >= c.maxBuffer {
        c.spillToDisk()
    }
}

// Flush sends all buffered events to the backend, then clears the buffer.
// Checks for and sends spill files before flushing the current buffer.
func (c *Collector) Flush(ctx context.Context) error {
    // Send spill files first
    if err := c.flushSpillFiles(ctx); err != nil {
        c.log.Debug("failed to flush spill files", "error", err)
    }

    c.mu.Lock()
    events := make([]Event, len(c.buffer))
    copy(events, c.buffer)
    c.buffer = c.buffer[:0]
    c.mu.Unlock()

    if len(events) == 0 {
        return nil
    }

    if err := c.backend.Send(ctx, events); err != nil {
        c.log.Warn("telemetry flush failed", "error", err, "events_dropped", len(events))
        return err
    }

    c.log.Debug("telemetry flushed", "events", len(events))

    return nil
}

// Drop clears all buffered events and deletes any spill files without sending.
// Called when the user disables telemetry to ensure immediate consent withdrawal.
func (c *Collector) Drop() error {
    c.mu.Lock()
    c.buffer = c.buffer[:0]
    c.mu.Unlock()

    return c.deleteSpillFiles()
}
```

### Buffer Spill

```go
const (
    maxSpillFileSize = 1 << 20 // 1 MB
    maxSpillFiles    = 10
)

// spillToDisk writes the current buffer to a spill file and clears the buffer.
// Must be called with c.mu held.
func (c *Collector) spillToDisk() {
    if c.dataDir == "" {
        return
    }

    data, err := json.Marshal(c.buffer)
    if err != nil {
        c.log.Debug("failed to marshal spill data", "error", err)
        return
    }

    // Enforce per-file size cap
    if len(data) > maxSpillFileSize {
        data = data[:maxSpillFileSize]
    }

    // Enforce max spill file count — delete oldest if at limit
    c.pruneSpillFiles()

    filename := filepath.Join(c.dataDir, fmt.Sprintf("telemetry-spill-%d.json", time.Now().UnixNano()))
    if err := os.WriteFile(filename, data, 0o644); err != nil {
        c.log.Debug("failed to write spill file", "error", err)
        return
    }

    c.buffer = c.buffer[:0]
}

// flushSpillFiles reads and sends all spill files, cleaning up after successful delivery.
// Delivery mode controls whether files are deleted before or after send.
func (c *Collector) flushSpillFiles(ctx context.Context) error {
    files, err := filepath.Glob(filepath.Join(c.dataDir, "telemetry-spill-*.json"))
    if err != nil || len(files) == 0 {
        return err
    }

    sort.Strings(files) // oldest first

    for _, f := range files {
        data, err := os.ReadFile(f)
        if err != nil {
            c.log.Debug("failed to read spill file", "error", err, "file", f)
            continue
        }

        var events []Event
        if err := json.Unmarshal(data, &events); err != nil {
            c.log.Debug("failed to unmarshal spill file", "error", err, "file", f)
            os.Remove(f) // corrupt file — discard
            continue
        }

        if c.deliveryMode == props.DeliveryAtMostOnce {
            os.Remove(f) // delete before send — no duplicates, possible loss
        }

        if err := c.backend.Send(ctx, events); err != nil {
            c.log.Debug("failed to send spill file", "error", err, "file", f)
            continue // leave file for retry (at-least-once) or already deleted (at-most-once)
        }

        if c.deliveryMode == props.DeliveryAtLeastOnce {
            os.Remove(f) // delete after successful send
        }
    }

    return nil
}

// deleteSpillFiles removes all spill files. Used by Drop() on consent withdrawal.
func (c *Collector) deleteSpillFiles() error {
    files, _ := filepath.Glob(filepath.Join(c.dataDir, "telemetry-spill-*.json"))
    for _, f := range files {
        os.Remove(f)
    }
    return nil
}

// pruneSpillFiles removes the oldest spill files when the count exceeds maxSpillFiles.
// Must be called with c.mu held.
func (c *Collector) pruneSpillFiles() {
    files, _ := filepath.Glob(filepath.Join(c.dataDir, "telemetry-spill-*.json"))
    if len(files) < maxSpillFiles {
        return
    }

    sort.Strings(files)
    for _, f := range files[:len(files)-maxSpillFiles+1] {
        os.Remove(f)
    }
}
```

### Data Directory Resolution

```go
// ResolveDataDir determines the directory for telemetry data files (spill files,
// local-only logs). Uses the config directory if it exists and is writable,
// otherwise falls back to /tmp.
func ResolveDataDir(p *props.Props) string {
    if p.Config != nil {
        v := p.Config.GetViper()
        if cfgFile := v.ConfigFileUsed(); cfgFile != "" {
            dir := filepath.Dir(cfgFile)
            if info, err := os.Stat(dir); err == nil && info.IsDir() {
                // Check writability
                testFile := filepath.Join(dir, ".telemetry-write-test")
                if f, err := os.Create(testFile); err == nil {
                    f.Close()
                    os.Remove(testFile)
                    return dir
                }
            }
        }
    }

    return os.TempDir()
}
```

### Machine ID Hashing

```go
// HashedMachineID returns a privacy-preserving machine identifier derived from
// multiple system signals: OS machine ID, first non-loopback MAC address,
// hostname, and username. Each signal degrades gracefully if unavailable.
// The result is the first 8 bytes of the SHA-256 hash, encoded as 16 hex chars.
// Computed fresh on every invocation — not persisted to config.
func HashedMachineID() string {
    var parts []string

    // OS machine ID — most reliable per-installation identifier
    // Linux: /etc/machine-id, macOS: IOPlatformUUID, Windows: MachineGuid
    parts = append(parts, osMachineID())

    // First non-loopback MAC address — hardware dimension
    parts = append(parts, firstMACAddress())

    // Hostname + username — fallback signals
    hostname, _ := os.Hostname()
    parts = append(parts, hostname)

    u, _ := user.Current()
    if u != nil {
        parts = append(parts, u.Username)
    }

    raw := strings.Join(parts, ":")
    h := sha256.Sum256([]byte(raw))

    return hex.EncodeToString(h[:8]) // 16 hex chars
}

// osMachineID returns the OS-level machine identifier.
// Linux: reads /etc/machine-id
// macOS: reads IOPlatformUUID via ioreg
// Windows: reads MachineGuid from registry
// Returns "" if unavailable.
func osMachineID() string {
    // Platform-specific implementation
    // ...
}

// firstMACAddress returns the hardware address of the first non-loopback
// network interface. Returns "" if unavailable (e.g. containers with
// randomised MACs — the hash still works with reduced uniqueness).
func firstMACAddress() string {
    ifaces, err := net.Interfaces()
    if err != nil {
        return ""
    }

    for _, iface := range ifaces {
        if iface.Flags&net.FlagLoopback != 0 {
            continue
        }
        if len(iface.HardwareAddr) > 0 {
            return iface.HardwareAddr.String()
        }
    }

    return ""
}
```

### Built-in Backends

#### No-Op (disabled state)

```go
type noopBackend struct{}

func NewNoopBackend() Backend                                      { return &noopBackend{} }
func (n *noopBackend) Send(_ context.Context, _ []Event) error     { return nil }
func (n *noopBackend) Close() error                                { return nil }
```

#### Stdout (debugging)

```go
func NewStdoutBackend(w io.Writer) Backend { return &stdoutBackend{w: w} }

func (s *stdoutBackend) Send(_ context.Context, events []Event) error {
    enc := json.NewEncoder(s.w)
    enc.SetIndent("", "  ")
    return enc.Encode(events)
}

func (s *stdoutBackend) Close() error { return nil }
```

#### File (local-only mode)

```go
func NewFileBackend(path string) Backend { return &fileBackend{path: path} }

func (f *fileBackend) Send(_ context.Context, events []Event) error {
    f.mu.Lock()
    defer f.mu.Unlock()

    file, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
    if err != nil {
        return errors.Wrap(err, "opening telemetry log")
    }

    defer file.Close()

    enc := json.NewEncoder(file)
    for _, event := range events {
        if err := enc.Encode(event); err != nil {
            return errors.Wrap(err, "writing telemetry event")
        }
    }

    return nil
}

func (f *fileBackend) Close() error { return nil }
```

#### HTTP

```go
func NewHTTPBackend(endpoint string, log logger.Logger) Backend {
    return &httpBackend{
        endpoint: endpoint,
        client:   gtbhttp.NewClient(gtbhttp.WithTimeout(5 * time.Second)),
        log:      log,
    }
}

func (h *httpBackend) Send(ctx context.Context, events []Event) error {
    body, err := json.Marshal(events)
    if err != nil {
        return errors.Wrap(err, "marshalling telemetry events")
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
    if err != nil {
        return errors.Wrap(err, "creating telemetry request")
    }

    req.Header.Set("Content-Type", "application/json")

    resp, err := h.client.Do(req)
    if err != nil {
        return nil // silently drop — telemetry must never block the user
    }

    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        h.log.Debug("telemetry endpoint returned non-success status",
            "status", resp.StatusCode, "endpoint", h.endpoint)
    }

    return nil
}

func (h *httpBackend) Close() error { return nil }
```

#### OpenTelemetry / OTLP

The OTLP backend exports events as OpenTelemetry log records via the OTLP HTTP exporter. This makes GTB telemetry compatible with any OTel-capable collector (Grafana Alloy, OpenTelemetry Collector, Datadog Agent, Honeycomb, etc.) and plugs directly into existing enterprise observability stacks.

**Event mapping**: each `telemetry.Event` is emitted as an OTel log record with:
- `Severity`: `Info`
- `Body`: the event `Name` string
- Attributes: `event.type`, `tool.name`, `tool.version`, `host.arch`, `os.type`, `machine.id` (hashed), plus any custom `Metadata` key/value pairs

**Error handling**: The OTel SDK's `logger.Emit(ctx, rec)` is fire-and-forget — errors surface asynchronously through the OTel error handler, not through `Backend.Send`. A custom OTel error handler is registered to route errors to the GTB logger at debug level, ensuring OTLP failures are visible when debugging.

**Dependencies** (add to `go.mod`):
```
go.opentelemetry.io/otel
go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp
go.opentelemetry.io/otel/sdk/log
```

```go
// OTelOption configures the OTLP backend.
type OTelOption func(*otelConfig)

type otelConfig struct {
    headers  map[string]string
    insecure bool
    log      logger.Logger
}

// WithOTelHeaders sets HTTP headers sent with every OTLP request (e.g. auth tokens).
func WithOTelHeaders(headers map[string]string) OTelOption {
    return func(c *otelConfig) { c.headers = headers }
}

// WithOTelInsecure disables TLS — use only for local collectors.
func WithOTelInsecure() OTelOption {
    return func(c *otelConfig) { c.insecure = true }
}

// WithOTelLogger sets the logger for routing OTel SDK errors.
func WithOTelLogger(log logger.Logger) OTelOption {
    return func(c *otelConfig) { c.log = log }
}

// NewOTelBackend creates a Backend that exports events as OTel log records via OTLP/HTTP.
// endpoint is the base URL of the OTLP HTTP endpoint
// (e.g. "https://collector.example.com:4318").
// A custom OTel error handler is registered to route SDK errors to the provided logger.
func NewOTelBackend(ctx context.Context, endpoint string, opts ...OTelOption) (Backend, error) {
    cfg := &otelConfig{}
    for _, o := range opts {
        o(cfg)
    }

    // Register custom error handler to surface OTel SDK errors via our logger
    if cfg.log != nil {
        otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
            cfg.log.Debug("OTel SDK error", "error", err)
        }))
    }

    exporterOpts := []otlploghttp.Option{
        otlploghttp.WithEndpoint(endpoint),
    }

    if cfg.insecure {
        exporterOpts = append(exporterOpts, otlploghttp.WithInsecure())
    }

    if len(cfg.headers) > 0 {
        exporterOpts = append(exporterOpts, otlploghttp.WithHeaders(cfg.headers))
    }

    exporter, err := otlploghttp.New(ctx, exporterOpts...)
    if err != nil {
        return nil, errors.Wrap(err, "creating OTLP log exporter")
    }

    provider := sdklog.NewLoggerProvider(
        sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
    )

    return &otelBackend{provider: provider}, nil
}

func (o *otelBackend) Send(ctx context.Context, events []Event) error {
    logger := o.provider.Logger("gtb-telemetry")

    for _, e := range events {
        var rec log.Record
        rec.SetTimestamp(e.Timestamp)
        rec.SetSeverity(log.SeverityInfo)
        rec.SetBody(log.StringValue(e.Name))
        rec.AddAttributes(
            log.String("event.type", string(e.Type)),
            log.String("tool.name", e.ToolName),
            log.String("tool.version", e.Version),
            log.String("host.arch", e.Arch),
            log.String("os.type", e.OS),
            log.String("machine.id", e.MachineID),
        )

        for k, v := range e.Metadata {
            rec.AddAttributes(log.String(k, v))
        }

        logger.Emit(ctx, rec)
    }

    return nil
}

func (o *otelBackend) Close() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return o.provider.Shutdown(ctx)
}
```

### Built-in Deletion Requestors

```go
// HTTPDeletionRequestor sends a deletion request via HTTP POST/DELETE.
type HTTPDeletionRequestor struct {
    endpoint string
    client   *http.Client
    log      logger.Logger
}

func NewHTTPDeletionRequestor(endpoint string, log logger.Logger) DeletionRequestor {
    return &HTTPDeletionRequestor{
        endpoint: endpoint,
        client:   gtbhttp.NewClient(gtbhttp.WithTimeout(10 * time.Second)),
        log:      log,
    }
}

func (h *HTTPDeletionRequestor) RequestDeletion(ctx context.Context, machineID string) error {
    body, _ := json.Marshal(map[string]string{"machine_id": machineID})
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
    if err != nil {
        return errors.Wrap(err, "creating deletion request")
    }

    req.Header.Set("Content-Type", "application/json")

    resp, err := h.client.Do(req)
    if err != nil {
        return errors.Wrap(err, "sending deletion request")
    }

    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        h.log.Debug("deletion endpoint returned non-success status",
            "status", resp.StatusCode, "endpoint", h.endpoint)
        return errors.Newf("deletion request returned status %d", resp.StatusCode)
    }

    return nil
}

// EmailDeletionRequestor composes a deletion request email.
type EmailDeletionRequestor struct {
    address string
    toolName string
}

func NewEmailDeletionRequestor(address, toolName string) DeletionRequestor {
    return &EmailDeletionRequestor{address: address, toolName: toolName}
}

func (e *EmailDeletionRequestor) RequestDeletion(_ context.Context, machineID string) error {
    // Opens a mailto: link with pre-filled subject and body
    subject := fmt.Sprintf("Data Deletion Request — %s", e.toolName)
    body := fmt.Sprintf("Please delete all telemetry data associated with machine ID: %s", machineID)
    url := fmt.Sprintf("mailto:%s?subject=%s&body=%s",
        e.address,
        url.QueryEscape(subject),
        url.QueryEscape(body))
    return openURL(url) // platform-specific browser/mail client launch
}

// EventDeletionRequestor sends a data.deletion_request event through the existing
// telemetry backend. This is the universal fallback — works with any backend type.
type EventDeletionRequestor struct {
    backend Backend
}

func NewEventDeletionRequestor(backend Backend) DeletionRequestor {
    return &EventDeletionRequestor{backend: backend}
}

func (e *EventDeletionRequestor) RequestDeletion(ctx context.Context, machineID string) error {
    event := Event{
        Timestamp: time.Now().UTC(),
        Type:      EventDeletionRequest,
        Name:      "deletion_request",
        MachineID: machineID,
    }

    return e.backend.Send(ctx, []Event{event})
}
```

### Backend Selection (root command)

```go
// In pkg/cmd/root/root.go — called from PersistentPreRunE

func buildTelemetryCollector(ctx context.Context, p *props.Props) *telemetry.Collector {
    dataDir := telemetry.ResolveDataDir(p)

    if p.Tool.IsDisabled(props.TelemetryCmd) {
        return telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(), p.Tool.Name, p.Version.Version, nil, p.Logger, dataDir, props.DeliveryAtLeastOnce)
    }

    cfg := telemetry.Config{
        Enabled:   p.Config.GetBool("telemetry.enabled"),
        LocalOnly: p.Config.GetBool("telemetry.local_only"),
    }

    // Env var override (non-interactive bypass — tool-name-agnostic)
    if val, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
        cfg.Enabled, _ = strconv.ParseBool(val)
    }

    if val, ok := os.LookupEnv("TELEMETRY_LOCAL"); ok {
        cfg.LocalOnly, _ = strconv.ParseBool(val)
    }

    if !cfg.Enabled {
        return telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(), p.Tool.Name, p.Version.Version, nil, p.Logger, dataDir, props.DeliveryAtLeastOnce)
    }

    deliveryMode := p.Tool.Telemetry.DeliveryMode
    if deliveryMode == "" {
        deliveryMode = props.DeliveryAtLeastOnce
    }

    var backend telemetry.Backend

    switch {
    case p.Tool.Telemetry.Backend != nil:
        // Custom backend supplied by the tool author.
        // TelemetryConfig.Backend is func(*Props) any to avoid an import cycle
        // in pkg/props. Type-assert here in pkg/cmd/root which imports both.
        raw := p.Tool.Telemetry.Backend(p)
        b, ok := raw.(telemetry.Backend)
        if !ok {
            p.Logger.Warn("TelemetryConfig.Backend did not return a telemetry.Backend; falling back to noop")
            backend = telemetry.NewNoopBackend()
        } else {
            backend = b
        }
    case cfg.LocalOnly:
        backend = telemetry.NewFileBackend(filepath.Join(dataDir, "telemetry.log"))
    case p.Tool.Telemetry.OTelEndpoint != "":
        opts := []telemetry.OTelOption{
            telemetry.WithOTelLogger(p.Logger),
        }
        if p.Tool.Telemetry.OTelInsecure {
            opts = append(opts, telemetry.WithOTelInsecure())
        }
        if len(p.Tool.Telemetry.OTelHeaders) > 0 {
            opts = append(opts, telemetry.WithOTelHeaders(p.Tool.Telemetry.OTelHeaders))
        }
        b, err := telemetry.NewOTelBackend(ctx, p.Tool.Telemetry.OTelEndpoint, opts...)
        if err != nil {
            p.Logger.Warn("failed to initialise OTel backend, falling back to noop", "error", err)
            backend = telemetry.NewNoopBackend()
        } else {
            backend = b
        }
    case p.Tool.Telemetry.Endpoint != "":
        backend = telemetry.NewHTTPBackend(p.Tool.Telemetry.Endpoint, p.Logger)
    default:
        backend = telemetry.NewNoopBackend() // enabled but no sink configured
    }

    return telemetry.NewCollector(cfg, backend, p.Tool.Name, p.Version.Version, p.Tool.Telemetry.Metadata, p.Logger, dataDir, deliveryMode)
}
```

### `Props` — new `Collector` field

```go
// In pkg/props/props.go
type Props struct {
    Tool         Tool
    Logger       logger.Logger
    Config       config.Containable
    Assets       Assets
    FS           afero.Fs
    Version      version.Version
    ErrorHandler errorhandling.ErrorHandler
    Collector    TelemetryCollector // always non-nil; noop when telemetry disabled
}
```

`TelemetryCollector` is the interface defined in `pkg/props/telemetry.go`. The concrete value assigned at runtime is `*telemetry.Collector` from `pkg/telemetry`, which implements this interface. `pkg/props` never imports `pkg/telemetry` — only `pkg/cmd/root` (which imports both) performs the wiring.

### `OnFinalize` flush

```go
// In pkg/cmd/root/root.go — registered during root command construction
cobra.OnFinalize(func() {
    if props.Collector != nil {
        // Re-check enabled state — if user ran `telemetry disable` mid-session,
        // respect the withdrawal of consent and do not flush.
        if !props.Config.GetBool("telemetry.enabled") {
            return
        }
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        _ = props.Collector.Flush(ctx)
    }
})
```

### CLI Commands

```go
// pkg/cmd/telemetry/telemetry.go

func NewCmdTelemetry(p *props.Props) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "telemetry",
        Short: "Manage anonymous usage telemetry",
    }

    cmd.AddCommand(newEnableCmd(p), newDisableCmd(p), newStatusCmd(p), newResetCmd(p))

    return cmd
}
```

Enable and disable write the setting and persist to disk using the same `WriteConfig`/`SafeWriteConfig` pattern as `pkg/cmd/config/set.go`. Disable also drops all buffered events and spill files immediately:

```go
func newEnableCmd(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "enable",
        Short: "Enable anonymous usage telemetry",
        RunE: func(cmd *cobra.Command, _ []string) error {
            p.Config.Set("telemetry.enabled", true)

            v := p.Config.GetViper()
            if err := v.WriteConfig(); err != nil {
                if err2 := v.SafeWriteConfig(); err2 != nil {
                    return errors.Wrap(err2, "failed to write config")
                }
            }

            p.Logger.Print("Telemetry enabled. Thank you for helping improve " + p.Tool.Name + "!")
            p.Logger.Print("No personally identifiable information is collected.")

            return nil
        },
    }
}

func newDisableCmd(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "disable",
        Short: "Disable usage telemetry",
        RunE: func(cmd *cobra.Command, _ []string) error {
            p.Config.Set("telemetry.enabled", false)

            v := p.Config.GetViper()
            if err := v.WriteConfig(); err != nil {
                if err2 := v.SafeWriteConfig(); err2 != nil {
                    return errors.Wrap(err2, "failed to write config")
                }
            }

            // Immediately drop all buffered and spilled events — the user's
            // withdrawal of consent is immediate and total.
            if p.Collector != nil {
                _ = p.Collector.Drop()
            }

            p.Logger.Print("Telemetry disabled. All pending events have been discarded.")

            return nil
        },
    }
}

func newStatusCmd(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "status",
        Short: "Show current telemetry status",
        RunE: func(cmd *cobra.Command, _ []string) error {
            enabled := p.Config.GetBool("telemetry.enabled")
            localOnly := p.Config.GetBool("telemetry.local_only")

            switch {
            case !enabled:
                p.Logger.Print("Telemetry: disabled")
            case localOnly:
                p.Logger.Print("Telemetry: enabled (local-only)")
            default:
                p.Logger.Print("Telemetry: enabled")
            }

            p.Logger.Print("Machine ID: " + telemetry.HashedMachineID())

            return nil
        },
    }
}

func newResetCmd(p *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "reset",
        Short: "Clear local telemetry data and request remote deletion",
        Long: "Clears all local telemetry data (buffered events, spill files, " +
            "local-only logs) and sends a data deletion request to the remote " +
            "backend. Telemetry is disabled after reset.",
        RunE: func(cmd *cobra.Command, _ []string) error {
            machineID := telemetry.HashedMachineID()

            // 1. Drop all local data
            if p.Collector != nil {
                _ = p.Collector.Drop()
            }

            // Clear local-only log if it exists
            dataDir := telemetry.ResolveDataDir(p)
            logFile := filepath.Join(dataDir, "telemetry.log")
            if _, err := os.Stat(logFile); err == nil {
                os.Remove(logFile)
            }

            // 2. Send deletion request via configured requestor
            requestor := buildDeletionRequestor(p)
            ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
            defer cancel()

            if err := requestor.RequestDeletion(ctx, machineID); err != nil {
                p.Logger.Print("Deletion request could not be sent: " + err.Error())
                if p.Tool.Help.Channel != "" {
                    p.Logger.Print("Contact " + p.Tool.Help.Channel + " to request manual deletion.")
                }
            } else {
                p.Logger.Print("Deletion request sent for machine ID: " + machineID)
            }

            // 3. Disable telemetry
            p.Config.Set("telemetry.enabled", false)
            v := p.Config.GetViper()
            if err := v.WriteConfig(); err != nil {
                if err2 := v.SafeWriteConfig(); err2 != nil {
                    return errors.Wrap(err2, "failed to write config")
                }
            }

            p.Logger.Print("Local telemetry data cleared. Telemetry disabled.")

            return nil
        },
    }
}

// buildDeletionRequestor constructs the appropriate DeletionRequestor.
// Uses the tool-author's custom requestor if configured, otherwise falls back
// to sending a data.deletion_request event through the existing backend.
func buildDeletionRequestor(p *props.Props) telemetry.DeletionRequestor {
    if p.Tool.Telemetry.DeletionRequestor != nil {
        raw := p.Tool.Telemetry.DeletionRequestor(p)
        if r, ok := raw.(telemetry.DeletionRequestor); ok {
            return r
        }
        p.Logger.Warn("TelemetryConfig.DeletionRequestor did not return a telemetry.DeletionRequestor; falling back to event-based")
    }

    // Fall back to event-based deletion request through the existing backend
    // This works with any backend type — HTTP, OTLP, file, etc.
    return telemetry.NewEventDeletionRequestor(/* backend from collector */)
}
```

### `pkg/setup/telemetry` — `init()` registration

```go
// TelemetryInitialiser implements setup.Initialiser.
// It prompts the user to opt into telemetry during init.
type TelemetryInitialiser struct {
    props        *props.Props
    skipTelemetry bool // bound to --skip-telemetry flag; on struct for test isolation
}

func NewTelemetryInitialiser(p *props.Props) *TelemetryInitialiser {
    return &TelemetryInitialiser{props: p}
}

func (t *TelemetryInitialiser) Name() string {
    return "telemetry"
}

// IsConfigured returns true if the telemetry.enabled key has been explicitly
// set in config, OR if the TELEMETRY_ENABLED environment variable is set
// (any value counts as "configured — no prompt needed").
func (t *TelemetryInitialiser) IsConfigured(cfg config.Containable) bool {
    if _, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
        return true
    }

    return cfg.IsSet("telemetry.enabled")
}

// Configure prompts the user to opt into telemetry.
// If TELEMETRY_ENABLED is set, applies it directly without prompting.
func (t *TelemetryInitialiser) Configure(p *props.Props, cfg config.Containable) error {
    // Non-interactive bypass
    if val, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
        enabled, _ := strconv.ParseBool(val)
        cfg.Set("telemetry.enabled", enabled)
        return nil
    }

    // Interactive huh form
    var optIn bool
    form := huh.NewForm(huh.NewGroup(
        huh.NewConfirm().
            Title("Anonymous usage telemetry").
            Description(
                "Help improve " + p.Tool.Name + " by sending anonymous usage statistics.\n" +
                "No personally identifiable information is collected.\n" +
                "You can change this at any time with `" + p.Tool.Name + " telemetry enable/disable`.",
            ).
            Value(&optIn),
    ))

    if err := form.Run(); err != nil {
        return errors.Wrap(err, "telemetry consent form")
    }

    cfg.Set("telemetry.enabled", optIn)

    return nil
}

func init() {
    // Create a shared initialiser reference so the flag callback can bind
    // to the struct's skipTelemetry field.
    var initialiser *TelemetryInitialiser

    setup.Register(props.TelemetryCmd,
        []setup.InitialiserProvider{
            func(p *props.Props) setup.Initialiser {
                initialiser = NewTelemetryInitialiser(p)
                if initialiser.skipTelemetry {
                    return nil
                }
                return initialiser
            },
        },
        []setup.SubcommandProvider{
            func(p *props.Props) []*cobra.Command {
                return []*cobra.Command{NewCmdInitTelemetry(p)}
            },
        },
        []setup.FeatureFlag{
            func(cmd *cobra.Command) {
                isCI := os.Getenv("CI") == "true"
                // Bind to the struct field once the initialiser is created.
                // During flag parsing, initialiser may not yet exist, so we
                // use a local var and copy it in the InitialiserProvider above.
                cmd.Flags().BoolVarP(&initialiser.skipTelemetry, "skip-telemetry", "", isCI,
                    "skip telemetry consent prompt (non-interactive environments)")
            },
        },
    )
}
```

---

## GTB Binary Configuration

The GTB binary (`internal/cmd/root/root.go`) enables telemetry to track anonymous framework adoption. This is the reference implementation and demonstrates the expected usage pattern for tool authors.

```go
// internal/cmd/root/root.go
p := &props.Props{
    Tool: props.Tool{
        Name:        "gtb",
        Summary:     "The gtb CLI",
        Description: "A CLI tool for managing and generating gtb projects.",
        ReleaseSource: props.ReleaseSource{
            Type:  "github",
            Owner: "phpboyscout",
            Repo:  "gtb",
        },
        Features: props.SetFeatures(
            props.Disable(props.InitCmd),
            props.Enable(props.AiCmd),
            props.Enable(props.TelemetryCmd), // collect anonymous framework usage
        ),
        Telemetry: props.TelemetryConfig{
            OTelEndpoint: "https://otlp-gateway-prod-gb-south-1.grafana.net/otlp",
            OTelHeaders: map[string]string{
                "Authorization": "Basic " + otelAuth, // injected via ldflags
            },
        },
    },
    // ...
}
```

**Compile-time credential injection**: The GTB binary uses Grafana Cloud's OTLP gateway for telemetry collection. The endpoint is hardcoded (it's not a secret), while the auth token is injected as a package-level variable via `-ldflags -X` at compile time by goreleaser — it never appears in source or environment variables at runtime. This keeps the binary self-contained.

```go
// internal/cmd/root/root.go — injected via ldflags
var otelAuth string // Base64-encoded "<instanceID>:<apiKey>"
```

Goreleaser ldflags (both build targets):
```
-X github.com/phpboyscout/go-tool-base/internal/cmd/root.otelAuth={{.Env.GTB_OTEL_AUTH}}
```

The `GTB_OTEL_AUTH` CI secret must be configured in the GitHub repository settings. The value is `base64("<instanceID>:<serviceAccountToken>")`. The Grafana Cloud free tier provides 50 GB/month log ingestion with 14-day retention — more than sufficient for CLI telemetry.

---

## Tool Author Usage

### Minimal (no custom backend)

```go
props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        Endpoint: "https://telemetry.example.com/events",
    },
}
```

### Custom backend

```go
props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        // Backend returns any; must be a telemetry.Backend at runtime.
        // pkg/cmd/root type-asserts the value — a failed assertion falls back to noop.
        Backend: func(p *props.Props) any {
            return myanalytics.NewBackend(p.Config.GetString("analytics.key"))
        },
        Metadata: map[string]string{"env": "production"},
    },
}
```

### OTLP / OpenTelemetry backend

```go
props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        OTelEndpoint: "https://collector.example.com:4318",
        OTelHeaders:  map[string]string{"x-api-key": os.Getenv("OTEL_API_KEY")},
    },
}
```

### Custom deletion requestor

```go
props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        Endpoint: "https://telemetry.example.com/events",
        DeletionRequestor: func(p *props.Props) any {
            return telemetry.NewHTTPDeletionRequestor(
                "https://telemetry.example.com/deletion",
                p.Logger,
            )
        },
    },
}
```

### Delivery mode

```go
props.Tool{
    Name: "mytool",
    Features: props.SetFeatures(props.Enable(props.TelemetryCmd)),
    Telemetry: props.TelemetryConfig{
        Endpoint:     "https://telemetry.example.com/events",
        DeliveryMode: props.DeliveryAtMostOnce, // default is DeliveryAtLeastOnce
    },
}
```

### Local-only (no transmission)

```go
// User sets in config: telemetry.local_only: true
// Or at runtime: TELEMETRY_ENABLED=true TELEMETRY_LOCAL=true
```

---

## Manifest Integration

The `.gtb/manifest.yaml` gains a `telemetry` block under `properties` so tool authors can declare their telemetry endpoint when creating a project via `generate project`. The generator reads this value and emits the appropriate `props.TelemetryConfig` initialisation in the generated root command.

### `internal/generator/manifest.go` — new `ManifestTelemetry` type

```go
// ManifestTelemetry holds telemetry configuration for generated tools.
type ManifestTelemetry struct {
    Endpoint     string `yaml:"endpoint,omitempty"`
    OTelEndpoint string `yaml:"otel_endpoint,omitempty"`
}

// ManifestProperties gains a Telemetry field:
type ManifestProperties struct {
    Name        string            `yaml:"name"`
    Description MultilineString   `yaml:"description"`
    Features    []ManifestFeature `yaml:"features"`
    Help        ManifestHelp      `yaml:"help,omitempty"`
    Telemetry   ManifestTelemetry `yaml:"telemetry,omitempty"` // NEW
}
```

### Example manifest snippet

```yaml
properties:
  name: mytool
  features:
    - name: telemetry
      enabled: true
  telemetry:
    endpoint: https://telemetry.example.com/events
```

Or with OTLP:

```yaml
properties:
  name: mytool
  features:
    - name: telemetry
      enabled: true
  telemetry:
    otel_endpoint: https://collector.example.com:4318
```

### `internal/generator/skeleton.go` — updated feature lists and config

`calculateEnabledFeatures` adds `"telemetry"` to the opt-in list:

```go
func calculateEnabledFeatures(features []ManifestFeature) []string {
    optInFeatures := []string{"ai", "config", "telemetry"} // telemetry is opt-in
    // ...
}
```

`SkeletonConfig` gains telemetry fields, populated from `ManifestTelemetry` during regeneration:

```go
type SkeletonConfig struct {
    // ... existing fields ...
    TelemetryEndpoint     string // populated from manifest telemetry.endpoint
    TelemetryOTelEndpoint string // populated from manifest telemetry.otel_endpoint
}
```

The anonymous data struct inside `generateSkeletonFiles` is updated to carry it through to `SkeletonRootData`:

```go
data := struct {
    // ... existing fields ...
    TelemetryEndpoint     string
    TelemetryOTelEndpoint string
}{
    // ...
    TelemetryEndpoint:     config.TelemetryEndpoint,
    TelemetryOTelEndpoint: config.TelemetryOTelEndpoint,
}
```

`regenerate.go` populates the fields from the manifest:

```go
// When building SkeletonConfig from the manifest during regeneration:
TelemetryEndpoint:     m.Properties.Telemetry.Endpoint,
TelemetryOTelEndpoint: m.Properties.Telemetry.OTelEndpoint,
```

### `internal/generator/templates/skeleton_root.go` — generator updates

`SkeletonRootData` gains telemetry fields:

```go
type SkeletonRootData struct {
    // ... existing fields ...
    TelemetryEndpoint     string
    TelemetryOTelEndpoint string
}
```

`buildToolDict` conditionally emits `Telemetry` with the appropriate endpoint:

```go
func buildToolDict(data SkeletonRootData) jen.Dict {
    toolDict := jen.Dict{
        // ... existing entries ...
    }

    telemetryDict := jen.Dict{}
    hasTelemetry := false

    if data.TelemetryOTelEndpoint != "" {
        telemetryDict[jen.Id("OTelEndpoint")] = jen.Lit(data.TelemetryOTelEndpoint)
        hasTelemetry = true
    } else if data.TelemetryEndpoint != "" {
        telemetryDict[jen.Id("Endpoint")] = jen.Lit(data.TelemetryEndpoint)
        hasTelemetry = true
    }

    if hasTelemetry {
        toolDict[jen.Id("Telemetry")] = jen.Qual("github.com/phpboyscout/go-tool-base/pkg/props", "TelemetryConfig").Values(telemetryDict)
    }

    return toolDict
}
```

`getFeatureCmd` gains a `"telemetry"` case:

```go
func getFeatureCmd(feature string) jen.Code {
    switch feature {
    // ... existing cases ...
    case "telemetry":
        return jen.Qual("github.com/phpboyscout/go-tool-base/pkg/props", "TelemetryCmd")
    }
    return nil
}
```

### CLI flags for `generate project`

The `generate project` command gains telemetry flags:

- `--telemetry-endpoint` — HTTP JSON endpoint for telemetry events
- `--telemetry-otel-endpoint` — OTLP/HTTP endpoint (takes precedence over `--telemetry-endpoint`)

When neither flag is provided and the interactive form is active, telemetry configuration is included in the project generation form prompts.

---

## Project Structure

```
pkg/telemetry/
├── telemetry.go            ← Event, Collector, Config, EventType constants, DeliveryMode
├── backend.go              ← Backend interface + noop/stdout/file/http implementations
├── backend_otel.go         ← OTel/OTLP backend + OTelOption helpers + custom error handler
├── deletion.go             ← DeletionRequestor interface + HTTP/email/event implementations
├── machine.go              ← HashedMachineID (exported), osMachineID, firstMACAddress
├── datadir.go              ← ResolveDataDir helper
├── spill.go                ← Buffer spill, flush spill files, prune, delete
├── telemetry_test.go       ← Collector tests (disabled, track, flush, concurrent, noPII, drop, spill)
├── backend_test.go         ← Backend tests (noop, stdout, file, http non-2xx logging, otel)
├── deletion_test.go        ← DeletionRequestor tests (http, email, event)
├── machine_test.go         ← HashedMachineID tests (stable, not raw, multi-signal)
├── spill_test.go           ← Spill tests (cap, prune, flush, delivery modes)
├── telemetry_integration_test.go  ← file/http/otel round-trip integration tests
pkg/setup/telemetry/
├── telemetry.go            ← TelemetryInitialiser + init() registration
├── telemetry_test.go       ← IsConfigured, Configure (env bypass, interactive), Name
pkg/cmd/telemetry/
├── telemetry.go            ← enable/disable/status/reset commands
├── telemetry_test.go       ← command tests
pkg/props/
├── telemetry.go            ← NEW: TelemetryCollector interface, EventType + constants,
│                              TelemetryConfig, DeliveryMode, TelemetryCmd flag
├── tool.go                 ← MODIFIED: Tool.Telemetry field
├── props.go                ← MODIFIED: Collector TelemetryCollector field
pkg/cmd/root/
├── root.go                 ← MODIFIED: buildTelemetryCollector (with ctx), OnFinalize flush
internal/cmd/root/
├── root.go                 ← MODIFIED: Enable(TelemetryCmd), Telemetry.Endpoint for GTB binary
internal/generator/
├── manifest.go             ← MODIFIED: ManifestTelemetry (endpoint + otel_endpoint),
│                              ManifestProperties.Telemetry
├── skeleton.go             ← MODIFIED: SkeletonConfig telemetry fields, calculateEnabledFeatures
├── regenerate.go           ← MODIFIED: populate telemetry fields from manifest
├── templates/
│   └── skeleton_root.go    ← MODIFIED: SkeletonRootData telemetry fields, buildToolDict,
│                              getFeatureCmd
```

---

## Environment Variables

| Variable | Values | Effect |
|----------|--------|--------|
| `TELEMETRY_ENABLED` | `true` / `false` / `1` / `0` | Bypasses interactive consent; writes config value when set during `init`; overrides config at runtime |
| `TELEMETRY_LOCAL` | `true` / `false` | Forces local-only mode (file backend) regardless of config |
| `CI` | `true` | Sets `--skip-telemetry` default to `true`, suppressing the init prompt |

These names are deliberately un-prefixed (no `GTB_` or tool-name prefix) so that tool authors building on GTB can use the same env var names without GTB-specific naming conventions bleeding into their users' environment.

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestCollector_Disabled` | Disabled config → noop, no backend calls |
| `TestCollector_Track` | Track events → buffered correctly with metadata merged |
| `TestCollector_Flush` | Flush → events sent to backend, buffer cleared |
| `TestCollector_FlushEmpty` | Flush with no events → backend not called |
| `TestCollector_ConcurrentTrack` | 100 goroutines tracking → no race |
| `TestCollector_FlushError` | Backend error → warning logged, error returned |
| `TestCollector_NoPII` | Event JSON does not contain raw hostname, username, or MAC |
| `TestCollector_Drop` | Drop → buffer cleared, spill files deleted, no events sent |
| `TestCollector_SpillOnCap` | Buffer reaches 1000 → spill file created, buffer cleared |
| `TestCollector_FlushReadsSpillFiles` | Spill files present → sent before current buffer |
| `TestCollector_SpillPrune` | >10 spill files → oldest deleted |
| `TestCollector_SpillSizeCap` | Spill file capped at 1MB |
| `TestCollector_DeliveryAtLeastOnce` | Spill file deleted after successful send |
| `TestCollector_DeliveryAtMostOnce` | Spill file deleted before send |
| `TestNoopBackend` | Send → nil, no side effects |
| `TestStdoutBackend` | Send → valid JSON written to writer |
| `TestFileBackend` | Send → events appended; concurrent sends safe |
| `TestHTTPBackend_Success` | Mock server → correct payload posted |
| `TestHTTPBackend_Timeout` | Slow server → no error returned (silent drop) |
| `TestHTTPBackend_Non2xx` | 400/401 response → debug log emitted, no error returned |
| `TestOTelBackend_Send` | Events emitted as OTel log records with correct attributes |
| `TestOTelBackend_Close` | `Close()` shuts down the OTLP exporter cleanly |
| `TestOTelBackend_InsecureOption` | `WithOTelInsecure` → TLS disabled |
| `TestOTelBackend_Headers` | `WithOTelHeaders` → headers present on OTLP request |
| `TestOTelBackend_ErrorHandler` | OTel SDK error → routed to GTB logger at debug level |
| `TestHashedMachineID_Stable` | Same process → same hash |
| `TestHashedMachineID_NotRaw` | Hash does not contain raw hostname, username, or MAC |
| `TestHashedMachineID_MultiSignal` | Uses OS machine ID, MAC, hostname, and username |
| `TestResolveDataDir_ConfigDir` | Config dir exists and writable → returns config dir |
| `TestResolveDataDir_Fallback` | Config dir unavailable → returns /tmp |
| `TestHTTPDeletionRequestor` | Sends POST with machine ID, handles non-2xx |
| `TestEmailDeletionRequestor` | Composes mailto URL with correct subject and body |
| `TestEventDeletionRequestor` | Sends data.deletion_request event through backend |
| `TestTelemetryInitialiser_Name` | Returns "telemetry" |
| `TestTelemetryInitialiser_IsConfigured_KeySet` | Config has `telemetry.enabled` → true |
| `TestTelemetryInitialiser_IsConfigured_EnvSet` | `TELEMETRY_ENABLED` present → true |
| `TestTelemetryInitialiser_IsConfigured_Neither` | Neither set → false |
| `TestTelemetryInitialiser_Configure_EnvTrue` | Env `true` → config set true, no form |
| `TestTelemetryInitialiser_Configure_EnvFalse` | Env `false` → config set false, no form |
| `TestEnableCmd` | Enable → config updated, persisted to disk |
| `TestDisableCmd` | Disable → config updated, persisted, buffer dropped, spill files deleted |
| `TestResetCmd` | Reset → local data cleared, deletion request sent, telemetry disabled |
| `TestStatusCmd_Disabled` | Not enabled → output contains "disabled" |
| `TestStatusCmd_Enabled` | Enabled → output contains "enabled" |
| `TestStatusCmd_LocalOnly` | Local-only → output contains "local-only" |
| `TestCustomBackend` | BackendFactory on Tool → factory called, events routed |
| `TestCustomDeletionRequestor` | DeletionRequestor on Tool → factory called, request sent |
| `TestEventTypeSync` | props.EventType constants match telemetry.EventType constants |
| `TestCalculateEnabledFeatures_Telemetry` | `telemetry: enabled` in manifest → in enabled list |
| `TestSkeletonRoot_TelemetryEndpoint` | Non-empty endpoint → `Telemetry: props.TelemetryConfig{...}` generated |
| `TestSkeletonRoot_TelemetryOTelEndpoint` | Non-empty OTel endpoint → `OTelEndpoint` field generated |
| `TestSkeletonRoot_NoTelemetryEndpoint` | Empty endpoint → no `Telemetry` field generated |

### Integration Tests

- **File backend round-trip**: `Collector` → `FileBackend` → flush → read file, verify JSON structure and no PII.
- **HTTP backend delivery**: `httptest.Server` → `HTTPBackend` → flush, assert payload received and non-2xx responses logged.
- **OTLP backend delivery**: local `otelcol` collector (or in-process OTLP receiver) → `OTelBackend` → flush, assert log records received with correct attributes and no PII.
- **Spill round-trip**: Fill buffer to cap → verify spill file created → flush → verify spill file sent and cleaned up.
- **Env bypass in init**: Set `TELEMETRY_ENABLED=true`, run `TelemetryInitialiser.Configure`, assert config value written and no form shown.
- **Deletion request round-trip**: `httptest.Server` → `HTTPDeletionRequestor` → `telemetry reset`, assert deletion request received.
- Gate with `testutil.SkipIfNotIntegration(t, "telemetry")`.

### E2E BDD Tests (Godog) — Moderate fit

`telemetry enable/disable/status/reset` are user-facing CLI commands with clear Given/When/Then semantics. Feature file: `features/cli/telemetry.feature`.

```gherkin
@cli @smoke
Feature: CLI Telemetry Command
  Users can opt in or out of anonymous usage telemetry.

  Background:
    Given the gtb binary is built

  Scenario: Telemetry is disabled by default
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stdout contains "disabled"

  Scenario: Enable telemetry
    When I run gtb with "telemetry enable"
    Then the exit code is 0
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stdout contains "enabled"

  Scenario: Disable telemetry after enabling
    When I run gtb with "telemetry enable"
    And I run gtb with "telemetry disable"
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stdout contains "disabled"

  Scenario: Disable telemetry discards pending events
    When I run gtb with "telemetry enable"
    And I run gtb with "telemetry disable"
    Then stdout contains "All pending events have been discarded"

  Scenario: Reset clears local data and disables telemetry
    When I run gtb with "telemetry enable"
    And I run gtb with "telemetry reset"
    Then the exit code is 0
    And stdout contains "Local telemetry data cleared"
    When I run gtb with "telemetry status"
    Then stdout contains "disabled"
```

The backend pipeline, collector internals, and spill mechanism are **not** testable via Godog — verify through unit and integration tests above.

### Coverage

- Target: 95%+ for `pkg/telemetry/` — privacy-sensitive code requires thorough testing.
- Target: 90%+ for `pkg/setup/telemetry/` and `pkg/cmd/telemetry/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.
- `gosec` must pass — no unguarded file writes, no leaked credentials.

---

## Documentation

- Godoc for all exported types and interfaces in `pkg/telemetry/` and `pkg/props/telemetry.go`.
- `docs/components/telemetry.md` must cover:
  - What data is collected (exact event types and fields with example JSON)
  - What is NOT collected (no PII, no command arguments, no file contents, no IP addresses)
  - **Machine ID explanation** — displayed by `telemetry status` and included in every event:
    - Derived by SHA-256 hashing `<os_machine_id>:<mac_address>:<hostname>:<username>`, taking the first 8 bytes (16 hex chars)
    - Uses multiple signals for uniqueness: OS machine ID (`/etc/machine-id` on Linux, `IOPlatformUUID` on macOS, `MachineGuid` on Windows), first non-loopback MAC address, hostname, and username
    - Each signal degrades gracefully if unavailable — the hash still works with reduced uniqueness
    - Cannot be reversed to recover any of the input values
    - Stable across invocations on the same machine for the same user; changes if any input signal changes (e.g. hostname rename, OS reinstall)
    - Computed fresh on every invocation — not persisted to config
    - Purpose: de-duplicate events across invocations without identifying individuals
    - Example: `telemetry status` output shows `Machine ID: 4a3f8c1d9e2b6f70`
  - How to enable/disable interactively and via `TELEMETRY_ENABLED` env var
  - Local-only mode (`TELEMETRY_LOCAL=true`)
  - How to supply a custom backend (`props.Tool.Telemetry.Backend`)
  - OTLP backend usage (`OTelEndpoint`, `OTelHeaders`)
  - Delivery modes (`DeliveryAtLeastOnce` vs `DeliveryAtMostOnce`)
  - Buffer overflow behaviour (spill to disk, caps, pruning)
  - **Data deletion / GDPR**: `telemetry reset` command, DeletionRequestor interface, built-in requestors
  - Manifest integration for `generate project`
- Update `docs/components/features.md` with `TelemetryCmd`.
- Privacy notice template for tool authors.

---

## Backwards Compatibility

- **No breaking changes**. `props.Tool` gains a zero-value-safe `Telemetry` field. Tools that don't set it and don't enable `TelemetryCmd` are entirely unaffected.
- `props.Props.Collector` is always non-nil — existing code that doesn't use telemetry is unaffected; code that calls `p.Collector.Track(...)` is safely ignored when disabled.
- Manifest files without a `telemetry` block are valid — `ManifestTelemetry` is omitempty and zero-value-safe.

---

## Open Questions

*No open questions remain.*

## Resolved Design Decisions

The following were open questions that have been resolved:

**`hashedMachineID` in `telemetry status`** — The machine ID is safe to display and should be shown in `telemetry status`. It is a privacy-preserving identifier derived from multiple system signals (OS machine ID, MAC address, hostname, username) via SHA-256; it does not reveal any raw personal data. The documentation (`docs/components/telemetry.md`) must clearly explain what the machine ID is, how it is derived, and that it cannot be reversed.

**Import cycle between `pkg/props` and `pkg/telemetry`** — Resolved by defining the `TelemetryCollector` interface and `EventType` constants in `pkg/props/telemetry.go`. `props.Props.Collector` is typed as `props.TelemetryCollector` (interface). `pkg/telemetry` imports `pkg/props`; `pkg/props` does not import `pkg/telemetry`. The concrete `*telemetry.Collector` implements the interface and the interface satisfaction is verified at compile time in `pkg/cmd/root`.

**`BackendFactory` and backend type location** — Resolved by keeping all backend logic (the `Backend` interface, noop/stdout/file/HTTP/OTLP implementations) inside `pkg/telemetry`. To avoid the import cycle, `TelemetryConfig.Backend` in `pkg/props` is typed as `func(*Props) any`. `pkg/cmd/root` (which imports both packages) performs the type assertion to `telemetry.Backend`. A failed assertion logs a warning and falls back to noop — tool authors are expected to return a correct type.

**EventType constants in both packages** — `EventType` (a `string` typedef) and its constants are defined in both `pkg/props` and `pkg/telemetry`. Since they resolve to plain strings, values from either package are interchangeable at the interface boundary. This gives consumers a clean import path via `pkg/props` while keeping `pkg/telemetry` self-contained. Synchronisation is verified by a dedicated test (`TestEventTypeSync`).

**Machine ID generation** — Uses multiple system signals (OS machine ID, MAC address, hostname, username) for robust uniqueness. Each signal degrades gracefully if unavailable. The hash is computed fresh on every invocation — not persisted to config — so it tracks the machine, not a stored identity.

**Endpoint configuration** — Telemetry endpoints (HTTP and OTLP) are tool-author concerns set in `props.TelemetryConfig` at build time. They are not user-configurable via the config file. The user config only stores consent (`telemetry.enabled`) and mode (`telemetry.local_only`). This prevents abuse and misconfiguration.

**Flush mechanism** — Uses Cobra's `OnFinalize` callback rather than `PersistentPostRunE` to ensure flush always runs regardless of whether subcommands define their own `PostRunE` hooks.

**Buffer overflow** — In-memory buffer capped at 1000 events. Overflow spills to disk (config dir if available, `/tmp` fallback) with 1MB per-file and 10-file caps. Every `Flush` checks for spill files first.

**Delivery guarantee** — Configurable via `TelemetryConfig.DeliveryMode`. At-least-once (default) deletes spill files after successful send. At-most-once deletes before send.

**Consent withdrawal** — `telemetry disable` immediately drops all buffered events and deletes spill files. `OnFinalize` re-checks the enabled state and no-ops if disabled mid-session. No events are sent after an explicit disable.

**GDPR data deletion** — `telemetry reset` clears local data and sends a deletion request via a pluggable `DeletionRequestor` interface. Built-in requestors: HTTP (POST to endpoint), email (mailto: link), event (sends `data.deletion_request` through existing backend). Falls back to event-based requestor if none configured.

**HTTP backend error visibility** — Non-2xx responses are logged at debug level via a logger passed to `NewHTTPBackend`. Network errors are silently dropped (by design). OTel SDK errors are routed to the GTB logger via a custom error handler.

**`--skip-telemetry` flag isolation** — The flag is bound to a field on the `TelemetryInitialiser` struct rather than a package-level variable, ensuring test isolation when multiple init commands run in the same process.

**GTB telemetry collection service** — Grafana Cloud (free tier) selected as the GTB telemetry backend. It provides 50 GB/month log ingestion with 14-day retention, native OTLP/HTTP support, and built-in dashboarding. The GTB binary uses the existing OTLP backend with Grafana Cloud's OTLP gateway endpoint and Basic auth credentials injected via environment variables at build time. No custom backend is needed for this use case. Additional vendor-specific backends (Datadog, PostHog) are specified separately in the [Telemetry Vendor Backends spec](2026-03-30-telemetry-vendor-backends.md) for tool authors who use those platforms.

---

## Future Considerations

- **Consent prompt on first run**: On first command invocation (not just `init`), if `TelemetryCmd` is enabled and `telemetry.enabled` is unset, show a one-time prompt rather than silently skipping.
- **Event sampling**: For high-volume tools, sample events (e.g., 10%) to reduce data volume.
- **Usage dashboards**: Companion service for aggregating and visualising telemetry across tool versions.
- **Buffer compaction**: Roll up duplicate events (same `EventType + Name`) into a single event with a `count` metadata field to reduce buffer and spill file pressure.

---

## Implementation Phases

### Phase 1 — Core types, backends, and infrastructure
1. Create `pkg/props/telemetry.go` — `TelemetryCollector` interface, `EventType` + constants, `TelemetryConfig` (with `Endpoint`, `OTelEndpoint`, `OTelHeaders`, `OTelInsecure`, `Backend func(*Props) any`, `DeletionRequestor func(*Props) any`, `DeliveryMode`, `Metadata`), `DeliveryMode` type + constants, `TelemetryCmd` feature flag
2. Add `Telemetry TelemetryConfig` to `Tool` in `pkg/props/tool.go`
3. Add `Collector TelemetryCollector` field to `Props` in `pkg/props/props.go`
4. Create `pkg/telemetry/` — `Backend` interface, `DeletionRequestor` interface, `Event`, `Collector` (implements `props.TelemetryCollector` including `Drop()`), `Config`, `EventType` constants (mirrored from `pkg/props`), noop/stdout/file/HTTP backends
5. Create `pkg/telemetry/backend_otel.go` — `NewOTelBackend`, `OTelOption`, `WithOTelHeaders`, `WithOTelInsecure`, `WithOTelLogger`, custom OTel error handler
6. Create `pkg/telemetry/deletion.go` — `HTTPDeletionRequestor`, `EmailDeletionRequestor`, `EventDeletionRequestor`
7. Create `pkg/telemetry/machine.go` — `HashedMachineID` (exported), `osMachineID`, `firstMACAddress`
8. Create `pkg/telemetry/datadir.go` — `ResolveDataDir` helper
9. Create `pkg/telemetry/spill.go` — buffer spill, flush spill files, prune, delete (respects `DeliveryMode`)
10. Add OTel dependencies to `go.mod`: `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp`, `go.opentelemetry.io/otel/sdk/log`
11. Interface satisfaction guard in `pkg/cmd/root`: `var _ props.TelemetryCollector = (*telemetry.Collector)(nil)`
12. Tests including concurrency, PII checks, OTel attribute verification, EventType sync, spill/delivery mode, deletion requestors, machine ID multi-signal, ResolveDataDir

### Phase 2 — Root command wiring
1. `buildTelemetryCollector(ctx context.Context, p *props.Props)` in `pkg/cmd/root/root.go`
2. Populate `props.Collector` in `PersistentPreRunE`
3. Register `OnFinalize` callback for flush (with enabled-state re-check)

### Phase 3 — Init integration
1. Create `pkg/setup/telemetry/` — `TelemetryInitialiser` (with `Name()`, `skipTelemetry` on struct), `init()` registration
2. `IsConfigured` (key or `TELEMETRY_ENABLED` env), `Configure` (env bypass + huh form)
3. Tests with injected forms, test isolation for `skipTelemetry`

### Phase 4 — CLI commands
1. Create `pkg/cmd/telemetry/` — `enable`, `disable` (with `Drop()`), `status`, `reset` (with `DeletionRequestor`)
2. Wire `telemetry` command into root via `setup.AddCommandWithMiddleware`
3. E2E Godog scenarios

### Phase 5 — GTB binary + manifest + generator scaffolding
1. Update `internal/cmd/root/root.go`: enable `TelemetryCmd`, set `Telemetry.OTelEndpoint` and `OTelHeaders` for Grafana Cloud (credentials via env vars / ldflags)
2. Update `internal/generator/manifest.go`: add `ManifestTelemetry` (with `endpoint` and `otel_endpoint`), `ManifestProperties.Telemetry`
3. Update `internal/generator/skeleton.go`: add `TelemetryEndpoint` and `TelemetryOTelEndpoint` to `SkeletonConfig`; add `"telemetry"` to `calculateEnabledFeatures`
4. Update `internal/generator/regenerate.go`: populate telemetry fields from manifest
5. Update `internal/generator/templates/skeleton_root.go`: add telemetry fields to `SkeletonRootData`; update `buildToolDict` (with OTLP precedence) and `getFeatureCmd`
6. Add `--telemetry-endpoint` and `--telemetry-otel-endpoint` CLI flags to `generate project`
7. Add telemetry configuration to the interactive project generation form
8. Tests: `TestCalculateEnabledFeatures_Telemetry`, `TestSkeletonRoot_TelemetryEndpoint`, `TestSkeletonRoot_TelemetryOTelEndpoint`, `TestSkeletonRoot_NoTelemetryEndpoint`

---

## Verification

```bash
go build ./...
go test -race ./pkg/telemetry/... ./pkg/setup/telemetry/... ./pkg/cmd/telemetry/...
go test ./...
golangci-lint run

# Manual
go run . telemetry status             # "disabled"
go run . telemetry enable             # enables, writes config
go run . telemetry status             # "enabled"
go run . telemetry disable            # disables, drops pending events
go run . telemetry reset              # clears data, sends deletion request, disables
TELEMETRY_ENABLED=true go run . init  # non-interactive consent
cat ~/.toolname/config.yaml | grep telemetry  # verify written
cat ~/.toolname/telemetry.log | jq .          # local-only events

# Generate a new tool with telemetry endpoint — verify generated root.go contains TelemetryConfig
go run . generate project --name myapp --repo org/myapp \
  --telemetry-endpoint https://telemetry.example.com/events

# Generate with OTLP endpoint
go run . generate project --name myapp --repo org/myapp \
  --telemetry-otel-endpoint https://collector.example.com:4318
```
