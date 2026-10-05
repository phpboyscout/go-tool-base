package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const navManifest = "properties:\n  name: tool\ncommands:\n  - name: widget\n"

func newNavProject(t *testing.T, mkdocs string, withManifest bool) (*Generator, afero.Fs) {
	t.Helper()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})

	if mkdocs != "" {
		require.NoError(t, afero.WriteFile(fs, "/work/mkdocs.yml", []byte(mkdocs), DefaultFileMode))
	}

	if withManifest {
		require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(navManifest), DefaultFileMode))
	}

	return g, fs
}

func TestRegenerateMkdocsNav_AddsANavWhenThereIsNone(t *testing.T) {
	t.Parallel()

	g, fs := newNavProject(t, "site_name: tool\n", true)

	require.NoError(t, g.regenerateMkdocsNav())

	got, err := afero.ReadFile(fs, "/work/mkdocs.yml")
	require.NoError(t, err)
	assert.Contains(t, string(got), "nav:")
	assert.Contains(t, string(got), "CLI")
	assert.Contains(t, string(got), "widget")
}

func TestRegenerateMkdocsNav_NoDocsToolchainIsANoOp(t *testing.T) {
	t.Parallel()

	g, fs := newNavProject(t, "", true)

	require.NoError(t, g.regenerateMkdocsNav())

	exists, err := afero.Exists(fs, "/work/mkdocs.yml")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRegenerateMkdocsNav_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mkdocs   string
		manifest bool
		want     string
	}{
		{name: "no manifest", mkdocs: "site_name: tool\n", manifest: false},
		{name: "unparseable", mkdocs: "nav: [unclosed\n", manifest: true, want: "failed to unmarshal mkdocs.yml"},
		{name: "not a map", mkdocs: "- just\n- a list\n", manifest: true, want: "not a valid map"},
		{name: "empty document", mkdocs: "# only a comment\n", manifest: true, want: "not a valid map"},
		{name: "nav is not a list", mkdocs: "nav:\n  home: index.md\n", manifest: true, want: "failed to decode nav"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, _ := newNavProject(t, tt.mkdocs, tt.manifest)

			err := g.regenerateMkdocsNav()
			require.Error(t, err)

			if tt.want != "" {
				assert.Contains(t, err.Error(), tt.want)
			}
		})
	}
}

func TestReadCommandSource_ReadsASingleFile(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/widget/cmd.go", []byte("package widget\n"), DefaultFileMode))

	got, err := g.readCommandSource("/work/pkg/cmd/widget/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, "package widget\n", got)
}
