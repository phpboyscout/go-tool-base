package setup

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// trappedForm leaves a form in the state huh v2.0.3 strands a user in
// (charmbracelet/huh#655): an input refused its empty value, then shift+tab
// blurred it, which re-ran the validator, so the form refused the page change
// and the focused field no longer takes keys.
func trappedForm(t *testing.T) (*huh.Form, *string) {
	t.Helper()

	var first, second string

	f := huh.NewForm(
		huh.NewGroup(huh.NewInput().Key("first").Value(&first)),
		huh.NewGroup(huh.NewInput().Key("second").Value(&second).Validate(func(s string) error {
			if s == "" {
				return errors.New("required")
			}

			return nil
		})),
	)
	f.Update(f.Init())

	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	f.Update(huh.NextField())
	f.NextGroup()
	require.Equal(t, "second", f.GetFocusedField().GetKey())

	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Error(t, f.GetFocusedField().Error(), "the empty value is refused")

	f.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	f.Update(huh.PrevField())
	f.PrevGroup()
	require.Equal(t, "second", f.GetFocusedField().GetKey(), "going back is refused while the field is in error")

	return f, &second
}

func TestFormTrap_HuhStrandsTheField(t *testing.T) {
	t.Parallel()

	f, second := trappedForm(t)
	f.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Empty(t, *second, "upstream behaviour: the blurred field ignores the key; if this fails, huh fixed #655 and the filter can go")
}

func TestRefocusRefusedField_LetsTheUserCorrectIt(t *testing.T) {
	t.Parallel()

	f, second := trappedForm(t)
	filter := refocusRefusedField(f)

	for _, msg := range []tea.Msg{tea.KeyPressMsg{Code: 'x', Text: "x"}, tea.KeyPressMsg{Code: tea.KeyEnter}} {
		f.Update(filter(nil, msg))
	}

	assert.Equal(t, "x", *second, "the next key reaches the field")
}

// Through RunForm, the real program: refused, back, and the next key still
// lands, so the form completes with the corrected value.
func TestRunForm_ARefusedFieldStaysEditable(t *testing.T) {
	t.Parallel()

	var name string

	f := huh.NewForm(
		huh.NewGroup(huh.NewNote().Title("Start")),
		huh.NewGroup(huh.NewInput().Title("Name").Value(&name).Validate(func(s string) error {
			if s == "" {
				return errors.New("required")
			}

			return nil
		})),
	)
	p := &props.Props{IO: formtest.TUI(formtest.Keys(formtest.Enter, formtest.Enter, "\x1b[Z", "x", formtest.Enter))}

	// A trapped form never completes, so the deadline turns a regression
	// into a failure rather than a hang.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, RunForm(ctx, p, f))
	assert.Equal(t, "x", name)
}
