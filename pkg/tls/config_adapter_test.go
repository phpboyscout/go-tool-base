package tls_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configmocks "gitlab.com/phpboyscout/go/config/mocks"

	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// newTestView pins a view over the given YAML document, with any further store
// options (e.g. config.WithEnv) layered above it.
func newTestView(t *testing.T, yaml string, opts ...config.StoreOption) *config.View {
	t.Helper()

	store, err := config.NewStore(t.Context(), append([]config.StoreOption{
		config.WithReaders(config.NamedSource{Name: "test", Content: []byte(yaml)}),
	}, opts...)...)
	require.NoError(t, err)

	return store.View()
}

func TestResolve_SharedAndOverride(t *testing.T) {
	t.Parallel()

	cfg := newTestView(t, `
server:
  tls:
    enabled: true
    cert: /shared/cert.pem
    key: /shared/key.pem
  http:
    tls:
      cert: /http/cert.pem
      key: /http/key.pem
`)

	// gRPC inherits the shared cert.
	grpcPair := gtbtls.Resolve(cfg, "server.grpc.tls")
	assert.True(t, grpcPair.Enabled)
	assert.Equal(t, "/shared/cert.pem", grpcPair.Cert)
	assert.True(t, grpcPair.Valid())

	// HTTP overrides only the cert/key, inheriting enabled from the shared block.
	httpPair := gtbtls.Resolve(cfg, "server.http.tls")
	assert.True(t, httpPair.Enabled)
	assert.Equal(t, "/http/cert.pem", httpPair.Cert)
	assert.Equal(t, "/http/key.pem", httpPair.Key)
}

func TestResolve_NoOverrides(t *testing.T) {
	t.Parallel()

	cfg := newTestView(t, `
server:
  tls:
    enabled: true
    cert: /shared/cert.pem
    key: /shared/key.pem
`)

	// Transport prefix has no keys set; everything inherits the shared block.
	pair := gtbtls.Resolve(cfg, "server.gateway.tls")
	assert.True(t, pair.Enabled)
	assert.Equal(t, "/shared/cert.pem", pair.Cert)
	assert.Equal(t, "/shared/key.pem", pair.Key)
}

func TestResolve_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_TLS_CERT", "/env/shared-cert.pem")
	t.Setenv("GTB_SERVER_HTTP_TLS_KEY", "/env/http-key.pem")

	cfg := newTestView(t, `
server:
  tls:
    enabled: true
    cert: /file/shared-cert.pem
    key: /file/shared-key.pem
  http:
    tls:
      cert: /file/http-cert.pem
`, config.WithEnv("GTB"))

	pair := gtbtls.Resolve(cfg, "server.http.tls")

	assert.True(t, pair.Enabled)
	assert.Equal(t, "/file/http-cert.pem", pair.Cert)
	assert.Equal(t, "/env/http-key.pem", pair.Key)
}

func TestResolve_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gtbtls.Pair{}, gtbtls.Resolve(nil, "server.http.tls"))
}

// TestResolve_MockReader pins the property the deleted legacy branch existed
// for: Resolve works against any config.Reader, so downstream tests can drive
// it with the published mocks rather than a real store.
func TestResolve_MockReader(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockReader(t)
	cfg.EXPECT().SectionExists("server.tls").Return(true).Once()
	cfg.EXPECT().UnmarshalKey("server.tls", mock.Anything).RunAndReturn(func(_ string, target any) error {
		pair, ok := target.(*gtbtls.Pair)
		require.True(t, ok)

		pair.Enabled = true
		pair.Cert = "/shared/cert.pem"
		pair.Key = "/shared/key.pem"

		return nil
	}).Once()
	cfg.EXPECT().SectionExists("server.http.tls").Return(true).Once()
	cfg.EXPECT().UnmarshalKey("server.http.tls", mock.Anything).RunAndReturn(func(_ string, target any) error {
		pair, ok := target.(*gtbtls.Pair)
		require.True(t, ok)

		pair.Cert = "/http/cert.pem"

		return nil
	}).Once()
	cfg.EXPECT().IsSet("server.http.tls.enabled").Return(false).Once()
	cfg.EXPECT().IsSet("server.http.tls.cert").Return(true).Once()
	cfg.EXPECT().IsSet("server.http.tls.key").Return(false).Once()

	pair := gtbtls.Resolve(cfg, "server.http.tls")

	assert.True(t, pair.Enabled)
	assert.Equal(t, "/http/cert.pem", pair.Cert)
	assert.Equal(t, "/shared/key.pem", pair.Key)
}
