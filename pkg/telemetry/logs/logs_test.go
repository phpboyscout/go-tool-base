package logs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// Construction tests; the flush-to-collector on Shutdown is exercised by the
// end-to-end harness run, so a Shutdown export error here is expected and ignored.

func TestNewProviderWithEndpoint(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	lp, err := NewProvider(context.Background(), res, otelcore.Settings{
		Endpoint: "http://localhost:4318",
		Insecure: true,
	})
	require.NoError(t, err)
	require.NotNil(t, lp)

	_ = lp.Shutdown(context.Background())
}

func TestNewProviderEmptyEndpointUsesEnvFallback(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	lp, err := NewProvider(context.Background(), res, otelcore.Settings{})
	require.NoError(t, err)
	require.NotNil(t, lp)

	_ = lp.Shutdown(context.Background())
}

func TestNewProviderBadEndpoint(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	_, err := NewProvider(context.Background(), res, otelcore.Settings{Endpoint: "\x7f"})
	require.Error(t, err)
}

func TestHandlerIsNonNil(t *testing.T) {
	res := otelcore.Resource("test", "v0.0.0")

	lp, err := NewProvider(context.Background(), res, otelcore.Settings{})
	require.NoError(t, err)

	assert.NotNil(t, Handler(lp, "macguffin"))

	_ = lp.Shutdown(context.Background())
}
