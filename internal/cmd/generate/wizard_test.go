package generate

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
)

// -- reactive-text / seed helpers ---------------------------------------------
//
// These pure helpers back the wizard's reactive field content (DescriptionFunc /
// PlaceholderFunc) and its seeded defaults (the name→env-prefix and
// backend→host validators). Testing them directly covers that logic without a
// terminal; the seed *wiring* on the real form is covered by the tea.Model drive
// below, and the interactive event loop by the @generator BDD suite.

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
// assert on the bound options struct — no TTY, no global stdin, parallel-safe.
// This is how huh tests itself (see docs/development/testing/huh-form-testing.md,
// Approach C). Here it proves the migration's key subtlety: the name field's
// validator seeds the env-prefix default on the real form built by wizardForm.

func keypress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: string(r), Code: r, ShiftedCode: r})
}

func codeKeypress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func TestWizardForm_NameSeedsEnvPrefix(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{}
	f := o.wizardForm()
	f.Update(f.Init())

	var m huh.Model = f
	for _, r := range "my-app" {
		m, _ = m.Update(keypress(r))
	}

	// Advancing past the name field runs its validator, which seeds the prefix
	// on o via the closure — the returned model is not needed.
	_, _ = m.Update(codeKeypress(tea.KeyEnter))

	assert.Equal(t, "MY_APP", o.EnvPrefix, "the name field's validator should seed the env-prefix default")
	assert.Equal(t, "my-app", o.Name)
}
