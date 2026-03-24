package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func testLogger() logger.Logger {
	return logger.NewNoop()
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)

	srv, err := NewServer(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	require.NotNil(t, srv)

	assert.Equal(t, ":0", srv.Addr)
	assert.Equal(t, readTimeout, srv.ReadTimeout)
	assert.Equal(t, writeTimeout, srv.WriteTimeout)
	assert.Equal(t, idleTimeout, srv.IdleTimeout)
	assert.NotNil(t, srv.TLSConfig)
}

func TestStart_HTTP(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	cfg.EXPECT().GetString("server.tls.cert").Return("")
	cfg.EXPECT().GetString("server.tls.key").Return("")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	startFn := Start(cfg, testLogger(), srv)

	// Start in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- startFn(context.Background())
	}()

	// Wait for server to be ready
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 50*time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))

	// Start should return nil (ErrServerClosed is swallowed)
	assert.NoError(t, <-errCh)
}

func TestStop(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: http.DefaultServeMux,
	}

	go func() { _ = srv.ListenAndServe() }()

	// Wait for it to start
	time.Sleep(50 * time.Millisecond)

	stopFn := Stop(testLogger(), srv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Should not panic
	stopFn(ctx)
}

func TestRegister(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	cfg.EXPECT().GetString("server.tls.cert").Return("")
	cfg.EXPECT().GetString("server.tls.key").Return("")

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := Register(context.Background(), "test-http", controller, cfg, testLogger(), http.DefaultServeMux)
	assert.NoError(t, err)
}

func TestStatus_ValidServer(t *testing.T) {
	t.Parallel()
	srv := &http.Server{}
	err := Status(srv)()
	assert.NoError(t, err)
}

func TestStatus_NilServer(t *testing.T) {
	t.Parallel()
	err := Status(nil)()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "http server is nil")
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	cfg.EXPECT().GetString("server.tls.cert").Return("")
	cfg.EXPECT().GetString("server.tls.key").Return("")

	controller := controls.NewController(context.Background(), controls.WithoutSignals())
	
	// Register a service that reports unhealthy
	controller.Register("unhealthy-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithStatus(func() error { return fmt.Errorf("failed") }),
	)

	_, err = Register(context.Background(), "test-http", controller, cfg, testLogger(), http.NewServeMux())
	require.NoError(t, err)

	controller.Start()

	// Check /healthz - should be 503
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", port))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusServiceUnavailable
	}, 2*time.Second, 50*time.Millisecond)

	controller.Stop()
	controller.Wait()
}

func TestProbes(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	cfg.EXPECT().GetString("server.tls.cert").Return("")
	cfg.EXPECT().GetString("server.tls.key").Return("")

	controller := controls.NewController(context.Background(), controls.WithoutSignals())
	
	controller.Register("test-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithLiveness(func() error { return nil }),
		controls.WithReadiness(func() error { return fmt.Errorf("not ready") }),
	)

	_, err = Register(context.Background(), "test-http", controller, cfg, testLogger(), http.NewServeMux())
	require.NoError(t, err)

	controller.Start()

	// Check /livez - should be 200
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/livez", port))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 50*time.Millisecond)

	// Check /readyz - should be 503
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/readyz", port))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusServiceUnavailable
	}, 2*time.Second, 50*time.Millisecond)

	controller.Stop()
	controller.Wait()
}
