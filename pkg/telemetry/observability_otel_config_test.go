package telemetry

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/observability/otelcore"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func otelCfgFrom(kv map[string]any) config.Containable {
	v := viper.New()
	for k, val := range kv {
		v.Set(k, val)
	}

	return config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), v)
}

func TestResolveSharedDefaults(t *testing.T) {
	cfg := otelCfgFrom(map[string]any{
		"telemetry.endpoint":        "https://collector:4318",
		"telemetry.insecure":        true,
		"telemetry.tracing.enabled": true,
	})

	s := resolveOTLPSettings(cfg, otelcore.SignalTracing)

	assert.True(t, s.Enabled)
	assert.Equal(t, "https://collector:4318", s.Endpoint)
	assert.True(t, s.Insecure)
}

func TestResolvePerSignalEndpointOverride(t *testing.T) {
	cfg := otelCfgFrom(map[string]any{
		"telemetry.endpoint":         "https://shared:4318",
		"telemetry.metrics.endpoint": "https://metrics:4318",
		"telemetry.metrics.enabled":  true,
	})

	s := resolveOTLPSettings(cfg, otelcore.SignalMetrics)

	assert.Equal(t, "https://metrics:4318", s.Endpoint)
}

func TestResolvePreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_TELEMETRY_ENDPOINT", "https://env-shared:4318")
	t.Setenv("GTB_TELEMETRY_TRACING_ENDPOINT", "https://env-tracing:4318")
	t.Setenv("GTB_TELEMETRY_TRACING_INSECURE", "true")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader("telemetry:\n  endpoint: https://file-shared:4318\n  tracing:\n    enabled: true\n    endpoint: https://file-tracing:4318\n    insecure: false\n")),
	)

	s := resolveOTLPSettings(cfg, otelcore.SignalTracing)

	assert.True(t, s.Enabled)
	assert.Equal(t, "https://env-tracing:4318", s.Endpoint)
	assert.True(t, s.Insecure)
}

func TestResolvePerSignalInsecureOverride(t *testing.T) {
	cfg := otelCfgFrom(map[string]any{
		"telemetry.insecure":      false,
		"telemetry.logs.insecure": true,
		"telemetry.logs.enabled":  true,
	})

	assert.True(t, resolveOTLPSettings(cfg, otelcore.SignalLogs).Insecure)
	assert.False(t, resolveOTLPSettings(cfg, otelcore.SignalTracing).Insecure)
}

func TestResolveEnabledIsPerSignal(t *testing.T) {
	cfg := otelCfgFrom(map[string]any{"telemetry.tracing.enabled": true})

	assert.True(t, resolveOTLPSettings(cfg, otelcore.SignalTracing).Enabled)
	assert.False(t, resolveOTLPSettings(cfg, otelcore.SignalMetrics).Enabled)
}

func TestResolveEmptyEndpointFallsBackToEnv(t *testing.T) {
	cfg := otelCfgFrom(map[string]any{"telemetry.logs.enabled": true})

	s := resolveOTLPSettings(cfg, otelcore.SignalLogs)

	assert.True(t, s.Enabled)
	assert.Empty(t, s.Endpoint, "an empty endpoint lets the SDK read OTEL_* env vars")
}

func TestResolveHeaders(t *testing.T) {
	cfg := otelCfgFrom(map[string]any{
		"telemetry.headers":         map[string]string{"authorization": "Bearer token"},
		"telemetry.tracing.enabled": true,
	})

	s := resolveOTLPSettings(cfg, otelcore.SignalTracing)

	assert.Equal(t, "Bearer token", s.Headers["authorization"])
}

func TestObserveSettingsFromConfigInitialSnapshot(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("telemetry:\n  endpoint: https://shared:4318\n  insecure: true\n  tracing:\n    enabled: true\n")),
	)

	settings, err := ObserveSettingsFromConfig(cfg, otelcore.SignalTracing)
	require.NoError(t, err)

	assert.Equal(t, otelcore.Settings{Enabled: true, Endpoint: "https://shared:4318", Insecure: true}, settings.Value())
	assert.True(t, settings.Exists())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObserveSettingsFromConfigRehydratesSharedDefaults(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("telemetry:\n  endpoint: https://shared:4318\n  tracing:\n    enabled: true\n")),
	)

	changes := make([]config.SectionChange[otelcore.Settings], 0, 1)
	settings, err := ObserveSettingsFromConfig(
		cfg,
		otelcore.SignalTracing,
		config.WithSectionApply(func(change config.SectionChange[otelcore.Settings]) error {
			changes = append(changes, change)

			return nil
		}),
	)
	require.NoError(t, err)

	cfg.Set("telemetry.endpoint", "https://changed:4318")
	require.NoError(t, runOTelCoreConfigObservers(cfg))

	assert.Equal(t, "https://changed:4318", settings.Value().Endpoint)
	assert.Equal(t, uint64(2), settings.Version())
	require.Len(t, changes, 1)
	assert.Equal(t, "https://shared:4318", changes[0].Previous.Value.Endpoint)
	assert.Equal(t, "https://changed:4318", changes[0].Current.Value.Endpoint)
}

func TestObserveSettingsFromConfigRehydratesSignalOverrides(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("telemetry:\n  endpoint: https://shared:4318\n  metrics:\n    enabled: true\n")),
	)

	settings, err := ObserveSettingsFromConfig(cfg, otelcore.SignalMetrics)
	require.NoError(t, err)

	cfg.Set("telemetry.metrics.endpoint", "https://metrics:4318")
	require.NoError(t, runOTelCoreConfigObservers(cfg))

	assert.Equal(t, otelcore.Settings{Enabled: true, Endpoint: "https://metrics:4318"}, settings.Value())
	assert.Equal(t, uint64(2), settings.Version())
}

func TestObserveSettingsFromConfigUnchangedReloadDoesNotIncrementVersion(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("telemetry:\n  endpoint: https://shared:4318\n  logs:\n    enabled: true\n")),
	)

	settings, err := ObserveSettingsFromConfig(cfg, otelcore.SignalLogs)
	require.NoError(t, err)

	cfg.Set("unrelated.value", "changed")
	require.NoError(t, runOTelCoreConfigObservers(cfg))

	assert.Equal(t, otelcore.Settings{Enabled: true, Endpoint: "https://shared:4318"}, settings.Value())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObservedSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := ObserveSettingsFromConfig(otelCfgFrom(map[string]any{
		"telemetry.logs.enabled": true,
	}), otelcore.SignalLogs)
	require.NoError(t, err)

	var source otelcore.SettingsSource = settings
	require.NotNil(t, source.Current())
	assert.True(t, source.Current().Enabled)
	assert.Equal(t, uint64(1), source.Version())
}

func runOTelCoreConfigObservers(c *config.Container) error {
	for _, observer := range c.GetObservers() {
		if err := observer.Run(c); err != nil {
			return err
		}
	}

	return nil
}
