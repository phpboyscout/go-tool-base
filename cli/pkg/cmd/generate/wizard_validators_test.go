package generate

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// typeAndSubmit types text into the focused field and presses Enter, then
// reports the field's validation error, if any. The field is not advanced.
func typeAndSubmit(f *huh.Form, m huh.Model, text string) (huh.Model, error) {
	m = typeAnswer(m, text)
	m, _ = m.Update(codeKeypress(tea.KeyEnter))

	if field := f.GetFocusedField(); field != nil {
		return m, field.Error()
	}

	return m, nil
}

// TestWizard_FieldsRefuseWhatTheGeneratorRefuses pins #48: the wizard's
// inline validators used to be weaker than generator.Validate*, so "My App",
// "1FOO" or "org//repo" passed the form and were rejected after the last
// page with every answer gone. Each field now delegates to the generator and
// refuses in place.
func TestWizard_FieldsRefuseWhatTheGeneratorRefuses(t *testing.T) {
	t.Parallel()

	t.Run("project name", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
		f := o.wizardForm()
		f.Update(f.Init())

		_, err := typeAndSubmit(f, f, "My App")
		require.Error(t, err, "a name the generator refuses must be refused at the field")
		assert.Contains(t, err.Error(), "Name", "the message names the field")
	})

	t.Run("environment prefix", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
		f := o.wizardForm()
		f.Update(f.Init())

		var m huh.Model = f
		m = typeAnswer(m, "my-app")
		m = driveUntilKey(f, m, o, "env-prefix")
		require.Equal(t, "env-prefix", f.GetFocusedField().GetKey())

		// Other, then the custom prefix page.
		m = typeAnswer(m, "↓↓")
		m, ok := advance(f, m)
		require.True(t, ok, "Other is accepted and the custom page follows")
		require.Equal(t, "env-prefix-custom", f.GetFocusedField().GetKey())

		_, err := typeAndSubmit(f, m, "1FOO")
		require.Error(t, err, "a prefix starting with a digit is refused by the generator")
	})

	t.Run("repository", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
		f := o.wizardForm()
		f.Update(f.Init())

		var m huh.Model = f
		m = typeAnswer(m, "my-app")
		m = driveUntilKey(f, m, o, "repo")
		require.Equal(t, "repo", f.GetFocusedField().GetKey())

		_, err := typeAndSubmit(f, m, "org//repo")
		require.Error(t, err, "an empty path segment is refused by the generator")
	})

	t.Run("chat providers cannot be emptied", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update", "ai"}, ChatProviders: generator.DefaultChatProviders()}
		f := o.wizardForm()
		f.Update(f.Init())

		var m huh.Model = f
		m = typeAnswer(m, "my-app")
		m = driveUntilKey(f, m, o, "chat-providers")
		require.Equal(t, "chat-providers", f.GetFocusedField().GetKey())

		// ctrl+a toggles every option off in huh's multi-select.
		m, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
		_, _ = m.Update(codeKeypress(tea.KeyEnter))

		require.Error(t, f.GetFocusedField().Error(), "the ai feature needs at least one provider, refused at the field")
	})
}
