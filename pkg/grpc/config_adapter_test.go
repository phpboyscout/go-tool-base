package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	googlegrpc "google.golang.org/grpc"

	"gitlab.com/phpboyscout/go/controls"
	transitgrpc "gitlab.com/phpboyscout/go/transit/grpc"
	transportgrpc "gitlab.com/phpboyscout/go/transport/grpc"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func cfgFromYAML(t *testing.T, yaml string) *config.View {
	t.Helper()

	return testutil.ViewFromYAML(t, yaml)
}

func TestServerSettingsFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_GRPC_PORT", "19082")
	t.Setenv("GTB_SERVER_GRPC_REFLECTION", "true")

	cfg := testutil.ViewFromYAML(t,
		"server:\n  grpc:\n    port: 19081\n    reflection: false\n",
		config.WithEnv("GTB"))

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

func TestServerSettingsFromConfig_HostRoundTrips(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n    host: 127.0.0.1\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, "127.0.0.1", got.Host, "the server.grpc.host bind-address key must round-trip")
	assert.Equal(t, 19081, got.Port)
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

	assert.Equal(t, transportgrpc.ServerSettings{}, ServerSettingsFromConfig(nil, ""))
}

// TestServerSettingsFromConfig_MalformedSectionFallsBackToFlatKeys pins the
// decode-error fallback: a section the typed decode rejects resolves per key,
// yielding zero values for the malformed keys instead of failing outright.
func TestServerSettingsFromConfig_MalformedSectionFallsBackToFlatKeys(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: not-a-number\n    reflection: true\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 0, got.Port)
	assert.True(t, got.Reflection)
}

func TestObserveServerSettingsFromConfig_InitialSnapshot(t *testing.T) {
	t.Parallel()

	cfg := testutil.StoreFromYAML(t, "server:\n  port: 18080\n  grpc:\n    reflection: true\n")

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	assert.Equal(t, transportgrpc.ServerSettings{Port: 18080, Reflection: true}, settings.Value())
	assert.True(t, settings.Exists())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObserveServerSettingsFromConfig_RehydratesSharedPortDefault(t *testing.T) {
	t.Parallel()

	cfg, src := testutil.MutableStoreFromYAML(t, "server:\n  port: 19081\n  grpc: {}\n")

	changes := make([]config.SectionChange[transportgrpc.ServerSettings], 0, 1)
	settings, err := ObserveServerSettingsFromConfig(
		cfg,
		"",
		config.WithSectionApply(func(change config.SectionChange[transportgrpc.ServerSettings]) error {
			changes = append(changes, change)

			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, 19081, settings.Value().Port)

	src.Set("server:\n  port: 19082\n  grpc: {}\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, 19082, settings.Value().Port)
	assert.Equal(t, uint64(2), settings.Version())
	require.Len(t, changes, 1)
	assert.Equal(t, 19081, changes[0].Previous.Value.Port)
	assert.Equal(t, 19082, changes[0].Current.Value.Port)
}

func TestObserveServerSettingsFromConfig_UnchangedReloadDoesNotIncrementVersion(t *testing.T) {
	t.Parallel()

	cfg, src := testutil.MutableStoreFromYAML(t, "server:\n  port: 19081\n  grpc: {}\n")

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	src.Set("server:\n  port: 19081\n  grpc: {}\nunrelated:\n  value: changed\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, 19081, settings.Value().Port)
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObservedServerSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := ObserveServerSettingsFromConfig(testutil.StoreFromYAML(t, "server:\n  grpc:\n    port: 19081\n"), "")
	require.NoError(t, err)

	var source transportgrpc.ServerSettingsSource = settings
	require.NotNil(t, source.Current())
	assert.Equal(t, 19081, source.Current().Port)
	assert.Equal(t, uint64(1), source.Version())
}

func TestNewServerFromReader_ReturnsConfiguredServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    reflection: true\n")

	srv, err := NewServerFromReader(cfg, WithConfigPrefix("server.grpc"), googlegrpc.MaxRecvMsgSize(1024))
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestStartFromReader_ReturnsStartFunc(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n")

	start := StartFromReader(cfg, logger.NewNoop(), googlegrpc.NewServer(), WithConfigPrefix("server.grpc"))

	assert.NotNil(t, start)
}

func TestDialLocalFromReader_ReturnsConnection(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n")

	conn, err := DialLocalFromReader(cfg, WithConfigPrefix("server.grpc"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.NotNil(t, conn)
}

func TestRegisterFromReader_ReturnsServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    reflection: true\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromReader(
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

func TestDialLocalFromReader_WithPort(t *testing.T) {
	t.Parallel()

	// No server.grpc block: the default prefix resolves to port 0, then the
	// explicit WithPort drives the dial target.
	cfg := cfgFromYAML(t, "name: x\n")

	conn, err := DialLocalFromReader(cfg, WithPort(19099))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.Equal(t, "localhost:19099", conn.Target(), "explicit WithPort must drive the dial target")
}

func TestDialLocalFromReader_ExplicitPortOverridesConfig(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n")

	conn, err := DialLocalFromReader(cfg, WithPort(19099))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.Equal(t, "localhost:19099", conn.Target(), "WithPort must override the configured port")
}

func TestDialLocalFromReader_ForwardsDialOption(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    port: 19081\n")

	conn, err := DialLocalFromReader(cfg, googlegrpc.WithUserAgent("gtb-dial-test"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.NotNil(t, conn)
	assert.Equal(t, "localhost:19081", conn.Target(), "a caller dial option must not disturb the resolved target")
}

func TestMergeRateLimitConfig(t *testing.T) {
	t.Parallel()

	base := transitgrpc.DefaultRateLimitConfig()
	override := transitgrpc.RateLimitConfig{
		RequestsPerSecond: 12,
		Burst:             34,
		MaxTrackedKeys:    56,
	}

	got := transitgrpc.MergeRateLimitConfig(base, override, transitgrpc.RateLimitConfigOverrides{
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

	cfg := testutil.ViewFromYAML(t, "server:\n  grpc:\n    ratelimit:\n      requests_per_second: 12\n      burst: 24\n      max_tracked_keys: 512\n", config.WithEnv("GTB"))

	got := RateLimitConfigFromConfig(cfg, "")

	assert.InDelta(t, 25.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 24, got.Burst)
	assert.Equal(t, 1024, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, transitgrpc.DefaultRateLimitConfig(), RateLimitConfigFromConfig(cfg, "server.grpc"))
}

func TestMergeCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	base := transitgrpc.DefaultCircuitBreakerConfig()
	override := transitgrpc.CircuitBreakerConfig{
		FailureThreshold:    4,
		Cooldown:            5 * time.Second,
		HalfOpenMaxRequests: 2,
	}

	got := transitgrpc.MergeCircuitBreakerConfig(base, override, transitgrpc.CircuitBreakerConfigOverrides{
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

	cfg := testutil.ViewFromYAML(t, "server:\n  grpc:\n    circuitbreaker:\n      failure_threshold: 4\n      cooldown: 5s\n      half_open_max_requests: 2\n", config.WithEnv("GTB"))

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 4, got.FailureThreshold)
	assert.Equal(t, 45*time.Second, got.Cooldown)
	assert.Equal(t, 9, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, transitgrpc.DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(cfg, "server.grpc"))
}
