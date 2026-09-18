package ai

import (
	"path/filepath"
	"strings"
	"testing"

	propstest "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func newTestProps(t *testing.T) *p.Props {
	t.Helper()

	return propstest.New(propstest.WithTool(p.Tool{Name: "test-tool"}))
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

func TestRunAIInit_ClaudeEnvVarMode(t *testing.T) {
	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	props.IO = envVarIO(t, "claude", "CUSTOM_ANTHROPIC_KEY")
	err := RunAIInit(t.Context(), props, dir)
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

	refused := &AIConfig{Provider: "claude", StorageMode: credentials.ModeLiteral, APIKey: "would-be-refused"}
	_, err := finaliseAIConfig(refused, cfg, allLinked)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal credential storage is refused under CI")
}

func TestRunAIInit_Claude(t *testing.T) {
	withNoCI(t)

	props := newTestProps(t)
	props.Assets = p.NewAssets()
	dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)

	props.IO = keyIO(t, "claude", credentials.ModeLiteral, "sk-ant-test123")
	err := RunAIInit(t.Context(), props, dir)
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

	props.IO = keyIO(t, "openai", credentials.ModeLiteral, "sk-openai-test456")
	err := RunAIInit(t.Context(), props, dir)
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

	props.IO = keyIO(t, "gemini", credentials.ModeLiteral, "AIza-gemini-test789")
	err := RunAIInit(t.Context(), props, dir)
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

	props.IO = keyIO(t, "claude", credentials.ModeLiteral, "sk-ant-test")
	err := RunAIInit(t.Context(), props, dir)
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
	props.IO = envVarIO(t, "claude", "ANTHROPIC_API_KEY")
	err := RunAIInit(t.Context(), props, dir)
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
	t.Parallel()

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

		err := writeAIConfig(t.Context(), cfg, "test-tool", &AIConfig{
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

		err := writeAIConfig(t.Context(), cfg, "test-tool", &AIConfig{
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

	props.IO = keyIO(t, "openai", credentials.ModeLiteral, "sk-test")
	err := RunAIInit(t.Context(), props, dir)
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
		{string(gochat.ProviderClaude), "Claude (Anthropic)"},
		{string(gochat.ProviderOpenAI), "OpenAI"},
		{string(gochat.ProviderGemini), "Gemini (Google)"},
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
	// Not parallel: the TUI route is paced by time (see keyIO), and literal
	// mode is only offered outside CI.
	withNoCI(t)

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

	props := newTestProps(t)
	props.IO = keyIO(t, "claude", credentials.ModeLiteral, "sk-ant-configure-test")

	err := (&AIInitialiser{}).Configure(t.Context(), props, cfg)
	assert.NoError(t, err)
}

// TestAIInitialiser_Configure_NoKey (F16 of the v0.43.0 manual round):
// literal mode with no key entered and none to keep is refused and nothing
// is written. It used to write the provider alone and report success, which
// over a pipe (where the password prompt cannot read) left a tool with a
// provider and no credential and a log line saying it was saved.
func TestAIInitialiser_Configure_NoKey(t *testing.T) {
	// Not parallel: the TUI route is paced by time (see keyIO), and literal
	// mode is only offered outside CI.
	withNoCI(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))

	props := newTestProps(t)
	props.IO = keyIO(t, "openai", credentials.ModeLiteral, "")

	err := (&AIInitialiser{}).Configure(t.Context(), props, cfg)
	require.ErrorIs(t, err, ErrNoKeyEntered)
}

// TestInitTemplate_SeedsNoEmptyCredentials pins the #1 fix: the AI init
// template must not seed empty credential placeholders. An empty `key: ""` (or
// `provider: ""`) reads as "configured but blank" and made validateConfig warn
// on every command; the wizard writes real values instead.
func TestInitTemplate_SeedsNoEmptyCredentials(t *testing.T) {
	t.Parallel()

	data, err := assets.ReadFile("assets/init/config.yaml")
	require.NoError(t, err)

	text := string(data)
	assert.NotContains(t, text, `key: ""`, "no empty credential key placeholder")
	assert.NotContains(t, text, `provider: ""`, "no empty provider placeholder")

	// Every non-blank, non-comment line would be a seeded key; there should be
	// none — the template is documentation only.
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		t.Errorf("AI init template must seed no keys, found: %q", line)
	}
}

func TestRunAIForms_ExistingKeyFallback(t *testing.T) {
	// Not parallel: the TUI route is paced by time (see keyIO), and literal
	// mode is only offered outside CI.
	withNoCI(t)

	// When the form leaves APIKey blank, runAIForms should fall back to ExistingKey.
	cfg := testutil.ViewFromYAML(t,
		"ai:\n  provider: claude\nanthropic:\n  api:\n    key: sk-ant-existing-key\n")

	props := newTestProps(t)
	props.IO = keyIO(t, "claude", credentials.ModeLiteral, "")

	aiCfg, err := runAIForms(t.Context(), props, cfg, allLinked)
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

	// Nobody is at the terminal, so the wizard refuses and RunAIInit
	// returns an error without prompting.
	props.IO = nonInteractiveIO()
	cmd := NewCmdInitAI(props)
	cmd.SetContext(t.Context())

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure AI")
}

func TestRunAIForms_RefusesWithNobodyAtTheTerminal(t *testing.T) {
	props := newTestProps(t)
	props.IO = nonInteractiveIO()

	aiCfg, err := runAIForms(t.Context(), props, testutil.ViewFromYAML(t, ""), allLinked)
	require.Error(t, err)
	assert.Nil(t, aiCfg)
	requireRefusedAsNonInteractive(t, err)
	assert.Contains(t, err.Error(), "AI configuration form cancelled")
}

func TestAIInitialiser_Configure_FormCancellation(t *testing.T) {
	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))

	props := newTestProps(t)
	props.IO = nonInteractiveIO()

	err := (&AIInitialiser{}).Configure(t.Context(), props, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AI configuration form cancelled")
}
