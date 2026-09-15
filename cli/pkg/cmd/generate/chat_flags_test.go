package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// The flag path for spec 0196 phase 1 (D9): the author's default is validated
// against the linked providers and reaches SkeletonConfig as recorded.
func TestSkeletonOptions_ChatFlags(t *testing.T) {
	t.Parallel()

	base := func() *SkeletonOptions {
		return &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
			Features: append([]string{"ai"}, generator.DefaultSelectedFeatures...)}
	}

	t.Run("several providers need a default", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.ChatProviders = generator.DefaultChatProviders()
		require.ErrorIs(t, o.validateFields(), generator.ErrChatDefaultRequired)
	})

	t.Run("one provider is its own default", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.ChatProviders = []string{"codex-local"}
		require.NoError(t, o.validateFields())
		assert.Equal(t, "codex-local", o.skeletonConfig(nil).Chat.Default.Provider)
	})

	t.Run("the default must be linked", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.ChatProviders = []string{"claude", "openai"}
		o.ChatDefault.Provider = "gemini"
		require.ErrorIs(t, o.validateFields(), generator.ErrChatDefaultNotLinked)

		o.ChatDefault.Provider = "openai"
		o.ChatDefault.Model = "gpt-x"
		require.NoError(t, o.validateFields())

		cfg := o.skeletonConfig(nil)
		assert.Equal(t, "openai", cfg.Chat.Default.Provider)
		assert.Equal(t, "gpt-x", cfg.Chat.Default.Model)
	})

	t.Run("openai-compatible needs its base URL", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.ChatProviders = []string{"openai-compatible"}
		require.ErrorIs(t, o.validateFields(), generator.ErrChatEndpointRequired)

		o.ChatDefault.BaseURL = "https://llm.internal/v1"
		require.NoError(t, o.validateFields())
		assert.Equal(t, "https://llm.internal/v1", o.skeletonConfig(nil).Chat.Default.BaseURL)
	})

	t.Run("without ai the default is dropped", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.Features = generator.DefaultSelectedFeatures
		o.ChatProviders = []string{"claude", "openai"}
		o.ChatDefault.Provider = "openai"
		require.NoError(t, o.validateFields())
		assert.True(t, o.skeletonConfig(nil).Chat.IsZero(), "no ai, no chat block")
	})
}

// TestProjectCommand_ChatFlagsExist pins the six flags by name.
func TestProjectCommand_ChatFlagsExist(t *testing.T) {
	t.Parallel()

	cmd := NewCmdSkeleton(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &SharedFlags{})
	for _, name := range []string{"chat-default-provider", "chat-default-model", "chat-base-url", "chat-api-version", "chat-project", "chat-location"} {
		assert.NotNilf(t, cmd.Flags().Lookup(name), "flag --%s", name)
	}
}
