package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

// logCapturer is the narrow slice of the buffer logger the advisory tests need
// (its concrete type is unexported), so the helper can return it by name.
type logCapturer interface {
	Contains(substr string) bool
}

// scaffoldProjectWithGoreleaser scaffolds a signing-less project, overwrites
// .goreleaser.yaml with the given content (optionally listing it in
// .gtb/ignore), records its on-disk hash into the manifest, and returns a
// generator whose logger captures output for advisory assertions.
func scaffoldProjectWithGoreleaser(t *testing.T, content string, ignore bool) (*Generator, string, logCapturer) {
	t.Helper()

	path := t.TempDir()
	scaffold := newSkeletonGeneratorForTest(t, afero.NewOsFs())
	require.NoError(t, scaffold.GenerateSkeleton(context.Background(), signingSkeletonConfig(path, ManifestSigning{})))

	goreleaserPath := filepath.Join(path, goreleaserAssetRelPath)
	require.NoError(t, os.WriteFile(goreleaserPath, []byte(content), 0o644))

	if ignore {
		require.NoError(t, os.MkdirAll(filepath.Join(path, ".gtb"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(path, ".gtb", "ignore"),
			[]byte(goreleaserAssetRelPath+"\n"), 0o644))
	}

	buf := logger.NewBuffer()
	p := &props.Props{FS: afero.NewOsFs(), Logger: buf, Config: emptyTestStore(t)}
	gen := New(p, &Config{Path: path, Overwrite: "allow"})
	gen.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	m, err := gen.loadManifest()
	require.NoError(t, err)

	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}

	m.Hashes[goreleaserAssetRelPath] = calculateHash([]byte(content))
	require.NoError(t, gen.writeManifest(m))

	return gen, goreleaserPath, buf
}

// customisedNonIgnoredGoreleaser is the same hand-edited release config, but
// NOT listed in .gtb/ignore — the safe-injection happy path.
const customisedNonIgnoredGoreleaser = `# CUSTOMISED BY THE PROJECT
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

// TestEnableSigning_InjectsIntoCustomisedNonIgnoredGoreleaser is the safe-
// injection happy path: a customised .goreleaser.yaml that is NOT ignored keeps
// all of its customisation and gains only the top-level signs: block. Every
// sentinel survives, the signs invocation is present, and the file still parses.
func TestEnableSigning_InjectsIntoCustomisedNonIgnoredGoreleaser(t *testing.T) {
	gen, goreleaserPath, _ := scaffoldProjectWithGoreleaser(t, customisedNonIgnoredGoreleaser, false)

	require.NoError(t, gen.EnableSigning(context.Background(), ManifestSigning{
		KeyID:     "alias/acme-release-signing-v1",
		KeySource: "both",
	}))

	got, err := os.ReadFile(goreleaserPath)
	require.NoError(t, err)

	// Every pre-existing block survives the injection untouched.
	for _, sentinel := range sentinels {
		assert.Contains(t, string(got), sentinel,
			"safe injection lost customisation %q", sentinel)
	}

	// The signs: block was added, complete, with the concrete manifest values.
	assert.Contains(t, string(got), "signs:")
	assert.Contains(t, string(got), goreleaserSignsMarker)
	assert.Contains(t, string(got), "cmd: gtb")
	assert.Contains(t, string(got), `- "alias/acme-release-signing-v1"`)

	// Everything before the appended block is byte-for-byte the original.
	assert.Greater(t, len(string(got)), len(customisedNonIgnoredGoreleaser))
	assert.Equal(t, customisedNonIgnoredGoreleaser, string(got)[:len(customisedNonIgnoredGoreleaser)],
		"safe injection must leave existing content byte-for-byte intact")

	// The result must still be valid YAML.
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(got, &doc))
	assert.Contains(t, doc, "signs")
	assert.Contains(t, doc, "app_bundles")
}

// authorSignsGoreleaser carries an author-written top-level signs: block with
// no gtb marker — enable must not clobber it.
const authorSignsGoreleaser = `# CUSTOMISED BY THE PROJECT
version: 2

builds:
  - id: krites
    main: ./cmd/krites

signs:                      # author's own signing block — gtb must not touch this
  - id: my-own-signing
    cmd: cosign
    artifacts: all
`

// TestEnableSigning_AdvisesWhenSignsBlockPresent is the advisory-paste path:
// when a signs: block is already present (author-written, no gtb marker), enable
// signing does NOT modify the file — it advises, printing the exact block to
// paste with the path — while the rest of enable signing still proceeds
// (trustkeys scaffold + root wiring land).
func TestEnableSigning_AdvisesWhenSignsBlockPresent(t *testing.T) {
	gen, goreleaserPath, buf := scaffoldProjectWithGoreleaser(t, authorSignsGoreleaser, false)

	require.NoError(t, gen.EnableSigning(context.Background(), ManifestSigning{
		KeyID:     "alias/acme-release-signing-v1",
		KeySource: "both",
	}))

	// The author's release config is left byte-for-byte unchanged.
	got, err := os.ReadFile(goreleaserPath)
	require.NoError(t, err)
	assert.Equal(t, authorSignsGoreleaser, string(got),
		"enable signing clobbered an author-written signs: block")

	// A fail-loud advisory was emitted, with the exact block to paste.
	assert.True(t, buf.Contains("Release config not modified"),
		"expected a fail-loud advisory when a signs: block is already present")
	assert.True(t, buf.Contains("cmd: gtb"),
		"advisory must print the exact signs: block for the user to paste")

	// The rest of enable signing still proceeded.
	assert.FileExists(t, filepath.Join(filepath.Dir(goreleaserPath), "internal", "trustkeys", "trustkeys.go"))
}

// TestDisableSigning_RemovesOnlyGtbInjectedBlock proves the disable side is the
// inverse of injection: enabling then disabling on a customised, non-ignored
// file restores it byte-for-byte (only the gtb-injected block is removed), and
// disabling never removes an author-written signs: block.
func TestDisableSigning_RemovesOnlyGtbInjectedBlock(t *testing.T) {
	t.Run("enable then disable round-trips to the original", func(t *testing.T) {
		gen, goreleaserPath, _ := scaffoldProjectWithGoreleaser(t, customisedNonIgnoredGoreleaser, false)

		require.NoError(t, gen.EnableSigning(context.Background(), ManifestSigning{
			KeyID:     "alias/acme-release-signing-v1",
			KeySource: "both",
		}))
		require.NoError(t, gen.DisableSigning(context.Background()))

		got, err := os.ReadFile(goreleaserPath)
		require.NoError(t, err)
		assert.Equal(t, customisedNonIgnoredGoreleaser, string(got),
			"disable must remove only the gtb-injected block, restoring the original")
	})

	t.Run("an author-written signs block is left untouched", func(t *testing.T) {
		gen, goreleaserPath, _ := scaffoldProjectWithGoreleaser(t, authorSignsGoreleaser, false)

		require.NoError(t, gen.DisableSigning(context.Background()))

		got, err := os.ReadFile(goreleaserPath)
		require.NoError(t, err)
		assert.Equal(t, authorSignsGoreleaser, string(got),
			"disable clobbered an author-written signs: block")
	})
}
