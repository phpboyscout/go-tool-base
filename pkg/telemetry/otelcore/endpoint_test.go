package otelcore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	t.Run("https endpoint is secure", func(t *testing.T) {
		ep, err := ParseEndpoint("https://collector:4318", false, nil)
		require.NoError(t, err)
		assert.Equal(t, "collector:4318", ep.Host)
		assert.Empty(t, ep.BasePath)
		assert.False(t, ep.Insecure)
	})

	t.Run("http scheme forces insecure", func(t *testing.T) {
		ep, err := ParseEndpoint("http://localhost:4318", false, nil)
		require.NoError(t, err)
		assert.True(t, ep.Insecure)
	})

	t.Run("explicit insecure flag overrides an https scheme", func(t *testing.T) {
		ep, err := ParseEndpoint("https://collector:4318", true, nil)
		require.NoError(t, err)
		assert.True(t, ep.Insecure)
	})

	t.Run("base path is preserved for the per-signal suffix", func(t *testing.T) {
		ep, err := ParseEndpoint("https://collector:4318/otlp", false, nil)
		require.NoError(t, err)
		assert.Equal(t, "/otlp", ep.BasePath)
		assert.Equal(t, "/otlp/v1/traces", ep.BasePath+"/v1/traces")
	})

	t.Run("headers pass through unchanged", func(t *testing.T) {
		h := map[string]string{"authorization": "Bearer token"}
		ep, err := ParseEndpoint("https://collector:4318", false, h)
		require.NoError(t, err)
		assert.Equal(t, h, ep.Headers)
	})

	t.Run("an unparseable url is an error", func(t *testing.T) {
		_, err := ParseEndpoint("\x7f", false, nil)
		require.Error(t, err)
	})
}
