package telemetry

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

// OTelOption configures the OTLP backend.
type OTelOption func(*otelConfig)

type otelConfig struct {
	headers  map[string]string
	insecure bool
	log      logger.Logger
}

// WithOTelHeaders sets HTTP headers sent with every OTLP request (e.g. auth tokens).
func WithOTelHeaders(headers map[string]string) OTelOption {
	return func(c *otelConfig) { c.headers = headers }
}

// WithOTelInsecure disables TLS — use only for local collectors.
func WithOTelInsecure() OTelOption {
	return func(c *otelConfig) { c.insecure = true }
}

// WithOTelLogger sets the logger for routing OTel SDK errors.
func WithOTelLogger(l logger.Logger) OTelOption {
	return func(c *otelConfig) { c.log = l }
}

type otelBackend struct {
	provider *sdklog.LoggerProvider
}

// NewOTelBackend creates a Backend that exports events as OTel log records via OTLP/HTTP.
// endpoint is the base URL of the OTLP HTTP endpoint
// (e.g. "https://collector.example.com:4318").
// A custom OTel error handler is registered to route SDK errors to the provided logger.
func NewOTelBackend(ctx context.Context, endpoint string, opts ...OTelOption) (Backend, error) {
	cfg := &otelConfig{}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.log != nil {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			cfg.log.Debug("OTel SDK error", "error", err)
		}))
	}

	exporterOpts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(endpoint),
	}

	if cfg.insecure {
		exporterOpts = append(exporterOpts, otlploghttp.WithInsecure())
	}

	if len(cfg.headers) > 0 {
		exporterOpts = append(exporterOpts, otlploghttp.WithHeaders(cfg.headers))
	}

	exporter, err := otlploghttp.New(ctx, exporterOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "creating OTLP log exporter")
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	return &otelBackend{provider: provider}, nil
}

func (o *otelBackend) Send(ctx context.Context, events []Event) error {
	l := o.provider.Logger("gtb-telemetry")

	for _, e := range events {
		var rec log.Record
		rec.SetTimestamp(e.Timestamp)
		rec.SetSeverity(log.SeverityInfo)
		rec.SetBody(log.StringValue(e.Name))
		rec.AddAttributes(
			log.String("event.type", string(e.Type)),
			log.String("tool.name", e.ToolName),
			log.String("tool.version", e.Version),
			log.String("host.arch", e.Arch),
			log.String("os.type", e.OS),
			log.String("machine.id", e.MachineID),
		)

		for k, v := range e.Metadata {
			rec.AddAttributes(log.String(k, v))
		}

		l.Emit(ctx, rec)
	}

	return nil
}

const otelShutdownTimeout = 5 * time.Second

func (o *otelBackend) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), otelShutdownTimeout)
	defer cancel()

	return o.provider.Shutdown(ctx)
}
