package controls_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

// freePort binds a listener on :0 to obtain a free port, closes it, and
// returns the port number. There is a small TOCTOU window but it is acceptable
// for test use.
func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	return port
}

// TestHTTPAndGRPC_SeparatePorts starts a controller with both an HTTP server
// and a gRPC server bound to different free ports, exercises the health
// endpoints on both, and verifies the controller shuts down cleanly.
func TestHTTPAndGRPC_SeparatePorts(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "controls")

	httpPort := freePort(t)
	grpcPort := freePort(t)

	ctx := context.Background()
	noop := logger.NewNoop()

	controller := controls.NewController(ctx, controls.WithoutSignals(), controls.WithLogger(noop))

	// Register HTTP server.
	httpCfg := mockConfig.NewMockContainable(t)
	httpCfg.EXPECT().GetInt("server.http.port").Return(httpPort)
	httpCfg.EXPECT().GetInt("server.http.max_header_bytes").Return(0).Maybe()
	httpCfg.EXPECT().GetBool("server.tls.enabled").Return(false)
	httpCfg.EXPECT().GetString("server.tls.cert").Return("")
	httpCfg.EXPECT().GetString("server.tls.key").Return("")

	_, err := gtbhttp.Register(ctx, "http", controller, httpCfg, noop, http.NewServeMux())
	require.NoError(t, err)

	// Register gRPC server.
	grpcCfg := mockConfig.NewMockContainable(t)
	grpcCfg.EXPECT().GetBool("server.grpc.reflection").Return(false).Maybe()
	grpcCfg.EXPECT().GetInt("server.grpc.port").Return(grpcPort)

	_, err = gtbgrpc.Register(ctx, "grpc", controller, grpcCfg, noop)
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	// Verify HTTP /healthz returns 200.
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", httpPort))
		if err != nil {
			return false
		}

		defer resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "HTTP /healthz should return 200")

	// Verify gRPC health Check returns SERVING.
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, fmt.Sprintf("localhost:%d", grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err)

	defer conn.Close()

	healthClient := grpc_health_v1.NewHealthClient(conn)

	require.Eventually(t, func() bool {
		resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			return false
		}

		return resp.Status == grpc_health_v1.HealthCheckResponse_SERVING
	}, 5*time.Second, 100*time.Millisecond, "gRPC health Check should return SERVING")

	assert.Equal(t, controls.Running, controller.GetState())
}
