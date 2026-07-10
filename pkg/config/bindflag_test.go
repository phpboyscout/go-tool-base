package config_test

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func newReaderContainer(t *testing.T, yaml string) config.Containable {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(bytes.NewReader([]byte(yaml))),
	)
}

func TestContainer_BindPFlag(t *testing.T) {
	t.Parallel()

	t.Run("changed flag overrides config value", func(t *testing.T) {
		t.Parallel()

		c := newReaderContainer(t, "server:\n  port: 8080\n")
		require.Equal(t, 8080, c.GetInt("server.port"))

		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.Int("server-port", 0, "server port")
		require.NoError(t, fs.Parse([]string{"--server-port", "9090"}))

		flag := fs.Lookup("server-port")
		require.True(t, flag.Changed)
		require.NoError(t, c.BindPFlag("server.port", flag))

		assert.Equal(t, 9090, c.GetInt("server.port"))
	})

	t.Run("nil flag returns error", func(t *testing.T) {
		t.Parallel()

		c := newReaderContainer(t, "server:\n  port: 8080\n")
		err := c.BindPFlag("server.port", nil)
		require.Error(t, err)
	})

	t.Run("binds on sub-container with qualified key", func(t *testing.T) {
		t.Parallel()

		c := newReaderContainer(t, "server:\n  port: 8080\n")
		sub := c.Sub("server")
		require.NotNil(t, sub)
		require.Equal(t, 8080, sub.GetInt("port"))

		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.Int("port", 0, "port")
		require.NoError(t, fs.Parse([]string{"--port", "7070"}))

		require.NoError(t, sub.BindPFlag("port", fs.Lookup("port")))
		assert.Equal(t, 7070, sub.GetInt("port"))
		// The bound value is visible from the parent at the qualified key too.
		assert.Equal(t, 7070, c.GetInt("server.port"))
	})
}
