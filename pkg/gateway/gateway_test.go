package gateway_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"gitlab.com/phpboyscout/go-tool-base/pkg/gateway"
)

func testConn(t *testing.T) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient("passthrough:///gateway-test", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func TestNew_ReturnsHandler(t *testing.T) {
	t.Parallel()

	var called bool

	h, err := gateway.New(context.Background(), testConn(t),
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

	_, err := gateway.New(context.Background(), testConn(t),
		func(_ context.Context, _ *runtime.ServeMux, _ *grpc.ClientConn) error {
			return errors.New("boom")
		})

	require.Error(t, err)
}
