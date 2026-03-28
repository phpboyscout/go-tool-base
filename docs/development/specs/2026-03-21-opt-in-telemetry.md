---
title: "Opt-in Telemetry Specification"
description: "Opt-in usage analytics framework with pluggable backends, privacy controls, and CLI management commands."
date: 2026-03-21
status: DRAFT
tags:
  - specification
  - telemetry
  - analytics
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Opt-in Telemetry Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Overview

Understanding how GTB-based tools are used helps maintainers prioritise features, identify common errors, and measure adoption. However, telemetry must be explicitly opt-in, privacy-preserving, and transparent.

This spec defines an opt-in telemetry framework with:

- Explicit opt-in via CLI command or config key (never enabled by default)
- Pluggable backend interface (stdout for debugging, HTTP for production, OpenTelemetry for enterprise)
- Defined event types for command invocations, errors, and feature usage
- Privacy controls including data anonymisation and local-only mode
- CLI management commands (`telemetry enable`, `telemetry disable`, `telemetry status`)
- Integration with the unified logger abstraction ([2026-03-23-unified-logger-abstraction](2026-03-23-unified-logger-abstraction.md)) for diagnostic output

---

## Design Decisions

**Opt-in, not opt-out**: Telemetry is disabled by default and requires explicit user action to enable. This is a firm ethical and legal requirement (GDPR, user trust). The feature flag `TelemetryCmd` must be enabled by the tool author, AND the user must opt in.

**Two-level gating**: Tool authors enable/disable telemetry availability via the `TelemetryCmd` feature flag. End users control their participation via `telemetry enable/disable`. Both must be active for data to be collected.

**Pluggable backends**: The `Backend` interface allows tool authors to choose their analytics platform. A no-op backend is always available. The framework provides stdout and HTTP backends; OpenTelemetry is a documented extension point.

**Anonymisation by default**: No personally identifiable information (PII) is collected. Machine IDs are hashed. IP addresses are not stored. Command arguments are not recorded (only command names).

**Local-only mode**: Users can enable telemetry in local-only mode where events are written to a file but never transmitted. Useful for tool authors debugging their own usage patterns.

**Unified logger integration**: The `Collector` and CLI commands use `logger.Logger` from `pkg/logger` (see [unified logger abstraction spec](2026-03-23-unified-logger-abstraction.md)) for diagnostic output. This ensures telemetry warnings and errors flow through the same logging pipeline as the rest of the application, including any structured logging or OTEL export configured by the user.

---

## Public API Changes

### New Feature Flag

```go
// In pkg/props/tool.go
const TelemetryCmd FeatureCmd = "telemetry"
```

### New Package: `pkg/telemetry`

```go
// Event represents a single telemetry event.
type Event struct {
    Timestamp time.Time         `json:"timestamp"`
    Type      EventType         `json:"type"`
    Name      string            `json:"name"`
    MachineID string            `json:"machine_id"` // hashed, not raw
    ToolName  string            `json:"tool_name"`
    Version   string            `json:"version"`
    OS        string            `json:"os"`
    Arch      string            `json:"arch"`
    Metadata  map[string]string `json:"metadata,omitempty"`
}

// EventType identifies the category of telemetry event.
type EventType string

const (
    EventCommandInvocation EventType = "command.invocation"
    EventCommandError      EventType = "command.error"
    EventFeatureUsed       EventType = "feature.used"
    EventUpdateCheck       EventType = "update.check"
    EventUpdateApplied     EventType = "update.applied"
)

// Backend is the interface for telemetry data sinks.
type Backend interface {
    // Send transmits a batch of events. Implementations should be
    // non-blocking or have short timeouts to avoid impacting CLI performance.
    Send(ctx context.Context, events []Event) error

    // Close flushes any buffered events and releases resources.
    Close() error
}

// Config holds telemetry configuration.
type Config struct {
    Enabled   bool   `json:"enabled" yaml:"enabled"`
    LocalOnly bool   `json:"local_only" yaml:"local_only"`
    Endpoint  string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// Collector accumulates events and periodically flushes to the backend.
type Collector struct {
    backend   Backend
    config    Config
    buffer    []Event
    mu        sync.Mutex
    machineID string
    log       logger.Logger
}
```

---

## Internal Implementation

### Collector

```go
// NewCollector creates a telemetry collector. If telemetry is disabled,
// returns a no-op collector that discards all events. The logger is used
// for diagnostic output (flush failures, backend errors).
func NewCollector(cfg Config, backend Backend, log logger.Logger) *Collector {
    if !cfg.Enabled {
        return &Collector{backend: &noopBackend{}, config: cfg, log: log}
    }

    return &Collector{
        backend:   backend,
        config:    cfg,
        machineID: hashedMachineID(),
        log:       log,
    }
}

// Track records a telemetry event. This method is safe for concurrent use.
func (c *Collector) Track(eventType EventType, name string, metadata map[string]string) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.buffer = append(c.buffer, Event{
        Timestamp: time.Now().UTC(),
        Type:      eventType,
        Name:      name,
        MachineID: c.machineID,
        OS:        runtime.GOOS,
        Arch:      runtime.GOARCH,
        Metadata:  metadata,
    })
}

// Flush sends all buffered events to the backend.
func (c *Collector) Flush(ctx context.Context) error {
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
```

### Machine ID Hashing

```go
func hashedMachineID() string {
    // Use hostname + user as a stable machine identifier
    hostname, _ := os.Hostname()
    user, _ := user.Current()
    raw := hostname + ":" + user.Username

    h := sha256.Sum256([]byte(raw))
    return hex.EncodeToString(h[:8]) // first 8 bytes = 16 hex chars
}
```

### Built-in Backends

#### No-Op Backend

```go
type noopBackend struct{}

func (n *noopBackend) Send(ctx context.Context, events []Event) error { return nil }
func (n *noopBackend) Close() error                                    { return nil }
```

#### Stdout Backend (Debugging)

```go
type stdoutBackend struct {
    w io.Writer
}

func NewStdoutBackend(w io.Writer) Backend {
    return &stdoutBackend{w: w}
}

func (s *stdoutBackend) Send(ctx context.Context, events []Event) error {
    enc := json.NewEncoder(s.w)
    enc.SetIndent("", "  ")
    return enc.Encode(events)
}

func (s *stdoutBackend) Close() error { return nil }
```

#### File Backend (Local-Only Mode)

```go
type fileBackend struct {
    path string
    mu   sync.Mutex
}

func NewFileBackend(path string) Backend {
    return &fileBackend{path: path}
}

func (f *fileBackend) Send(ctx context.Context, events []Event) error {
    f.mu.Lock()
    defer f.mu.Unlock()

    file, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

#### HTTP Backend

```go
type httpBackend struct {
    endpoint string
    client   *http.Client
}

func NewHTTPBackend(endpoint string) Backend {
    return &httpBackend{
        endpoint: endpoint,
        client:   http.NewClient(http.WithTimeout(5 * time.Second)),
    }
}

func (h *httpBackend) Send(ctx context.Context, events []Event) error {
    body, err := json.Marshal(events)
    if err != nil {
        return errors.Wrap(err, "marshalling telemetry events")
    }

    req, err := http.NewRequestWithContext(ctx, "POST", h.endpoint, bytes.NewReader(body))
    if err != nil {
        return errors.Wrap(err, "creating telemetry request")
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := h.client.Do(req)
    if err != nil {
        return nil // silently drop — telemetry should never block the user
    }
    defer resp.Body.Close()

    return nil
}

func (h *httpBackend) Close() error { return nil }
```

### CLI Commands

```go
// pkg/cmd/telemetry/telemetry.go

func NewCmdTelemetry(props *p.Props) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "telemetry",
        Short: "Manage usage telemetry",
    }

    cmd.AddCommand(
        newStatusCmd(props),
        newEnableCmd(props),
        newDisableCmd(props),
    )

    return cmd
}

func newEnableCmd(props *p.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "enable",
        Short: "Enable anonymous usage telemetry",
        RunE: func(cmd *cobra.Command, args []string) error {
            props.Config.Set("telemetry.enabled", true)
            props.Logger.Print("Telemetry enabled. Thank you for helping improve " + props.Tool.Name + "!")
            props.Logger.Print("No personally identifiable information is collected.")
            return nil
        },
    }
}

func newDisableCmd(props *p.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "disable",
        Short: "Disable usage telemetry",
        RunE: func(cmd *cobra.Command, args []string) error {
            props.Config.Set("telemetry.enabled", false)
            props.Logger.Print("Telemetry disabled.")
            return nil
        },
    }
}

func newStatusCmd(props *p.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "status",
        Short: "Show telemetry status",
        RunE: func(cmd *cobra.Command, args []string) error {
            enabled := props.Config.GetBool("telemetry.enabled")
            localOnly := props.Config.GetBool("telemetry.local_only")

            if !enabled {
                props.Logger.Print("Telemetry: disabled")
            } else if localOnly {
                props.Logger.Print("Telemetry: enabled (local-only)")
            } else {
                props.Logger.Print("Telemetry: enabled")
            }

            props.Logger.Print("Machine ID: " + hashedMachineID())
            return nil
        },
    }
}
```

### Integration with Root Command

```go
// In PersistentPreRunE, after config and logger are initialised:
func setupTelemetry(props *p.Props) *telemetry.Collector {
    if props.Tool.IsDisabled(p.TelemetryCmd) {
        return telemetry.NewCollector(telemetry.Config{}, nil, props.Logger)
    }

    cfg := telemetry.Config{
        Enabled:   props.Config.GetBool("telemetry.enabled"),
        LocalOnly: props.Config.GetBool("telemetry.local_only"),
        Endpoint:  props.Config.GetString("telemetry.endpoint"),
    }

    var backend telemetry.Backend
    switch {
    case !cfg.Enabled:
        backend = nil // no-op
    case cfg.LocalOnly:
        backend = telemetry.NewFileBackend(filepath.Join(configDir, "telemetry.log"))
    case cfg.Endpoint != "":
        backend = telemetry.NewHTTPBackend(cfg.Endpoint)
    default:
        backend = nil // no endpoint configured
    }

    return telemetry.NewCollector(cfg, backend, props.Logger)
}

// In PersistentPostRunE:
func flushTelemetry(collector *telemetry.Collector) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    _ = collector.Flush(ctx)
}
```

---

## Project Structure

```
pkg/telemetry/
├── telemetry.go       ← NEW: Event, Collector, Config types (imports pkg/logger)
├── backend.go         ← NEW: Backend interface, noop/stdout/file/http implementations
├── machine.go         ← NEW: hashed machine ID
├── telemetry_test.go  ← NEW: collector tests
├── backend_test.go    ← NEW: backend tests
pkg/cmd/telemetry/
├── telemetry.go       ← NEW: enable/disable/status commands (uses props.Logger)
├── telemetry_test.go  ← NEW: command tests
pkg/props/
├── tool.go            ← MODIFIED: add TelemetryCmd feature flag
pkg/cmd/root/
├── root.go            ← MODIFIED: telemetry setup and flush
```

**Dependency**: This spec depends on [2026-03-23-unified-logger-abstraction](2026-03-23-unified-logger-abstraction.md). The `Collector` accepts `logger.Logger` and CLI commands use `props.Logger.Print()`. If logger abstraction is not yet implemented, use `charmbracelet/log` directly as an interim measure.

---

## Testing Strategy

| Test | Scenario |
|------|----------|
| `TestCollector_Disabled` | Disabled config → events silently discarded |
| `TestCollector_Track` | Track events → buffered correctly |
| `TestCollector_Flush` | Flush → events sent to backend, buffer cleared |
| `TestCollector_FlushEmpty` | Flush with no events → no backend call |
| `TestCollector_ConcurrentTrack` | 100 goroutines tracking → no race |
| `TestNoopBackend` | Send → returns nil, no side effects |
| `TestStdoutBackend` | Send → JSON written to writer |
| `TestFileBackend` | Send → events appended to file |
| `TestHTTPBackend_Success` | Mock server → events posted |
| `TestHTTPBackend_Timeout` | Slow server → no error (silently drops) |
| `TestCollector_FlushError_LogsWarning` | Backend error → warning logged via logger |
| `TestCollector_FlushSuccess_LogsDebug` | Successful flush → debug message logged |
| `TestHashedMachineID` | Same machine → same hash |
| `TestHashedMachineID_NotRaw` | Hash does not contain hostname |
| `TestEnableCmd` | Enable → config updated |
| `TestDisableCmd` | Disable → config updated |
| `TestStatusCmd_Disabled` | Disabled → shows "disabled" |
| `TestStatusCmd_Enabled` | Enabled → shows "enabled" |
| `TestEvent_NoArguments` | Command args are not included in events |

### Privacy Test

```go
func TestEvent_NoPII(t *testing.T) {
    collector := NewCollector(Config{Enabled: true}, &testBackend{})
    collector.Track(EventCommandInvocation, "generate", map[string]string{
        "subcommand": "docs",
    })

    events := collector.buffer
    assert.Len(t, events, 1)

    // Verify no PII
    eventJSON, _ := json.Marshal(events[0])
    content := string(eventJSON)
    hostname, _ := os.Hostname()
    assert.NotContains(t, content, hostname)
}
```

### Integration Tests

- **Collector → backend round-trip**: Create a `Collector` with a real `FileBackend`, track events, flush, then read the output file and verify JSON structure and content.
- **HTTP backend delivery**: Start an `httptest.Server`, configure `HTTPBackend` with its URL, flush events, and assert the server received the expected payload.
- **Config toggle round-trip**: Enable telemetry via `viper.Set`, create a `Collector` from config, track an event, disable via config, verify flush is a no-op.
- Gate with `testutil.SkipIfNotIntegration(t, "telemetry")` in a dedicated `telemetry_integration_test.go` file.

### E2E BDD Tests (Godog) — **Moderate fit**

The `telemetry enable/disable/status` subcommands are user-facing CLI operations with clear Given/When/Then semantics. Feature file: `features/cli/telemetry.feature`.

```gherkin
@cli @integration
Feature: CLI Telemetry Command
  Users can opt in or out of anonymous usage telemetry
  and check their current telemetry status.

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
    Then the exit code is 0
    When I run gtb with "telemetry disable"
    Then the exit code is 0
    When I run gtb with "telemetry status"
    Then the exit code is 0
    And stdout contains "disabled"

  Scenario: Telemetry help text
    When I run gtb with "telemetry --help"
    Then the exit code is 0
    And stdout contains "telemetry"
```

The data collection and backend pipeline are **not** testable via Godog — verify these through unit tests and the integration tests above.

### Coverage
- Target: 95%+ for `pkg/telemetry/` (privacy-sensitive code requires thorough testing).
- Target: 90%+ for `pkg/cmd/telemetry/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.
- The `gosec` linter should pass — no sensitive data handling concerns.

---

## Documentation

- Godoc for all exported types in `pkg/telemetry/`.
- User-facing documentation in `docs/components/telemetry.md`:
  - What data is collected (exact event types and fields)
  - What data is NOT collected (no PII, no arguments, no file contents)
  - How to enable/disable
  - Local-only mode explanation
  - How to inspect collected data
- Privacy policy template for tool authors.
- Update `docs/components/features.md` with `TelemetryCmd` feature flag.

---

## Backwards Compatibility

- **No breaking changes**. Telemetry is entirely additive and disabled by default.
- Tools that don't enable the `TelemetryCmd` feature flag see no changes.
- No existing configuration keys are modified.

---

## Future Considerations

- **OpenTelemetry backend**: For enterprise users who already have OTel infrastructure, a `telemetry.NewOTelBackend()` would integrate directly with their tracing/metrics pipeline. The unified logger abstraction's `slog.Handler` bridge (see [logger spec](2026-03-23-unified-logger-abstraction.md)) enables routing structured log events to the same OTEL collector, providing a single pipeline for both telemetry events and application logs.
- **Usage dashboards**: A companion web service that aggregates telemetry data and provides visualisations for tool authors.
- **Consent prompt**: On first run after tool update, prompt the user to opt in rather than requiring a manual `telemetry enable` command.
- **Event sampling**: For high-volume tools, sample events (e.g., 10%) to reduce data volume while maintaining statistical significance.

---

## Implementation Phases

### Phase 1 — Core Framework
1. Create `pkg/telemetry/` with Event, Collector, Config
2. Implement no-op and stdout backends
3. Add tests including concurrency and privacy

### Phase 2 — Backends
1. Implement file backend for local-only mode
2. Implement HTTP backend with timeout
3. Add backend tests with mock servers

### Phase 3 — CLI Commands
1. Create `telemetry enable/disable/status` commands
2. Add `TelemetryCmd` feature flag
3. Wire into root command

### Phase 4 — Integration
1. Set up collector in root command pre-run
2. Track command invocations in post-run
3. Flush on exit with timeout
4. Add integration tests

---

## Verification

```bash
go build ./...
go test -race ./pkg/telemetry/... ./pkg/cmd/telemetry/...
go test ./...
golangci-lint run --fix

# Manual verification
go run . telemetry status     # should show "disabled"
go run . telemetry enable     # should enable
go run . telemetry status     # should show "enabled"
go run . generate docs        # should track event (if telemetry enabled)
go run . telemetry disable    # should disable

# Verify no PII in events
cat ~/.toolname/telemetry.log | jq .  # inspect local-only events
```
