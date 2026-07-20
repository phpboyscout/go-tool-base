package chat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestLoggerFromProps(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, loggerFromProps(nil), "nil props yields a non-nil discard logger")
	assert.NotNil(t, loggerFromProps(&props.Props{Logger: logger.NewNoop()}))
}

func TestFallbackConfigFromProps_NilAndEmpty(t *testing.T) {
	t.Parallel()

	fc, err := fallbackConfigFromProps(nil)
	require.NoError(t, err)
	assert.Zero(t, fc)

	fc, err = fallbackConfigFromProps(&props.Props{})
	require.NoError(t, err)
	assert.Zero(t, fc)
}

func TestNewHardenedChatHTTPClient(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gochat.DefaultChatRequestTimeout, newHardenedChatHTTPClient(0).Timeout,
		"non-positive timeout falls back to the default")
	assert.Equal(t, 3*time.Minute, newHardenedChatHTTPClient(3*time.Minute).Timeout)
}
