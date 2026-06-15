package props_test

import (
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props/propstest"
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
	_ props.TelemetryProvider     = (*props.Props)(nil)
	_ props.LoggingConfigProvider = (*props.Props)(nil)
	_ props.CoreProvider          = (*props.Props)(nil)
)

// TestTelemetryProvider verifies that both *props.Props and a propstest-built
// Props satisfy TelemetryProvider and expose a non-nil collector via the getter.
func TestTelemetryProvider(t *testing.T) {
	t.Parallel()

	var p props.TelemetryProvider = propstest.New()
	if p.GetCollector() == nil {
		t.Fatal("propstest.New() GetCollector() returned nil; want non-nil NoopCollector")
	}
}
