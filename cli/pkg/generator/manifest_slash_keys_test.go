package generator

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// A manifest regenerated on Windows keys nested skeleton files with
// backslashes. Regenerating the same project elsewhere must still find every
// hash, or an unedited file a newer gtb renders differently reads as not
// created by gtb and is kept (#191).
func TestGenerateSkeleton_ReadsBackslashHashKeys(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)
	g.config.Overwrite = OverwriteDeny
	cfg := SkeletonConfig{
		Name: "sttool", Repo: "acme/sttool", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		Description: "slash keys", Path: "/work",
		Features:       []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}, {Name: "gitlab", Enabled: true}},
		ReleaseChannel: ReleaseChannelStatic,
		ReleaseBaseURL: "https://pkg.acme.dev/acme/sttool",
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	const nested, older = "scripts/move-pointer.sh", "#!/bin/sh\n# rendered by an older gtb\n"
	require.NoError(t, afero.WriteFile(fs, "/work/"+nested, []byte(older), 0o755))

	m := readIssue13Manifest(t, fs)
	windows := map[string]string{}

	for key, hash := range m.Hashes {
		windows[strings.ReplaceAll(key, "/", `\`)] = hash
	}

	windows[`scripts\move-pointer.sh`] = calculateHash([]byte(older))
	m.Hashes = windows
	require.NoError(t, EncodeManifestFile(fs, "/work/.gtb/manifest.yaml", &m))

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	updated, err := afero.ReadFile(fs, "/work/"+nested)
	require.NoError(t, err)
	assert.NotEqual(t, older, string(updated), "a file matching its recorded hash takes the new render")

	for key := range readIssue13Manifest(t, fs).Hashes {
		assert.NotContains(t, key, `\`, "the manifest is written back with slash keys")
	}
}

func TestSlashHashKeys(t *testing.T) {
	t.Parallel()

	m := Manifest{Hashes: map[string]string{
		`scripts\push.sh`: "windows",
		`docs\a\b.md`:     "stale",
		"docs/a/b.md":     "newer",
		"go.sum":          "flat",
	}}

	m.slashHashKeys()

	assert.Equal(t, map[string]string{
		"scripts/push.sh": "windows",
		"docs/a/b.md":     "newer",
		"go.sum":          "flat",
	}, m.Hashes)
}

// regenerate manifest writes the project's hashes back, so it repairs a
// Windows-written manifest too.
func TestRegenerateManifest_WritesSlashHashKeys(t *testing.T) {
	t.Parallel()

	manifest := "properties:\n  name: test-tool\nhashes:\n  'scripts\\move-pointer.sh': abc\n"

	fs, g := roundTripProject(t, "approve", approveSource, manifest)
	require.NoError(t, g.RegenerateManifest(context.Background()))

	_, m := readRoundTripManifest(t, fs)
	assert.Equal(t, map[string]string{"scripts/move-pointer.sh": "abc"}, m.Hashes)
}
