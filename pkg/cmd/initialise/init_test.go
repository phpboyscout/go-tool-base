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
