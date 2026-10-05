package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
