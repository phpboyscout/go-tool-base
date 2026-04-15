package forms

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
)

// navigableKeyMap returns a huh KeyMap with Escape added to the Quit binding.
func navigableKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))

	return km
}

// NewNavigable creates a huh.Form with Escape bound to Quit and rendering
// in the alternate screen buffer. Each form gets a clean display and leaves
// no residual output when it completes or is aborted. Pressing Escape causes
// Run() to return huh.ErrUserAborted, which a Wizard interprets as
// "go back one step".
//
// In Bubble Tea v2 the alternate screen is no longer a program option; it
// is a property of the View struct. huh v2 exposes a view hook that lets
// callers mutate the final View — we use it to flip AltScreen on.
func NewNavigable(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).
		WithKeyMap(navigableKeyMap()).
		WithViewHook(func(v tea.View) tea.View {
			v.AltScreen = true

			return v
		})
}

// Wizard manages a multi-step interactive form flow with Escape-to-go-back
// navigation. Each step is either a static set of huh.Groups displayed as a
// single navigable form, or a custom function for dynamic logic.
//
// Build with NewWizard, optionally extend with Group/Step, then call Run.
type Wizard struct {
	steps []func() error
}

// NewWizard creates a Wizard whose initial steps are the given groups — each
// group becomes one navigable form step. For a purely static multi-step flow
// this is all you need:
//
//	err := forms.NewWizard(groupA, groupB, groupC).Run()
//
// For mixed flows, chain Group and Step after the constructor.
func NewWizard(groups ...*huh.Group) *Wizard {
	w := &Wizard{}

	for _, g := range groups {
		w.steps = append(w.steps, func() error {
			return NewNavigable(g).Run()
		})
	}

	return w
}

// Group adds a step that displays one or more huh groups as a single
// navigable form. Use this for static form content that doesn't depend
// on earlier steps.
func (w *Wizard) Group(groups ...*huh.Group) *Wizard {
	w.steps = append(w.steps, func() error {
		return NewNavigable(groups...).Run()
	})

	return w
}

// Step adds a custom step function for dynamic form logic, such as
// steps whose content depends on a previous selection or that need
// to perform setup before prompting.
func (w *Wizard) Step(fn func() error) *Wizard {
	w.steps = append(w.steps, fn)

	return w
}

// Run executes all steps in order with back-navigation support.
// Escape goes back one step; abort on the first step propagates
// huh.ErrUserAborted to the caller.
func (w *Wizard) Run() error {
	return runSteps(w.steps)
}

// RunStepsWithBack executes a sequence of form step functions with
// back-navigation support. Prefer Wizard for new code; this function
// is retained for backward compatibility.
func RunStepsWithBack(steps []func() error) error {
	return runSteps(steps)
}

func runSteps(steps []func() error) error {
	i := 0

	for i < len(steps) {
		err := steps[i]()
		if errors.Is(err, huh.ErrUserAborted) {
			if i == 0 {
				return err
			}

			i--

			continue
		}

		if err != nil {
			return err
		}

		i++
	}

	return nil
}
