package http

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
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
	t.Setenv("GTB_SERVER_HTTP_PORT", "18082")
	t.Setenv("GTB_SERVER_HTTP_MAX_HEADER_BYTES", "4096")

	cfg := prefixedCfgFromYAML(t, "GTB", "server:\n  http:\n    port: 18081\n    max_header_bytes: 2048\n")

	got := ServerSettingsFromConfig(cfg, "")

	assert.Equal(t, 18082, got.Port)
	assert.Equal(t, 4096, got.MaxHeaderBytes)
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

	assert.Equal(t, DefaultRateLimitConfig(), RateLimitConfigFromConfig(cfg, "server.http"))
}

func TestRateLimitConfigFromConfig_CustomPrefix(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "admin:\n  ratelimit:\n    burst: 7\n")

	got := RateLimitConfigFromConfig(cfg, "admin")
	assert.Equal(t, 7, got.Burst)
}

func TestMergeCircuitBreakerConfig(t *testing.T) {
	t.Parallel()

	base := DefaultCircuitBreakerConfig()
	override := CircuitBreakerConfig{
		FailureThreshold:    7,
		Cooldown:            10 * time.Second,
		HalfOpenMaxRequests: 3,
	}

	got := MergeCircuitBreakerConfig(base, override, CircuitBreakerConfigOverrides{
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

	assert.Equal(t, DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(cfg, "server.http"))
}
