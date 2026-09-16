package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDefaultsAgree pins that a Set resolved with no states and the
// SetFeatures() default output describe the same posture: a feature is
// default-enabled in one iff it is in the other. Both derive from the
// descriptors' Default, so this is the guard against a second switch.
func TestDefaultsAgree(t *testing.T) {
	t.Parallel()

	defaults := map[FeatureID]bool{}
	for _, f := range SetFeatures() {
		defaults[f.ID] = f.Enabled
	}

	set := enabledFor(t, Tool{})

	for _, d := range set.Descriptors() {
		assert.Equal(t, defaults[d.FeatureID()], set.Enabled(d.FeatureID()),
			"default for %q must agree between SetFeatures and the resolved Set", d.FeatureID())
	}

	// Spot-check the intended default posture so the derivation itself is pinned.
	assert.True(t, set.Enabled(UpdateCmd))
	assert.True(t, set.Enabled(ChangelogCmd))
	assert.False(t, set.Enabled(AiCmd))
	assert.False(t, set.Enabled(ConfigCmd))
	assert.False(t, set.Enabled(ManCmd))
}
