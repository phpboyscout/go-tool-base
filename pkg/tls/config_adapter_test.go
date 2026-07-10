package tls_test

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	configmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

func newTestConfig() config.Containable {
	return config.NewContainerFromViper(logger.ToSlog(logger.NewCharm(io.Discard)), viper.New())
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

func TestResolve_NoOverrides(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()
	cfg.Set("server.tls.enabled", true)
	cfg.Set("server.tls.cert", "/shared/cert.pem")
	cfg.Set("server.tls.key", "/shared/key.pem")

	// Transport prefix has no keys set; everything inherits the shared block.
	pair := gtbtls.Resolve(cfg, "server.gateway.tls")
	assert.True(t, pair.Enabled)
	assert.Equal(t, "/shared/cert.pem", pair.Cert)
	assert.Equal(t, "/shared/key.pem", pair.Key)
}

func TestResolve_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_TLS_CERT", "/env/shared-cert.pem")
	t.Setenv("GTB_SERVER_HTTP_TLS_KEY", "/env/http-key.pem")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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

func TestResolve_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gtbtls.Pair{}, gtbtls.Resolve(nil, "server.http.tls"))
}

func TestResolve_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.tls.enabled").Return(true).Once()
	cfg.EXPECT().GetString("server.tls.cert").Return("/shared/cert.pem").Once()
	cfg.EXPECT().GetString("server.tls.key").Return("/shared/key.pem").Once()
	cfg.EXPECT().IsSet("server.http.tls.enabled").Return(false).Once()
	cfg.EXPECT().IsSet("server.http.tls.cert").Return(true).Once()
	cfg.EXPECT().GetString("server.http.tls.cert").Return("/http/cert.pem").Once()
	cfg.EXPECT().IsSet("server.http.tls.key").Return(false).Once()

	pair := gtbtls.Resolve(cfg, "server.http.tls")

	assert.True(t, pair.Enabled)
	assert.Equal(t, "/http/cert.pem", pair.Cert)
	assert.Equal(t, "/shared/key.pem", pair.Key)
}
