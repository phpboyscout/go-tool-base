package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"

	"gitlab.com/phpboyscout/go/controls"

	"gitlab.com/phpboyscout/go/observability/otelcore"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func testObservabilitySettings(mutators ...func(*ObservabilitySettings)) ObservabilitySettings {
	settings := ObservabilitySettings{
		ServiceName: "macguffinsvc",
		Version:     "v1.2.3",
		Logger:      logger.ToSlog(logger.NewNoop()),
	}
	for _, mutate := range mutators {
		mutate(&settings)
	}

	return settings
}

func withTracing(settings otelcore.Settings) func(*ObservabilitySettings) {
	return func(s *ObservabilitySettings) { s.Tracing.OTLP = settings }
}

func withMetrics(settings otelcore.Settings, interval time.Duration) func(*ObservabilitySettings) {
	return func(s *ObservabilitySettings) {
		s.Metrics.OTLP = settings
		if interval > 0 {
			s.Metrics.Interval = interval
			s.Metrics.IntervalSet = true
		}
	}
}

func withLogs(settings otelcore.Settings) func(*ObservabilitySettings) {
	return func(s *ObservabilitySettings) { s.Logs.OTLP = settings }
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

// TestBuildSignalsRollsBackOnPartialFailure proves that when a later signal
// builder fails, the shutdown funcs of providers installed by earlier builders
// are run (best-effort, reverse order) instead of being stranded.
func TestBuildSignalsRollsBackOnPartialFailure(t *testing.T) {
	restoreGlobals(t)

	var order []int

	// Two builders install successfully (recording a shutdown spy each), the
	// third fails — the first two shutdowns must run, newest-first. The first
	// shutdown returns an error to exercise the best-effort logging branch.
	makeOK := func(id int, shutdownErr error) signalBuilder {
		return func(_ context.Context, _ ObservabilitySettings, _ *resource.Resource) (func(context.Context) error, bool, error) {
			return func(context.Context) error {
				order = append(order, id)

				return shutdownErr
			}, true, nil
		}
	}

	failing := func(_ context.Context, _ ObservabilitySettings, _ *resource.Resource) (func(context.Context) error, bool, error) {
		return nil, false, errors.New("signal install failed")
	}

	shutdowns, err := buildSignals(context.Background(), testObservabilitySettings(), nil,
		[]signalBuilder{makeOK(1, errors.New("shutdown boom")), makeOK(2, nil), failing})

	require.Error(t, err)
	assert.Nil(t, shutdowns, "no shutdowns returned on failure")
	assert.Equal(t, []int{2, 1}, order, "installed providers must be shut down in reverse order")
}

func TestSetupAllSignalsDisabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testObservabilitySettings(), nil)
	require.NoError(t, err)
	require.NotNil(t, sh)
	assert.NoError(t, sh(context.Background()), "no providers, nothing to flush")
}

func TestSetupTracingEnabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testObservabilitySettings(
		withTracing(otelcore.Settings{
			Enabled:  true,
			Endpoint: "http://localhost:4318",
			Insecure: true,
		}),
	), nil)
	require.NoError(t, err)
	assert.NoError(t, sh(context.Background()))
}

func TestSetupMetricsEnabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testObservabilitySettings(
		withMetrics(otelcore.Settings{
			Enabled:  true,
			Endpoint: "http://localhost:4318",
			Insecure: true,
		}, time.Second),
	), nil)
	require.NoError(t, err)
	_ = sh(context.Background()) // no collector here; the flush export error is expected
}

func TestSetupLogsEnabled(t *testing.T) {
	restoreGlobals(t)

	sh, err := Setup(context.Background(), testObservabilitySettings(
		withLogs(otelcore.Settings{
			Enabled:  true,
			Endpoint: "http://localhost:4318",
			Insecure: true,
		}),
	), nil)
	require.NoError(t, err)
	_ = sh(context.Background())
}

// All signals enabled with a controller exercises the shutdown-registration path.
func TestSetupAllEnabledRegistersControllerShutdown(t *testing.T) {
	restoreGlobals(t)

	controller := controls.NewController(context.Background(), controls.WithLogger(logger.ToSlog(logger.NewNoop())))

	otlp := otelcore.Settings{Enabled: true, Endpoint: "http://localhost:4318", Insecure: true}
	sh, err := Setup(context.Background(), testObservabilitySettings(
		withTracing(otlp),
		withMetrics(otlp, 0),
		withLogs(otlp),
	), controller)
	require.NoError(t, err)
	require.NotNil(t, sh)
}

// Regression: the telemetry service must survive the controller actually
// starting it. It used to register with a nil Start, and the supervisor calls
// Start unconditionally — so Start()/Stop()/Wait() panicked the process. This
// test would have caught that (the earlier registration test never started the
// controller).
func TestSetupTelemetryServiceRunsUnderController(t *testing.T) {
	restoreGlobals(t)

	controller := controls.NewController(context.Background(),
		controls.WithoutSignals(), controls.WithLogger(logger.ToSlog(logger.NewNoop())))

	_, err := Setup(context.Background(), testObservabilitySettings(
		withTracing(otelcore.Settings{Enabled: true}), // no endpoint: nothing exported, fully hermetic
	), controller)
	require.NoError(t, err)

	controller.Start()
	controller.Stop()
	controller.Wait()
}
