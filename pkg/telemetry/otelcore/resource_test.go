package otelcore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func TestResource(t *testing.T) {
	res := Resource("macguffinsvc", "v1.2.3")
	set := res.Set()

	name, ok := set.Value(semconv.ServiceNameKey)
	require.True(t, ok, "resource must carry service.name")
	assert.Equal(t, "macguffinsvc", name.AsString())

	ver, ok := set.Value(semconv.ServiceVersionKey)
	require.True(t, ok, "resource must carry service.version")
	assert.Equal(t, "v1.2.3", ver.AsString())
}
