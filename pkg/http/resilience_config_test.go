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

func TestRateLimitConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    ratelimit:\n      requests_per_second: 10\n      burst: 20\n      max_tracked_keys: 256\n")

	got := RateLimitConfigFromConfig(cfg, "") // empty -> default "server.http" prefix

	assert.InDelta(t, 10.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 20, got.Burst)
	assert.Equal(t, 256, got.MaxTrackedKeys)
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

func TestCircuitBreakerConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  http:\n    circuitbreaker:\n      failure_threshold: 7\n      cooldown: 10s\n      half_open_max_requests: 3\n")

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 7, got.FailureThreshold)
	assert.Equal(t, 10*time.Second, got.Cooldown)
	assert.Equal(t, 3, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(cfg, "server.http"))
}
