package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

func TestDefaultChatProviders(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"claude", "claude-local", "openai", "openai-compatible", "codex-local",
		"gemini", "gemini-vertex", "agy-local", "bedrock", "azure-openai",
	}, DefaultChatProviders())
}

func TestValidateChatProviders(t *testing.T) {
	t.Parallel()

	ai := []ManifestFeature{{Name: string(props.AiCmd), Enabled: true}}
	noAI := []ManifestFeature{{Name: string(props.AiCmd), Enabled: false}}

	tests := []struct {
		name      string
		providers []string
		features  []ManifestFeature
		wantErr   error
	}{
		{"configurable set passes", DefaultChatProviders(), ai, nil},
		{"one provider passes", []string{"claude-local"}, ai, nil},
		{"unknown name is refused", []string{"chatgpt"}, ai, ErrUnknownChatProvider},
		{"local CLIs pass", []string{"codex-local", "agy-local"}, ai, nil},
		{"a provider the wizard cannot configure still links", []string{"bedrock"}, ai, nil},
		{"empty with ai is refused", nil, ai, ErrChatProvidersRequired},
		{"empty without ai passes", nil, noAI, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateChatProviders(tt.providers, tt.features)
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestChatModules(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"gitlab.com/phpboyscout/go/chat-anthropic", "gitlab.com/phpboyscout/go/chat-gemini"},
		chatModules([]string{"gemini", "claude", "claude-local", "not-a-provider"}))
	assert.Empty(t, chatModules(nil))
}

func TestForgeModules(t *testing.T) {
	t.Parallel()

	features := []ManifestFeature{
		{Name: string(forge.GithubFeature), Enabled: true},
		{Name: string(forge.GiteaFeature), Enabled: true},
		{Name: string(forge.CodebergFeature), Enabled: true},
		{Name: string(forge.BitbucketFeature), Enabled: false},
		{Name: string(props.AiCmd), Enabled: true},
	}

	assert.Equal(t,
		[]string{"gitlab.com/phpboyscout/go/forge-gitea", "gitlab.com/phpboyscout/go/forge-github"},
		forgeModules(features))
	assert.Empty(t, forgeModules(nil))
}

func TestSyncAdapterFiles(t *testing.T) {
	t.Parallel()

	t.Run("writes both files from the manifest", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		m := &Manifest{Properties: ManifestProperties{
			Name: "tool",
			Features: []ManifestFeature{
				{Name: string(forge.GitlabFeature), Enabled: true},
				{Name: string(props.AiCmd), Enabled: true},
			},
			Chat: ManifestChat{Providers: []string{"openai"}},
		}}

		require.NoError(t, g.syncAdapterFiles(m))

		chatGo, err := afero.ReadFile(fs, "/proj/cmd/tool/chat.go")
		require.NoError(t, err)
		assert.Contains(t, string(chatGo), `_ "gitlab.com/phpboyscout/go/chat-openai"`)
		assert.NotContains(t, string(chatGo), "chat-anthropic")

		forgeGo, err := afero.ReadFile(fs, "/proj/cmd/tool/forge.go")
		require.NoError(t, err)
		assert.Contains(t, string(forgeGo), `_ "gitlab.com/phpboyscout/go/forge-gitlab"`)
	})

	t.Run("no chat block with ai enabled records the default set", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		m := &Manifest{Properties: ManifestProperties{
			Name:     "tool",
			Features: []ManifestFeature{{Name: string(props.AiCmd), Enabled: true}},
		}}
		require.NoError(t, fs.MkdirAll("/proj/.gtb", 0o755))
		require.NoError(t, g.marshalManifestFile(ManifestPathFor("/proj"), m))

		// The default is recorded before rendering (so a later hash
		// persistence cannot drop it) and the files follow the manifest.
		require.NoError(t, g.syncDerivedManifestFields(m))
		require.NoError(t, g.syncAdapterFiles(m))

		assert.Equal(t, DefaultChatProviders(), m.Properties.Chat.Providers)

		written, err := g.decodeManifestFile(ManifestPathFor("/proj"))
		require.NoError(t, err)
		assert.Equal(t, DefaultChatProviders(), written.Properties.Chat.Providers)

		chatGo, err := afero.ReadFile(fs, "/proj/cmd/tool/chat.go")
		require.NoError(t, err)
		assert.Contains(t, string(chatGo), "chat-anthropic")
		assert.Contains(t, string(chatGo), "chat-openai")
		assert.Contains(t, string(chatGo), "chat-gemini")
	})

	t.Run("ai disabled leaves no chat.go, and removes one left over", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		require.NoError(t, afero.WriteFile(fs, "/proj/cmd/tool/chat.go", []byte("package main\n"), 0o644))

		m := &Manifest{Properties: ManifestProperties{
			Name:     "tool",
			Features: []ManifestFeature{{Name: string(props.AiCmd), Enabled: false}},
		}}

		require.NoError(t, g.syncAdapterFiles(m))

		assert.Nil(t, m.Properties.Chat.Providers)

		exists, err := afero.Exists(fs, "/proj/cmd/tool/chat.go")
		require.NoError(t, err)
		assert.False(t, exists, "a feature the tool does not use leaves no file behind")
	})

	// #94: the provider list is the record of linked modules, a shortcut for
	// a tool that drives go/chat from its own code; the ai feature is the
	// switch for GTB's AI-based features and is not needed to link.
	t.Run("providers without ai render chat.go", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		m := &Manifest{Properties: ManifestProperties{
			Name:     "tool",
			Features: []ManifestFeature{{Name: string(props.AiCmd), Enabled: false}},
			Chat:     ManifestChat{Providers: []string{"claude", "gemini"}},
		}}

		require.NoError(t, g.syncAdapterFiles(m))

		chatGo, err := afero.ReadFile(fs, "/proj/cmd/tool/chat.go")
		require.NoError(t, err)
		assert.Contains(t, string(chatGo), `_ "gitlab.com/phpboyscout/go/chat-anthropic"`)
		assert.Contains(t, string(chatGo), `_ "gitlab.com/phpboyscout/go/chat-gemini"`)
		assert.Contains(t, string(chatGo), `props.DeclareLinks(props.ChatLinkPrefix, "claude", "gemini")`)
		assert.NotContains(t, string(chatGo), "chat/assets", "the defaults bundle is the ai feature's")
	})

	t.Run("no forge feature leaves no forge.go, and removes one left over", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		require.NoError(t, afero.WriteFile(fs, "/proj/cmd/tool/forge.go", []byte("package main\n"), 0o644))

		m := &Manifest{Properties: ManifestProperties{Name: "tool"}}

		require.NoError(t, g.syncAdapterFiles(m))

		exists, err := afero.Exists(fs, "/proj/cmd/tool/forge.go")
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

func TestRecoverChatProviders(t *testing.T) {
	t.Parallel()

	t.Run("absent file recovers nil", func(t *testing.T) {
		t.Parallel()

		g, _ := newPureGenerator(t, &Config{Path: "/proj"})
		assert.Nil(t, g.recoverChatProviders())
	})

	t.Run("recovers every provider the linked modules register", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		require.NoError(t, fs.MkdirAll("/proj/cmd/tool", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/proj/cmd/tool/chat.go",
			[]byte("package main\n\nimport _ \"gitlab.com/phpboyscout/go/chat-gemini\"\n"), 0o644))

		assert.Equal(t, []string{"gemini", "gemini-vertex", "agy-local"}, g.recoverChatProviders())
	})

	t.Run("recovers the declared links exactly when the file has them", func(t *testing.T) {
		t.Parallel()

		// A chat.go the generator writes since #81 declares the author's
		// choice; that beats the widest reading of the imported modules.
		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		require.NoError(t, fs.MkdirAll("/proj/cmd/tool", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/proj/cmd/tool/chat.go", []byte(`package main

import (
	props "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	_ "gitlab.com/phpboyscout/go/chat-gemini"
)

func init() {
	props.DeclareLinks(props.ChatLinkPrefix, "agy-local")
}
`), 0o644))

		assert.Equal(t, []string{"agy-local"}, g.recoverChatProviders())
	})
}

// The provider list links its modules whatever the ai feature says (#94);
// the feature gates GTB's AI-based features, not the wiring.
func TestChatModulesFor_FollowsTheListNotTheFeature(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"gitlab.com/phpboyscout/go/chat-anthropic"}, chatModulesFor([]string{"claude"}))
	assert.Empty(t, chatModulesFor(nil))

	assert.False(t, chatFileWanted(ManifestProperties{}))
	assert.True(t, chatFileWanted(ManifestProperties{Chat: ManifestChat{Providers: []string{"claude"}}}))
	assert.True(t, chatFileWanted(ManifestProperties{Features: []ManifestFeature{{Name: string(props.AiCmd), Enabled: true}}}),
		"under ai an empty list is still a file")
}
