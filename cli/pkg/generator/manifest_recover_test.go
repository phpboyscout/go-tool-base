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
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// TestRegenerateManifest_FromScratchReconstructsProperties is the D6 guard for
// the 2026-07-11-manifest-feature-recovery spec: a project generated with a
// non-trivial configuration must have its manifest properties reconstructed from
// source after the manifest is deleted — features (opt-ins, a disabled default,
// and keychain from its artefact), env prefix, help, and docs layout — proving
// the from-scratch scan no longer silently drops them.
func TestRegenerateManifest_FromScratchReconstructsProperties(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Config:  emptyTestStore(t),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	const path = "/proj"

	g := New(p, &Config{Path: path})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	cfg := SkeletonConfig{
		Name:        "roundtrip",
		Repo:        "acme/roundtrip",
		Host:        "gitlab.com",
		Description: "round trip",
		Path:        path,
		EnvPrefix:   "RT",
		HelpType:    "slack",
		SlackTeam:   "Platform",
		// A disabled default (doctor), enabled opt-ins (ai/config), and keychain
		// (recovered from its blank-import artefact, not SetFeatures).
		Features: []ManifestFeature{
			{Name: "doctor", Enabled: false},
			{Name: "ai", Enabled: true},
			{Name: "config", Enabled: true},
			{Name: "keychain", Enabled: true},
		},
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	before := readManifest(t, fs, path)

	// Delete the manifest and rebuild it purely from source.
	require.NoError(t, fs.Remove(ManifestPathFor(path)))
	require.NoError(t, g.RegenerateManifest(context.Background()))

	after := readManifest(t, fs, path)

	assert.Equal(t, "roundtrip", after.Properties.Name)
	assert.Equal(t, "RT", after.Properties.EnvPrefix)
	assert.Equal(t, "slack", after.Properties.Help.Type)
	assert.Equal(t, "Platform", after.Properties.Help.SlackTeam)
	assert.Equal(t, DocsLayoutDiataxis, after.Properties.DocsLayout)

	// The delta feature set round-trips exactly: doctor disabled, ai/config
	// enabled, keychain recovered from its artefact; default-state features carry
	// no entry.
	assert.ElementsMatch(t, before.Properties.Features, after.Properties.Features)
	assert.False(t, featureEnabledIn(after.Properties.Features, "doctor"))
	assert.True(t, featureEnabledIn(after.Properties.Features, "ai"))
	assert.True(t, featureEnabledIn(after.Properties.Features, "keychain"))
	assert.True(t, featureEnabledIn(after.Properties.Features, "update"), "default-on feature inferred")
}

func readManifest(t *testing.T, fs afero.Fs, path string) Manifest {
	t.Helper()

	data, err := afero.ReadFile(fs, ManifestPathFor(path))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	return m
}
