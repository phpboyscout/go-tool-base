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

func testLogger() logger.Logger {
	return logger.NewNoop()
}

func TestGRPCServer_ReflectionDefaultOff(t *testing.T) {
	t.Parallel()

	// GetBool returns false for a missing / zero-value key, which is the default.
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	services := srv.GetServiceInfo()
	assert.NotContains(t, services, "grpc.reflection.v1alpha.ServerReflection",
		"reflection must be off by default")
	assert.NotContains(t, services, "grpc.reflection.v1.ServerReflection",
		"reflection must be off by default")
}

func TestNewServer_ReflectionDisabled(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false)

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	assert.NotNil(t, srv)

	services := srv.GetServiceInfo()
	assert.NotContains(t, services, "grpc.reflection.v1alpha.ServerReflection")
	assert.NotContains(t, services, "grpc.reflection.v1.ServerReflection")
}

func TestNewServer_ReflectionEnabled(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(true)

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	assert.NotNil(t, srv)

	services := srv.GetServiceInfo()
	// reflection service should be registered
	hasReflection := false
	for name := range services {
		if name == "grpc.reflection.v1alpha.ServerReflection" || name == "grpc.reflection.v1.ServerReflection" {
			hasReflection = true
			break
		}
	}
	assert.True(t, hasReflection, "reflection service should be registered")
}

func TestStart_ListenAndServe(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startFn := Start(cfg, testLogger(), srv)

	errCh := make(chan error, 1)
	go func() {
		errCh <- startFn(context.Background())
	}()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Graceful stop should cause Start to return nil
	srv.GracefulStop()

	assert.NoError(t, <-errCh)
}

func TestStop_GracefulStop(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	stopFn := Stop(testLogger(), srv)

	// Should not panic even without a listener
	stopFn(context.Background())
}

func TestRegister(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(0)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := Register(context.Background(), "test-grpc", controller, cfg, testLogger())
	assert.NoError(t, err)
}

func TestStatus_ValidServer(t *testing.T) {
	t.Parallel()
	srv := &grpc.Server{}
	err := Status(srv)()
	assert.NoError(t, err)
}

func TestStatus_NilServer(t *testing.T) {
	t.Parallel()
	err := Status(nil)()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "grpc server is nil")
}

func TestGRPCHealth(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())
	
	_, err = Register(context.Background(), "test-grpc", controller, cfg, testLogger())
	require.NoError(t, err)

	controller.Start()

	// Connect to gRPC health service
	// Use DialContext as Dial is deprecated
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, fmt.Sprintf("localhost:%d", port), 
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err)
	defer conn.Close()

	client := grpc_health_v1.NewHealthClient(conn)

	// Check health - should be SERVING initially
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			return false
		}
		return resp.Status == grpc_health_v1.HealthCheckResponse_SERVING
	}, 2*time.Second, 100*time.Millisecond)

	controller.Stop()
	controller.Wait()
}

func TestGRPCProbes(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())
	
	controller.Register("test-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithLiveness(func() error { return nil }),
		controls.WithReadiness(func() error { return fmt.Errorf("not ready") }),
	)

	_, err = Register(context.Background(), "test-grpc", controller, cfg, testLogger())
	require.NoError(t, err)

	controller.Start()

	// Connect to gRPC health service
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, fmt.Sprintf("localhost:%d", port), 
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err)
	defer conn.Close()

	client := grpc_health_v1.NewHealthClient(conn)

	// Check liveness - should be SERVING
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "liveness"})
		if err != nil {
			return false
		}
		return resp.Status == grpc_health_v1.HealthCheckResponse_SERVING
	}, 2*time.Second, 100*time.Millisecond)

	// Check readiness - should be NOT_SERVING
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "readiness"})
		if err != nil {
			return false
		}
		return resp.Status == grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}, 2*time.Second, 100*time.Millisecond)

	controller.Stop()
	controller.Wait()
}

func TestGRPCPortConfig_Specific(t *testing.T) {
	t.Parallel()
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(9090)
	
	srv, _ := NewServer(cfg)
	startFn := Start(cfg, testLogger(), srv)
	assert.NotNil(t, startFn)
}

func TestGRPCPortConfig_Fallback(t *testing.T) {
	t.Parallel()
	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(0)
	cfg.EXPECT().GetInt("server.port").Return(8080)

	srv, _ := NewServer(cfg)
	startFn := Start(cfg, testLogger(), srv)
	assert.NotNil(t, startFn)
}

func TestRegister_WithInterceptors(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	chain := NewInterceptorChain(Interceptor{
		Unary: func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		},
	})

	_, err = Register(context.Background(), "test-grpc", controller, cfg, testLogger(),
		WithInterceptors(chain),
	)
	require.NoError(t, err)
}

func TestRegister_MixedOptions(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	cfg.EXPECT().GetInt("server.grpc.port").Return(port)

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	chain := NewInterceptorChain(LoggingInterceptor(testLogger()))

	// Mix RegisterOption and grpc.ServerOption
	_, err = Register(context.Background(), "test-grpc", controller, cfg, testLogger(),
		WithInterceptors(chain),
		grpc.MaxRecvMsgSize(4*1024*1024),
	)
	require.NoError(t, err)
}
