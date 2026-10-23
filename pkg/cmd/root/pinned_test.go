package root

import (
	"context"
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

// Spec 0104 D6: a pinned tool is left alone by the pre-run check, whatever
// the author's policy and whoever is at the terminal.
func TestCheckForUpdates_PinnedIsLeftAlone(t *testing.T) {
	t.Setenv("CI", "")

	for _, policy := range []p.UpdatePolicy{p.UpdatePolicyEnabled, p.UpdatePolicyPrompt, p.UpdatePolicyDisabled} {
		for _, tt := range []struct {
			name string
			io   func() p.IO
		}{
			{name: "attended", io: func() p.IO { return promptIO("n") }},
			{name: "unattended", io: nonInteractiveIO},
		} {
			t.Run(string(policy)+"/"+tt.name, func(t *testing.T) {
				props := outdatedProps(t, policy, tt.io())
				require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool.Name, "v2.0.0"))
				require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.0.0"))
				checked := filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), "last_checked")
				stale := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
				require.NoError(t, props.FS.Chtimes(checked, stale, stale))

				result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

				require.NoError(t, result.Error, "a pin wins over the enabled policy")
				assert.False(t, result.ShouldExit)
				assert.False(t, result.HasUpdated)
				info, err := props.FS.Stat(checked)
				require.NoError(t, err)
				assert.True(t, info.ModTime().Equal(stale), "a pinned run makes no release probe and leaves last_checked alone")
				assert.False(t, loggedAt(t, props, logger.WarnLevel, "a newer"), "no cached reminder")
				assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced))
				assert.True(t, loggedAt(t, props, logger.DebugLevel, "update check skipped: pinned"))
			})
		}
	}
}

func TestCheckForUpdates_CorruptPinIsIgnoredWithOneWarning(t *testing.T) {
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyPrompt, promptIO("n"))
	marker := filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), "pinned_version")
	require.NoError(t, props.FS.MkdirAll(filepath.Dir(marker), 0o700))
	require.NoError(t, afero.WriteFile(props.FS, marker, []byte("garbage"), 0o600))

	checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	assert.True(t, loggedAt(t, props, logger.WarnLevel, marker), "the warning names the file")
	assert.True(t, checkRan(props), "a corrupt pin reads as unpinned, so the check runs")
}

func TestCheckForUpdates_CorruptPinIsQuietWhenNoCheckRuns(t *testing.T) {
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyPrompt, nonInteractiveIO())
	marker := filepath.Join(setup.GetDefaultConfigDir(props.FS, props.Tool.Name), "pinned_version")
	require.NoError(t, props.FS.MkdirAll(filepath.Dir(marker), 0o700))
	require.NoError(t, afero.WriteFile(props.FS, marker, []byte("garbage"), 0o600))

	checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	assert.False(t, loggedAt(t, props, logger.WarnLevel, "update pin"), "an unattended run that makes no check says nothing")
}
