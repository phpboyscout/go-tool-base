package ignore

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
)

// writeManifest seeds a .gtb/manifest.yaml at "." (the path checkDivergedUnignored
// hardcodes) with the given tracked-file hashes.
func writeManifest(t *testing.T, fs afero.Fs, hashes map[string]string) {
	t.Helper()

	m := &generator.Manifest{
		Properties: generator.ManifestProperties{Name: "mytool"},
		Version:    generator.ManifestVersion{GoToolBase: "v1.0.0"},
		Hashes:     hashes,
	}
	require.NoError(t, fs.MkdirAll(".gtb", 0o755))
	require.NoError(t, generator.EncodeManifestFile(fs, generator.ManifestPathFor("."), m))
}

func TestCheckDivergedUnignored_SkipsWhenNotAGeneratedProject(t *testing.T) {
	t.Parallel()

	p := newTestProps(afero.NewMemMapFs())

	res := checkDivergedUnignored(context.Background(), p)

	assert.Equal(t, "skip", res.Status)
	assert.Contains(t, res.Message, "no .gtb/manifest.yaml")
}

func TestCheckDivergedUnignored_PassesWhenNothingDiverged(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)

	// Tracked file has a stored hash but is absent on disk: nothing to
	// overwrite, so it never diverges -> pass.
	writeManifest(t, fs, map[string]string{"justfile": "some-hash"})

	res := checkDivergedUnignored(context.Background(), p)

	assert.Equal(t, "pass", res.Status)
	assert.Contains(t, res.Message, "no diverged")
}

func TestCheckDivergedUnignored_WarnsOnDivergedUnignoredFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)

	// Stored hash cannot match the on-disk content, and the file is not
	// ignored -> it will prompt on the next regenerate -> warn.
	writeManifest(t, fs, map[string]string{"justfile": "stale-hash-that-cannot-match"})
	require.NoError(t, afero.WriteFile(fs, "justfile", []byte("drifted content\n"), 0o644))

	res := checkDivergedUnignored(context.Background(), p)

	assert.Equal(t, "warn", res.Status)
	assert.Contains(t, res.Message, "diverged")
	assert.Contains(t, res.Details, "justfile")
	assert.Contains(t, res.Details, "gtb ignore add")
}
