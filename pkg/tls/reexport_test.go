package tls_test

import (
	cryptotls "crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	configmocks "gitlab.com/phpboyscout/go/config/mocks"

	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// TestDefaultConfig checks the re-exported hardened server/client config is
// returned with a floor of TLS 1.2.
func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := gtbtls.DefaultConfig()
	require.NotNil(t, cfg)
	assert.GreaterOrEqual(t, cfg.MinVersion, uint16(cryptotls.VersionTLS12),
		"default config must enforce at least TLS 1.2")
}

// TestClientConfig checks the re-exported client config builder returns a
// hardened config when seeded with the system roots (no CA files).
func TestClientConfig(t *testing.T) {
	t.Parallel()

	cfg, err := gtbtls.ClientConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.GreaterOrEqual(t, cfg.MinVersion, uint16(cryptotls.VersionTLS12))
}

// TestCertPool exercises the re-exported CertPool builder for both the
// system-roots path (no arguments) and the error path (a missing file).
func TestCertPool(t *testing.T) {
	t.Parallel()

	pool, err := gtbtls.CertPool()
	require.NoError(t, err)
	assert.NotNil(t, pool)

	_, err = gtbtls.CertPool("/nonexistent/ca.pem")
	assert.Error(t, err, "a missing CA file must surface an error")
}

// TestResolve_MockReader_AllOverrides covers the case where every
// transport-specific field overrides the shared pair — the enabled and key
// override branches the single-override case does not exercise.
func TestResolve_MockReader_AllOverrides(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockReader(t)
	cfg.EXPECT().SectionExists("server.tls").Return(true).Once()
	cfg.EXPECT().UnmarshalKey("server.tls", mock.Anything).RunAndReturn(func(_ string, target any) error {
		pair, ok := target.(*gtbtls.Pair)
		require.True(t, ok)

		pair.Cert = "/shared/cert.pem"
		pair.Key = "/shared/key.pem"

		return nil
	}).Once()
	cfg.EXPECT().SectionExists("server.grpc.tls").Return(true).Once()
	cfg.EXPECT().UnmarshalKey("server.grpc.tls", mock.Anything).RunAndReturn(func(_ string, target any) error {
		pair, ok := target.(*gtbtls.Pair)
		require.True(t, ok)

		pair.Enabled = true
		pair.Cert = "/grpc/cert.pem"
		pair.Key = "/grpc/key.pem"

		return nil
	}).Once()
	cfg.EXPECT().IsSet("server.grpc.tls.enabled").Return(true).Once()
	cfg.EXPECT().IsSet("server.grpc.tls.cert").Return(true).Once()
	cfg.EXPECT().IsSet("server.grpc.tls.key").Return(true).Once()

	pair := gtbtls.Resolve(cfg, "server.grpc.tls")

	assert.True(t, pair.Enabled, "transport override must flip enabled on")
	assert.Equal(t, "/grpc/cert.pem", pair.Cert)
	assert.Equal(t, "/grpc/key.pem", pair.Key)
}
