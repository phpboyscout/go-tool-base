package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	g.runLintPass(t.Context(), t.TempDir())

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
			// pass logs a Warn and returns. What is asserted is that it TRIED.
			g.runLintPass(t.Context(), t.TempDir())

			assert.True(t, buf.Contains("Running golangci-lint"),
				"%q is not the literal \"true\" and must not skip the pass", value)
		})
	}
}
