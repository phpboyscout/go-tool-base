package ai

import (
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	mockConfig "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
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

	init := NewAIInitialiser(props, WithAIForm(func(_ *AIConfig) []*huh.Form { return nil }))
	require.NotNil(t, init)
	assert.Equal(t, "AI integration", init.Name())

	// A nil Assets must not panic.
	props.Assets = nil
	assert.NotNil(t, NewAIInitialiser(props))
}

// TestDefaultProviderForm exercises the provider-form constructor in
// both the normal path and the AI_PROVIDER override-warning path.
func TestDefaultProviderForm(t *testing.T) {
	t.Run("without env override", func(t *testing.T) {
		t.Setenv(chat.EnvAIProvider, "")

		cfg := &AIConfig{}
		assert.NotNil(t, defaultProviderForm(cfg))
	})

	t.Run("with env override warning", func(t *testing.T) {
		t.Setenv(chat.EnvAIProvider, "openai")

		cfg := &AIConfig{}
		assert.NotNil(t, defaultProviderForm(cfg))
	})
}

// TestDefaultEnvVarForm exercises the env-var-name form constructor,
// including the default-name population branch.
func TestDefaultEnvVarForm(t *testing.T) {
	t.Parallel()

	t.Run("populates default name", func(t *testing.T) {
		t.Parallel()

		cfg := &AIConfig{Provider: string(gochat.ProviderClaude)}
		assert.NotNil(t, defaultEnvVarForm(cfg))
		assert.Equal(t, chat.EnvClaudeKey, cfg.EnvVarName,
			"blank EnvVarName must be seeded with the provider default")
	})

	t.Run("keeps explicit name", func(t *testing.T) {
		t.Parallel()

		cfg := &AIConfig{Provider: string(gochat.ProviderOpenAI), EnvVarName: "CUSTOM"}
		assert.NotNil(t, defaultEnvVarForm(cfg))
		assert.Equal(t, "CUSTOM", cfg.EnvVarName)
	})
}

// TestDefaultKeyForm exercises the key-input form constructor in both
// the plain and the token-env-override-warning paths.
func TestDefaultKeyForm(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		t.Setenv(chat.EnvClaudeKey, "")

		cfg := &AIConfig{Provider: string(gochat.ProviderClaude)}
		assert.NotNil(t, defaultKeyForm(cfg))
	})

	t.Run("with token env override and existing key", func(t *testing.T) {
		t.Setenv(chat.EnvClaudeKey, "sk-ant-from-env")

		cfg := &AIConfig{Provider: string(gochat.ProviderClaude), ExistingKey: "sk-ant-existing"}
		assert.NotNil(t, defaultKeyForm(cfg))
	})

	t.Run("unknown provider has no env var", func(t *testing.T) {
		t.Parallel()

		cfg := &AIConfig{Provider: "unknown"}
		assert.NotNil(t, defaultKeyForm(cfg))
	})
}

// TestDefaultStorageModeForm exercises the storage-mode form
// constructor and its default-mode seeding.
func TestDefaultStorageModeForm(t *testing.T) {
	t.Parallel()

	cfg := &AIConfig{Provider: string(gochat.ProviderClaude)}
	assert.NotNil(t, defaultStorageModeForm(cfg))
	assert.Equal(t, credentials.ModeEnvVar, cfg.StorageMode,
		"blank StorageMode must default to env-var mode")
}

// TestStorageModeDescription covers both the CI and non-CI branches.
func TestStorageModeDescription(t *testing.T) {
	t.Parallel()

	assert.Contains(t, storageModeDescription(true), "CI environment detected")
	assert.Contains(t, storageModeDescription(false), "out of the config file")
}

// TestApplyStorageModeWrite_UnknownMode covers the default arm of the
// storage-mode dispatch switch.
func TestApplyStorageModeWrite_UnknownMode(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	err := applyStorageModeWrite(cfg, "tool", keys, &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.Mode("bogus"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown credential storage mode")
}

// TestApplyStorageModeWrite_EnvVar covers the env-var arm writing the
// env key onto the container.
func TestApplyStorageModeWrite_EnvVar(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().Set(chat.ConfigKeyClaudeEnv, "MY_KEY").Once()
	// The mode-owned write is followed by a stale-clear pass that blanks
	// the literal and keychain key paths the env-var mode does not own.
	cfg.EXPECT().Set(chat.ConfigKeyClaudeKey, "").Maybe()
	cfg.EXPECT().Set(chat.ConfigKeyClaudeKeychain, "").Maybe()

	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	require.NoError(t, applyStorageModeWrite(cfg, "tool", keys, &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.ModeEnvVar,
		EnvVarName:  "MY_KEY",
	}))
}

// TestApplyStorageModeWrite_KeychainStoreError covers the keychain arm
// when the backend rejects the write. Serial: relies on the default
// stub backend (no credtest installed), whose Store always fails.
func TestApplyStorageModeWrite_KeychainStoreError(t *testing.T) {
	cfg := mockConfig.NewMockContainable(t)
	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	err := applyStorageModeWrite(cfg, "tool", keys, &AIConfig{
		Provider:    string(gochat.ProviderClaude),
		StorageMode: credentials.ModeKeychain,
		APIKey:      "sk-ant",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing AI API key in OS keychain")
}

// TestWriteAICredentialKeys_UnknownProvider covers the early no-op
// return when the provider has no config-key triple.
func TestWriteAICredentialKeys_UnknownProvider(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	require.NoError(t, writeAICredentialKeys(cfg, "tool", &AIConfig{Provider: "unknown"}))
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

// TestSetAICredentialOnViper covers each storage-mode arm of the
// viper write-back, plus the unknown-mode default and the
// keychain-error branch.
func TestSetAICredentialOnViper(t *testing.T) {
	t.Run("env-var mode records env name", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		require.NoError(t, setAICredentialOnViper(v, "tool", &AIConfig{
			Provider:    string(gochat.ProviderOpenAI),
			StorageMode: credentials.ModeEnvVar,
			EnvVarName:  "OAI",
		}))
		assert.Equal(t, "OAI", v.GetString(chat.ConfigKeyOpenAIEnv))
	})

	t.Run("literal mode records key", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		require.NoError(t, setAICredentialOnViper(v, "tool", &AIConfig{
			Provider:    string(gochat.ProviderOpenAI),
			StorageMode: credentials.ModeLiteral,
			APIKey:      "sk-lit",
		}))
		assert.Equal(t, "sk-lit", v.GetString(chat.ConfigKeyOpenAIKey))
	})

	t.Run("unknown mode errors", func(t *testing.T) {
		t.Parallel()

		v := viper.New()
		err := setAICredentialOnViper(v, "tool", &AIConfig{
			Provider:    string(gochat.ProviderOpenAI),
			StorageMode: credentials.Mode("bogus"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown credential storage mode")
	})

	t.Run("keychain mode writes ref", func(t *testing.T) {
		credtest.Install(t)

		v := viper.New()
		require.NoError(t, setAICredentialOnViper(v, "tool", &AIConfig{
			Provider:    string(gochat.ProviderOpenAI),
			StorageMode: credentials.ModeKeychain,
			APIKey:      "sk-kc",
		}))
		assert.Equal(t, "tool/openai.api", v.GetString(chat.ConfigKeyOpenAIKeychain))
	})

	t.Run("keychain mode store failure surfaces", func(t *testing.T) {
		// Serial: default stub backend rejects Store.
		v := viper.New()
		err := setAICredentialOnViper(v, "tool", &AIConfig{
			Provider:    string(gochat.ProviderOpenAI),
			StorageMode: credentials.ModeKeychain,
			APIKey:      "sk-kc",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storing AI API key in OS keychain")
	})
}

// TestWriteAIConfig_EnvVarMode covers writeAIConfig's env-var write-back
// path over afero — it must persist the env name, not a literal key.
func TestWriteAIConfig_EnvVarMode(t *testing.T) {
	t.Parallel()

	props := newTestProps(t)
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := writeAIConfig(props, dir, &AIConfig{
		Provider:    string(gochat.ProviderOpenAI),
		StorageMode: credentials.ModeEnvVar,
		EnvVarName:  "OPENAI_TOKEN",
	})
	require.NoError(t, err)

	content, err := afero.ReadFile(props.FS, filepath.Join(dir, setup.DefaultConfigFilename))
	require.NoError(t, err)
	text := string(content)
	assert.Contains(t, text, "env: OPENAI_TOKEN")
	assert.NotContains(t, text, "key:")
}

// TestWriteAIConfig_KeychainMode covers writeAIConfig's keychain
// write-back path over afero.
func TestWriteAIConfig_KeychainMode(t *testing.T) {
	credtest.Install(t)

	props := newTestProps(t)
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := writeAIConfig(props, dir, &AIConfig{
		Provider:    string(gochat.ProviderGemini),
		StorageMode: credentials.ModeKeychain,
		APIKey:      "AIza-kc",
	})
	require.NoError(t, err)

	content, err := afero.ReadFile(props.FS, filepath.Join(dir, setup.DefaultConfigFilename))
	require.NoError(t, err)
	assert.Contains(t, string(content), "keychain: test-tool/gemini.api")
	assert.NotContains(t, string(content), "AIza-kc")
}

// TestWriteAIConfig_KeychainStoreError covers the error return when
// setAICredentialOnViper fails inside writeAIConfig. Serial: default
// stub backend rejects Store.
func TestWriteAIConfig_KeychainStoreError(t *testing.T) {
	props := newTestProps(t)
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := writeAIConfig(props, dir, &AIConfig{
		Provider:    string(gochat.ProviderGemini),
		StorageMode: credentials.ModeKeychain,
		APIKey:      "AIza-fail",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing AI API key in OS keychain")
}

// TestLoadExistingAIConfig covers the missing-file no-op, the
// happy-merge path, and the parse-error path.
func TestLoadExistingAIConfig(t *testing.T) {
	t.Parallel()

	t.Run("missing file is a no-op", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		v := viper.New()
		v.SetConfigType("yaml")
		require.NoError(t, loadExistingAIConfig(v, fs, "/nope/config.yaml"))
	})

	t.Run("valid file merges", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/cfg.yaml", []byte("log:\n  level: debug\n"), 0o600))

		v := viper.New()
		v.SetConfigType("yaml")
		require.NoError(t, loadExistingAIConfig(v, fs, "/cfg.yaml"))
		assert.Equal(t, "debug", v.GetString("log.level"))
	})

	t.Run("unparseable file errors", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/bad.yaml", []byte("::: not yaml :::\n\t- broken"), 0o600))

		v := viper.New()
		v.SetConfigType("yaml")
		err := loadExistingAIConfig(v, fs, "/bad.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse existing config")
	})
}

// TestWriteKeychainRefToViper covers the blank-ref no-op (blank APIKey)
// and the error pass-through.
func TestWriteKeychainRefToViper(t *testing.T) {
	keys, ok := providerConfigKeys(string(gochat.ProviderClaude))
	require.True(t, ok)

	t.Run("blank key is a no-op", func(t *testing.T) {
		credtest.Install(t)

		v := viper.New()
		require.NoError(t, writeKeychainRefToViper(v, "tool", keys, &AIConfig{
			Provider: string(gochat.ProviderClaude),
			APIKey:   "",
		}))
		assert.Empty(t, v.GetString(chat.ConfigKeyClaudeKeychain))
	})

	t.Run("store error propagates", func(t *testing.T) {
		// Serial: default stub backend rejects Store.
		v := viper.New()
		err := writeKeychainRefToViper(v, "tool", keys, &AIConfig{
			Provider: string(gochat.ProviderClaude),
			APIKey:   "sk-ant",
		})
		require.Error(t, err)
	})
}

// TestPruneAICredentialKeys covers the prune helper: an unknown
// provider returns the input unchanged, and a known provider drops all
// three credential key paths (including pruning now-empty parents)
// while preserving unrelated config.
func TestPruneAICredentialKeys(t *testing.T) {
	t.Parallel()

	t.Run("unknown provider returns input unchanged", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		v := viper.New()
		v.Set("log.level", "debug")

		out := pruneAICredentialKeys(v, fs, "unknown")
		assert.Same(t, v, out, "unknown provider must return the same instance")
	})

	t.Run("drops all credential keys but keeps unrelated config", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		v := viper.New()
		v.Set("log.level", "debug")
		v.Set(chat.ConfigKeyClaudeKey, "sk-ant-stale")
		v.Set(chat.ConfigKeyClaudeEnv, "STALE_ENV")
		v.Set(chat.ConfigKeyClaudeKeychain, "tool/anthropic.api")

		out := pruneAICredentialKeys(v, fs, string(gochat.ProviderClaude))
		assert.Equal(t, "debug", out.GetString("log.level"))
		assert.Empty(t, out.GetString(chat.ConfigKeyClaudeKey))
		assert.Empty(t, out.GetString(chat.ConfigKeyClaudeEnv))
		assert.Empty(t, out.GetString(chat.ConfigKeyClaudeKeychain))
		// The now-empty anthropic.api parent should be gone entirely.
		assert.Nil(t, out.Get("anthropic"))
	})
}

// TestDeleteNestedKey covers each arm of the recursive delete helper,
// including the empty-path no-op, the leaf delete, the nested delete
// with empty-parent pruning, and the missing-intermediate no-op.
func TestDeleteNestedKey(t *testing.T) {
	t.Parallel()

	t.Run("empty path is a no-op", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{"a": 1}
		deleteNestedKey(m, nil)
		assert.Equal(t, map[string]any{"a": 1}, m)
	})

	t.Run("leaf delete", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{"a": 1, "b": 2}
		deleteNestedKey(m, []string{"a"})
		assert.Equal(t, map[string]any{"b": 2}, m)
	})

	t.Run("nested delete prunes empty parent", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{"a": map[string]any{"b": 1}}
		deleteNestedKey(m, []string{"a", "b"})
		assert.Empty(t, m, "the now-empty parent must be pruned")
	})

	t.Run("nested delete keeps non-empty parent", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{"a": map[string]any{"b": 1, "c": 2}}
		deleteNestedKey(m, []string{"a", "b"})
		assert.Equal(t, map[string]any{"a": map[string]any{"c": 2}}, m)
	})

	t.Run("missing intermediate is a no-op", func(t *testing.T) {
		t.Parallel()

		m := map[string]any{"x": 1}
		deleteNestedKey(m, []string{"a", "b"})
		assert.Equal(t, map[string]any{"x": 1}, m)
	})
}

// TestStoreAIKeyInKeychain_StoreFailureWrapsHint covers the error
// wrapping path in storeAIKeyInKeychain when the backend Store fails.
// Serial: default stub backend rejects Store.
func TestStoreAIKeyInKeychain_StoreFailureWrapsHint(t *testing.T) {
	_, err := storeAIKeyInKeychain("tool", &AIConfig{
		Provider: string(gochat.ProviderClaude),
		APIKey:   "sk-ant",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing AI API key in OS keychain")
}

// TestRunAIInit_EnvVarFormCancellation drives the env-var stage with a
// real (TTY-less) form so runAICredentialStage's env-var branch
// returns the cancellation error.
func TestRunAIInit_EnvVarFormCancellation(t *testing.T) {
	t.Parallel()

	cfg := newMockConfig(t, map[string]any{})

	opt := func(c *formConfig) {
		c.providerFormCreator = func(ac *AIConfig) *huh.Form {
			ac.Provider = string(gochat.ProviderClaude)
			ac.StorageMode = credentials.ModeEnvVar

			return nil
		}
		c.storageModeFormCreator = func(_ *AIConfig) *huh.Form { return nil }
		c.envVarFormCreator = func(_ *AIConfig) *huh.Form {
			return huh.NewForm(huh.NewGroup(huh.NewInput().Title("env")))
		}
	}

	aiCfg, err := runAIForms(cfg, opt)
	require.Error(t, err)
	assert.Nil(t, aiCfg)
	assert.Contains(t, err.Error(), "AI configuration form cancelled")
}

// TestRunAICredentialStage_EnvVarSuccess covers the env-var branch's
// happy (nil-form) return path.
func TestRunAICredentialStage_EnvVarSuccess(t *testing.T) {
	t.Parallel()

	fCfg := newAIFormConfig(WithAIForm(func(c *AIConfig) []*huh.Form {
		c.EnvVarName = "X"

		return nil
	}))

	aiCfg := &AIConfig{Provider: string(gochat.ProviderClaude), StorageMode: credentials.ModeEnvVar}
	got, err := runAICredentialStage(fCfg, aiCfg)
	require.NoError(t, err)
	assert.Equal(t, "X", got.EnvVarName)
}

// TestNewCmdInitAI_Success drives the RunE happy path end-to-end with
// an injected form, asserting the config file is written and no error
// surfaces.
func TestNewCmdInitAI_Success(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	cmd := NewCmdInitAI(props, WithAIForm(mockEnvVarFormCreator("claude", "ANTHROPIC_TOKEN")))
	require.NoError(t, cmd.Flags().Set("dir", dir))

	require.NoError(t, cmd.RunE(cmd, nil))

	content, err := afero.ReadFile(props.FS, filepath.Join(dir, setup.DefaultConfigFilename))
	require.NoError(t, err)
	assert.Contains(t, string(content), "env: ANTHROPIC_TOKEN")
}
