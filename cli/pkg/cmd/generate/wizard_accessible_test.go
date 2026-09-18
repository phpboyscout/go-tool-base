package generate

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// pipedAccessible is a stdin that is not a terminal, with accessible mode
// asked for: --accessible or GTB_ACCESSIBLE=true on a piped stdin.
func pipedAccessible(answers io.Reader) *props.Props {
	return &props.Props{IO: props.StdIO{Stdin: answers, Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}}
}

// TestGeneratorWizards_HonourAccessible: the generator's wizards run as line
// prompts on a piped stdin when accessible mode is asked for, the rule the
// framework's wizards already follow. They used to demand a terminal and
// refuse, although the notes promised every wizard honours --accessible.
func TestGeneratorWizards_HonourAccessible(t *testing.T) {
	t.Parallel()

	t.Run("piped stdin without accessible is still refused", func(t *testing.T) {
		t.Parallel()

		assert.False(t, promptable(nobodyTyping()))
		require.ErrorIs(t, (&AddFlagOptions{}).ValidateOrPrompt(context.Background(), nobodyTyping()), ErrNonInteractive)
		require.ErrorIs(t, (&CommandOptions{}).ValidateOrPrompt(context.Background(), nobodyTyping()), ErrNonInteractive)
		require.ErrorIs(t, (&SkeletonOptions{}).ValidateOrPrompt(context.Background(), nobodyTyping()), ErrNonInteractive)
	})

	t.Run("add-flag takes its answers as line prompts", func(t *testing.T) {
		t.Parallel()

		p := pipedAccessible(formtest.Answers("deploy", "env", "1", "Target environment", "e", "n", "."))
		require.True(t, promptable(p))

		o := &AddFlagOptions{}
		require.NoError(t, o.ValidateOrPrompt(context.Background(), p))

		assert.Equal(t, "deploy", o.CommandName)
		assert.Equal(t, "env", o.FlagName)
		assert.Equal(t, "string", o.FlagType)
		assert.Equal(t, "Target environment", o.Description)
		assert.Equal(t, "e", o.Shorthand)
		assert.False(t, o.Persistent)
		assert.Equal(t, ".", o.Path)
	})

	t.Run("an accessible terminal is promptable too", func(t *testing.T) {
		t.Parallel()

		assert.True(t, promptable(&props.Props{IO: formtest.AccessibleTTY(formtest.Answers())}))
	})
}
