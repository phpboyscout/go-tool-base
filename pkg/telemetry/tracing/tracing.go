// Package tracing builds an OpenTelemetry TracerProvider that batches spans to
// an OTLP/HTTP collector, configured from a resolved [otelcore.Settings]. It is
// one of the GTB observability signals; the transport span instrumentation lives
// in pkg/http and pkg/grpc and reads whichever provider is installed globally.
package tracing

import (
	"context"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// DefaultSampling is the parent-based ratio applied when none is configured.
// It is deliberately production-safe: set telemetry.tracing.sampling = 1.0 to
// record every trace while developing or following the tutorial.
const DefaultSampling = 0.1

type config struct {
	sampling float64
}

// Option configures the tracer provider.
type Option func(*config)

// WithSampling sets the parent-based head sampling ratio (0.0–1.0).
// Defaults to [DefaultSampling].
func WithSampling(ratio float64) Option {
	return func(c *config) { c.sampling = ratio }
}

// NewProvider builds a TracerProvider that batches spans to the OTLP/HTTP
// collector described by s. When s.Endpoint is empty the exporter falls back to
// the standard OTEL_EXPORTER_OTLP_* environment variables. Sampling is
// parent-based around a TraceID-ratio sampler, so a sampled trace stays whole
// across services.
//
// The caller owns the returned provider's Shutdown — typically by registering it
// on the controls.Controller so spans flush on graceful stop.
func NewProvider(
	ctx context.Context,
	res *resource.Resource,
	s otelcore.Settings,
	opts ...Option,
) (*sdktrace.TracerProvider, error) {
	cfg := config{sampling: DefaultSampling}
	for _, o := range opts {
		o(&cfg)
	}

	exporter, err := newExporter(ctx, s)
	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.sampling))),
	), nil
}

func newExporter(ctx context.Context, s otelcore.Settings) (sdktrace.SpanExporter, error) {
	if s.Endpoint == "" {
		// No endpoint configured: let the SDK read OTEL_EXPORTER_OTLP_* env vars.
		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "creating OTLP trace exporter")
		}

		return exp, nil
	}

	ep, err := otelcore.ParseEndpoint(s.Endpoint, s.Insecure, s.Headers)
	if err != nil {
		return nil, err
	}

	httpOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(ep.Host),
		otlptracehttp.WithURLPath(ep.BasePath + "/v1/traces"),
	}

	if ep.Insecure {
		httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
	}

	if len(ep.Headers) > 0 {
		httpOpts = append(httpOpts, otlptracehttp.WithHeaders(ep.Headers))
	}

	exp, err := otlptracehttp.New(ctx, httpOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "creating OTLP trace exporter")
	}

	return exp, nil
}
