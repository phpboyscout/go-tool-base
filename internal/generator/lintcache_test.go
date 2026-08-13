package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
