package ai

import (
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

// mockKeychainFormCreator drives the wizard through the keychain
// stage as if the user selected OS keychain mode, entered an API
// key, and submitted. ExistingKey is left blank so the form path
// mirrors a first-time setup rather than a re-run.
func mockKeychainFormCreator(provider, apiKey string) func(*AIConfig) []*huh.Form {
	return func(cfg *AIConfig) []*huh.Form {
		cfg.Provider = provider
		cfg.StorageMode = credentials.ModeKeychain
		cfg.APIKey = apiKey

		return nil
	}
}

// End-to-end: the wizard's keychain mode must (a) write the API key
// through the registered backend and (b) record the
// "<tool>/<account>" reference under `{provider}.api.keychain` in
// the config file — with no literal key persisted.
func TestRunAIInit_KeychainMode_WritesReferenceAndStoresSecret(t *testing.T) {
	credtest.Install(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(props, dir, WithAIForm(mockKeychainFormCreator("openai", "sk-keychain-test")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	text := string(content)
	assert.Contains(t, text, "provider: openai")
	// The keychain ref uses "<tool>/<account>" — tool name is
	// "test-tool" via newTestProps, account is "openai.api".
	assert.Contains(t, text, "keychain: test-tool/openai.api",
		"keychain mode must record the keychain reference in config")
	assert.NotContains(t, text, "sk-keychain-test",
		"API key must never appear in the config file under keychain mode")

	// And the secret itself must be retrievable from the backend.
	got, err := credentials.Retrieve(t.Context(), "test-tool", "openai.api")
	require.NoError(t, err)
	assert.Equal(t, "sk-keychain-test", got)
}

// Configure path (as opposed to RunAIInit) exercises the shared
// container flow used by the top-level wizard. It must Set the
// keychain config key — not the literal key — on the container.
func TestAIInitialiser_Configure_KeychainMode(t *testing.T) {
	credtest.Install(t)

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyAIProvider).Return("").Maybe()
	cfg.EXPECT().GetString(mock.Anything).Return("").Maybe()
	cfg.EXPECT().Set(chat.ConfigKeyAIProvider, string(chat.ProviderClaude)).Once()
	cfg.EXPECT().Set(chat.ConfigKeyClaudeKeychain, "test-tool/anthropic.api").Once()

	i := &AIInitialiser{
		formOpts: []FormOption{
			WithAIForm(mockKeychainFormCreator(string(chat.ProviderClaude), "sk-ant-configure-keychain")),
		},
	}

	err := i.Configure(newTestProps(t), cfg)
	require.NoError(t, err)

	got, err := credentials.Retrieve(t.Context(), "test-tool", "anthropic.api")
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-configure-keychain", got)
}

// A blank APIKey in keychain mode must be a no-op on the backend and
// must not produce an error — tests that bypass the form can leave
// APIKey blank without side-effects on the backend.
func TestStoreAIKeyInKeychain_BlankKeyIsNoop(t *testing.T) {
	credtest.Install(t)

	ref, err := storeAIKeyInKeychain("test-tool", &AIConfig{
		Provider: string(chat.ProviderOpenAI),
		APIKey:   "",
	})
	require.NoError(t, err)
	assert.Empty(t, ref, "blank API key must not generate a config reference")

	_, retrieveErr := credentials.Retrieve(t.Context(), "test-tool", "openai.api")
	require.ErrorIs(t, retrieveErr, credentials.ErrCredentialNotFound,
		"blank key must not have been written")
}

// Missing provider account => error. This guards against a future
// refactor that adds a provider without wiring up its keychain
// account slot — the wizard should fail loudly rather than silently
// writing to an unnamed slot.
func TestStoreAIKeyInKeychain_UnknownProviderErrors(t *testing.T) {
	credtest.Install(t)

	_, err := storeAIKeyInKeychain("test-tool", &AIConfig{
		Provider: "unknown-provider",
		APIKey:   "something",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot write keychain entry")
}
