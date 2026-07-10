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

	// Decode errors are deliberately tolerated. Config/SignalConfig hold only
	// simple string/bool/map fields; a malformed value yields the zero section,
	// which resolves to an empty Endpoint and lets the OTel SDK fall back to the
	// standard OTEL_EXPORTER_OTLP_* environment variables — the same graceful
	// degradation the pre-refactor typed getters produced by coercion. Resolve
	// must stay error-free to satisfy WithSectionDefaultFunc below, so there is
	// no error to surface here.
	shared, _ := gtbconfig.UnmarshalSection[Config](cfg, Root)
	signalSection, _ := gtbconfig.UnmarshalSection[SignalConfig](cfg, sig)

	return ResolveSettings(shared.Value, signalSection.Value, SignalOverrides{
		Endpoint: cfg.IsSet(sig + ".endpoint"),
		Headers:  cfg.IsSet(sig + ".headers"),
		Insecure: cfg.IsSet(sig + ".insecure"),
	})
}

// ObserveSettingsFromConfig binds a single telemetry signal's resolved OTLP
// settings to cfg and keeps the typed snapshot rehydrated after successful
// config reloads.
func ObserveSettingsFromConfig(
	cfg gtbconfig.Containable,
	signal string,
	opts ...gtbconfig.SectionBindingOption[Settings],
) (*gtbconfig.ObservedSection[Settings], error) {
	key := Root + "." + signal

	bindingOpts := make([]gtbconfig.SectionBindingOption[Settings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, gtbconfig.WithSectionDefaultFunc(func(next gtbconfig.Containable) Settings {
		return Resolve(next, signal)
	}, mergeResolvedSettings))
	bindingOpts = append(bindingOpts, opts...)

	return gtbconfig.ObserveSection[Settings](cfg, key, bindingOpts...)
}

// mergeResolvedSettings intentionally ignores the unmarshalled overlay. A
// signal's Settings are the shared-plus-override resolution produced by
// Resolve (via the default func), which a single-section decode cannot
// reproduce — and Settings deliberately carries no decode tags because it is
// never populated by UnmarshalSection. The observer's decode runs only to
// detect that telemetry.<signal> exists and to trigger reloads; the resolved
// value always comes from the recomputed defaults, which this returns verbatim.
func mergeResolvedSettings(defaults, _ Settings) Settings {
	return defaults
}
