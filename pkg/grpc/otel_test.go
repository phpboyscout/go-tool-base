package grpc

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// OTelStatsHandler must integrate as a server option that NewServer accepts.
// The span/metric emission itself is proven by the HTTP in-memory span test and
// the end-to-end harness run; here we assert the wiring builds a server.
func TestOTelStatsHandlerBuildsServer(t *testing.T) {
	cfg := config.NewContainerFromViper(logger.NewNoop(), viper.New())

	srv, err := NewServer(cfg, OTelStatsHandler())
	require.NoError(t, err)
	assert.NotNil(t, srv)
}
