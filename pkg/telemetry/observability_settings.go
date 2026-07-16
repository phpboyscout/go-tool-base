package telemetry

import (
	"log/slog"
	"time"

	"gitlab.com/phpboyscout/go/observability/otelcore"
)

// ObservabilitySettings contains the package-owned settings used to initialise
// OpenTelemetry tracing, metrics, and logs.
type ObservabilitySettings struct {
	ServiceName string
	Version     string
	Logger      *slog.Logger
	Tracing     ObservabilitySignalSettings
	Metrics     ObservabilitySignalSettings
	Logs        ObservabilitySignalSettings
}

func (s ObservabilitySettings) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}

	return slog.New(slog.DiscardHandler)
}

// ObservabilitySignalSettings contains the resolved OTLP target for one signal
// plus optional signal-specific tuning.
type ObservabilitySignalSettings struct {
	OTLP        otelcore.Settings
	Sampling    float64
	SamplingSet bool
	Interval    time.Duration
	IntervalSet bool
}
