package root

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/pkgmanager"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// fakeDetector answers as a package manager would and records what it was asked.
type fakeDetector struct {
	owned bool
	asked []string
}

func (d *fakeDetector) Detect(_ context.Context, exe string) (pkgmanager.Install, bool) {
	d.asked = append(d.asked, exe)

	return pkgmanager.Install{Manager: pkgmanager.Dpkg, Package: "tool"}, d.owned
}

// Spec 0097 D7: an install a package manager owns is left to that manager.
func TestCheckForUpdates_PackagedIsLeftAlone(t *testing.T) {
	t.Setenv("CI", "")

	for _, policy := range []p.UpdatePolicy{p.UpdatePolicyEnabled, p.UpdatePolicyPrompt, p.UpdatePolicyDisabled} {
		for name, tt := range map[string]struct {
			io     p.IO
			cached bool
			asked  int
		}{
			"attended":                     {io: promptIO("n"), asked: 1},
			"attended, known to be behind": {io: promptIO("n"), cached: true, asked: 1},
			"unattended":                   {io: nonInteractiveIO()},
		} {
			t.Run(string(policy)+" "+name, func(t *testing.T) {
				props := outdatedProps(t, policy, tt.io)
				marker := filepath.Join(setup.ConfigDir(props.Tool), "pinned_version")
				require.NoError(t, afero.WriteFile(props.FS, marker, []byte("garbage"), 0o600))

				stale := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
				checked := filepath.Join(setup.ConfigDir(props.Tool), "last_checked")

				if tt.cached {
					require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool, "v2.0.0"))
					require.NoError(t, props.FS.Chtimes(checked, stale, stale))
				}

				if tt.asked == 0 && policy == p.UpdatePolicyEnabled {
					tt.asked = 1
				}

				detector := &fakeDetector{owned: true}
				state := newRootState()
				state.detector = detector

				result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

				require.NoError(t, result.Error)
				assert.False(t, result.ShouldExit)
				assert.False(t, result.HasUpdated)
				assert.False(t, loggedAt(t, props, logger.WarnLevel, ""), "nothing is said to the user")
				assert.Len(t, detector.asked, tt.asked)
				assert.Equal(t, tt.asked == 1, loggedAt(t, props, logger.DebugLevel, "update check skipped: package manager"))

				if tt.cached {
					info, err := props.FS.Stat(checked)
					require.NoError(t, err)
					assert.True(t, info.ModTime().Equal(stale), "no release probe")

					return
				}

				assert.False(t, checkRan(props), "no release probe")
			})
		}
	}
}

// The detector may run dpkg-query and rpm, so a run with nothing to say or do
// does not ask it.
func TestCheckForUpdates_DetectorIsAskedOnlyWhenItMatters(t *testing.T) {
	t.Setenv("CI", "")

	exe, err := os.Executable()
	require.NoError(t, err)

	for name, tt := range map[string]struct {
		policy  p.UpdatePolicy
		io      p.IO
		checked bool
		version string
		pinned  bool
		env     string
		asked   int
	}{
		"throttled, up to date":             {policy: p.UpdatePolicyEnabled, io: promptIO("n"), checked: true},
		"throttled, cached is this version": {policy: p.UpdatePolicyPrompt, io: promptIO("n"), checked: true, version: "v1.0.0"},
		"throttled, known to be behind":     {policy: p.UpdatePolicyPrompt, io: promptIO("n"), checked: true, version: "v2.0.0", asked: 1},
		"pinned":                            {policy: p.UpdatePolicyEnabled, io: promptIO("n"), pinned: true},
		"named by its environment":          {policy: p.UpdatePolicyEnabled, io: promptIO("n"), env: "mise"},
		"unattended, no policy to enforce":  {policy: p.UpdatePolicyPrompt, io: nonInteractiveIO()},
		"unattended, a policy not enforced": {policy: p.UpdatePolicyEnabled, io: nonInteractiveIO(), asked: 1},
		"a reminder and a probe in one run": {policy: p.UpdatePolicyDisabled, io: promptIO("n"), version: "v2.0.0", asked: 1},
		"a probe with nothing recorded":     {policy: p.UpdatePolicyDisabled, io: promptIO("n"), asked: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(setup.InstallManagerEnvVariable, tt.env)

			props := outdatedProps(t, tt.policy, tt.io)

			if tt.checked || tt.version != "" {
				require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool, tt.version))
			}

			if !tt.checked && tt.version != "" {
				stale := time.Now().Add(-30 * 24 * time.Hour)
				require.NoError(t, props.FS.Chtimes(filepath.Join(setup.ConfigDir(props.Tool), "last_checked"), stale, stale))
			}

			if tt.pinned {
				require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool, "v1.0.0"))
			}

			detector := &fakeDetector{}
			state := newRootState()
			state.detector = detector

			checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

			require.Len(t, detector.asked, tt.asked)

			for _, asked := range detector.asked {
				assert.Equal(t, exe, asked)
			}
		})
	}
}

// Nothing changes for an install no package manager owns.
func TestCheckForUpdates_UnpackagedIsChecked(t *testing.T) {
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyEnabled, promptIO("n"))
	state := newRootState()
	state.detector = &fakeDetector{}

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, state)

	require.Error(t, result.Error, "the enabled policy still blocks")
	assert.True(t, checkRan(props))
}

const cellarEnv = "GTB_TEST_RUNS_FROM_A_CELLAR"

// A root with no detector set uses the real one, so this runs a copy of the
// test binary from a path Homebrew would have installed it to.
func TestPackageManaged_UsesTheRealDetector(t *testing.T) {
	if os.Getenv(cellarEnv) != "" {
		props := outdatedProps(t, p.UpdatePolicyDisabled, promptIO("n"))
		require.True(t, newRootState().packageManaged(t.Context(), props))
		t.Log(cellarEnv)

		return
	}

	exe, err := os.Executable()
	require.NoError(t, err)

	binary, err := os.ReadFile(exe)
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "Cellar", "tool", "1.0.0")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	require.NoError(t, root.WriteFile("tool", binary, 0o700))

	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exec ./tool -test.v -test.run='^TestPackageManaged_UsesTheRealDetector$'")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), cellarEnv+"=1")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	// A pattern that matches no test exits zero too.
	require.Contains(t, string(out), cellarEnv)
}
