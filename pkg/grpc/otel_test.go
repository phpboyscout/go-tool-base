package grpc

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// OTelStatsHandler must integrate as a server option that NewServer accepts.
// The span/metric emission itself is proven by the HTTP in-memory span test and
// the end-to-end harness run; here we assert the wiring builds a server.
func TestOTelStatsHandlerBuildsServer(t *testing.T) {
	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())

	srv, err := NewServerFromContainable(cfg, OTelStatsHandler())
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

// OTelClientHandler must integrate as a dial option on a client connection. This
// is what the gateway applies so a REST request and the gRPC call it proxies
// share one trace; the propagation itself is proven by the harness run.
func TestOTelClientHandlerBuildsClient(t *testing.T) {
	conn, err := grpc.NewClient("passthrough:///telemetry-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		OTelClientHandler())
	require.NoError(t, err)
	require.NotNil(t, conn)

	_ = conn.Close()
}
