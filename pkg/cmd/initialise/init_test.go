package initialise

import (
	"path/filepath"
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProps() *p.Props {
	return &p.Props{
		Tool:         p.Tool{Name: "test-tool"},
		Logger:       logger.NewNoop(),
		FS:           afero.NewMemMapFs(),
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}
}

// resetSkipFlags re-parses the registered feature flags with explicit false
// values so that package-level skip vars (e.g. skipAI, skipLogin, skipKey)
// are reset after tests that set them via command execution.
func resetSkipFlags(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		resetCmd := &cobra.Command{Use: "reset"}
		registerFeatureFlags(resetCmd)
		_ = resetCmd.ParseFlags([]string{
			"--skip-login=false",
			"--skip-key=false",
			"--skip-ai=false",
		})
	})
}

func TestDiscoverInitialisers_AiEnabled(t *testing.T) {
	t.Parallel()
	props := newTestProps()
	props.Tool.Features = p.SetFeatures(p.Enable(p.AiCmd))

	initialisers := discoverInitialisers(props)
	// AiCmd has a registered provider — at least one should be returned
	assert.NotEmpty(t, initialisers)
}

func TestDiscoverInitialisers_AllDisabled(t *testing.T) {
	t.Parallel()
	props := newTestProps()
	// Disable everything so no initialisers are discovered
	props.Tool.Features = p.SetFeatures(
		p.Disable(p.UpdateCmd),
		p.Disable(p.InitCmd),
		p.Disable(p.McpCmd),
		p.Disable(p.DocsCmd),
		p.Disable(p.DoctorCmd),
		p.Disable(p.AiCmd),
	)

	initialisers := discoverInitialisers(props)
	assert.Empty(t, initialisers)
}

func TestRegisterSubcommands_AiEnabled(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	props := newTestProps()
	props.Tool.Features = p.SetFeatures(p.Enable(p.AiCmd))

	parent := &cobra.Command{Use: "init"}
	registerSubcommands(props, parent)
	// ai subcommand should have been registered
	assert.NotEmpty(t, parent.Commands())
}

func TestNewCmdInit(t *testing.T) {
	resetSkipFlags(t)

	fs := afero.NewMemMapFs()
	// Mock HOME for default config dir
	t.Setenv("HOME", "/tmp/home")

	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger:       logger.NewNoop(),
		FS:           fs,
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}

	props.Assets = p.NewAssets()
	cmd := NewCmdInit(props)

	// Execute command with defaults
	// This will try to write to /tmp/home/.config/test-tool/config.yaml (or similar)
	cmd.SetArgs([]string{"--skip-login", "--skip-key", "--clean"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify file exists
	defaultDir := setup.GetDefaultConfigDir(fs, "test-tool")
	exists, _ := afero.DirExists(fs, defaultDir)
	assert.True(t, exists, "config dir should exist")

	configFile := filepath.Join(defaultDir, setup.DefaultConfigFilename)
	exists, _ = afero.Exists(fs, configFile)
	assert.True(t, exists, "config file should exist")
}

func TestNewCmdInit_FlagCombinations(t *testing.T) {
	resetSkipFlags(t)
	t.Setenv("HOME", "/tmp/home")

	tests := []struct {
		name  string
		flags []string
	}{
		{
			name:  "skip-login only",
			flags: []string{"--skip-login", "--skip-ai"},
		},
		{
			name:  "skip-key only",
			flags: []string{"--skip-key", "--skip-ai"},
		},
		{
			name:  "skip-login and skip-key",
			flags: []string{"--skip-login", "--skip-key", "--skip-ai"},
		},
		{
			name:  "clean only",
			flags: []string{"--clean", "--skip-login", "--skip-key", "--skip-ai"},
		},
		{
			name:  "all flags",
			flags: []string{"--skip-login", "--skip-key", "--skip-ai", "--clean"},
		},
		{
			name:  "no flags besides skips",
			flags: []string{"--skip-login", "--skip-key", "--skip-ai"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()

			props := &p.Props{
				Tool:         p.Tool{Name: "test-tool"},
				Logger:       logger.NewNoop(),
				FS:           fs,
				Assets:       p.NewAssets(),
				ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
			}

			cmd := NewCmdInit(props)
			cmd.SetArgs(tt.flags)

			err := cmd.Execute()
			require.NoError(t, err)

			defaultDir := setup.GetDefaultConfigDir(fs, "test-tool")
			configFile := filepath.Join(defaultDir, setup.DefaultConfigFilename)
			exists, _ := afero.Exists(fs, configFile)
			assert.True(t, exists, "config file should exist")
		})
	}
}

func TestNewCmdInit_CleanOverwritesExistingConfig(t *testing.T) {
	resetSkipFlags(t)
	t.Setenv("HOME", "/tmp/home")

	fs := afero.NewMemMapFs()

	props := &p.Props{
		Tool:         p.Tool{Name: "test-tool"},
		Logger:       logger.NewNoop(),
		FS:           fs,
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}

	defaultDir := setup.GetDefaultConfigDir(fs, "test-tool")
	configFile := filepath.Join(defaultDir, setup.DefaultConfigFilename)

	// Write an existing config with a custom value
	existingConfig := "custom_key: custom_value\n"
	require.NoError(t, fs.MkdirAll(defaultDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, configFile, []byte(existingConfig), 0o644))

	// Run init with --clean — should replace config, not merge
	cmd := NewCmdInit(props)
	cmd.SetArgs([]string{"--clean", "--skip-login", "--skip-key", "--skip-ai"})
	require.NoError(t, cmd.Execute())

	content, err := afero.ReadFile(fs, configFile)
	require.NoError(t, err)

	// The custom_key should NOT be present because --clean replaces the config
	assert.NotContains(t, string(content), "custom_key")
}

func TestNewCmdInit_WithoutCleanMergesExistingConfig(t *testing.T) {
	resetSkipFlags(t)
	t.Setenv("HOME", "/tmp/home")

	fs := afero.NewMemMapFs()

	props := &p.Props{
		Tool:         p.Tool{Name: "test-tool"},
		Logger:       logger.NewNoop(),
		FS:           fs,
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}

	defaultDir := setup.GetDefaultConfigDir(fs, "test-tool")
	configFile := filepath.Join(defaultDir, setup.DefaultConfigFilename)

	// Write an existing config with a custom value
	existingConfig := "custom_key: custom_value\n"
	require.NoError(t, fs.MkdirAll(defaultDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, configFile, []byte(existingConfig), 0o644))

	// Run init WITHOUT --clean — should merge existing config
	cmd := NewCmdInit(props)
	cmd.SetArgs([]string{"--skip-login", "--skip-key", "--skip-ai"})
	require.NoError(t, cmd.Execute())

	content, err := afero.ReadFile(fs, configFile)
	require.NoError(t, err)

	// The custom_key should still be present because config was merged
	assert.Contains(t, string(content), "custom_key")
	assert.Contains(t, string(content), "custom_value")
}

func TestNewCmdInit_CustomDir(t *testing.T) {
	resetSkipFlags(t)
	t.Setenv("HOME", "/tmp/home")

	fs := afero.NewMemMapFs()

	props := &p.Props{
		Tool:         p.Tool{Name: "test-tool"},
		Logger:       logger.NewNoop(),
		FS:           fs,
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
	}

	customDir := "/tmp/custom-config-dir"

	cmd := NewCmdInit(props)
	cmd.SetArgs([]string{"--dir", customDir, "--skip-login", "--skip-key", "--skip-ai"})
	require.NoError(t, cmd.Execute())

	configFile := filepath.Join(customDir, setup.DefaultConfigFilename)
	exists, _ := afero.Exists(fs, configFile)
	assert.True(t, exists, "config file should exist in custom dir")
}
