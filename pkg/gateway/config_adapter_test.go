package gateway_test

import (
	"context"
	"io"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/gateway"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func testCfg() config.Containable {
	c := config.NewContainerFromViper(logger.NewCharm(io.Discard), viper.New())
	c.Set("server.grpc.port", 50099)

	return c
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
func TestNewFromContainable_DialLocalError(t *testing.T) {
	t.Parallel()

	c := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	c.Set("server.grpc.tls.enabled", true)
	c.Set("server.grpc.tls.cert", "/nonexistent/ca.pem")

	_, err := gateway.NewFromContainable(context.Background(), c, noopRegister)
	require.Error(t, err)
}

func TestRegisterFromContainable_PropagatesNewError(t *testing.T) {
	t.Parallel()

	c := config.NewContainerFromViper(logger.NewNoop(), viper.New())
	c.Set("server.grpc.tls.enabled", true)
	c.Set("server.grpc.tls.cert", "/nonexistent/ca.pem")

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := gateway.RegisterFromContainable(context.Background(), "test-gateway", controller, c,
		logger.NewNoop(), noopRegister)
	require.Error(t, err)
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
