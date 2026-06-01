package telemetry

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
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
		Config:  config.NewContainerFromViper(logger.NewNoop(), v),
		Logger:  logger.NewNoop(),
	}
}

// restoreGlobals snapshots the OTel globals so a Setup call that installs
// providers does not leak into sibling tests in this package.
func restoreGlobals(t *testing.T) {
	t.Helper()

	tp := otel.GetTracerProvider()
	mp := otel.GetMeterProvider()
	prop := otel.GetTextMapPropagator()

	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		otel.SetTextMapPropagator(prop)
	})
}

func TestSetupAllSignalsDisabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testProps(map[string]any{}), nil)
	require.NoError(t, err)
	require.NotNil(t, sh)
	assert.NoError(t, sh(context.Background()), "no providers, nothing to flush")
}

func TestSetupTracingEnabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testProps(map[string]any{
		"telemetry.tracing.enabled":  true,
		"telemetry.tracing.endpoint": "http://localhost:4318",
		"telemetry.tracing.insecure": true,
	}), nil)
	require.NoError(t, err)
	assert.NoError(t, sh(context.Background()))
}

// Observability must run on implied consent: enabling a signal does not require
// the analytics opt-in (telemetry.enabled), and vice versa.
func TestSetupObservabilityIndependentOfAnalyticsConsent(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testProps(map[string]any{
		"telemetry.enabled":          false, // analytics opt-in OFF
		"telemetry.tracing.enabled":  true,  // observability ON
		"telemetry.tracing.endpoint": "http://localhost:4318",
		"telemetry.tracing.insecure": true,
	}), nil)
	require.NoError(t, err)
	assert.NoError(t, sh(context.Background()))
}

func TestSetupMetricsEnabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testProps(map[string]any{
		"telemetry.metrics.enabled":  true,
		"telemetry.metrics.endpoint": "http://localhost:4318",
		"telemetry.metrics.insecure": true,
		"telemetry.metrics.interval": "1s",
	}), nil)
	require.NoError(t, err)
	_ = sh(context.Background()) // no collector here; the flush export error is expected
}

func TestSetupLogsEnabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testProps(map[string]any{
		"telemetry.logs.enabled":  true,
		"telemetry.logs.endpoint": "http://localhost:4318",
		"telemetry.logs.insecure": true,
	}), nil)
	require.NoError(t, err)
	_ = sh(context.Background())
}

// All signals enabled with a controller exercises the shutdown-registration path.
func TestSetupAllEnabledRegistersControllerShutdown(t *testing.T) {
	restoreGlobals(t)

	controller := controls.NewController(context.Background(), controls.WithLogger(logger.NewNoop()))

	sh, err := Setup(context.Background(), testProps(map[string]any{
		"telemetry.endpoint":        "http://localhost:4318",
		"telemetry.insecure":        true,
		"telemetry.tracing.enabled": true,
		"telemetry.metrics.enabled": true,
		"telemetry.logs.enabled":    true,
	}), controller)
	require.NoError(t, err)
	require.NotNil(t, sh)
}
