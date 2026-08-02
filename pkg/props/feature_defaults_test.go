package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDefaultEnabledDerivedFromDefaultFeatures pins that isDefaultEnabled is
// derived from DefaultFeatures rather than being an independently-maintained
// switch. For every known feature the two agree: a feature is default-enabled
// iff it appears (enabled) in SetFeatures()'s default output. This is the
// guard the old keep-in-sync comment asked humans to enforce.
func TestDefaultEnabledDerivedFromDefaultFeatures(t *testing.T) {
	t.Parallel()

	defaults := map[FeatureID]bool{}
	for _, f := range SetFeatures() {
		defaults[f.ID] = f.Enabled
	}

	for _, feature := range AllFeatures {
		assert.Equal(t, defaults[feature], isDefaultEnabled(feature),
			"isDefaultEnabled(%q) must agree with the default feature set", feature)
	}

	// Spot-check the intended default posture so the derivation itself is pinned.
	assert.True(t, isDefaultEnabled(UpdateCmd))
	assert.True(t, isDefaultEnabled(ChangelogCmd))
	assert.False(t, isDefaultEnabled(AiCmd))
	assert.False(t, isDefaultEnabled(ConfigCmd))
	assert.False(t, isDefaultEnabled(ManCmd))
}
