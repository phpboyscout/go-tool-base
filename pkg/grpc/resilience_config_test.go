package grpc

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

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    ratelimit:\n      requests_per_second: 12\n      burst: 24\n      max_tracked_keys: 512\n")

	got := RateLimitConfigFromConfig(cfg, "") // empty -> default "server.grpc" prefix

	assert.InDelta(t, 12.0, got.RequestsPerSecond, 0)
	assert.Equal(t, 24, got.Burst)
	assert.Equal(t, 512, got.MaxTrackedKeys)
}

func TestRateLimitConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, DefaultRateLimitConfig(), RateLimitConfigFromConfig(cfg, "server.grpc"))
}

func TestCircuitBreakerConfigFromConfig_ReadsValues(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "server:\n  grpc:\n    circuitbreaker:\n      failure_threshold: 4\n      cooldown: 5s\n      half_open_max_requests: 2\n")

	got := CircuitBreakerConfigFromConfig(cfg, "")

	assert.Equal(t, 4, got.FailureThreshold)
	assert.Equal(t, 5*time.Second, got.Cooldown)
	assert.Equal(t, 2, got.HalfOpenMaxRequests)
}

func TestCircuitBreakerConfigFromConfig_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	cfg := cfgFromYAML(t, "name: x\n")

	assert.Equal(t, DefaultCircuitBreakerConfig(), CircuitBreakerConfigFromConfig(cfg, "server.grpc"))
}
