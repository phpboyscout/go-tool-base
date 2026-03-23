package generator

import (
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
)

func TestResolveProvider(t *testing.T) {
	tests := []struct {
		name           string
		configProvider string
		propProvider   string
		expected       chat.Provider
	}{
		{
			name:     "Default (Claude)",
			expected: chat.ProviderClaude,
		},
		{
			name:         "Provider from Props (Config)",
			propProvider: "gemini",
			expected:     chat.ProviderGemini,
		},
		{
			name:           "Provider from Generator Config",
			configProvider: "claude",
			expected:       chat.ProviderClaude,
		},
		{
			name:           "Generator Config overrides Props",
			configProvider: "claude",
			propProvider:   "gemini",
			expected:       chat.ProviderClaude,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AI_PROVIDER", "") // Ensure no env var interference
			c := config.NewFilesContainer(logger.NewNoop(), afero.NewMemMapFs())
			if tt.propProvider != "" {
				c.GetViper().Set("ai.provider", tt.propProvider)
			}

			g := &Generator{
				props: &props.Props{
					Config: c,
				},
				config: &Config{
					AIProvider: tt.configProvider,
				},
			}

			assert.Equal(t, tt.expected, g.resolveProvider())
		})
	}
}

func TestResolveToken(t *testing.T) {
	c := config.NewFilesContainer(logger.NewNoop(), afero.NewMemMapFs())
	c.GetViper().Set("openai.api.key", "sk-openai")
	c.GetViper().Set("anthropic.api.key", "sk-anthropic")
	c.GetViper().Set("gemini.api.key", "sk-gemini")

	g := &Generator{
		props: &props.Props{
			Config: c,
		},
	}

	tests := []struct {
		name     string
		provider chat.Provider
		expected string
	}{
		{name: "OpenAI", provider: chat.ProviderOpenAI, expected: "sk-openai"},
		{name: "Claude", provider: chat.ProviderClaude, expected: "sk-anthropic"},
		{name: "Gemini", provider: chat.ProviderGemini, expected: "sk-gemini"},
		{name: "Unknown", provider: chat.Provider("unknown"), expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, g.resolveToken(tt.provider))
		})
	}

	t.Run("No Config", func(t *testing.T) {
		gNoConfig := &Generator{props: &props.Props{Config: nil}}
		assert.Empty(t, gNoConfig.resolveToken(chat.ProviderOpenAI))
	})
}

func TestGetImportPath(t *testing.T) {
	tests := []struct {
		name        string
		setupFS     func(fs afero.Fs)
		configPath  string
		configName  string
		expected    string
		expectError bool
	}{
		{
			name: "Standard nested path",
			setupFS: func(fs afero.Fs) {
				_ = afero.WriteFile(fs, "/work/go.mod", []byte("module test-module\n"), 0644)
				_ = fs.MkdirAll("/work/pkg/cmd/child", 0755)
			},
			configPath: "/work",
			configName: "child",
			expected:   "test-module/pkg/cmd/child",
		},
		{
			name: "Config with no go.mod",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/work/pkg/cmd/child", 0755)
			},
			configPath:  "/work",
			configName:  "child",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			if tt.setupFS != nil {
				tt.setupFS(fs)
			}

			// Mock Logger
			l := logger.NewNoop()

			g := &Generator{
				props: &props.Props{
					FS:     fs,
					Logger: l,
				},
				config: &Config{
					Path: tt.configPath,
					Name: tt.configName,
				},
			}

			path, err := g.getImportPath()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, path)
			}
		})
	}
}
