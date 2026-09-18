package generate

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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
// promptable reports whether a wizard can run on this invocation's streams: a
// terminal, or accessible line prompts on any reader (--accessible,
// GTB_ACCESSIBLE=true, TERM=dumb). It is the same rule setup.RunForm applies to
// the framework's wizards; the generator's used to demand a terminal and so
// refused a piped stdin that had asked for line prompts.
func promptable(p *props.Props) bool {
	return setup.Promptable(p.GetIO())
}

// runForm runs a wizard form on the invocation's streams through the
// framework's runner, so accessible mode, the theme and the headless program
// options are applied once, in one place.
func runForm(ctx context.Context, p *props.Props, f *huh.Form) error {
	return setup.RunFormOn(ctx, p.GetIO(), f)
}

func newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).
		WithTheme(setup.FormTheme()).
		WithViewHook(func(v tea.View) tea.View {
			v.AltScreen = true

			return v
		})
}
