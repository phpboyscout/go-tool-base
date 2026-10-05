// Package formpage runs a wizard's pages so that accessible mode asks what
// the TUI shows. huh's accessible runner asks every group in turn, never
// consults a group's hide condition (huh v2.0.3; huh#780) and never
// evaluates the reactive *Func bindings, and huh exposes no way to read a
// hide condition back. So a wizard declares its pages here: each visible page
// runs on its own, built when its turn comes.
package formpage

import "charm.land/huh/v2"

// Page is one group of a wizard and the condition that hides it.
type Page struct {
	build func() *huh.Group
	hide  func() bool
}

// Of is a page of a group already built.
func Of(g *huh.Group) Page {
	return Page{build: func() *huh.Group { return g }}
}

// Deferred is a page built by build. On a terminal every page is built
// before the form runs, as Of is; in accessible mode it is built when its
// turn comes, so the text it computes from earlier answers is current.
func Deferred(build func() *huh.Group) Page {
	return Page{build: build}
}

// HiddenWhen hides the page while hide reports true, on the TUI path through
// huh and in accessible mode through Run.
func (p Page) HiddenWhen(hide func() bool) Page {
	p.hide = hide

	return p
}

// Group builds the page's huh group, with its hide condition attached.
func (p Page) Group() *huh.Group {
	g := p.build()
	if p.hide != nil {
		g = g.WithHideFunc(p.hide)
	}

	return g
}

func (p Page) hidden() bool {
	return p.hide != nil && p.hide()
}

// Groups builds the pages' groups, in order.
func Groups(pages ...Page) []*huh.Group {
	groups := make([]*huh.Group, len(pages))
	for i, p := range pages {
		groups[i] = p.Group()
	}

	return groups
}

// Run runs the pages through build and run. On a terminal they are one
// form, so back-navigation crosses pages. In accessible mode each visible
// page is its own form, its hide condition asked and its group built when
// its turn comes, so the answers on earlier pages decide both.
func Run(pages []Page, accessible bool, build func(...*huh.Group) *huh.Form, run func(*huh.Form) error) error {
	if !accessible {
		return run(build(Groups(pages...)...))
	}

	for _, p := range pages {
		if p.hidden() {
			continue
		}

		if err := run(build(p.Group())); err != nil {
			return err
		}
	}

	return nil
}

// ValidateAnswer validates the answer a text field will keep. At an
// accessible prompt huh validates the line as typed, so an empty line, which
// keeps the field's current value, was validated as "" and a required field
// refused its own pre-filled answer (huh v2.0.3; huh#833). On the TUI path
// the bound value follows every keystroke, so this changes nothing there.
func ValidateAnswer(current *string, validate func(string) error) func(string) error {
	return func(s string) error {
		if s == "" {
			s = *current
		}

		return validate(s)
	}
}
