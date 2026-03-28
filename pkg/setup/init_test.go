package setup

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestInitializeConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "config.yaml")

	l := logger.NewNoop()
	p := &props.Props{
		Logger: l,
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
		require.NoError(t, p.FS.MkdirAll(tmpDir, 0755))

		err := afero.WriteFile(p.FS, targetFile, []byte("existing: value\n"), 0644)
		require.NoError(t, err)

		cfg, err := initializeConfig(p, tmpDir, targetFile, false)
		require.NoError(t, err)
		assert.Equal(t, "value", cfg.GetString("existing"))
	})
}

func TestWriteGitignore_NewDir(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/home/user/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	err := writeGitignore(fs, configDir)
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, filepath.Join(configDir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "*.env")
	assert.Contains(t, string(content), "*.secret")
	assert.Contains(t, string(content), "*.key")
}

func TestWriteGitignore_ExistingPreserved(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/home/user/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	existingContent := "# my custom gitignore\n*.log\n"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, ".gitignore"), []byte(existingContent), 0644))

	err := writeGitignore(fs, configDir)
	require.NoError(t, err)

	// Should not overwrite existing .gitignore
	content, err := afero.ReadFile(fs, filepath.Join(configDir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(content))
}

func TestWarnIfAPIKeysInGitRepo_Warns(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/project/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	// Create .git dir in parent to simulate git repo
	_ = fs.MkdirAll("/project/.git", 0755)

	// Write config with an API key pattern
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-abc123"), 0644))

	var buf bytes.Buffer
	l := logger.NewCharm(&buf)

	p := &props.Props{
		Logger: l,
		FS:     fs,
	}

	warnIfAPIKeysInGitRepo(p, configDir)
	assert.Contains(t, buf.String(), "API keys")
}

func TestWarnIfAPIKeysInGitRepo_NoGitRepo(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/project/.mytool"
	_ = fs.MkdirAll(configDir, 0755)

	// No .git directory
	require.NoError(t, afero.WriteFile(fs, filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-abc123"), 0644))

	var buf bytes.Buffer
	l := logger.NewCharm(&buf)

	p := &props.Props{
		Logger: l,
		FS:     fs,
	}

	warnIfAPIKeysInGitRepo(p, configDir)
	assert.Empty(t, buf.String())
}
