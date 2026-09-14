package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestModuleFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		module   string
		ok       bool
	}{
		{"github", "gitlab.com/phpboyscout/go/forge-github", true},
		{"gitlab", "gitlab.com/phpboyscout/go/forge-gitlab", true},
		{"gitea", "gitlab.com/phpboyscout/go/forge-gitea", true},
		{"codeberg", "gitlab.com/phpboyscout/go/forge-gitea", true},
		{"bitbucket", "gitlab.com/phpboyscout/go/forge-bitbucket", true},
		{"direct", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()

			module, ok := ModuleFor(tt.provider)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.module, module)
		})
	}
}

// TestModuleFor_CoversEveryProfile pins the table to the profile set: a forge
// added without a module entry would pass every other test and then produce a
// hint with no import path in it.
func TestModuleFor_CoversEveryProfile(t *testing.T) {
	t.Parallel()

	for feature, profile := range profilesByFeature {
		_, ok := ModuleFor(profile.Provider)
		assert.True(t, ok, "profile for %s names provider %q with no module entry", feature, profile.Provider)
	}
}

func TestUnlinked(t *testing.T) {
	t.Parallel()

	profiles := []Profile{
		{Feature: props.FeatureID("alpha"), Provider: "alpha", Label: "Alpha"},
		{Feature: props.FeatureID("beta"), Provider: "beta", Label: "Beta"},
		{Feature: props.FeatureID("gamma"), Provider: "gamma", Label: "Gamma"},
	}
	enabled := func(id props.FeatureID) bool { return id == "alpha" || id == "beta" }
	registered := func(provider string) bool { return provider == "alpha" }

	got := unlinked(profiles, enabled, registered)

	require.Len(t, got, 1)
	assert.Equal(t, props.FeatureID("beta"), got[0].Feature)
	assert.Equal(t, "beta", got[0].Provider)
	assert.Equal(t, "Beta", got[0].Label)
}

func TestUnlinked_DisabledFeaturesAreNotChecked(t *testing.T) {
	t.Parallel()

	profiles := []Profile{{Feature: props.FeatureID("alpha"), Provider: "alpha"}}
	nothingEnabled := func(props.FeatureID) bool { return false }
	nothingRegistered := func(string) bool { return false }

	assert.Empty(t, unlinked(profiles, nothingEnabled, nothingRegistered))
}

// TestUnlinked_RealRegistry runs the exported query against the real registry
// with every forge feature enabled. The framework links no adapter (spec 0194
// D3), so every profile is reported, each with the module the hint will name.
// cli/cmd/gtb carries the mirror test asserting the empty result.
func TestUnlinked_RealRegistry(t *testing.T) {
	t.Parallel()

	tool := props.Tool{}
	for feature := range profilesByFeature {
		tool.Features = append(tool.Features, props.Feature{ID: feature, Enabled: true})
	}

	missing := Unlinked(tool)
	require.Len(t, missing, len(profilesByFeature))

	for _, m := range missing {
		assert.NotEmpty(t, m.Module, "%s has no module for the hint", m.Label)
	}
}

func TestUnlinkedError(t *testing.T) {
	t.Parallel()

	err := UnlinkedError([]UnlinkedForge{
		{Feature: props.FeatureID("github"), Provider: "github", Label: "GitHub", Module: "gitlab.com/phpboyscout/go/forge-github"},
	})

	require.ErrorIs(t, err, ErrForgeNotLinked)
	assert.Contains(t, err.Error(), "GitHub")
	assert.Contains(t, errors.FlattenHints(err), `_ "gitlab.com/phpboyscout/go/forge-github"`)
}

func TestUnlinkedError_NilForNone(t *testing.T) {
	t.Parallel()

	assert.NoError(t, UnlinkedError(nil))
}

func TestModuleForFeature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		feature props.FeatureID
		module  string
		ok      bool
	}{
		{GithubFeature, "gitlab.com/phpboyscout/go/forge-github", true},
		{GitlabFeature, "gitlab.com/phpboyscout/go/forge-gitlab", true},
		{GiteaFeature, "gitlab.com/phpboyscout/go/forge-gitea", true},
		{CodebergFeature, "gitlab.com/phpboyscout/go/forge-gitea", true},
		{BitbucketFeature, "gitlab.com/phpboyscout/go/forge-bitbucket", true},
		{props.AiCmd, "", false},
		{props.FeatureID(""), "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.feature), func(t *testing.T) {
			t.Parallel()

			module, ok := ModuleForFeature(tt.feature)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.module, module)
		})
	}
}
