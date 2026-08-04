package props

import (
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// Narrow provider interfaces let a package declare only the Props capability it
// needs, rather than taking the whole container. Only the interfaces with a real
// production consumer are kept: eight further single-capability interfaces
// (ConfigReader, ConfigFSProvider, FileSystemProvider, VersionProvider,
// ErrorHandlerProvider, TelemetryProvider, LoggingConfigProvider, CoreProvider)
// were removed pre-1.0 as dead API — they had zero consumers and were pure
// ornament. Their getter methods remain on *Props for direct use; reintroduce an
// interface only when a consumer actually narrows to it. See
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0162-gtb-framework-followups §2.3.1 and
// docs/reference/migration/2026-07-23-props-interface-prune.md.

// LoggerProvider provides access to the application logger.
type LoggerProvider interface {
	GetLogger() logger.Logger
}

// ConfigProvider provides access to the live configuration store.
//
// Take this when you need to write configuration, observe changes, or hold a
// reference across a reload.
type ConfigProvider interface {
	GetConfig() *config.Store
}

// AssetProvider provides access to embedded assets.
type AssetProvider interface {
	GetAssets() Assets
}

// ToolMetadataProvider provides access to tool configuration and metadata.
type ToolMetadataProvider interface {
	GetTool() Tool
}

// Compile-time interface satisfaction checks.
var (
	_ LoggerProvider       = (*Props)(nil)
	_ ConfigProvider       = (*Props)(nil)
	_ AssetProvider        = (*Props)(nil)
	_ ToolMetadataProvider = (*Props)(nil)
)
