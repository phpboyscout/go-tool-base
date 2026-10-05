package enable

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	plainManifest = "properties:\n  module_path: github.com/acme/tool\ncommands:\n  - name: post\n    description: publish\n"

	mcpOffManifest = `properties:
  module_path: github.com/acme/tool
  features:
    - name: mcp
      enabled: false
commands:
  - name: post
    description: publish
`
)

func newTestProps(fs afero.Fs) *props.Props {
	return &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		IO:     props.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard},
	}
}

// writeProject writes the manifest plus the go.mod the root re-render reads its
// module path from.
func writeProject(t *testing.T, fs afero.Fs, manifest string) {
	t.Helper()

	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(manifest), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module github.com/acme/tool\n\ngo 1.26\n"), 0o644))
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

func TestNewCmdEnable_Metadata(t *testing.T) {
	t.Parallel()

	cmd := NewCmdEnable(&props.Props{}).Command
	assert.Equal(t, "enable [feature...]", cmd.Use)
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

func TestNewCmdEnable_TogglesFeatureOn(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, plainManifest)

	p := newTestProps(fs)
	require.False(t, featureEnabled(t, p, "telemetry"), "telemetry defaults off")

	cmd := NewCmdEnable(p).Command
	cmd.SetArgs([]string{"telemetry", "--path", "/work"})
	require.NoError(t, cmd.Execute())

	assert.True(t, featureEnabled(t, p, "telemetry"))
	assert.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "props.TelemetryCmd")
}

func TestNewCmdEnable_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want error
	}{
		{name: "no feature on a non-interactive stdin", args: []string{"--path", "/work"}, want: icmd.ErrFeatureNameRequired},
		{name: "unknown feature", args: []string{"no-such-feature", "--path", "/work"}, want: generator.ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeProject(t, fs, plainManifest)

			cmd := NewCmdEnable(newTestProps(fs)).Command
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)

			require.ErrorIs(t, cmd.Execute(), tt.want)
			assert.Equal(t, plainManifest, readFile(t, fs, "/work/.gtb/manifest.yaml"),
				"a refused toggle must not touch the manifest")
		})
	}
}

func TestEnableMCP_NoArgsTogglesFeatureOn(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, mcpOffManifest)

	p := newTestProps(fs)
	require.False(t, featureEnabled(t, p, "mcp"))

	cmd := NewCmdEnableMCP(p).Command
	cmd.SetArgs([]string{"--path", "/work"})
	require.NoError(t, cmd.Execute())

	assert.True(t, featureEnabled(t, p, "mcp"))
}

func TestEnableSigning_RoutesFromParent(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, plainManifest)

	cmd := NewCmdEnable(newTestProps(fs)).Command
	cmd.SetArgs([]string{"signing", "--path", "/work", "--email", "release@acme.test"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, readFile(t, fs, "/work/.gtb/manifest.yaml"), "external_key_email: release@acme.test")
	assert.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "Signing:")
}

// stdinRemaining reports what a test's stdin still holds, which tells whether a
// prompt read from it.
func stdinRemaining(t *testing.T, p *props.Props) string {
	t.Helper()

	rest, err := io.ReadAll(p.GetIO().In())
	require.NoError(t, err)

	return string(rest)
}

func TestNewCmdEnableSigning_Writes(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, plainManifest)

	cmd := NewCmdEnableSigning(newTestProps(fs)).Command
	cmd.SetArgs([]string{
		"--path", "/work",
		"--email", "release@acme.test",
		"--key-source", "external",
		"--require-signature",
		"--require-external-crosscheck",
		"--key-id", "alias/release",
		"--kms-region", "eu-west-1",
		"--public-key", "internal/trustkeys/keys/signing-key-v2.asc",
	})
	require.NoError(t, cmd.Execute())

	manifest := readFile(t, fs, "/work/.gtb/manifest.yaml")
	for _, want := range []string{
		"external_key_email: release@acme.test",
		"key_source: external",
		"require_signature: true",
		"require_external_crosscheck: true",
		"backend: aws-kms",
		"key_id: alias/release",
		"kms_region: eu-west-1",
		"public_key: internal/trustkeys/keys/signing-key-v2.asc",
	} {
		assert.Contains(t, manifest, want)
	}

	assert.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "Signing:")

	exists, err := afero.Exists(fs, "/work/pkg/cmd/root/signing.go")
	require.NoError(t, err)
	assert.True(t, exists, "signing.go is emitted")
}

func TestNewCmdEnableSigning_RerunKeepsUnprovidedFields(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeProject(t, fs, plainManifest)
	p := newTestProps(fs)

	first := NewCmdEnableSigning(p).Command
	first.SetArgs([]string{"--path", "/work", "--email", "release@acme.test", "--key-source", "embedded"})
	require.NoError(t, first.Execute())

	second := NewCmdEnableSigning(p).Command
	second.SetArgs([]string{"--path", "/work", "--require-signature"})
	require.NoError(t, second.Execute())

	current, err := generator.New(p, &generator.Config{Path: "/work"}).CurrentSigning()
	require.NoError(t, err)
	assert.True(t, current.Enabled)
	assert.True(t, current.RequireSignature)
	assert.Equal(t, "release@acme.test", current.ExternalKeyEmail)
	assert.Equal(t, "embedded", current.KeySource)
}

func TestNewCmdEnableSigning_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		args     []string
		want     error
	}{
		{
			name:     "invalid key source",
			manifest: plainManifest,
			args:     []string{"--email", "release@acme.test", "--key-source", "everywhere"},
			want:     ErrInvalidKeySource,
		},
		{
			name:     "unregistered backend",
			manifest: plainManifest,
			args:     []string{"--email", "release@acme.test", "--backend", "no-such-backend"},
			want:     ErrInvalidBackend,
		},
		{
			name:     "generator rejects the merged block",
			manifest: plainManifest,
			args:     []string{"--email", "not an email"},
			want:     generator.ErrInvalidInput,
		},
		{
			name: "not a project",
			args: []string{"--email", "release@acme.test"},
			want: generator.ErrNotGoToolBaseProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			if tt.manifest != "" {
				writeProject(t, fs, tt.manifest)
			}

			cmd := NewCmdEnableSigning(newTestProps(fs)).Command
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(append([]string{"--path", "/work"}, tt.args...))

			require.ErrorIs(t, cmd.Execute(), tt.want)

			exists, err := afero.Exists(fs, "/work/pkg/cmd/root/signing.go")
			require.NoError(t, err)
			assert.False(t, exists, "a refused enable writes nothing")
		})
	}
}

func accessibleProps(fs afero.Fs, stdin string) *props.Props {
	return &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		IO: props.StdIO{
			Stdin:          strings.NewReader(stdin),
			Stdout:         &bytes.Buffer{},
			Stderr:         &bytes.Buffer{},
			AccessibleMode: true,
		},
	}
}

func TestNewCmdEnableSigning_PromptsForEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
	}{
		{name: "no config store"},
		{name: "ci config key off", config: "ci: false\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeProject(t, fs, plainManifest)
			p := accessibleProps(fs, "release@acme.test\n2\n")
			if tt.config != "" {
				p.Config = testutil.StoreFromYAML(t, tt.config)
			}

			cmd := NewCmdEnableSigning(p).Command
			cmd.SetArgs([]string{"--path", "/work"})
			require.NoError(t, cmd.Execute())

			current, err := generator.New(p, &generator.Config{Path: "/work"}).CurrentSigning()
			require.NoError(t, err)
			assert.Equal(t, "release@acme.test", current.ExternalKeyEmail)
			assert.Equal(t, "embedded", current.KeySource, "option 2 of the key-source select")
		})
	}
}

func TestNewCmdEnableSigning_NoPrompt(t *testing.T) {
	t.Parallel()

	const stdin = "should-not-be-read@acme.test\n"

	tests := []struct {
		name     string
		manifest string
		args     []string
		ciFlag   bool
		config   string
	}{
		{
			name:     "email already in the manifest",
			manifest: "properties:\n  module_path: github.com/acme/tool\n  signing:\n    external_key_email: release@acme.test\n",
		},
		{
			name:     "email passed as a flag",
			manifest: plainManifest,
			args:     []string{"--email", "release@acme.test"},
		},
		{
			name:     "the --ci flag",
			manifest: plainManifest,
			args:     []string{"--ci"},
			ciFlag:   true,
		},
		{
			name:     "the ci config key",
			manifest: plainManifest,
			config:   "ci: true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeProject(t, fs, tt.manifest)
			p := accessibleProps(fs, stdin)
			if tt.config != "" {
				p.Config = testutil.StoreFromYAML(t, tt.config)
			}

			cmd := NewCmdEnableSigning(p).Command
			if tt.ciFlag {
				cmd.Flags().Bool("ci", false, "")
			}

			cmd.SetArgs(append([]string{"--path", "/work"}, tt.args...))
			require.NoError(t, cmd.Execute())

			assert.Equal(t, stdin, stdinRemaining(t, p), "no prompt may read stdin")
			assert.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "Signing:")
		})
	}
}
