package props_test

import (
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// Compile-time interface satisfaction checks.
var (
	_ props.LoggerProvider        = (*props.Props)(nil)
	_ props.ConfigProvider        = (*props.Props)(nil)
	_ props.FileSystemProvider    = (*props.Props)(nil)
	_ props.AssetProvider         = (*props.Props)(nil)
	_ props.VersionProvider       = (*props.Props)(nil)
	_ props.ErrorHandlerProvider  = (*props.Props)(nil)
	_ props.ToolMetadataProvider  = (*props.Props)(nil)
	_ props.LoggingConfigProvider = (*props.Props)(nil)
	_ props.CoreProvider          = (*props.Props)(nil)
)
