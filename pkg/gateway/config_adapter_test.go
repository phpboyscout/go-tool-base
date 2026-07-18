package gateway_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go/controls"
	transportgateway "gitlab.com/phpboyscout/go/transport/gateway"
	transportgrpc "gitlab.com/phpboyscout/go/transport/grpc"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"

	transithttp "gitlab.com/phpboyscout/go/transit/http"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/gateway"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func testCfg() config.Containable {
	c := config.NewContainerFromViper(logger.ToSlog(logger.NewCharm(io.Discard)), viper.New())
	c.Set("server.grpc.port", 50099)

	return c
}

// cfgWithGatewayPort returns a config container with the gateway HTTP port set
// to the supplied free port. The gateway's own HTTP server reads
// "server.gateway.port"; the in-process gRPC dial reads "server.grpc.port".
func cfgWithGatewayPort(t *testing.T, port int) config.Containable {
	t.Helper()

	c := config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), viper.New())
	c.Set("server.gateway.port", port)
	c.Set("server.grpc.port", 50099)

	return c
}

func gatewayCfgFromYAML(t *testing.T, yaml string) *config.Container {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)
}

func TestSettingsFromConfig_ComposesTransportSettings(t *testing.T) {
	t.Parallel()

	cfg := gatewayCfgFromYAML(t, "server:\n  gateway:\n    port: 18081\n  grpc:\n    port: 19081\n    reflection: true\n")

	settings := gateway.SettingsFromConfig(cfg)

	assert.Equal(t, transporthttp.ServerSettings{Port: 18081}, settings.HTTP)
	assert.Equal(t, transportgrpc.ServerSettings{Port: 19081, Reflection: true}, settings.GRPC)
}

func TestObserveSettingsFromConfig_RehydratesTransportSettings(t *testing.T) {
	t.Parallel()

	cfg := gatewayCfgFromYAML(t, "server:\n  gateway:\n    port: 18081\n  grpc:\n    port: 19081\n")

	changes := make([]config.SectionChange[transportgateway.Settings], 0, 2)
	settings, err := gateway.ObserveSettingsFromConfig(
		cfg,
		config.WithSectionApply(func(change config.SectionChange[transportgateway.Settings]) error {
			changes = append(changes, change)

			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, 18081, settings.Value().HTTP.Port)
	assert.Equal(t, 19081, settings.Value().GRPC.Port)

	cfg.Set("server.gateway.port", 18082)
	require.NoError(t, runGatewayConfigObservers(cfg))

	cfg.Set("server.grpc.port", 19082)
	require.NoError(t, runGatewayConfigObservers(cfg))

	assert.Equal(t, 18082, settings.Value().HTTP.Port)
	assert.Equal(t, 19082, settings.Value().GRPC.Port)
	assert.Equal(t, uint64(3), settings.Version())
	require.Len(t, changes, 2)
	assert.Equal(t, 18081, changes[0].Previous.Value.HTTP.Port)
	assert.Equal(t, 18082, changes[0].Current.Value.HTTP.Port)
	assert.Equal(t, 19081, changes[1].Previous.Value.GRPC.Port)
	assert.Equal(t, 19082, changes[1].Current.Value.GRPC.Port)
}

func TestObservedSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := gateway.ObserveSettingsFromConfig(gatewayCfgFromYAML(t, "server:\n  gateway:\n    port: 18081\n"))
	require.NoError(t, err)

	var source transportgateway.SettingsSource = settings
	require.NotNil(t, source.Current())
	assert.Equal(t, 18081, source.Current().HTTP.Port)
	assert.Equal(t, uint64(1), source.Version())
}

func TestNewFromContainable_ReturnsHandler(t *testing.T) {
	t.Parallel()

	var called bool

	h, err := gateway.NewFromContainable(context.Background(), testCfg(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			called = true

			return nil
		})

	require.NoError(t, err)
	assert.NotNil(t, h)
	assert.True(t, called, "register func should be invoked")
}

// TestWithDialOptions verifies a caller-supplied dial option is accepted and the
// handler is still constructed successfully.
func TestWithDialOptions(t *testing.T) {
	t.Parallel()

	h, err := gateway.NewFromContainable(context.Background(), testCfg(), noopRegister,
		gateway.WithDialOptions(grpc.WithUserAgent("gtb-gateway-test")),
	)
	require.NoError(t, err)
	assert.NotNil(t, h)
}

// TestNewFromContainable_DialLocalError forces the in-process gRPC dial to fail
// by enabling TLS with a certificate path that does not exist, so
// TLSClientCredentials cannot build a transport.
// TestWithMuxOptions_HandlerHonoursMatcher verifies a mux option threaded via
// WithMuxOptions is applied to the built handler: a custom incoming-header
// matcher runs when a request is driven through the gateway handler.
func TestWithMuxOptions_HandlerHonoursMatcher(t *testing.T) {
	t.Parallel()

	var matcherCalled bool

	h, err := gateway.NewFromContainable(context.Background(), testCfg(), noopRegister,
		gateway.WithMuxOptions(runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			matcherCalled = true

			return runtime.DefaultHeaderMatcher(key)
		})),
	)
	require.NoError(t, err)
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("X-Trace", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.True(t, matcherCalled, "WithMuxOptions header matcher must be honoured by the handler")
}

// TestNewFromContainable_PropagatesRegisterError forces the transport New to
// fail (its register func errors) and asserts NewFromContainable surfaces the
// error rather than returning a half-built handler.
func TestNewFromContainable_PropagatesRegisterError(t *testing.T) {
	t.Parallel()

	_, err := gateway.NewFromContainable(context.Background(), testCfg(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			return errors.New("register boom")
		})
	require.Error(t, err)
}

// TestNewFromContainable_WithDialOptionsAndMiddleware combines a caller dial
// option with a middleware chain and asserts the chain wraps the built handler.
func TestNewFromContainable_WithDialOptionsAndMiddleware(t *testing.T) {
	t.Parallel()

	chain := transithttp.NewChain(headerMiddleware("X-Gateway-MW", "1"))

	h, err := gateway.NewFromContainable(context.Background(), testCfg(), noopRegister,
		gateway.WithDialOptions(grpc.WithUserAgent("gtb-gateway-test")),
		gateway.WithMiddleware(chain),
	)
	require.NoError(t, err)
	require.NotNil(t, h)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, "1", rec.Header().Get("X-Gateway-MW"), "the middleware chain must wrap the gateway handler")
}

func TestNewFromContainable_DialLocalError(t *testing.T) {
	t.Parallel()

	c := config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), viper.New())
	c.Set("server.grpc.tls.enabled", true)
	c.Set("server.grpc.tls.cert", "/nonexistent/ca.pem")

	_, err := gateway.NewFromContainable(context.Background(), c, noopRegister)
	require.Error(t, err)
}

func TestRegisterFromContainable_PropagatesNewError(t *testing.T) {
	t.Parallel()

	c := config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), viper.New())
	c.Set("server.grpc.tls.enabled", true)
	c.Set("server.grpc.tls.cert", "/nonexistent/ca.pem")

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := gateway.RegisterFromContainable(context.Background(), "test-gateway", controller, c,
		logger.NewNoop(), noopRegister)
	require.Error(t, err)
}

func TestRegisterFromContainable_ReturnsManagedServer(t *testing.T) {
	t.Parallel()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())
	chain := transithttp.NewChain(headerMiddleware("X-Gateway-MW", "1"))

	srv, err := gateway.RegisterFromContainable(
		context.Background(),
		"test-gateway",
		controller,
		cfgWithGatewayPort(t, freePort(t)),
		logger.NewNoop(),
		noopRegister,
		gateway.WithMiddleware(chain),
	)
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestRegisterFromConfig_PropagatesRegisterError(t *testing.T) {
	t.Parallel()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := gateway.RegisterFromConfig(context.Background(), "test-gateway", controller,
		cfgWithGatewayPort(t, freePort(t)), logger.NewNoop(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			return errors.New("register boom")
		})
	require.Error(t, err)
}

func TestRegisterFromConfig_ReturnsManagedServer(t *testing.T) {
	t.Parallel()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())
	chain := transithttp.NewChain(func(next http.Handler) http.Handler { return next })

	srv, err := gateway.RegisterFromConfig(
		context.Background(),
		"test-gateway",
		controller,
		cfgWithGatewayPort(t, freePort(t)),
		logger.NewNoop(),
		noopRegister,
		gateway.WithMiddleware(chain),
	)
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func runGatewayConfigObservers(c *config.Container) error {
	for _, observer := range c.GetObservers() {
		if err := observer.Run(c); err != nil {
			return err
		}
	}

	return nil
}
