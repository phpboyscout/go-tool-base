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

type typedFullConfig struct {
	OpenAI typedProviderConfig `mapstructure:"openai"`
}

type complexSectionConfig struct {
	APIKey   string               `json:"api_key"`
	YAMLName string               `yaml:"yaml_name"`
	Count    uint                 `mapstructure:"count"`
	Ratio    float64              `mapstructure:"ratio"`
	Skipped  string               `mapstructure:"-"`
	Inline   complexInlineConfig  `mapstructure:",squash"`
	Pointer  *complexPointerValue `mapstructure:"pointer"`
}

type complexInlineConfig struct {
	InlineName string `mapstructure:"inline_name"`
}

type complexPointerValue struct {
	Value string `mapstructure:"value"`
}

func TestContainer_Unmarshal(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
  enabled: true
`)),
	)

	var out typedFullConfig
	require.NoError(t, c.Unmarshal(&out))

	assert.Equal(t, "file-key", out.OpenAI.Key)
	assert.True(t, out.OpenAI.Enabled)
}

func TestContainer_UnmarshalKey(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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

func TestContainer_UnmarshalKey_RejectsInvalidTargets(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
`)),
	)

	require.ErrorContains(t, c.UnmarshalKey("openai", nil), "nil target")

	var out *typedProviderConfig
	require.ErrorContains(t, c.UnmarshalKey("openai", out), "result must be addressable")
}

func TestContainer_UnmarshalKey_PreservesEnvBinding(t *testing.T) {
	t.Setenv("GTB_OPENAI_KEY", "env-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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

func TestContainer_UnmarshalKey_OverlaysResolvedComplexFields(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
section:
  api_key: file-api-key
  yaml_name: file-yaml-name
  count: 7
  ratio: 1.5
  skipped: should-not-map
  inline_name: inline-value
  pointer:
    value: pointer-value
`)),
	)

	var out complexSectionConfig
	require.NoError(t, c.UnmarshalKey("section", &out))

	assert.Equal(t, "file-api-key", out.APIKey)
	assert.Equal(t, "file-yaml-name", out.YAMLName)
	assert.Equal(t, uint(7), out.Count)
	assert.InDelta(t, 1.5, out.Ratio, 0.001)
	assert.Empty(t, out.Skipped)
	assert.Equal(t, "inline-value", out.Inline.InlineName)
	require.NotNil(t, out.Pointer)
	assert.Equal(t, "pointer-value", out.Pointer.Value)
}

func TestContainer_SectionExists(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
`)),
	)

	assert.True(t, c.SectionExists(""))
	assert.True(t, c.SectionExists("openai"))
	assert.False(t, c.SectionExists("missing"))
}

func TestUnmarshalSection(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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

func TestUnmarshalSection_NilConfig(t *testing.T) {
	t.Parallel()

	section, err := config.UnmarshalSection[typedProviderConfig](nil, "openai")
	require.NoError(t, err)

	assert.False(t, section.Exists)
	assert.Zero(t, section.Value)
}

func TestUnmarshalSection_EnvOnlyNestedValueExists(t *testing.T) {
	t.Setenv("GTB_OPENAI_KEY", "env-only-key")

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
	assert.Equal(t, uint64(1), binding.Version())
	assert.Len(t, c.GetObservers(), 1)
}

func TestObservedSection_ZeroValue(t *testing.T) {
	t.Parallel()

	var binding config.ObservedSection[typedProviderConfig]

	assert.False(t, binding.Exists())
	assert.Zero(t, binding.Value())
	assert.Nil(t, binding.Current())
	assert.Equal(t, uint64(0), binding.Version())
}

func TestObserveSection_DefaultsRequireMergeForExistingSection(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: file-key
`)),
	)

	_, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionDefaults(typedProviderConfig{Timeout: time.Second}, nil),
	)

	require.ErrorContains(t, err, "section defaults require a merge function")
}

func TestObserveSection_DynamicDefaultsRehydrateOnReload(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
defaults:
  timeout: 5s
openai:
  key: file-key
`)),
	)

	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionDefaultFunc(func(cfg config.Containable) typedProviderConfig {
			return typedProviderConfig{Timeout: cfg.GetDuration("defaults.timeout")}
		}, mergeTypedProviderConfig),
	)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, binding.Value().Timeout)

	c.Set("defaults.timeout", "9s")
	require.NoError(t, runConfigObservers(c))

	assert.Equal(t, 9*time.Second, binding.Value().Timeout)
	assert.Equal(t, uint64(2), binding.Version())
}

func TestObserveSection_RehydratesOnObserver(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: initial-key
`)),
	)

	applied := make([]config.SectionChange[typedProviderConfig], 0, 1)
	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionApply(func(change config.SectionChange[typedProviderConfig]) error {
			applied = append(applied, change)

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
	assert.Equal(t, uint64(2), binding.Version())
	require.Len(t, applied, 1)
	assert.True(t, applied[0].Changed)
	assert.False(t, applied[0].Initial)
	assert.Equal(t, uint64(2), applied[0].Version)
	assert.Equal(t, "initial-key", applied[0].Previous.Value.Key)
	assert.True(t, applied[0].Current.Exists)
	assert.Equal(t, "reload-key", applied[0].Current.Value.Key)
}

func TestObserveSection_UnchangedReloadDoesNotApply(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithSectionApply(func(config.SectionChange[typedProviderConfig]) error {
			applyCalls++

			return nil
		}),
	)
	require.NoError(t, err)

	initial := binding.Current()
	require.NotNil(t, initial)

	c.Set("unrelated.value", "changed")
	require.NoError(t, runConfigObservers(c))

	assert.Equal(t, "initial-key", binding.Value().Key)
	assert.Same(t, initial, binding.Current())
	assert.Equal(t, uint64(1), binding.Version())
	assert.Zero(t, applyCalls)
}

func TestObserveSection_CustomEqualityControlsChangeDetection(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(`
openai:
  key: initial-key
  env: INITIAL_ENV
`)),
	)

	applyCalls := 0
	binding, err := config.ObserveSection[typedProviderConfig](
		c,
		"openai",
		config.WithSectionEqual(func(previous, current config.Section[typedProviderConfig]) bool {
			return previous.Exists == current.Exists && previous.Value.Key == current.Value.Key
		}),
		config.WithSectionApply(func(config.SectionChange[typedProviderConfig]) error {
			applyCalls++

			return nil
		}),
	)
	require.NoError(t, err)

	c.Set("openai.env", "ROTATED_ENV_NAME")
	require.NoError(t, runConfigObservers(c))

	assert.Equal(t, "INITIAL_ENV", binding.Value().Env)
	assert.Equal(t, uint64(1), binding.Version())
	assert.Zero(t, applyCalls)

	c.Set("openai.key", "rotated-key")
	require.NoError(t, runConfigObservers(c))

	assert.Equal(t, "rotated-key", binding.Value().Key)
	assert.Equal(t, "ROTATED_ENV_NAME", binding.Value().Env)
	assert.Equal(t, uint64(2), binding.Version())
	assert.Equal(t, 1, applyCalls)
}

func TestObserveSection_InvalidReloadPreservesPriorSnapshot(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithSectionApply(func(config.SectionChange[typedProviderConfig]) error {
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
	assert.Equal(t, uint64(1), binding.Version())
	assert.Zero(t, applyCalls)
}

func TestObserveSection_DecodeErrorPreservesPriorSnapshot(t *testing.T) {
	t.Parallel()

	c := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
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
		config.WithSectionApply(func(config.SectionChange[typedProviderConfig]) error {
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
	assert.Equal(t, uint64(1), binding.Version())
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
