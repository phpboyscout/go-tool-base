package telemetry

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/redact"
)

// OTelOption configures the OTLP backend.
type OTelOption func(*otelConfig)

type otelConfig struct {
	headers     map[string]string
	insecure    bool
	log         logger.Logger
	serviceName string
	serviceVer  string
	// pendingWarnings captures diagnostics raised during option
	// application. They are emitted by NewOTelBackend once the logger
	// is known — WithOTelLogger may run after WithOTelHeaders, so we
	// cannot log directly from within the option closure.
	pendingWarnings []string
}

// WithOTelHeaders sets HTTP headers sent with every OTLP request (e.g. auth tokens).
//
// Advisory: header keys matching the sensitive pattern (anything
// suggesting a credential — Authorization, X-API-Key, custom names
// containing "auth"/"token"/"secret"/etc.) produce a WARN at backend
// initialisation so operators can audit which headers carry secrets
// and verify their exporter uses TLS. See M-6 in
// docs/development/reports/security-audit-2026-04-17.md.
func WithOTelHeaders(headers map[string]string) OTelOption {
	return func(c *otelConfig) {
		c.headers = headers

		for k := range headers {
			if redact.IsSensitiveHeaderKey(k) {
				c.pendingWarnings = append(c.pendingWarnings,
					"OTel header "+k+" appears to carry credentials; "+
						"ensure the exporter uses TLS and that any HTTP "+
						"middleware logging headers redacts this name. "+
						"See docs/components/telemetry.md.")
			}
		}
	}
}

// WithOTelInsecure disables TLS — use only for local collectors.
func WithOTelInsecure() OTelOption {
	return func(c *otelConfig) { c.insecure = true }
}

// WithOTelLogger sets the logger for routing OTel SDK errors.
func WithOTelLogger(l logger.Logger) OTelOption {
	return func(c *otelConfig) { c.log = l }
}

// WithOTelService sets the service.name and service.version resource attributes.
// These appear as labels in Grafana/Loki and identify the tool in the telemetry data.
func WithOTelService(name, version string) OTelOption {
	return func(c *otelConfig) {
		c.serviceName = name
		c.serviceVer = version
	}
}

type otelBackend struct {
	provider *sdklog.LoggerProvider
}

// NewOTelBackend creates a Backend that exports events as OTel log records via OTLP/HTTP.
// endpoint is the full URL of the OTLP HTTP endpoint
// (e.g. "https://otlp-gateway-prod-eu-west-0.grafana.net/otlp").
// The URL is parsed into host and path components — the SDK appends /v1/logs
// to the path automatically.
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

		for _, w := range cfg.pendingWarnings {
			cfg.log.Warn(w)
		}
	}

	exporterOpts, err := buildOTelExporterOpts(endpoint, cfg)
	if err != nil {
		return nil, err
	}

	exporter, err := otlploghttp.New(ctx, exporterOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "creating OTLP log exporter")
	}

	providerOpts := []sdklog.LoggerProviderOption{
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	}

	if cfg.serviceName != "" {
		res := resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.serviceName),
			semconv.ServiceVersion(cfg.serviceVer),
		)
		providerOpts = append(providerOpts, sdklog.WithResource(res))
	}

	provider := sdklog.NewLoggerProvider(providerOpts...)

	return &otelBackend{provider: provider}, nil
}

func buildOTelExporterOpts(endpoint string, cfg *otelConfig) ([]otlploghttp.Option, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.Wrap(err, "parsing OTLP endpoint URL")
	}

	exporterOpts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(u.Host),
	}

	// Append /v1/logs to the base path so the SDK sends to the correct route.
	urlPath := u.Path + "/v1/logs"
	exporterOpts = append(exporterOpts, otlploghttp.WithURLPath(urlPath))

	if u.Scheme == "http" || cfg.insecure {
		exporterOpts = append(exporterOpts, otlploghttp.WithInsecure())
	}

	if len(cfg.headers) > 0 {
		exporterOpts = append(exporterOpts, otlploghttp.WithHeaders(cfg.headers))
	}

	return exporterOpts, nil
}

func (o *otelBackend) Send(ctx context.Context, events []Event) error {
	l := o.provider.Logger("gtb-telemetry")

	for _, e := range events {
		var rec log.Record
		rec.SetTimestamp(e.Timestamp)
		rec.SetSeverity(log.SeverityInfo)
		rec.SetBody(log.StringValue(e.Name))
		rec.AddAttributes(
			log.String("event.name", e.Name),
			log.String("event.type", string(e.Type)),
			log.String("tool.name", e.ToolName),
			log.String("tool.version", e.Version),
			log.String("host.arch", e.Arch),
			log.String("os.type", e.OS),
			log.String("os.version", e.OSVersion),
			log.String("go.version", e.GoVersion),
			log.String("machine.id", e.MachineID),
			log.Int64("command.duration_ms", e.DurationMs),
			log.Int("command.exit_code", e.ExitCode),
		)

		// Extended collection fields — only present when the tool author enables them
		if len(e.Args) > 0 {
			rec.AddAttributes(log.String("command.args", strings.Join(e.Args, " ")))
		}

		if e.Error != "" {
			rec.AddAttributes(log.String("command.error", e.Error))
		}

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
