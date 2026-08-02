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

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
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
		expectedDebug bool
	}{
		{
			name: "default flags",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("debug", false, "debug flag")
				return cmd
			},
			expectError:   false,
			expectedDebug: false,
		},
		{
			name: "debug flag set to true",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
				cmd.Flags().Bool("debug", true, "debug flag")
				return cmd
			},
			expectError:   false,
			expectedDebug: true,
		},
		{
			name: "missing debug flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{}
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
			assert.Equal(t, tt.expectedDebug, flags.Debug)
		})
	}
}

func TestBuildConfigStore(t *testing.T) {
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
			cfg, err := buildConfigStore(t.Context(), opts)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, cfg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			view := cfg.View()

			// Test expected values
			if tt.expectedMainKey != "" {
				assert.Equal(t, tt.expectedMainKey, view.GetString("main.key"))
			}
			if tt.expectedEmbedKey != "" {
				assert.Equal(t, tt.expectedEmbedKey, view.GetString("embedded.key"))
			}
			if tt.expectedSharedKey != "" {
				// Access the shared value from the main section
				assert.Equal(t, tt.expectedSharedKey, view.GetString("main.shared"))
			}
		})
	}
}

// TestBuildConfigStore_WriteSucceedsWithMissingLowerPrecedenceFile is the
// regression guard for the config-set-routes-to-/etc bug. When a lower-
// precedence config path is declared but absent (the system /etc file a user
// has not created), a write to the existing higher-precedence user file must
// succeed: the absent file is excluded from the layers, so it can neither
// capture the write nor break the store's candidate rebuild on Apply.
func TestBuildConfigStore_WriteSucceedsWithMissingLowerPrecedenceFile(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := &p.Props{Logger: logger.NewNoop(), FS: afero.NewMemMapFs(), Assets: p.NewAssets()}
	userPath := "/home/u/.tool/config.yaml"
	etcPath := "/etc/tool/config.yaml" // declared, never created — lower precedence
	require.NoError(t, afero.WriteFile(props.FS, userPath, []byte("log:\n  level: info\n"), 0o600))

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		CfgPaths:   []string{etcPath, userPath}, // /etc first (lowest), user last (write target)
		Props:      props,
		AllowEmpty: false,
	})
	require.NoError(t, err)

	_, err = store.Apply(t.Context(), config.Set("log.level", "debug"))
	require.NoError(t, err, "a write must not fail because a lower-precedence declared file is absent")
	assert.Equal(t, "debug", store.View().GetString("log.level"))

	data, err := afero.ReadFile(props.FS, userPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "level: debug")
}

// TestBuildConfigStore_CreatesWriteTargetWhenAllAbsent covers Option A's write-
// target exception: with no config file on disk but writes allowed, the highest-
// precedence path is still a valid write destination and the write creates it.
func TestBuildConfigStore_CreatesWriteTargetWhenAllAbsent(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	props := &p.Props{Logger: logger.NewNoop(), FS: afero.NewMemMapFs(), Assets: p.NewAssets()}
	userPath := "/home/u/.tool/config.yaml"
	etcPath := "/etc/tool/config.yaml"

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		CfgPaths:   []string{etcPath, userPath}, // neither exists
		Props:      props,
		AllowEmpty: true,
	})
	require.NoError(t, err)

	_, err = store.Apply(t.Context(), config.Set("feature.enabled", true))
	require.NoError(t, err)

	// The write created the user path (the target), not the /etc path.
	exists, _ := afero.Exists(props.FS, userPath)
	assert.True(t, exists, "the write target must be created")
	etcExists, _ := afero.Exists(props.FS, etcPath)
	assert.False(t, etcExists, "the absent lower-precedence path must not be created")
}

func TestExistingConfigPaths(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/a.yaml", []byte("x: 1\n"), 0o600))
	fsys := (&p.Props{FS: fs}).GetConfigFS()

	got, err := existingConfigPaths(fsys, []string{"/a.yaml", "/missing.yaml"})
	require.NoError(t, err)
	assert.Equal(t, []string{"/a.yaml"}, got)
}

func TestDeclaredConfigPaths(t *testing.T) {
	t.Parallel()

	// Write target (last) absent → appended so a write can create it.
	assert.Equal(t, []string{"/etc.yaml", "/user.yaml"},
		declaredConfigPaths([]string{"/etc.yaml"}, []string{"/etc.yaml", "/user.yaml"}))

	// Write target present → returned unchanged.
	assert.Equal(t, []string{"/etc.yaml", "/user.yaml"},
		declaredConfigPaths([]string{"/etc.yaml", "/user.yaml"}, []string{"/etc.yaml", "/user.yaml"}))

	// No paths → nothing to declare.
	assert.Empty(t, declaredConfigPaths(nil, nil))
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

			// The embedded config arrives through a registered asset bundle,
			// exactly as a feature ships one.
			mockAssets := fstest.MapFS{
				tt.embedConfigPath: &fstest.MapFile{Data: []byte(tt.embedConfigContent)},
			}

			props := &p.Props{
				Logger: l,
				FS:     fs,
				Assets: p.NewAssets(p.AssetMap{"test": mockAssets}),
			}

			store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
				CfgPaths:    []string{"main-config.yaml"},
				ConfigPaths: []string{tt.embedConfigPath},
				Props:       props,
			})
			require.NoError(t, err, "failed to build config store")

			// The merged config should have main config values taking precedence
			mergedCfg := store.View()

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

			view := testutil.ViewFromYAML(t,
				"log:\n  level: "+tt.logLevel+"\n  format: "+tt.logFormat+"\n")

			// Create level var for MCP logging
			mcpLogLevel := &slog.LevelVar{}
			// Default to info
			mcpLogLevel.Set(slog.LevelInfo)

			// Configure logging
			configureLogging(props, flags, view, mcpLogLevel)

			// Map the expected charm level to slog for the assertions below.
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

			// Verify the application logger's level was configured correctly.
			// GetLevel left the Logger interface; assert via Enabled instead.
			assert.True(t, props.Logger.Enabled(context.Background(), expectedSlogLevel),
				"logger should be enabled at the configured level")

			if expectedSlogLevel > slog.LevelDebug {
				assert.False(t, props.Logger.Enabled(context.Background(), expectedSlogLevel-1),
					"logger should be disabled below the configured level")
			}

			// Verify MCP log level matches.
			assert.Equal(t, expectedSlogLevel, mcpLogLevel.Level())
		})
	}
}

func TestShouldSkipUpdateCheck(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	// Neutralise any ambient CI=true (set by the GitLab runner): the update
	// check now also skips on the CI environment variable for flag/environment
	// parity, so the non-CI "do not skip" cases below must pin CI unset to test
	// the deterministic baseline. The explicit CI paths are covered by the
	// configCI cases here and by TestShouldSkipUpdateCheck_CIEnvVar. Set on the
	// parent (which is not itself parallel) so it applies to every subtest.
	t.Setenv("CI", "")

	tests := []struct {
		name         string
		toolDisabled []p.FeatureID
		redirecting  bool
		configCI     bool
		cmdName      string
		cmdFeature   p.FeatureID
		cmdMarked    bool
		expectedSkip bool
	}{
		{
			name:         "skip when update command disabled in tool",
			toolDisabled: []p.FeatureID{p.UpdateCmd},
			expectedSkip: true,
		},
		{
			name:         "skip when redirecting to update",
			redirecting:  true,
			expectedSkip: true,
		},
		{
			// The --ci flag reaches configuration through the flags layer, so
			// the config key is the single source of truth here.
			name:         "skip when CI flag is true",
			configCI:     true,
			expectedSkip: true,
		},
		{
			name:         "skip when config CI is true",
			configCI:     true,
			expectedSkip: true,
		},
		{
			// Exemption is feature-based, not name-based: the init command is
			// identified by its InitCmd feature annotation.
			name:         "skip when running the init-feature command",
			cmdName:      "init",
			cmdFeature:   p.InitCmd,
			expectedSkip: true,
		},
		{
			name:         "skip when running the update-feature command",
			cmdName:      "update",
			cmdFeature:   p.UpdateCmd,
			expectedSkip: true,
		},
		{
			// version carries the MarkSkipUpdateCheck annotation rather than a
			// feature (it is wrapped with the empty feature).
			name:         "skip when running an annotated command",
			cmdName:      "version",
			cmdMarked:    true,
			expectedSkip: true,
		},
		{
			name:         "do not skip for normal command",
			cmdName:      "other",
			expectedSkip: false,
		},
		{
			// A downstream command coincidentally named like an old Use-string
			// skip entry gets no exemption — annotations decide, not names.
			name:         "do not skip for a bare command named auth",
			cmdName:      "auth",
			expectedSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create per-test state
			state := newRootState()
			state.redirectingToUpdate = tt.redirecting

			yaml := "{}\n"
			if tt.configCI {
				yaml = "ci: true\n"
			}

			mockCfg := testutil.StoreFromYAML(t, yaml)

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

			// Create command, with the exempting metadata under test: a
			// feature annotation (setup.Wrap) or the skip-update-check stamp.
			cmd := &cobra.Command{
				Use: tt.cmdName,
			}
			if tt.cmdFeature != "" {
				setup.Wrap(tt.cmdFeature, cmd)
			}

			if tt.cmdMarked {
				setup.MarkSkipUpdateCheck(cmd)
			}

			// Test shouldSkipUpdateCheck
			result := shouldSkipUpdateCheck(props, mockCfg.View(), cmd, state)

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
			handleOutdatedVersion(context.Background(), props, tt.message, result, state, p.UpdatePolicyPrompt, WithForm(mockFormCreator))

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

	handleOutdatedVersion(context.Background(), props, "Version 2.0.0 is available", result, state, p.UpdatePolicyPrompt, WithForm(failingFormCreator))

	assert.False(t, state.redirectingToUpdate, "a failed prompt must not redirect to update")
	assert.False(t, result.HasUpdated, "a failed prompt must not run the update")
	assert.NoError(t, result.Error, "a failed prompt must decline gracefully, not attempt an update")
}

// TestHandleOutdatedVersion_Policies pins the three-state ForcedUpdate
// behaviour for the paths that do not perform a real self-update: disabled
// (warn only), prompt-decline (continue), and enabled-decline / enabled with no
// answerable prompt (blocked with a non-zero error — never a masked continue).
func TestHandleOutdatedVersion_Policies(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	newProps := func() *p.Props {
		return &p.Props{Logger: logger.NewNoop(), FS: afero.NewMemMapFs(), Tool: p.Tool{Name: "test-tool"}}
	}

	t.Run("disabled: warn only, no prompt, no error or exit", func(t *testing.T) {
		t.Parallel()

		formCalled := false
		form := func(_ *bool) *huh.Form { formCalled = true; return nil }
		result := &UpdateCheckResult{}

		handleOutdatedVersion(context.Background(), newProps(), "v2 available", result, newRootState(), p.UpdatePolicyDisabled, WithForm(form))

		assert.False(t, formCalled, "disabled must not prompt")
		assert.False(t, result.HasUpdated)
		assert.False(t, result.ShouldExit)
		assert.NoError(t, result.Error)
	})

	t.Run("prompt + decline: continue with no error", func(t *testing.T) {
		t.Parallel()

		form := func(runUpdate *bool) *huh.Form { *runUpdate = false; return nil }
		result := &UpdateCheckResult{}

		handleOutdatedVersion(context.Background(), newProps(), "v2 available", result, newRootState(), p.UpdatePolicyPrompt, WithForm(form))

		require.NoError(t, result.Error, "prompt-decline continues with the command")
		assert.False(t, result.ShouldExit)
		assert.False(t, result.HasUpdated)
	})

	t.Run("enabled + decline: blocked with non-zero error", func(t *testing.T) {
		t.Parallel()

		form := func(runUpdate *bool) *huh.Form { *runUpdate = false; return nil }
		result := &UpdateCheckResult{}

		handleOutdatedVersion(context.Background(), newProps(), "v2 available", result, newRootState(), p.UpdatePolicyEnabled, WithForm(form))

		require.Error(t, result.Error, "enabled blocks when a required update is declined")
		assert.False(t, result.HasUpdated)
		assert.False(t, result.ShouldExit)
	})

	t.Run("enabled + no answerable prompt: blocked with non-zero error", func(t *testing.T) {
		t.Parallel()

		failing := func(runUpdate *bool) *huh.Form {
			return createUpdatePromptForm(runUpdate).
				WithInput(strings.NewReader("")).
				WithOutput(io.Discard).
				WithTimeout(100 * time.Millisecond)
		}
		result := &UpdateCheckResult{}

		handleOutdatedVersion(context.Background(), newProps(), "v2 available", result, newRootState(), p.UpdatePolicyEnabled, WithForm(failing))

		require.Error(t, result.Error, "enabled blocks when no prompt can be answered (cron/CI/pipe)")
	})
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

// TestNewCmdRoot_DefaultsCollector proves the documented Props.Collector
// invariant: a Props constructed as a struct literal (no Collector set) gets a
// non-nil, disabled noop collector once the root command tree is built — before
// the PersistentPreRunE ever runs.
func TestNewCmdRoot_DefaultsCollector(t *testing.T) {
	// Not parallel: NewCmdRoot seals the process-global middleware registry.
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Tool: p.Tool{
			Name:     "tool",
			Features: p.SetFeatures(p.Disable(p.UpdateCmd), p.Disable(p.InitCmd), p.Disable(p.McpCmd), p.Disable(p.DocsCmd)),
		},
	}

	require.Nil(t, props.Collector, "precondition: struct-literal Props has no collector")

	_ = NewCmdRoot(props)

	require.NotNil(t, props.Collector, "NewCmdRoot must default the collector to uphold the invariant")
	assert.False(t, props.Collector.Enabled(), "the defaulted collector is a disabled noop")
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
	handleOutdatedVersion(context.Background(), props, "new version", result, state, p.UpdatePolicyPrompt)

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
	t.Parallel()

	// The warning tracks the current credential schema (doctor.LiteralCredentialKeys)
	// — after the forge migration that is github.auth.value, not the stale
	// pre-migration github.token that could never fire.
	log := logger.NewBuffer()
	cfg := testutil.ViewFromYAML(t, "github:\n  auth:\n    value: \"\"\n")
	validateConfig(cfg, log)
	assert.True(t, log.Contains("github.auth.value is set but empty"))
}

// TestValidateConfig_StaleGithubTokenKeyNotChecked pins the regression: the
// pre-forge-migration "github.token" key is no longer in the warned set, so a
// literal empty github.token never produces the stale warning.
func TestValidateConfig_StaleGithubTokenKeyNotChecked(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()
	cfg := testutil.ViewFromYAML(t, "github:\n  token: \"\"\n")
	validateConfig(cfg, log)
	assert.False(t, log.Contains("github.token"))
}

func TestValidateConfig_NoWarningForMissingKeys(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()
	cfg := testutil.ViewFromYAML(t, "other: value\n")
	validateConfig(cfg, log)
	assert.False(t, log.Contains("is set but empty"))
}

func TestEmbeddedSources_NilAssets(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: nil,
	}
	result := embeddedSources(ConfigLoadOptions{
		ConfigPaths: []string{"config.yaml"},
		Props:       props,
	})
	assert.Empty(t, result)
}

func TestEmbeddedSources_EmptyPaths(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}
	result := embeddedSources(ConfigLoadOptions{
		ConfigPaths: []string{},
		Props:       props,
	})
	assert.Empty(t, result)
}

func TestEmbeddedSources_WithAssets(t *testing.T) {
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
	result := embeddedSources(ConfigLoadOptions{
		ConfigPaths: []string{"config.yaml"},
		Props:       props,
	})
	require.Len(t, result, 1)
	assert.Equal(t, "embedded:config.yaml", result[0].Name)
	assert.Equal(t, "key: value\n", string(result[0].Content))
}

func TestMapLogLevel_Default(t *testing.T) {
	t.Parallel()
	// A level value that matches no case should return slog.LevelInfo
	unknown := logger.Level(9999)
	assert.Equal(t, slog.LevelInfo, mapLogLevel(unknown))
}

func TestBuildConfigStore_WithEmbeddedConfig(t *testing.T) {
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

	cfg, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		CfgPaths:    []string{"main.yaml"},
		ConfigPaths: []string{"defaults.yaml"},
		Props:       props,
		AllowEmpty:  false,
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	view := cfg.View()
	// main.yaml overrides the embedded default
	assert.Equal(t, "override", view.GetString("main.key"))
	// embedded-only key preserved
	assert.Equal(t, "value", view.GetString("main.extra"))
}

func TestExecute_Success(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)
	t.Parallel()

	var buf strings.Builder
	l := logger.NewCharm(&buf)
	exitCalled := false
	eh := errorhandling.New(logger.ToSlog(l), nil, errorhandling.WithExitFunc(func(int) { exitCalled = true }))

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
	eh := errorhandling.New(logger.ToSlog(log), nil, errorhandling.WithExitFunc(func(int) { exitCalled = true }))

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
	eh := errorhandling.New(logger.ToSlog(log), nil, errorhandling.WithExitFunc(func(int) { exitCalled = true }))

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

	var configWhenChildHookRan *config.Store

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
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
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
