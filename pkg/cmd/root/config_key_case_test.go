package root

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
)

// go/config spec 0015 D8: two keys in one file that differ only in case used
// to load with a random winner. The tool now stops, naming the file.
func TestBuildConfigStore_RefusesAFileWhoseKeysCollideInCase(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.yaml",
		[]byte("Log:\n  Level: warn\nlog:\n  level: debug\n"), 0o600))

	_, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    tomlLinkedProps(t, fs, nil),
		CfgPaths: []string{"/etc/mytool/config.yaml"},
	})
	require.ErrorIs(t, err, config.ErrKeyCollision)
	assert.Contains(t, err.Error(), "/etc/mytool/config.yaml")
}

// go/config spec 0015 D9: setting an existing key spelled differently edits
// the file's key instead of adding a second one beside it.
func TestBuildConfigStore_ASetKeepsTheFilesSpelling(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/etc/mytool/config.toml", []byte("[Log]\nLevel = \"warn\"\n"), 0o600))

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    tomlLinkedProps(t, fs, nil),
		CfgPaths: []string{"/etc/mytool/config.toml"},
	})
	require.NoError(t, err)

	_, err = store.Apply(t.Context(), config.Set("log.level", "debug"))
	require.NoError(t, err)

	written, err := afero.ReadFile(fs, "/etc/mytool/config.toml")
	require.NoError(t, err)
	assert.Equal(t, "[Log]\nLevel = \"debug\"\n", string(written))
}
