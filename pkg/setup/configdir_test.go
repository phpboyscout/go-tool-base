package setup

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetDefaultConfigDir must be pure: it computes and returns the path but never
// creates the directory. Merely resolving the path (e.g. while building --help
// or default flag values) must not materialise ~/.toolname on disk.
func TestGetDefaultConfigDir_DoesNotCreateDirectory(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const name = "puretool"

	dir := GetDefaultConfigDir(fs, name)
	require.NotEmpty(t, dir, "expected a resolvable home dir in the test environment")

	exists, err := afero.DirExists(fs, dir)
	require.NoError(t, err)
	assert.False(t, exists, "GetDefaultConfigDir must not create the directory as a side effect")
}

// setTimeSinceLastIn writes a timestamp marker and must create the parent config
// directory lazily at write time, now that GetDefaultConfigDir is pure.
func TestSetTimeSinceLastIn_CreatesDirectoryAtWrite(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	configDir := "/home/example/.timestamptool"

	// Precondition: the directory does not exist.
	exists, err := afero.DirExists(fs, configDir)
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, setTimeSinceLastIn(fs, configDir, CheckedKey))

	// The write must have created the directory and the marker file.
	dirExists, err := afero.DirExists(fs, configDir)
	require.NoError(t, err)
	assert.True(t, dirExists, "the writer must MkdirAll the config dir at first write")

	fileExists, err := afero.Exists(fs, filepath.Join(configDir, "last_checked"))
	require.NoError(t, err)
	assert.True(t, fileExists, "the timestamp marker must be written")
}

// An empty config dir remains a no-op (the Plan 2 empty-HOME guard) and must not
// attempt any MkdirAll/write against a relative path.
func TestSetTimeSinceLastIn_EmptyDirIsNoop(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	require.NoError(t, setTimeSinceLastIn(fs, "", CheckedKey))

	// Nothing should have been created.
	exists, err := afero.Exists(fs, "last_checked")
	require.NoError(t, err)
	assert.False(t, exists, "empty config dir must remain a no-op")
}
