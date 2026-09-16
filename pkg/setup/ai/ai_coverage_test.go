package ai

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestRegisteredProviders exercises the closures registered in init()
// by reading them back from the setup registry and invoking them. The
// init() body only runs at import time; calling the closures here
// covers the provider/subcommand/feature-flag bodies.
func TestRegisteredProviders(t *testing.T) {
	// NOT t.Parallel(), and the sub-tests below are serial too: the registered
	// AI closures both READ the package-level skipAI flag (InitialiserProvider)
	// and WRITE it (FeatureFlag binds &skipAI via pflag.BoolVarP). Running the
	// read and write sub-tests concurrently is a data race on process-global
	// flag state — exactly the pattern CLAUDE.md's "no package-level mocking
	// hooks" guidance forbids under t.Parallel(). Keep them sequential.
	props := newTestProps(t)
	props.Assets = p.NewAssets()

	t.Run("initialiser provider returns AIInitialiser when not skipped", func(t *testing.T) {
		ips := setup.GetInitialisers()[p.AiCmd]
		require.NotEmpty(t, ips, "AI initialiser provider must be registered")

		// skipAI defaults to false; the provider should yield a live
		// initialiser whose name matches.
		var got setup.Initialiser
		for _, ip := range ips {
			if i := ip(props); i != nil {
				got = i
			}
		}

		require.NotNil(t, got)
		assert.Equal(t, "AI integration", got.Name())
	})

	t.Run("subcommand provider yields the init ai command", func(t *testing.T) {
		sps := setup.GetSubcommands()[p.AiCmd]
		require.NotEmpty(t, sps, "AI subcommand provider must be registered")

		found := false
		for _, sp := range sps {
			for _, cmd := range sp(props) {
				if cmd.Use == "ai" {
					found = true
				}
			}
		}

		assert.True(t, found, "registered subcommand provider must emit the `ai` command")
	})

	t.Run("feature flag registers --skip-ai", func(t *testing.T) {
		fps := setup.GetFeatureFlags()[p.AiCmd]
		require.NotEmpty(t, fps, "AI feature-flag provider must be registered")

		cmd := NewCmdInitAI(props)
		for _, fp := range fps {
			fp(cmd)
		}

		assert.NotNil(t, cmd.Flags().Lookup("skip-ai"),
			"feature flag must register the --skip-ai flag")
	})
}

// TestInitialiserProviderSkips covers the skipAI=true branch of the
// registered initialiser provider closure. Serial: mutates the
// package-level skipAI flag.
func TestInitialiserProviderSkips(t *testing.T) {
	prev := skipAI
	skipAI = true
	t.Cleanup(func() { skipAI = prev })

	props := newTestProps(t)
	props.Assets = p.NewAssets()

	ips := setup.GetInitialisers()[p.AiCmd]
	require.NotEmpty(t, ips)

	for _, ip := range ips {
		assert.Nil(t, ip(props), "provider must return nil when skipAI is set")
	}
}

// TestNewAIInitialiser_MountsAssets covers NewAIInitialiser and Name.
func TestNewAIInitialiser_MountsAssets(t *testing.T) {
	t.Parallel()

	props := newTestProps(t)
	props.Assets = p.NewAssets()

	init := NewAIInitialiser(props)
	require.NotNil(t, init)
	assert.Equal(t, "AI integration", init.Name())

	// A nil Assets must not panic.
	props.Assets = nil
	assert.NotNil(t, NewAIInitialiser(props))
}

// TestProviderGroup covers the provider page in both the plain path and the
// AI_PROVIDER note path.
func TestProviderGroup(t *testing.T) {
	t.Run("without env override", func(t *testing.T) {
		t.Setenv(chat.EnvAIProvider, "")

		assert.NotNil(t, providerGroup(&AIConfig{}, allLinked))
	})

	t.Run("with env override note", func(t *testing.T) {
		t.Setenv(chat.EnvAIProvider, "openai")

		assert.NotNil(t, providerGroup(&AIConfig{}, allLinked))
	})
}

// TestDefaultEnvVarForm exercises the env-var-name form constructor,
// including the default-name population branch.
// TestAIForm_PagesFollowTheAnswers drives the one form through the key route
// and asserts the pages a person sees: the env var page for env-var mode, the
// key page otherwise, neither for a provider without a credential. The pages
// themselves are what the RunAIInit tests exercise; this pins their gating.
func TestAIForm_PagesFollowTheAnswers(t *testing.T) {
	t.Parallel()

	view := testutil.ViewFromYAML(t, "")

	envVar := &AIConfig{Provider: "claude", StorageMode: credentials.ModeEnvVar}
	f := aiForm(t.Context(), envVar, view, allLinked)
	require.NotNil(t, f)

	local := &AIConfig{Provider: "claude-local"}
	assert.NotNil(t, aiForm(t.Context(), local, view, allLinked))

	assert.NotNil(t, envVarGroup(envVar, func() bool { return false }))
	assert.NotNil(t, keyGroup(&AIConfig{Provider: "claude"}, view, func() bool { return false }))
}

// TestStorageModeChanges_UnknownMode covers the default arm of the
// storage-mode dispatch switch.
func TestStorageModeChanges_UnknownMode(t *testing.T) {
	t.Parallel()

	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	_, err := storageModeChanges(t.Context(), "tool", keys, &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.Mode("bogus"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown credential storage mode")
}

// TestStorageModeChanges_EnvVar covers the env-var arm: the mode-owned key is
// set and the literal and keychain key paths it does not own are removed, all
// in one ordered change list.
func TestStorageModeChanges_EnvVar(t *testing.T) {
	t.Parallel()

	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	changes, err := storageModeChanges(t.Context(), "tool", keys, &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.ModeEnvVar,
		EnvVarName:  "MY_KEY",
	})
	require.NoError(t, err)
	assert.Equal(t, []config.Change{
		config.Set(chat.ConfigKeyClaudeEnv, "MY_KEY"),
		config.Remove(chat.ConfigKeyClaudeKey),
		config.Remove(chat.ConfigKeyClaudeKeychain),
	}, changes)
}

// TestStorageModeChanges_KeychainStoreError covers the keychain arm when the
// backend rejects the write. Serial: relies on the default stub backend (no
// credtest installed), whose Store always fails. The change list must be nil
// so nothing is committed to the config file.
func TestStorageModeChanges_KeychainStoreError(t *testing.T) {
	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	changes, err := storageModeChanges(t.Context(), "tool", keys, &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.ModeKeychain,
		APIKey:      "sk-ant",
	})
	require.Error(t, err)
	assert.Nil(t, changes)
	assert.Contains(t, err.Error(), "storing AI API key in OS keychain")
}

// TestCredentialChanges_UnknownProvider covers the early no-op return when the
// provider has no config-key triple.
func TestCredentialChanges_UnknownProvider(t *testing.T) {
	t.Parallel()

	changes, err := credentialChanges(t.Context(), "tool", &AIConfig{Provider: "unknown"})
	require.NoError(t, err)
	assert.Nil(t, changes)
}

// TestProviderConfigKeys covers the unknown-provider false return.
func TestProviderConfigKeys(t *testing.T) {
	t.Parallel()

	_, ok := providerConfigKeys("unknown")
	assert.False(t, ok)

	triple, ok := providerConfigKeys(string(gochat.ProviderGemini))
	require.True(t, ok)
	assert.Equal(t, chat.ConfigKeyGeminiEnv, triple.env)
	assert.Equal(t, chat.ConfigKeyGeminiKey, triple.literal)
	assert.Equal(t, chat.ConfigKeyGeminiKeychain, triple.keychain)
	assert.Equal(t, []string{triple.env, triple.literal, triple.keychain}, triple.all(),
		"all() is the exclusive-write key set")
}

// TestProviderEnvConfigKey covers all arms incl. the unknown default.
func TestProviderEnvConfigKey(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		string(gochat.ProviderClaude): chat.ConfigKeyClaudeEnv,
		string(gochat.ProviderOpenAI): chat.ConfigKeyOpenAIEnv,
		string(gochat.ProviderGemini): chat.ConfigKeyGeminiEnv,
		"unknown":                     "",
	}
	for provider, want := range cases {
		assert.Equal(t, want, providerEnvConfigKey(provider))
	}
}

// TestProviderKeychainConfigKey covers all arms incl. the default.
func TestProviderKeychainConfigKey(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		string(gochat.ProviderClaude): chat.ConfigKeyClaudeKeychain,
		string(gochat.ProviderOpenAI): chat.ConfigKeyOpenAIKeychain,
		string(gochat.ProviderGemini): chat.ConfigKeyGeminiKeychain,
		"unknown":                     "",
	}
	for provider, want := range cases {
		assert.Equal(t, want, providerKeychainConfigKey(provider))
	}
}

// TestProviderKeychainAccount covers all arms incl. the default.
func TestProviderKeychainAccount(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		string(gochat.ProviderClaude): "anthropic.api",
		string(gochat.ProviderOpenAI): "openai.api",
		string(gochat.ProviderGemini): "gemini.api",
		"unknown":                     "",
	}
	for provider, want := range cases {
		assert.Equal(t, want, providerKeychainAccount(provider))
	}
}

// TestStoreAIKeyInKeychain_StoreFailureWrapsHint covers the error
// wrapping path in storeAIKeyInKeychain when the backend Store fails.
// Serial: default stub backend rejects Store.
func TestStoreAIKeyInKeychain_StoreFailureWrapsHint(t *testing.T) {
	_, err := storeAIKeyInKeychain(t.Context(), "tool", &AIConfig{
		Provider: string(gochat.ProviderClaude),
		APIKey:   "sk-ant",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing AI API key in OS keychain")
}

// TestFinaliseAIConfig pins the rules that hold whatever the form did: a
// blank env var name takes the provider's well-known one; a provider with no
// credential drops the credential answers; a blank key keeps the existing
// one; literal is refused under CI.
func TestFinaliseAIConfig(t *testing.T) {
	// Not parallel: CI is cleared for the literal cases and set for the last.
	withNoCI(t)

	view := testutil.ViewFromYAML(t, "anthropic:\n  api:\n    key: sk-existing\n")

	got, err := finaliseAIConfig(&AIConfig{Provider: "claude", StorageMode: credentials.ModeEnvVar}, view, allLinked)
	require.NoError(t, err)
	assert.Equal(t, chat.EnvClaudeKey, got.EnvVarName, "blank takes the well-known name")

	got, err = finaliseAIConfig(&AIConfig{Provider: "claude-local", StorageMode: credentials.ModeLiteral, APIKey: "x"}, view, allLinked)
	require.NoError(t, err)
	assert.Empty(t, got.APIKey, "a local CLI carries no credential")
	assert.Empty(t, got.StorageMode)

	got, err = finaliseAIConfig(&AIConfig{Provider: "claude", StorageMode: credentials.ModeLiteral}, view, allLinked)
	require.NoError(t, err)
	assert.Equal(t, "sk-existing", got.APIKey, "blank keeps the existing key")

	_, err = finaliseAIConfig(&AIConfig{Provider: "bedrock"}, view, func(gochat.Provider) bool { return false })
	require.ErrorIs(t, err, ErrProviderNotLinked)

	t.Setenv("CI", "true")

	_, err = finaliseAIConfig(&AIConfig{Provider: "claude", StorageMode: credentials.ModeLiteral, APIKey: "x"}, view, allLinked)
	require.Error(t, err, "literal is refused under CI")
	assert.Contains(t, err.Error(), "refused under CI")
}

// TestNewCmdInitAI_Success drives the RunE happy path end-to-end with
// an injected form, asserting the config file is written and no error
// surfaces.
func TestNewCmdInitAI_Success(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	props.IO = envVarIO(t, "claude", "ANTHROPIC_TOKEN")
	cmd := NewCmdInitAI(props)
	cmd.SetContext(t.Context())
	require.NoError(t, cmd.Flags().Set("dir", dir))

	require.NoError(t, cmd.RunE(cmd, nil))

	content, err := afero.ReadFile(props.FS, filepath.Join(dir, setup.DefaultConfigFilename))
	require.NoError(t, err)
	assert.Contains(t, string(content), "env: ANTHROPIC_TOKEN")
}
