package generate

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// newForm builds a huh.Form rendered in the alternate screen buffer, so each
// wizard leaves a clean display and no residual output on exit. In Bubble Tea v2
// the alternate screen is a property of the View rather than a program option,
// so it is set through the view hook.
//
// Back-navigation is huh's native shift+tab: it moves across every visible group
// in the form, skipping hidden ones and preserving entered values. ctrl+c
// aborts. Conditional sections use Group.WithHideFunc and content that depends on
// an earlier answer uses the reactive *Func binders — together they let a single
// form express what previously needed a multi-form wizard with faked back-steps.
func newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).
		WithViewHook(func(v tea.View) tea.View {
			v.AltScreen = true

			return v
		})
}
