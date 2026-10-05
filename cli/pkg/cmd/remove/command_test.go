package remove

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const projectRoot = "/work"

const twoCommandManifest = "properties:\n  name: mytool\ncommands:\n" +
	"  - name: alpha\n" +
	"  - name: beta\n" +
	"    protected: true\n"

const rootCmdGo = `package root

import (
	"github.com/spf13/cobra"

	"test-mod/pkg/cmd/alpha"
	"test-mod/pkg/cmd/beta"
)

func NewCmdRoot(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "mytool"}
	cmd.AddCommand(alpha.NewCmdAlpha(p))
	cmd.AddCommand(beta.NewCmdBeta(p))

	return cmd
}
`

func newProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	files := map[string]string{
		"/.gtb/manifest.yaml":     twoCommandManifest,
		"/go.mod":                 "module test-mod\n",
		"/pkg/cmd/root/cmd.go":    rootCmdGo,
		"/pkg/cmd/alpha/cmd.go":   "package alpha\n",
		"/pkg/cmd/alpha/main.go":  "package alpha\n",
		"/pkg/cmd/beta/cmd.go":    "package beta\n",
		"/pkg/cmd/beta/main.go":   "package beta\n",
		"/docs/commands/alpha.md": "# alpha\n",
		"/docs/commands/index.md": "# commands\n",
	}

	for name, body := range files {
		require.NoError(t, afero.WriteFile(fs, projectRoot+name, []byte(body), 0o644))
	}

	return p, fs
}

func execute(p *props.Props, args ...string) error {
	cmd := NewCmdCommand(p)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd.Execute()
}

func TestNewCmdRemove_RegistersCommandSubcommand(t *testing.T) {
	t.Parallel()

	root := NewCmdRemove(&props.Props{}).Command
	assert.Equal(t, "remove", root.Use)

	sub, _, err := root.Find([]string{"command"})
	require.NoError(t, err)
	assert.Equal(t, "command", sub.Use)
}

func TestNewCmdCommand_Flags(t *testing.T) {
	t.Parallel()

	cmd := NewCmdCommand(&props.Props{})

	tests := []struct {
		flag      string
		shorthand string
		def       string
	}{
		{flag: "name", shorthand: "n", def: ""},
		{flag: "path", shorthand: "p", def: "."},
		{flag: "parent", def: "root"},
		{flag: "force", shorthand: "f", def: "false"},
	}

	for _, tt := range tests {
		f := cmd.Flags().Lookup(tt.flag)
		require.NotNil(t, f, tt.flag)
		assert.Equal(t, tt.shorthand, f.Shorthand, tt.flag)
		assert.Equal(t, tt.def, f.DefValue, tt.flag)
	}
}

func TestRemoveCommand_NameIsRequired(t *testing.T) {
	t.Parallel()

	p, _ := newProject(t)

	err := execute(p, "--path", projectRoot)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `required flag(s) "name" not set`)
}

func TestRemoveCommand_RemovesCommandAndUpdatesManifest(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	require.NoError(t, execute(p, "--name", "alpha", "--path", projectRoot))

	exists, err := afero.DirExists(fs, projectRoot+"/pkg/cmd/alpha")
	require.NoError(t, err)
	assert.False(t, exists, "the command directory is deleted")

	manifest, err := afero.ReadFile(fs, projectRoot+"/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.NotContains(t, string(manifest), "name: alpha")
	assert.Contains(t, string(manifest), "name: beta", "siblings are untouched")

	rootGo, err := afero.ReadFile(fs, projectRoot+"/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.NotContains(t, string(rootGo), "alpha.NewCmdAlpha", "the parent no longer registers it")
	assert.Contains(t, string(rootGo), "beta.NewCmdBeta")

	betaExists, err := afero.Exists(fs, projectRoot+"/pkg/cmd/beta/main.go")
	require.NoError(t, err)
	assert.True(t, betaExists)
}

func TestRemoveCommand_ProtectedNeedsForce(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	err := execute(p, "--name", "beta", "--path", projectRoot)
	require.ErrorIs(t, err, generator.ErrCommandProtected)

	exists, statErr := afero.Exists(fs, projectRoot+"/pkg/cmd/beta/main.go")
	require.NoError(t, statErr)
	assert.True(t, exists, "a refused removal deletes nothing")

	require.NoError(t, execute(p, "--name", "beta", "--path", projectRoot, "--force"))

	exists, statErr = afero.DirExists(fs, projectRoot+"/pkg/cmd/beta")
	require.NoError(t, statErr)
	assert.False(t, exists, "--force overrides the protection")
}

func TestRemoveCommand_RejectsInvalidInputBeforeTouchingTheProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "traversal name", args: []string{"--name", "../evil"}},
		{name: "reserved name", args: []string{"--name", "root"}},
		{name: "empty name", args: []string{"--name", ""}},
		{name: "traversal parent", args: []string{"--name", "alpha", "--parent", "alpha/../.."}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, fs := newProject(t)

			err := execute(p, append(tt.args, "--path", projectRoot)...)
			require.ErrorIs(t, err, generator.ErrInvalidInput)

			exists, statErr := afero.Exists(fs, projectRoot+"/pkg/cmd/alpha/main.go")
			require.NoError(t, statErr)
			assert.True(t, exists)
		})
	}
}

func TestRemoveCommand_NotAProject(t *testing.T) {
	t.Parallel()

	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}

	err := execute(p, "--name", "alpha", "--path", projectRoot)
	require.ErrorIs(t, err, generator.ErrNotGoToolBaseProject)
}

func TestCommandOptions_RunHonoursCancelledContext(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := &CommandOptions{Name: "alpha", Path: projectRoot, Parent: "root"}
	require.ErrorIs(t, opts.Run(ctx, p), context.Canceled)

	exists, err := afero.Exists(fs, projectRoot+"/pkg/cmd/alpha/main.go")
	require.NoError(t, err)
	assert.True(t, exists)
}
