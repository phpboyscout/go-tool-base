package generator

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The scaffold's copy is canonical and gtb's own release runs the same bytes
// (spec 0212 D5).
const (
	ownHomebrewScript      = "../../../scripts/push-homebrew-cask.sh"
	scaffoldHomebrewScript = "assets/skeleton/scripts/push-homebrew-cask.sh"
)

func TestHomebrewScripts_AreIdentical(t *testing.T) {
	t.Parallel()

	own, err := os.ReadFile(ownHomebrewScript)
	require.NoError(t, err)

	scaffold, err := os.ReadFile(scaffoldHomebrewScript)
	require.NoError(t, err)

	assert.True(t, bytes.Equal(own, scaffold), "scripts/push-homebrew-cask.sh and the scaffold's copy have drifted")
}

// The fixtures are fixed bash scripts that take their paths from the
// environment, so no command line is assembled from a variable.
const (
	seedTapScript = `set -e
git init --quiet --bare --initial-branch=main "$TAP"
git clone --quiet "$TAP" "$WORK"
cd "$WORK"
echo tap > README.md
git add README.md
git -c user.name=t -c user.email=t@t commit --quiet -m seed
git push --quiet origin HEAD:main`

	anotherReleaseScript = `set -e
git clone --quiet "$TAP" "$WORK"
cd "$WORK"
echo "another tool" > Casks.md
git add Casks.md
git -c user.name=t -c user.email=t@t commit --quiet -m "another release"`

	tapLogScript = `git -C "$TAP" log --format=%s main`
)

func fixtureEnv(t *testing.T, vars ...string) []string {
	t.Helper()

	return append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, vars...)
}

func tapLog(t *testing.T, tap string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "bash", "-c", tapLogScript)
	cmd.Env = fixtureEnv(t, "TAP="+tap)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return string(out)
}

// tapFixture is a bare tap with one commit on main, and a rendered cask in a
// scratch dist/.
func tapFixture(t *testing.T) (tap, dist string) {
	t.Helper()

	root := t.TempDir()
	tap = filepath.Join(root, "tap.git")

	cmd := exec.CommandContext(t.Context(), "bash", "-c", seedTapScript)
	cmd.Env = fixtureEnv(t, "TAP="+tap, "WORK="+filepath.Join(root, "seed"))

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	dist = filepath.Join(root, "dist")
	require.NoError(t, os.MkdirAll(filepath.Join(dist, "homebrew", "Casks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dist, "homebrew", "Casks", "hbtool.rb"), []byte("cask \"hbtool\" do\nend\n"), 0o600))

	return tap, dist
}

func runPushScript(t *testing.T, path, tap, dist string) (string, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "bash", scaffoldHomebrewScript, "hbtool", "acme/homebrew")
	cmd.Env = []string{"PATH=" + path, "HOME=" + t.TempDir(), "TAP_URL=" + tap, "DIST=" + dist, "TAG=v1.2.3"}

	out, err := cmd.CombinedOutput()

	return string(out), err
}

func TestHomebrewScript_PushesTheCaskOnce(t *testing.T) {
	t.Parallel()

	tap, dist := tapFixture(t)

	out, err := runPushScript(t, os.Getenv("PATH"), tap, dist)
	require.NoError(t, err, out)
	assert.Contains(t, out, "pushed Casks/hbtool.rb for v1.2.3")
	assert.Contains(t, tapLog(t, tap), "Brew cask update for hbtool version v1.2.3")

	out, err = runPushScript(t, os.Getenv("PATH"), tap, dist)
	require.NoError(t, err, out)
	assert.Contains(t, out, "nothing to push")
}

// Another release pushing to the tap between the script's clone and its push
// is replayed over, not fatal. A git wrapper on PATH advances the tap from a
// second clone just before the script's first push.
func TestHomebrewScript_RebasesWhenTheTapMoved(t *testing.T) {
	t.Parallel()

	tap, dist := tapFixture(t)

	other := filepath.Join(t.TempDir(), "other")
	cmd := exec.CommandContext(t.Context(), "bash", "-c", anotherReleaseScript)
	cmd.Env = fixtureEnv(t, "TAP="+tap, "WORK="+other)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	realGit, err := exec.LookPath("git")
	require.NoError(t, err)

	bin := t.TempDir()
	wrapper := "#!/usr/bin/env bash\n" +
		"if [ \"$1\" = push ] && [ ! -e " + filepath.Join(bin, "moved") + " ]; then\n" +
		"  touch " + filepath.Join(bin, "moved") + "\n" +
		"  " + realGit + " -C " + other + " push --quiet origin HEAD:main\n" +
		"fi\n" +
		"exec " + realGit + " \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o700))

	pushed, err := runPushScript(t, bin+string(os.PathListSeparator)+os.Getenv("PATH"), tap, dist)
	require.NoError(t, err, pushed)
	assert.Contains(t, pushed, "push attempt 1 was rejected")
	assert.Contains(t, pushed, "pushed Casks/hbtool.rb")

	log := tapLog(t, tap)
	assert.Contains(t, log, "another release")
	assert.Contains(t, log, "Brew cask update for hbtool version v1.2.3")
}
