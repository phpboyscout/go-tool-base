package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// customisedGoreleaser is a deliberately hand-edited .goreleaser.yaml carrying
// markers the stock skeleton never renders: a second build (the macOS .app),
// an app_bundles block, and a comment explaining a deliberate Windows
// exclusion. If `gtb enable signing` re-renders the skeleton over this file,
// every marker below disappears — exactly the krites regression in issue #4.
const customisedGoreleaser = `# CUSTOMISED BY THE PROJECT — DO NOT REGENERATE (listed in .gtb/ignore)
version: 2

before:
  hooks:
    - ./scripts/stage-onnxruntime.sh   # SENTINEL: onnx dylib staging

builds:
  - id: krites
    main: ./cmd/krites
    binary: krites
    goos: [linux, darwin]   # SENTINEL: Windows deliberately excluded — POSIX-only dlopen/Setsid
  - id: krites-app          # SENTINEL: the macOS .app executable
    main: ./cmd/krites-app
    binary: krites-app

app_bundles:                # SENTINEL: signed .app packaging
  - id: krites-app
    icon: ./assets/krites.icns

dmg:                        # SENTINEL: dmg packaging
  - id: krites-dmg
`

// sentinels are substrings present only in the customised file, never in the
// stock skeleton render. Their disappearance is the data loss issue #4 reports.
var sentinels = []string{
	"stage-onnxruntime.sh",
	"Windows deliberately excluded",
	"krites-app",
	"app_bundles:",
	"dmg:",
}

// scaffoldPlainProjectWithIgnoredGoreleaser scaffolds a signing-less project,
// overwrites .goreleaser.yaml with a customised release config, lists it in
// .gtb/ignore, and records the customised on-disk hash into the manifest —
// exactly the state `gtb regenerate project` leaves an ignored file in
// ("skip generation but hash on-disk content", skeleton.go hashIgnoredFile).
func scaffoldPlainProjectWithIgnoredGoreleaser(t *testing.T) (*Generator, string) {
	t.Helper()

	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	require.NoError(t, g.GenerateSkeleton(context.Background(), signingSkeletonConfig(path, ManifestSigning{})))

	goreleaserPath := filepath.Join(path, goreleaserAssetRelPath)
	require.NoError(t, os.WriteFile(goreleaserPath, []byte(customisedGoreleaser), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(path, ".gtb"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, ".gtb", "ignore"),
		[]byte(goreleaserAssetRelPath+"\n"), 0o644))

	// Re-point the generator at the scaffolded project.
	gen := New(g.props, &Config{Path: path, Overwrite: "allow"})
	gen.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	// Record the customised file's on-disk hash into the manifest, mirroring
	// what `regenerate project` does for an ignored file (hashIgnoredFile).
	m, err := gen.loadManifest()
	require.NoError(t, err)

	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}

	m.Hashes[goreleaserAssetRelPath] = calculateHash([]byte(customisedGoreleaser))
	require.NoError(t, gen.writeManifest(m))

	return gen, goreleaserPath
}

// TestEnableSigning_PreservesCustomisedIgnoredGoreleaser is the red test for
// issue #4: a customised .goreleaser.yaml, listed in .gtb/ignore and hash-
// recorded by a prior regenerate, must SURVIVE `gtb enable signing`. On main
// enable-signing re-renders the skeleton over it (regenerateGoreleaserAsset),
// and because the recorded hash equals the customised content's hash the
// conflict guard sees "unchanged" and overwrites — every sentinel is lost.
func TestEnableSigning_PreservesCustomisedIgnoredGoreleaser(t *testing.T) {
	gen, goreleaserPath := scaffoldPlainProjectWithIgnoredGoreleaser(t)

	require.NoError(t, gen.EnableSigning(context.Background(), ManifestSigning{
		KeyID:     "alias/acme-release-signing-v1",
		KeySource: "both",
	}))

	got, err := os.ReadFile(goreleaserPath)
	require.NoError(t, err)

	for _, sentinel := range sentinels {
		assert.Contains(t, string(got), sentinel,
			"customised .goreleaser.yaml lost %q — enable signing clobbered an ignored file", sentinel)
	}
}

// TestEnableSigning_HonoursGtbIgnore asserts the narrower contract: any path
// listed in .gtb/ignore must be left byte-for-byte untouched by enable
// signing. On main the command never calls LoadIgnoreRules, so the ignore
// entry has no effect and the file is rewritten.
func TestEnableSigning_HonoursGtbIgnore(t *testing.T) {
	gen, goreleaserPath := scaffoldPlainProjectWithIgnoredGoreleaser(t)

	require.NoError(t, gen.EnableSigning(context.Background(), ManifestSigning{
		KeyID:     "alias/acme-release-signing-v1",
		KeySource: "both",
	}))

	got, err := os.ReadFile(goreleaserPath)
	require.NoError(t, err)

	assert.Equal(t, customisedGoreleaser, string(got),
		"path listed in .gtb/ignore was modified by enable signing")
}
