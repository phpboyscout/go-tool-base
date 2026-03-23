package props

import (
	"github.com/spf13/afero"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/version"
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
