package ai

import (
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func newTestProps(t *testing.T) *p.Props {
	t.Helper()

	fs := afero.NewMemMapFs()

	return &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger:       logger.NewNoop(),
		FS:           fs,
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
	}
}

// withNoCI clears the CI env var for the duration of a test so
// literal-mode wizard flows run deterministically under GitHub
// Actions (which sets CI=true automatically). Use from the test
// body — cannot be inlined into newTestProps because t.Setenv is
// forbidden after t.Parallel, and many callers parallel-ise.
func withNoCI(t *testing.T) {
	t.Helper()
	t.Setenv("CI", "")
}

func mockFormCreator(provider, apiKey string) func(*AIConfig) []*huh.Form {
	return func(cfg *AIConfig) []*huh.Form {
		cfg.Provider = provider
		cfg.APIKey = apiKey
		// Older tests rely on literal-mode semantics; make the
		// choice explicit so adding a new default mode in the future
		// cannot silently reroute these writes.
		cfg.StorageMode = credentials.ModeLiteral

		return nil // skip form rendering
	}
}

// mockEnvVarFormCreator simulates the user selecting env-var storage
// mode and entering an env var name — the Phase 1 recommended path.
func mockEnvVarFormCreator(provider, envVarName string) func(*AIConfig) []*huh.Form {
	return func(cfg *AIConfig) []*huh.Form {
		cfg.Provider = provider
		cfg.StorageMode = credentials.ModeEnvVar
		cfg.EnvVarName = envVarName

		return nil
	}
}

func TestRunAIInit_ClaudeEnvVarMode(t *testing.T) {
	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockEnvVarFormCreator("claude", "CUSTOM_ANTHROPIC_KEY")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	view := testutil.ViewFromYAML(t, string(content))
	assert.Equal(t, "claude", view.GetString(chat.ConfigKeyAIProvider))
	assert.Equal(t, "CUSTOM_ANTHROPIC_KEY", view.GetString(chat.ConfigKeyClaudeEnv),
		"env-var mode must record the env var NAME under {provider}.api.env")
	assert.Empty(t, view.GetString(chat.ConfigKeyClaudeKey),
		"env-var mode must leave the literal {provider}.api.key blank")
}

func TestRunAIForms_CIRefusesLiteral(t *testing.T) {
	t.Setenv("CI", "true")

	cfg := testutil.ViewFromYAML(t, "")

	_, err := runAIForms(cfg, WithAIForm(mockFormCreator("claude", "sk-should-be-refused")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal credential storage is refused under CI")
}

func TestRunAIInit_Claude(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockFormCreator("claude", "sk-ant-test123")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	exists, _ := afero.Exists(props.FS, configFile)
	assert.True(t, exists, "config file should exist")

	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "provider: claude")
	assert.Contains(t, contentStr, "anthropic:")
	assert.Contains(t, contentStr, "sk-ant-test123")
}

func TestRunAIInit_OpenAI(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockFormCreator("openai", "sk-openai-test456")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "provider: openai")
	assert.Contains(t, contentStr, "openai:")
	assert.Contains(t, contentStr, "sk-openai-test456")
}

func TestRunAIInit_Gemini(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockFormCreator("gemini", "AIza-gemini-test789")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "provider: gemini")
	assert.Contains(t, contentStr, "gemini:")
	assert.Contains(t, contentStr, "AIza-gemini-test789")
}

func TestRunAIInit_OnlyWritesSelectedProviderKey(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockFormCreator("claude", "sk-ant-test")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "provider: claude")
	assert.Contains(t, contentStr, "anthropic:")
	assert.Contains(t, contentStr, "sk-ant-test")
	// Should NOT contain openai or gemini keys
	assert.NotContains(t, contentStr, "openai")
	assert.NotContains(t, contentStr, "gemini")
}

// TestRunAIInit_SwitchingToEnvVarPurgesStaleLiteral is the regression
// guard for the keryx-reported security bug: re-running the wizard in
// env-var mode over a config that already held a literal API key must
// purge the literal secret from the persisted file — never leave both
// a `.env` reference and a plaintext `.key` behind. Under the
// exclusive-write flow the stale key is removed from the file
// entirely, not blanked.
func TestRunAIInit_SwitchingToEnvVarPurgesStaleLiteral(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)
	configFile := filepath.Join(dir, setup.DefaultConfigFilename)

	// A config left behind by a prior literal-mode run.
	require.NoError(t, props.FS.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(props.FS, configFile,
		[]byte("anthropic:\n  api:\n    key: sk-ant-STALE-SECRET\n"), 0o600))

	// Re-run the wizard, this time choosing env-var mode.
	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockEnvVarFormCreator("claude", "ANTHROPIC_API_KEY")))
	require.NoError(t, err)

	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "env: ANTHROPIC_API_KEY",
		"env-var mode must record the env var name")
	assert.NotContains(t, contentStr, "sk-ant-STALE-SECRET",
		"switching to env-var mode must purge the stale literal secret from config")

	view := testutil.ViewFromYAML(t, string(content))
	assert.Empty(t, view.GetString(chat.ConfigKeyClaudeKey),
		"the stale literal key must resolve as empty after the switch")
}

// TestWriteAIConfig_ModeSwitchClearsStaleKeys exercises the live-config write
// path (the interactive wizard) directly through the atomic writer: writing a
// credential in one mode must remove the key paths owned by the other modes,
// enforcing the spec's single-credential-key invariant, and must persist the
// provider in the same write.
func TestWriteAIConfig_ModeSwitchClearsStaleKeys(t *testing.T) {
	openEditor := func(t *testing.T) setup.Editor {
		t.Helper()

		props := newTestProps(t)
		props.Assets = p.NewAssets()

		editor, _, err := setup.OpenConfigEditor(t.Context(), props, "/cfg", false)
		require.NoError(t, err)

		return editor
	}

	t.Run("env-var mode clears a stale literal", func(t *testing.T) {
		cfg := openEditor(t)
		require.NoError(t, cfg.Set("anthropic.api.key", "sk-ant-STALE"))

		err := writeAIConfig(cfg, "test-tool", &AIConfig{
			Provider:    "claude",
			StorageMode: credentials.ModeEnvVar,
			EnvVarName:  "ANTHROPIC_API_KEY",
		})
		require.NoError(t, err)

		view := cfg.View()
		assert.Equal(t, "claude", view.GetString("ai.provider"))
		assert.Equal(t, "ANTHROPIC_API_KEY", view.GetString("anthropic.api.env"))
		assert.False(t, view.IsSet("anthropic.api.key"),
			"env-var mode must remove the stale literal key")
	})

	t.Run("literal mode clears a stale env reference", func(t *testing.T) {
		cfg := openEditor(t)
		require.NoError(t, cfg.Set("anthropic.api.env", "OLD_ENV_NAME"))

		// Held in a variable rather than an inline APIKey literal so
		// gosec G101 doesn't flag a "hardcoded credential" test fixture.
		replacement := "sk-ant-new"

		err := writeAIConfig(cfg, "test-tool", &AIConfig{
			Provider:    "claude",
			StorageMode: credentials.ModeLiteral,
			APIKey:      replacement,
		})
		require.NoError(t, err)

		view := cfg.View()
		assert.Equal(t, "claude", view.GetString("ai.provider"))
		assert.Equal(t, replacement, view.GetString("anthropic.api.key"))
		assert.False(t, view.IsSet("anthropic.api.env"),
			"literal mode must remove the stale env reference")
	})
}

func TestRunAIInit_MergesExistingConfig(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	// Create existing config
	existingConfig := `log:
  level: debug
github:
  auth:
    value: existing-token
`
	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	require.NoError(t, afero.WriteFile(props.FS, configFile, []byte(existingConfig), 0o644))

	err := RunAIInit(t.Context(), props, dir, WithAIForm(mockFormCreator("openai", "sk-test")))
	require.NoError(t, err)

	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	// AI config should be present
	assert.Contains(t, contentStr, "provider: openai")
	assert.Contains(t, contentStr, "sk-test")
	// Existing config should be preserved
	assert.Contains(t, contentStr, "level: debug")
	assert.Contains(t, contentStr, "existing-token")
}

func TestProviderConfigKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		expected string
	}{
		{string(gochat.ProviderClaude), chat.ConfigKeyClaudeKey},
		{string(gochat.ProviderOpenAI), chat.ConfigKeyOpenAIKey},
		{string(gochat.ProviderGemini), chat.ConfigKeyGeminiKey},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, providerConfigKey(tt.provider))
		})
	}
}

func TestIsAIConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(t *testing.T) *p.Props
		expected bool
	}{
		{
			name: "nil config",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = nil

				return props
			},
			expected: false,
		},
		{
			name: "no provider set",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = testutil.StoreFromYAML(t, "")

				return props
			},
			expected: false,
		},
		{
			name: "claude with key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = testutil.StoreFromYAML(t,
					"ai:\n  provider: claude\nanthropic:\n  api:\n    key: sk-ant-test\n")

				return props
			},
			expected: true,
		},
		{
			name: "claude without key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = testutil.StoreFromYAML(t, "ai:\n  provider: claude\n")

				return props
			},
			expected: false,
		},
		{
			name: "openai with key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = testutil.StoreFromYAML(t,
					"ai:\n  provider: openai\nopenai:\n  api:\n    key: sk-test\n")

				return props
			},
			expected: true,
		},
		{
			name: "gemini with key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = testutil.StoreFromYAML(t,
					"ai:\n  provider: gemini\ngemini:\n  api:\n    key: AIza-test\n")

				return props
			},
			expected: true,
		},
		{
			name: "unknown provider",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = testutil.StoreFromYAML(t, "ai:\n  provider: unknown\n")

				return props
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			props := tt.setup(t)
			assert.Equal(t, tt.expected, IsAIConfigured(props))
		})
	}
}

func TestMaskKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in  string
		out string
	}{
		{"", "****"},
		{"abc", "****"},
		{"abcd", "****"},
		{"abcde", "****bcde"},
		{"sk-ant-api-key", "****-key"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.out, maskKey(tt.in))
		})
	}
}

func TestProviderEnvVar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		envVar   string
	}{
		{string(gochat.ProviderClaude), chat.EnvClaudeKey},
		{string(gochat.ProviderOpenAI), chat.EnvOpenAIKey},
		{string(gochat.ProviderGemini), chat.EnvGeminiKey},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.envVar, providerEnvVar(tt.provider))
		})
	}
}

func TestIsValidProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		valid    bool
	}{
		{string(gochat.ProviderClaude), true},
		{string(gochat.ProviderOpenAI), true},
		{string(gochat.ProviderGemini), true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.valid, isValidProvider(tt.provider))
		})
	}
}

func TestProviderLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		label    string
	}{
		{string(gochat.ProviderClaude), "Anthropic (Claude)"},
		{string(gochat.ProviderOpenAI), "OpenAI"},
		{string(gochat.ProviderGemini), "Google Gemini"},
		{"custom-provider", "custom-provider"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.label, providerLabel(tt.provider))
		})
	}
}

func TestAIInitialiser_IsConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string
		expected bool
	}{
		{
			name:     "no provider",
			yaml:     "",
			expected: false,
		},
		{
			name:     "invalid provider",
			yaml:     "ai:\n  provider: bad\n",
			expected: false,
		},
		{
			name:     "valid provider no key",
			yaml:     "ai:\n  provider: claude\n",
			expected: false,
		},
		{
			name:     "claude with key",
			yaml:     "ai:\n  provider: claude\nanthropic:\n  api:\n    key: sk-ant-test\n",
			expected: true,
		},
		{
			name:     "openai with key",
			yaml:     "ai:\n  provider: openai\nopenai:\n  api:\n    key: sk-openai-test\n",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testutil.ViewFromYAML(t, tt.yaml)
			i := &AIInitialiser{}
			assert.Equal(t, tt.expected, i.IsConfigured(cfg))
		})
	}
}

func TestAIInitialiser_Configure(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))
	// Provider and credential commit in ONE transactional Apply: the provider
	// is set, the literal key is set, and the sibling env/keychain key paths a
	// prior storage mode may have used are removed. Committing together means a
	// credential-write failure can never orphan the provider.
	cfg.EXPECT().Apply([]config.Change{
		config.Set(chat.ConfigKeyAIProvider, string(gochat.ProviderClaude)),
		config.Set(chat.ConfigKeyClaudeKey, "sk-ant-configure-test"),
		config.Remove(chat.ConfigKeyClaudeEnv),
		config.Remove(chat.ConfigKeyClaudeKeychain),
	}).Return(nil).Once()

	i := &AIInitialiser{
		formOpts: []FormOption{
			WithAIForm(func(c *AIConfig) []*huh.Form {
				c.Provider = string(gochat.ProviderClaude)
				c.APIKey = "sk-ant-configure-test"

				return nil
			}),
		},
	}

	err := i.Configure(newTestProps(t), cfg)
	assert.NoError(t, err)
}

func TestAIInitialiser_Configure_NoKey(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))
	// No credential supplied, so only the provider is written — but still
	// through the single combined Apply.
	cfg.EXPECT().Apply([]config.Change{
		config.Set(chat.ConfigKeyAIProvider, string(gochat.ProviderOpenAI)),
	}).Return(nil).Once()

	i := &AIInitialiser{
		formOpts: []FormOption{
			WithAIForm(func(c *AIConfig) []*huh.Form {
				c.Provider = string(gochat.ProviderOpenAI)
				// APIKey intentionally blank
				return nil
			}),
		},
	}

	err := i.Configure(newTestProps(t), cfg)
	assert.NoError(t, err)
}

func TestRunAIForms_ExistingKeyFallback(t *testing.T) {
	t.Parallel()

	// When the form leaves APIKey blank, runAIForms should fall back to ExistingKey.
	cfg := testutil.ViewFromYAML(t,
		"ai:\n  provider: claude\nanthropic:\n  api:\n    key: sk-ant-existing-key\n")

	aiCfg, err := runAIForms(cfg, WithAIForm(func(c *AIConfig) []*huh.Form {
		c.Provider = string(gochat.ProviderClaude)
		// APIKey intentionally not set — should fall back to ExistingKey
		return nil
	}))

	require.NoError(t, err)
	assert.Equal(t, "sk-ant-existing-key", aiCfg.APIKey)
}

func TestNewCmdInitAI_Wiring(t *testing.T) {
	t.Parallel()

	props := newTestProps(t)
	cmd := NewCmdInitAI(props)

	assert.Equal(t, "ai", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.Flags().Lookup("dir"))
	// The init subcommand must use RunE (not Run) so failures route
	// through the ErrorHandler/telemetry path instead of a direct
	// logger.Fatalf that bypasses hints, ExitFunc, and the flush.
	assert.NotNil(t, cmd.RunE, "init ai must expose RunE")
	assert.Nil(t, cmd.Run, "init ai must not use the fatal-on-error Run")
}

// TestNewCmdInitAI_RunEReturnsError proves a configuration failure is
// surfaced as a returned error (consumed by cobra's standard error
// path / ErrorHandler) rather than terminating the process via
// logger.Fatalf. A non-TTY form stage fails deterministically here.
func TestNewCmdInitAI_RunEReturnsError(t *testing.T) {
	// Not parallel: forces CI so the storage-mode stage refuses a TTY
	// and the credential path cannot complete interactively.
	t.Setenv("CI", "true")

	props := newTestProps(t)
	props.Assets = p.NewAssets()

	// Inject a provider form that fails to run (no TTY) so RunAIInit
	// returns an error without prompting.
	cmd := NewCmdInitAI(props, WithAIForm(func(_ *AIConfig) []*huh.Form {
		return []*huh.Form{
			huh.NewForm(huh.NewGroup(huh.NewInput().Title("dummy"))),
		}
	}))
	cmd.SetContext(t.Context())

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure AI")
}

func TestRunAIForms_ProviderFormCancellation(t *testing.T) {
	t.Parallel()

	cfg := testutil.ViewFromYAML(t, "")

	// Inject a providerFormCreator that returns a real huh.Form.
	// In a test environment (no TTY) huh.Form.Run() will return an error,
	// simulating user cancellation.
	cancelOpt := func(c *formConfig) {
		c.providerFormCreator = func(_ *AIConfig) *huh.Form {
			return huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("dummy"),
				),
			)
		}
		// keyFormCreator returns nil — we never reach stage 2.
		c.keyFormCreator = func(_ *AIConfig) *huh.Form {
			return nil
		}
	}

	aiCfg, err := runAIForms(cfg, cancelOpt)
	require.Error(t, err)
	assert.Nil(t, aiCfg)
	assert.Contains(t, err.Error(), "AI configuration form cancelled")
}

func TestRunAIForms_KeyFormCancellation(t *testing.T) {
	t.Parallel()

	cfg := testutil.ViewFromYAML(t, "")

	// Provider form succeeds (returns nil to skip), but key form fails.
	cancelOpt := func(c *formConfig) {
		c.providerFormCreator = func(ac *AIConfig) *huh.Form {
			ac.Provider = string(gochat.ProviderClaude)
			return nil // skip provider selection
		}
		c.keyFormCreator = func(_ *AIConfig) *huh.Form {
			return huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("dummy-key"),
				),
			)
		}
	}

	aiCfg, err := runAIForms(cfg, cancelOpt)
	require.Error(t, err)
	assert.Nil(t, aiCfg)
	assert.Contains(t, err.Error(), "AI configuration form cancelled")
}

func TestAIInitialiser_Configure_FormCancellation(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))

	i := &AIInitialiser{
		formOpts: []FormOption{
			func(c *formConfig) {
				c.providerFormCreator = func(_ *AIConfig) *huh.Form {
					return huh.NewForm(
						huh.NewGroup(
							huh.NewInput().Title("cancelled"),
						),
					)
				}
				c.keyFormCreator = func(_ *AIConfig) *huh.Form {
					return nil
				}
			},
		},
	}

	err := i.Configure(newTestProps(t), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AI configuration form cancelled")
}
