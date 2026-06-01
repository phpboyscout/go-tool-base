package otelcore

import "gitlab.com/phpboyscout/go-tool-base/pkg/config"

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

// Resolve reads telemetry.<signal>.* overlaid on the shared telemetry.* keys, in
// the same shared-plus-override style as pkg/tls. A per-signal key, when set,
// overrides the shared value for that one field. Enabled is per-signal only.
//
// An empty Endpoint is intentional and not an error: it lets the OTel SDK fall
// back to the standard OTEL_EXPORTER_OTLP_* environment variables, so operators
// who configure the ecosystem's env vars need set nothing in GTB config beyond
// telemetry.<signal>.enabled.
func Resolve(cfg config.Containable, signal string) Settings {
	sig := Root + "." + signal

	s := Settings{
		Enabled:  cfg.GetBool(sig + ".enabled"),
		Endpoint: cfg.GetString(Root + ".endpoint"),
		Insecure: cfg.GetBool(Root + ".insecure"),
		Headers:  cfg.GetViper().GetStringMapString(Root + ".headers"),
	}

	if cfg.IsSet(sig + ".endpoint") {
		s.Endpoint = cfg.GetString(sig + ".endpoint")
	}

	if cfg.IsSet(sig + ".insecure") {
		s.Insecure = cfg.GetBool(sig + ".insecure")
	}

	if cfg.IsSet(sig + ".headers") {
		s.Headers = cfg.GetViper().GetStringMapString(sig + ".headers")
	}

	return s
}
