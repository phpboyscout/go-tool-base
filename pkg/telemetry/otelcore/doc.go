// Package otelcore holds the OTLP/OTel export plumbing shared by GTB's analytics
// pipeline and its web-service observability signals: OTLP/HTTP endpoint parsing,
// the service resource, and telemetry.* configuration resolution.
//
// It deliberately imports no signal exporters. Traces, metrics and logs each
// build their own typed exporter (otlptracehttp, otlpmetrichttp, otlploghttp)
// from a resolved [Settings] and [Endpoint], so this package stays free of any
// single signal's dependencies and can be shared by all of them.
package otelcore
