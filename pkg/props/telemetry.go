package props

import "context"

// TelemetryCmd is the feature flag for the telemetry command group.
// Default disabled — tool authors must explicitly enable it.
const TelemetryCmd = FeatureCmd("telemetry")

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

// TelemetryCollector is the interface through which commands emit telemetry events.
// Defined here (not in pkg/telemetry) to avoid an import cycle.
// The concrete implementation is *telemetry.Collector from pkg/telemetry.
// Props.Collector is always non-nil — when telemetry is disabled it is a noop.
type TelemetryCollector interface {
	// Track records a telemetry event. No-op when the collector is disabled.
	Track(eventType EventType, name string, extra map[string]string)
	// TrackCommand records a command invocation with duration and exit code.
	TrackCommand(name string, durationMs int64, exitCode int, extra map[string]string)
	// TrackCommandExtended records a command invocation with full context including
	// arguments and error message. When ExtendedCollection is disabled on the
	// collector, args and errMsg are silently dropped — callers do not need to
	// check the flag themselves.
	TrackCommandExtended(name string, args []string, durationMs int64, exitCode int, errMsg string, extra map[string]string)
	// Flush sends all buffered events to the backend and clears the buffer.
	// Checks for and sends spill files before flushing the current buffer.
	Flush(ctx context.Context) error
	// Drop clears all buffered events and deletes any spill files without sending.
	Drop() error
}

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

	// ExtendedCollection enables collection of command arguments and error messages
	// in telemetry events. Default: false. When false, args and errors are never
	// recorded regardless of what callers pass to TrackCommandExtended.
	//
	// Enable this only in closed enterprise environments where users are
	// contractually bound by security policies. In public-facing tools, leave
	// this disabled to preserve user privacy.
	ExtendedCollection bool `json:"extended_collection,omitempty" yaml:"extended_collection,omitempty"`

	// DeliveryMode controls the delivery guarantee. Default: DeliveryAtLeastOnce.
	DeliveryMode DeliveryMode `json:"delivery_mode,omitempty" yaml:"delivery_mode,omitempty"`

	// Metadata is additional key/value pairs included in every event.
	// Useful for custom dimensions like environment name or deployment tier.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}
