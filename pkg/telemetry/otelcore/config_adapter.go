package otelcore

import gtbconfig "gitlab.com/phpboyscout/go-tool-base/pkg/config"

// Resolve reads telemetry.<signal>.* overlaid on the shared telemetry.* keys, in
// the same shared-plus-override style as pkg/tls. A per-signal key, when set,
// overrides the shared value for that one field. Enabled is per-signal only.
//
// An empty Endpoint is intentional and not an error: it lets the OTel SDK fall
// back to the standard OTEL_EXPORTER_OTLP_* environment variables, so operators
// who configure the ecosystem's env vars need set nothing in GTB config beyond
// telemetry.<signal>.enabled.
func Resolve(cfg gtbconfig.Containable, signal string) Settings {
	if cfg == nil {
		return Settings{}
	}

	sig := Root + "." + signal

	shared, _ := gtbconfig.UnmarshalSection[Config](cfg, Root)
	signalSection, _ := gtbconfig.UnmarshalSection[SignalConfig](cfg, sig)

	return ResolveSettings(shared.Value, signalSection.Value, SignalOverrides{
		Endpoint: cfg.IsSet(sig + ".endpoint"),
		Headers:  cfg.IsSet(sig + ".headers"),
		Insecure: cfg.IsSet(sig + ".insecure"),
	})
}
