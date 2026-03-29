package controls_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	gtbgrpc "github.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "github.com/phpboyscout/go-tool-base/pkg/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// httpGet is a helper that performs a GET and returns status code + body.
func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

func newHTTPCfg(t *testing.T, port int) *mockConfig.MockContainable {
	t.Helper()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(port)
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	cfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	cfg.EXPECT().GetString("server.tls.cert").Return("")
	cfg.EXPECT().GetString("server.tls.key").Return("")

	return cfg
}

func newGRPCCfg(t *testing.T, port int) *mockConfig.MockContainable {
	t.Helper()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)

	return cfg
}

func TestHTTP_AllHealthEndpoints(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	_, err := gtbhttp.Register(ctx, "http", controller, newHTTPCfg(t, port), noop, http.NewServeMux())
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	base := fmt.Sprintf("http://localhost:%d", port)

	// Wait for server to be ready
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)

	// All three health endpoints should return 200 with JSON
	for _, endpoint := range []string{"/healthz", "/livez", "/readyz"} {
		status, body := httpGet(t, base+endpoint)
		assert.Equal(t, http.StatusOK, status, "endpoint %s should return 200", endpoint)

		var report controls.HealthReport
		require.NoError(t, json.Unmarshal([]byte(body), &report), "endpoint %s should return valid JSON", endpoint)
		assert.True(t, report.OverallHealthy, "endpoint %s should report healthy", endpoint)
	}
}

func TestHTTP_MiddlewareAppliedToAppRoutes(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	var middlewareCalls atomic.Int64

	mw := gtbhttp.NewChain(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middlewareCalls.Add(1)
			w.Header().Set("X-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "ok")
	})

	_, err := gtbhttp.Register(ctx, "http", controller, newHTTPCfg(t, port), noop, mux,
		gtbhttp.WithMiddleware(mw))
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	base := fmt.Sprintf("http://localhost:%d", port)

	// Wait for ready
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)

	// App route should pass through middleware
	resp, err := http.Get(base + "/api/test")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "applied", resp.Header.Get("X-Middleware"))

	appCalls := middlewareCalls.Load()
	assert.Equal(t, int64(1), appCalls, "middleware should be called for app routes")

	// Health endpoint should NOT pass through middleware
	beforeHealth := middlewareCalls.Load()
	status, _ := httpGet(t, base+"/healthz")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, beforeHealth, middlewareCalls.Load(), "middleware should NOT be called for health endpoints")
}

func TestHTTP_CustomHealthCheck_AffectsEndpoints(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	_, err := gtbhttp.Register(ctx, "http", controller, newHTTPCfg(t, port), noop, http.NewServeMux())
	require.NoError(t, err)

	// Register a readiness check that starts unhealthy
	healthy := &atomic.Bool{}
	require.NoError(t, controller.RegisterHealthCheck(controls.HealthCheck{
		Name: "db-connection",
		Check: func(_ context.Context) controls.CheckResult {
			if healthy.Load() {
				return controls.CheckResult{Status: controls.CheckHealthy}
			}

			return controls.CheckResult{Status: controls.CheckUnhealthy, Message: "db down"}
		},
		Type: controls.CheckTypeReadiness,
	}))

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	base := fmt.Sprintf("http://localhost:%d", port)

	// Wait for server
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return true
	}, 5*time.Second, 50*time.Millisecond)

	// Readiness should be unhealthy
	status, _ := httpGet(t, base+"/readyz")
	assert.Equal(t, http.StatusServiceUnavailable, status, "readyz should be 503 when check is unhealthy")

	// Liveness should still be healthy (db check is readiness-only)
	status, _ = httpGet(t, base+"/livez")
	assert.Equal(t, http.StatusOK, status, "livez should be 200 (db check is readiness-only)")

	// Mark as healthy
	healthy.Store(true)

	status, _ = httpGet(t, base+"/readyz")
	assert.Equal(t, http.StatusOK, status, "readyz should be 200 after check becomes healthy")
}

func TestGRPC_HealthProbes(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	_, err := gtbgrpc.Register(ctx, "grpc", controller, newGRPCCfg(t, port), noop)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := grpc_health_v1.NewHealthClient(conn)

	// Overall health
	require.Eventually(t, func() bool {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 100*time.Millisecond, "overall health should be SERVING")

	// Liveness probe
	require.Eventually(t, func() bool {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "liveness"})
		return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 100*time.Millisecond, "liveness probe should be SERVING")

	// Readiness probe
	require.Eventually(t, func() bool {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "readiness"})
		return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 100*time.Millisecond, "readiness probe should be SERVING")
}

func TestGRPC_WithInterceptors(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	var interceptorCalls atomic.Int64

	chain := gtbgrpc.NewInterceptorChain(gtbgrpc.Interceptor{
		Unary: func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			interceptorCalls.Add(1)

			return handler(ctx, req)
		},
	})

	_, err := gtbgrpc.Register(ctx, "grpc", controller, newGRPCCfg(t, port), noop,
		gtbgrpc.WithInterceptors(chain))
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := grpc_health_v1.NewHealthClient(conn)

	// Make an RPC that passes through the interceptor
	require.Eventually(t, func() bool {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 100*time.Millisecond)

	assert.Positive(t, interceptorCalls.Load(), "interceptor should be called for gRPC health check RPC")
}

func TestGracefulShutdown_StopsAcceptingConnections(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(),
		controls.WithLogger(noop),
		controls.WithShutdownTimeout(5*time.Second))

	_, err := gtbhttp.Register(ctx, "http", controller, newHTTPCfg(t, port), noop, http.NewServeMux())
	require.NoError(t, err)

	controller.Start()

	base := fmt.Sprintf("http://localhost:%d", port)

	// Wait for ready
	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)

	// Initiate shutdown
	controller.Stop()
	controller.Wait()

	// After shutdown, new connections should fail
	_, err = net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 500*time.Millisecond)
	assert.Error(t, err, "should not be able to connect after shutdown")
}

func TestHTTP_AppHandlerServesRequests(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	port := freePort(t)
	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	mux := http.NewServeMux()
	mux.HandleFunc("/api/hello", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"message":"hello world"}`)
	})
	mux.HandleFunc("/api/error", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})

	_, err := gtbhttp.Register(ctx, "http", controller, newHTTPCfg(t, port), noop, mux)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	base := fmt.Sprintf("http://localhost:%d", port)

	require.Eventually(t, func() bool {
		resp, err := http.Get(base + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)

	// Successful endpoint
	status, body := httpGet(t, base+"/api/hello")
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "hello world")

	// Error endpoint
	status, body = httpGet(t, base+"/api/error")
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, body, "internal error")
}
