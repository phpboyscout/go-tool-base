package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

type typedProviderConfig struct {
	Key     string        `mapstructure:"key"`
	Env     string        `mapstructure:"env"`
	Enabled bool          `mapstructure:"enabled"`
	Timeout time.Duration `mapstructure:"timeout"`
}

func TestContainer_UnmarshalKey(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
  env: OPENAI_API_KEY
  enabled: true
  timeout: 5s
`)),
	)

	var out typedProviderConfig
	require.NoError(t, c.UnmarshalKey("openai", &out))

	assert.Equal(t, "file-key", out.Key)
	assert.Equal(t, "OPENAI_API_KEY", out.Env)
	assert.True(t, out.Enabled)
	assert.Equal(t, 5*time.Second, out.Timeout)
}

func TestContainer_UnmarshalKey_PreservesEnvBinding(t *testing.T) {
	t.Setenv("GTB_OPENAI_KEY", "env-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
  enabled: false
`)),
		config.WithEnvPrefix("GTB"),
	)

	var out typedProviderConfig
	require.NoError(t, c.UnmarshalKey("openai", &out))

	assert.Equal(t, "env-key", out.Key)
	assert.False(t, out.Enabled)
}

func TestContainer_UnmarshalKey_SubContainerPreservesEnvBinding(t *testing.T) {
	t.Setenv("GTB_PROVIDERS_OPENAI_KEY", "nested-env-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
providers:
  openai:
    key: file-key
`)),
		config.WithEnvPrefix("GTB"),
	)

	providers := c.Sub("providers")
	require.NotNil(t, providers)

	var out typedProviderConfig
	require.NoError(t, providers.UnmarshalKey("openai", &out))

	assert.Equal(t, "nested-env-key", out.Key)
}

func TestUnmarshalSection(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
`)),
	)

	section, err := config.UnmarshalSection[typedProviderConfig](c, "openai")
	require.NoError(t, err)

	assert.True(t, section.Exists)
	assert.Equal(t, "file-key", section.Value.Key)

	missing, err := config.UnmarshalSection[typedProviderConfig](c, "anthropic")
	require.NoError(t, err)

	assert.False(t, missing.Exists)
	assert.Zero(t, missing.Value)
}

func TestUnmarshalSection_EnvOnlyNestedValueExists(t *testing.T) {
	t.Setenv("GTB_OPENAI_KEY", "env-only-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("other: value\n")),
		config.WithEnvPrefix("GTB"),
	)

	section, err := config.UnmarshalSection[typedProviderConfig](c, "openai")
	require.NoError(t, err)

	assert.True(t, section.Exists)
	assert.Equal(t, "env-only-key", section.Value.Key)
}
