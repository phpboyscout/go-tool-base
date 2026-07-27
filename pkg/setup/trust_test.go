package setup_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// writeProjectFile writes a project-local config file on a real filesystem under
// a temp dir (the trust store keys on absolute paths, and the store itself lives
// under the OS home) and returns the absolute path.
func writeProjectFile(t *testing.T, contents string) (afero.Fs, string) {
	t.Helper()

	// A real HOME so GetDefaultConfigDir resolves and the trust store persists
	// alongside a real, isolated directory.
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	dir := t.TempDir()
	path := filepath.Join(dir, ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, path, []byte(contents), 0o600))

	return fs, path
}

func TestProjectConfigTrust_RoundTrip(t *testing.T) {
	fs, path := writeProjectFile(t, "update:\n  require_signature: false\n")

	// Untrusted by default.
	trusted, err := setup.IsProjectConfigTrusted(fs, "mytool", path)
	require.NoError(t, err)
	assert.False(t, trusted, "a freshly discovered file is untrusted")

	// Trust it.
	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

	trusted, err = setup.IsProjectConfigTrusted(fs, "mytool", path)
	require.NoError(t, err)
	assert.True(t, trusted, "after trust the file is trusted")

	// It appears in the list.
	list, err := setup.ListTrustedProjects(fs, "mytool")
	require.NoError(t, err)
	abs, _ := filepath.Abs(path)
	assert.Contains(t, list, abs)

	// Forgetting revokes trust.
	require.NoError(t, setup.UntrustProjectConfig(fs, "mytool", path))
	trusted, err = setup.IsProjectConfigTrusted(fs, "mytool", path)
	require.NoError(t, err)
	assert.False(t, trusted, "after forget the file is untrusted")
}

// TestProjectConfigTrust_EditRevokes pins the direnv-style behaviour: editing a
// trusted file invalidates trust because the recorded content hash no longer
// matches.
func TestProjectConfigTrust_EditRevokes(t *testing.T) {
	fs, path := writeProjectFile(t, "log:\n  level: info\n")

	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

	trusted, err := setup.IsProjectConfigTrusted(fs, "mytool", path)
	require.NoError(t, err)
	require.True(t, trusted)

	// Edit the file: trust must lapse until re-trusted.
	require.NoError(t, afero.WriteFile(fs, path, []byte("log:\n  level: debug\n"), 0o600))

	trusted, err = setup.IsProjectConfigTrusted(fs, "mytool", path)
	require.NoError(t, err)
	assert.False(t, trusted, "editing a trusted file revokes trust")
}

func TestIsProjectConfigTrusted_EmptyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	trusted, err := setup.IsProjectConfigTrusted(afero.NewOsFs(), "mytool", "")
	require.NoError(t, err)
	assert.False(t, trusted)
}

// TestDiscoverProjectConfig covers the discovery walk directly.
func TestDiscoverProjectConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/repo/sub/deep", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/repo/.mytool.yaml", []byte("a: 1\n"), 0o644))

	assert.Equal(t, "/repo/.mytool.yaml", setup.DiscoverProjectConfig(fs, "mytool", "/repo/sub/deep"),
		"walks up from a nested dir")
	assert.Equal(t, "/repo/.mytool.yaml", setup.DiscoverProjectConfig(fs, "mytool", "/repo"),
		"found at the dir itself")
	assert.Empty(t, setup.DiscoverProjectConfig(fs, "other", "/repo/sub"),
		"a different tool name does not match")
	assert.Empty(t, setup.DiscoverProjectConfig(fs, "mytool", "/elsewhere"),
		"absent above the start dir")
	assert.Empty(t, setup.DiscoverProjectConfig(fs, "", "/repo"), "empty tool name")
	assert.Empty(t, setup.DiscoverProjectConfig(fs, "mytool", ""), "empty start dir")
}

// TestTrustProjectConfig_MissingFile surfaces the read error when hashing a file
// that does not exist.
func TestTrustProjectConfig_MissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := setup.TrustProjectConfig(afero.NewOsFs(), "mytool", filepath.Join(t.TempDir(), ".mytool.yaml"))
	require.Error(t, err)
}

// TestTrustProjectConfig_EmptyPath rejects an empty target.
func TestTrustProjectConfig_EmptyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	require.Error(t, setup.TrustProjectConfig(afero.NewOsFs(), "mytool", ""))
}

// TestUntrustProjectConfig_NotTrustedIsNoOp confirms revoking an untrusted (or
// empty) path is a no-op, not an error.
func TestUntrustProjectConfig_NotTrustedIsNoOp(t *testing.T) {
	fs, path := writeProjectFile(t, "log:\n  level: info\n")

	require.NoError(t, setup.UntrustProjectConfig(fs, "mytool", path), "never trusted -> no-op")
	require.NoError(t, setup.UntrustProjectConfig(fs, "mytool", ""), "empty path -> no-op")
}

// TestListTrustedProjects_Empty returns an empty list before anything is trusted.
func TestListTrustedProjects_Empty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	list, err := setup.ListTrustedProjects(afero.NewOsFs(), "mytool")
	require.NoError(t, err)
	assert.Empty(t, list)
}

// TestIsProjectConfigTrusted_MalformedStore surfaces a corrupt trust store as an
// error rather than a silent "trusted".
func TestIsProjectConfigTrusted_MalformedStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fs := afero.NewOsFs()
	// Trust a file first so the store exists, then corrupt the store.
	// writeProjectFile resets HOME to a fresh temp, so restore ours afterwards.
	_, path := writeProjectFile(t, "log:\n  level: info\n")
	t.Setenv("HOME", home)
	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

	storePath := filepath.Join(home, ".mytool", "trusted-projects.yaml")
	require.NoError(t, afero.WriteFile(fs, storePath, []byte("trusted: [not-a-map\n"), 0o600))

	_, err := setup.IsProjectConfigTrusted(fs, "mytool", path)
	require.Error(t, err, "a corrupt trust store must not silently resolve to trusted")
}
