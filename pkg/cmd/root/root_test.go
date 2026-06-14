package root

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cockroachdb/errors"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configMocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// root_test.go provides comprehensive unit tests for the extracted functions in root.go
// These tests focus on the configuration loading, merging, flag processing, and logging setup
// functionality that was extracted from the PersistentPreRunE function for better testability.

func TestExtractFlags(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
				require.Error(t, err)
				assert.Nil(t, flags)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, flags)
			assert.Equal(t, tt.expectedCI, flags.CI)
			assert.Equal(t, tt.expectedDebug, flags.Debug)
		})
	}
}

func TestLoadAndMergeConfig(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
				require.Error(t, err)
				assert.Nil(t, cfg)
				return
			}

			require.NoError(t, err)
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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
			mainCfg, err := config.Load([]string{"main-config.yaml"}, fs, false, config.WithLogger(l))
			require.NoError(t, err, "failed to load main config")

			// Setup mock embedded filesystem using fstest.MapFS
			mockAssets := fstest.MapFS{
				tt.embedConfigPath: &fstest.MapFile{Data: []byte(tt.embedConfigContent)},
			}

			// Load embedded config using our mock
			embeddedCfg, err := config.LoadEmbed([]string{tt.embedConfigPath}, mockAssets, config.WithLogger(l))
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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
			case logger.FatalLevel:
				expectedSlogLevel = slog.LevelError
			default:
				expectedSlogLevel = slog.LevelInfo
			}
			assert.Equal(t, expectedSlogLevel, mcpLogLevel.Level())
		})
	}
}

func TestShouldSkipUpdateCheck(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	// NOTE: The "user accepts update" path is not unit-testable here.
	// When the user accepts, handleOutdatedVersion calls update.Update()
	// directly (a package-level function, not behind a mockable interface).
	// update.Update creates a real setup.Updater that requires VCS clients,
	// network access, and binary replacement. Injecting a mock would require
	// refactoring handleOutdatedVersion to accept an updater interface, which
	// is out of scope for this test file. The accept path is exercised via
	// integration tests and the update command's own test suite instead.
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

// TestHandleOutdatedVersion_PromptFailureMustNotAutoUpdate proves that a
// failed update prompt (no TTY, Ctrl-C abort, timeout) is treated as "No" —
// not as consent to self-update. The form runs headless with an empty input
// and a short timeout so Run() deterministically returns an error
// (huh.ErrTimeout) without needing a terminal, simulating the cron/CI/pipe
// environments where the prompt cannot be answered.
func TestHandleOutdatedVersion_PromptFailureMustNotAutoUpdate(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	failingFormCreator := func(runUpdate *bool) *huh.Form {
		return createUpdatePromptForm(runUpdate).
			WithInput(strings.NewReader("")).
			WithOutput(io.Discard).
			WithTimeout(100 * time.Millisecond)
	}

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Tool:   p.Tool{Name: "test-tool"},
	}

	state := newRootState()
	result := &UpdateCheckResult{}

	handleOutdatedVersion(context.Background(), props, "Version 2.0.0 is available", result, state, WithForm(failingFormCreator))

	assert.False(t, state.redirectingToUpdate, "a failed prompt must not redirect to update")
	assert.False(t, result.HasUpdated, "a failed prompt must not run the update")
	assert.NoError(t, result.Error, "a failed prompt must decline gracefully, not attempt an update")
}

func TestWithFormOption(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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
	// Not parallel: calls NewCmdRoot twice, each of which seals the middleware
	// registry. Reset between calls to prevent the second from panicking.
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

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
	setup.ResetRegistryForTesting() // reset before second NewCmdRoot seals again
	cmd2 := NewCmdRoot(props2)

	// They should be independent commands
	assert.Equal(t, "tool1", cmd1.Use)
	assert.Equal(t, "tool2", cmd2.Use)
}

// TestNewCmdRoot_SecondConstructionDoesNotPanic proves a second NewCmdRoot in
// the same process does not panic on global-middleware re-registration after
// the registry was sealed by the first construction.
func TestNewCmdRoot_SecondConstructionDoesNotPanic(t *testing.T) {
	// Not parallel: mutates the process-global middleware registry.
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	mkProps := func(name string) *p.Props {
		return &p.Props{
			Logger: logger.NewNoop(),
			FS:     afero.NewMemMapFs(),
			Tool: p.Tool{
				Name:     name,
				Features: p.SetFeatures(p.Disable(p.UpdateCmd), p.Disable(p.InitCmd), p.Disable(p.McpCmd), p.Disable(p.DocsCmd)),
			},
		}
	}

	_ = NewCmdRoot(mkProps("tool1"))

	assert.NotPanics(t, func() {
		_ = NewCmdRoot(mkProps("tool2"))
	}, "second NewCmdRoot must not panic after the first sealed the registry")
}

func TestRootState_DefaultFormCreator(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	state := newRootState()
	assert.NotNil(t, state.formCreator, "default form creator should not be nil")
	assert.False(t, state.redirectingToUpdate, "redirectingToUpdate should default to false")
}

func TestErrUpdateComplete(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	// ErrUpdateComplete should be detectable via errors.Is
	err := ErrUpdateComplete
	require.ErrorIs(t, err, ErrUpdateComplete)

	// Wrapping should still be detectable
	wrapped := errors.Wrap(err, "wrapped")
	assert.ErrorIs(t, wrapped, ErrUpdateComplete)
}

func TestHandleOutdatedVersion_SetsStateFlag(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
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

func TestMapLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		level    logger.Level
		expected slog.Level
	}{
		{"debug", logger.DebugLevel, slog.LevelDebug},
		{"info", logger.InfoLevel, slog.LevelInfo},
		{"warn", logger.WarnLevel, slog.LevelWarn},
		{"error", logger.ErrorLevel, slog.LevelError},
		{"fatal", logger.FatalLevel, slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, mapLogLevel(tt.level))
		})
	}
}

func TestValidateConfig_WarnsOnEmptySetKeys(t *testing.T) {
	// Not parallel — t.Setenv modifies process environment
	t.Setenv("GITHUB_TOKEN", "")

	log := logger.NewBuffer()
	cfg := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(log), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader("github:\n  token: \"\"\n")))
	validateConfig(cfg, log)
	assert.True(t, log.Contains("github.token is set but empty"))
}

func TestValidateConfig_NoWarningForMissingKeys(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()
	cfg := config.NewReaderContainer(afero.NewMemMapFs(), config.WithLogger(log), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader("other: value\n")))
	validateConfig(cfg, log)
	assert.False(t, log.Contains("is set but empty"))
}

func TestMergeEmbeddedConfigs_NilAssets(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	props := &p.Props{
		Logger: l,
		FS:     afero.NewMemMapFs(),
		Assets: nil,
	}
	result, err := mergeEmbeddedConfigs(ConfigLoadOptions{
		ConfigPaths: []string{"config.yaml"},
		Props:       props,
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestMergeEmbeddedConfigs_EmptyPaths(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	props := &p.Props{
		Logger: l,
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}
	result, err := mergeEmbeddedConfigs(ConfigLoadOptions{
		ConfigPaths: []string{},
		Props:       props,
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestMergeEmbeddedConfigs_WithAssets(t *testing.T) {
	t.Parallel()

	assets := p.NewAssets(p.AssetMap{
		"test": fstest.MapFS{
			"config.yaml": &fstest.MapFile{Data: []byte("key: value\n")},
		},
	})
	props := &p.Props{
		Logger: logger.NewNoop(),
		Assets: assets,
	}
	result, err := mergeEmbeddedConfigs(ConfigLoadOptions{
		ConfigPaths: []string{"config.yaml"},
		Props:       props,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "value", result.GetString("key"))
}

func TestMapLogLevel_Default(t *testing.T) {
	t.Parallel()
	// A level value that matches no case should return slog.LevelInfo
	unknown := logger.Level(9999)
	assert.Equal(t, slog.LevelInfo, mapLogLevel(unknown))
}

func TestLoadAndMergeConfig_WithEmbeddedConfig(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	l := logger.NewNoop()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "main.yaml", []byte("main:\n  key: override\n"), 0o644))

	assets := p.NewAssets(p.AssetMap{
		"embedded": fstest.MapFS{
			"defaults.yaml": &fstest.MapFile{Data: []byte("main:\n  key: default\n  extra: value\n")},
		},
	})

	props := &p.Props{
		Logger: l,
		FS:     fs,
		Assets: assets,
	}

	cfg, err := loadAndMergeConfig(ConfigLoadOptions{
		CfgPaths:    []string{"main.yaml"},
		ConfigPaths: []string{"defaults.yaml"},
		Props:       props,
		AllowEmpty:  false,
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// main.yaml overrides the embedded default
	assert.Equal(t, "override", cfg.GetString("main.key"))
	// embedded-only key preserved
	assert.Equal(t, "value", cfg.GetString("main.extra"))
}

func TestExecute_Success(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	var buf strings.Builder
	l := logger.NewCharm(&buf)
	exitCalled := false
	eh := errorhandling.New(l, nil, errorhandling.WithExitFunc(func(int) { exitCalled = true }))

	props := &p.Props{
		Logger:       l,
		ErrorHandler: eh,
	}

	cmd := &cobra.Command{
		Use: "root",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	Execute(setup.Wrap("", cmd), props)
	assert.False(t, exitCalled)
}

func TestExecute_ErrUpdateComplete(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	log := logger.NewBuffer()
	exitCalled := false
	eh := errorhandling.New(log, nil, errorhandling.WithExitFunc(func(int) { exitCalled = true }))

	props := &p.Props{
		Logger:       log,
		ErrorHandler: eh,
	}

	cmd := &cobra.Command{
		Use: "root",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ErrUpdateComplete
		},
	}

	Execute(setup.Wrap("", cmd), props)
	assert.False(t, exitCalled)
	assert.True(t, log.Contains("update complete"))
}

func TestExecute_FatalError(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	log := logger.NewBuffer()
	exitCalled := false
	eh := errorhandling.New(log, nil, errorhandling.WithExitFunc(func(int) { exitCalled = true }))

	props := &p.Props{
		Logger:       log,
		ErrorHandler: eh,
	}

	cmd := &cobra.Command{
		Use: "root",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("fatal test error")
		},
	}

	Execute(setup.Wrap("", cmd), props)
	assert.True(t, exitCalled)
	assert.True(t, log.Contains("fatal test error"))
}

func TestMiddleware_IntegrationWithCobra(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	var executed bool
	cmd := &cobra.Command{
		Use: "test",
		RunE: func(cmd *cobra.Command, args []string) error {
			executed = true
			return nil
		},
	}

	setup.RegisterGlobalMiddleware(setup.WithRecovery(logger.NewNoop()))

	cmd.RunE = setup.Chain(p.UpdateCmd, cmd.RunE)
	err := cmd.RunE(cmd, nil)

	require.NoError(t, err)
	assert.True(t, executed)
}

func TestNewCmdRoot_SubcommandsHaveMiddleware(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	var middlewareExecuted bool
	mw := func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			middlewareExecuted = true
			return next(cmd, args)
		}
	}

	// 1. Register global middleware
	setup.RegisterGlobalMiddleware(mw)

	var subcommandExecuted bool
	subcmd := &cobra.Command{
		Use: "sub",
		RunE: func(cmd *cobra.Command, args []string) error {
			subcommandExecuted = true
			return nil
		},
	}

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
		Tool: p.Tool{
			Name: "test",
		},
	}

	// 2. Create root with subcommand - this calls registerFeatureCommands which seals the registry
	// and should now correctly wrap the passed subcommands.
	rootCmd := NewCmdRoot(props, setup.Wrap("", subcmd))

	// 3. Execute the subcommand directly via RunE
	err := subcmd.RunE(subcmd, nil)

	require.NoError(t, err)
	assert.True(t, subcommandExecuted, "subcommand should have executed")
	assert.True(t, middlewareExecuted, "middleware should have executed for subcommand passed to constructor")

	// 4. Test manual registration after root creation
	middlewareExecuted = false
	subcommandExecuted = false
	manualSubcmd := &cobra.Command{
		Use: "manual",
		RunE: func(cmd *cobra.Command, args []string) error {
			subcommandExecuted = true
			return nil
		},
	}

	// Using the new public helper
	rootCmd.Register(setup.Wrap("", manualSubcmd))

	err = manualSubcmd.RunE(manualSubcmd, nil)
	require.NoError(t, err)
	assert.True(t, subcommandExecuted)
	assert.True(t, middlewareExecuted, "middleware should have executed for manually registered subcommand")
}

// bootstrapTestProps builds a Props with the network/prompt-driven features
// disabled so an end-to-end Execute through the command tree exercises only the
// config-loading bootstrap without hitting the update check or telemetry prompt.
// A real config file is written to the in-memory FS and its path returned so the
// caller can point --config at it, keeping the config load deterministic.
func bootstrapTestProps(t *testing.T) (*p.Props, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	cfgPath := "/config.yaml"
	require.NoError(t, afero.WriteFile(fs, cfgPath, []byte("log:\n  level: info\n"), 0o644))

	return &p.Props{
		Logger: logger.NewNoop(),
		FS:     fs,
		Assets: p.NewAssets(),
		Tool: p.Tool{
			Name: "bootstraptool",
			Features: p.SetFeatures(
				p.Disable(p.UpdateCmd),
				p.Disable(p.McpCmd),
				p.Disable(p.DocsCmd),
				p.Disable(p.DoctorCmd),
				p.Disable(p.TelemetryCmd),
			),
		},
	}, cfgPath
}

// TestBootstrapRunsDespiteChildPersistentPreRunE is the core regression test for
// the bootstrap-prerun-traversal fix. A downstream subcommand that defines its
// own PersistentPreRunE must NOT shadow the root bootstrap: cobra runs only the
// closest PersistentPreRunE unless EnableTraverseRunHooks is set. Without the
// fix the child hook shadows the root hook, props.Config / props.Collector are
// never populated, and bootstrap is silently skipped.
func TestBootstrapRunsDespiteChildPersistentPreRunE(t *testing.T) {
	// Not parallel: NewCmdRoot seals the process-global middleware registry,
	// and the test relies on cobra's process-global EnableTraverseRunHooks.
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props, cfgPath := bootstrapTestProps(t)

	var (
		childHookRan bool
		childRan     bool
	)

	child := &cobra.Command{
		Use: "child",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			childHookRan = true

			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			childRan = true

			return nil
		},
	}

	rootCmd := NewCmdRoot(props, setup.Wrap("", child))
	rootCmd.SetArgs([]string{"--config", cfgPath, "child"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	require.NoError(t, rootCmd.ExecuteContext(context.Background()))

	assert.True(t, childRan, "child command should have executed")
	assert.True(t, childHookRan, "child PersistentPreRunE should have executed")
	require.NotNil(t, props.Config,
		"root bootstrap must run even when a child defines its own PersistentPreRunE — props.Config is nil, so bootstrap was silently skipped")
	require.NotNil(t, props.Collector,
		"root bootstrap must build the telemetry collector even when a child defines its own PersistentPreRunE")
}

// TestBootstrapOrdering_RootBeforeChild asserts the documented ordering: with
// EnableTraverseRunHooks the framework bootstrap (root PersistentPreRunE) runs
// before the child's PersistentPreRunE (root→leaf), so the child can rely on
// props.Config already being populated.
func TestBootstrapOrdering_RootBeforeChild(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props, cfgPath := bootstrapTestProps(t)

	var configWhenChildHookRan config.Containable

	child := &cobra.Command{
		Use: "child",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Capture whether the root bootstrap (which sets props.Config) has
			// already run by the time the child hook fires.
			configWhenChildHookRan = props.Config

			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}

	rootCmd := NewCmdRoot(props, setup.Wrap("", child))
	rootCmd.SetArgs([]string{"--config", cfgPath, "child"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	require.NoError(t, rootCmd.ExecuteContext(context.Background()))

	assert.NotNil(t, configWhenChildHookRan,
		"framework bootstrap must run before the child PersistentPreRunE (root→leaf ordering)")
}

// TestNewRootPreRunE_ConfigPathsNotAccumulated proves the configpaths-closure
// fix: the PersistentPreRunE closure must clone the captured configPaths slice
// each invocation so the per-run "assets/init/config.yaml" append neither grows
// the captured slice across repeated runs nor clobbers the caller's backing
// array (it is given spare capacity here so an aliasing append would corrupt the
// element past len).
func TestNewRootPreRunE_ConfigPathsNotAccumulated(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	// Not parallel: exercises the process-global middleware registry path.

	// A slice with spare capacity and a sentinel element past its length. An
	// aliasing append in the closure would overwrite "sentinel".
	backing := make([]string, 1, 2)
	backing[0] = "config.yaml"
	backing = append(backing, "sentinel")
	configPaths := backing[:1] // len 1, cap 2, backing[1] == "sentinel"

	props := &p.Props{
		Logger:       logger.NewNoop(),
		FS:           afero.NewMemMapFs(),
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
		Tool: p.Tool{
			Name: "tool",
			// InitCmd disabled -> allowEmpty true -> the append fires.
			// Update/Telemetry disabled -> no network / no consent prompt.
			Features: p.SetFeatures(
				p.Disable(p.InitCmd),
				p.Disable(p.UpdateCmd),
				p.Disable(p.TelemetryCmd),
			),
		},
	}

	mcpLogLevel := &slog.LevelVar{}
	state := newRootState()
	preRun := newRootPreRunE(props, configPaths, mcpLogLevel, state, map[string]*pflag.Flag{})

	mkCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "tool"}
		cmd.Flags().Bool("ci", true, "ci flag")
		cmd.Flags().Bool("debug", false, "debug flag")
		cmd.SetContext(context.Background())
		return cmd
	}

	// Invoke the closure multiple times, as repeated Execute would.
	for range 3 {
		require.NoError(t, preRun(mkCmd(), nil))
	}

	// The caller's slice length is untouched...
	assert.Len(t, configPaths, 1, "captured slice length must not grow")
	assert.Equal(t, "config.yaml", configPaths[0])
	// ...and the sentinel past len was never clobbered by an aliasing append.
	assert.Equal(t, "sentinel", backing[1],
		"closure must clone configPaths, not append into the caller's backing array")
}
