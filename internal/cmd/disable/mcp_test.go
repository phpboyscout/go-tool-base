package disable

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func writeManifest(t *testing.T, fs afero.Fs, root string) {
	t.Helper()

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	manifest := "commands:\n  - name: post\n    description: publish\n"
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))
}

func TestDisableMCP_Metadata(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDisableMCP(&props.Props{}).Command
	assert.Equal(t, "mcp [command-path...]", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("path"), "must have a --path flag")
}

func TestDisableMCP_WithholdsCommand(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	writeManifest(t, fs, "/work")

	cmd := NewCmdDisableMCP(p).Command
	cmd.SetArgs([]string{"post", "--path", "/work"})
	require.NoError(t, cmd.Execute())

	manifest, _ := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp_enabled: false")

	cmdGo, err := afero.ReadFile(fs, "/work/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
}
