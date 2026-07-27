package props_test

import (
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props/test"
)

// Compile-time interface satisfaction checks for the kept provider interfaces.
var (
	_ props.LoggerProvider       = (*props.Props)(nil)
	_ props.ConfigProvider       = (*props.Props)(nil)
	_ props.AssetProvider        = (*props.Props)(nil)
	_ props.ToolMetadataProvider = (*props.Props)(nil)
)

// TestFixtureCollector verifies that a fixture-built Props exposes a non-nil
// collector via the getter.
func TestFixtureCollector(t *testing.T) {
	t.Parallel()

	if test.New().GetCollector() == nil {
		t.Fatal("test.New() GetCollector() returned nil; want non-nil NoopCollector")
	}
}
