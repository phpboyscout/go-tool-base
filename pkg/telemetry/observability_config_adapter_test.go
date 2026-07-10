package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func testProps(kv map[string]any) *props.Props {
	v := viper.New()
	for k, val := range kv {
		v.Set(k, val)
	}

	return &props.Props{
		Tool:    props.Tool{Name: "macguffinsvc"},
		Version: version.NewInfo("v1.2.3", "abc123", "2026-06-01"),
		Config:  config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), v),
		Logger:  logger.NewNoop(),
	}
}

func TestObservabilitySettingsFromProps(t *testing.T) {
	t.Parallel()

	settings := ObservabilitySettingsFromProps(testProps(map[string]any{
		"telemetry.endpoint":         "http://shared:4318",
		"telemetry.insecure":         true,
		"telemetry.tracing.enabled":  true,
		"telemetry.tracing.endpoint": "http://traces:4318",
		"telemetry.tracing.sampling": 0.25,
		"telemetry.metrics.enabled":  true,
		"telemetry.metrics.interval": "3s",
		"telemetry.logs.enabled":     true,
	}))

	assert.Equal(t, "macguffinsvc", settings.ServiceName)
	assert.Equal(t, "v1.2.3", settings.Version)
	assert.Equal(t, "http://traces:4318", settings.Tracing.OTLP.Endpoint)
	assert.True(t, settings.Tracing.OTLP.Enabled)
	assert.True(t, settings.Tracing.OTLP.Insecure)
	assert.True(t, settings.Tracing.SamplingSet)
	assert.InEpsilon(t, 0.25, settings.Tracing.Sampling, 0.001)
	assert.True(t, settings.Metrics.IntervalSet)
	assert.Equal(t, 3*time.Second, settings.Metrics.Interval)
	assert.Equal(t, "http://shared:4318", settings.Metrics.OTLP.Endpoint)
	assert.True(t, settings.Logs.OTLP.Enabled)
}

// Observability must run on implied consent: enabling a signal does not require
// the analytics opt-in (telemetry.enabled), and vice versa.
func TestSetupFromProps_ObservabilityIndependentOfAnalyticsConsent(t *testing.T) {
	restoreGlobals(t)

	sh, err := SetupFromProps(context.Background(), testProps(map[string]any{
		"telemetry.enabled":          false, // analytics opt-in OFF
		"telemetry.tracing.enabled":  true,  // observability ON
		"telemetry.tracing.endpoint": "http://localhost:4318",
		"telemetry.tracing.insecure": true,
	}), nil)
	require.NoError(t, err)
	assert.NoError(t, sh(context.Background()))
}
