package generate

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
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
	assert.Empty(t, o.EnvPrefix, "nothing seeds the prefix any more; the field offers it as a suggestion")
}
