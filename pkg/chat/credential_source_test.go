package chat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestCredentialSources_WalkTheDoctorRungs pins #131: a provider's key comes
// from the same walk doctor reports from, in its order.
func TestCredentialSources_WalkTheDoctorRungs(t *testing.T) {
	t.Setenv("CHAT_SOURCE_TEST_KEY", "from-env-ref")
	t.Setenv(EnvClaudeKey, "from-fallback")

	tests := []struct {
		name     string
		provider gochat.Provider
		yaml     string
		want     string
	}{
		{"env reference beats the literal", gochat.ProviderOpenAI,
			"openai:\n  api:\n    env: CHAT_SOURCE_TEST_KEY\n    key: literal\n", "from-env-ref"},
		{"literal", gochat.ProviderGemini, "gemini:\n  api:\n    key: literal\n", "literal"},
		{"fallback variable when config says nothing", gochat.ProviderClaude, "{}\n", "from-fallback"},
		{"azure reads its own root", gochat.ProviderAzureOpenAI, "azure:\n  api:\n    key: azure-literal\n", "azure-literal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, ok := credentialSources(chatStoreFromYAML(t, tt.yaml).View())(tt.provider)
			require.True(t, ok)

			got, err := source(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCredentialSources_NoSourceWhereGTBHoldsNoKey(t *testing.T) {
	t.Parallel()

	resolve := credentialSources(chatStoreFromYAML(t, "gemini:\n  api:\n    key: literal\n").View())

	for _, provider := range []gochat.Provider{
		gochat.ProviderClaudeLocal,
		gochat.ProviderBedrock,
		// Vertex authenticates with application default credentials and
		// refuses a set Config.Credential (chat-gemini spec 0017 D8).
		gochat.ProviderGeminiVertex,
	} {
		_, ok := resolve(provider)
		assert.False(t, ok, provider)
	}
}

// TestCredentialSources_CompatibleNeverReadsOpenAIKey: OPENAI_API_KEY belongs
// to api.openai.com, and openai-compatible talks to every other host
// (chat-openai#10), so the walk drops the fallback variable for it.
func TestCredentialSources_CompatibleNeverReadsOpenAIKey(t *testing.T) {
	t.Setenv(EnvOpenAIKey, "an-openai-key")

	resolve := credentialSources(chatStoreFromYAML(t, "{}\n").View())

	compatible, ok := resolve(gochat.ProviderOpenAICompatible)
	require.True(t, ok)
	got, err := compatible(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)

	openai, ok := resolve(gochat.ProviderOpenAI)
	require.True(t, ok)
	got, err = openai(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "an-openai-key", got)
}

// TestCredentialSources_BrokenKeychainIsAnError: with nothing below it, a
// keychain that will not answer is the error, not an empty key.
func TestCredentialSources_BrokenKeychainIsAnError(t *testing.T) {
	t.Setenv(EnvClaudeKey, "")

	source, ok := credentialSources(chatStoreFromYAML(t, "anthropic:\n  api:\n    keychain: no-slash\n").View())(gochat.ProviderClaude)
	require.True(t, ok)

	_, err := source(t.Context())
	require.ErrorIs(t, err, credentialposture.ErrMalformedKeychainRef)
}

func TestCredentialSources_NilConfigStillReadsTheFallback(t *testing.T) {
	t.Setenv(EnvGeminiKey, "from-fallback")

	source, ok := credentialSources(nil)(gochat.ProviderGemini)
	require.True(t, ok)

	got, err := source(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "from-fallback", got)
}

// TestNewFromProps_BrokenKeychainFailsConstruction: the error reaches the
// caller from chat.New rather than reading as a missing key.
func TestNewFromProps_BrokenKeychainFailsConstruction(t *testing.T) {
	t.Setenv(EnvClaudeKey, "")

	// The root module links no provider, so claude is registered here with a
	// factory that resolves its key the way every provider module does. The
	// registry has no removal, and no other pkg/chat test builds a claude client.
	gochat.RegisterProvider(gochat.ProviderClaude, func(ctx context.Context, settings gochat.Settings) (gochat.ChatClient, error) {
		if _, err := gochat.ProviderCredential(settings.Config, EnvClaudeKey)(ctx); err != nil {
			return nil, err
		}

		return &fakeClient{chatReply: "ok"}, nil
	})

	_, err := NewFromProps(t.Context(), &props.Props{
		Logger: logger.NewNoop(),
		Config: chatStoreFromYAML(t, "anthropic:\n  api:\n    keychain: no-slash\n"),
	}, gochat.Config{Provider: gochat.ProviderClaude})
	require.ErrorIs(t, err, credentialposture.ErrMalformedKeychainRef)
}

func TestSettingsFromProps_LeavesACallersCredentialAlone(t *testing.T) {
	t.Parallel()

	p := &props.Props{Logger: logger.NewNoop(), Config: chatStoreFromYAML(t, "openai:\n  api:\n    key: from-config\n")}

	settings, err := SettingsFromProps(p, gochat.Config{Provider: gochat.ProviderOpenAI, Credential: gochat.StaticCredential("mine")})
	require.NoError(t, err)
	got, err := settings.Config.Credential(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "mine", got)

	settings, err = SettingsFromProps(p, gochat.Config{Provider: gochat.ProviderOpenAI, Token: "a-token"})
	require.NoError(t, err)
	assert.Nil(t, settings.Config.Credential, "a Token beside a Credential is refused, so a caller's Token stands alone")
}
