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

	t.Run("without ai the default is dropped and the list stays", func(t *testing.T) {
		t.Parallel()

		o := base()
		o.Features = generator.DefaultSelectedFeatures
		o.ChatProviders = []string{"claude", "openai"}
		o.ChatDefault.Provider = "openai"
		require.NoError(t, o.validateFields())

		cfg := o.skeletonConfig(nil)
		assert.Equal(t, []string{"claude", "openai"}, cfg.Chat.Providers, "the wiring is the tool's (#94)")
		assert.True(t, cfg.Chat.Default.IsZero(), "the default is the ai feature's")
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

// #94: the provider list is a shortcut for wiring go/chat modules and is
// recorded whether or not the ai feature is on; ai without an explicit list
// links the default set (spec 0194 D7), and an explicit empty list with ai is
// still refused.
func TestSkeletonOptions_ChatProvidersWithoutAi(t *testing.T) {
	t.Parallel()

	t.Run("a list without ai is recorded", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
			Features: generator.DefaultSelectedFeatures, ChatProviders: []string{"claude", "gemini"}}
		o.settleChatProviders()
		require.NoError(t, o.validateFields())

		cfg := o.skeletonConfig(nil)
		assert.Equal(t, []string{"claude", "gemini"}, cfg.Chat.Providers)
		assert.Empty(t, cfg.Chat.Default.Provider, "the default is the ai feature's")
	})

	t.Run("no list and no ai links nothing", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: generator.DefaultSelectedFeatures}
		o.settleChatProviders()
		require.NoError(t, o.validateFields())
		assert.Empty(t, o.skeletonConfig(nil).Chat.Providers)
	})

	t.Run("ai with no list takes the default set", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
			Features: append([]string{"ai"}, generator.DefaultSelectedFeatures...)}
		o.settleChatProviders()
		assert.Equal(t, generator.DefaultChatProviders(), o.ChatProviders)
	})

	t.Run("ai with an explicit empty list is refused", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", Repo: "org/tool", ForgeBackend: "github",
			Features: append([]string{"ai"}, generator.DefaultSelectedFeatures...), ChatProviders: []string{}, ChatProvidersSet: true}
		o.settleChatProviders()
		require.ErrorIs(t, o.validateFields(), generator.ErrChatProvidersRequired)
	})
}

// --chat-tool-bridge=false records the opt-out; leaving the flag alone keeps
// the default rule, so the manifest says nothing (#104).
func TestProjectCommand_ChatToolBridgeFlag(t *testing.T) {
	t.Parallel()

	manifestAfter := func(t *testing.T, args ...string) string {
		t.Helper()

		fs := afero.NewMemMapFs()
		cmd := NewCmdSkeleton(&props.Props{FS: fs, Logger: logger.NewNoop()}, &SharedFlags{})
		cmd.SetArgs(append([]string{"--name", "tool", "--repo", "org/tool", "--forge-backend", "github",
			"--features", "ai", "--chat-providers", "claude-local", "--path", "/work", "--no-git"}, args...))
		require.NoError(t, cmd.Execute())

		raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
		require.NoError(t, err)

		return string(raw)
	}

	t.Run("passed false, it opts out", func(t *testing.T) {
		t.Parallel()

		assert.Contains(t, manifestAfter(t, "--chat-tool-bridge=false"), "tool_bridge: false")
	})

	t.Run("left alone, the default rule stands", func(t *testing.T) {
		t.Parallel()

		assert.NotContains(t, manifestAfter(t), "tool_bridge")
	})
}
