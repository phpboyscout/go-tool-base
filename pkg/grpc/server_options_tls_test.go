package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health/grpc_health_v1"

	"gitlab.com/phpboyscout/go/controls"

	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
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

	ctx := context.Background()
	controller := controls.NewController(ctx, controls.WithoutSignals())
	settings := gtbgrpc.ServerSettings{Port: port}
	tlsPair := gtbtls.Pair{Enabled: true, Cert: certPath, Key: keyPath}

	_, err := gtbgrpc.Register("grpc", controller, logger.ToSlog(logger.NewNoop()), settings, tlsPair,
		gtbgrpc.WithConfigPrefix("server.internal"))
	require.NoError(t, err)

	controller.Start()
	t.Cleanup(func() {
		controller.Stop()
		controller.Wait()
	})

	conn, err := gtbgrpc.DialLocal(settings, tlsPair, gtbgrpc.WithConfigPrefix("server.internal"))
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
