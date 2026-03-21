package props_test

import (
	"testing"

	"github.com/charmbracelet/log"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/version"
)

// Compile-time interface satisfaction checks.
var (
	_ props.LoggerProvider       = (*props.Props)(nil)
	_ props.ConfigProvider       = (*props.Props)(nil)
	_ props.FileSystemProvider   = (*props.Props)(nil)
	_ props.AssetProvider        = (*props.Props)(nil)
	_ props.VersionProvider      = (*props.Props)(nil)
	_ props.ErrorHandlerProvider = (*props.Props)(nil)
	_ props.ToolMetadataProvider = (*props.Props)(nil)
	_ props.LoggingConfigProvider = (*props.Props)(nil)
	_ props.CoreProvider         = (*props.Props)(nil)
)

func TestGetLogger_ReturnsField(t *testing.T) {
	logger := log.Default()
	p := &props.Props{Logger: logger}
	assert.Same(t, logger, p.GetLogger())
}

func TestGetConfig_ReturnsField(t *testing.T) {
	p := &props.Props{Config: nil}
	assert.Nil(t, p.GetConfig())
}

func TestGetFS_ReturnsField(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs}
	assert.Same(t, fs, p.GetFS())
}

func TestGetAssets_ReturnsField(t *testing.T) {
	p := &props.Props{Assets: nil}
	assert.Nil(t, p.GetAssets())
}

func TestGetVersion_ReturnsField(t *testing.T) {
	v := version.NewInfo("1.0.0", "abc123", "2026-01-01")
	p := &props.Props{Version: v}
	assert.Equal(t, v, p.GetVersion())
}

func TestGetErrorHandler_ReturnsField(t *testing.T) {
	p := &props.Props{ErrorHandler: nil}
	assert.Nil(t, p.GetErrorHandler())
}

func TestGetTool_ReturnsField(t *testing.T) {
	tool := props.Tool{Name: "test-tool", Summary: "A test tool"}
	p := &props.Props{Tool: tool}
	assert.Equal(t, tool, p.GetTool())
}
