package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

// Spec 0204 D2: the formats a tool links are declared; its own format is one
// of the writable four and must be linked unless it is YAML.
func TestValidateConfigFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		formats []string
		format  string
		wantErr string
	}{
		{name: "nothing declared"},
		{name: "toml linked and own", formats: []string{"toml"}, format: "toml"},
		{name: "read-only formats linked", formats: []string{"ini", "dotenv", "properties", "xml"}},
		{name: "yaml may be named", formats: []string{"yaml", "toml"}, format: "yaml"},
		{name: "an unknown format", formats: []string{"toml", "yml"}, wantErr: "unknown config format"},
		{name: "a read-only own format", formats: []string{"ini"}, format: "ini", wantErr: "yaml, toml, json or hcl"},
		{name: "an own format not linked", format: "toml", wantErr: "--config-formats"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateConfigFormats(tc.formats, tc.format)
			if tc.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, errors.FlattenHints(err), tc.wantErr)
		})
	}
}

// YAML is built in and never recorded; the rest are recorded once each, in
// the family's order, so a manifest reads the same however they were given.
func TestNormaliseConfigFormats(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"toml", "json", "dotenv"}, normaliseConfigFormats([]string{"dotenv", "yaml", "json", "toml", "json"}))
	assert.Nil(t, normaliseConfigFormats([]string{"yaml"}))
	assert.Empty(t, normaliseOwnFormat("yaml"))
	assert.Equal(t, "toml", normaliseOwnFormat("toml"))
}

func generateWithFormats(t *testing.T, fs afero.Fs, formats []string, format string) *Generator {
	t.Helper()

	g := newSkeletonGeneratorForTest(t, fs)
	cfg := SkeletonConfig{
		Name: "fmttool", Repo: "acme/fmttool", Host: "github.com", ForgeBackend: "github",
		Description: "formats", Path: "/work",
		Features:      []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: "github", Enabled: true}},
		ConfigFormats: formats,
		ConfigFormat:  format,
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	return g
}

// A tool linking formats gets cmd/<name>/config.go blank-importing each, and
// its own format in the root's ConfigSpec.
func TestGenerateSkeleton_LinksTheDeclaredFormats(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := generateWithFormats(t, fs, []string{"dotenv", "toml"}, "toml")

	linked := readGenerated(t, fs, "/work/cmd/fmttool/config.go")
	assert.Contains(t, linked, `_ "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/toml"`)
	assert.Contains(t, linked, `_ "gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/dotenv"`)

	root := readGenerated(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.Contains(t, root, `props.ConfigSpec{Format: "toml"}`)

	g.config.Path = "/work"
	m, err := g.loadManifest()
	require.NoError(t, err)
	assert.Equal(t, []string{"toml", "dotenv"}, m.Properties.Config.Formats)
	assert.Equal(t, "toml", m.Properties.Config.Format)
}

// A tool that links nothing beyond YAML has no config.go and no ConfigSpec.
func TestGenerateSkeleton_NoFormatsNoFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	generateWithFormats(t, fs, nil, "")

	exists, err := afero.Exists(fs, "/work/cmd/fmttool/config.go")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.NotContains(t, readGenerated(t, fs, "/work/pkg/cmd/root/cmd.go"), "ConfigSpec")
}

// The file follows the manifest: formats removed from it leave the binary on
// the next regenerate.
func TestRegenerateProject_DropsConfigGoWithTheLastFormat(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := generateWithFormats(t, fs, []string{"toml"}, "")

	g.config.Path = "/work"
	g.config.Overwrite = OverwriteAllow
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return []byte("done"), nil }

	m, err := g.loadManifest()
	require.NoError(t, err)

	m.Properties.Config.Formats = nil
	raw, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, ManifestPathFor("/work"), raw, 0o644))

	require.NoError(t, g.RegenerateProject(context.Background()))

	exists, err := afero.Exists(fs, "/work/cmd/fmttool/config.go")
	require.NoError(t, err)
	assert.False(t, exists, "config.go goes with the last linked format")
}
