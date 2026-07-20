package telemetry

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/observability/otelcore"

	"gitlab.com/phpboyscout/go/config"
)

func otelStoreFrom(t *testing.T, yaml string, opts ...config.StoreOption) *config.Store {
	t.Helper()

	store, err := config.NewStore(t.Context(), append([]config.StoreOption{
		config.WithReaders(config.NamedSource{Name: "test.yaml", Content: []byte(yaml)}),
	}, opts...)...)
	require.NoError(t, err)

	return store
}

// otelMutableSource is a backend whose content a test can change before
// calling Store.Reload — the reload idiom the config module's own tests use.
type otelMutableSource struct {
	mu      sync.Mutex
	content []byte
}

func (m *otelMutableSource) ID() string { return "test.yaml" }

func (m *otelMutableSource) Capabilities() config.Capabilities { return config.Capabilities{} }

func (m *otelMutableSource) set(yaml string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.content = []byte(yaml)
}

func (m *otelMutableSource) Load(ctx context.Context, _ []config.Layer) ([]config.Layer, error) {
	m.mu.Lock()
	content := m.content
	m.mu.Unlock()

	return config.NewReaderBackend("test.yaml", content).Load(ctx, nil)
}

func otelMutableStoreFrom(t *testing.T, yaml string) (*config.Store, *otelMutableSource) {
	t.Helper()

	src := &otelMutableSource{content: []byte(yaml)}

	store, err := config.NewStore(t.Context(), config.WithBackend(src))
	require.NoError(t, err)

	return store, src
}

func TestResolveSharedDefaults(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, `
telemetry:
  endpoint: https://collector:4318
  insecure: true
  tracing:
    enabled: true
`).View()

	s := resolveOTLPSettings(cfg, otelcore.SignalTracing)

	assert.True(t, s.Enabled)
	assert.Equal(t, "https://collector:4318", s.Endpoint)
	assert.True(t, s.Insecure)
}

func TestResolvePerSignalEndpointOverride(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, `
telemetry:
  endpoint: https://shared:4318
  metrics:
    endpoint: https://metrics:4318
    enabled: true
`).View()

	s := resolveOTLPSettings(cfg, otelcore.SignalMetrics)

	assert.Equal(t, "https://metrics:4318", s.Endpoint)
}

func TestResolvePreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_TELEMETRY_ENDPOINT", "https://env-shared:4318")
	t.Setenv("GTB_TELEMETRY_TRACING_ENDPOINT", "https://env-tracing:4318")
	t.Setenv("GTB_TELEMETRY_TRACING_INSECURE", "true")

	cfg := otelStoreFrom(t,
		"telemetry:\n  endpoint: https://file-shared:4318\n  tracing:\n    enabled: true\n    endpoint: https://file-tracing:4318\n    insecure: false\n",
		config.WithEnv("GTB")).View()

	s := resolveOTLPSettings(cfg, otelcore.SignalTracing)

	assert.True(t, s.Enabled)
	assert.Equal(t, "https://env-tracing:4318", s.Endpoint)
	assert.True(t, s.Insecure)
}

func TestResolvePerSignalInsecureOverride(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, `
telemetry:
  insecure: false
  logs:
    insecure: true
    enabled: true
`).View()

	assert.True(t, resolveOTLPSettings(cfg, otelcore.SignalLogs).Insecure)
	assert.False(t, resolveOTLPSettings(cfg, otelcore.SignalTracing).Insecure)
}

func TestResolveEnabledIsPerSignal(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, "telemetry:\n  tracing:\n    enabled: true\n").View()

	assert.True(t, resolveOTLPSettings(cfg, otelcore.SignalTracing).Enabled)
	assert.False(t, resolveOTLPSettings(cfg, otelcore.SignalMetrics).Enabled)
}

func TestResolveEmptyEndpointFallsBackToEnv(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, "telemetry:\n  logs:\n    enabled: true\n").View()

	s := resolveOTLPSettings(cfg, otelcore.SignalLogs)

	assert.True(t, s.Enabled)
	assert.Empty(t, s.Endpoint, "an empty endpoint lets the SDK read OTEL_* env vars")
}

func TestResolveHeaders(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, `
telemetry:
  headers:
    authorization: Bearer token
  tracing:
    enabled: true
`).View()

	s := resolveOTLPSettings(cfg, otelcore.SignalTracing)

	assert.Equal(t, "Bearer token", s.Headers["authorization"])
}

func TestObserveSettingsFromConfigInitialSnapshot(t *testing.T) {
	t.Parallel()

	cfg := otelStoreFrom(t, "telemetry:\n  endpoint: https://shared:4318\n  insecure: true\n  tracing:\n    enabled: true\n")

	settings, err := ObserveSettingsFromConfig(cfg, otelcore.SignalTracing)
	require.NoError(t, err)

	assert.Equal(t, otelcore.Settings{Enabled: true, Endpoint: "https://shared:4318", Insecure: true}, settings.Value())
	assert.True(t, settings.Exists())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObserveSettingsFromConfigRehydratesSharedDefaults(t *testing.T) {
	t.Parallel()

	cfg, src := otelMutableStoreFrom(t, "telemetry:\n  endpoint: https://shared:4318\n  tracing:\n    enabled: true\n")

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

	src.set("telemetry:\n  endpoint: https://changed:4318\n  tracing:\n    enabled: true\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, "https://changed:4318", settings.Value().Endpoint)
	assert.Equal(t, uint64(2), settings.Version())
	require.Len(t, changes, 1)
	assert.Equal(t, "https://shared:4318", changes[0].Previous.Value.Endpoint)
	assert.Equal(t, "https://changed:4318", changes[0].Current.Value.Endpoint)
}

func TestObserveSettingsFromConfigRehydratesSignalOverrides(t *testing.T) {
	t.Parallel()

	cfg, src := otelMutableStoreFrom(t, "telemetry:\n  endpoint: https://shared:4318\n  metrics:\n    enabled: true\n")

	settings, err := ObserveSettingsFromConfig(cfg, otelcore.SignalMetrics)
	require.NoError(t, err)

	src.set("telemetry:\n  endpoint: https://shared:4318\n  metrics:\n    enabled: true\n    endpoint: https://metrics:4318\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, otelcore.Settings{Enabled: true, Endpoint: "https://metrics:4318"}, settings.Value())
	assert.Equal(t, uint64(2), settings.Version())
}

func TestObserveSettingsFromConfigUnchangedReloadDoesNotIncrementVersion(t *testing.T) {
	t.Parallel()

	cfg, src := otelMutableStoreFrom(t, "telemetry:\n  endpoint: https://shared:4318\n  logs:\n    enabled: true\n")

	settings, err := ObserveSettingsFromConfig(cfg, otelcore.SignalLogs)
	require.NoError(t, err)

	src.set("telemetry:\n  endpoint: https://shared:4318\n  logs:\n    enabled: true\nunrelated:\n  value: changed\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, otelcore.Settings{Enabled: true, Endpoint: "https://shared:4318"}, settings.Value())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObservedSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := ObserveSettingsFromConfig(
		otelStoreFrom(t, "telemetry:\n  logs:\n    enabled: true\n"), otelcore.SignalLogs)
	require.NoError(t, err)

	var source otelcore.SettingsSource = settings
	require.NotNil(t, source.Current())
	assert.True(t, source.Current().Enabled)
	assert.Equal(t, uint64(1), source.Version())
}
