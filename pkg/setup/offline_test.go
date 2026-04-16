package setup

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func setupOfflineUpdater(t *testing.T, fs afero.Fs, toolName string) *SelfUpdater {
	t.Helper()

	currentBin := "/usr/local/bin/" + toolName
	require.NoError(t, fs.MkdirAll(filepath.Dir(currentBin), 0o755))

	return NewOfflineUpdater(props.Tool{Name: toolName}, logger.NewNoop(), fs,
		WithOsExecutable(func() (string, error) { return currentBin, nil }),
		WithExecLookPath(func(_ string) (string, error) { return currentBin, nil }),
	)
}

func TestUpdateFromFile_Success(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	tarData := createTarGz(t, toolName, "binary-content")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))

	targetPath, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.NoError(t, err)
	assert.Equal(t, "/usr/local/bin/"+toolName, targetPath)

	content, err := afero.ReadFile(fs, targetPath)
	require.NoError(t, err)
	assert.Equal(t, "binary-content", string(content))
}

func TestUpdateFromFile_WithValidChecksum(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	tarData := createTarGz(t, toolName, "binary-content")
	hash := fmt.Sprintf("%x", sha256.Sum256(tarData))

	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz.sha256", []byte(hash+"  release.tar.gz\n"), 0o644))

	targetPath, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.NoError(t, err)
	assert.Equal(t, "/usr/local/bin/"+toolName, targetPath)
}

func TestUpdateFromFile_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	tarData := createTarGz(t, toolName, "binary-content")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz.sha256", []byte("0000000000000000000000000000000000000000000000000000000000000000  release.tar.gz\n"), 0o644))

	_, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestUpdateFromFile_NoSidecar(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	tarData := createTarGz(t, toolName, "binary-content")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))

	targetPath, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.NoError(t, err)
	assert.NotEmpty(t, targetPath)
}

func TestUpdateFromFile_FileNotFound(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	_, err := updater.UpdateFromFile("/nonexistent/release.tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read update file")
}

func TestUpdateFromFile_InvalidTarball(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", []byte("not-a-tarball"), 0o644))

	_, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.Error(t, err)
}

func TestUpdateFromFile_BinaryNotInArchive(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	tarData := createTarGz(t, "other-tool", "binary-content")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))

	targetPath, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.NoError(t, err)
	assert.NotEmpty(t, targetPath)
}
