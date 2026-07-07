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

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

func testLogger() logger.Logger {
	return logger.NewNoop()
}

func TestDefaultMaxGRPCMessageBytes(t *testing.T) {
	t.Parallel()

	// The default must be 1 MiB. Any change to this constant is a
	// behavioural change and requires updating both the spec
	// (2026-04-17-post-audit-hardening-bundle.md) and this test.
	assert.Equal(t, 1<<20, DefaultMaxGRPCMessageBytes,
		"DefaultMaxGRPCMessageBytes must remain 1 MiB")
}

func TestGRPCServer_ReflectionDefaultOff(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	services := srv.GetServiceInfo()
	assert.NotContains(t, services, "grpc.reflection.v1alpha.ServerReflection",
		"reflection must be off by default")
	assert.NotContains(t, services, "grpc.reflection.v1.ServerReflection",
		"reflection must be off by default")
}

func TestNewServer_ReflectionDisabled(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerSettings{Reflection: false})
	require.NoError(t, err)
	assert.NotNil(t, srv)

	services := srv.GetServiceInfo()
	assert.NotContains(t, services, "grpc.reflection.v1alpha.ServerReflection")
	assert.NotContains(t, services, "grpc.reflection.v1.ServerReflection")
}

func TestNewServer_ReflectionEnabled(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerSettings{Reflection: true})
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

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	startFn := Start(testLogger(), srv, ServerSettings{}, gtbtls.Pair{})

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

	srv, err := NewServer(ServerSettings{})
	require.NoError(t, err)

	stopFn := Stop(testLogger(), srv)

	// Should not panic even without a listener
	stopFn(context.Background())
}

func TestRegister(t *testing.T) {
	t.Parallel()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err := Register("test-grpc", controller, testLogger(), ServerSettings{}, gtbtls.Pair{})
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grpc server is nil")
}

func TestStatus_ReportsServeExitError(t *testing.T) {
	t.Parallel()

	state := &serveState{}
	statusFn := serverStatus(&grpc.Server{}, state)

	require.NoError(t, statusFn())

	state.setExit(fmt.Errorf("serve loop died"))

	err := statusFn()
	require.Error(t, err, "Status must report a dead serve loop, not only a nil server")
	assert.Contains(t, err.Error(), "serve loop died")
}

func TestGRPCHealth(t *testing.T) {
	t.Parallel()

	// Get a free port
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	_, err = Register("test-grpc", controller, testLogger(), ServerSettings{Port: port}, gtbtls.Pair{})
	require.NoError(t, err)

	controller.Start()

	// Connect to gRPC health service
	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := grpc_health_v1.NewHealthClient(conn)

	// Check health - should be SERVING initially
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			return false
		}
		return resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
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

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	controller.Register("test-service",
		controls.WithStart(func(_ context.Context) error { return nil }),
		controls.WithStop(func(_ context.Context) {}),
		controls.WithLiveness(func() error { return nil }),
		controls.WithReadiness(func() error { return fmt.Errorf("not ready") }),
	)

	_, err = Register("test-grpc", controller, testLogger(), ServerSettings{Port: port}, gtbtls.Pair{})
	require.NoError(t, err)

	controller.Start()

	// Connect to gRPC health service
	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := grpc_health_v1.NewHealthClient(conn)

	// Check liveness - should be SERVING
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "liveness"})
		if err != nil {
			return false
		}
		return resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
	}, 2*time.Second, 100*time.Millisecond)

	// Check readiness - should be NOT_SERVING
	require.Eventually(t, func() bool {
		resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{Service: "readiness"})
		if err != nil {
			return false
		}
		return resp.GetStatus() == grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}, 2*time.Second, 100*time.Millisecond)

	controller.Stop()
	controller.Wait()
}

func TestStart_UsesSettingsPort(t *testing.T) {
	t.Parallel()

	srv, _ := NewServer(ServerSettings{})
	startFn := Start(testLogger(), srv, ServerSettings{Port: 9090}, gtbtls.Pair{})
	assert.NotNil(t, startFn)
}

func TestStart_UsesEphemeralPortByDefault(t *testing.T) {
	t.Parallel()

	srv, _ := NewServer(ServerSettings{})
	startFn := Start(testLogger(), srv, ServerSettings{}, gtbtls.Pair{})
	assert.NotNil(t, startFn)
}

func TestRegister_WithInterceptors(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	chain := NewInterceptorChain(Interceptor{
		Unary: func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		},
	})

	_, err = Register("test-grpc", controller, testLogger(), ServerSettings{Port: port}, gtbtls.Pair{},
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

	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	chain := NewInterceptorChain(LoggingInterceptor(testLogger()))

	// Mix RegisterOption and grpc.ServerOption
	_, err = Register("test-grpc", controller, testLogger(), ServerSettings{Port: port}, gtbtls.Pair{},
		WithInterceptors(chain),
		grpc.MaxRecvMsgSize(4*1024*1024),
	)
	require.NoError(t, err)
}
