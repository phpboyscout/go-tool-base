package otelcore

import (
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
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
}

func TestParseEndpoint_Rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
	}{
		{"empty", ""},
		{"control character", "\x7f"},
		{"newline injection", "https://collector:4318\nHost: evil"},
		{"missing scheme", "collector:4318"},
		{"unsupported scheme", "grpc://collector:4318"},
		{"ftp scheme", "ftp://collector:4318"},
		{"missing host", "https://"},
		{"userinfo with password", "https://user:pass@collector:4318"},
		{"userinfo without password", "https://user@collector:4318"},
		{"over length", "https://collector:4318/" + strings.Repeat("a", MaxEndpointLength)},
		{"unparseable", "https://%zz"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseEndpoint(tc.rawURL, false, nil)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidEndpoint),
				"error should wrap ErrInvalidEndpoint, got %v", err)
		})
	}
}

func TestParseEndpoint_Accepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
	}{
		{"plain https host", "https://collector:4318"},
		{"http host", "http://localhost:4318"},
		{"with base path", "https://otlp-gateway.example.net/otlp"},
		{"uppercase scheme", "HTTPS://collector:4318"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ep, err := ParseEndpoint(tc.rawURL, false, nil)
			require.NoError(t, err)
			assert.NotEmpty(t, ep.Host)
		})
	}
}
