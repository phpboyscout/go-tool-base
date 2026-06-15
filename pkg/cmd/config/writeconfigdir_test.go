package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfigAtomic must create the parent config directory lazily at write
// time, now that setup.GetDefaultConfigDir is pure and no longer creates
// ~/.toolname as a side effect.
func TestWriteConfigAtomic_CreatesParentDirectory(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := "/home/example/.writetool"
	path := filepath.Join(dir, "config.yaml")

	// Precondition: the parent directory does not exist.
	exists, err := afero.DirExists(fs, dir)
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, writeConfigAtomic(fs, path, []byte("telemetry:\n  enabled: false\n")))

	dirExists, err := afero.DirExists(fs, dir)
	require.NoError(t, err)
	assert.True(t, dirExists, "writeConfigAtomic must MkdirAll the parent dir at write")

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: false")
}
