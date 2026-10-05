package disable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const signingManifest = `properties:
  module_path: github.com/acme/tool
commands:
  - name: post
    description: publish
`

func newTestProps(fs afero.Fs) *props.Props {
	return &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		IO:     props.StdIO{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
	}
}

func writeFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()

	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
}

// writeProject writes the manifest plus the go.mod the root re-render reads its
// module path from.
func writeProject(t *testing.T, fs afero.Fs, manifest string) {
	t.Helper()

	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
	writeFile(t, fs, "/work/.gtb/manifest.yaml", manifest)
	writeFile(t, fs, "/work/go.mod", "module github.com/acme/tool\n\ngo 1.26\n")
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()

	b, err := afero.ReadFile(fs, path)
	require.NoError(t, err)

	return string(b)
}

func featureEnabled(t *testing.T, p *props.Props, name string) bool {
	t.Helper()

	on, err := generator.New(p, &generator.Config{Path: "/work"}).FeatureEnabled(name)
	require.NoError(t, err)

	return on
}

func TestNewCmdDisable_Metadata(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDisable(&props.Props{}).Command
	assert.Equal(t, "disable [feature...]", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("path"))

	for _, feature := range generator.ToggleableFeatures {
		assert.Contains(t, cmd.Long, feature, "the long help lists every toggleable feature")
	}

	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"signing", "mcp"}, names)
}

func TestNewCmdDisable_TogglesFeatureOff(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, "commands:\n  - name: post\n    description: publish\n")

	p := newTestProps(fs)
	assert.True(t, featureEnabled(t, p, "doctor"), "doctor defaults on")

	cmd := NewCmdDisable(p).Command
	cmd.SetArgs([]string{"doctor", "--path", "/work"})
	require.NoError(t, cmd.Execute())

	assert.False(t, featureEnabled(t, p, "doctor"))

	root := readFile(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.Contains(t, root, "props.Disable(props.DoctorCmd)")
}

func TestNewCmdDisable_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want error
	}{
		{
			name: "no feature on a non-interactive stdin",
			args: []string{"--path", "/work"},
			want: icmd.ErrFeatureNameRequired,
		},
		{
			name: "unknown feature",
			args: []string{"no-such-feature", "--path", "/work"},
			want: generator.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeManifest(t, fs, "/work")

			cmd := NewCmdDisable(newTestProps(fs)).Command
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			require.ErrorIs(t, err, tt.want)

			assert.Equal(t, "commands:\n  - name: post\n    description: publish\n",
				readFile(t, fs, "/work/.gtb/manifest.yaml"), "a refused toggle must not touch the manifest")
		})
	}
}

func TestDisableSigning_DropsSigning(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, signingManifest)
	p := newTestProps(fs)

	require.NoError(t, generator.New(p, &generator.Config{Path: "/work", Overwrite: "allow"}).
		EnableSigning(t.Context(), generator.ManifestSigning{ExternalKeyEmail: "release@acme.test"}))
	require.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "Signing:")
	writeFile(t, fs, "/work/internal/trustkeys/keys/signing-key-v1.asc", "author key")

	cmd := NewCmdDisable(p).Command
	cmd.SetArgs([]string{"signing", "--path", "/work"})
	require.NoError(t, cmd.Execute())

	manifest := readFile(t, fs, "/work/.gtb/manifest.yaml")
	assert.Contains(t, manifest, "release@acme.test", "the rest of the signing block is kept")

	current, err := generator.New(p, &generator.Config{Path: "/work"}).CurrentSigning()
	require.NoError(t, err)
	assert.False(t, current.Enabled)

	root := readFile(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.NotContains(t, root, "Signing:")

	exists, err := afero.Exists(fs, "/work/pkg/cmd/root/signing.go")
	require.NoError(t, err)
	assert.False(t, exists, "signing.go is removed")

	assert.Equal(t, "author key", readFile(t, fs, "/work/internal/trustkeys/keys/signing-key-v1.asc"),
		"author keys are never deleted")
}

func TestDisableSigning_NotAProject(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	cmd := newCmdDisableSigning(newTestProps(fs)).Command
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--path", "/nowhere"})

	require.Error(t, cmd.Execute())

	exists, err := afero.DirExists(fs, "/nowhere")
	require.NoError(t, err)
	assert.False(t, exists, "nothing is written when there is no project")
}

func TestDisableMCP_NoArgsTogglesFeatureOff(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, "commands:\n  - name: post\n    description: publish\n")

	p := newTestProps(fs)
	assert.True(t, featureEnabled(t, p, "mcp"), "mcp defaults on")

	cmd := NewCmdDisableMCP(p).Command
	cmd.SetArgs([]string{"--path", "/work"})
	require.NoError(t, cmd.Execute())

	assert.False(t, featureEnabled(t, p, "mcp"))
}
