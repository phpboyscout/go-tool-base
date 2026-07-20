package props

import (
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// LoggerProvider provides access to the application logger.
type LoggerProvider interface {
	GetLogger() logger.Logger
}

// ConfigProvider provides access to the live configuration store.
//
// Take this when you need to write configuration, observe changes, or hold a
// reference across a reload. Code that only reads should take [ConfigReader]
// instead, which cannot do any of those things and says so in its signature.
type ConfigProvider interface {
	GetConfig() *config.Store
}

// ConfigReader provides read access to resolved configuration.
//
// A view is pinned to one snapshot, so every value read through it comes from
// the same resolved configuration — reads cannot straddle a reload. Call it per
// logical operation rather than holding the result: a view kept across a reload
// keeps serving the values it was pinned to.
type ConfigReader interface {
	GetConfigView() *config.View
}

// ConfigFSProvider adapts the application filesystem for the configuration
// store, bridging afero to the narrower interface config defines.
type ConfigFSProvider interface {
	GetConfigFS() config.FS
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

// TelemetryProvider provides access to the telemetry collector.
type TelemetryProvider interface {
	GetCollector() TelemetryCollector
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
	_ ConfigReader          = (*Props)(nil)
	_ ConfigFSProvider      = (*Props)(nil)
	_ FileSystemProvider    = (*Props)(nil)
	_ AssetProvider         = (*Props)(nil)
	_ VersionProvider       = (*Props)(nil)
	_ ErrorHandlerProvider  = (*Props)(nil)
	_ ToolMetadataProvider  = (*Props)(nil)
	_ TelemetryProvider     = (*Props)(nil)
	_ LoggingConfigProvider = (*Props)(nil)
	_ CoreProvider          = (*Props)(nil)
)
