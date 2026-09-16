package setup

import (
	"context"
	"io"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestRunForm pins spec 0198 D2: one helper runs every form on the
// invocation's streams, refuses a stdin that is neither a terminal nor in
// accessible mode before the form opens, and drives a real form headlessly
// when it is.
func TestRunForm(t *testing.T) {
	t.Parallel()

	t.Run("refuses a non-interactive, non-accessible stdin", func(t *testing.T) {
		t.Parallel()

		var name string
		f := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Name").Value(&name)))
		p := &props.Props{IO: props.StdIO{Stdin: formtest.Answers("x"), Stdout: io.Discard, Stderr: io.Discard}}

		err := RunForm(context.Background(), p, f)
		require.ErrorIs(t, err, ErrNonInteractive)
		assert.Empty(t, name, "the form never opened")
	})

	t.Run("accessible answers drive the real form", func(t *testing.T) {
		t.Parallel()

		var mode, name string
		yes := false
		f := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Mode").Options(huh.NewOption("Env", "env"), huh.NewOption("Keychain", "keychain")).Value(&mode),
			huh.NewInput().Title("Var").Value(&name),
			huh.NewConfirm().Title("Sure?").Value(&yes),
		))
		p := &props.Props{IO: props.StdIO{Stdin: formtest.Answers("2", "MY_VAR", "y"), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}}

		require.NoError(t, RunForm(context.Background(), p, f))
		assert.Equal(t, "keychain", mode)
		assert.Equal(t, "MY_VAR", name)
		assert.True(t, yes)
	})

	t.Run("keys drive the real TUI path", func(t *testing.T) {
		t.Parallel()

		var mode string
		f := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Mode").Options(huh.NewOption("Env", "env"), huh.NewOption("Keychain", "keychain")).Value(&mode),
		))
		p := &props.Props{IO: formtest.TUI(formtest.Keys(formtest.Down, formtest.Enter))}

		require.NoError(t, RunForm(context.Background(), p, f))
		assert.Equal(t, "keychain", mode)
	})
}
