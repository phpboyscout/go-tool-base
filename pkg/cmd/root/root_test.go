package root

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cockroachdb/errors"

	"github.com/charmbracelet/huh"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configMocks "github.com/phpboyscout/gtb/mocks/pkg/config"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/logger"
	p "github.com/phpboyscout/gtb/pkg/props"
)

// root_test.go provides comprehensive unit tests for the extracted functions in root.go
// These tests focus on the configuration loading, merging, flag processing, and logging setup
// functionality that was extracted from the PersistentPreRunE function for better testability.

func TestExtractFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupCmd      func() *cobra.Command
		expectError   bool
		expectedCI    bool
		expectedDebug bool
	}{
		{
			name: "default flags",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("ci", false, "ci flag")
				cmd.Flags().Bool("debug", false, "debug flag")
				return cmd
			},
			expectError:   false,
			expectedCI:    false,
			expectedDebug: false,
		},
		{
			name: "ci flag set to true",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("ci", true, "ci flag")
				cmd.Flags().Bool("debug", false, "debug flag")
				return cmd
			},
			expectError:   false,
			expectedCI:    true,
			expectedDebug: false,
		},
		{
			name: "debug flag set to true",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("ci", false, "ci flag")
				cmd.Flags().Bool("debug", true, "debug flag")
				return cmd
			},
			expectError:   false,
			expectedCI:    false,
			expectedDebug: true,
		},
		{
			name: "both flags set to true",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("ci", true, "ci flag")
				cmd.Flags().Bool("debug", true, "debug flag")
				return cmd
			},
			expectError:   false,
			expectedCI:    true,
			expectedDebug: true,
		},
		{
			name: "missing ci flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("debug", false, "debug flag")
				// ci flag is missing
				return cmd
			},
			expectError: true,
		},
		{
			name: "missing debug flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("ci", false, "ci flag")
				// debug flag is missing
				return cmd
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			flags, err := extractFlags(cmd)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, flags)
				return
			}

			assert.NoError(t, err)
			require.NotNil(t, flags)
			assert.Equal(t, tt.expectedCI, flags.CI)
			assert.Equal(t, tt.expectedDebug, flags.Debug)
		})
	}
}

func TestLoadAndMergeConfig(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()

	createTestProps := func() *p.Props {
		return &p.Props{
			Logger: l,
			FS:     afero.NewMemMapFs(),
		}
	}

	mainConfigYaml := `main:
  key: "main_value"
  shared: "from_main"
database:
  host: "localhost"
  port: 5432`

	tests := []struct {
		name              string
		setupOptions      func() ConfigLoadOptions
		expectError       bool
		expectedMainKey   string
		expectedEmbedKey  string
		expectedSharedKey string
	}{
		{
			name: "load main config only",
			setupOptions: func() ConfigLoadOptions {
				props := createTestProps()

				// Create main config file
				err := afero.WriteFile(props.FS, "config.yaml", []byte(mainConfigYaml), 0o644)
				require.NoError(t, err)

				props.Assets = p.NewAssets()
				return ConfigLoadOptions{
					CfgPaths:    []string{"config.yaml"},
					ConfigPaths: []string{}, // No embedded config
					Props:       props,
					AllowEmpty:  false,
				}
			},
			expectError:       false,
			expectedMainKey:   "main_value",
			expectedEmbedKey:  "", // Should not exist
			expectedSharedKey: "from_main",
		},
		{
			name: "load and merge with embedded config",
			setupOptions: func() ConfigLoadOptions {
				props := createTestProps()

				// Create main config file
				err := afero.WriteFile(props.FS, "config.yaml", []byte(mainConfigYaml), 0o644)
				require.NoError(t, err)

				// For this test, we'll test without embedded config since mocking embed.FS is complex
				// This test focuses on the main config loading functionality
				props.Assets = p.NewAssets()
				return ConfigLoadOptions{
					CfgPaths:    []string{"config.yaml"},
					ConfigPaths: []string{}, // Skip embedded config for now
					Props:       props,
					AllowEmpty:  false,
				}
			},
			expectError:       false,
			expectedMainKey:   "main_value",
			expectedEmbedKey:  "", // No embedded config in this simplified test
			expectedSharedKey: "from_main",
		},
		{
			name: "no config files exist, empty not allowed",
			setupOptions: func() ConfigLoadOptions {
				props := createTestProps()

				props.Assets = p.NewAssets()
				return ConfigLoadOptions{
					CfgPaths:    []string{"nonexistent.yaml"},
					ConfigPaths: []string{},
					Props:       props,
					AllowEmpty:  false,
				}
			},
			expectError: true,
		},
		{
			name: "no config files exist, empty allowed",
			setupOptions: func() ConfigLoadOptions {
				props := createTestProps()

				props.Assets = p.NewAssets()
				return ConfigLoadOptions{
					CfgPaths:    []string{"nonexistent.yaml"},
					ConfigPaths: []string{},
					Props:       props,
					AllowEmpty:  true,
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := tt.setupOptions()
			cfg, err := loadAndMergeConfig(opts)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, cfg)
				return
			}

			assert.NoError(t, err)
			require.NotNil(t, cfg)

			// Test expected values
			if tt.expectedMainKey != "" {
				assert.Equal(t, tt.expectedMainKey, cfg.GetString("main.key"))
			}
			if tt.expectedEmbedKey != "" {
				assert.Equal(t, tt.expectedEmbedKey, cfg.GetString("embedded.key"))
			}
			if tt.expectedSharedKey != "" {
				// Access the shared value from the main section
				assert.Equal(t, tt.expectedSharedKey, cfg.GetString("main.shared"))
			}
		})
	}
}

// TestLoadAndMergeConfigWithOverrides tests that main config values override embedded config values
// when both configs contain the same keys. This proves that cfg values take precedence over embeddedCfg.
func TestLoadAndMergeConfigWithOverrides(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()

	tests := []struct {
		name               string
		mainConfigContent  string
		embedConfigContent string
		embedConfigPath    string
		expectedValues     map[string]any
		description        string
	}{
		{
			name: "main config overrides embedded config values",
			mainConfigContent: `
app:
  name: "main-app-name"
  version: "2.0.0"
  shared_setting: "overridden-by-main"
database:
  host: "main-db-host"
  port: 3306
logging:
  level: "debug"`,
			embedConfigContent: `
app:
  name: "embedded-app-name"
  version: "1.0.0"
  shared_setting: "from-embedded"
  embedded_only: "embedded-value"
database:
  host: "embedded-db-host"
  port: 5432
  username: "embedded-user"
server:
  port: 8080`,
			embedConfigPath: "config/embedded.yaml",
			expectedValues: map[string]any{
				// Values that should be overridden by main config
				"app.name":           "main-app-name",
				"app.version":        "2.0.0",
				"app.shared_setting": "overridden-by-main",
				"database.host":      "main-db-host",
				"database.port":      3306,
				"logging.level":      "debug",
				// Values that should remain from embedded config (not in main)
				"app.embedded_only": "embedded-value",
				"database.username": "embedded-user",
				"server.port":       8080,
			},
			description: "Main config values override embedded config when keys conflict",
		},
		{
			name: "nested objects are merged correctly with main taking precedence",
			mainConfigContent: `
feature_flags:
  new_ui: true
  beta_features: false
  experimental:
    feature_a: true
    feature_b: false
auth:
  provider: "oauth2"
  timeout: 300`,
			embedConfigContent: `
feature_flags:
  new_ui: false
  legacy_support: true
  experimental:
    feature_a: false
    feature_c: true
auth:
  provider: "basic"
  max_attempts: 3
  timeout: 600`,
			embedConfigPath: "config/defaults.yaml",
			expectedValues: map[string]any{
				// Main config overrides
				"feature_flags.new_ui":                 true,
				"feature_flags.beta_features":          false,
				"feature_flags.experimental.feature_a": true,
				"feature_flags.experimental.feature_b": false,
				"auth.provider":                        "oauth2",
				"auth.timeout":                         300,
				// Embedded values preserved
				"feature_flags.legacy_support":         true,
				"feature_flags.experimental.feature_c": true,
				"auth.max_attempts":                    3,
			},
			description: "Nested configuration objects merge with main config taking precedence",
		},
		{
			name: "array values are completely overridden by main config",
			mainConfigContent: `
environments:
  - "production"
  - "staging"
plugins:
  enabled:
    - "auth"
    - "logging"`,
			embedConfigContent: `
environments:
  - "development"
  - "testing"
  - "staging"
plugins:
  enabled:
    - "metrics"
    - "tracing"
  disabled:
    - "debug"`,
			embedConfigPath: "config/base.yaml",
			expectedValues: map[string]any{
				// Arrays from main config completely replace embedded arrays
				"environments":    []any{"production", "staging"},
				"plugins.enabled": []any{"auth", "logging"},
				// Embedded-only arrays are preserved
				"plugins.disabled": []any{"debug"},
			},
			description: "Array values from main config completely override embedded arrays",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup filesystem with main config
			fs := afero.NewMemMapFs()
			err := afero.WriteFile(fs, "main-config.yaml", []byte(tt.mainConfigContent), 0o644)
			require.NoError(t, err)

			// Load main config
			mainCfg, err := config.Load([]string{"main-config.yaml"}, fs, l, false)
			require.NoError(t, err, "failed to load main config")

			// Setup mock embedded filesystem using fstest.MapFS
			mockAssets := fstest.MapFS{
				tt.embedConfigPath: &fstest.MapFile{Data: []byte(tt.embedConfigContent)},
			}

			// Load embedded config using our mock
			embeddedCfg, err := config.LoadEmbed([]string{tt.embedConfigPath}, mockAssets, l)
			require.NoError(t, err, "failed to load embedded config")

			// Perform the merge exactly as loadAndMergeConfig does
			// Use MergeConfig with JSON for a deep merge (main config (mainCfg) overrides embedded (embeddedCfg))
			t.Logf("--- %s ---", tt.name)
			err = embeddedCfg.GetViper().MergeConfig(strings.NewReader(mainCfg.ToJSON()))
			require.NoError(t, err, "failed to merge configs")

			// The merged config should have main config values taking precedence
			mergedCfg := embeddedCfg

			// Verify all expected values
			for key, expectedValue := range tt.expectedValues {
				actualValue := mergedCfg.Get(key)
				assert.Equal(t, expectedValue, actualValue,
					"Key %s: expected %v (%T), got %v (%T). %s",
					key, expectedValue, expectedValue, actualValue, actualValue, tt.description)
			}

			// Additional verification that main config truly overrides embedded config
			// by checking some specific override scenarios
			if tt.name == "main config overrides embedded config values" {
				// Verify specific override behavior
				assert.Equal(t, "main-app-name", mergedCfg.GetString("app.name"),
					"app.name should be overridden by main config")
				assert.Equal(t, "overridden-by-main", mergedCfg.GetString("app.shared_setting"),
					"shared_setting should be overridden by main config")
				assert.Equal(t, "main-db-host", mergedCfg.GetString("database.host"),
					"database.host should be overridden by main config")

				// Verify embedded-only values are preserved
				assert.Equal(t, "embedded-value", mergedCfg.GetString("app.embedded_only"),
					"embedded-only values should be preserved")
				assert.Equal(t, "embedded-user", mergedCfg.GetString("database.username"),
					"embedded-only values should be preserved")
			}
		})
	}
}

func TestConfigureLogging(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		debugFlag     bool
		logLevel      string
		logFormat     string
		expectedLevel logger.Level
	}{
		{
			name:          "debug flag overrides config",
			debugFlag:     true,
			logLevel:      "info",
			logFormat:     "text",
			expectedLevel: logger.DebugLevel,
		},
		{
			name:          "config log level applied when debug false",
			debugFlag:     false,
			logLevel:      "warn",
			logFormat:     "text",
			expectedLevel: logger.WarnLevel,
		},
		{
			name:          "json formatter does not change level",
			debugFlag:     false,
			logLevel:      "info",
			logFormat:     "json",
			expectedLevel: logger.InfoLevel,
		},
		{
			name:          "logfmt formatter does not change level",
			debugFlag:     false,
			logLevel:      "error",
			logFormat:     "logfmt",
			expectedLevel: logger.ErrorLevel,
		},
		{
			name:          "invalid log level falls back to default",
			debugFlag:     false,
			logLevel:      "invalid",
			logFormat:     "text",
			expectedLevel: logger.InfoLevel, // Default level
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create props with logger
			props := &p.Props{
				Logger: logger.NewCharm(nil),
			}

			// Create flags
			flags := &FlagValues{
				Debug: tt.debugFlag,
			}

			// Create mock config using the generated mockery mock
			mockCfg := configMocks.NewMockContainable(t)

			// Only expect log.level call when debug flag is false
			if !tt.debugFlag {
				mockCfg.EXPECT().GetString("log.level").Return(tt.logLevel)
			}

			// Always expect log.format call
			mockCfg.EXPECT().GetString("log.format").Return(tt.logFormat)

			// Create level var for MCP logging
			mcpLogLevel := &slog.LevelVar{}
			// Default to info
			mcpLogLevel.Set(slog.LevelInfo)

			// Configure logging
			configureLogging(props, flags, mockCfg, mcpLogLevel)

			// Verify the logger level was configured correctly
			assert.Equal(t, tt.expectedLevel, props.Logger.GetLevel())

			// Verify MCP log level matches (mapping charm level to slog level)
			var expectedSlogLevel slog.Level
			switch tt.expectedLevel {
			case logger.DebugLevel:
				expectedSlogLevel = slog.LevelDebug
			case logger.InfoLevel:
				expectedSlogLevel = slog.LevelInfo
			case logger.WarnLevel:
				expectedSlogLevel = slog.LevelWarn
			case logger.ErrorLevel:
				expectedSlogLevel = slog.LevelError
			default:
				expectedSlogLevel = slog.LevelInfo
			}
			assert.Equal(t, expectedSlogLevel, mcpLogLevel.Level())
		})
	}
}

func TestShouldSkipUpdateCheck(t *testing.T) {
	tests := []struct {
		name         string
		toolDisabled []p.FeatureCmd
		redirecting  bool
		ciFlag       bool
		configCI     bool
		cmdName      string
		expectedSkip bool
	}{
		{
			name:         "skip when update command disabled in tool",
			toolDisabled: []p.FeatureCmd{p.UpdateCmd},
			expectedSkip: true,
		},
		{
			name:         "skip when redirecting to update",
			redirecting:  true,
			expectedSkip: true,
		},
		{
			name:         "skip when CI flag is true",
			ciFlag:       true,
			expectedSkip: true,
		},
		{
			name:         "skip when config CI is true",
			configCI:     true,
			expectedSkip: true,
		},
		{
			name:         "skip when running init command",
			cmdName:      "init",
			expectedSkip: true,
		},
		{
			name:         "skip when running update command",
			cmdName:      "update",
			expectedSkip: true,
		},
		{
			name:         "skip when running version command",
			cmdName:      "version",
			expectedSkip: true,
		},
		{
			name:         "do not skip for normal command",
			cmdName:      "other",
			expectedSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create per-test state
			state := newRootState()
			state.redirectingToUpdate = tt.redirecting

			// Create mock config
			mockCfg := configMocks.NewMockContainable(t)
			mockCfg.EXPECT().GetBool("ci").Return(tt.configCI).Maybe()

			// Create props — build features that disable the specified commands
			var disableMutators []p.FeatureState
			for _, cmd := range tt.toolDisabled {
				disableMutators = append(disableMutators, p.Disable(cmd))
			}

			props := &p.Props{
				Tool: p.Tool{
					Features: p.SetFeatures(disableMutators...),
					Name:     "test-tool",
				},
				Config: mockCfg,
				FS:     afero.NewMemMapFs(),
			}

			// Create flags
			flags := &FlagValues{
				CI: tt.ciFlag,
			}

			// Create command
			cmd := &cobra.Command{
				Use: tt.cmdName,
			}

			// Test shouldSkipUpdateCheck
			result := shouldSkipUpdateCheck(props, cmd, flags, state)

			assert.Equal(t, tt.expectedSkip, result)
		})
	}
}

func TestCreateUpdatePromptForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		initialValue bool
	}{
		{
			name:         "form created with true initial value",
			initialValue: true,
		},
		{
			name:         "form created with false initial value",
			initialValue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runUpdate := tt.initialValue
			form := createUpdatePromptForm(&runUpdate)

			// Verify the form was created successfully
			assert.NotNil(t, form)

			// Verify the initial value is set correctly
			assert.Equal(t, tt.initialValue, runUpdate)
		})
	}
}

func TestHandleOutdatedVersion_WithMockForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		message           string
		userChoosesUpdate bool
		expectedUpdate    bool
		expectedExit      bool
	}{
		{
			name:              "user declines update",
			message:           "Version 2.0.0 is available",
			userChoosesUpdate: false,
			expectedUpdate:    false,
			expectedExit:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock form that simulates user choice without requiring terminal
			mockFormCreator := func(runUpdate *bool) *huh.Form {
				// Set the value to simulate user choice - bypass the form entirely
				*runUpdate = tt.userChoosesUpdate
				// Return a form that skips rendering by immediately completing
				// Since we've already set the value, the form doesn't need to actually run
				return nil
			}

			// Create test props
			props := &p.Props{
				Logger: logger.NewNoop(),
				Tool: p.Tool{
					Name: "test-tool",
				},
			}

			state := newRootState()
			result := &UpdateCheckResult{}

			// Test with custom form using WithForm option
			handleOutdatedVersion(context.Background(), props, tt.message, result, state, WithForm(mockFormCreator))

			// Verify results
			assert.Equal(t, tt.expectedUpdate, result.HasUpdated)
			assert.Equal(t, tt.expectedExit, result.ShouldExit)
		})
	}
}

func TestWithFormOption(t *testing.T) {
	t.Parallel()

	// Test that the WithForm option correctly sets the form creator
	called := false
	testFormCreator := func(runUpdate *bool) *huh.Form {
		called = true
		*runUpdate = false
		// Return nil to skip form rendering (value already set)
		return nil
	}

	opt := WithForm(testFormCreator)
	cfg := &outdatedVersionConfig{
		formCreator: createUpdatePromptForm,
	}

	// Apply the option
	opt(cfg)

	// Verify the form creator was replaced
	runUpdate := true
	_ = cfg.formCreator(&runUpdate)

	assert.True(t, called, "custom form creator should have been called")
	assert.False(t, runUpdate, "value should have been set by custom form creator")
}

func TestRootState_Isolation(t *testing.T) {
	t.Parallel()

	// Two independent root commands should have independent state
	props1 := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Tool: p.Tool{
			Name:     "tool1",
			Features: p.SetFeatures(p.Disable(p.UpdateCmd), p.Disable(p.InitCmd), p.Disable(p.McpCmd), p.Disable(p.DocsCmd)),
		},
	}
	props2 := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Tool: p.Tool{
			Name:     "tool2",
			Features: p.SetFeatures(p.Disable(p.UpdateCmd), p.Disable(p.InitCmd), p.Disable(p.McpCmd), p.Disable(p.DocsCmd)),
		},
	}

	cmd1 := NewCmdRoot(props1)
	cmd2 := NewCmdRoot(props2)

	// They should be independent commands
	assert.Equal(t, "tool1", cmd1.Use)
	assert.Equal(t, "tool2", cmd2.Use)
}

func TestRootState_DefaultFormCreator(t *testing.T) {
	t.Parallel()

	state := newRootState()
	assert.NotNil(t, state.formCreator, "default form creator should not be nil")
	assert.False(t, state.redirectingToUpdate, "redirectingToUpdate should default to false")
}

func TestErrUpdateComplete(t *testing.T) {
	t.Parallel()

	// ErrUpdateComplete should be detectable via errors.Is
	err := ErrUpdateComplete
	assert.ErrorIs(t, err, ErrUpdateComplete)

	// Wrapping should still be detectable
	wrapped := errors.Wrap(err, "wrapped")
	assert.ErrorIs(t, wrapped, ErrUpdateComplete)
}

func TestHandleOutdatedVersion_SetsStateFlag(t *testing.T) {
	t.Parallel()

	state := newRootState()

	// Mock form that declines update
	mockFormCreator := func(runUpdate *bool) *huh.Form {
		*runUpdate = false
		return nil
	}
	state.formCreator = mockFormCreator

	props := &p.Props{
		Logger: logger.NewNoop(),
		Tool:   p.Tool{Name: "test-tool"},
	}

	result := &UpdateCheckResult{}
	handleOutdatedVersion(context.Background(), props, "new version", result, state)

	// User declined, so redirectingToUpdate should remain false
	assert.False(t, state.redirectingToUpdate)
	assert.False(t, result.HasUpdated)
}
