package gateway_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/gateway"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func testCfg() config.Containable {
	c := config.NewContainerFromViper(logger.NewCharm(io.Discard), viper.New())
	c.Set("server.grpc.port", 50099)

	return c
}

func TestNew_ReturnsHandler(t *testing.T) {
	t.Parallel()

	var called bool

	h, err := gateway.New(context.Background(), testCfg(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			called = true

			return nil
		})

	require.NoError(t, err)
	assert.NotNil(t, h)
	assert.True(t, called, "register func should be invoked")
}

func TestNew_PropagatesRegisterError(t *testing.T) {
	t.Parallel()

	_, err := gateway.New(context.Background(), testCfg(),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			return errors.New("boom")
		})

	require.Error(t, err)
}
