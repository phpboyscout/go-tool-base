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

	assert.Lenf(t, FeatureCatalogue, len(props.AllFeatures()),
		"FeatureCatalogue must cover exactly props.AllFeatures — a new FeatureID needs a catalogue entry")

	defaultTool := props.Tool{Features: props.SetFeatures()}
	for _, cmd := range props.AllFeatures() {
		d, ok := byCmd[cmd]
		assert.Truef(t, ok, "props.AllFeatures %q is missing from FeatureCatalogue", cmd)
		assert.Equalf(t, defaultTool.IsEnabled(cmd), d.Default,
			"catalogue Default for %q disagrees with props.DefaultFeatures", cmd)
	}
}
