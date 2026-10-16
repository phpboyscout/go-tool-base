package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// Spec 0209 OQ3: the Configuration page asks the key case, defaulting to
// folded, which declares nothing.
func TestWizard_ConfigurationPageKeyCase(t *testing.T) {
	t.Parallel()

	t.Run("accepted, it declares nothing", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Repo: "org/my-app"}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "config-key-case")
		require.Equal(t, "config-key-case", f.GetFocusedField().GetKey())

		finishWizard(t, f, m)
		require.NoError(t, o.afterWizard())
		assert.Empty(t, o.ConfigKeyCase)
	})

	t.Run("exact is chosen", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Repo: "org/my-app"}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "config-key-case")

		m = typeAnswer(m, "↓")
		m, ok := advance(f, m)
		require.True(t, ok)

		finishWizard(t, f, m)
		require.NoError(t, o.afterWizard())
		assert.Equal(t, "exact", o.ConfigKeyCase)
		assert.Equal(t, "exact", o.skeletonConfig(nil).ConfigKeyCase)
	})
}

func TestSkeletonOptions_ConfigKeyCaseIsValidated(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: generator.DefaultSelectedFeatures}
	o.ConfigKeyCase = "upper"
	require.Error(t, o.validateFields())

	o.ConfigKeyCase = "exact"
	require.NoError(t, o.validateFields())
	assert.Equal(t, "exact", o.skeletonConfig(nil).ConfigKeyCase)
}

func TestWizardSummary_ShowsTheKeyCase(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "my-app", Repo: "org/my-app", ConfigKeyCase: "exact"}
	assert.Contains(t, shownNote(t, o.wizardSummary()), "exact")

	o.ConfigKeyCase = ""
	assert.Contains(t, shownNote(t, o.wizardSummary()), "folded")
}

// A revisit of an exact project keeps it exact.
func TestOptionsFromManifest_KeepsTheKeyCase(t *testing.T) {
	t.Parallel()

	start := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
		Features: generator.DefaultSelectedFeatures, ConfigKeyCase: "exact"}
	require.NoError(t, start.validateFields())

	m := generator.ManifestFromSkeletonConfig(start.skeletonConfig(nil), nil, "v1.0.0")
	require.Equal(t, "exact", m.Properties.Config.KeyCase)
	assert.Equal(t, "exact", optionsFromManifest(m).ConfigKeyCase)
}
