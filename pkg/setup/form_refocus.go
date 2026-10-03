package setup

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// refocusRefusedField works around charmbracelet/huh#655 (open at v2.0.3):
// leaving a field blurs it, the blur re-runs its validator, and the form then
// refuses the page change, stranding the user on a field that takes no keys.
// While the form holds an error, each key re-focuses the field it is on.
func refocusRefusedField(f *huh.Form) func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, isKey := msg.(tea.KeyPressMsg); !isKey || len(f.Errors()) == 0 {
			return msg
		}

		if field := f.GetFocusedField(); field != nil {
			_ = field.Focus()
		}

		return msg
	}
}
