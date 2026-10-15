package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/exectest"
	"gitlab.com/phpboyscout/go-tool-base/internal/pkgmanager"
)

// newManagedUpdater builds an updater whose running binary sits at bin, with a
// newer verified release on offer, so only the managed-install guard can stop
// it replacing the binary (gtb#129).
func newManagedUpdater(t *testing.T, bin string) *SelfUpdater {
	t.Helper()

	asset := forgetest.TarGzAsset(e2eToolName, e2eToolName, "new-binary")
	s, _ := newE2EUpdater(t, forgetest.New(forgetest.WithRelease("v1.1.0", asset, forgetest.ChecksumsAsset(false, asset))))

	require.NoError(t, afero.WriteFile(s.Fs, bin, []byte("old-binary"), 0o755))
	s.osExecutable = exectest.FakeExecutable(bin)
	s.execLookPath = exectest.FakeLookPath(bin)

	return s
}

func TestUpdate_RefusesAPackageManagedBinary(t *testing.T) {
	t.Parallel()

	dpkg := pkgmanager.New(pkgmanager.WithLookPath(exectest.FakeLookPath("/usr/bin/dpkg-query")),
		pkgmanager.WithCommand(exectest.EchoCommand(e2eToolName+": /usr/bin/"+e2eToolName))).Detect

	tests := []struct {
		name    string
		bin     string
		force   bool
		opts    []UpdaterOption
		upgrade string
	}{
		{name: "Homebrew cask", bin: "/opt/homebrew/Caskroom/" + e2eToolName + "/1.0.0/" + e2eToolName, upgrade: "brew upgrade --cask " + e2eToolName},
		{name: "Homebrew cask with --force", bin: "/opt/homebrew/Caskroom/" + e2eToolName + "/1.0.0/" + e2eToolName, force: true, upgrade: "brew upgrade --cask " + e2eToolName},
		{name: "Homebrew formula", bin: "/home/linuxbrew/.linuxbrew/Cellar/" + e2eToolName + "/1.0.0/bin/" + e2eToolName, upgrade: "brew upgrade " + e2eToolName},
		{name: "dpkg", bin: "/usr/bin/" + e2eToolName, opts: []UpdaterOption{withManagedInstallDetector(dpkg)}, upgrade: "apt-get install --only-upgrade " + e2eToolName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newManagedUpdater(t, tc.bin)
			s.force = tc.force

			for _, o := range tc.opts {
				o(s)
			}

			_, err := s.Update(t.Context())
			require.ErrorIs(t, err, ErrManagedInstall)
			assert.Contains(t, errors.FlattenHints(err), tc.upgrade)

			got, readErr := afero.ReadFile(s.Fs, tc.bin)
			require.NoError(t, readErr)
			assert.Equal(t, "old-binary", string(got), "a managed binary must not be replaced")
		})
	}
}

func TestUpdateFromFile_RefusesAPackageManagedBinary(t *testing.T) {
	t.Parallel()

	s := newManagedUpdater(t, "/opt/homebrew/Caskroom/"+e2eToolName+"/1.0.0/"+e2eToolName)

	_, err := s.UpdateFromFile(t.Context(), "/nonexistent/release.tar.gz")
	require.ErrorIs(t, err, ErrManagedInstall)
	assert.Contains(t, errors.FlattenHints(err), "brew upgrade --cask "+e2eToolName)
}
