package tls_test

import (
	cryptotls "crypto/tls"
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

func newTestConfig() config.Containable {
	return config.NewContainerFromViper(logger.NewCharm(io.Discard), viper.New())
}

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

func TestResolve_SharedAndOverride(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()
	cfg.Set("server.tls.enabled", true)
	cfg.Set("server.tls.cert", "/shared/cert.pem")
	cfg.Set("server.tls.key", "/shared/key.pem")

	// gRPC inherits the shared cert.
	grpcPair := gtbtls.Resolve(cfg, "server.grpc.tls")
	assert.True(t, grpcPair.Enabled)
	assert.Equal(t, "/shared/cert.pem", grpcPair.Cert)
	assert.True(t, grpcPair.Valid())

	// HTTP overrides only the cert/key, inheriting enabled from the shared block.
	cfg.Set("server.http.tls.cert", "/http/cert.pem")
	cfg.Set("server.http.tls.key", "/http/key.pem")

	httpPair := gtbtls.Resolve(cfg, "server.http.tls")
	assert.True(t, httpPair.Enabled)
	assert.Equal(t, "/http/cert.pem", httpPair.Cert)
	assert.Equal(t, "/http/key.pem", httpPair.Key)
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

func TestResolve_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_TLS_CERT", "/env/shared-cert.pem")
	t.Setenv("GTB_SERVER_HTTP_TLS_KEY", "/env/http-key.pem")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader(`
server:
  tls:
    enabled: true
    cert: /file/shared-cert.pem
    key: /file/shared-key.pem
  http:
    tls:
      cert: /file/http-cert.pem
`)),
	)

	pair := gtbtls.Resolve(cfg, "server.http.tls")

	assert.True(t, pair.Enabled)
	assert.Equal(t, "/file/http-cert.pem", pair.Cert)
	assert.Equal(t, "/env/http-key.pem", pair.Key)
}

func TestCertPool_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := gtbtls.CertPool("/does/not/exist.pem")
	require.Error(t, err)
}
