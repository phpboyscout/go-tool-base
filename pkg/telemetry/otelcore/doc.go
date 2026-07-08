// Package otelcore holds the OTLP/OTel export plumbing shared by GTB's analytics
// pipeline and its web-service observability signals: OTLP/HTTP endpoint
// parsing, the service resource, telemetry.* configuration resolution, and the
// GTB config adapter that can observe a resolved signal settings source.
//
// It deliberately imports no signal exporters. Traces, metrics and logs each
// build their own typed exporter (otlptracehttp, otlpmetrichttp, otlploghttp)
// from a resolved [Settings] and [Endpoint], so this package stays free of any
// single signal's dependencies and can be shared by all of them. Long-lived
// callers can depend on [SettingsSource] to see when a resolved settings
// snapshot changes without importing the GTB config container.
package otelcore
