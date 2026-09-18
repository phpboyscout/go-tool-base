package generate

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// -- reactive-text / seed helpers ---------------------------------------------
//
// These pure helpers back the wizard's reactive field content (DescriptionFunc,
// PlaceholderFunc, SuggestionsFunc). Testing them directly covers that logic
// without a terminal; the wiring on the real form is covered by the tea.Model
// drive in wizard_bindings_test.go, and the interactive event loop by the
// generator BDD suite.

func TestBackendLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "GitHub", backendLabel(""))
	assert.Equal(t, "GitHub", backendLabel("github"))
	assert.Equal(t, "GitLab", backendLabel("gitlab"))
}

func TestRepoDescription(t *testing.T) {
	t.Parallel()
	assert.Contains(t, repoDescription("github"), "org/repo")
	assert.Contains(t, repoDescription("gitlab"), "nested groups")
}

func TestRepoPlaceholder(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "org/repo", repoPlaceholder("github"))
	assert.Equal(t, "group/subgroup/repo", repoPlaceholder("gitlab"))
}

func TestDeriveEnvPrefix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "MY_APP", deriveEnvPrefix("my-app"))
	assert.Equal(t, "GTB", deriveEnvPrefix("gtb"))
	assert.Equal(t, "A_B_C", deriveEnvPrefix("a-b-c"))
	assert.Empty(t, deriveEnvPrefix(""))
}

// -- tea.Model drive of the real wizard form ----------------------------------
//
// huh forms are Bubble Tea models, so a test can feed synthetic key events and
// assert on the bound options struct: no TTY, no global stdin, parallel-safe.
// This is how huh tests itself (see docs/development/testing/huh-form-testing.md,
// Approach C). The full drive lives in wizard_bindings_test.go.

func keypress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: string(r), Code: r, ShiftedCode: r})
}

func codeKeypress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

// TestWizardForm_NameIsBound pins the plain binding the drive relies on: text
// typed into the first field reaches the options struct on Enter.
func TestWizardForm_NameIsBound(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{}
	f := o.wizardForm()
	f.Update(f.Init())

	var m huh.Model = f
	for _, r := range "my-app" {
		m, _ = m.Update(keypress(r))
	}

	_, _ = m.Update(codeKeypress(tea.KeyEnter))

	assert.Equal(t, "my-app", o.Name)
	assert.Empty(t, o.EnvPrefix, "the prefix is resolved from the page's choice after the wizard, not typed")
}

// TestWizard_EnvPrefixDefaultsToTheDerivedName: the env-prefix page is a
// select whose default is the prefix derived from the project name, so a
// user who accepts every default gets MY_APP rather than no prefix at all.
func TestWizard_EnvPrefixDefaultsToTheDerivedName(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"repo": "org/my-app"})

	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())

	assert.Contains(t, seen, "env-prefix")
	assert.NotContains(t, seen, "env-prefix-custom", "the custom page shows only for Other")
	assert.Equal(t, "MY_APP", o.skeletonConfig(nil).EnvPrefix)
}

// TestWizard_EnvPrefixNoneAndOther: None yields no prefix; Other opens the
// custom page and takes what is typed there.
func TestWizard_EnvPrefixNoneAndOther(t *testing.T) {
	t.Parallel()

	none := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, none, map[string]string{"repo": "org/my-app", "env-prefix": "↓"})
	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, none.afterWizard())
	assert.Empty(t, none.skeletonConfig(nil).EnvPrefix)

	other := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen = keysSeen(t, other, map[string]string{"repo": "org/my-app", "env-prefix": "↓↓", "env-prefix-custom": "CUSTOM_X"})
	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, other.afterWizard())
	assert.Contains(t, seen, "env-prefix-custom")
	assert.Equal(t, "CUSTOM_X", other.skeletonConfig(nil).EnvPrefix)
}

// TestOptionsFromManifest_EnvPrefixChoice: a revisit pre-selects the choice
// the recorded prefix corresponds to, so the page reads as the author left it.
func TestOptionsFromManifest_EnvPrefixChoice(t *testing.T) {
	t.Parallel()

	for prefix, want := range map[string]string{"": envPrefixNone, "TOOL": envPrefixDerived, "OTHER_X": envPrefixOther} {
		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
			Features: generator.DefaultSelectedFeatures, EnvPrefix: prefix}
		require.NoError(t, o.validateFields())

		back := optionsFromManifest(generator.ManifestFromSkeletonConfig(o.skeletonConfig(nil), nil, "v1.0.0"))
		assert.Equalf(t, want, back.envPrefixChoice, "prefix %q", prefix)
		assert.Equal(t, prefix, back.EnvPrefix)
	}
}
