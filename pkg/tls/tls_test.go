package tls_test

import (
	cryptotls "crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := gtbtls.DefaultConfig()

	assert.Equal(t, uint16(cryptotls.VersionTLS12), cfg.MinVersion)
	assert.NotEmpty(t, cfg.CipherSuites)
	assert.NotEmpty(t, cfg.CurvePreferences)
	assert.Equal(t, cryptotls.X25519, cfg.CurvePreferences[0])

	for _, suite := range cfg.CipherSuites {
		assert.Contains(t, []uint16{
			cryptotls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			cryptotls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			cryptotls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			cryptotls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			cryptotls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			cryptotls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		}, suite)
	}
}

func TestResolvePair(t *testing.T) {
	t.Parallel()

	shared := gtbtls.Pair{
		Enabled: true,
		Cert:    "/shared/cert.pem",
		Key:     "/shared/key.pem",
	}
	transport := gtbtls.Pair{
		Enabled: false,
		Cert:    "/transport/cert.pem",
		Key:     "/transport/key.pem",
	}

	pair := gtbtls.ResolvePair(shared, transport, gtbtls.PairOverrides{
		Enabled: true,
		Cert:    true,
	})

	assert.False(t, pair.Enabled)
	assert.Equal(t, "/transport/cert.pem", pair.Cert)
	assert.Equal(t, "/shared/key.pem", pair.Key)
}

func TestCertPool_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := gtbtls.CertPool("/does/not/exist.pem")
	require.Error(t, err)
}
