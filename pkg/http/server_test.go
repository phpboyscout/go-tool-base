package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMaxBytesMiddleware_RejectsOversizedBody(t *testing.T) {
	t.Parallel()

	const limit int64 = 1024

	// Handler attempts to read the body; MaxBytesReader returns an error
	// once the limit is exceeded.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	wrapped := MaxBytesMiddleware(limit)(handler)
	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)

	t.Run("body within limit is accepted", func(t *testing.T) {
		t.Parallel()

		body := strings.NewReader(strings.Repeat("a", int(limit)))
		resp, err := http.Post(srv.URL, "text/plain", body)
		require.NoError(t, err)

		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("body over limit is rejected", func(t *testing.T) {
		t.Parallel()

		body := strings.NewReader(strings.Repeat("a", int(limit)+1))
		resp, err := http.Post(srv.URL, "text/plain", body)
		require.NoError(t, err)

		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	})
}

func TestMaxBytesMiddleware_NonPositiveLimitDisablesCap(t *testing.T) {
	t.Parallel()

	// A zero/negative limit must be treated as "no cap" so downstream
	// handlers see the raw body unchanged.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = fmt.Fprintf(w, "read=%d", n)
	})

	wrapped := MaxBytesMiddleware(0)(handler)
	srv := httptest.NewServer(wrapped)
	t.Cleanup(srv.Close)

	body := strings.NewReader(strings.Repeat("a", 1<<15)) // 32 KiB
	resp, err := http.Post(srv.URL, "text/plain", body)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("read=%d", 1<<15), string(got))
}

func mockTLSDisabled(cfg *mockConfig.MockContainable) {
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().GetString("server.tls.cert").Return("").Maybe()
	cfg.EXPECT().GetString("server.tls.key").Return("").Maybe()
	cfg.EXPECT().IsSet("server.http.tls.enabled").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.http.tls.cert").Return(false).Maybe()
	cfg.EXPECT().IsSet("server.http.tls.key").Return(false).Maybe()
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServer(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	require.NotNil(t, srv)

	assert.Equal(t, ":0", srv.Addr)
	assert.Equal(t, readTimeout, srv.ReadTimeout)
	assert.Equal(t, writeTimeout, srv.WriteTimeout)
	assert.Equal(t, idleTimeout, srv.IdleTimeout)
	assert.Equal(t, 1<<20, srv.MaxHeaderBytes, "should default to 1MB")
	assert.NotNil(t, srv.TLSConfig)
}

func TestNewServer_BaseContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "sentinel")

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServer(ctx, cfg, http.DefaultServeMux)
	require.NoError(t, err)
	require.NotNil(t, srv.BaseContext)

	// BaseContext must propagate the parent context to each request.
	baseCtx := srv.BaseContext(nil)
	assert.Equal(t, "sentinel", baseCtx.Value(ctxKey{}))
}

func TestHTTPPortConfig_Specific(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(19876)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServer(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	assert.Equal(t, ":19876", srv.Addr)
}

func TestHTTPPortConfig_Fallback(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(19877)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServer(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	assert.Equal(t, ":19877", srv.Addr)
}

func TestNewServer_MaxHeaderBytes_Configured(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(2048)

	srv, err := NewServer(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	require.NotNil(t, srv)

	assert.Equal(t, 2048, srv.MaxHeaderBytes)
}

func TestHTTPServer_MaxHeaderBytes_Zero(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0)

	srv, err := NewServer(context.Background(), cfg, http.DefaultServeMux)
	require.NoError(t, err)
	assert.Equal(t, 1<<20, srv.MaxHeaderBytes, "zero config value should default to 1MB")
}

func TestHTTPServer_RejectsOversizedHeaders(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	// Use a small MaxHeaderBytes limit so the test does not need to send 1 MB of data.
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetInt("server.port").Return(0).Maybe()
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(100)
	mockTLSDisabled(cfg)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err = Register(context.Background(), "test-http", controller, cfg, testLogger(), http.NewServeMux())
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	require.Eventually(t, func() bool {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d/healthz", port), nil)
		// Go enforces MaxHeaderBytes + 4096 before returning 431, so we need
		// a header value larger than 4196 bytes to trigger the limit of 100.
		req.Header.Set("X-Large-Header", strings.Repeat("a", 5000))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// Connection was reset by server — also indicates oversized headers were rejected.
			return true
		}

		defer func() { _ = resp.Body.Close() }()

		return resp.StatusCode == http.StatusRequestHeaderFieldsTooLarge
	}, 2*time.Second, 50*time.Millisecond, "oversized headers should be rejected with 431")
}

func TestStart_HTTP(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	mockTLSDisabled(cfg)

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
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	mockTLSDisabled(cfg)

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
	require.Error(t, err)
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
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	mockTLSDisabled(cfg)

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
		defer func() { _ = resp.Body.Close() }()
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
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	mockTLSDisabled(cfg)

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
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 50*time.Millisecond)

	// Check /readyz - should be 503
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/readyz", port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusServiceUnavailable
	}, 2*time.Second, 50*time.Millisecond)

	controller.Stop()
	controller.Wait()
}

func TestRegister_WithMiddleware(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	mockTLSDisabled(cfg)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	var middlewareCalled bool
	chain := NewChain(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalled = true
			w.Header().Set("X-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	_, err = Register(context.Background(), "test-http", controller, cfg, testLogger(), mux,
		WithMiddleware(chain),
	)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	// Verify middleware applies to application routes
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/test", port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.Header.Get("X-Middleware") == "applied" && resp.StatusCode == http.StatusOK
	}, 2*time.Second, 50*time.Millisecond)

	assert.True(t, middlewareCalled)
}

func TestRegister_WithMiddleware_HealthEndpointsUnaffected(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	mockTLSDisabled(cfg)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	// Middleware that blocks all requests
	chain := NewChain(func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	})

	_, err = Register(context.Background(), "test-http", controller, cfg, testLogger(), http.NewServeMux(),
		WithMiddleware(chain),
	)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	// Health endpoints should NOT be affected by middleware
	for _, path := range []string{"/healthz", "/livez", "/readyz"} {
		require.Eventually(t, func() bool {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", port, path))
			if err != nil {
				return false
			}
			defer func() { _ = resp.Body.Close() }()
			// Should be 200 (healthy), NOT 403 (blocked by middleware)
			return resp.StatusCode == http.StatusOK
		}, 2*time.Second, 50*time.Millisecond, "health endpoint %s should not be affected by middleware", path)
	}
}
