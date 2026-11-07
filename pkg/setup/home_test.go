package setup

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
)

var errNoSource = errors.New("no source")

func source(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func failingSource() (string, error) { return "", errNoSource }

func TestFirstUsableHome(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()
	file := filepath.Join(first, "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	gone := filepath.Join(first, "gone")

	for _, tc := range []struct {
		name    string
		sources []func() (string, error)
		want    string
	}{
		{"the first source wins when usable", []func() (string, error){source(first), source(second)}, first},
		{"a failing source falls through", []func() (string, error){failingSource, source(second)}, second},
		{"an empty path falls through", []func() (string, error){source(""), source(second)}, second},
		{"a relative path falls through", []func() (string, error){source("."), source(second)}, second},
		{"a subdirectory path falls through", []func() (string, error){source("sub"), source(second)}, second},
		{"a literal tilde falls through", []func() (string, error){source("~"), source(second)}, second},
		{"a missing directory falls through", []func() (string, error){source(gone), source(second)}, second},
		{"a file falls through", []func() (string, error){source(file), source(second)}, second},
		{"no source is usable", []func() (string, error){failingSource, source("."), source(gone)}, ""},
		{"no sources at all", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, firstUsableHome(tc.sources...))
		})
	}
}

func TestUserHomeDir_PrefersAnExistingHomeVariable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	assert.Equal(t, home, UserHomeDir())
}

// HOME is no authority on whether the user has a home: the account still knows.
func TestUserHomeDir_FallsBackToTheAccount(t *testing.T) {
	account, err := user.Current()
	if err != nil || !usableHome(account.HomeDir) {
		t.Skip("this account has no usable home of its own")
	}

	for _, home := range []string{"", ".", "sub", "~", filepath.Join(t.TempDir(), "gone")} {
		t.Run("HOME="+home, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)

			assert.Equal(t, account.HomeDir, UserHomeDir())
		})
	}
}

func TestPinnedVersion_NoHomeIsANoOp(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	require.NoError(t, setPinnedVersionIn(fs, "", "v1.0.0"))

	pinned, err := getPinnedVersionIn(fs, "")
	require.NoError(t, err)
	assert.Empty(t, pinned)

	entries, err := afero.ReadDir(fs, ".")
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing lands in the working directory")
}

// A path joined onto the empty directory would resolve against the working
// directory, which anyone can write to.
func TestConfigPathsUnder_NoHomeHasNoUserFile(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{filepath.Join(string(filepath.Separator), "etc", "mytool", "config.yaml")},
		configPathsUnder("", "mytool", "config.yaml"))
}

func TestConfigPathsUnder_HomeAddsTheUserFile(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(string(filepath.Separator), "somewhere", ".mytool")

	assert.Equal(t,
		[]string{
			filepath.Join(string(filepath.Separator), "etc", "mytool", "config.yaml"),
			filepath.Join(dir, "config.yaml"),
		},
		configPathsUnder(dir, "mytool", "config.yaml"))
}

func TestTrustStorePathUnder_NoHomeHasNoStore(t *testing.T) {
	t.Parallel()

	assert.Empty(t, trustStorePathUnder(""))
}
