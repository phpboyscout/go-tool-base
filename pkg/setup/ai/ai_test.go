package ai

import (
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"

	mockConfig "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
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
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
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

	err := RunAIInit(props, dir, WithAIForm(mockEnvVarFormCreator("claude", "CUSTOM_ANTHROPIC_KEY")))
	require.NoError(t, err)

	configFile := filepath.Join(dir, setup.DefaultConfigFilename)
	content, err := afero.ReadFile(props.FS, configFile)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "provider: claude")
	assert.Contains(t, contentStr, "env: CUSTOM_ANTHROPIC_KEY",
		"env-var mode must record the env var NAME under {provider}.api.env")
	assert.NotContains(t, contentStr, "key:",
		"env-var mode must NOT write the literal {provider}.api.key field")
}

func TestRunAIForms_CIRefusesLiteral(t *testing.T) {
	t.Setenv("CI", "true")

	cfg := config.NewFilesContainer(afero.NewMemMapFs())

	_, err := runAIForms(cfg, WithAIForm(mockFormCreator("claude", "sk-should-be-refused")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal credential storage is refused under CI")
}

func TestRunAIInit_Claude(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	err := RunAIInit(props, dir, WithAIForm(mockFormCreator("claude", "sk-ant-test123")))
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

	err := RunAIInit(props, dir, WithAIForm(mockFormCreator("openai", "sk-openai-test456")))
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

	err := RunAIInit(props, dir, WithAIForm(mockFormCreator("gemini", "AIza-gemini-test789")))
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

	err := RunAIInit(props, dir, WithAIForm(mockFormCreator("claude", "sk-ant-test")))
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

	err := RunAIInit(props, dir, WithAIForm(mockFormCreator("openai", "sk-test")))
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
		{string(chat.ProviderClaude), chat.ConfigKeyClaudeKey},
		{string(chat.ProviderOpenAI), chat.ConfigKeyOpenAIKey},
		{string(chat.ProviderGemini), chat.ConfigKeyGeminiKey},
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
				props.Config = newMockConfig(t, map[string]any{})

				return props
			},
			expected: false,
		},
		{
			name: "claude with key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = newMockConfig(t, map[string]any{
					chat.ConfigKeyAIProvider: string(chat.ProviderClaude),
					chat.ConfigKeyClaudeKey:  "sk-ant-test",
				})

				return props
			},
			expected: true,
		},
		{
			name: "claude without key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = newMockConfig(t, map[string]any{
					chat.ConfigKeyAIProvider: string(chat.ProviderClaude),
				})

				return props
			},
			expected: false,
		},
		{
			name: "openai with key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = newMockConfig(t, map[string]any{
					chat.ConfigKeyAIProvider: string(chat.ProviderOpenAI),
					chat.ConfigKeyOpenAIKey:  "sk-test",
				})

				return props
			},
			expected: true,
		},
		{
			name: "gemini with key",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = newMockConfig(t, map[string]any{
					chat.ConfigKeyAIProvider: string(chat.ProviderGemini),
					chat.ConfigKeyGeminiKey:  "AIza-test",
				})

				return props
			},
			expected: true,
		},
		{
			name: "unknown provider",
			setup: func(t *testing.T) *p.Props {
				t.Helper()
				props := newTestProps(t)
				props.Config = newMockConfig(t, map[string]any{
					chat.ConfigKeyAIProvider: "unknown",
				})

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
		{string(chat.ProviderClaude), chat.EnvClaudeKey},
		{string(chat.ProviderOpenAI), chat.EnvOpenAIKey},
		{string(chat.ProviderGemini), chat.EnvGeminiKey},
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
		{string(chat.ProviderClaude), true},
		{string(chat.ProviderOpenAI), true},
		{string(chat.ProviderGemini), true},
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
		{string(chat.ProviderClaude), "Anthropic (Claude)"},
		{string(chat.ProviderOpenAI), "OpenAI"},
		{string(chat.ProviderGemini), "Google Gemini"},
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
		values   map[string]any
		expected bool
	}{
		{
			name:     "no provider",
			values:   map[string]any{},
			expected: false,
		},
		{
			name:     "invalid provider",
			values:   map[string]any{chat.ConfigKeyAIProvider: "bad"},
			expected: false,
		},
		{
			name:     "valid provider no key",
			values:   map[string]any{chat.ConfigKeyAIProvider: string(chat.ProviderClaude)},
			expected: false,
		},
		{
			name: "claude with key",
			values: map[string]any{
				chat.ConfigKeyAIProvider: string(chat.ProviderClaude),
				chat.ConfigKeyClaudeKey:  "sk-ant-test",
			},
			expected: true,
		},
		{
			name: "openai with key",
			values: map[string]any{
				chat.ConfigKeyAIProvider: string(chat.ProviderOpenAI),
				chat.ConfigKeyOpenAIKey:  "sk-openai-test",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := newMockConfig(t, tt.values)
			i := &AIInitialiser{}
			assert.Equal(t, tt.expected, i.IsConfigured(cfg))
		})
	}
}

func TestAIInitialiser_Configure(t *testing.T) {
	t.Parallel()

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyAIProvider).Return("").Maybe()
	cfg.EXPECT().GetString(mock.Anything).Return("").Maybe()
	cfg.EXPECT().Set(chat.ConfigKeyAIProvider, string(chat.ProviderClaude)).Once()
	cfg.EXPECT().Set(chat.ConfigKeyClaudeKey, "sk-ant-configure-test").Once()

	i := &AIInitialiser{
		formOpts: []FormOption{
			WithAIForm(func(c *AIConfig) []*huh.Form {
				c.Provider = string(chat.ProviderClaude)
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

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyAIProvider).Return("").Maybe()
	cfg.EXPECT().GetString(mock.Anything).Return("").Maybe()
	cfg.EXPECT().Set(chat.ConfigKeyAIProvider, string(chat.ProviderOpenAI)).Once()

	i := &AIInitialiser{
		formOpts: []FormOption{
			WithAIForm(func(c *AIConfig) []*huh.Form {
				c.Provider = string(chat.ProviderOpenAI)
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
	cfg := newMockConfig(t, map[string]any{
		chat.ConfigKeyAIProvider: string(chat.ProviderClaude),
		chat.ConfigKeyClaudeKey:  "sk-ant-existing-key",
	})

	aiCfg, err := runAIForms(cfg, WithAIForm(func(c *AIConfig) []*huh.Form {
		c.Provider = string(chat.ProviderClaude)
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
}

func TestRunAIForms_ProviderFormCancellation(t *testing.T) {
	t.Parallel()

	cfg := newMockConfig(t, map[string]any{})

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

	cfg := newMockConfig(t, map[string]any{})

	// Provider form succeeds (returns nil to skip), but key form fails.
	cancelOpt := func(c *formConfig) {
		c.providerFormCreator = func(ac *AIConfig) *huh.Form {
			ac.Provider = string(chat.ProviderClaude)
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

	cfg := mockConfig.NewMockContainable(t)
	cfg.EXPECT().GetString(chat.ConfigKeyAIProvider).Return("").Maybe()
	cfg.EXPECT().GetString(mock.Anything).Return("").Maybe()

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

// newMockConfig creates a config.Containable mock with the given values.
func newMockConfig(t *testing.T, values map[string]any) config.Containable {
	t.Helper()

	m := mockConfig.NewMockContainable(t)
	for k, v := range values {
		if str, ok := v.(string); ok {
			m.On("GetString", k).Return(str)
		} else {
			m.On("Get", k).Return(v)
		}
	}

	// Default fallbacks for common keys if not specified
	m.On("GetString", chat.ConfigKeyAIProvider).Return("").Maybe()
	m.On("GetString", chat.ConfigKeyClaudeKey).Return("").Maybe()
	m.On("GetString", chat.ConfigKeyOpenAIKey).Return("").Maybe()
	m.On("GetString", chat.ConfigKeyGeminiKey).Return("").Maybe()
	m.On("IsSet", mock.Anything).Return(false).Maybe()
	m.On("Get", mock.Anything).Return(nil).Maybe()
	m.On("GetString", mock.Anything).Return("").Maybe()

	return m
}
