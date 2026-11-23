package root

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0217 D3: a tool whose installer names its manager is left alone by the
// pre-run check, whatever the policy, and never reads the pin marker.
func TestCheckForUpdates_ManagedIsLeftAlone(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv(setup.InstallManagerEnvVariable, "mise")

	for _, policy := range []p.UpdatePolicy{p.UpdatePolicyEnabled, p.UpdatePolicyPrompt, p.UpdatePolicyDisabled} {
		t.Run(string(policy), func(t *testing.T) {
			props := outdatedProps(t, policy, promptIO("n"))
			require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool, "v2.0.0"))
			checked := filepath.Join(setup.ConfigDir(props.Tool), "last_checked")
			stale := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
			require.NoError(t, props.FS.Chtimes(checked, stale, stale))
			marker := filepath.Join(setup.ConfigDir(props.Tool), "pinned_version")
			require.NoError(t, afero.WriteFile(props.FS, marker, []byte("garbage"), 0o600))
			watched := &openRecorder{Fs: props.FS, path: marker}
			props.FS = watched

			result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

			require.NoError(t, result.Error, "a managed install wins over the enabled policy")
			assert.False(t, result.ShouldExit)
			assert.False(t, result.HasUpdated)
			info, err := props.FS.Stat(checked)
			require.NoError(t, err)
			assert.True(t, info.ModTime().Equal(stale), "no release probe")
			assert.False(t, loggedAt(t, props, logger.WarnLevel, "a newer"), "no cached reminder")
			assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced))
			assert.Zero(t, watched.opens, "the pin marker is never read")
			assert.True(t, loggedAt(t, props, logger.DebugLevel, "update check skipped: managed install"))
		})
	}
}

// openRecorder counts the opens of one path.
type openRecorder struct {
	afero.Fs
	path  string
	opens int
}

func (r *openRecorder) Open(name string) (afero.File, error) {
	r.count(name)

	return r.Fs.Open(name)
}

func (r *openRecorder) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	r.count(name)

	return r.Fs.OpenFile(name, flag, perm)
}

func (r *openRecorder) count(name string) {
	if filepath.Clean(name) == filepath.Clean(r.path) {
		r.opens++
	}
}
