package ai

import (
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockKeychainFormCreator("openai", "sk-keychain-test")))
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
// editor flow used by the top-level wizard. It must Set the
// keychain config key — not the literal key — through the editor.
func TestAIInitialiser_Configure_KeychainMode(t *testing.T) {
	credtest.Install(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))
	// Provider and keychain reference commit in ONE transactional Apply, with
	// the sibling env/literal key paths removed. The keychain external store
	// runs before this Apply, so a locked keychain aborts before the provider
	// is written — never orphaning it.
	cfg.EXPECT().Apply([]config.Change{
		config.Set(chat.ConfigKeyAIProvider, string(gochat.ProviderClaude)),
		config.Set(chat.ConfigKeyClaudeKeychain, "test-tool/anthropic.api"),
		config.Remove(chat.ConfigKeyClaudeEnv),
		config.Remove(chat.ConfigKeyClaudeKey),
	}).Return(nil).Once()

	i := &AIInitialiser{
		formOpts: []FormOption{
			WithAIForm(mockKeychainFormCreator(string(gochat.ProviderClaude), "sk-ant-configure-keychain")),
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
		Provider: string(gochat.ProviderOpenAI),
		APIKey:   "",
	})
	require.NoError(t, err)
	assert.Empty(t, ref, "blank API key must not generate a config reference")

	_, retrieveErr := credentials.Retrieve(t.Context(), "test-tool", "openai.api")
	require.ErrorIs(t, retrieveErr, credentials.ErrCredentialNotFound,
		"blank key must not have been written")
}

// TestWriteAIConfig_KeychainFailureLeavesNothing is the regression guard for
// the orphaned-provider bug: when the OS keychain store fails, writeAIConfig
// must not have persisted the provider (or anything) to the config file. Not
// parallel and no credtest.Install, so the default stub backend's Store fails.
func TestWriteAIConfig_KeychainFailureLeavesNothing(t *testing.T) {
	props := newTestProps(t)
	props.Assets = p.NewAssets()

	editor, path, err := setup.OpenConfigEditor(t.Context(), props, "/cfg", false)
	require.NoError(t, err)

	// A non-empty, non-secret-looking value: it only needs to be present so the
	// keychain store is attempted (and fails), without tripping gosec G101.
	placeholder := "should-not-persist"

	writeErr := writeAIConfig(editor, "test-tool", &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.ModeKeychain,
		APIKey:      placeholder,
	})
	require.Error(t, writeErr, "a failed keychain store must abort the write")

	assert.False(t, editor.View().IsSet("ai.provider"),
		"the provider must not be persisted when the credential write fails")

	if data, rerr := afero.ReadFile(props.FS, path); rerr == nil {
		assert.NotContains(t, string(data), "provider",
			"nothing must reach the config file when the keychain store fails")
	}
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
