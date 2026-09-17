package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestManifestProperties_MCPMode_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	out, err := yaml.Marshal(ManifestProperties{Name: "t", MCP: ManifestMCP{Mode: "direct"}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "mcp:\n    mode: direct\n")

	var back ManifestProperties
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, "direct", back.MCP.Mode)

	bare, err := yaml.Marshal(ManifestProperties{Name: "t"})
	require.NoError(t, err)
	assert.NotContains(t, string(bare), "mcp:", "compact is the absent default")
}

func TestValidateMCPMode(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"", "compact", "direct"} {
		require.NoError(t, ValidateMCPMode(ok), ok)
	}

	require.ErrorIs(t, ValidateMCPMode("sideways"), ErrInvalidInput)
}

func renderRootWithMode(t *testing.T, mode string) string {
	t.Helper()

	src, err := renderRoot(t, templates.SkeletonRootData{Name: "tool", Description: "d", MCPMode: mode})
	require.NoError(t, err)

	return src
}

func TestSkeletonRoot_RendersDirectModeOnly(t *testing.T) {
	t.Parallel()

	assert.Contains(t, renderRootWithMode(t, "direct"), "props.MCPConfig{Mode: props.MCPDirect}")
	assert.NotContains(t, renderRootWithMode(t, "compact"), "MCPConfig", "compact renders nothing so existing roots already say compact")
	assert.NotContains(t, renderRootWithMode(t, ""), "MCPConfig")
}

func TestExtractProperties_ReadsTheMCPModeBack(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: "/work"})

	for mode, want := range map[string]string{"direct": "direct", "": ""} {
		path := "/work/pkg/cmd/root/cmd.go"
		require.NoError(t, afero.WriteFile(fs, path, []byte(renderRootWithMode(t, mode)), 0o644))

		properties, _, err := g.extractProjectProperties(path)
		require.NoError(t, err)
		assert.Equal(t, want, properties.MCP.Mode, "rendered %q", mode)
	}
}

func TestSetSetting_MCPModeRecordsRendersAndRejects(t *testing.T) {
	t.Parallel()

	g, fs, _ := newPerimeterTestProject(t, settingsManifest)
	root := g.config.Path

	require.NoError(t, g.SetSetting(context.Background(), "mcp.mode", []string{"direct"}))

	manifest, _ := afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp:\n    mode: direct\n")

	rootGo, err := afero.ReadFile(fs, root+"/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), "props.MCPConfig{Mode: props.MCPDirect}")

	got, err := g.GetSetting("mcp.mode")
	require.NoError(t, err)
	assert.Equal(t, []string{"direct"}, got)

	require.NoError(t, g.SetSetting(context.Background(), "mcp.mode", []string{"compact"}))

	manifest, _ = afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mode: compact", "an explicit compact is recorded as said")

	rootGo, _ = afero.ReadFile(fs, root+"/pkg/cmd/root/cmd.go")
	assert.NotContains(t, string(rootGo), "MCPConfig")

	require.Error(t, g.SetSetting(context.Background(), "mcp.mode", []string{"sideways"}))
}
