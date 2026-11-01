package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// earlierRenderHash stands in for the hash gtb recorded when it last rendered
// .goreleaser.yaml, before the developer edited the file.
const earlierRenderHash = "hash-of-an-earlier-gtb-render"

const unparseableGoreleaser = "version: 2\nbuilds: [unclosed\n"

// scaffoldWithEditedGoreleaser leaves .goreleaser.yaml holding content that
// differs from the hash the manifest stores for it: a file the developer has
// edited since gtb last wrote it.
func scaffoldWithEditedGoreleaser(t *testing.T, content string, ignore bool) (*Generator, string) {
	t.Helper()

	gen, goreleaserPath, _ := scaffoldProjectWithGoreleaser(t, content, ignore)
	setStoredGoreleaserHash(t, gen, earlierRenderHash)

	return gen, goreleaserPath
}

func setStoredGoreleaserHash(t *testing.T, gen *Generator, hash string) {
	t.Helper()

	m, err := gen.loadManifest()
	require.NoError(t, err)

	m.Hashes[goreleaserAssetRelPath] = hash
	require.NoError(t, gen.writeManifest(m))
}

func storedGoreleaserHash(t *testing.T, gen *Generator) string {
	t.Helper()

	m, err := gen.loadManifest()
	require.NoError(t, err)

	return m.Hashes[goreleaserAssetRelPath]
}

var testSigning = ManifestSigning{KeyID: "alias/acme-release-signing-v1", KeySource: "both"}

// TestSigning_KeepsTheBaselineOfAnEditedGoreleaser pins spec 0188 D9 on the
// signing path (#150): when signing leaves an edited .goreleaser.yaml as it is,
// or edits it, the stored hash must not become the hash of the developer's
// content. If it did, the next regenerate would judge the file unmodified and
// overwrite it without asking.
func TestSigning_KeepsTheBaselineOfAnEditedGoreleaser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		ignore  bool
		apply   func(*Generator) error
	}{
		{
			name:    "enable, ignored",
			content: customisedGoreleaser,
			ignore:  true,
			apply:   func(g *Generator) error { return g.EnableSigning(context.Background(), testSigning) },
		},
		{
			name:    "enable, unparseable",
			content: unparseableGoreleaser,
			apply:   func(g *Generator) error { return g.EnableSigning(context.Background(), testSigning) },
		},
		{
			name:    "enable, author signs block present",
			content: authorSignsGoreleaser,
			apply:   func(g *Generator) error { return g.EnableSigning(context.Background(), testSigning) },
		},
		{
			name:    "enable, block injected into an edited file",
			content: customisedNonIgnoredGoreleaser,
			apply:   func(g *Generator) error { return g.EnableSigning(context.Background(), testSigning) },
		},
		{
			name:    "disable, no signs block",
			content: customisedNonIgnoredGoreleaser,
			apply:   func(g *Generator) error { return g.DisableSigning(context.Background()) },
		},
		{
			name:    "disable, author signs block",
			content: authorSignsGoreleaser,
			apply:   func(g *Generator) error { return g.DisableSigning(context.Background()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gen, _ := scaffoldWithEditedGoreleaser(t, tt.content, tt.ignore)

			require.NoError(t, tt.apply(gen))

			assert.Equal(t, earlierRenderHash, storedGoreleaserHash(t, gen),
				"signing adopted the developer's edits as the baseline")
		})
	}
}

// TestEnableSigning_TracksItsEditToAnUnmodifiedGoreleaser is the other half:
// when the file is still exactly what gtb last wrote, gtb's own edit becomes the
// new baseline, so the file stays tracked as unmodified.
func TestEnableSigning_TracksItsEditToAnUnmodifiedGoreleaser(t *testing.T) {
	t.Parallel()

	gen, goreleaserPath, _ := scaffoldProjectWithGoreleaser(t, customisedNonIgnoredGoreleaser, false)

	require.NoError(t, gen.EnableSigning(context.Background(), testSigning))

	got, err := os.ReadFile(goreleaserPath)
	require.NoError(t, err)
	assert.Contains(t, string(got), goreleaserSignsMarker)
	assert.Equal(t, calculateHash(got), storedGoreleaserHash(t, gen))
}

// TestSigning_UnignoredEditSurvivesRegenerate runs the sequence #150 reported:
// an edited file is ignored, signing advises over it, the rule is removed, and
// the project is regenerated. The edit must survive.
func TestSigning_UnignoredEditSurvivesRegenerate(t *testing.T) {
	t.Parallel()

	gen, goreleaserPath := scaffoldWithEditedGoreleaser(t, customisedGoreleaser, true)

	require.NoError(t, gen.EnableSigning(context.Background(), testSigning))
	require.NoError(t, os.Remove(filepath.Join(filepath.Dir(goreleaserPath), ".gtb", "ignore")))

	regen := New(gen.props, &Config{Path: gen.config.Path, Overwrite: "deny"})
	regen.runCommand = gen.runCommand
	require.NoError(t, regen.RegenerateProject(context.Background()))

	got, err := os.ReadFile(goreleaserPath)
	require.NoError(t, err)
	assert.Equal(t, customisedGoreleaser, string(got), "regenerate overwrote the developer's edit")
}
