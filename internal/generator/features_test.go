package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newFeatureProject scaffolds a plain in-memory project and returns a generator
// pointed at it, ready for ApplyFeatures calls.
func newFeatureProject(t *testing.T) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	g := New(p, &Config{})
	g.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name:        "feat-tool",
		Repo:        "acme/feat-tool",
		Host:        "github.com",
		Description: "feature toggle fixture",
		Path:        "/work",
	}))

	// A generator bound to the scaffolded project, as the CLI would build it.
	return New(p, &Config{Path: "/work", Overwrite: "allow"}), fs
}

func readRootCmd(t *testing.T, fs afero.Fs) string {
	t.Helper()

	b, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)

	return string(b)
}

func readManifestFeatures(t *testing.T, fs afero.Fs) []ManifestFeature {
	t.Helper()

	b, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(b, &m))

	return m.Properties.Features
}

func TestApplyFeatures_EnableDefaultOff(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	changed, err := g.ApplyFeatures(context.Background(), map[string]bool{"ai": true})
	require.NoError(t, err)
	assert.Equal(t, []string{"ai"}, changed)

	// Manifest records the non-default enable; root wires props.Enable(props.AiCmd).
	assert.Equal(t, []ManifestFeature{{Name: "ai", Enabled: true}}, readManifestFeatures(t, fs))
	assert.Contains(t, readRootCmd(t, fs), "props.Enable(props.AiCmd)")
}

func TestApplyFeatures_DisableDefaultOn(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	changed, err := g.ApplyFeatures(context.Background(), map[string]bool{"doctor": false})
	require.NoError(t, err)
	assert.Equal(t, []string{"doctor"}, changed)

	assert.Equal(t, []ManifestFeature{{Name: "doctor", Enabled: false}}, readManifestFeatures(t, fs))
	assert.Contains(t, readRootCmd(t, fs), "props.Disable(props.DoctorCmd)")
}

func TestApplyFeatures_ReturnToDefaultRemovesEntryAndToggle(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	// Enable ai (default-off), then disable it again → back to default.
	_, err := g.ApplyFeatures(context.Background(), map[string]bool{"ai": true})
	require.NoError(t, err)

	changed, err := g.ApplyFeatures(context.Background(), map[string]bool{"ai": false})
	require.NoError(t, err)
	assert.Equal(t, []string{"ai"}, changed)

	// Entry removed; root drops the redundant SetFeatures call entirely.
	assert.Empty(t, readManifestFeatures(t, fs))

	root := readRootCmd(t, fs)
	assert.NotContains(t, root, "props.AiCmd")
	assert.NotContains(t, root, "SetFeatures",
		"with no non-default features the root must omit SetFeatures")
}

func TestApplyFeatures_Idempotent(t *testing.T) {
	t.Parallel()

	g, _ := newFeatureProject(t)

	// ai is default-off; requesting disabled is a no-op.
	changed, err := g.ApplyFeatures(context.Background(), map[string]bool{"ai": false})
	require.NoError(t, err)
	assert.Empty(t, changed)

	// doctor is default-on; requesting enabled is a no-op.
	changed, err = g.ApplyFeatures(context.Background(), map[string]bool{"doctor": true})
	require.NoError(t, err)
	assert.Empty(t, changed)
}

func TestApplyFeatures_UnknownFeatureRejected(t *testing.T) {
	t.Parallel()

	g, _ := newFeatureProject(t)

	_, err := g.ApplyFeatures(context.Background(), map[string]bool{"nope": true})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestApplyFeatures_MultipleAtOnce(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	changed, err := g.ApplyFeatures(context.Background(), map[string]bool{
		"ai":     true,  // default-off → enable
		"config": true,  // default-off → enable
		"docs":   false, // default-on → disable
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"ai", "config", "docs"}, changed) // sorted

	root := readRootCmd(t, fs)
	assert.Contains(t, root, "props.Enable(props.AiCmd)")
	assert.Contains(t, root, "props.Enable(props.ConfigCmd)")
	assert.Contains(t, root, "props.Disable(props.DocsCmd)")
}

// TestApplyFeatures_SurvivesRegenerate encodes the keryx regression: a feature
// toggled via ApplyFeatures lives in the manifest, so a full regenerate (which
// renders from the manifest) keeps the wiring — it is not dropped the way a
// hand-edited DO-NOT-EDIT root would be.
func TestApplyFeatures_SurvivesRegenerate(t *testing.T) {
	t.Parallel()

	g, fs := newFeatureProject(t)

	_, err := g.ApplyFeatures(context.Background(), map[string]bool{"ai": true})
	require.NoError(t, err)
	require.Contains(t, readRootCmd(t, fs), "props.Enable(props.AiCmd)")

	require.NoError(t, g.RegenerateProject(context.Background()))
	assert.Contains(t, readRootCmd(t, fs), "props.Enable(props.AiCmd)",
		"a regenerate must keep the manifest-recorded feature toggle")
}

func TestValidateFeatureName(t *testing.T) {
	t.Parallel()

	for _, name := range ToggleableFeatures {
		require.NoErrorf(t, ValidateFeatureName(name), "%q should be valid", name)
	}

	for _, bad := range []string{"", "keychain", "signing", "AI", "unknown"} {
		err := ValidateFeatureName(bad)
		require.Errorf(t, err, "%q should be rejected", bad)
		require.ErrorIs(t, err, ErrInvalidInput)
	}
}
