package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

func TestNewProviderWithEndpoint(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	tp, err := NewProvider(context.Background(), res, otelcore.Settings{
		Endpoint: "http://localhost:4318",
		Insecure: true,
	})
	require.NoError(t, err)
	require.NotNil(t, tp)
	assert.NoError(t, tp.Shutdown(context.Background()))
}

func TestNewProviderEmptyEndpointUsesEnvFallback(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	tp, err := NewProvider(context.Background(), res, otelcore.Settings{})
	require.NoError(t, err)
	require.NotNil(t, tp)
	assert.NoError(t, tp.Shutdown(context.Background()))
}

func TestNewProviderBadEndpoint(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	_, err := NewProvider(context.Background(), res, otelcore.Settings{Endpoint: "\x7f"})
	require.Error(t, err)
}

func TestWithSamplingApplies(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	tp, err := NewProvider(context.Background(), res,
		otelcore.Settings{Endpoint: "http://localhost:4318", Insecure: true},
		WithSampling(1.0),
	)
	require.NoError(t, err)
	require.NotNil(t, tp)
	assert.NoError(t, tp.Shutdown(context.Background()))
}
