package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/phpboyscout/go/errors"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Issue #15. golangci-lint caches findings against absolute paths and returns
// them from a later run in a different directory; under --fix it then writes to
// those paths. Run from a git worktree of a repo linted elsewhere, that means
// writing into the OTHER checkout — a user's working tree rewritten by a
// regeneration they ran in a worktree specifically to avoid touching it.

func TestCommandEnv_ScopesTheLintCacheToTheLintedDirectory(t *testing.T) {
	t.Setenv("GOLANGCI_LINT_CACHE", "")

	env := commandEnv("golangci-lint", t.TempDir())

	var got string

	for _, e := range env {
		if strings.HasPrefix(e, "GOLANGCI_LINT_CACHE=") {
			got = strings.TrimPrefix(e, "GOLANGCI_LINT_CACHE=")
		}
	}

	require.NotEmpty(t, got, "golangci-lint must not inherit a cache shared with other checkouts")
	assert.Contains(t, got, filepath.Join("gtb", "golangci-lint"))
}

func TestCommandEnv_DifferentDirectoriesGetDifferentCaches(t *testing.T) {
	t.Setenv("GOLANGCI_LINT_CACHE", "")

	// The property that fixes the bug: a worktree cannot inherit entries keyed
	// to the checkout it was made from.
	main, err := lintCacheDir("/repos/project")
	require.NoError(t, err)

	worktree, err := lintCacheDir("/tmp/scratch/worktree-of-project")
	require.NoError(t, err)

	assert.NotEqual(t, main, worktree)
}

func TestCommandEnv_TheSameDirectoryKeepsAWarmCache(t *testing.T) {
	t.Parallel()

	// Repeat runs in one place must still hit a warm cache, or every
	// regeneration pays for a cold analysis of the whole project.
	first, err := lintCacheDir("/repos/project")
	require.NoError(t, err)

	second, err := lintCacheDir("/repos/project")
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestCommandEnv_RespectsAnOperatorsOwnSetting(t *testing.T) {
	t.Setenv("GOLANGCI_LINT_CACHE", "/my/own/cache")

	env := commandEnv("golangci-lint", t.TempDir())

	count := 0

	for _, e := range env {
		if strings.HasPrefix(e, "GOLANGCI_LINT_CACHE=") {
			count++

			assert.Equal(t, "GOLANGCI_LINT_CACHE=/my/own/cache", e,
				"an explicit choice is the operator's to make")
		}
	}

	assert.Equal(t, 1, count, "and it must not be shadowed by a second entry")
}

func TestCommandEnv_LeavesOtherCommandsAlone(t *testing.T) {
	t.Setenv("GOLANGCI_LINT_CACHE", "")

	env := commandEnv("go", t.TempDir())

	assert.Equal(t, os.Environ(), env, "only golangci-lint needs this")
}

// The golangci-lint pass is the single most expensive step a generation takes —
// a full lint of a real Go project, on a cold findings cache because
// golangci-lint keys on absolute paths and every generation writes to a new
// directory. The e2e suite paid it 38 times per run and it is what pushed that
// suite past its 25-minute CI ceiling.
//
// These two guard the switch that turns it off, because a skip that silently
// stops skipping restores the cost without anything going red — the suite would
// simply get slow again, which is exactly how it got slow the first time.

func TestRunLintPass_SkippedWhenEnvSaysSo(t *testing.T) {
	t.Setenv(SkipLintEnv, "true")

	buf := logger.NewBuffer()
	g := &Generator{props: &props.Props{Logger: buf, FS: afero.NewOsFs()}}

	require.NoError(t, g.runLintPass(t.Context(), t.TempDir(), lintFix), "a skipped pass fails nothing")

	assert.True(t, buf.Contains("Skipping golangci-lint pass"),
		"the skip must say so, so a slow suite can be traced to it")
	assert.False(t, buf.Contains("Running golangci-lint"),
		"golangci-lint must not be announced, let alone run")
}

func TestRunLintPass_OnlyTheLiteralTrueSkips(t *testing.T) {
	// No t.Parallel: t.Setenv below is incompatible with it.
	//
	// Follows GTB_NON_INTERACTIVE's convention. Anything other than the literal
	// "true" means "not set" — so a stray "1" or "yes" in a CI file cannot
	// silently disable the pass for real users, which is the direction that
	// matters: failing to skip is slow, failing to lint is a changed artefact.
	for _, value := range []string{"1", "yes", "TRUE", "True", ""} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv(SkipLintEnv, value)

			buf := logger.NewBuffer()
			g := &Generator{props: &props.Props{Logger: buf, FS: afero.NewOsFs()}}

			// A directory with no Go module: golangci-lint exits non-zero, the
			// pass logs a Warn and reports it. What is asserted is that it TRIED.
			_ = g.runLintPass(t.Context(), t.TempDir(), lintFix)

			assert.True(t, buf.Contains("Running golangci-lint"),
				"%q is not the literal \"true\" and must not skip the pass", value)
		})
	}
}

// recordingRunner captures every command the generator runs and answers each
// from a script of (output, error) pairs.
type recordingRunner struct {
	calls  [][]string
	script []struct {
		out string
		err error
	}
}

func (r *recordingRunner) run(_ context.Context, _, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))

	if len(r.script) == 0 {
		return nil, nil
	}

	next := r.script[0]
	r.script = r.script[1:]

	return []byte(next.out), next.err
}

// TestRunLintPass_RegenerateVerifiesWithoutFix (deferred item 3 of the
// v0.43.0 round): on a v0.42.0 upgrade the regenerate's lint step rewrote
// pkg/cmd/greet/main.go, the file the author owns, because it ran with --fix.
// A regenerate verifies; only a fresh generate, where every file is the
// generator's, may fix.
func TestRunLintPass_RegenerateVerifiesWithoutFix(t *testing.T) {
	t.Setenv(SkipLintEnv, "")

	rec := &recordingRunner{}
	g := &Generator{props: &props.Props{Logger: logger.NewBuffer(), FS: afero.NewOsFs()}, runCommand: rec.run}

	require.NoError(t, g.runLintPass(t.Context(), t.TempDir(), lintVerify))
	require.NoError(t, g.runLintPass(t.Context(), t.TempDir(), lintFix))

	require.Len(t, rec.calls, 2)
	assert.Equal(t, []string{"golangci-lint", "run"}, rec.calls[0], "regenerate verifies only")
	assert.Equal(t, []string{"golangci-lint", "run", "--fix"}, rec.calls[1], "a fresh generate may fix")
}

// TestRunLintPass_RetriesOnceWhenAnotherLintHoldsTheLock (deferred item 5):
// two gtb runs at once collide on golangci-lint's lock and the second exited
// 3 with "verification step failed" for a reason that was not the project's.
// The lock message earns one retry; a second collision is reported for what
// it is.
func TestRunLintPass_RetriesOnceWhenAnotherLintHoldsTheLock(t *testing.T) {
	t.Setenv(SkipLintEnv, "")

	locked := errors.New("exit status 3")
	rec := &recordingRunner{}
	rec.script = append(rec.script,
		struct {
			out string
			err error
		}{"Error: parallel golangci-lint is running\n", locked},
		struct {
			out string
			err error
		}{"", nil},
	)

	buf := logger.NewBuffer()
	g := &Generator{props: &props.Props{Logger: buf, FS: afero.NewOsFs()}, runCommand: rec.run, lintRetryDelay: new(time.Duration)}

	require.NoError(t, g.runLintPass(t.Context(), t.TempDir(), lintVerify), "the retry succeeds")
	assert.Len(t, rec.calls, 2)
	assert.True(t, buf.Contains("another golangci-lint is running"), "the wait is announced")

	rec = &recordingRunner{}
	rec.script = append(rec.script,
		struct {
			out string
			err error
		}{"Error: parallel golangci-lint is running\n", locked},
		struct {
			out string
			err error
		}{"Error: parallel golangci-lint is running\n", locked},
	)
	g = &Generator{props: &props.Props{Logger: logger.NewBuffer(), FS: afero.NewOsFs()}, runCommand: rec.run, lintRetryDelay: new(time.Duration)}

	err := g.runLintPass(t.Context(), t.TempDir(), lintVerify)
	require.ErrorIs(t, err, ErrLintLocked, "a second collision is named, not a verification failure")
	assert.Len(t, rec.calls, 2, "one retry, not a loop")
}
