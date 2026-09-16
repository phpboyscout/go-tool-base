package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"
)

// TestCatalogue_IsTheRegistry is spec 0199 T5: the catalogue is derived from
// the registry, so every scaffoldable descriptor this binary declares appears
// exactly once with its ConstName and ConstPackage, and its default agrees
// with what SetFeatures resolves. There is no second table to keep in step.
func TestCatalogue_IsTheRegistry(t *testing.T) {
	t.Parallel()

	snapshot := features.Default().Snapshot()
	catalogue := Catalogue()

	seen := map[props.FeatureID]bool{}
	for _, d := range catalogue {
		assert.NotEmptyf(t, d.ConstName, "descriptor for %q needs a ConstName", d.ID)
		assert.NotEmptyf(t, d.ConstPackage, "descriptor for %q needs a ConstPackage", d.ID)
		assert.Falsef(t, seen[d.ID], "duplicate catalogue entry for %q", d.ID)
		seen[d.ID] = true
	}

	var want []props.FeatureID
	for _, k := range []props.FeatureKind{props.KindBuiltin, props.KindForge, props.KindLink} {
		for _, d := range snapshot.OfKind(k) {
			want = append(want, d.FeatureID())
		}
	}

	assert.Len(t, catalogue, len(want), "the catalogue is every builtin, forge and link the generator links")
	for _, id := range want {
		assert.Truef(t, seen[id], "%q is declared but not in the catalogue", id)
	}

	defaults, err := features.Resolve(snapshot, props.StatesOf(props.SetFeatures()))
	require.NoError(t, err)

	for _, d := range catalogue {
		assert.Equalf(t, defaults.Enabled(d.ID), d.Default, "catalogue Default for %q disagrees with the registry", d.ID)
	}
}

// TestCatalogue_LinksEveryScaffoldableFeature: the generator's catalogue is
// complete because this package links the forges and the keychain link. A
// forge or the keychain missing here would be a project the generator could
// not scaffold.
func TestCatalogue_LinksEveryScaffoldableFeature(t *testing.T) {
	t.Parallel()

	ids := map[props.FeatureID]props.FeatureKind{}
	for _, d := range Catalogue() {
		ids[d.ID] = d.Kind
	}

	for _, id := range []props.FeatureID{forge.GithubFeature, forge.GitlabFeature, forge.GiteaFeature, forge.CodebergFeature, forge.BitbucketFeature} {
		assert.Equalf(t, props.KindForge, ids[id], "%q must be a forge in the catalogue", id)
	}

	assert.Equal(t, props.KindLink, ids[keychain.KeychainFeature], "keychain is a link kind in the catalogue")

	d, ok := CatalogueEntry(string(keychain.KeychainFeature))
	require.True(t, ok)
	assert.Equal(t, "KeychainFeature", d.ConstName)
	assert.Equal(t, keychain.PackagePath, d.ConstPackage)
	assert.False(t, d.Default, "a link's presence is its enablement; the manifest entry decides")
}

// TestCatalogueIn_FollowsTheRegistryGiven: adding a descriptor to a registry
// adds a catalogue row; a plugin kind a downstream declares is not GTB's to
// scaffold and is filtered.
func TestCatalogueIn_FollowsTheRegistryGiven(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	require.NoError(t, r.Declare(props.FeatureDescriptor{ID: "extra", ConstName: "ExtraCmd", ConstPackage: "example.com/extra", Kind: props.KindBuiltin}))
	require.NoError(t, r.Declare(props.FeatureDescriptor{ID: "theirs", ConstName: "Theirs", ConstPackage: "example.com/theirs", Kind: "plugin"}))

	rows := CatalogueIn(r.Snapshot())
	require.Len(t, rows, 1)
	assert.Equal(t, props.FeatureID("extra"), rows[0].ID)

	_, ok := CatalogueEntry("nope")
	assert.False(t, ok)
}
