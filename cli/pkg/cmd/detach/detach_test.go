package detach

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

const signingCLI = "gitlab.com/phpboyscout/go/signing-cli"

func newProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	require.NoError(t, generator.New(p, &generator.Config{}).GenerateSkeleton(context.Background(), generator.SkeletonConfig{
		Name:         "detach-tool",
		Repo:         "acme/detach-tool",
		Host:         "github.com",
		ForgeBackend: forge.GithubFeature,
		Description:  "detach fixture",
		Path:         "/work",
	}))

	return p, fs
}

func attachSigningCLI(t *testing.T, p *props.Props) {
	t.Helper()

	require.NoError(t, generator.New(p, &generator.Config{Path: "/work", Overwrite: "allow"}).
		AttachExternalCommand(context.Background(), generator.ExternalCommandSpec{
			Module:  signingCLI,
			Version: "v0.1.0",
			Attach:  []generator.ManifestExternalAttach{{Constructor: "NewCmdSign", Args: []string{"logger"}, Wrap: true}},
		}))
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()

	b, err := afero.ReadFile(fs, path)
	require.NoError(t, err)

	return string(b)
}

func TestNewCmdDetach_Structure(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDetach(&props.Props{}).Command
	assert.Equal(t, "detach", cmd.Use)

	got := map[string]bool{}
	for _, c := range NewCmdDetach(&props.Props{}).Commands() {
		got[c.Name()] = true
	}

	assert.True(t, got["command"], "expected 'detach command' subcommand")
}

func TestNewCmdDetachCommand_Flags(t *testing.T) {
	t.Parallel()

	cmd := newCmdDetachCommand(&props.Props{}).Command
	assert.Equal(t, "command <module>", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("path"), "must have a --path flag")
}

func TestDetachCommand_RemovesAttachment(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)
	attachSigningCLI(t, p)
	require.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "signingcli.NewCmdSign")

	var out bytes.Buffer

	cmd := NewCmdDetach(p).Command
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"command", signingCLI, "--path", "/work"})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "detached "+signingCLI+"\n", out.String())
	assert.NotContains(t, readFile(t, fs, "/work/.gtb/manifest.yaml"), signingCLI)
	assert.NotContains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "signingcli")
}

func TestDetachCommand_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project bool
		args    []string
		wantErr error
	}{
		{name: "unknown module", project: true, args: []string{"example.com/not-attached", "--path", "/work"}, wantErr: generator.ErrInvalidInput},
		{name: "missing manifest", args: []string{signingCLI, "--path", "/work"}, wantErr: generator.ErrNotGoToolBaseProject},
		{name: "no module argument", project: true, args: []string{"--path", "/work"}},
		{name: "too many arguments", project: true, args: []string{"a", "b", "--path", "/work"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
			if tc.project {
				p, _ = newProject(t)
			}

			var out bytes.Buffer

			cmd := newCmdDetachCommand(p).Command
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			require.Error(t, err)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}

			assert.NotContains(t, out.String(), "detached")
		})
	}
}
