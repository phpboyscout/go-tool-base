package props

import (
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Props is the primary dependency injection container for GTB applications.
// When writing functions that accept Props, consider whether a narrow interface
// (LoggerProvider, ConfigProvider, etc.) would suffice.
type Props struct {
	Tool         Tool
	Logger       logger.Logger
	Config       config.Containable
	Assets       Assets
	FS           afero.Fs
	Version      version.Version
	ErrorHandler errorhandling.ErrorHandler
	// Collector is always non-nil once the root command tree is built: the
	// bootstrap defaults it to a NoopCollector and later replaces it with the
	// resolved *telemetry.Collector. Consumers may call it unconditionally.
	Collector TelemetryCollector
}

// GetLogger returns the application logger.
func (p *Props) GetLogger() logger.Logger { return p.Logger }

// GetConfig returns the application configuration.
func (p *Props) GetConfig() config.Containable { return p.Config }

// GetAssets returns the embedded assets.
func (p *Props) GetAssets() Assets { return p.Assets }

// GetFS returns the application filesystem.
func (p *Props) GetFS() afero.Fs { return p.FS }

// GetVersion returns the version information.
func (p *Props) GetVersion() version.Version { return p.Version }

// GetErrorHandler returns the error handler.
func (p *Props) GetErrorHandler() errorhandling.ErrorHandler { return p.ErrorHandler }

// GetTool returns the tool metadata.
func (p *Props) GetTool() Tool { return p.Tool }

// GetCollector returns the telemetry collector. It is always non-nil once the
// root command tree is built, so consumers may call it unconditionally.
func (p *Props) GetCollector() TelemetryCollector { return p.Collector }
