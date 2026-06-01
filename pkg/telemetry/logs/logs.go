// Package logs builds an OpenTelemetry LoggerProvider that batches log records to
// an OTLP/HTTP collector, and an slog.Handler bridge so the GTB logger can emit
// OTel records as well as its human-readable stderr output. It is one of the GTB
// observability signals.
//
// Trace correlation: records emitted within an active span carry trace_id and
// span_id automatically when logged through a context-aware path. GTB's request
// access logs are correlated by the transport logging middleware/interceptor,
// which injects those fields from the request context — so correlation reaches
// both the stderr sink and the OTLP records here.
package logs

import (
	"context"
	"log/slog"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"

	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// NewProvider builds a LoggerProvider that batches log records to the OTLP/HTTP
// collector described by s. When s.Endpoint is empty the exporter falls back to
// the standard OTEL_EXPORTER_OTLP_* environment variables. The caller owns the
// returned provider's Shutdown — typically registered on the controls.Controller.
func NewProvider(
	ctx context.Context,
	res *resource.Resource,
	s otelcore.Settings,
) (*sdklog.LoggerProvider, error) {
	exporter, err := newExporter(ctx, s)
	if err != nil {
		return nil, err
	}

	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	), nil
}

// Handler returns an slog.Handler that emits OTel log records through lp. name is
// the instrumentation scope. Feed it to logger.NewSlog, or tee it beside the
// stderr handler, to ship the application's logs to the collector.
func Handler(lp *sdklog.LoggerProvider, name string) slog.Handler {
	return otelslog.NewHandler(name, otelslog.WithLoggerProvider(lp))
}

func newExporter(ctx context.Context, s otelcore.Settings) (sdklog.Exporter, error) {
	if s.Endpoint == "" {
		// No endpoint configured: let the SDK read OTEL_EXPORTER_OTLP_* env vars.
		exp, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "creating OTLP log exporter")
		}

		return exp, nil
	}

	ep, err := otelcore.ParseEndpoint(s.Endpoint, s.Insecure, s.Headers)
	if err != nil {
		return nil, err
	}

	httpOpts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(ep.Host),
		otlploghttp.WithURLPath(ep.BasePath + "/v1/logs"),
	}

	if ep.Insecure {
		httpOpts = append(httpOpts, otlploghttp.WithInsecure())
	}

	if len(ep.Headers) > 0 {
		httpOpts = append(httpOpts, otlploghttp.WithHeaders(ep.Headers))
	}

	exp, err := otlploghttp.New(ctx, httpOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "creating OTLP log exporter")
	}

	return exp, nil
}
