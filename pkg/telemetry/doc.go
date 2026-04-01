// Package telemetry provides an opt-in telemetry framework with pluggable
// backends, privacy controls, bounded buffering, and GDPR-compliant data
// deletion for CLI tools built on GTB.
//
// # Architecture
//
// Telemetry is gated at two levels: the tool author enables the TelemetryCmd
// feature flag, and the user opts in via the telemetry enable command or
// TELEMETRY_ENABLED environment variable. When either gate is inactive,
// the collector is a silent noop — callers never need to check.
//
// Events are buffered in memory (capped at 1000) and flushed on process
// exit via Cobra's OnFinalize callback. If the buffer fills before flush,
// events are spilled to disk and recovered on the next flush.
//
// # Backends
//
// The Backend interface allows tool authors to supply custom analytics
// platforms. Built-in backends:
//
//   - NewNoopBackend — silently discards events (used when disabled)
//   - NewStdoutBackend — pretty-printed JSON to a writer (debugging)
//   - NewFileBackend — newline-delimited JSON to a file (local-only mode)
//   - NewHTTPBackend — JSON POST to an HTTP endpoint
//   - NewOTelBackend — OpenTelemetry log records via OTLP/HTTP
//
// Vendor-specific backends for Datadog and PostHog are in subpackages.
//
// # Privacy
//
// No personally identifiable information is collected. Machine IDs are
// SHA-256 hashed from multiple system signals. Command arguments and file
// contents are never recorded unless ExtendedCollection is explicitly
// enabled by the tool author for enterprise environments.
//
// # Usage
//
//	// Track a command invocation
//	p.Collector.TrackCommand("generate", durationMs, exitCode, nil)
//
//	// Track a feature usage event
//	p.Collector.Track(props.EventFeatureUsed, "ai.chat", map[string]string{"provider": "claude"})
package telemetry
