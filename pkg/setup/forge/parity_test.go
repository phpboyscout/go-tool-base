package forge_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// The parity guards below all range over props.FeaturesOfKind(props.KindForge)
// rather than a list written here. That is the point: a forge added later is
// picked up automatically, and if it is missing an initialiser, a subcommand, a
// config bundle or display data, one of these fails — instead of the gap being
// noticed months later, which is exactly how GitLab came to have a registered
// adapter, a generator wizard option and no credential path at all.

// TestEveryForgeHasAnInitialiser is the headline guard from spec 0185: a
// registered forge with no way to configure it is the defect this spec exists
// to close.
func TestEveryForgeHasAnInitialiser(t *testing.T) {
	t.Parallel()

	initialisers := setup.GetInitialisers()

	for _, id := range props.FeaturesOfKind(props.KindForge) {
		assert.NotEmptyf(t, initialisers[id],
			"forge %q is registered but has no initialiser — it cannot be configured", id)
	}
}

// TestEveryForgeHasAnInitSubcommand pins the other half: the feature is
// reachable explicitly, not only through the full init run.
func TestEveryForgeHasAnInitSubcommand(t *testing.T) {
	t.Parallel()

	subcommands := setup.GetSubcommands()

	for _, id := range props.FeaturesOfKind(props.KindForge) {
		assert.NotEmptyf(t, subcommands[id],
			"forge %q has no `init %s` subcommand", id, id)
	}
}

// TestEveryForgeShipsAConfigBundle covers spec 0185 D4, and with it the hazard
// spec 0183 identified: vcs.ConfigFromReader(cfg).Sub(forge) returns nil when
// the section is absent, and GTB was insulated from that only because the
// GitHub bundle happened to guarantee a `github:` key existed.
func TestEveryForgeShipsAConfigBundle(t *testing.T) {
	t.Parallel()

	assets := setup.GetAssets()

	for _, id := range props.FeaturesOfKind(props.KindForge) {
		bundles := assets[id]
		require.NotEmptyf(t, bundles, "forge %q ships no config bundle", id)

		for _, b := range bundles {
			data, err := fs.ReadFile(b.Bundle, setup.DefaultsAssetPath)
			require.NoErrorf(t, err, "forge %q bundle %q has no %s",
				id, b.Name, setup.DefaultsAssetPath)

			assert.Containsf(t, string(data), string(id)+":",
				"forge %q bundle must declare a top-level %q section so Sub(%q) is non-nil",
				id, id, id)
		}
	}
}

// TestEveryForgeHasDisplayData guards the seam the project generator reads
// through. A forge with no display data would render a blank label in the
// backend chooser.
func TestEveryForgeHasDisplayData(t *testing.T) {
	t.Parallel()

	for _, id := range props.FeaturesOfKind(props.KindForge) {
		d, ok := forge.DisplayFor(id)
		require.Truef(t, ok, "forge %q has no display data", id)

		assert.NotEmptyf(t, d.Label, "forge %q has no label", id)
		assert.NotEmptyf(t, d.RepoPlaceholder, "forge %q has no repository placeholder", id)
		assert.NotEmptyf(t, d.RepoDescription, "forge %q has no repository description", id)
		assert.Equalf(t, id, d.ID, "display data for %q reports the wrong ID", id)
	}
}

// TestForgeFeaturesCoverEveryRegisteredProfile is the guard in the other
// direction: Displays() must not silently drop a forge, which it would if a
// profile were added without registering its feature.
func TestForgeFeaturesCoverEveryRegisteredProfile(t *testing.T) {
	t.Parallel()

	displays := forge.Displays()
	kinds := props.FeaturesOfKind(props.KindForge)

	assert.Len(t, displays, len(kinds),
		"every registered forge feature must have display data, and vice versa")
}

// TestNewForgeFeaturesAreRegistered extends the 0184 enumeration guard to the
// forges this spec adds. doctor's support bundle ranges over props.AllFeatures.
func TestNewForgeFeaturesAreRegistered(t *testing.T) {
	t.Parallel()

	all := props.AllFeatures()

	for _, id := range []props.FeatureID{forge.GitlabFeature, forge.GiteaFeature, forge.CodebergFeature} {
		assert.Containsf(t, all, id, "%q must appear in the feature enumeration", id)

		d, ok := props.DescriptorFor(id)
		require.Truef(t, ok, "%q must be registered", id)
		assert.Equalf(t, props.KindForge, d.Kind, "%q must be a forge", id)
		assert.Falsef(t, d.Default, "%q must not be default-enabled", id)
	}
}
