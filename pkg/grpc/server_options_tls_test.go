package grpc_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health/grpc_health_v1"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// TestServerOptions_CustomPrefixWithTLS proves the new WithConfigPrefix option
// drives a complete TLS server lifecycle on a non-default config block: the
// server reads its port from "server.internal.port", and DialLocal targets the
// same prefix. This is the two-independent-servers scenario the option exists
// for, exercised over TLS.
func TestServerOptions_CustomPrefixWithTLS(t *testing.T) {
	t.Parallel()

	certPath, keyPath := writeSelfSignedCert(t)
	port := freePort(t)

	cfg := config.NewContainerFromViper(logger.NewCharm(io.Discard), viper.New())
	cfg.Set("server.internal.port", port)
	// TLS settings are shared (server.tls.*) and cascade to every prefix.
	cfg.Set("server.tls.enabled", true)
	cfg.Set("server.tls.cert", certPath)
	cfg.Set("server.tls.key", keyPath)

	ctx := context.Background()
	controller := controls.NewController(ctx, controls.WithoutSignals())

	_, err := gtbgrpc.RegisterFromContainable(ctx, "grpc", controller, cfg, logger.NewCharm(io.Discard),
		gtbgrpc.WithConfigPrefix("server.internal"))
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	conn, err := gtbgrpc.DialLocalFromContainable(cfg, gtbgrpc.WithConfigPrefix("server.internal"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := grpc_health_v1.NewHealthClient(conn).Check(
		callCtx, &grpc_health_v1.HealthCheckRequest{},
	)
	require.NoError(t, err)
	require.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.GetStatus())
}
