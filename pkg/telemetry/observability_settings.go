package telemetry

import (
	"time"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry/otelcore"
)

// ObservabilitySettings contains the package-owned settings used to initialise
// OpenTelemetry tracing, metrics, and logs.
type ObservabilitySettings struct {
	ServiceName string
	Version     string
	Logger      logger.Logger
	Tracing     ObservabilitySignalSettings
	Metrics     ObservabilitySignalSettings
	Logs        ObservabilitySignalSettings
}

func (s ObservabilitySettings) logger() logger.Logger {
	if s.Logger != nil {
		return s.Logger
	}

	return logger.NewNoop()
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
