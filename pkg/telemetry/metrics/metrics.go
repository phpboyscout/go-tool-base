// Package metrics builds an OpenTelemetry MeterProvider that pushes metrics to an
// OTLP/HTTP collector on a periodic interval, configured from a resolved
// [otelcore.Settings]. It is one of the GTB observability signals; the standard
// server metrics (http.server.*, rpc.server.*) are produced by the otelhttp and
// otelgrpc instrumentation in pkg/http and pkg/grpc, which read whichever
// MeterProvider is installed globally.
package metrics

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// DefaultInterval is the metric export interval applied when none is configured.
const DefaultInterval = 60 * time.Second

type config struct {
	interval time.Duration
}

// Option configures the meter provider.
type Option func(*config)

// WithInterval sets the PeriodicReader export interval. Defaults to [DefaultInterval].
func WithInterval(d time.Duration) Option {
	return func(c *config) { c.interval = d }
}

// NewProvider builds a MeterProvider that pushes metrics to the OTLP/HTTP
// collector described by s on a periodic interval. When s.Endpoint is empty the
// exporter falls back to the standard OTEL_EXPORTER_OTLP_* environment variables.
//
// The caller owns the returned provider's Shutdown — typically by registering it
// on the controls.Controller so the final metric batch flushes on graceful stop.
func NewProvider(
	ctx context.Context,
	res *resource.Resource,
	s otelcore.Settings,
	opts ...Option,
) (*sdkmetric.MeterProvider, error) {
	cfg := config{interval: DefaultInterval}
	for _, o := range opts {
		o(&cfg)
	}

	exporter, err := newExporter(ctx, s)
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(cfg.interval))

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	), nil
}

func newExporter(ctx context.Context, s otelcore.Settings) (sdkmetric.Exporter, error) {
	if s.Endpoint == "" {
		// No endpoint configured: let the SDK read OTEL_EXPORTER_OTLP_* env vars.
		exp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "creating OTLP metric exporter")
		}

		return exp, nil
	}

	ep, err := otelcore.ParseEndpoint(s.Endpoint, s.Insecure, s.Headers)
	if err != nil {
		return nil, err
	}

	httpOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(ep.Host),
		otlpmetrichttp.WithURLPath(ep.BasePath + "/v1/metrics"),
	}

	if ep.Insecure {
		httpOpts = append(httpOpts, otlpmetrichttp.WithInsecure())
	}

	if len(ep.Headers) > 0 {
		httpOpts = append(httpOpts, otlpmetrichttp.WithHeaders(ep.Headers))
	}

	exp, err := otlpmetrichttp.New(ctx, httpOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "creating OTLP metric exporter")
	}

	return exp, nil
}
