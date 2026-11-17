package root

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// checkoutHome makes a home directory that is the root of a git checkout and
// writes contents as the tool's user config file in it, returning both paths.
func checkoutHome(t *testing.T, fs afero.Fs, contents string, isCheckout bool) (home, userFile string) {
	t.Helper()

	home = t.TempDir()
	t.Setenv("HOME", home)

	if isCheckout {
		require.NoError(t, fs.MkdirAll(filepath.Join(home, ".git"), 0o700))
	}

	userFile = filepath.Join(home, ".mytool", "config.yaml")
	require.NoError(t, fs.MkdirAll(filepath.Dir(userFile), 0o700))
	require.NoError(t, afero.WriteFile(fs, userFile, []byte(contents), 0o600))

	return home, userFile
}

func buildUserStore(t *testing.T, fs afero.Fs, log logger.Logger, userFile, projectPath string) *config.Store {
	t.Helper()

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:             &p.Props{Tool: p.Tool{Name: "mytool"}, Logger: log, FS: fs},
		CfgPaths:          []string{userFile},
		ProjectConfigPath: projectPath,
	})
	require.NoError(t, err)

	return store
}

func TestCheckoutHome_UserFileIsFiltered(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, hostileProjectConfig, true)

	log := logger.NewBuffer()
	view := buildUserStore(t, fs, log, userFile, "").View()

	assert.False(t, view.IsSet("update.require_signature"), "a home file in a checkout must not set require_signature")
	assert.False(t, view.IsSet("update.policy"))
	assert.False(t, view.GetBool("telemetry.enabled"))
	assert.Empty(t, view.GetString("github.auth.value"))
	assert.Equal(t, "debug", view.GetString("log.level"), "workflow keys still apply")
	assert.True(t, log.Contains("inside a git checkout"), "the ignored keys are warned about")
}

func TestCheckoutHome_UserFileOutsideACheckoutApplies(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, hostileProjectConfig, false)

	view := buildUserStore(t, fs, logger.NewNoop(), userFile, "").View()

	assert.True(t, view.IsSet("update.require_signature"))
	assert.False(t, view.GetBool("update.require_signature"), "the user's own file is honoured outside a checkout")
}

func TestCheckoutHome_FilteredUserFileStaysWritable(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, hostileProjectConfig, true)

	store := buildUserStore(t, fs, logger.NewNoop(), userFile, "")

	_, err := store.Apply(t.Context(), config.Set("log.level", "warn"))
	require.NoError(t, err)

	written, err := afero.ReadFile(fs, userFile)
	require.NoError(t, err)
	assert.Contains(t, string(written), "level: warn", "the write lands in the user file")
	assert.Contains(t, string(written), "require_signature: false", "a write keeps the keys the filter ignored")
}

func TestCheckoutHome_ShippedTrustRecordIsIgnored(t *testing.T) {
	fs := afero.NewOsFs()
	home, userFile := checkoutHome(t, fs, "log:\n  level: info\n", true)

	project := filepath.Join(t.TempDir(), ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, project, []byte(hostileProjectConfig), 0o600))

	sum := sha256.Sum256([]byte(hostileProjectConfig))
	record := "trusted:\n  " + project + ": " + hex.EncodeToString(sum[:]) + "\n"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(home, ".mytool", "trusted-projects.yaml"), []byte(record), 0o600))

	view := buildUserStore(t, fs, logger.NewNoop(), userFile, project).View()

	assert.False(t, view.IsSet("update.require_signature"),
		"a trust record inside a checkout must not trust the project file")
}

func TestCheckoutHome_NamedFileOutsideTheConfigDirIsNotFiltered(t *testing.T) {
	fs := afero.NewOsFs()
	home, _ := checkoutHome(t, fs, "log:\n  level: info\n", true)

	named := filepath.Join(home, "named.yaml")
	require.NoError(t, afero.WriteFile(fs, named, []byte(hostileProjectConfig), 0o600))

	view := buildUserStore(t, fs, logger.NewNoop(), named, "").View()

	assert.True(t, view.IsSet("update.require_signature"),
		"a file named with --config outside the config directory is the caller's choice")
}

func TestCheckoutHome_HomeReachedThroughASymlinkIsFiltered(t *testing.T) {
	fs := afero.NewOsFs()

	checkout := t.TempDir()
	require.NoError(t, fs.MkdirAll(filepath.Join(checkout, ".git"), 0o700))
	target := filepath.Join(checkout, "ci-home")
	require.NoError(t, fs.MkdirAll(filepath.Join(target, ".mytool"), 0o700))

	home := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Symlink(target, home))
	t.Setenv("HOME", home)

	userFile := filepath.Join(home, ".mytool", "config.yaml")
	require.NoError(t, afero.WriteFile(fs, userFile, []byte(hostileProjectConfig), 0o600))

	view := buildUserStore(t, fs, logger.NewNoop(), userFile, "").View()

	assert.False(t, view.IsSet("update.require_signature"),
		"a home that resolves into a checkout is filtered")
}
