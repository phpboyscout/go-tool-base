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

func TestMustUnmarshalSection(t *testing.T) {
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

	section := config.MustUnmarshalSection[typedProviderConfig](c, "openai")

	assert.True(t, section.Exists)
	assert.Equal(t, "file-key", section.Value.Key)
}

func TestMustUnmarshalSection_PanicsOnDecodeError(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  timeout: definitely-not-a-duration
`)),
	)

	require.Panics(t, func() {
		_ = config.MustUnmarshalSection[typedProviderConfig](c, "openai")
	})
}

func TestObserveSection_InitialUnmarshalAndRegistersObserver(t *testing.T) {
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

	defaults := typedProviderConfig{Timeout: 5 * time.Second}
	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionDefaults(defaults, mergeTypedProviderConfig),
	)
	require.NoError(t, err)

	assert.True(t, binding.Exists())
	assert.Equal(t, "file-key", binding.Value().Key)
	assert.Equal(t, 5*time.Second, binding.Value().Timeout)
	require.NotNil(t, binding.Current())
	assert.Equal(t, binding.Value(), *binding.Current())
	assert.Len(t, c.GetObservers(), 1)
}

func TestObserveSection_RehydratesOnObserver(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: initial-key
`)),
	)

	applied := make([]config.Section[typedProviderConfig], 0, 1)
	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionApply(func(section config.Section[typedProviderConfig]) error {
			applied = append(applied, section)

			return nil
		}),
	)
	require.NoError(t, err)

	initial := binding.Current()
	require.NotNil(t, initial)

	c.Set("openai.key", "reload-key")
	require.NoError(t, runConfigObservers(c))

	assert.Equal(t, "reload-key", binding.Value().Key)
	assert.NotSame(t, initial, binding.Current())
	require.Len(t, applied, 1)
	assert.True(t, applied[0].Exists)
	assert.Equal(t, "reload-key", applied[0].Value.Key)
}

func TestObserveSection_InvalidReloadPreservesPriorSnapshot(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: initial-key
`)),
	)

	applyCalls := 0
	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionValidator(func(next typedProviderConfig) error {
			if next.Key == "invalid-key" {
				return assert.AnError
			}

			return nil
		}),
		config.WithSectionApply(func(config.Section[typedProviderConfig]) error {
			applyCalls++

			return nil
		}),
	)
	require.NoError(t, err)

	previous := binding.Current()
	require.NotNil(t, previous)

	c.Set("openai.key", "invalid-key")
	require.Error(t, runConfigObservers(c))

	assert.Equal(t, "initial-key", binding.Value().Key)
	assert.Same(t, previous, binding.Current())
	assert.Zero(t, applyCalls)
}

func TestObserveSection_DecodeErrorPreservesPriorSnapshot(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  timeout: 5s
`)),
	)

	applyCalls := 0
	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionApply(func(config.Section[typedProviderConfig]) error {
			applyCalls++

			return nil
		}),
	)
	require.NoError(t, err)

	previous := binding.Current()
	require.NotNil(t, previous)

	c.Set("openai.timeout", "not-a-duration")
	require.Error(t, runConfigObservers(c))

	assert.Equal(t, 5*time.Second, binding.Value().Timeout)
	assert.Same(t, previous, binding.Current())
	assert.Zero(t, applyCalls)
}

func runConfigObservers(c *config.Container) error {
	for _, observer := range c.GetObservers() {
		if err := observer.Run(c); err != nil {
			return err
		}
	}

	return nil
}

func mergeTypedProviderConfig(defaults, overlay typedProviderConfig) typedProviderConfig {
	if overlay.Key != "" {
		defaults.Key = overlay.Key
	}
	if overlay.Env != "" {
		defaults.Env = overlay.Env
	}
	if overlay.Timeout != 0 {
		defaults.Timeout = overlay.Timeout
	}

	defaults.Enabled = overlay.Enabled

	return defaults
}
