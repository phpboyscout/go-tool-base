package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// These tests verify provider construction. The final flush-to-collector on
// Shutdown is exercised by the end-to-end harness run against a live collector;
// here there is none, so a Shutdown export error is expected and ignored.

func TestNewProviderWithEndpoint(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	mp, err := NewProvider(context.Background(), res, otelcore.Settings{
		Endpoint: "http://localhost:4318",
		Insecure: true,
	})
	require.NoError(t, err)
	require.NotNil(t, mp)

	_ = mp.Shutdown(context.Background())
}

func TestNewProviderEmptyEndpointUsesEnvFallback(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	mp, err := NewProvider(context.Background(), res, otelcore.Settings{})
	require.NoError(t, err)
	require.NotNil(t, mp)

	_ = mp.Shutdown(context.Background())
}

func TestNewProviderBadEndpoint(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	_, err := NewProvider(context.Background(), res, otelcore.Settings{Endpoint: "\x7f"})
	require.Error(t, err)
}

func TestWithIntervalApplies(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	mp, err := NewProvider(context.Background(), res,
		otelcore.Settings{Endpoint: "http://localhost:4318", Insecure: true},
		WithInterval(5*time.Second),
	)
	require.NoError(t, err)
	require.NotNil(t, mp)

	_ = mp.Shutdown(context.Background())
}
