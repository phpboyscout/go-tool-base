package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func goreleaserManifest() *Manifest {
	return &Manifest{
		Properties: ManifestProperties{Name: "tool", Description: "a tool"},
		ReleaseSource: ManifestReleaseSource{
			Type: "github", Host: "github.com", Owner: "acme", Repo: "tool",
		},
	}
}

func TestApplyGoreleaserSigns_AbsentFileIsRenderedWhole(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})
	m := goreleaserManifest()

	require.NoError(t, g.applyGoreleaserSigns(m))

	got, err := afero.ReadFile(fs, "/work/.goreleaser.yaml")
	require.NoError(t, err)
	assert.NotContains(t, string(got), "signs:", "signing is off, so no signs block")
	assert.Equal(t, calculateHash(got), m.Hashes[goreleaserAssetRelPath])
}

func TestApplyGoreleaserSigns_UnparseableFileIsLeftAlone(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})
	body := []byte("- not\n- a mapping\n")
	require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", body, DefaultFileMode))

	m := goreleaserManifest()
	m.Properties.Signing = ManifestSigning{Enabled: true, KeyID: "alias/release"}

	require.NoError(t, g.applyGoreleaserSigns(m))

	got, err := afero.ReadFile(fs, "/work/.goreleaser.yaml")
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// goreleaser reads only the first YAML document, so a file holding a second
// is advised rather than spliced: the block would land in the last (#216).
func TestApplyGoreleaserSigns_ASecondDocumentIsLeftAlone(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"after ...":          "version: 2\nbuilds: []\n...\nfoo: bar\n",
		"after ---":          "version: 2\nbuilds: []\n---\nfoo: bar\n",
		"a trailing ...":     "version: 2\nbuilds: []\n...\n",
		"... then a comment": "version: 2\nbuilds: []\n...\n# a comment\n",
	} {
		log := logger.NewBuffer()
		fs := afero.NewMemMapFs()
		g := New(&props.Props{FS: fs, Logger: log}, &Config{Path: "/work"})
		require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", []byte(body), DefaultFileMode))

		m := goreleaserManifest()
		m.Properties.Signing = ManifestSigning{Enabled: true, KeyID: "alias/release"}

		require.NoError(t, g.applyGoreleaserSigns(m), name)

		got, err := afero.ReadFile(fs, "/work/.goreleaser.yaml")
		require.NoError(t, err)
		assert.Equal(t, body, string(got), name)
		assert.True(t, log.Contains("add the following top-level block"), "%s: the signs block is advised", name)
		assert.True(t, log.Contains("add the rotation manifest's two lines by hand"), "%s: so are the rotation lines", name)
	}
}

func TestParseYAMLMapping(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]bool{
		"version: 2\n":                   true,
		"---\nversion: 2\n":              true,
		"version: 2\n...\n":              false,
		"version: 2\n... # end\n":        false,
		"version: 2\nx: \"...\"\n":       true,
		"version: 2\nx: |\n  ...\n":      true,
		"version: 2\n...\nfoo: bar\n":    false,
		"version: 2\n---\nfoo: bar\n":    false,
		"":                               false,
		"- a\n":                          false,
		"version: 2\n---\n- not a map\n": false,
	} {
		_, ok := parseYAMLMapping([]byte(body))
		assert.Equal(t, want, ok, "%q", body)
	}
}

func TestApplyGoreleaserSigns_DisableLeavesASecondDocumentAlone(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		body    string
		advised bool
	}{
		"with a signs block": {"version: 2\nsigns:\n  - cmd: sigillum\n---\nfoo: bar\n", true},
		"with none":          {"version: 2\nbuilds: []\n---\nfoo: bar\n", false},
	} {
		log := logger.NewBuffer()
		fs := afero.NewMemMapFs()
		g := New(&props.Props{FS: fs, Logger: log}, &Config{Path: "/work"})
		require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", []byte(tc.body), DefaultFileMode))

		m := goreleaserManifest()
		require.NoError(t, g.applyGoreleaserSigns(m), name)

		got, err := afero.ReadFile(fs, "/work/.goreleaser.yaml")
		require.NoError(t, err)
		assert.Equal(t, tc.body, string(got), name)
		assert.Equal(t, tc.advised, log.Contains("remove the release-signing signs: block"), "%s: %v", name, log.Messages())
	}
}
