package initialise

import (
	"path/filepath"
	"testing"

	"github.com/phpboyscout/gtb/pkg/errorhandling"
	"github.com/phpboyscout/gtb/pkg/logger"
	p "github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
