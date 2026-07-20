package telemetry

import (
	"context"
	"log/slog"

	"gitlab.com/phpboyscout/go/controls"

	"gitlab.com/phpboyscout/go/observability/otelcore"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Per-signal tuning keys, layered on the shared telemetry.* keys otelcore resolves.
const (
	configKeySampling = otelcore.Root + ".tracing.sampling"
	configKeyInterval = otelcore.Root + ".metrics.interval"
)

// ObservabilitySettingsFromProps adapts GTB props/config into package-owned
// observability settings while preserving the existing telemetry.* key layout.
func ObservabilitySettingsFromProps(p *props.Props) ObservabilitySettings {
	settings := ObservabilitySettings{Logger: slog.New(slog.DiscardHandler)}
	if p == nil {
		return settings
	}

	settings.ServiceName = p.Tool.Name

	settings.Logger = loggerFromProps(p)
	if p.Version != nil {
		settings.Version = p.Version.GetVersion()
	}

	// One pinned view for every read below, so the per-signal sections and the
	// tuning keys resolve against the same snapshot.
	cfg := props.ViewOrNil(p)

	settings.Tracing.OTLP = resolveOTLPSettings(cfg, otelcore.SignalTracing)
	settings.Metrics.OTLP = resolveOTLPSettings(cfg, otelcore.SignalMetrics)
	settings.Logs.OTLP = resolveOTLPSettings(cfg, otelcore.SignalLogs)

	if cfg != nil && cfg.IsSet(configKeySampling) {
		settings.Tracing.Sampling = cfg.GetFloat(configKeySampling)
		settings.Tracing.SamplingSet = true
	}

	if cfg != nil && cfg.IsSet(configKeyInterval) {
		settings.Metrics.Interval = cfg.GetDuration(configKeyInterval)
		settings.Metrics.IntervalSet = true
	}

	return settings
}

// SetupFromProps adapts GTB props/config into typed observability settings, then
// installs the enabled OpenTelemetry providers.
func SetupFromProps(ctx context.Context, p *props.Props, controller controls.Controllable) (Shutdown, error) {
	return Setup(ctx, ObservabilitySettingsFromProps(p), controller)
}

func loggerFromProps(p *props.Props) *slog.Logger {
	if p != nil {
		return logger.ToSlog(p.Logger)
	}

	return slog.New(slog.DiscardHandler)
}
