package setup

import (
	"bytes"
	"context"
	"io"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"

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

// TestStorageModeGroup pins spec 0198 D5: the one storage-mode selector
// offers what the environment allows, starts on the recommended mode, and
// takes an answer at an accessible prompt.
func TestStorageModeGroup(t *testing.T) {
	t.Setenv("CI", "")

	out := &bytes.Buffer{}
	p := &props.Props{IO: props.StdIO{Stdin: formtest.Answers("2"), Stdout: out, Stderr: out, AccessibleMode: true}}

	var mode credentials.Mode

	group := StorageModeGroup(t.Context(), p, &mode, func() bool { return false })
	require.NotNil(t, group)
	assert.Equal(t, credentials.ModeEnvVar, mode, "the recommended mode is the default before the form runs")

	require.NoError(t, RunForm(t.Context(), p, huh.NewForm(group)))
	assert.Equal(t, credentials.ModeLiteral, mode, "no keychain answers under go test, so the second choice is literal")
	assert.Contains(t, out.String(), "Credential Storage")

	// A preset mode is kept.
	preset := credentials.ModeLiteral
	StorageModeGroup(t.Context(), p, &preset, func() bool { return false })
	assert.Equal(t, credentials.ModeLiteral, preset)
}

func TestStorageModeDescription(t *testing.T) {
	t.Parallel()

	assert.Contains(t, storageModeDescription(true), "CI environment detected")
	assert.Contains(t, storageModeDescription(false), "Environment variable references")
}
