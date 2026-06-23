package gateway_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/gateway"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// headerMiddleware is a trivial Middleware that stamps a marker header, used to
// prove a chain is (or is not) applied to a given route.
func headerMiddleware(key, value string) gtbhttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(key, value)
			next.ServeHTTP(w, r)
		})
	}
}

// cfgWithGatewayPort returns a config container with the gateway HTTP port set
// to the supplied free port. The gateway's own HTTP server reads
// "server.gateway.port"; the in-process gRPC dial reads "server.grpc.port".
func cfgWithGatewayPort(t *testing.T, port int) config.Containable {
	t.Helper()

	c := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	c.Set("server.gateway.port", port)
	c.Set("server.grpc.port", 50099)

	return c
}

// freePort reserves and immediately releases an ephemeral port, returning the
// number so a server can bind it without a fixed-port collision.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	return port
}

func noopRegister(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
	return nil
}

// TestWithMuxOptions verifies the mux option is threaded through New: a custom
// incoming-header matcher passed via WithMuxOptions must be honoured by the
// resulting handler (the mux is built with the option applied).
func TestWithMuxOptions(t *testing.T) {
	t.Parallel()

	var matcherCalled bool

	opt := gateway.WithMuxOptions(runtime.WithIncomingHeaderMatcher(
		func(key string) (string, bool) {
			matcherCalled = true

			return runtime.DefaultHeaderMatcher(key)
		},
	))

	h, err := gateway.New(context.Background(), testCfg(), noopRegister, opt)
	require.NoError(t, err)
	require.NotNil(t, h)

	// Drive a request through the mux so the incoming-header matcher runs.
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("X-Trace", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.True(t, matcherCalled, "WithMuxOptions header matcher should be invoked")
}

// TestWithDialOptions verifies a caller-supplied dial option is accepted and the
// handler is still constructed successfully.
func TestWithDialOptions(t *testing.T) {
	t.Parallel()

	h, err := gateway.New(context.Background(), testCfg(), noopRegister,
		gateway.WithDialOptions(grpc.WithUserAgent("gtb-gateway-test")),
	)
	require.NoError(t, err)
	assert.NotNil(t, h)
}

// TestWithMiddleware_New verifies WithMiddleware wraps the handler returned by
// New, so every request through it passes through the chain.
func TestWithMiddleware_New(t *testing.T) {
	t.Parallel()

	chain := gtbhttp.NewChain(headerMiddleware("X-Gateway-MW", "1"))

	h, err := gateway.New(context.Background(), testCfg(), noopRegister, gateway.WithMiddleware(chain))
	require.NoError(t, err)
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, "1", rec.Header().Get("X-Gateway-MW"), "the chain should wrap New's handler")
}

// TestWithMiddleware_Register verifies WithMiddleware wraps the REST routes on
// the managed server while leaving the health endpoints outside the chain.
func TestWithMiddleware_Register(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cfg := cfgWithGatewayPort(t, port)
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	chain := gtbhttp.NewChain(headerMiddleware("X-Gateway-MW", "1"))

	_, err := gateway.Register(context.Background(), "test-gateway", controller, cfg,
		logger.NewNoop(), noopRegister, gateway.WithMiddleware(chain))
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	// A gateway route is wrapped by the chain: the marker header is present.
	require.Eventually(t, func() bool {
		resp, getErr := http.Get(fmt.Sprintf("http://localhost:%d/no/such/route", port))
		if getErr != nil {
			return false
		}

		defer func() { _ = resp.Body.Close() }()

		return resp.Header.Get("X-Gateway-MW") == "1"
	}, 3*time.Second, 50*time.Millisecond, "gateway routes should be wrapped by the chain")

	// The health endpoint is mounted outside the chain: no marker header.
	resp, getErr := http.Get(fmt.Sprintf("http://localhost:%d/healthz", port))
	require.NoError(t, getErr)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-Gateway-MW"), "health endpoints must stay outside the middleware chain")
}

// TestNew_DialLocalError forces the in-process gRPC dial to fail by enabling TLS
// with a certificate path that does not exist, so TLSClientCredentials cannot
// build a transport — exercising New's DialLocal error branch.
func TestNew_DialLocalError(t *testing.T) {
	t.Parallel()

	c := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	c.Set("server.grpc.tls.enabled", true)
	c.Set("server.grpc.tls.cert", "/nonexistent/ca.pem")

	_, err := gateway.New(context.Background(), c, noopRegister)
	require.Error(t, err)
}

// TestRegister wires the gateway as its own controller-managed HTTP server and
// drives the controls lifecycle: the health endpoints come up and graceful
// shutdown completes.
func TestRegister(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cfg := cfgWithGatewayPort(t, port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	var registerCalled bool

	srv, err := gateway.Register(context.Background(), "test-gateway", controller, cfg,
		logger.NewNoop(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			registerCalled = true

			return nil
		})
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.True(t, registerCalled, "register func should run during Register")

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	// The controller-managed HTTP server serves the standard health probe.
	require.Eventually(t, func() bool {
		resp, getErr := http.Get(fmt.Sprintf("http://localhost:%d/healthz", port))
		if getErr != nil {
			return false
		}

		defer func() { _ = resp.Body.Close() }()

		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 50*time.Millisecond, "gateway health endpoint should report OK")
}

// TestRegister_ServesGatewayMux verifies the gateway mux is mounted on "/" of
// the managed HTTP server: an unrouted path returns the grpc-gateway 404,
// confirming the mux — not net/http's default mux — handles the request.
func TestRegister_ServesGatewayMux(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cfg := cfgWithGatewayPort(t, port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := gateway.Register(context.Background(), "test-gateway", controller, cfg,
		logger.NewNoop(), noopRegister)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	require.Eventually(t, func() bool {
		resp, getErr := http.Get(fmt.Sprintf("http://localhost:%d/no/such/route", port))
		if getErr != nil {
			return false
		}

		defer func() { _ = resp.Body.Close() }()

		// grpc-gateway's ServeMux answers unrouted requests with 404.
		return resp.StatusCode == http.StatusNotFound
	}, 3*time.Second, 50*time.Millisecond, "gateway mux should handle the root route")
}

// TestRegister_PropagatesNewError verifies Register surfaces a failure from the
// underlying New call (here, the DialLocal TLS error) rather than masking it.
func TestRegister_PropagatesNewError(t *testing.T) {
	t.Parallel()

	c := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	c.Set("server.grpc.tls.enabled", true)
	c.Set("server.grpc.tls.cert", "/nonexistent/ca.pem")

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := gateway.Register(context.Background(), "test-gateway", controller, c,
		logger.NewNoop(), noopRegister)
	require.Error(t, err)
}

// TestRegister_PropagatesRegisterError verifies a register-func error aborts
// Register (via New) without leaving a server registered.
func TestRegister_PropagatesRegisterError(t *testing.T) {
	t.Parallel()

	cfg := cfgWithGatewayPort(t, freePort(t))

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := gateway.Register(context.Background(), "test-gateway", controller, cfg,
		logger.NewNoop(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			return errors.New("register boom")
		})
	require.Error(t, err)
}
