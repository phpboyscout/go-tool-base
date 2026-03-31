package http

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultTLSConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultTLSConfig()

	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.NotEmpty(t, cfg.CipherSuites)
	assert.NotEmpty(t, cfg.CurvePreferences)

	// Ensure X25519 is preferred
	assert.Equal(t, tls.X25519, cfg.CurvePreferences[0])

	// Verify all cipher suites are curated (AEAD based)
	for _, suite := range cfg.CipherSuites {
		assert.Contains(t, []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		}, suite)
	}
}
