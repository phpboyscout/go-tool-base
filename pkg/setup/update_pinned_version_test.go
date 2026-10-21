package setup_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestPinnedVersion_RoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const name = "pinned-version-tool"

	pinned, err := setup.GetPinnedVersion(fs, name)
	require.NoError(t, err)
	assert.Empty(t, pinned, "a tool starts unpinned")

	require.NoError(t, setup.SetPinnedVersion(fs, name, "v1.2.0-rc.1"))

	pinned, err = setup.GetPinnedVersion(fs, name)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0-rc.1", pinned)

	info, err := fs.Stat(filepath.Join(setup.GetDefaultConfigDir(fs, name), "pinned_version"))
	require.NoError(t, err)
	assert.Equal(t, "-rw-------", info.Mode().Perm().String(), "the marker is owner-only, as last_checked is")

	require.NoError(t, setup.SetPinnedVersion(fs, name, ""))

	pinned, err = setup.GetPinnedVersion(fs, name)
	require.NoError(t, err)
	assert.Empty(t, pinned, "an empty version clears the pin")

	require.NoError(t, setup.SetPinnedVersion(fs, name, ""), "clearing an absent pin is not an error")
}

func TestPinnedVersion_RefusesAVersionThatIsNotATag(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const name = "pinned-invalid-tool"

	require.ErrorIs(t, setup.SetPinnedVersion(fs, name, "v1.2"), setup.ErrInvalidPinnedVersion)

	pinned, err := setup.GetPinnedVersion(fs, name)
	require.NoError(t, err)
	assert.Empty(t, pinned, "a refused pin writes nothing")
}

func TestPinnedVersion_CorruptMarkerReadsAsUnpinned(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const name = "pinned-corrupt-tool"
	marker := filepath.Join(setup.GetDefaultConfigDir(fs, name), "pinned_version")
	require.NoError(t, afero.WriteFile(fs, marker, []byte("not a version\n"), 0o600))

	pinned, err := setup.GetPinnedVersion(fs, name)

	require.ErrorIs(t, err, setup.ErrInvalidPinnedVersion)
	assert.Contains(t, err.Error(), marker, "the error names the file")
	assert.Empty(t, pinned)
}

func TestPinnedVersion_NoHomeIsANoOp(t *testing.T) {
	t.Setenv("HOME", "")

	fs := afero.NewMemMapFs()
	const name = "pinned-nohome-tool"

	require.NoError(t, setup.SetPinnedVersion(fs, name, "v1.0.0"))

	pinned, err := setup.GetPinnedVersion(fs, name)
	require.NoError(t, err)
	assert.Empty(t, pinned)

	entries, err := afero.ReadDir(fs, ".")
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing lands in the working directory")
}
