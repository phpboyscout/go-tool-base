// Package telemetrytypes holds the telemetry value types shared between GTB's
// dependency-injection container (pkg/props) and the collector implementation
// (pkg/telemetry). Keeping them here lets both packages agree on the same
// EventType and DeliveryMode without either importing the other: props can
// declare the TelemetryCollector interface and the tool-author TelemetryConfig
// field, while telemetry keeps the runtime behaviour, and neither drags in the
// other's dependency graph.
//
// This package imports nothing from GTB (or anywhere else) by design — it is a
// leaf so that pkg/telemetry stays free of a pkg/props import and remains a
// standalone extraction candidate.
package telemetrytypes

// EventType identifies the category of a telemetry event. It is a string so
// that values are stable on the wire and interchangeable across the boundary
// between props and telemetry.
type EventType string

const (
	// EventCommandInvocation records that a command ran.
	EventCommandInvocation EventType = "command.invocation"
	// EventCommandError records that a command failed.
	EventCommandError EventType = "command.error"
	// EventFeatureUsed records that a named feature was exercised.
	EventFeatureUsed EventType = "feature.used"
	// EventUpdateCheck records a self-update check.
	EventUpdateCheck EventType = "update.check"
	// EventUpdateApplied records that a self-update was applied.
	EventUpdateApplied EventType = "update.applied"
	// EventDeletionRequest records a user data-deletion request.
	EventDeletionRequest EventType = "data.deletion_request"
)

// DeliveryMode controls the delivery guarantee for spilled telemetry events.
type DeliveryMode string

const (
	// DeliveryAtLeastOnce deletes spill files only after a successful send, so
	// events survive restarts and transient backend outages. Possible
	// duplicates if the acknowledgement is lost.
	//
	// The guarantee is bounded by the on-disk spill cap: when accumulated spill
	// files reach the collector's maxSpillFiles limit (a long-offline machine
	// whose backend is unreachable), the oldest un-sent files are discarded to
	// keep disk usage bounded. Each such prune is logged at WARN. Within the cap
	// there is no data loss; beyond it, the oldest events are dropped.
	DeliveryAtLeastOnce DeliveryMode = "at_least_once"
	// DeliveryAtMostOnce deletes spill files before sending.
	// Possible data loss; no duplicates.
	DeliveryAtMostOnce DeliveryMode = "at_most_once"
)
