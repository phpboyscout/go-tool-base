package regenerate

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

const projectManifest = "properties:\n  name: mytool\ncommands:\n" +
	"  - name: alpha\n" +
	"    description: the alpha command\n"

const sourceManifest = "properties:\n  name: mytool\n"

const rootSource = `package root

import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"

	"test-mod/pkg/cmd/social"
)

func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, social.NewCmdSocial(p))
}
`

const socialSource = `package social

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdSocial(p *props.Props) *setup.Command {
	return setup.Wrap("social", &cobra.Command{
		Use:   "social",
		Short: "social media tools",
	})
}
`

func writeFiles(t *testing.T, fs afero.Fs, files map[string]string) {
	t.Helper()

	for name, body := range files {
		require.NoError(t, afero.WriteFile(fs, projectRoot+name, []byte(body), 0o644))
	}
}

// newProject is a project whose manifest names a command with no source yet,
// so `regenerate project` has a registration file to write.
func newProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		"/.gtb/manifest.yaml":  projectManifest,
		"/go.mod":              "module test-mod\n",
		"/pkg/cmd/root/cmd.go": "package root\nfunc NewCmdRoot(p interface{}) {}\n",
	})

	return &props.Props{FS: fs, Logger: logger.NewNoop()}, fs
}

// newSourceProject is a project whose manifest has drifted from its source:
// the code registers `social`, the manifest names nothing.
func newSourceProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	writeFiles(t, fs, map[string]string{
		"/.gtb/manifest.yaml":    sourceManifest,
		"/go.mod":                "module test-mod\n",
		"/pkg/cmd/root/cmd.go":   rootSource,
		"/pkg/cmd/social/cmd.go": socialSource,
	})

	return &props.Props{FS: fs, Logger: logger.NewNoop()}, fs
}

func execute(p *props.Props, args ...string) error {
	cmd := NewCmdRegenerate(p).Command
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd.Execute()
}

func readFile(t *testing.T, fs afero.Fs, name string) string {
	t.Helper()

	data, err := afero.ReadFile(fs, projectRoot+name)
	require.NoError(t, err)

	return string(data)
}

func TestNewCmdRegenerate_Structure(t *testing.T) {
	t.Parallel()

	cmd := NewCmdRegenerate(&props.Props{}).Command
	assert.Equal(t, "regenerate", cmd.Use)
	assert.NotNil(t, cmd.PersistentFlags().Lookup("dry-run"))

	for _, name := range []string{"project", "manifest"} {
		sub, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
		assert.Equal(t, name, sub.Use)
		assert.NotNil(t, sub.Flags().Lookup("path"), name)
	}

	project, _, err := cmd.Find([]string{"project"})
	require.NoError(t, err)

	for flag, def := range map[string]string{
		"force":       "false",
		"overwrite":   "ask",
		"update-docs": "false",
		"no-verify":   "false",
	} {
		f := project.Flags().Lookup(flag)
		require.NotNil(t, f, flag)
		assert.Equal(t, def, f.DefValue, flag)
	}
}

func TestSharedFlags_DryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags *SharedFlags
		want  bool
	}{
		{name: "nil receiver reads the default", flags: nil, want: false},
		{name: "unset", flags: &SharedFlags{}, want: false},
		{name: "set", flags: &SharedFlags{DryRun: true}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.flags.dryRun())
		})
	}
}

func TestRegenerateProject_WritesRegistrationFiles(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	require.NoError(t, execute(p, "project", "--path", projectRoot, "--overwrite", "allow", "--no-verify"))

	cmdGo := readFile(t, fs, "/pkg/cmd/alpha/cmd.go")
	assert.Contains(t, cmdGo, "package alpha")
	assert.Contains(t, cmdGo, "the alpha command")

	assert.Contains(t, readFile(t, fs, "/pkg/cmd/root/cmd.go"), "alpha.NewCmdAlpha(p)",
		"the root registers the manifest's command")
}

func TestRegenerateProject_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	require.NoError(t, execute(p, "project", "--path", projectRoot, "--overwrite", "allow", "--no-verify", "--dry-run"))

	exists, err := afero.Exists(fs, projectRoot+"/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)
	assert.False(t, exists, "--dry-run reaches the generator")
	assert.Equal(t, projectManifest, readFile(t, fs, "/.gtb/manifest.yaml"))
}

func TestRegenerateProject_OverwriteValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   string
		wantErr bool
	}{
		{value: "allow"},
		{value: "deny"},
		{value: "ask"},
		{value: "ALLOW", wantErr: true},
		{value: "sometimes", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			p, fs := newProject(t)

			err := execute(p, "project", "--path", projectRoot, "--overwrite", tt.value, "--no-verify")
			if !tt.wantErr {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrInvalidOverwriteValue)
			assert.Contains(t, err.Error(), "\""+tt.value+"\"")

			exists, statErr := afero.Exists(fs, projectRoot+"/pkg/cmd/alpha/cmd.go")
			require.NoError(t, statErr)
			assert.False(t, exists, "a rejected flag writes nothing")
		})
	}
}

// TestProjectOptions_RunDefaultsEmptyOverwriteToAsk drives Run directly, as a
// library caller would, with no flag parsing to supply the default.
func TestProjectOptions_RunDefaultsEmptyOverwriteToAsk(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	opts := &ProjectOptions{Path: projectRoot, NoVerify: true}
	require.NoError(t, opts.Run(context.Background(), p))
	assert.Equal(t, string(generator.OverwriteAsk), opts.Overwrite)
	assert.Contains(t, readFile(t, fs, "/pkg/cmd/alpha/cmd.go"), "package alpha")
}

func TestRegenerateProject_NotAProject(t *testing.T) {
	t.Parallel()

	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}

	err := execute(p, "project", "--path", projectRoot, "--overwrite", "allow", "--no-verify")
	require.ErrorIs(t, err, generator.ErrNotGoToolBaseProject)
}

// TestRegenerateManifest_NoCommandSource: the manifest is the output here, so
// its absence is not the failure; a project with no command tree to scan is.
func TestRegenerateManifest_NoCommandSource(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	err := execute(p, "manifest", "--path", projectRoot)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pkg/cmd directory not found")

	exists, statErr := afero.Exists(fs, projectRoot+"/.gtb/manifest.yaml")
	require.NoError(t, statErr)
	assert.False(t, exists)
}

func TestRegenerateManifest_RebuildsFromSource(t *testing.T) {
	t.Parallel()

	p, fs := newSourceProject(t)

	require.NoError(t, execute(p, "manifest", "--path", projectRoot))

	manifest := readFile(t, fs, "/.gtb/manifest.yaml")
	assert.Contains(t, manifest, "name: social")
	assert.Contains(t, manifest, "social media tools")
}

func TestRegenerateManifest_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	p, fs := newSourceProject(t)

	require.NoError(t, execute(p, "manifest", "--path", projectRoot, "--dry-run"))
	assert.Equal(t, sourceManifest, readFile(t, fs, "/.gtb/manifest.yaml"))
}
