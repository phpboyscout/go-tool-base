package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestSkipUpdateCheck_FeatureBased_NotUseString: exemption is decided by the
// typed feature annotation, not the Use string — a renamed update command
// (with an args suffix, even) wrapped with UpdateCmd must still skip.
func TestSkipUpdateCheck_FeatureBased_NotUseString(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()

	cmd := Wrap(props.UpdateCmd, &cobra.Command{Use: "upgrade [target]"})

	assert.True(t, SkipUpdateCheck(memFS, "feat-tool", cmd.Command, 0),
		"an UpdateCmd-feature command must skip the update check regardless of its Use string")
}

// TestSkipUpdateCheck_InitFeature_SubtreeWalk: a subcommand under an
// init-feature parent (e.g. `init github`, wrapped with a provider feature)
// must skip via the parent walk.
func TestSkipUpdateCheck_InitFeature_SubtreeWalk(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()

	parent := Wrap(props.InitCmd, &cobra.Command{Use: "init"})
	child := Wrap("github", &cobra.Command{Use: "github"})
	parent.Register(child)

	assert.True(t, SkipUpdateCheck(memFS, "subtree-tool", child.Command, 0),
		"a subcommand of an InitCmd-feature parent must skip the update check")
}

// TestSkipUpdateCheck_DownstreamAuthName_NotSkipped: a downstream command that
// merely happens to be named "auth" (or "init") carries no exempting feature
// or annotation, so it must NOT skip — name matching no longer decides.
func TestSkipUpdateCheck_DownstreamAuthName_NotSkipped(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()

	for _, use := range []string{"auth", "init"} {
		cmd := &cobra.Command{Use: use}
		assert.False(t, SkipUpdateCheck(memFS, "collide-tool", cmd, 0),
			"a bare downstream command named %q must not be exempted by name", use)
	}
}

// TestSkipUpdateCheck_MarkAnnotation: the MarkSkipUpdateCheck stamp exempts a
// command, and — via the parent walk — its whole subtree.
func TestSkipUpdateCheck_MarkAnnotation(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()

	parent := Wrap("", &cobra.Command{Use: "mcp"})
	MarkSkipUpdateCheck(parent.Command)

	child := Wrap("", &cobra.Command{Use: "start"})
	parent.Register(child)

	assert.True(t, SkipsUpdateCheck(parent.Command), "stamped command must report the annotation")
	assert.False(t, SkipsUpdateCheck(child.Command), "SkipsUpdateCheck is self-only; subtree semantics live in SkipUpdateCheck")

	assert.True(t, SkipUpdateCheck(memFS, "mark-tool", parent.Command, 0))
	assert.True(t, SkipUpdateCheck(memFS, "mark-tool", child.Command, 0),
		"a stamped parent must cover its subtree")
}

// TestMarkSkipUpdateCheck_NilSafe mirrors SkipConfigCheck's nil tolerance.
func TestMarkSkipUpdateCheck_NilSafe(t *testing.T) {
	t.Parallel()

	assert.Nil(t, MarkSkipUpdateCheck(nil))
	assert.False(t, SkipsUpdateCheck(nil))
}

// TestSkipUpdateCheck_IntervalStillApplies: the throttle half is unchanged —
// a recent check within the interval skips, interval 0 checks every time.
func TestSkipUpdateCheck_IntervalStillApplies(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()
	cmd := &cobra.Command{Use: "run"}

	require.NoError(t, SetTimeSinceLast(memFS, "interval-tool", CheckedKey))

	assert.True(t, SkipUpdateCheck(memFS, "interval-tool", cmd, 1<<40),
		"a recent check within the interval must skip")
	assert.False(t, SkipUpdateCheck(memFS, "interval-tool", cmd, 0),
		"interval 0 means check on every invocation")
}
