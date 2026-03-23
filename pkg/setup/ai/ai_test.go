package ai

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/gtb/pkg/logger"

	mockConfig "github.com/phpboyscout/gtb/mocks/pkg/config"
	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	p "github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"
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

func mockFormCreator(provider, apiKey string) func(*AIConfig) []*huh.Form {
	return func(cfg *AIConfig) []*huh.Form {
		cfg.Provider = provider
		cfg.APIKey = apiKey

		return nil // skip form rendering
	}
}

func TestRunAIInit_Claude(t *testing.T) {
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
