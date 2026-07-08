package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	googlegrpc "google.golang.org/grpc"

	configmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func cfgFromYAML(t *testing.T, yaml string) config.Containable {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)
}

func prefixedCfgFromYAML(t *testing.T, prefix, yaml string) config.Containable {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix(prefix),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)
}

func TestServerSettingsFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_GRPC_PORT", "19082")
	t.Setenv("GTB_SERVER_GRPC_REFLECTION", "true")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  grpc:\n    port: 19081\n    reflection: false\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 19082, got.Port)
	assert.True(t, got.Reflection)
}

func TestServerSettingsFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n    reflection: true\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 19081, got.Port)
	assert.True(t, got.Reflection)
}

func TestServerSettingsFromConfig_FallsBackToSharedPort(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  port: 18080\n  grpc:\n    reflection: false\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 18080, got.Port)
	assert.False(t, got.Reflection)
}

func TestServerSettingsFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ServerSettings{}, ServerSettingsFromConfig(nil, ""))
}

func TestServerSettingsFromConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.grpc.port").Return(19081).Once()
	cfg.EXPECT().GetBool("server.grpc.reflection").Return(true).Once()

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 19081, got.Port)
	assert.True(t, got.Reflection)
}

func TestObserveServerSettingsFromConfig_InitialSnapshot(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  port: 18080\n  grpc:\n    reflection: true\n")

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	assert.Equal(t, ServerSettings{Port: 18080, Reflection: true}, settings.Value())
	assert.True(t, settings.Exists())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObserveServerSettingsFromConfig_RehydratesSharedPortDefault(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("server:\n  port: 19081\n  grpc: {}\n")),
	)

	changes := make([]config.SectionChange[ServerSettings], 0, 1)
	settings, err := ObserveServerSettingsFromConfig(
		cfg,
		"",
		config.WithSectionApply(func(change config.SectionChange[ServerSettings]) error {
			changes = append(changes, change)

			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, 19081, settings.Value().Port)

	cfg.Set("server.port", 19082)
	require.NoError(t, runGRPCConfigObservers(cfg))

	assert.Equal(t, 19082, settings.Value().Port)
	assert.Equal(t, uint64(2), settings.Version())
	require.Len(t, changes, 1)
	assert.Equal(t, 19081, changes[0].Previous.Value.Port)
	assert.Equal(t, 19082, changes[0].Current.Value.Port)
}

func TestObserveServerSettingsFromConfig_UnchangedReloadDoesNotIncrementVersion(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("server:\n  port: 19081\n  grpc: {}\n")),
	)

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	cfg.Set("unrelated.value", "changed")
	require.NoError(t, runGRPCConfigObservers(cfg))

	assert.Equal(t, 19081, settings.Value().Port)
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObservedServerSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := ObserveServerSettingsFromConfig(cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n"), "")
	require.NoError(t, err)

	var source ServerSettingsSource = settings
	require.NotNil(t, source.Current())
	assert.Equal(t, 19081, source.Current().Port)
	assert.Equal(t, uint64(1), source.Version())
}

func TestNewServerFromContainable_ReturnsConfiguredServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    reflection: true\n")

	srv, err := NewServerFromContainable(cfg, WithConfigPrefix("server.grpc"), googlegrpc.MaxRecvMsgSize(1024))
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestStartFromContainable_ReturnsStartFunc(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n")

	start := StartFromContainable(cfg, logger.NewNoop(), googlegrpc.NewServer(), WithConfigPrefix("server.grpc"))

	assert.NotNil(t, start)
}

func TestDialLocalFromContainable_ReturnsConnection(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n")

	conn, err := DialLocalFromContainable(cfg, WithConfigPrefix("server.grpc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.NotNil(t, conn)
}

func TestRegisterFromContainable_ReturnsServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    reflection: true\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromContainable(
		context.Background(),
		"test-grpc",
		controller,
		cfg,
		logger.NewNoop(),
		WithConfigPrefix("server.grpc"),
		googlegrpc.MaxRecvMsgSize(1024),
	)
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestMergeRateLimitConfig(t *testing.T) {
	t.Parallel()

	base := DefaultRateLimitConfig()
	override := RateLimitConfig{
		RequestsPerSecond: 12,
		Burst:             34,
		MaxTrackedKeys:    56,
	}

	got := MergeRateLimitConfig(base, override, RateLimitConfigOverrides{
		RequestsPerSecond: true,
		MaxTrackedKeys:    true,
	})

	assert.InDelta(t, 12.0, got.RequestsPerSecond, 0)
	assert.Equal(t, base.Burst, got.Burst)
	assert.Equal(t, 56, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    ratelimit:\n      requests_per_second: 12\n      burst: 24\n      max_tracked_keys: 512\n")

	got := RateLimitConfigFromConfig(cfg, "") // empty -> default "server.grpc" prefix

	assert.InDelta(t, 12.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 24, got.Burst)
	assert.Equal(t, 512, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_GRPC_RATELIMIT_REQUESTS_PER_SECOND", "25")
	t.Setenv("GTB_SERVER_GRPC_RATELIMIT_MAX_TRACKED_KEYS", "1024")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  grpc:\n    ratelimit:\n      requests_per_second: 12\n      burst: 24\n      max_tracked_keys: 512\n")

	got := RateLimitConfigFromConfig(cfg, "")

	assert.InDelta(t, 25.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 24, got.Burst)
	assert.Equal(t, 1024, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, DefaultRateLimitConfig(), RateLimitConfigFromConfig(cfg, "server.grpc"))
}

func TestMergeCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	base := DefaultCircuitBreakerConfig()
	override := CircuitBreakerConfig{
		FailureThreshold:    4,
		Cooldown:            5 * time.Second,
		HalfOpenMaxRequests: 2,
	}

	got := MergeCircuitBreakerConfig(base, override, CircuitBreakerConfigOverrides{
		FailureThreshold: true,
		Cooldown:         true,
	})

	assert.Equal(t, 4, got.FailureThreshold)
	assert.Equal(t, 5*time.Second, got.Cooldown)
	assert.Equal(t, base.HalfOpenMaxRequests, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    circuitbreaker:\n      failure_threshold: 4\n      cooldown: 5s\n      half_open_max_requests: 2\n")

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 4, got.FailureThreshold)
	assert.Equal(t, 5*time.Second, got.Cooldown)
	assert.Equal(t, 2, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_GRPC_CIRCUITBREAKER_COOLDOWN", "45s")
	t.Setenv("GTB_SERVER_GRPC_CIRCUITBREAKER_HALF_OPEN_MAX_REQUESTS", "9")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  grpc:\n    circuitbreaker:\n      failure_threshold: 4\n      cooldown: 5s\n      half_open_max_requests: 2\n")

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 4, got.FailureThreshold)
	assert.Equal(t, 45*time.Second, got.Cooldown)
	assert.Equal(t, 9, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(cfg, "server.grpc"))
}

func runGRPCConfigObservers(c *config.Container) error {
	for _, observer := range c.GetObservers() {
		if err := observer.Run(c); err != nil {
			return err
		}
	}

	return nil
}
