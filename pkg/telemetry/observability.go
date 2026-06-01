package telemetry

import (
	"context"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/logs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/metrics"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/tracing"
)

// Per-signal tuning keys, layered on the shared telemetry.* keys otelcore resolves.
const (
	configKeySampling = otelcore.Root + ".tracing.sampling"
	configKeyInterval = otelcore.Root + ".metrics.interval"
)

// Shutdown flushes and stops the observability providers that Setup installed.
type Shutdown func(context.Context) error

// signalBuilder builds one observability signal. It returns the provider's
// shutdown and true when the signal is enabled, or (nil, false, nil) when the
// operator has not enabled it.
type signalBuilder func(context.Context, *props.Props, *resource.Resource) (func(context.Context) error, bool, error)

// Setup builds every enabled observability provider (traces, metrics, logs) from
// p.Config, installs them as the OTel globals, sets the W3C propagators so traces
// join across services, and — when a controller is supplied — registers a service
// so the providers flush on graceful stop. A signal whose telemetry.<signal>.enabled
// is false is skipped.
//
// This is the implied-consent path: it is gated only by the operator's
// telemetry.<signal>.enabled configuration, never by the analytics opt-in
// (telemetry.enabled). The two are independent — operational telemetry the
// operator configures is not personal usage data a user must consent to.
//
// The returned Shutdown is for callers without a controller; when a controller is
// supplied it owns shutdown and the return value can be ignored.
func Setup(ctx context.Context, p *props.Props, controller controls.Controllable) (Shutdown, error) {
	res := otelcore.Resource(p.Tool.Name, p.Version.GetVersion())

	// Cross-service trace propagation: W3C trace context + baggage.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	var shutdowns []func(context.Context) error

	for _, build := range []signalBuilder{setupTracing, setupMetrics, setupLogs} {
		sh, enabled, err := build(ctx, p, res)
		if err != nil {
			return nil, err
		}

		if enabled {
			shutdowns = append(shutdowns, sh)
		}
	}

	shutdown := func(ctx context.Context) error {
		var errs error
		for _, fn := range shutdowns {
			errs = errors.CombineErrors(errs, fn(ctx))
		}

		return errs
	}

	if controller != nil && len(shutdowns) > 0 {
		controller.Register("telemetry", controls.WithStop(func(ctx context.Context) {
			if err := shutdown(ctx); err != nil {
				p.Logger.Warn("telemetry shutdown error", "error", err)
			}
		}))
	}

	return shutdown, nil
}

func setupTracing(ctx context.Context, p *props.Props, res *resource.Resource) (func(context.Context) error, bool, error) {
	s := otelcore.Resolve(p.Config, otelcore.SignalTracing)
	if !s.Enabled {
		return nil, false, nil
	}

	var opts []tracing.Option
	if p.Config.IsSet(configKeySampling) {
		opts = append(opts, tracing.WithSampling(p.Config.GetFloat(configKeySampling)))
	}

	tp, err := tracing.NewProvider(ctx, res, s, opts...)
	if err != nil {
		return nil, false, err
	}

	otel.SetTracerProvider(tp)

	return tp.Shutdown, true, nil
}

func setupMetrics(ctx context.Context, p *props.Props, res *resource.Resource) (func(context.Context) error, bool, error) {
	s := otelcore.Resolve(p.Config, otelcore.SignalMetrics)
	if !s.Enabled {
		return nil, false, nil
	}

	var opts []metrics.Option
	if p.Config.IsSet(configKeyInterval) {
		opts = append(opts, metrics.WithInterval(p.Config.GetDuration(configKeyInterval)))
	}

	mp, err := metrics.NewProvider(ctx, res, s, opts...)
	if err != nil {
		return nil, false, err
	}

	otel.SetMeterProvider(mp)

	return mp.Shutdown, true, nil
}

func setupLogs(ctx context.Context, p *props.Props, res *resource.Resource) (func(context.Context) error, bool, error) {
	s := otelcore.Resolve(p.Config, otelcore.SignalLogs)
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
