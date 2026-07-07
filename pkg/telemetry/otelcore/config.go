package otelcore

// Root is the config key prefix shared by the analytics pipeline and the
// observability signals.
const Root = "telemetry"

// Signal names, used both as the per-signal config sub-key (telemetry.<signal>.*)
// and as the OTLP URL path suffix.
const (
	SignalTracing = "tracing"
	SignalMetrics = "metrics"
	SignalLogs    = "logs"
)

// Settings is the resolved OTLP target for a single signal.
type Settings struct {
	Enabled  bool
	Endpoint string // OTLP/HTTP base URL; empty means "let the SDK read OTEL_* env vars"
	Headers  map[string]string
	Insecure bool
}

// Config holds shared OTLP settings that can be overlaid by each signal.
type Config struct {
	Endpoint string            `mapstructure:"endpoint" yaml:"endpoint" json:"endpoint"`
	Headers  map[string]string `mapstructure:"headers" yaml:"headers" json:"headers"`
	Insecure bool              `mapstructure:"insecure" yaml:"insecure" json:"insecure"`
}

// SignalConfig holds per-signal OTLP settings.
type SignalConfig struct {
	Enabled  bool              `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Endpoint string            `mapstructure:"endpoint" yaml:"endpoint" json:"endpoint"`
	Headers  map[string]string `mapstructure:"headers" yaml:"headers" json:"headers"`
	Insecure bool              `mapstructure:"insecure" yaml:"insecure" json:"insecure"`
}

// SignalOverrides records which per-signal fields were explicitly supplied by
// an adapter and should override shared config.
type SignalOverrides struct {
	Endpoint bool
	Headers  bool
	Insecure bool
}

// ResolveSettings overlays per-signal settings on shared telemetry settings.
// Enabled is always per-signal only.
func ResolveSettings(shared Config, signal SignalConfig, overrides SignalOverrides) Settings {
	settings := Settings{
		Enabled:  signal.Enabled,
		Endpoint: shared.Endpoint,
		Headers:  cloneHeaders(shared.Headers),
		Insecure: shared.Insecure,
	}

	if overrides.Endpoint {
		settings.Endpoint = signal.Endpoint
	}

	if overrides.Headers {
		settings.Headers = cloneHeaders(signal.Headers)
	}

	if overrides.Insecure {
		settings.Insecure = signal.Insecure
	}

	return settings
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	out := make(map[string]string, len(headers))
	for key, value := range headers {
		out[key] = value
	}

	return out
}
