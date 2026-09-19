package generate

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startWizard types the name and returns the form and model ready to drive.
func startWizard(o *SkeletonOptions) (*huh.Form, huh.Model) {
	f := o.wizardForm()
	f.Update(f.Init())

	var m huh.Model = f
	m = typeAnswer(m, "my-app")

	return f, m
}

// TestWizard_AIPage pins spec 0196 D1 and D2 on the wizard: the default
// provider is asked on the AI page, the author chooses it between several
// providers rather than the generator, one provider is its own default, and
// the endpoint pages appear only for the providers that need them.
func TestWizard_AIPage(t *testing.T) {
	t.Parallel()

	t.Run("several providers: the author must choose", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update", "ai"}, ChatProviders: []string{"claude", "openai"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "chat-default")
		require.Equal(t, "chat-default", f.GetFocusedField().GetKey(), "the default select follows the providers")

		// Enter on the placeholder is refused: nothing is chosen for the author.
		m, ok := advance(f, m)
		require.False(t, ok, "an unchosen default does not pass")
		assert.Empty(t, o.ChatDefault.Provider)

		// Down to the second provider and accept.
		m, _ = m.Update(codeKeypress(tea.KeyDown))
		m, _ = m.Update(codeKeypress(tea.KeyDown))
		_, ok = advance(f, m)
		require.True(t, ok)
		assert.Equal(t, "openai", o.ChatDefault.Provider)

		// openai needs no endpoint, so no endpoint page follows.
		assert.NotEqual(t, "chat-base-url", f.GetFocusedField().GetKey())
	})

	t.Run("one provider is its own default and its endpoint is asked", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update", "ai"}, ChatProviders: []string{"openai-compatible"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "chat-default")
		require.Equal(t, "chat-default", f.GetFocusedField().GetKey())

		m, ok := advance(f, m)
		require.True(t, ok, "a single provider is accepted as offered")
		assert.Equal(t, "openai-compatible", o.ChatDefault.Provider)

		// The model is optional.
		m, ok = advance(f, m)
		require.True(t, ok)

		require.Equal(t, "chat-base-url", f.GetFocusedField().GetKey(), "openai-compatible needs its endpoint")

		_, ok = advance(f, m)
		require.False(t, ok, "an empty base URL is refused")

		m = typeAnswer(m, "https://llm.internal/v1")
		m, ok = advance(f, m)
		require.True(t, ok)
		assert.Equal(t, "https://llm.internal/v1", o.ChatDefault.BaseURL)

		for i := 0; i < 10 && f.State != huh.StateCompleted; i++ {
			m, _ = advance(f, m)
		}

		require.Equal(t, huh.StateCompleted, f.State)
		require.NoError(t, o.afterWizard())
		assert.Equal(t, "openai-compatible", o.ChatDefault.Provider)
		assert.Equal(t, "https://llm.internal/v1", o.ChatDefault.BaseURL)
		assert.Empty(t, o.ChatDefault.Project, "cloud addressing does not apply to this provider")
	})

	t.Run("without ai the providers are still asked, the default page is skipped and the default cleared", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"update"}, ChatProviders: []string{"claude"}}
		o.ChatDefault.Provider = "claude"
		f, m := startWizard(o)
		// #94: the providers page is a permanent fixture, the wiring is the
		// tool's whether or not ai is on.
		m = driveUntilKey(f, m, o, "chat-providers")
		require.Equal(t, "chat-providers", f.GetFocusedField().GetKey(), "no ai, the providers page still shows")
		m = driveUntilKey(f, m, o, "chat-default")
		assert.NotEqual(t, "chat-default", f.GetFocusedField().GetKey(), "no ai, no AI defaults page")

		for i := 0; i < 20 && f.State != huh.StateCompleted; i++ {
			m, _ = advance(f, m)
		}

		require.Equal(t, huh.StateCompleted, f.State)
		require.NoError(t, o.afterWizard())
		assert.True(t, o.ChatDefault.IsZero(), "a default bound on a hidden page does not survive it")
	})
}
