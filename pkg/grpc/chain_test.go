package grpc

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"gitlab.com/phpboyscout/go/controls"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// This test exercises GTB's own wiring of a transit InterceptorChain through
// Register — the seam this package owns. The chain assembly itself is tested in
// gitlab.com/phpboyscout/go/transit.
func TestInterceptorChain_MultipleInterceptors_Ordering(t *testing.T) {
	t.Parallel()

	var order []string

	mkUnary := func(name string) grpc.UnaryServerInterceptor {
		return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			order = append(order, name)
			return handler(ctx, req)
		}
	}

	chain := NewInterceptorChain(
		Interceptor{Unary: mkUnary("first")},
		Interceptor{Unary: mkUnary("second")},
		Interceptor{Unary: mkUnary("third")},
	)

	// Verify actual execution order via a real gRPC health-check RPC.
	// The health service is registered automatically by Register(), giving us a
	// unary endpoint to call without defining a custom proto service.
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err = Register("chain-order-test", controller, logger.ToSlog(logger.NewNoop()), ServerSettings{Port: port}, gtbtls.Pair{},
		WithInterceptors(chain),
	)
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

	// Issue a unary health-check RPC; this passes through our interceptor chain.
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, []string{"first", "second", "third"}, order,
		"interceptors must execute in the order they were added to the chain")
}
