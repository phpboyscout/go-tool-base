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
		_, dup := byCmd[d.Cmd]
		assert.Falsef(t, dup, "duplicate catalogue entry for %q", d.Cmd)
		byCmd[d.Cmd] = d
	}

	// The catalogue must cover every BUILTIN feature — not every registered
	// feature. AllFeatures() reflects what this binary linked, so a plugin's
	// features are present or absent depending on the test binary's imports,
	// and asserting against it would make this guard's strictness an accident
	// of the import graph. Builtins are always registered by props itself, so
	// scoping to them is both exact and link-independent.
	//
	// Plugin-kind features (forge) are deliberately not scaffoldable yet: the
	// generator emits props.<Const>, and their constants live elsewhere. Spec
	// 0185 makes them selectable and brings them into this catalogue.
	builtins := props.FeaturesOfKind(props.KindBuiltin)

	assert.Lenf(t, FeatureCatalogue, len(builtins),
		"FeatureCatalogue must cover exactly the builtin features — a new one needs a catalogue entry")

	defaultTool := props.Tool{Features: props.SetFeatures()}
	for _, cmd := range builtins {
		d, ok := byCmd[cmd]
		assert.Truef(t, ok, "builtin %q is missing from FeatureCatalogue", cmd)
		assert.Equalf(t, defaultTool.IsEnabled(cmd), d.Default,
			"catalogue Default for %q disagrees with the registry", cmd)
	}
}
