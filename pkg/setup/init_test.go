package setup

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitializeConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "config.yaml")

	logger := log.New(io.Discard)
	p := &props.Props{
		Logger: logger,
		FS:     afero.NewMemMapFs(),
	}

	// Test case 1: New config creation
	t.Run("Create new config", func(t *testing.T) {
		cfg, err := initializeConfig(p, tmpDir, targetFile, false)
		require.NoError(t, err)
		assert.NotNil(t, cfg)
	})

	// Test case 2: Existing config merge
	t.Run("Merge existing config", func(t *testing.T) {
		p.FS.MkdirAll(tmpDir, 0755)

		err := afero.WriteFile(p.FS, targetFile, []byte("existing: value\n"), 0644)
		require.NoError(t, err)

		cfg, err := initializeConfig(p, tmpDir, targetFile, false)
		require.NoError(t, err)
		assert.Equal(t, "value", cfg.GetString("existing"))
	})
}
