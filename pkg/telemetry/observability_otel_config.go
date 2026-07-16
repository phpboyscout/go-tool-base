package telemetry

import (
	"gitlab.com/phpboyscout/go/observability/otelcore"

	gtbconfig "gitlab.com/phpboyscout/go-tool-base/pkg/config"
)

// resolveOTLPSettings reads telemetry.<signal>.* overlaid on the shared telemetry.*
// keys, in the same shared-plus-override style as pkg/tls. A per-signal key, when
// set, overrides the shared value for that one field. Enabled is per-signal only.
//
// This is GTB's config-key adapter over the framework-free
// gitlab.com/phpboyscout/go/observability/otelcore core: it materialises typed
// otelcore values from a GTB config container, then delegates the merge to
// otelcore.ResolveSettings. An empty Endpoint is intentional and not an error — it
// lets the OTel SDK fall back to the standard OTEL_EXPORTER_OTLP_* environment
// variables.
func resolveOTLPSettings(cfg gtbconfig.Containable, signal string) otelcore.Settings {
	if cfg == nil {
		return otelcore.Settings{}
	}

	sig := otelcore.Root + "." + signal

	// Decode errors are deliberately tolerated: a malformed value yields the zero
	// section, which resolves to an empty Endpoint and the OTEL_* env fallback.
	shared, _ := gtbconfig.UnmarshalSection[otelcore.Config](cfg, otelcore.Root)
	signalSection, _ := gtbconfig.UnmarshalSection[otelcore.SignalConfig](cfg, sig)

	return otelcore.ResolveSettings(shared.Value, signalSection.Value, otelcore.SignalOverrides{
		Endpoint: cfg.IsSet(sig + ".endpoint"),
		Headers:  cfg.IsSet(sig + ".headers"),
		Insecure: cfg.IsSet(sig + ".insecure"),
	})
}

// ObserveSettingsFromConfig binds a single telemetry signal's resolved OTLP
// settings to cfg and keeps the typed snapshot rehydrated after successful config
// reloads. The returned section satisfies [otelcore.SettingsSource].
func ObserveSettingsFromConfig(
	cfg gtbconfig.Containable,
	signal string,
	opts ...gtbconfig.SectionBindingOption[otelcore.Settings],
) (*gtbconfig.ObservedSection[otelcore.Settings], error) {
	key := otelcore.Root + "." + signal

	bindingOpts := make([]gtbconfig.SectionBindingOption[otelcore.Settings], 0, 1+len(opts))
	bindingOpts = append(bindingOpts, gtbconfig.WithSectionDefaultFunc(func(next gtbconfig.Containable) otelcore.Settings {
		return resolveOTLPSettings(next, signal)
	}, mergeResolvedSettings))
	bindingOpts = append(bindingOpts, opts...)

	return gtbconfig.ObserveSection[otelcore.Settings](cfg, key, bindingOpts...)
}

// mergeResolvedSettings intentionally ignores the unmarshalled overlay: a signal's
// Settings are the shared-plus-override resolution produced by resolveOTLPSettings
// (via the default func), which a single-section decode cannot reproduce. The
// observer's decode runs only to detect that telemetry.<signal> exists and to
// trigger reloads; the resolved value always comes from the recomputed defaults.
func mergeResolvedSettings(defaults, _ otelcore.Settings) otelcore.Settings {
	return defaults
}
