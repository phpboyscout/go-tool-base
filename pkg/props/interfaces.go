package props

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/afero"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	"github.com/phpboyscout/gtb/pkg/version"
)

// LoggerProvider provides access to the application logger.
type LoggerProvider interface {
	GetLogger() *log.Logger
}

// ConfigProvider provides access to the application configuration.
type ConfigProvider interface {
	GetConfig() config.Containable
}

// FileSystemProvider provides access to the application filesystem.
type FileSystemProvider interface {
	GetFS() afero.Fs
}

// AssetProvider provides access to embedded assets.
type AssetProvider interface {
	GetAssets() Assets
}

// VersionProvider provides access to version information.
type VersionProvider interface {
	GetVersion() version.Version
}

// ErrorHandlerProvider provides access to the error handler.
type ErrorHandlerProvider interface {
	GetErrorHandler() errorhandling.ErrorHandler
}

// ToolMetadataProvider provides access to tool configuration and metadata.
type ToolMetadataProvider interface {
	GetTool() Tool
}

// LoggingConfigProvider is the most common combination — logging and configuration.
type LoggingConfigProvider interface {
	LoggerProvider
	ConfigProvider
}

// CoreProvider provides the three most commonly needed capabilities.
type CoreProvider interface {
	LoggerProvider
	ConfigProvider
	FileSystemProvider
}

// Compile-time interface satisfaction checks.
var (
	_ LoggerProvider        = (*Props)(nil)
	_ ConfigProvider        = (*Props)(nil)
	_ FileSystemProvider    = (*Props)(nil)
	_ AssetProvider         = (*Props)(nil)
	_ VersionProvider       = (*Props)(nil)
	_ ErrorHandlerProvider  = (*Props)(nil)
	_ ToolMetadataProvider  = (*Props)(nil)
	_ LoggingConfigProvider = (*Props)(nil)
	_ CoreProvider          = (*Props)(nil)
)
