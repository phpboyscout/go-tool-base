package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestRunRegistrationSteps_LeavesAKeptFileAlone: when the command's cmd.go
// was kept (a hand edit under --overwrite deny), the child re-registration
// must neither rewrite the kept file nor record its hash as the generated
// one. Doing so made the hand edit read as generated, so the next regenerate
// saw no conflict and overwrote it silently.
func TestRunRegistrationSteps_LeavesAKeptFileAlone(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/work"

	m := Manifest{Commands: []ManifestCommand{{Name: "hello", Commands: []ManifestCommand{{Name: "sub"}}}}}
	m.Properties.Name = "tool"
	m.Version.GoToolBase = "v1.0.0"
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", raw, 0o644))

	cmdDir := root + "/pkg/cmd/hello"
	kept := []byte("package hello\n\n// hand edit, kept under --overwrite deny\n")
	require.NoError(t, fs.MkdirAll(cmdDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, cmdDir+"/cmd.go", kept, 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)}, &Config{Path: root, Name: "hello"})
	data := templates.CommandData{Name: "hello", Hashes: map[string]string{"cmd.go": "the-resolver's-hash"}, RegistrationKept: true}

	var result PipelineResult
	newCommandPipeline(g, PipelineOptions{SkipRegistration: true}).runRegistrationSteps(data, cmdDir, &result)
	assert.Empty(t, result.Warnings)

	got, err := afero.ReadFile(fs, cmdDir+"/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, string(kept), string(got), "the kept file is not touched")
	assert.Equal(t, "the-resolver's-hash", data.Hashes["cmd.go"], "the recorded hash stays the resolver's, so the next run still sees the edit")
}

// TestSetMCPEnabled_KeptFileThenThreeLevels: one generator re-renders a
// command whose cmd.go is kept, then a grandchild, child and parent, as
// `gtb disable mcp` does for several paths. Each re-render must re-register the
// level below, so nothing from the kept render reaches the next (#167, #176).
func TestSetMCPEnabled_KeptFileThenThreeLevels(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/work"

	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/tool\n\ngo 1.27\n"), 0o644))

	m := Manifest{Commands: []ManifestCommand{
		{Name: "hook", Description: "hooks"},
		{Name: "worklist", Description: "worklists", Commands: []ManifestCommand{
			{Name: "list", Description: "list them", Commands: []ManifestCommand{
				{Name: "item", Description: "one item"},
			}},
		}},
	}}
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", raw, 0o644))

	handEdited := "package hook\n\n// hand edit, kept under --overwrite deny\n"
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/hook", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/hook/cmd.go", []byte(handEdited), 0o644))

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)}, &Config{Path: root, Overwrite: OverwriteDeny})

	// Deepest first: a child's re-render registers it in its parent again,
	// which would mask a parent that lost its children.
	for _, path := range []string{"hook", "worklist/list/item", "worklist/list", "worklist"} {
		require.NoError(t, g.SetMCPEnabled(t.Context(), path, false), path)
	}

	kept, err := afero.ReadFile(fs, root+"/pkg/cmd/hook/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, handEdited, string(kept), "the kept file is left alone")

	for parent, child := range map[string]string{
		"/pkg/cmd/worklist/cmd.go":      "list.NewCmdList(",
		"/pkg/cmd/worklist/list/cmd.go": "item.NewCmdItem(",
	} {
		got, err := afero.ReadFile(fs, root+parent)
		require.NoError(t, err)
		assert.Contains(t, string(got), child, "%s registers its child", parent)
	}
}
