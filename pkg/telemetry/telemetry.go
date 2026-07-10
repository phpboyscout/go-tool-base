// Package telemetry provides an opt-in telemetry framework with pluggable
// backends, privacy controls, and bounded buffering with disk spill.
package telemetry

import (
	"context"
	"log/slog"
	"maps"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"gitlab.com/phpboyscout/go-tool-base/pkg/osinfo"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/redact"
)

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
	Timestamp  time.Time         `json:"timestamp"`
	Type       EventType         `json:"type"`
	Name       string            `json:"name"`
	MachineID  string            `json:"machine_id"`
	ToolName   string            `json:"tool_name"`
	Version    string            `json:"version"`
	OS         string            `json:"os"`
	Arch       string            `json:"arch"`
	GoVersion  string            `json:"go_version"`
	OSVersion  string            `json:"os_version"`
	DurationMs int64             `json:"duration_ms,omitempty"`
	ExitCode   int               `json:"exit_code,omitempty"`
	Args       []string          `json:"args,omitempty"`  // only populated when ExtendedCollection is enabled
	Error      string            `json:"error,omitempty"` // only populated when ExtendedCollection is enabled
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Config holds runtime telemetry configuration read from the user's config file.
// Endpoints are not included — they are tool-author concerns set in
// props.TelemetryConfig, not user-configurable.
type Config struct {
	Enabled   bool `yaml:"enabled"`
	LocalOnly bool `yaml:"local_only"`
}

const defaultMaxBuffer = 1000

// Collector accumulates events and flushes to the backend.
// All methods are safe for concurrent use. When disabled, all operations
// are no-ops — callers do not need to check whether telemetry is enabled.
type Collector struct {
	backend            Backend
	config             Config
	toolName           string
	version            string
	machineID          string
	goVersion          string
	osVersion          string
	extendedCollection bool
	// backendInfo holds the human-readable backend description. It is set by
	// the root command after backend selection and read by the doctor/status
	// commands — a documented cross-goroutine handoff — so it is guarded by an
	// atomic.Value rather than the buffer mutex. Stored value is always string.
	backendInfo  atomic.Value
	metadata     map[string]string
	buffer       []Event
	mu           sync.Mutex
	log          *slog.Logger
	dataDir      string
	deliveryMode props.DeliveryMode
	maxBuffer    int
}

// NewCollector creates a Collector. When cfg.Enabled is false, returns a noop
// collector so callers never need to nil-check.
func NewCollector(cfg Config, backend Backend, toolName, version string, metadata map[string]string, log *slog.Logger, dataDir string, deliveryMode props.DeliveryMode, extendedCollection bool) *Collector {
	if !cfg.Enabled {
		c := &Collector{backend: NewNoopBackend(), log: log, maxBuffer: defaultMaxBuffer}
		c.SetBackendInfo("noop (disabled)")

		return c
	}

	if deliveryMode == "" {
		deliveryMode = props.DeliveryAtLeastOnce
	}

	return &Collector{
		backend:            backend,
		config:             cfg,
		toolName:           toolName,
		version:            version,
		machineID:          HashedMachineID(),
		goVersion:          runtime.Version(),
		osVersion:          osinfo.Version(),
		extendedCollection: extendedCollection,
		metadata:           metadata,
		log:                log,
		dataDir:            dataDir,
		deliveryMode:       deliveryMode,
		maxBuffer:          defaultMaxBuffer,
	}
}

// Enabled reports whether this collector is actively collecting, as opposed to
// a no-op disabled collector. It reflects the resolved decision (config, env
// override, or tool-author ForceEnabled), so callers should gate a flush on
// this rather than re-reading the raw config key.
func (c *Collector) Enabled() bool {
	return c.config.Enabled
}

// Track records a telemetry event. No-op when collector is disabled.
// When the in-memory buffer reaches maxBuffer, events are spilled to disk.
// mergeMetadata combines collector metadata with per-event extras and redacts
// every value at the ingest boundary. Metadata is operator-supplied free-form
// text (endpoints, identifiers) and is the last place to catch a stray
// credential before it ships, mirroring the args/errMsg redaction.
func (c *Collector) mergeMetadata(extra map[string]string) map[string]string {
	merged := make(map[string]string, len(c.metadata)+len(extra))
	maps.Copy(merged, c.metadata)
	maps.Copy(merged, extra)

	for k, v := range merged {
		merged[k] = redact.String(v)
	}

	return merged
}

// record is the single buffering path shared by every Track* method.
// The caller supplies the event-type-specific fields (Type, Name, the
// optional Duration/ExitCode/Args/Error) on evt; record stamps the
// common envelope (timestamp, machine/tool identity, runtime info),
// merges and redacts metadata, appends under the buffer lock, and
// spills to disk when the buffer is full. Centralising this is what
// keeps the spill behaviour identical across the Track methods — the
// earlier per-method divergence is exactly how the spill inconsistency
// survived.
//
// evt.Args / evt.Error are expected to already be redacted by the
// caller (TrackCommandExtended) since their inclusion is gated on
// extendedCollection; record redacts only the merged metadata.
func (c *Collector) record(evt Event, extra map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.config.Enabled || c.backend == nil {
		return
	}

	evt.Timestamp = time.Now().UTC()
	evt.MachineID = c.machineID
	evt.ToolName = c.toolName
	evt.Version = c.version
	evt.OS = runtime.GOOS
	evt.Arch = runtime.GOARCH
	evt.GoVersion = c.goVersion
	evt.OSVersion = c.osVersion
	evt.Metadata = c.mergeMetadata(extra)

	c.buffer = append(c.buffer, evt)

	if len(c.buffer) >= c.maxBuffer {
		c.spillToDisk()
	}
}

func (c *Collector) Track(eventType props.EventType, name string, extra map[string]string) {
	c.record(Event{
		Type: EventType(eventType),
		Name: name,
	}, extra)
}

// TrackCommand records a command invocation event with duration and exit code.
// This is a convenience wrapper around Track for command lifecycle events.
func (c *Collector) TrackCommand(name string, durationMs int64, exitCode int, extra map[string]string) {
	c.record(Event{
		Type:       EventType(props.EventCommandInvocation),
		Name:       name,
		DurationMs: durationMs,
		ExitCode:   exitCode,
	}, extra)
}

// TrackCommandExtended records a command invocation with full context.
// When ExtendedCollection is disabled on the collector, args and errMsg are
// silently dropped — callers do not need to check the flag themselves.
func (c *Collector) TrackCommandExtended(name string, args []string, durationMs int64, exitCode int, errMsg string, extra map[string]string) {
	event := Event{
		Type:       EventType(props.EventCommandInvocation),
		Name:       name,
		DurationMs: durationMs,
		ExitCode:   exitCode,
	}

	// Only include args and error when extended collection is explicitly enabled.
	// Both fields are redacted unconditionally before shipping: callers forget,
	// and the ingest boundary is the last chance to strip credentials. Closes
	// M-5 from docs/development/reports/security-audit-2026-04-17.md.
	if c.extendedCollection {
		redactedArgs := make([]string, len(args))
		for i, a := range args {
			redactedArgs[i] = redact.String(a)
		}

		event.Args = redactedArgs
		event.Error = redact.String(errMsg)
	}

	c.record(event, extra)
}

// Flush sends all buffered events to the backend, then clears the buffer.
// Checks for and sends spill files before flushing the current buffer.
func (c *Collector) Flush(ctx context.Context) error {
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
		// Under at-least-once, a failed send must not lose the batch: spill it
		// back to disk so the next flush retries. Without this re-spill the
		// buffer was already cleared above, defeating the no-data-loss
		// guarantee whenever a backend surfaces a transport error.
		if c.deliveryMode == props.DeliveryAtLeastOnce {
			c.mu.Lock()
			c.buffer = append(c.buffer, events...)
			c.spillToDisk()
			c.mu.Unlock()

			c.log.Warn("telemetry flush failed; batch spilled for retry",
				"error", err, "events_spilled", len(events))
		} else {
			c.log.Warn("telemetry flush failed", "error", err, "events_dropped", len(events))
		}

		return err
	}

	c.log.Debug("telemetry flushed", "events", len(events))

	return nil
}

// Close flushes pending events and shuts down the backend gracefully.
// Must be called on process exit to ensure backends like OTLP flush
// their internal batch queues.
func (c *Collector) Close(ctx context.Context) error {
	if err := c.Flush(ctx); err != nil {
		c.log.Debug("flush during close failed", "error", err)
	}

	return c.backend.Close()
}

// BackendInfo returns a human-readable description of the active backend.
// Safe for concurrent use: the value is read atomically, honouring the
// documented cross-goroutine handoff with SetBackendInfo.
func (c *Collector) BackendInfo() string {
	if v, ok := c.backendInfo.Load().(string); ok {
		return v
	}

	return ""
}

// SetBackendInfo sets the human-readable backend description.
// Called by the root command after backend selection. Safe for concurrent use.
func (c *Collector) SetBackendInfo(info string) {
	c.backendInfo.Store(info)
}

// Drop clears all buffered events and deletes any spill files without sending.
// Called when the user disables telemetry to ensure immediate consent withdrawal.
func (c *Collector) Drop() error {
	c.mu.Lock()
	c.buffer = c.buffer[:0]
	c.mu.Unlock()

	return c.deleteSpillFiles()
}
