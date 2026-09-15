package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

var aiOn = []ManifestFeature{{Name: string(props.AiCmd), Enabled: true}}

// TestValidateChatDefault pins spec 0196 D1, D3 and D9: the default is the
// author's to state whenever more than one provider is linked, it must be one
// of them, and the providers that refuse to construct without an endpoint
// must be given one.
func TestValidateChatDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		providers []string
		def       ManifestChatDefault
		features  []ManifestFeature
		wantErr   error
	}{
		{name: "one provider needs no default", providers: []string{"claude"}, features: aiOn},
		{name: "several providers need a default", providers: []string{"claude", "openai"}, features: aiOn, wantErr: ErrChatDefaultRequired},
		{name: "the default must be linked", providers: []string{"claude", "openai"}, def: ManifestChatDefault{Provider: "gemini"}, features: aiOn, wantErr: ErrChatDefaultNotLinked},
		{name: "a linked default is accepted", providers: []string{"claude", "openai"}, def: ManifestChatDefault{Provider: "openai", Model: "gpt-x"}, features: aiOn},
		{name: "openai-compatible needs a base URL", providers: []string{"openai-compatible"}, def: ManifestChatDefault{Provider: "openai-compatible"}, features: aiOn, wantErr: ErrChatEndpointRequired},
		{name: "a single provider's endpoint rules apply to the implied default", providers: []string{"openai-compatible"}, features: aiOn, wantErr: ErrChatEndpointRequired},
		{name: "openai-compatible with a base URL", providers: []string{"openai-compatible"}, def: ManifestChatDefault{Provider: "openai-compatible", BaseURL: "https://llm.internal/v1"}, features: aiOn},
		{name: "azure needs a base URL and an API version", providers: []string{"azure-openai"}, def: ManifestChatDefault{Provider: "azure-openai", BaseURL: "https://x.openai.azure.com"}, features: aiOn, wantErr: ErrChatEndpointRequired},
		{name: "azure complete", providers: []string{"azure-openai"}, def: ManifestChatDefault{Provider: "azure-openai", BaseURL: "https://x.openai.azure.com", APIVersion: "2024-10-21"}, features: aiOn},
		{name: "an insecure base URL is refused", providers: []string{"openai-compatible"}, def: ManifestChatDefault{Provider: "openai-compatible", BaseURL: "http://llm.internal/v1"}, features: aiOn, wantErr: ErrChatEndpointRequired},
		{name: "without ai nothing is required", providers: []string{"claude", "openai"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateChatDefault(tt.def, tt.providers, tt.features)
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestManifestChatDefault_Marshal: the default block is written only when set,
// and an absent chat block stays absent.
func TestManifestChatDefault_Marshal(t *testing.T) {
	t.Parallel()

	withDefault := ManifestProperties{Name: "tool", Chat: ManifestChat{
		Providers: []string{"claude"},
		Default:   ManifestChatDefault{Provider: "claude", Model: "claude-opus-5"},
	}}
	out, err := yaml.Marshal(withDefault)
	require.NoError(t, err)
	assert.Contains(t, string(out), "default:\n")
	assert.Contains(t, string(out), "provider: claude\n")
	assert.Contains(t, string(out), "model: claude-opus-5\n")
	assert.NotContains(t, string(out), "base_url", "empty fields are omitted")

	var back ManifestProperties
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, withDefault.Chat, back.Chat)

	noDefault := ManifestProperties{Name: "tool", Chat: ManifestChat{Providers: []string{"claude"}}}
	out, err = yaml.Marshal(noDefault)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "default:")
}

// TestDeriveMissingManifestFields_OneProviderIsItsOwnDefault: an older
// manifest with a single provider gains its default; one with several is
// left for the author to decide, and the sync says so.
func TestDeriveMissingManifestFields_OneProviderIsItsOwnDefault(t *testing.T) {
	t.Parallel()

	single := &Manifest{Properties: ManifestProperties{Features: aiOn, Chat: ManifestChat{Providers: []string{"codex-local"}}}}
	changed, err := deriveMissingManifestFields(single)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "codex-local", single.Properties.Chat.Default.Provider)

	several := &Manifest{Properties: ManifestProperties{Features: aiOn, Chat: ManifestChat{Providers: []string{"claude", "openai"}}}}
	changed, err = deriveMissingManifestFields(several)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Empty(t, several.Properties.Chat.Default.Provider, "the generator does not guess between providers")
}

// TestSyncAdapterFiles_WritesTheChatDefaultsBundle pins D4: the author's
// default lives in a bundle beside chat.go, at the path the framework's
// embedded-defaults layer reads, with an init template alongside, and
// chat.go registers it. No default, no bundle.
func TestSyncAdapterFiles_WritesTheChatDefaultsBundle(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/proj"})
	m := &Manifest{Properties: ManifestProperties{
		Name:     "tool",
		Features: aiOn,
		Chat: ManifestChat{
			Providers: []string{"openai-compatible"},
			Default:   ManifestChatDefault{Provider: "openai-compatible", Model: "local", BaseURL: "https://llm.internal/v1"},
		},
	}}

	require.NoError(t, g.syncAdapterFiles(m))

	defaults, err := afero.ReadFile(fs, "/proj/cmd/tool/chat/assets/config.yaml")
	require.NoError(t, err)
	assert.Equal(t, "ai:\n  provider: \"openai-compatible\"\n  model: \"local\"\n  base_url: \"https://llm.internal/v1\"\n", string(defaults),
		"every value is quoted, so a dated api_version stays a string")

	seed, err := afero.ReadFile(fs, "/proj/cmd/tool/chat/assets/init/config.yaml")
	require.NoError(t, err)
	assert.Equal(t, string(defaults), string(seed), "the init template seeds the same values")

	chatGo, err := afero.ReadFile(fs, "/proj/cmd/tool/chat.go")
	require.NoError(t, err)
	assert.Contains(t, string(chatGo), `//go:embed chat`)
	assert.Contains(t, string(chatGo), `setup.RegisterAssets(props.AiCmd, "chat"`)
	assert.Contains(t, string(chatGo), `fs.Sub(`)

	// The author removes the default: the bundle goes and chat.go stops
	// registering it.
	m.Properties.Chat.Default = ManifestChatDefault{}
	require.NoError(t, g.syncAdapterFiles(m))

	exists, _ := afero.Exists(fs, "/proj/cmd/tool/chat/assets/config.yaml")
	assert.False(t, exists, "no default, no bundle")

	chatGo, err = afero.ReadFile(fs, "/proj/cmd/tool/chat.go")
	require.NoError(t, err)
	assert.NotContains(t, string(chatGo), "RegisterAssets")
}
