package setup

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

	// An archive missing the expected binary must fail loudly, not report a
	// successful update while leaving the old binary in place.
	_, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrBinaryNotInArchive)
}

func TestUpdateFromFile_RequireChecksumNoSidecar(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)
	updater.requireChecksum = true

	tarData := createTarGz(t, toolName, "binary-content")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))

	// require_checksum is on but there is no .sha256 sidecar: the offline
	// path must refuse rather than warn-and-proceed.
	_, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "require_checksum")
}

func TestUpdateFromFile_RequireSignatureRefused(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)
	updater.requireSignature = true

	tarData := createTarGz(t, toolName, "binary-content")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))

	// The offline path cannot verify an OpenPGP signature, so a required
	// signature must abort rather than be silently skipped.
	_, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "require_signature")
}

func TestUpdateFromFile_WindowsExeBinary(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	toolName := "test-tool"
	updater := setupOfflineUpdater(t, fs, toolName)

	// GoReleaser names the Windows inner binary "<name>.exe"; extract must
	// match it, otherwise Windows self-update is a silent no-op.
	tarData := createTarGz(t, toolName+".exe", "windows-binary")
	require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", tarData, 0o644))

	targetPath, err := updater.UpdateFromFile("/tmp/release.tar.gz")
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, targetPath)
	require.NoError(t, err)
	assert.Equal(t, "windows-binary", string(content))
}

func TestResolveTargetPath_LookPathFailureFallsBack(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	u := NewOfflineUpdater(props.Tool{Name: "test-tool"}, logger.NewNoop(), fs,
		WithOsExecutable(func() (string, error) { return "/opt/custom/test-tool", nil }),
		WithExecLookPath(func(_ string) (string, error) { return "", assert.AnError }),
	)

	// LookPath failure (tool not on PATH) must not abort — the running
	// executable's own path is authoritative.
	got, err := u.resolveTargetPath()
	require.NoError(t, err)
	assert.Equal(t, "/opt/custom/test-tool", got)
}

func TestResolveTargetPath_NonInteractiveDiffersUsesExecutable(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	u := NewOfflineUpdater(props.Tool{Name: "test-tool"}, logger.NewNoop(), fs,
		WithOsExecutable(func() (string, error) { return "/running/test-tool", nil }),
		WithExecLookPath(func(_ string) (string, error) { return "/usr/bin/test-tool", nil }),
	)
	u.isInteractive = func() bool { return false }

	// Differing paths with no TTY must default to the running executable
	// rather than blocking on an unanswerable prompt.
	got, err := u.resolveTargetPath()
	require.NoError(t, err)
	assert.Equal(t, "/running/test-tool", got)
}
