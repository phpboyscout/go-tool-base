package telemetry

import (
	"context"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/logs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/metrics"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/tracing"
)

// Shutdown flushes and stops the observability providers that Setup installed.
type Shutdown func(context.Context) error

// signalBuilder builds one observability signal. It returns the provider's
// shutdown and true when the signal is enabled, or (nil, false, nil) when the
// operator has not enabled it.
type signalBuilder func(context.Context, ObservabilitySettings, *resource.Resource) (func(context.Context) error, bool, error)

// Setup builds every enabled observability provider (traces, metrics, logs) from
// typed settings, installs them as the OTel globals, sets the W3C propagators so
// traces join across services, and — when a controller is supplied — registers a
// service so the providers flush on graceful stop. A disabled signal is skipped.
//
// This is the implied-consent path: it is gated only by the operator's
// per-signal observability settings, never by the analytics opt-in
// (telemetry.enabled). The two are independent — operational telemetry the
// operator configures is not personal usage data a user must consent to.
//
// The returned Shutdown is for callers without a controller; when a controller is
// supplied it owns shutdown and the return value can be ignored.
func Setup(ctx context.Context, settings ObservabilitySettings, controller controls.Controllable) (Shutdown, error) {
	log := settings.logger()
	res := otelcore.Resource(settings.ServiceName, settings.Version)

	// Cross-service trace propagation: W3C trace context + baggage.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdowns, err := buildSignals(ctx, settings, res, []signalBuilder{setupTracing, setupMetrics, setupLogs})
	if err != nil {
		return nil, err
	}

	shutdown := func(ctx context.Context) error {
		var errs error
		for _, fn := range shutdowns {
			errs = errors.CombineErrors(errs, fn(ctx))
		}

		return errs
	}

	if controller != nil && len(shutdowns) > 0 {
		// The providers do their work in their own background batch goroutines, so
		// this "service" only needs to stay alive until shutdown and flush on the
		// way out. Start blocks until the context is cancelled (the controller's
		// shutdown signal); the flush happens in Stop. A nil Start would panic the
		// supervisor, which calls it unconditionally.
		controller.Register("telemetry",
			controls.WithStart(func(ctx context.Context) error {
				<-ctx.Done()

				return nil
			}),
			controls.WithStop(func(ctx context.Context) {
				if err := shutdown(ctx); err != nil {
					log.Warn("telemetry shutdown error", "error", err)
				}
			}),
			controls.WithStatus(func() error { return nil }),
		)
	}

	return shutdown, nil
}

// buildSignals runs each signal builder in order, accumulating the shutdown func
// of every provider that successfully installed. If a builder fails, the
// already-installed providers are torn down (reverse order, best-effort) before
// the error is returned, so a partial failure never strands a global provider.
func buildSignals(ctx context.Context, settings ObservabilitySettings, res *resource.Resource, builders []signalBuilder) ([]func(context.Context) error, error) {
	var shutdowns []func(context.Context) error

	for _, build := range builders {
		sh, enabled, err := build(ctx, settings, res)
		if err != nil {
			// A later builder failed after earlier signals already installed
			// their global providers (tracer/meter/logger). Tear those down,
			// best-effort and in reverse install order, so we don't strand
			// providers and their background batch goroutines.
			teardownInstalled(ctx, settings, shutdowns)

			return nil, err
		}

		if enabled {
			shutdowns = append(shutdowns, sh)
		}
	}

	return shutdowns, nil
}

// teardownInstalled runs the shutdown funcs of providers already installed
// before a setup step failed, best-effort and in reverse install order, so a
// partial Setup failure does not strand global providers (and their background
// batch goroutines). Shutdown errors are logged, not returned: the caller is
// already returning the original setup error.
func teardownInstalled(ctx context.Context, settings ObservabilitySettings, shutdowns []func(context.Context) error) {
	log := settings.logger()

	for i := len(shutdowns) - 1; i >= 0; i-- {
		if err := shutdowns[i](ctx); err != nil {
			log.Warn("telemetry setup rollback: provider shutdown error", "error", err)
		}
	}
}

func setupTracing(ctx context.Context, settings ObservabilitySettings, res *resource.Resource) (func(context.Context) error, bool, error) {
	s := settings.Tracing.OTLP
	if !s.Enabled {
		return nil, false, nil
	}

	var opts []tracing.Option
	if settings.Tracing.SamplingSet {
		opts = append(opts, tracing.WithSampling(settings.Tracing.Sampling))
	}

	tp, err := tracing.NewProvider(ctx, res, s, opts...)
	if err != nil {
		return nil, false, err
	}

	otel.SetTracerProvider(tp)

	return tp.Shutdown, true, nil
}

func setupMetrics(ctx context.Context, settings ObservabilitySettings, res *resource.Resource) (func(context.Context) error, bool, error) {
	s := settings.Metrics.OTLP
	if !s.Enabled {
		return nil, false, nil
	}

	var opts []metrics.Option
	if settings.Metrics.IntervalSet {
		opts = append(opts, metrics.WithInterval(settings.Metrics.Interval))
	}

	mp, err := metrics.NewProvider(ctx, res, s, opts...)
	if err != nil {
		return nil, false, err
	}

	otel.SetMeterProvider(mp)

	return mp.Shutdown, true, nil
}

func setupLogs(ctx context.Context, settings ObservabilitySettings, res *resource.Resource) (func(context.Context) error, bool, error) {
	s := settings.Logs.OTLP
	if !s.Enabled {
		return nil, false, nil
	}

	lp, err := logs.NewProvider(ctx, res, s)
	if err != nil {
		return nil, false, err
	}

	otellogglobal.SetLoggerProvider(lp)

	return lp.Shutdown, true, nil
}
