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
