package formpage_test

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formpage"
)

// recorder stands in for the form runner: it records the groups of every
// form it is asked to run, in order.
type recorder struct{ runs [][]*huh.Group }

func (r *recorder) build(groups ...*huh.Group) *huh.Form {
	r.runs = append(r.runs, groups)

	return huh.NewForm(groups...)
}

func (*recorder) run(*huh.Form) error { return nil }

// threeGroups are fresh per test: building a form or hiding a group mutates it.
func threeGroups() (first, second, third *huh.Group) {
	return huh.NewGroup(huh.NewNote().Title("first")),
		huh.NewGroup(huh.NewNote().Title("second")),
		huh.NewGroup(huh.NewNote().Title("third"))
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("on a terminal every page goes into one form", func(t *testing.T) {
		t.Parallel()

		first, second, third := threeGroups()
		r := &recorder{}
		pages := []formpage.Page{formpage.Of(first), formpage.Of(second).HiddenWhen(func() bool { return true }), formpage.Of(third)}

		require.NoError(t, formpage.Run(pages, false, r.build, r.run))
		assert.Equal(t, [][]*huh.Group{{first, second, third}}, r.runs, "huh hides the page itself on the TUI path")
	})

	t.Run("accessible runs each visible page on its own", func(t *testing.T) {
		t.Parallel()

		first, second, third := threeGroups()
		r := &recorder{}
		pages := []formpage.Page{formpage.Of(first), formpage.Of(second).HiddenWhen(func() bool { return true }), formpage.Of(third)}

		require.NoError(t, formpage.Run(pages, true, r.build, r.run))
		assert.Equal(t, [][]*huh.Group{{first}, {third}}, r.runs)
	})

	t.Run("a page's hide is asked when its turn comes", func(t *testing.T) {
		t.Parallel()

		first, second, _ := threeGroups()
		shown := false
		r := &recorder{}
		pages := []formpage.Page{formpage.Of(first), formpage.Of(second).HiddenWhen(func() bool { return !shown })}

		run := func(*huh.Form) error {
			shown = true // the first page's answer reveals the second

			return nil
		}

		require.NoError(t, formpage.Run(pages, true, r.build, run))
		assert.Equal(t, [][]*huh.Group{{first}, {second}}, r.runs)
	})

	t.Run("a deferred page is built when its turn comes", func(t *testing.T) {
		t.Parallel()

		name := ""
		var built []string
		page := func(title string) formpage.Page {
			return formpage.Deferred(func() *huh.Group {
				built = append(built, title+" for "+name)

				return huh.NewGroup(huh.NewNote().Title(title))
			})
		}

		r := &recorder{}
		run := func(*huh.Form) error {
			name = "mytool" // the first page's answer

			return nil
		}

		require.NoError(t, formpage.Run([]formpage.Page{page("name"), page("summary")}, true, r.build, run))
		assert.Equal(t, []string{"name for ", "summary for mytool"}, built)
	})

	t.Run("on a terminal deferred pages are all built before the form runs", func(t *testing.T) {
		t.Parallel()

		var built int
		page := formpage.Deferred(func() *huh.Group {
			built++

			return huh.NewGroup(huh.NewNote())
		})

		r := &recorder{}
		require.NoError(t, formpage.Run([]formpage.Page{page, page}, false, r.build, r.run))
		assert.Equal(t, 2, built)
		assert.Len(t, r.runs, 1)
	})

	t.Run("a page that fails stops the run", func(t *testing.T) {
		t.Parallel()

		first, _, third := threeGroups()
		r := &recorder{}
		stop := assert.AnError
		pages := []formpage.Page{formpage.Of(first), formpage.Of(third)}

		err := formpage.Run(pages, true, r.build, func(*huh.Form) error { return stop })
		require.ErrorIs(t, err, stop)
		assert.Len(t, r.runs, 1)
	})
}

func TestGroups(t *testing.T) {
	t.Parallel()

	first := huh.NewGroup(huh.NewNote())
	second := huh.NewGroup(huh.NewNote())

	assert.Equal(t, []*huh.Group{first, second}, formpage.Groups(formpage.Of(first), formpage.Of(second)))
	assert.Same(t, first, formpage.Of(first).Group())
}

func TestValidateAnswer(t *testing.T) {
	t.Parallel()

	required := func(s string) error {
		if s == "" {
			return assert.AnError
		}

		return nil
	}

	current := "acme/mytool"
	validate := formpage.ValidateAnswer(&current, required)

	require.NoError(t, validate(""), "an empty line keeps the current answer, which is valid")
	require.NoError(t, validate("other/repo"))

	current = ""
	require.ErrorIs(t, validate(""), assert.AnError, "with nothing to keep, an empty line is still refused")
}
