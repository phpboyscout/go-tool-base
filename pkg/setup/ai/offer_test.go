package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestProviderOptions_OfferEveryDisplayedProvider pins spec 0196 D7 (phase 2):
// init ai offers the framework's whole provider table, not the three original
// API providers, labelled from the same table the wizard and help text use.
func TestProviderOptions_OfferEveryDisplayedProvider(t *testing.T) {
	t.Parallel()

	opts := providerOptions(func(gochat.Provider) bool { return true })
	displays := chat.ProviderDisplays()
	require.Len(t, opts, len(displays))

	for i, d := range displays {
		assert.Equal(t, string(d.ID), opts[i].Value)
		assert.Contains(t, opts[i].Key, d.Label)
	}
}

// TestRunAIForms_LocalProviderSkipsTheCredentialStages: a provider that
// carries no GTB credential (a local CLI, bedrock) is written as the provider
// and nothing else; the storage and key forms never run.
func TestRunAIForms_LocalProviderSkipsTheCredentialStages(t *testing.T) {
	t.Parallel()

	store := testutil.StoreFromYAML(t, "")

	// The answers stop at the provider: were the storage page asked, the
	// form would run out of input and fail.
	props := newTestProps(t)
	props.IO = localIO(t, "claude-local")

	got, err := runAIForms(t.Context(), props, store.View(), allLinked)
	require.NoError(t, err)
	assert.Equal(t, string(gochat.ProviderClaudeLocal), got.Provider)
	assert.Empty(t, got.StorageMode, "no credential, no storage-mode page")
}

// TestIsAIConfigured_LocalProviderNeedsNoKey: a tool whose ai.provider is a
// local CLI is configured with the provider alone.
func TestIsAIConfigured_LocalProviderNeedsNoKey(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: testutil.StoreFromYAML(t, "ai:\n  provider: codex-local\n")}
	assert.True(t, IsAIConfigured(p))

	p = &props.Props{Config: testutil.StoreFromYAML(t, "ai:\n  provider: azure-openai\n")}
	assert.False(t, IsAIConfigured(p), "an API provider still needs its key")

	p = &props.Props{Config: testutil.StoreFromYAML(t, "ai:\n  provider: azure-openai\nazure:\n  api:\n    env: AZ\n")}
	assert.True(t, IsAIConfigured(p), "an env reference configures an API provider")
}

// TestProviderKeys_DeriveFromChat: the per-provider key helpers are the
// framework's one derivation, so azure and the local CLIs are covered
// without another hand-kept switch.
func TestProviderKeys_DeriveFromChat(t *testing.T) {
	t.Parallel()

	assert.Equal(t, chat.ConfigKeyAzureKey, providerConfigKey("azure-openai"))
	assert.Equal(t, chat.ConfigKeyAzureEnv, providerEnvConfigKey("azure-openai"))
	assert.Equal(t, chat.ConfigKeyAzureKeychain, providerKeychainConfigKey("azure-openai"))
	assert.Equal(t, "azure.api", providerKeychainAccount("azure-openai"))
	assert.Equal(t, chat.EnvAzureKey, providerEnvVar("azure-openai"))
	assert.Equal(t, chat.ConfigKeyOpenAIKey, providerConfigKey("openai-compatible"))
	assert.Empty(t, providerConfigKey("claude-local"))
	assert.True(t, isValidProvider("claude-local"), "every provider the module table knows is valid")
	assert.False(t, isValidProvider("anthropic"), "a credential section name is not a provider")
}

// TestDefaultProviderForm_EnvNoteSaysFallback pins OQ3: AI_PROVIDER is read
// only when ai.provider is unset, and the note says so instead of promising
// an override the code does not perform.
func TestDefaultProviderForm_EnvNoteSaysFallback(t *testing.T) {
	t.Setenv(chat.EnvAIProvider, "openai")

	note := envOverrideNote()
	require.NotEmpty(t, note)
	assert.Contains(t, note, "only when ai.provider is unset")
	assert.NotContains(t, note, "takes precedence over the config file")
}
