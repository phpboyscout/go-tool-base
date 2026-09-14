package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestFeatureCatalogue_CoversAllFeatures is the anti-fragility guard: it fails
// if a props.FeatureID is added without a matching FeatureCatalogue entry (so
// the generator's renderer and scanner would silently drop it), and if the
// catalogue's default-enabled state disagrees with what SetFeatures resolves
// from props.DefaultFeatures. Because the renderer (getFeatureCmd) and the
// scanner (extractFeaturesFromSetFeatures) both loop over this one table, keeping
// it complete and correct is what keeps them symmetric.
func TestFeatureCatalogue_CoversAllFeatures(t *testing.T) {
	t.Parallel()

	byCmd := make(map[props.FeatureID]FeatureDescriptor, len(FeatureCatalogue))
	for _, d := range FeatureCatalogue {
		assert.NotEmptyf(t, d.ConstName, "descriptor for %q needs a ConstName", d.Cmd)
		assert.NotEmptyf(t, d.ConstPackage, "descriptor for %q needs a ConstPackage", d.Cmd)
		_, dup := byCmd[d.Cmd]
		assert.Falsef(t, dup, "duplicate catalogue entry for %q", d.Cmd)
		byCmd[d.Cmd] = d
	}

	// The catalogue must cover every builtin AND every forge feature. Builtins
	// are always registered by props itself; the forge features are registered
	// by pkg/setup/forge, which this package imports for its catalogue entries
	// — so both sets are present here by construction rather than by accident
	// of the import graph, which is what previously kept this guard scoped to
	// builtins alone.
	//
	// Ranging over AllFeatures() would still be wrong: a downstream tool may
	// register features of its own, and those are not GTB's to scaffold.
	scaffoldable := append(
		props.FeaturesOfKind(props.KindBuiltin),
		props.FeaturesOfKind(props.KindForge)...,
	)

	assert.Lenf(t, FeatureCatalogue, len(scaffoldable),
		"FeatureCatalogue must cover exactly the builtin and forge features — a new one needs a catalogue entry")

	defaultTool := props.Tool{Features: props.SetFeatures()}

	for _, cmd := range scaffoldable {
		d, ok := byCmd[cmd]
		if !assert.Truef(t, ok, "feature %q is missing from FeatureCatalogue", cmd) {
			continue
		}

		assert.Equalf(t, defaultTool.IsEnabled(cmd), d.Default,
			"catalogue Default for %q disagrees with the registry", cmd)

		// The registry already records where each constant is declared. Cross-
		// checking it here means the emitter's qualifier cannot drift from the
		// package that actually declares the identifier.
		reg, found := props.DescriptorFor(cmd)
		if assert.Truef(t, found, "feature %q is not in the props registry", cmd) {
			assert.Equalf(t, reg.ConstPackage, d.ConstPackage,
				"catalogue ConstPackage for %q disagrees with the registry", cmd)
			assert.Equalf(t, reg.ConstName, d.ConstName,
				"catalogue ConstName for %q disagrees with the registry", cmd)
		}
	}
}
