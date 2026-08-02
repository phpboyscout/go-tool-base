package forge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestForgeFeaturesAreEnumerable is the regression guard for a live defect: the
// github and bitbucket features were created inline as props.FeatureID("github")
// and never registered anywhere, so props.AllFeatures did not contain them —
// and doctor's support bundle, which ranges over it, silently omitted both from
// every report it has ever produced.
//
// The blank import is the whole mechanism: importing this package must be
// sufficient for the features to appear.
func TestForgeFeaturesAreEnumerable(t *testing.T) {
	t.Parallel()

	all := props.AllFeatures()

	assert.Contains(t, all, forge.GithubFeature,
		"github must appear in the feature enumeration; doctor's report ranges over it")
	assert.Contains(t, all, forge.BitbucketFeature,
		"bitbucket must appear in the feature enumeration")
}

// TestForgeFeaturesAreForgeKind pins the classification, which is what lets a
// caller ask "what forges are there?" instead of hand-listing them.
func TestForgeFeaturesAreForgeKind(t *testing.T) {
	t.Parallel()

	forges := props.FeaturesOfKind(props.KindForge)

	assert.Contains(t, forges, forge.GithubFeature)
	assert.Contains(t, forges, forge.BitbucketFeature)
	assert.NotContains(t, forges, props.UpdateCmd, "a builtin is not a forge")
}

// TestForgeFeaturesAreNotDefaultOn guards the property that makes blank imports
// safe to reason about: importing a provider changes what is available, never
// what is on.
func TestForgeFeaturesAreNotDefaultOn(t *testing.T) {
	t.Parallel()

	for _, id := range []props.FeatureID{forge.GithubFeature, forge.BitbucketFeature} {
		d, ok := props.DescriptorFor(id)
		require.Truef(t, ok, "%q must be registered", id)
		assert.Falsef(t, d.Default, "%q must not be default-enabled", id)

		var tool props.Tool
		assert.Falsef(t, tool.IsEnabled(id), "%q must be off unless a tool enables it", id)
	}
}
