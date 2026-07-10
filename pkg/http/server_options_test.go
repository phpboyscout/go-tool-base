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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
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

	srv, err := NewServer(context.Background(), ServerSettings{Port: 18081}, http.DefaultServeMux, WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	assert.Equal(t, ":18081", srv.Addr)
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

	srv, err := NewServer(context.Background(), ServerSettings{}, http.DefaultServeMux, WithPort(9090))
	require.NoError(t, err)
	assert.Equal(t, ":9090", srv.Addr)
}

func TestNewServer_WithPort_OverridesConfiguredPort(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(context.Background(), ServerSettings{Port: 9090}, http.DefaultServeMux, WithPort(7000))
	require.NoError(t, err)
	assert.Equal(t, ":7000", srv.Addr)
}

func TestNewServer_TwoServers_DistinctAddr(t *testing.T) {
	t.Parallel()

	pub, err := NewServer(context.Background(), ServerSettings{Port: 8080}, http.DefaultServeMux)
	require.NoError(t, err)

	adm, err := NewServer(context.Background(), ServerSettings{Port: 9091}, http.DefaultServeMux, WithConfigPrefix("server.admin"))
	require.NoError(t, err)

	assert.Equal(t, ":8080", pub.Addr)
	assert.Equal(t, ":9091", adm.Addr)
}

func TestNewServer_WithMaxHeaderBytes_OverridesConfig(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(context.Background(), ServerSettings{MaxHeaderBytes: 2048}, http.DefaultServeMux, WithMaxHeaderBytes(4096))
	require.NoError(t, err)
	assert.Equal(t, 4096, srv.MaxHeaderBytes)
}

func TestNewServer_WithTimeouts(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(context.Background(), ServerSettings{}, http.DefaultServeMux,
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

	srv, err := NewServer(context.Background(), ServerSettings{}, http.DefaultServeMux)
	require.NoError(t, err)
	assert.Equal(t, readTimeout, srv.ReadTimeout)
	assert.Equal(t, writeTimeout, srv.WriteTimeout)
	assert.Equal(t, idleTimeout, srv.IdleTimeout)
}

func TestNewServer_WithServerTLSConfig(t *testing.T) {
	t.Parallel()

	custom := &tls.Config{MinVersion: tls.VersionTLS13}

	srv, err := NewServer(context.Background(), ServerSettings{}, http.DefaultServeMux, WithServerTLSConfig(custom))
	require.NoError(t, err)
	assert.Same(t, custom, srv.TLSConfig)
}

func TestNewServer_InvalidPort(t *testing.T) {
	t.Parallel()

	_, err := NewServer(context.Background(), ServerSettings{}, http.DefaultServeMux, WithPort(70000))
	require.Error(t, err)
}

func TestNewServer_EmptyPrefix(t *testing.T) {
	t.Parallel()

	_, err := NewServer(context.Background(), ServerSettings{}, http.DefaultServeMux, WithConfigPrefix(""))
	require.Error(t, err)
}

func TestRegister_WithConfigPrefix_RoutesServerOption(t *testing.T) {
	t.Parallel()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := Register(context.Background(), "admin", controller, logger.ToSlog(testLogger()), http.DefaultServeMux, ServerSettings{}, gtbtls.Pair{},
		WithConfigPrefix("server.admin"))
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestStart_EmptyPrefixDefaultsToHTTP(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	srv := &http.Server{Addr: ":0", Handler: http.NewServeMux()}

	startFn := StartWithTLSPair(logger.ToSlog(logger.NewCharm(&buf)), srv, gtbtls.Pair{})
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

	// Ephemeral bind: :0 lets the OS choose the port.
	srv := &http.Server{
		Addr:    ":0",
		Handler: http.NewServeMux(),
	}

	startFn := StartWithTLSPair(logger.ToSlog(logger.NewCharm(&buf)), srv, gtbtls.Pair{})
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

func TestStart_WithExplicitTLSPair(t *testing.T) {
	t.Parallel()

	var buf safeBuffer

	srv := &http.Server{Addr: ":0", Handler: http.NewServeMux()}

	startFn := StartWithTLSPair(logger.ToSlog(logger.NewCharm(&buf)), srv, gtbtls.Pair{})
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
