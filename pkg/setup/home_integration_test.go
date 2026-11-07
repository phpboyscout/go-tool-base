//go:build linux

package setup_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

const (
	homeProbeEnv = "GTB_TEST_HOME_PROBE"
	probeID      = 54321

	// Paths as the probe sees them, inside its chroot.
	probeBinary   = "/probe"
	envHomeDir    = "/env-home"
	accountDir    = "/account-home"
	missingDir    = "/gone"
	plainFile     = "/a-file"
	relativeHome  = "."
	noAccount     = ""
	unsetHomeName = "<unset>"
)

// TestHomeProbe is not a test of its own: the matrix below runs it, compiled
// without cgo, inside a chroot whose account database it controls.
func TestHomeProbe(t *testing.T) {
	if os.Getenv(homeProbeEnv) == "" {
		t.Skip("run by TestUserHomeDir_Matrix")
	}

	fmt.Printf("resolved=%s\n", setup.UserHomeDir())
}

// TestUserHomeDir_Matrix crosses what HOME says with what the account says, in a
// build like a release's, for the cases a unit test cannot reach: an account with
// no usable home, or none.
func TestUserHomeDir_Matrix(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "home")

	if os.Getuid() != 0 {
		// A skip in CI would turn the job green with nothing tested.
		if os.Getenv("CI") != "" {
			t.Fatal("needs root, to chroot")
		}

		t.Skip("needs root, to chroot")
	}

	root := newProbeRoot(t)

	accounts := map[string]string{
		"usable":   accountDir,
		"missing":  missingDir,
		"a file":   plainFile,
		"relative": relativeHome,
		"absent":   noAccount,
	}

	for _, home := range []string{unsetHomeName, "", relativeHome, "sub", "~", missingDir, plainFile} {
		for name, dir := range accounts {
			want := ""
			if name == "usable" {
				want = accountDir
			}

			t.Run(fmt.Sprintf("HOME=%s account=%s", home, name), func(t *testing.T) {
				assert.Equal(t, want, probeHome(t, root, home, dir))
			})
		}
	}

	for name, dir := range accounts {
		t.Run("an existing HOME wins over account="+name, func(t *testing.T) {
			assert.Equal(t, envHomeDir, probeHome(t, root, envHomeDir, dir))
		})
	}
}

func newProbeRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	for _, dir := range []string{"etc", envHomeDir, accountDir} {
		require.NoError(t, os.Mkdir(filepath.Join(root, dir), 0o750))
	}

	require.NoError(t, os.WriteFile(filepath.Join(root, plainFile), nil, 0o600))
	require.NoError(t, os.WriteFile(accountsPath(root), nil, 0o600))

	//nolint:gosec // G204: compiles this package to a path under the test's own temp directory
	build := exec.Command("go", "test", "-c", "-o", filepath.Join(root, probeBinary), ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	require.NoError(t, os.Chown(root, probeID, probeID))

	for _, path := range []string{"etc", "etc/passwd", envHomeDir, accountDir, plainFile, probeBinary} {
		require.NoError(t, os.Chown(filepath.Join(root, path), probeID, probeID))
	}

	return root
}

func accountsPath(root string) string {
	return filepath.Join(root, "etc", "passwd")
}

func probeHome(t *testing.T, root, home, dir string) string {
	t.Helper()

	entries := "root:x:0:0::/root:/bin/sh\n"
	if dir != noAccount {
		entries += fmt.Sprintf("probe:x:%d:%d::%s:/bin/sh\n", probeID, probeID, dir)
	}

	require.NoError(t, os.WriteFile(accountsPath(root), []byte(entries), 0o600))

	cmd := exec.Command(probeBinary, "-test.run=^TestHomeProbe$")
	cmd.Dir = "/"
	// A chroot because only the account database needs swapping, and default
	// Docker settings refuse the user namespace a rootless sandbox would need.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot:     root,
		Credential: &syscall.Credential{Uid: probeID, Gid: probeID},
	}
	cmd.Env = []string{homeProbeEnv + "=1"}

	if home != unsetHomeName {
		cmd.Env = append(cmd.Env, "HOME="+home)
	}

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	for line := range strings.SplitSeq(string(out), "\n") {
		if resolved, ok := strings.CutPrefix(line, "resolved="); ok {
			return resolved
		}
	}

	require.Fail(t, "the probe reported nothing", string(out))

	return ""
}
