package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func newSetEnabledFixture(t *testing.T) (*props.Props, string, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	root := "/work"

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	manifest := "commands:\n" +
		"  - name: post\n" +
		"    description: publish\n" +
		"  - name: secret\n" +
		"    description: secret\n" +
		"    protected: true\n"
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))

	return p, root, fs
}

func TestSetMCPEnabled_DisableThenEnable(t *testing.T) {
	t.Parallel()

	p, root, fs := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	// Disable -> manifest records false and cmd.go stamps ExcludeFromMCP.
	require.NoError(t, g.SetMCPEnabled(context.Background(), "post", false))

	manifest, _ := afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp_enabled: false")

	cmdGo, err := afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
	assert.NotContains(t, string(cmdGo), "setup.IncludeInMCP(cmd)")

	// Enable -> manifest records true and cmd.go swaps to IncludeInMCP.
	require.NoError(t, g.SetMCPEnabled(context.Background(), "post", true))

	manifest, _ = afero.ReadFile(fs, root+"/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "mcp_enabled: true")

	cmdGo, err = afero.ReadFile(fs, root+"/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "setup.IncludeInMCP(cmd)")
	assert.NotContains(t, string(cmdGo), "setup.ExcludeFromMCP(cmd)")
}

func TestSetMCPEnabled_RefusesProtected(t *testing.T) {
	t.Parallel()

	p, root, _ := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	err := g.SetMCPEnabled(context.Background(), "secret", false)
	require.ErrorIs(t, err, ErrCommandProtected)
}

func TestSetMCPEnabled_NotFound(t *testing.T) {
	t.Parallel()

	p, root, _ := newSetEnabledFixture(t)
	g := New(p, &Config{Path: root})

	err := g.SetMCPEnabled(context.Background(), "missing", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

// A cmd.go kept for one command must not stop a later command in the same run
// getting its children re-registered (#167).
func TestSetMCPEnabled_KeptFileDoesNotCostLaterCommandsTheirChildren(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	root := "/work"

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/tool\n\ngo 1.27\n"), 0o644))

	manifest := "commands:\n" +
		"  - name: hook\n" +
		"    description: hooks\n" +
		"  - name: worklist\n" +
		"    description: worklists\n" +
		"    commands:\n" +
		"      - name: list\n" +
		"        description: list them\n"
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte(manifest), 0o644))

	handEdited := "package hook\n\n// registers a child by hand\n"
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/hook", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/hook/cmd.go", []byte(handEdited), 0o644))

	g := New(p, &Config{Path: root, Overwrite: OverwriteDeny})

	require.NoError(t, g.SetMCPEnabled(context.Background(), "hook", false))
	require.NoError(t, g.SetMCPEnabled(context.Background(), "worklist", false))

	kept, err := afero.ReadFile(fs, root+"/pkg/cmd/hook/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, handEdited, string(kept))

	worklist, err := afero.ReadFile(fs, root+"/pkg/cmd/worklist/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(worklist), "setup.ExcludeFromMCP(cmd)")
	assert.Contains(t, string(worklist), `"example.com/tool/pkg/cmd/worklist/list"`)
	assert.Contains(t, string(worklist), "list.NewCmdList(")
}
