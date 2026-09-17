package annotate

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func newProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
	manifest := "commands:\n  - name: post\n    description: publish\n"
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(manifest), 0o644))

	return &props.Props{FS: fs, Logger: logger.NewNoop()}, fs
}

func run(t *testing.T, p *props.Props, args ...string) error {
	t.Helper()

	cmd := NewCmdAnnotate(p).Command
	cmd.SetArgs(append(args, "--path", "/work"))

	return cmd.Execute()
}

func TestAnnotate_Metadata(t *testing.T) {
	t.Parallel()

	cmd := NewCmdAnnotate(&props.Props{}).Command
	assert.Equal(t, "annotate <command-path>", cmd.Use)

	for _, flag := range []string{"title", "read-only", "destructive", "idempotent", "open-world", "clear", "path"} {
		assert.NotNil(t, cmd.Flags().Lookup(flag), flag)
	}
}

func TestAnnotate_SetsAndFlipsHints(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	require.NoError(t, run(t, p, "post", "--title", "Post it", "--read-only", "--open-world=false"))

	manifest, _ := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "title: Post it\n")
	assert.Contains(t, string(manifest), "read_only: true\n")
	assert.Contains(t, string(manifest), "open_world: false\n")
	assert.NotContains(t, string(manifest), "destructive")

	cmdGo, err := afero.ReadFile(fs, "/work/pkg/cmd/post/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), `setup.AnnotateMCP(cmd, setup.MCPHints{Title: "Post it", ReadOnly: new(true), OpenWorld: new(false)})`)

	// A bare --read-only=false flips only that hint; the title stays.
	require.NoError(t, run(t, p, "post", "--read-only=false"))

	manifest, _ = afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	assert.Contains(t, string(manifest), "title: Post it\n")
	assert.Contains(t, string(manifest), "read_only: false\n")
}

func TestAnnotate_ClearRemovesTheBlock(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)
	require.NoError(t, run(t, p, "post", "--idempotent"))
	require.NoError(t, run(t, p, "post", "--clear"))

	manifest, _ := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	assert.NotContains(t, string(manifest), "mcp_hints")

	cmdGo, _ := afero.ReadFile(fs, "/work/pkg/cmd/post/cmd.go")
	assert.NotContains(t, string(cmdGo), "AnnotateMCP")
}

func TestAnnotate_RefusesNothingToDoAndClearWithHints(t *testing.T) {
	t.Parallel()

	p, _ := newProject(t)

	require.Error(t, run(t, p, "post"), "no flag given is a usage error, not a silent no-op")
	require.Error(t, run(t, p, "post", "--clear", "--read-only"), "clear and a hint together are contradictory")
	require.Error(t, run(t, p, "missing", "--read-only"))
}
