package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/controls"
	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"

	"gitlab.com/phpboyscout/go/config"
	configmocks "gitlab.com/phpboyscout/go/config/mocks"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func cfgFromYAML(t *testing.T, yaml string) config.Containable {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)
}

func prefixedCfgFromYAML(t *testing.T, prefix, yaml string) config.Containable {
	t.Helper()

	return config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix(prefix),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)
}

func TestServerSettingsFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_HTTP_PORT", "18082")
	t.Setenv("GTB_SERVER_HTTP_MAX_HEADER_BYTES", "4096")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  http:\n    port: 18081\n    max_header_bytes: 2048\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 18082, got.Port)
	assert.Equal(t, 4096, got.MaxHeaderBytes)
}

func TestServerSettingsFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, transporthttp.ServerSettings{}, ServerSettingsFromConfig(nil, ""))
}

func TestServerSettingsFromConfig_LegacyContainable(t *testing.T) {
	t.Parallel()

	cfg := configmocks.NewMockContainable(t)
	cfg.EXPECT().GetInt("server.http.port").Return(18081).Once()
	cfg.EXPECT().GetInt("server.http.max_header_bytes").Return(4096).Once()

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 18081, got.Port)
	assert.Equal(t, 4096, got.MaxHeaderBytes)
}

func TestObserveServerSettingsFromConfig_InitialSnapshot(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  port: 18080\n  http:\n    max_header_bytes: 4096\n")

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	assert.Equal(t, transporthttp.ServerSettings{Port: 18080, MaxHeaderBytes: 4096}, settings.Value())
	assert.True(t, settings.Exists())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObserveServerSettingsFromConfig_RehydratesSharedPortDefault(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("server:\n  port: 18080\n  http: {}\n")),
	)

	changes := make([]config.SectionChange[transporthttp.ServerSettings], 0, 1)
	settings, err := ObserveServerSettingsFromConfig(
		cfg,
		"",
		config.WithSectionApply(func(change config.SectionChange[transporthttp.ServerSettings]) error {
			changes = append(changes, change)

			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, 18080, settings.Value().Port)

	cfg.Set("server.port", 18081)
	require.NoError(t, runHTTPConfigObservers(cfg))

	assert.Equal(t, 18081, settings.Value().Port)
	assert.Equal(t, uint64(2), settings.Version())
	require.Len(t, changes, 1)
	assert.Equal(t, 18080, changes[0].Previous.Value.Port)
	assert.Equal(t, 18081, changes[0].Current.Value.Port)
}

func TestObserveServerSettingsFromConfig_UnchangedReloadDoesNotIncrementVersion(t *testing.T) {
	t.Parallel()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.ToSlog(logger.NewNoop())),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader("server:\n  port: 18080\n  http: {}\n")),
	)

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	cfg.Set("unrelated.value", "changed")
	require.NoError(t, runHTTPConfigObservers(cfg))

	assert.Equal(t, 18080, settings.Value().Port)
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObservedServerSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := ObserveServerSettingsFromConfig(cfgFromYAML(t, "server:\n  http:\n    port: 18080\n"), "")
	require.NoError(t, err)

	var source transporthttp.ServerSettingsSource = settings
	require.NotNil(t, source.Current())
	assert.Equal(t, 18080, source.Current().Port)
	assert.Equal(t, uint64(1), source.Version())
}

func TestNewServerFromContainable_ReturnsConfiguredServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n    max_header_bytes: 4096\n")

	srv, err := NewServerFromContainable(context.Background(), cfg, stdhttp.NewServeMux(), WithConfigPrefix("server.http"))
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestStartFromContainable_ReturnsStartFunc(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	srv := &stdhttp.Server{Handler: stdhttp.NewServeMux()}

	start := StartFromContainable(cfg, logger.NewNoop(), srv, WithConfigPrefix("server.http"))

	assert.NotNil(t, start)
}

func TestRegisterFromContainable_ReturnsServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromContainable(
		context.Background(),
		"test-http",
		controller,
		cfg,
		logger.NewNoop(),
		stdhttp.NewServeMux(),
		WithConfigPrefix("server.http"),
	)
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestNewServerFromContainable_WithPortBypassesConfig(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	srv, err := NewServerFromContainable(context.Background(), cfg, stdhttp.NewServeMux(), WithPort(18099))
	require.NoError(t, err)
	assert.Equal(t, ":18099", srv.Addr, "explicit WithPort must win over the configured port")
}

func TestNewServerFromContainable_InvalidPortFallsBackToEmptySettings(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	for _, port := range []int{-1, 70000} {
		srv, err := NewServerFromContainable(context.Background(), cfg, stdhttp.NewServeMux(), WithPort(port))
		require.NoError(t, err)
		assert.Equalf(t, ":0", srv.Addr, "an out-of-range WithPort(%d) must short-circuit to empty settings", port)
	}
}

func TestNewServerFromContainable_ForwardsTransportServerOption(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	srv, err := NewServerFromContainable(context.Background(), cfg, stdhttp.NewServeMux(),
		WithConfigPrefix("server.http"),
		transporthttp.WithReadTimeout(7*time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, ":18081", srv.Addr)
	assert.Equal(t, 7*time.Second, srv.ReadTimeout, "a transport ServerOption must be forwarded to the constructor")
}

func TestRegisterFromContainable_InvalidPortFallsBackToEmptySettings(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromContainable(
		context.Background(), "test-http", controller, cfg, logger.NewNoop(), stdhttp.NewServeMux(),
		WithPort(70000),
	)
	require.NoError(t, err)
	assert.Equal(t, ":0", srv.Addr, "an out-of-range WithPort must short-circuit Register to empty settings")
}

func TestRegisterFromContainable_ForwardsTransportOption(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromContainable(
		context.Background(), "test-http", controller, cfg, logger.NewNoop(), stdhttp.NewServeMux(),
		transporthttp.WithReadTimeout(5*time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, ":18081", srv.Addr)
	assert.Equal(t, 5*time.Second, srv.ReadTimeout, "a forwarded transport ServerOption must reach the built server")
}

func TestRegisterFromContainable_WithConfigPrefixSelectsBlock(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n  admin:\n    port: 18085\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromContainable(
		context.Background(), "admin-http", controller, cfg, logger.NewNoop(), stdhttp.NewServeMux(),
		WithConfigPrefix("server.admin"),
	)
	require.NoError(t, err)
	assert.Equal(t, ":18085", srv.Addr, "WithConfigPrefix must select the non-default config block's port")
}

func TestStartFromContainable_DefaultPrefix(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	srv := &stdhttp.Server{Handler: stdhttp.NewServeMux()}

	// No WithConfigPrefix -> the default "server.http" block is resolved.
	start := StartFromContainable(cfg, logger.NewNoop(), srv)

	assert.NotNil(t, start)
}

func TestMergeRateLimitConfig(t *testing.T) {
	t.Parallel()

	base := transithttp.DefaultRateLimitConfig()
	override := transithttp.RateLimitConfig{
		RequestsPerSecond: 12,
		Burst:             34,
		MaxTrackedKeys:    56,
	}

	got := transithttp.MergeRateLimitConfig(base, override, transithttp.RateLimitConfigOverrides{
		RequestsPerSecond: true,
		MaxTrackedKeys:    true,
	})

	assert.InDelta(t, 12.0, got.RequestsPerSecond, 0)
	assert.Equal(t, base.Burst, got.Burst)
	assert.Equal(t, 56, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    ratelimit:\n      requests_per_second: 10\n      burst: 20\n      max_tracked_keys: 256\n")

	got := RateLimitConfigFromConfig(cfg, "") // empty -> default "server.http" prefix

	assert.InDelta(t, 10.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 20, got.Burst)
	assert.Equal(t, 256, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_HTTP_RATELIMIT_REQUESTS_PER_SECOND", "25")
	t.Setenv("GTB_SERVER_HTTP_RATELIMIT_MAX_TRACKED_KEYS", "512")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  http:\n    ratelimit:\n      requests_per_second: 10\n      burst: 20\n      max_tracked_keys: 256\n")

	got := RateLimitConfigFromConfig(cfg, "")

	assert.InDelta(t, 25.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 20, got.Burst)
	assert.Equal(t, 512, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, transithttp.DefaultRateLimitConfig(), RateLimitConfigFromConfig(cfg, "server.http"))
}

func TestRateLimitConfigFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, transithttp.DefaultRateLimitConfig(), RateLimitConfigFromConfig(nil, ""))
}

func TestCircuitBreakerConfigFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, transithttp.DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(nil, ""))
}

func TestRateLimitConfigFromConfig_CustomPrefix(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "admin:\n  ratelimit:\n    burst: 7\n")

	got := RateLimitConfigFromConfig(cfg, "admin")
	assert.Equal(t, 7, got.Burst)
}

func TestMergeCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	base := transithttp.DefaultCircuitBreakerConfig()
	override := transithttp.CircuitBreakerConfig{
		FailureThreshold:    7,
		Cooldown:            10 * time.Second,
		HalfOpenMaxRequests: 3,
	}

	got := transithttp.MergeCircuitBreakerConfig(base, override, transithttp.CircuitBreakerConfigOverrides{
		FailureThreshold: true,
		Cooldown:         true,
	})

	assert.Equal(t, 7, got.FailureThreshold)
	assert.Equal(t, 10*time.Second, got.Cooldown)
	assert.Equal(t, base.HalfOpenMaxRequests, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    circuitbreaker:\n      failure_threshold: 7\n      cooldown: 10s\n      half_open_max_requests: 3\n")

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 7, got.FailureThreshold)
	assert.Equal(t, 10*time.Second, got.Cooldown)
	assert.Equal(t, 3, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_HTTP_CIRCUITBREAKER_COOLDOWN", "45s")
	t.Setenv("GTB_SERVER_HTTP_CIRCUITBREAKER_HALF_OPEN_MAX_REQUESTS", "9")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  http:\n    circuitbreaker:\n      failure_threshold: 7\n      cooldown: 10s\n      half_open_max_requests: 3\n")

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 7, got.FailureThreshold)
	assert.Equal(t, 45*time.Second, got.Cooldown)
	assert.Equal(t, 9, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, transithttp.DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(cfg, "server.http"))
}

func runHTTPConfigObservers(c *config.Container) error {
	for _, observer := range c.GetObservers() {
		if err := observer.Run(c); err != nil {
			return err
		}
	}

	return nil
}
