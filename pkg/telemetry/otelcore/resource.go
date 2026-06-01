package otelcore

import (
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Resource builds the OTel resource that identifies the emitting service. name
// and version populate the service.name and service.version semantic-convention
// attributes that appear as labels on every signal in the backend.
func Resource(name, version string) *resource.Resource {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(name),
		semconv.ServiceVersion(version),
	)
}
