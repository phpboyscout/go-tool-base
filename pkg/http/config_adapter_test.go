package http

import (
	"context"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/controls"
	transithttp "gitlab.com/phpboyscout/go/transit/http"
	transporthttp "gitlab.com/phpboyscout/go/transport/http"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func cfgFromYAML(t *testing.T, yaml string) *config.View {
	t.Helper()

	return testutil.ViewFromYAML(t, yaml)
}

func TestServerSettingsFromConfig_PreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_SERVER_HTTP_PORT", "18082")
	t.Setenv("GTB_SERVER_HTTP_MAX_HEADER_BYTES", "4096")

	cfg := testutil.ViewFromYAML(t,
		"server:\n  http:\n    port: 18081\n    max_header_bytes: 2048\n",
		config.WithEnv("GTB"))

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 18082, got.Port)
	assert.Equal(t, 4096, got.MaxHeaderBytes)
}

func TestServerSettingsFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, transporthttp.ServerSettings{}, ServerSettingsFromConfig(nil, ""))
}

func TestServerSettingsFromConfig_HostRoundTrips(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n    host: 127.0.0.1\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, "127.0.0.1", got.Host, "the server.http.host bind-address key must round-trip")
	assert.Equal(t, 18081, got.Port)
}

// TestServerSettingsFromConfig_MalformedSectionFallsBackToFlatKeys pins the
// decode-error fallback: a section the typed decode rejects resolves per key,
// yielding zero values for the malformed keys instead of failing outright.
func TestServerSettingsFromConfig_MalformedSectionFallsBackToFlatKeys(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: not-a-number\n    max_header_bytes: 4096\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 0, got.Port)
	assert.Equal(t, 4096, got.MaxHeaderBytes)
}

func TestObserveServerSettingsFromConfig_InitialSnapshot(t *testing.T) {
	t.Parallel()

	cfg := testutil.StoreFromYAML(t, "server:\n  port: 18080\n  http:\n    max_header_bytes: 4096\n")

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	assert.Equal(t, transporthttp.ServerSettings{Port: 18080, MaxHeaderBytes: 4096}, settings.Value())
	assert.True(t, settings.Exists())
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObserveServerSettingsFromConfig_RehydratesSharedPortDefault(t *testing.T) {
	t.Parallel()

	cfg, src := testutil.MutableStoreFromYAML(t, "server:\n  port: 18080\n  http: {}\n")

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

	src.Set("server:\n  port: 18081\n  http: {}\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, 18081, settings.Value().Port)
	assert.Equal(t, uint64(2), settings.Version())
	require.Len(t, changes, 1)
	assert.Equal(t, 18080, changes[0].Previous.Value.Port)
	assert.Equal(t, 18081, changes[0].Current.Value.Port)
}

func TestObserveServerSettingsFromConfig_UnchangedReloadDoesNotIncrementVersion(t *testing.T) {
	t.Parallel()

	cfg, src := testutil.MutableStoreFromYAML(t, "server:\n  port: 18080\n  http: {}\n")

	settings, err := ObserveServerSettingsFromConfig(cfg, "")
	require.NoError(t, err)

	src.Set("server:\n  port: 18080\n  http: {}\nunrelated:\n  value: changed\n")
	require.NoError(t, cfg.Reload(t.Context()))

	assert.Equal(t, 18080, settings.Value().Port)
	assert.Equal(t, uint64(1), settings.Version())
}

func TestObservedServerSettingsSatisfiesSource(t *testing.T) {
	t.Parallel()

	settings, err := ObserveServerSettingsFromConfig(testutil.StoreFromYAML(t, "server:\n  http:\n    port: 18080\n"), "")
	require.NoError(t, err)

	var source transporthttp.ServerSettingsSource = settings
	require.NotNil(t, source.Current())
	assert.Equal(t, 18080, source.Current().Port)
	assert.Equal(t, uint64(1), source.Version())
}

func TestNewServerFromReader_ReturnsConfiguredServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n    max_header_bytes: 4096\n")

	srv, err := NewServerFromReader(context.Background(), cfg, stdhttp.NewServeMux(), WithConfigPrefix("server.http"))
	require.NoError(t, err)

	assert.NotNil(t, srv)
}

func TestStartFromReader_ReturnsStartFunc(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	srv := &stdhttp.Server{Handler: stdhttp.NewServeMux()}

	start := StartFromReader(cfg, logger.NewNoop(), srv, WithConfigPrefix("server.http"))

	assert.NotNil(t, start)
}

func TestRegisterFromReader_ReturnsServer(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromReader(
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

func TestNewServerFromReader_WithPortBypassesConfig(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	srv, err := NewServerFromReader(context.Background(), cfg, stdhttp.NewServeMux(), WithPort(18099))
	require.NoError(t, err)
	assert.Equal(t, ":18099", srv.Addr, "explicit WithPort must win over the configured port")
}

func TestNewServerFromReader_InvalidPortIsHardError(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	for _, port := range []int{-1, 70000} {
		srv, err := NewServerFromReader(context.Background(), cfg, stdhttp.NewServeMux(), WithPort(port))
		require.Errorf(t, err, "an out-of-range WithPort(%d) must fail construction, not bind an ephemeral port", port)
		assert.Nilf(t, srv, "no server must be returned for out-of-range WithPort(%d)", port)
	}
}

func TestNewServerFromReader_ForwardsTransportServerOption(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	srv, err := NewServerFromReader(context.Background(), cfg, stdhttp.NewServeMux(),
		WithConfigPrefix("server.http"),
		transporthttp.WithReadTimeout(7*time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, ":18081", srv.Addr)
	assert.Equal(t, 7*time.Second, srv.ReadTimeout, "a transport ServerOption must be forwarded to the constructor")
}

func TestRegisterFromReader_InvalidPortIsHardError(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromReader(
		context.Background(), "test-http", controller, cfg, logger.NewNoop(), stdhttp.NewServeMux(),
		WithPort(70000),
	)
	require.Error(t, err, "an out-of-range WithPort must fail Register, not bind an ephemeral port")
	assert.Nil(t, srv, "no server must be registered for an out-of-range WithPort")
}

func TestNewServerFromReader_RejectsUnknownOptionType(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")

	// A value that is neither a GTB ServerOption nor a transport ServerOption
	// (here a bare string standing in for e.g. a mis-targeted interceptor chain)
	// must be rejected, not silently discarded.
	srv, err := NewServerFromReader(context.Background(), cfg, stdhttp.NewServeMux(), "not-an-option")
	require.Error(t, err, "an unsupported option type must fail construction, not be silently dropped")
	assert.Contains(t, err.Error(), "string", "the error must name the rejected concrete type")
	assert.Nil(t, srv)
}

func TestStartFromReader_WarnsOnUnknownOptionType(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	buf := logger.NewBuffer()

	srv, err := NewServerFromReader(context.Background(), cfg, stdhttp.NewServeMux())
	require.NoError(t, err)

	// StartFromReader has no error return, so an unsupported option must at least
	// surface as a WARN naming the offending type rather than vanishing.
	_ = StartFromReader(cfg, buf, srv, 42)
	assert.True(t, buf.Contains("unsupported server option"),
		"StartFromReader must WARN about an unsupported option type; got %v", buf.Messages())
	assert.Truef(t, hasKeyvalContaining(buf.Entries(), "int"),
		"the WARN must name the rejected concrete type; got %v", buf.Entries())
}

// hasKeyvalContaining reports whether any captured entry has a keyval string
// value containing sub.
func hasKeyvalContaining(entries []logger.Entry, sub string) bool {
	for _, e := range entries {
		for _, kv := range e.Keyvals {
			if s, ok := kv.(string); ok && strings.Contains(s, sub) {
				return true
			}
		}
	}

	return false
}

func TestRegisterFromReader_ForwardsTransportOption(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromReader(
		context.Background(), "test-http", controller, cfg, logger.NewNoop(), stdhttp.NewServeMux(),
		transporthttp.WithReadTimeout(5*time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, ":18081", srv.Addr)
	assert.Equal(t, 5*time.Second, srv.ReadTimeout, "a forwarded transport ServerOption must reach the built server")
}

func TestRegisterFromReader_WithConfigPrefixSelectsBlock(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n  admin:\n    port: 18085\n")
	controller := controls.NewController(context.Background(), controls.WithoutSignals())

	srv, err := RegisterFromReader(
		context.Background(), "admin-http", controller, cfg, logger.NewNoop(), stdhttp.NewServeMux(),
		WithConfigPrefix("server.admin"),
	)
	require.NoError(t, err)
	assert.Equal(t, ":18085", srv.Addr, "WithConfigPrefix must select the non-default config block's port")
}

func TestStartFromReader_DefaultPrefix(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    port: 18081\n")
	srv := &stdhttp.Server{Handler: stdhttp.NewServeMux()}

	// No WithConfigPrefix -> the default "server.http" block is resolved.
	start := StartFromReader(cfg, logger.NewNoop(), srv)

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

	cfg := testutil.ViewFromYAML(t, "server:\n  http:\n    ratelimit:\n      requests_per_second: 10\n      burst: 20\n      max_tracked_keys: 256\n", config.WithEnv("GTB"))

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

	cfg := testutil.ViewFromYAML(t, "server:\n  http:\n    circuitbreaker:\n      failure_threshold: 7\n      cooldown: 10s\n      half_open_max_requests: 3\n", config.WithEnv("GTB"))

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
