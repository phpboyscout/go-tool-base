package telemetry

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/controls"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// Per-signal tuning keys, layered on the shared telemetry.* keys otelcore resolves.
const (
	configKeySampling = otelcore.Root + ".tracing.sampling"
	configKeyInterval = otelcore.Root + ".metrics.interval"
)

// ObservabilitySettingsFromProps adapts GTB props/config into package-owned
// observability settings while preserving the existing telemetry.* key layout.
func ObservabilitySettingsFromProps(p *props.Props) ObservabilitySettings {
	settings := ObservabilitySettings{Logger: logger.NewNoop()}
	if p == nil {
		return settings
	}

	settings.ServiceName = p.Tool.Name

	settings.Logger = loggerFromProps(p)
	if p.Version != nil {
		settings.Version = p.Version.GetVersion()
	}

	cfg := p.Config
	settings.Tracing.OTLP = otelcore.Resolve(cfg, otelcore.SignalTracing)
	settings.Metrics.OTLP = otelcore.Resolve(cfg, otelcore.SignalMetrics)
	settings.Logs.OTLP = otelcore.Resolve(cfg, otelcore.SignalLogs)

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

func loggerFromProps(p *props.Props) logger.Logger {
	if p != nil && p.Logger != nil {
		return p.Logger
	}

	return logger.NewNoop()
}
