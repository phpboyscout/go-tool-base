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

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/controls"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestNewInterceptorChain_Empty(t *testing.T) {
	t.Parallel()

	chain := NewInterceptorChain()
	opts := chain.ServerOptions()

	assert.Empty(t, opts)
}

func TestInterceptorChain_UnaryOnly(t *testing.T) {
	t.Parallel()

	called := false
	chain := NewInterceptorChain(Interceptor{
		Unary: func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			called = true
			return handler(ctx, req)
		},
	})

	opts := chain.ServerOptions()
	assert.Len(t, opts, 1, "should produce one ServerOption for unary interceptors")

	// Verify the interceptor is callable by building a server with the options
	srv := grpc.NewServer(opts...)
	assert.NotNil(t, srv)
	srv.Stop()

	// The called flag can only be verified through an actual RPC, but we
	// verify the chain assembled without error.
	_ = called
}

func TestInterceptorChain_StreamOnly(t *testing.T) {
	t.Parallel()

	chain := NewInterceptorChain(Interceptor{
		Stream: func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, ss)
		},
	})

	opts := chain.ServerOptions()
	assert.Len(t, opts, 1, "should produce one ServerOption for stream interceptors")
}

func TestInterceptorChain_BothUnaryAndStream(t *testing.T) {
	t.Parallel()

	chain := NewInterceptorChain(Interceptor{
		Unary: func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		},
		Stream: func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, ss)
		},
	})

	opts := chain.ServerOptions()
	assert.Len(t, opts, 2, "should produce two ServerOptions: one unary, one stream")
}

func TestInterceptorChain_NilFieldsSkipped(t *testing.T) {
	t.Parallel()

	chain := NewInterceptorChain(
		Interceptor{Unary: nil, Stream: nil},
		Interceptor{
			Unary: func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
				return handler(ctx, req)
			},
		},
	)

	// Only one interceptor with a non-nil unary field
	assert.Len(t, chain.unary, 1)
	assert.Empty(t, chain.stream)
}

func TestInterceptorChain_Append_Immutable(t *testing.T) {
	t.Parallel()

	interceptorA := Interceptor{
		Unary: func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		},
	}
	interceptorB := Interceptor{
		Unary: func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		},
	}

	original := NewInterceptorChain(interceptorA)
	extended := original.Append(interceptorB)

	assert.Len(t, original.unary, 1, "original chain should not be modified")
	assert.Len(t, extended.unary, 2, "extended chain should have both interceptors")
}

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

	// Verify ordering is preserved in the internal slice
	assert.Len(t, chain.unary, 3)

	// Verify actual execution order via a real gRPC health-check RPC.
	// The health service is registered automatically by Register(), giving
	// us a unary endpoint to call without defining a custom proto service.
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err = Register(context.Background(), "chain-order-test", controller, cfg, logger.NewNoop(),
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
