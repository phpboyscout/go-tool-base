package generate

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// advance presses Enter on the focused field and, when the field accepted it
// (no validation error), delivers huh's own next-field message. The commands
// huh returns are not executed: they include cursor-blink ticks that block,
// and NextField (plus the group step below) is all the drive needs.
func advance(f *huh.Form, m huh.Model) (huh.Model, bool) {
	m, _ = m.Update(codeKeypress(tea.KeyEnter))

	if field := f.GetFocusedField(); field != nil && field.Error() != nil {
		return m, false
	}

	before := f.GetFocusedField()
	m, _ = m.Update(huh.NextField())

	// On a group's last field the group answers NextField with a next-group
	// command the drive does not run, so it steps the form itself.
	if f.GetFocusedField() == before {
		_ = f.NextGroup()
	}

	return m, true
}

// driveToCompletion feeds the wizard what a user types when accepting every
// default: a name, then Enter through each field, typing the repository where
// the form insists on one. It stops at the first field that refuses, and
// returns the form so the test can assert on its state.
func driveToCompletion(t *testing.T, o *SkeletonOptions) *huh.Form {
	t.Helper()

	f := o.wizardForm()
	f.Update(f.Init())

	var m huh.Model = f
	for _, r := range "my-app" {
		m, _ = m.Update(keypress(r))
	}

	for i := 0; i < 40 && f.State != huh.StateCompleted; i++ {
		if field := f.GetFocusedField(); field != nil && field.GetKey() == "repo" && o.Repo == "" {
			for _, r := range "org/my-app" {
				m, _ = m.Update(keypress(r))
			}
		}

		var ok bool
		if m, ok = advance(f, m); !ok {
			break
		}
	}

	return f
}

// TestWizard_AcceptingDefaultsCompletesAndDerivesHost pins #41: huh binds an
// Input's value once at construction, so the backend validator's seed never
// reached the Git Host field and the page refused Enter with "host is
// required". Accepting the defaults must complete the wizard, and the host
// must resolve from the backend afterwards.
func TestWizard_AcceptingDefaultsCompletesAndDerivesHost(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f := driveToCompletion(t, o)

	require.Equal(t, huh.StateCompleted, f.State, "accepting every default must complete the wizard; got repo=%q host=%q", o.Repo, o.Host)
	assert.Equal(t, "my-app", o.Name)
	assert.Equal(t, "github.com", o.resolvedHost(), "an empty host resolves from the chosen backend")
}

// TestWizard_KeepsExplicitFeatureSelection pins #42: option state is
// initialised from the current selection, so a narrow --features passed
// before the wizard opens is not widened back to the defaults when the user
// accepts the page.
func TestWizard_KeepsExplicitFeatureSelection(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: []string{"ai"}, ChatProviders: []string{"codex-local"}}
	f := driveToCompletion(t, o)

	require.Equal(t, huh.StateCompleted, f.State)
	assert.Equal(t, []string{"ai"}, o.Features, "the wizard must not add the default features back")
	assert.Equal(t, []string{"codex-local"}, o.ChatProviders, "the wizard must not add every provider back")
}

// focusFeatures drives the form to the Features multi-select.
func focusFeatures(t *testing.T, o *SkeletonOptions) *huh.Form {
	t.Helper()

	f := o.wizardForm()
	f.Update(f.Init())

	var m huh.Model = f
	for _, r := range "my-app" {
		m, _ = m.Update(keypress(r))
	}

	for i := 0; i < 3; i++ {
		m, _ = advance(f, m)
	}

	require.IsType(t, &huh.MultiSelect[string]{}, f.GetFocusedField())

	return f
}

// TestWizard_EveryFeatureIsVisibleOnFirstPaint pins #43: huh sizes an
// auto-height multi-select to its options and then subtracts the title and
// description lines, so the sixteenth feature (OS Keychain, default-ticked)
// rendered off-screen until the cursor had moved fifteen times.
func TestWizard_EveryFeatureIsVisibleOnFirstPaint(t *testing.T) {
	t.Parallel()

	f := focusFeatures(t, &SkeletonOptions{Features: generator.DefaultSelectedFeatures})
	view := fmt.Sprint(f.View())

	for _, name := range generator.SelectableFeatures {
		assert.Containsf(t, view, featureLabel(name), "feature %q is not on the first paint of the list", name)
	}
}
