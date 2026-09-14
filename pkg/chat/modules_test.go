package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/errors"
)

func TestProviderModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider gochat.Provider
		module   string
		ok       bool
	}{
		{gochat.ProviderClaude, "gitlab.com/phpboyscout/go/chat-anthropic", true},
		{gochat.ProviderClaudeLocal, "gitlab.com/phpboyscout/go/chat-anthropic", true},
		{gochat.ProviderOpenAI, "gitlab.com/phpboyscout/go/chat-openai", true},
		{gochat.ProviderOpenAICompatible, "gitlab.com/phpboyscout/go/chat-openai", true},
		{gochat.ProviderCodexLocal, "gitlab.com/phpboyscout/go/chat-openai", true},
		{gochat.ProviderGemini, "gitlab.com/phpboyscout/go/chat-gemini", true},
		{gochat.ProviderGeminiVertex, "gitlab.com/phpboyscout/go/chat-gemini", true},
		{gochat.ProviderAgyLocal, "gitlab.com/phpboyscout/go/chat-gemini", true},
		{gochat.ProviderBedrock, "gitlab.com/phpboyscout/go/chat-bedrock", true},
		{gochat.ProviderAzureOpenAI, "gitlab.com/phpboyscout/go/chat-openai-azure", true},
		{gochat.Provider("nope"), "", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			t.Parallel()

			module, ok := ProviderModule(tt.provider)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.module, module)
		})
	}
}

func TestProviderModules_ListsEveryProvider(t *testing.T) {
	t.Parallel()

	modules := ProviderModules()
	require.Len(t, modules, 10)

	seen := map[gochat.Provider]bool{}
	for _, m := range modules {
		assert.False(t, seen[m.Provider], "provider %s listed twice", m.Provider)
		seen[m.Provider] = true
		assert.NotEmpty(t, m.Module)
	}
}

func TestHintUnsupportedProvider(t *testing.T) {
	t.Parallel()

	t.Run("registry miss gains the module to import", func(t *testing.T) {
		t.Parallel()

		err := hintUnsupportedProvider(errors.New("unsupported provider: bedrock"), gochat.ProviderBedrock)

		assert.Contains(t, errors.FlattenHints(err), `_ "gitlab.com/phpboyscout/go/chat-bedrock"`)
	})

	t.Run("other errors pass through", func(t *testing.T) {
		t.Parallel()

		orig := errors.New("dial tcp: refused")
		err := hintUnsupportedProvider(orig, gochat.ProviderClaude)

		assert.Same(t, orig, err)
	})

	t.Run("nil passes through", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, hintUnsupportedProvider(nil, gochat.ProviderClaude))
	})

	t.Run("unknown provider gets no hint", func(t *testing.T) {
		t.Parallel()

		orig := errors.New("unsupported provider: nope")
		err := hintUnsupportedProvider(orig, gochat.Provider("nope"))

		assert.Same(t, orig, err)
	})
}

// TestUnsupportedProviderWording pins the wording this adapter matches on. It
// is go/chat's, not ours: a change there turns the hint off silently, and this
// is the test that says so.
func TestUnsupportedProviderWording(t *testing.T) {
	t.Parallel()

	_, err := gochat.New(t.Context(), gochat.Settings{
		Config: gochat.Config{Provider: gochat.Provider("no-such-provider-for-test")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), unsupportedProviderWording)
}
