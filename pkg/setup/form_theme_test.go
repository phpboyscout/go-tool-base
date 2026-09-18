package setup_test

import (
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestFormTheme_SetsTheFormInFromTheEdge: every wizard renders with a margin
// on the left and a blank line above, the way huh's own examples do, rather
// than against the terminal's edge.
func TestFormTheme_SetsTheFormInFromTheEdge(t *testing.T) {
	t.Parallel()

	for _, dark := range []bool{true, false} {
		styles := setup.FormTheme().Theme(dark)
		require.NotNil(t, styles)
		assert.Equal(t, 2, styles.Form.Base.GetPaddingLeft(), "dark=%v", dark)
		assert.Equal(t, 1, styles.Form.Base.GetPaddingTop(), "dark=%v", dark)
	}

	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Padded").Description("every line starts two columns in"),
	)).WithTheme(setup.FormTheme())
	f.Update(f.Init())

	view := ansi.Strip(f.View())
	require.NotEmpty(t, view)

	for _, line := range strings.Split(view, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		assert.Truef(t, strings.HasPrefix(line, "  "), "line %q is not set in from the edge", line)
	}
}
