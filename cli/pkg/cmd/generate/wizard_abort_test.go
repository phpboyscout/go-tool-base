package generate

import (
	"bytes"
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// cancelled is a context already done, so a TUI form refuses to start: the
// error path every wizard shares with ctrl+c.
func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func tuiProps() *props.Props {
	return &props.Props{IO: formtest.TUI(formtest.Keys())}
}

func TestWizards_AFormThatDoesNotRunStopsTheCommand(t *testing.T) {
	t.Parallel()

	t.Run("command", func(t *testing.T) {
		t.Parallel()

		require.Error(t, (&CommandOptions{}).ValidateOrPrompt(cancelled(), tuiProps()))
	})

	t.Run("command flag loop", func(t *testing.T) {
		t.Parallel()

		o := &CommandOptions{}
		require.Error(t, o.runFlagLoop(cancelled(), tuiProps()))
		assert.Empty(t, o.Flags, "an aborted flag form records nothing")
	})

	t.Run("project", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{ChatProviders: []string{"claude"}}
		require.Error(t, o.ValidateOrPrompt(cancelled(), tuiProps()))
	})

	t.Run("revisit", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		p.IO = tuiProps().IO

		err := (&WizardOptions{Path: "/work"}).Run(cancelled(), p, &bytes.Buffer{})
		require.Error(t, err)
		assert.Contains(t, errors.FlattenHints(err), "gtb wizard needs a terminal")
	})
}

// TestCommandPrompt_AbortingTheFlagFormStopsTheCommand drives the main page on
// the key route, asks for flags, then presses ctrl+c on the flag form. Keyed
// and paced, so not parallel.
func TestCommandPrompt_AbortingTheFlagFormStopsTheCommand(t *testing.T) {
	main := formtest.Keys("deploy", formtest.Enter, "Deploy", formtest.Enter, formtest.Enter, formtest.Enter,
		formtest.Enter, formtest.Enter, formtest.Enter, formtest.Enter, formtest.Yes, formtest.Enter, formtest.Enter)
	flag := formtest.Keys("\x03")

	o := &CommandOptions{Parent: "root"}
	err := o.ValidateOrPrompt(context.Background(), &props.Props{IO: formtest.TUIForms(main, flag)})
	require.ErrorIs(t, err, huh.ErrUserAborted)
	assert.True(t, o.AddFlags)
	assert.Empty(t, o.Flags)
}
