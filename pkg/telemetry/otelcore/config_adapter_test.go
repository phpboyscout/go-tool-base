package otelcore

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func cfgFrom(kv map[string]any) config.Containable {
	v := viper.New()
	for k, val := range kv {
		v.Set(k, val)
	}

	return config.NewContainerFromViper(logger.NewNoop(), v)
}

func TestResolveSharedDefaults(t *testing.T) {
	cfg := cfgFrom(map[string]any{
		"telemetry.endpoint":        "https://collector:4318",
		"telemetry.insecure":        true,
		"telemetry.tracing.enabled": true,
	})

	s := Resolve(cfg, SignalTracing)

	assert.True(t, s.Enabled)
	assert.Equal(t, "https://collector:4318", s.Endpoint)
	assert.True(t, s.Insecure)
}

func TestResolvePerSignalEndpointOverride(t *testing.T) {
	cfg := cfgFrom(map[string]any{
		"telemetry.endpoint":         "https://shared:4318",
		"telemetry.metrics.endpoint": "https://metrics:4318",
		"telemetry.metrics.enabled":  true,
	})

	s := Resolve(cfg, SignalMetrics)

	assert.Equal(t, "https://metrics:4318", s.Endpoint)
}

func TestResolvePreservesEnvAwareSectionUnmarshal(t *testing.T) {
	t.Setenv("GTB_TELEMETRY_ENDPOINT", "https://env-shared:4318")
	t.Setenv("GTB_TELEMETRY_TRACING_ENDPOINT", "https://env-tracing:4318")
	t.Setenv("GTB_TELEMETRY_TRACING_INSECURE", "true")

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithLogger(logger.NewNoop()),
		config.WithConfigFormat("yaml"),
		config.WithEnvPrefix("GTB"),
		config.WithConfigReaders(strings.NewReader("telemetry:\n  endpoint: https://file-shared:4318\n  tracing:\n    enabled: true\n    endpoint: https://file-tracing:4318\n    insecure: false\n")),
	)

	s := Resolve(cfg, SignalTracing)

	assert.True(t, s.Enabled)
	assert.Equal(t, "https://env-tracing:4318", s.Endpoint)
	assert.True(t, s.Insecure)
}

func TestResolvePerSignalInsecureOverride(t *testing.T) {
	cfg := cfgFrom(map[string]any{
		"telemetry.insecure":      false,
		"telemetry.logs.insecure": true,
		"telemetry.logs.enabled":  true,
	})

	assert.True(t, Resolve(cfg, SignalLogs).Insecure)
	assert.False(t, Resolve(cfg, SignalTracing).Insecure)
}

func TestResolveEnabledIsPerSignal(t *testing.T) {
	cfg := cfgFrom(map[string]any{"telemetry.tracing.enabled": true})

	assert.True(t, Resolve(cfg, SignalTracing).Enabled)
	assert.False(t, Resolve(cfg, SignalMetrics).Enabled)
}

func TestResolveEmptyEndpointFallsBackToEnv(t *testing.T) {
	cfg := cfgFrom(map[string]any{"telemetry.logs.enabled": true})

	s := Resolve(cfg, SignalLogs)

	assert.True(t, s.Enabled)
	assert.Empty(t, s.Endpoint, "an empty endpoint lets the SDK read OTEL_* env vars")
}

func TestResolveHeaders(t *testing.T) {
	cfg := cfgFrom(map[string]any{
		"telemetry.headers":         map[string]string{"authorization": "Bearer token"},
		"telemetry.tracing.enabled": true,
	})

	s := Resolve(cfg, SignalTracing)

	assert.Equal(t, "Bearer token", s.Headers["authorization"])
}
