// Package telemetry provides an opt-in telemetry framework with pluggable
// backends, privacy controls, and bounded buffering with disk spill.
package telemetry

import (
	"context"
	"maps"
	"runtime"
	"sync"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
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
	metadata           map[string]string
	buffer             []Event
	mu                 sync.Mutex
	log                logger.Logger
	dataDir            string
	deliveryMode       props.DeliveryMode
	maxBuffer          int
}

// NewCollector creates a Collector. When cfg.Enabled is false, returns a noop
// collector so callers never need to nil-check.
func NewCollector(cfg Config, backend Backend, toolName, version string, metadata map[string]string, log logger.Logger, dataDir string, deliveryMode props.DeliveryMode, extendedCollection bool) *Collector {
	if !cfg.Enabled {
		return &Collector{backend: NewNoopBackend(), log: log, maxBuffer: defaultMaxBuffer}
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
		osVersion:          osVersion(),
		extendedCollection: extendedCollection,
		metadata:           metadata,
		log:                log,
		dataDir:            dataDir,
		deliveryMode:       deliveryMode,
		maxBuffer:          defaultMaxBuffer,
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
	maps.Copy(merged, c.metadata)
	maps.Copy(merged, extra)

	c.buffer = append(c.buffer, Event{
		Timestamp: time.Now().UTC(),
		Type:      EventType(eventType),
		Name:      name,
		MachineID: c.machineID,
		ToolName:  c.toolName,
		Version:   c.version,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: c.goVersion,
		OSVersion: c.osVersion,
		Metadata:  merged,
	})

	if len(c.buffer) >= c.maxBuffer {
		c.spillToDisk()
	}
}

// TrackCommand records a command invocation event with duration and exit code.
// This is a convenience wrapper around Track for command lifecycle events.
func (c *Collector) TrackCommand(name string, durationMs int64, exitCode int, extra map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.backend == nil {
		return
	}

	merged := make(map[string]string, len(c.metadata)+len(extra))
	maps.Copy(merged, c.metadata)
	maps.Copy(merged, extra)

	c.buffer = append(c.buffer, Event{
		Timestamp:  time.Now().UTC(),
		Type:       EventType(props.EventCommandInvocation),
		Name:       name,
		MachineID:  c.machineID,
		ToolName:   c.toolName,
		Version:    c.version,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		GoVersion:  c.goVersion,
		OSVersion:  c.osVersion,
		DurationMs: durationMs,
		ExitCode:   exitCode,
		Metadata:   merged,
	})

	if len(c.buffer) >= c.maxBuffer {
		c.spillToDisk()
	}
}

// TrackCommandExtended records a command invocation with full context.
// When ExtendedCollection is disabled on the collector, args and errMsg are
// silently dropped — callers do not need to check the flag themselves.
func (c *Collector) TrackCommandExtended(name string, args []string, durationMs int64, exitCode int, errMsg string, extra map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.backend == nil {
		return
	}

	merged := make(map[string]string, len(c.metadata)+len(extra))
	maps.Copy(merged, c.metadata)
	maps.Copy(merged, extra)

	event := Event{
		Timestamp:  time.Now().UTC(),
		Type:       EventType(props.EventCommandInvocation),
		Name:       name,
		MachineID:  c.machineID,
		ToolName:   c.toolName,
		Version:    c.version,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		GoVersion:  c.goVersion,
		OSVersion:  c.osVersion,
		DurationMs: durationMs,
		ExitCode:   exitCode,
		Metadata:   merged,
	}

	// Only include args and error when extended collection is explicitly enabled
	if c.extendedCollection {
		event.Args = args
		event.Error = errMsg
	}

	c.buffer = append(c.buffer, event)

	if len(c.buffer) >= c.maxBuffer {
		c.spillToDisk()
	}
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
