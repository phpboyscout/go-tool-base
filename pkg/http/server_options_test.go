package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockConfig "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// safeBuffer is a goroutine-safe bytes.Buffer for capturing log output written
// from the server's Serve goroutine while the test reads it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestNewServer_WithConfigPrefix(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.admin.port").Return(18081)
	cfg.EXPECT().GetInt("server.admin.max_header_bytes").Return(0)

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	assert.Equal(t, ":18081", srv.Addr)
}

func TestServerSettingsFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_HTTP_PORT", "18082")
	t.Setenv("GTB_SERVER_HTTP_MAX_HEADER_BYTES", "4096")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader("server:\n  http:\n    port: 18081\n    max_header_bytes: 2048\n")),
	)

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 18082, got.Port)
	assert.Equal(t, 4096, got.MaxHeaderBytes)
}

func TestNewServer_WithSettings(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(context.Background(), ServerSettings{
		Port:           18083,
		MaxHeaderBytes: 8192,
	}, http.DefaultServeMux)
	require.NoError(t, err)
	assert.Equal(t, ":18083", srv.Addr)
	assert.Equal(t, 8192, srv.MaxHeaderBytes)
}

func TestNewServer_WithPort_BypassesConfig(t *testing.T) {
	t.Parallel()

	// No port config expectations are registered: WithPort must skip the
	// config lookups entirely. max_header_bytes is still resolved from config.
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithPort(9090))
	require.NoError(t, err)
	assert.Equal(t, ":9090", srv.Addr)
}

func TestNewServer_WithPort_OverridesConfiguredPort(t *testing.T) {
	t.Parallel()

	// Even when config carries a port, WithPort wins and the config port is
	// never consulted.
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithPort(7000))
	require.NoError(t, err)
	assert.Equal(t, ":7000", srv.Addr)
}

func TestNewServer_TwoServers_DistinctAddr(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(8080)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)
	cfg.EXPECT().GetInt("server.admin.port").Return(9091)
	cfg.EXPECT().GetInt("server.admin.max_header_bytes").Return(0)

	pub, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)

	adm, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithConfigPrefix("server.admin"))
	require.NoError(t, err)

	assert.Equal(t, ":8080", pub.Addr)
	assert.Equal(t, ":9091", adm.Addr)
}

func TestNewServer_WithMaxHeaderBytes_OverridesConfig(t *testing.T) {
	t.Parallel()

	// The option takes precedence, so config max_header_bytes is not read.
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithMaxHeaderBytes(4096))
	require.NoError(t, err)
	assert.Equal(t, 4096, srv.MaxHeaderBytes)
}

func TestNewServer_WithTimeouts(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux,
		WithReadTimeout(3*time.Second),
		WithWriteTimeout(7*time.Second),
		WithIdleTimeout(42*time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, srv.ReadTimeout)
	assert.Equal(t, 7*time.Second, srv.WriteTimeout)
	assert.Equal(t, 42*time.Second, srv.IdleTimeout)
}

func TestNewServer_DefaultTimeoutsPreserved(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	assert.Equal(t, readTimeout, srv.ReadTimeout)
	assert.Equal(t, writeTimeout, srv.WriteTimeout)
	assert.Equal(t, idleTimeout, srv.IdleTimeout)
}

func TestNewServer_WithServerTLSConfig(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	custom := &tls.Config{MinVersion: tls.VersionTLS13}

	srv, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithServerTLSConfig(custom))
	require.NoError(t, err)
	assert.Same(t, custom, srv.TLSConfig)
}

func TestNewServer_InvalidPort(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)

	_, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithPort(70000))
	require.Error(t, err)
}

func TestNewServer_EmptyPrefix(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)

	_, err := NewServerFromContainable(context.Background(), cfg, http.DefaultServeMux, WithConfigPrefix(""))
	require.Error(t, err)
}

func TestRegister_WithConfigPrefix_RoutesServerOption(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.admin.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.admin.max_header_bytes").Return(0).Maybe()
	// TLS resolution for the custom prefix happens at Start (registered lazily).
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.admin.tls.key").Return(false).Maybe()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromContainable(context.Background(), "admin", controller, cfg, testLogger(), http.DefaultServeMux,
		WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestStart_EmptyPrefixDefaultsToHTTP(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	cfg := mockConfig.NewMockContainable(t)
	mockTLSDisabled(cfg)

	srv := &http.Server{Addr: ":0", Handler: http.NewServeMux()}

	// An empty prefix falls back to the default "server.http" block.
	startFn := StartFromContainable(cfg, logger.NewCharm(&buf), srv, WithConfigPrefix(""))
	require.NoError(t, startFn(context.Background()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "starting http server")
	}, 2*time.Second, 20*time.Millisecond)
}

func TestStart_LogsBoundEphemeralPort(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	cfg := mockConfig.NewMockContainable(t)
	mockTLSDisabled(cfg)

	// Ephemeral bind: :0 lets the OS choose the port.
	srv := &http.Server{
		Addr:    ":0",
		Handler: http.NewServeMux(),
	}

	startFn := StartFromContainable(cfg, logger.NewCharm(&buf), srv)
	require.NoError(t, startFn(context.Background()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "starting http server")
	}, 2*time.Second, 20*time.Millisecond)

	out := buf.String()
	// The resolved port must be logged, never the literal :0.
	assert.NotContains(t, out, "addr=:0")
	assert.NotContains(t, out, `addr=":0"`)
}

func TestStart_WithConfigPrefix_ResolvesPrefixedTLS(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	cfg := mockConfig.NewMockContainable(t)
	// Shared TLS prefix is always read; the per-server prefix override resolves
	// the "server.gateway.tls.*" block rather than the default "server.http.*".
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.gateway.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.gateway.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.gateway.tls.key").Return(false).Maybe()

	srv := &http.Server{Addr: ":0", Handler: http.NewServeMux()}

	startFn := StartFromContainable(cfg, logger.NewCharm(&buf), srv, WithConfigPrefix("server.gateway"))
	require.NoError(t, startFn(context.Background()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "starting http server")
	}, 2*time.Second, 20*time.Millisecond)
}
