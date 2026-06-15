package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/errors"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverRepository(t *testing.T) {
	t.Parallel()

	t.Run("not_a_repo", func(t *testing.T) {
		t.Parallel()

		found, err := DiscoverRepository(t.TempDir())
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("is_a_repo", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		found, err := DiscoverRepository(dir)
		require.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("subdir_of_repo_walks_upward", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		sub := filepath.Join(dir, "nested", "deeper")
		require.NoError(t, os.MkdirAll(sub, 0o755))

		found, err := DiscoverRepository(sub)
		require.NoError(t, err)
		assert.True(t, found, "a subdirectory of a repo must be detected as already-a-repo")
	})
}

func TestRepo_InitLocal(t *testing.T) {
	t.Parallel()

	t.Run("initialises_fresh_repo_on_branch", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		r := &Repo{}

		gitRepo, tree, err := r.InitLocal(dir, "main")
		require.NoError(t, err)
		require.NotNil(t, gitRepo)
		require.NotNil(t, tree)

		// The default branch reference exists even before the first commit
		// (HEAD points at refs/heads/main symbolically).
		head, err := gitRepo.Reference(plumbing.HEAD, false)
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/main", head.Target().String())
	})

	t.Run("defaults_branch_to_main", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		r := &Repo{}

		gitRepo, _, err := r.InitLocal(dir, "")
		require.NoError(t, err)

		head, err := gitRepo.Reference(plumbing.HEAD, false)
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/main", head.Target().String())
	})

	t.Run("custom_branch", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		r := &Repo{}

		gitRepo, _, err := r.InitLocal(dir, "trunk")
		require.NoError(t, err)

		head, err := gitRepo.Reference(plumbing.HEAD, false)
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/trunk", head.Target().String())
	})

	t.Run("refuses_existing_repo", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		r := &Repo{}
		_, _, err = r.InitLocal(dir, "main")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrAlreadyRepository), "expected ErrAlreadyRepository, got %v", err)
	})

	t.Run("init_error_when_path_is_a_file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		filePath := filepath.Join(dir, "afile")
		require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

		r := &Repo{}
		_, _, err := r.InitLocal(filePath, "main")
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrAlreadyRepository), "a plain file is not a repository")
	})

	t.Run("refuses_subdir_of_existing_repo", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		sub := filepath.Join(dir, "child")
		require.NoError(t, os.MkdirAll(sub, 0o755))

		r := &Repo{}
		_, _, err = r.InitLocal(sub, "main")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrAlreadyRepository))
	})
}

func TestRepo_AddAll_HonoursGitignore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := &Repo{}

	_, _, err := r.InitLocal(dir, "main")
	require.NoError(t, err)

	// A .gitignore that excludes bin/ and *.log.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("bin/\n*.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.log"), []byte("noise\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin", "tool"), []byte("binary\n"), 0o644))

	require.NoError(t, r.AddAll())

	hash, err := r.Commit("chore: initial", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.test"},
	})
	require.NoError(t, err)
	assert.False(t, hash.IsZero())

	committed := committedPaths(t, r)
	assert.Contains(t, committed, ".gitignore", ".gitignore must itself be committed")
	assert.Contains(t, committed, "main.go")
	assert.NotContains(t, committed, "app.log", "ignored *.log must not be committed")
	assert.NotContains(t, committed, "bin/tool", "ignored bin/ contents must not be committed")
}

func TestRepo_AddAll_StagingError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := &Repo{}

	_, _, err := r.InitLocal(dir, "main")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.go"), []byte("package f\n"), 0o644))
	// Corrupt the git index so AddWithOptions fails, exercising the error wrap.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("not-an-index"), 0o644))

	err = r.AddAll()
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNoWorktree))
}

func TestRepo_AddAll_NoWorktree(t *testing.T) {
	t.Parallel()

	r := &Repo{}
	err := r.AddAll()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoWorktree))
}

func TestThreadSafeRepo_InitLocalAndAddAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := &ThreadSafeRepo{repo: &Repo{}}

	gitRepo, tree, err := r.InitLocal(dir, "main")
	require.NoError(t, err)
	require.NotNil(t, gitRepo)
	require.NotNil(t, tree)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.log"), []byte("x\n"), 0o644))

	require.NoError(t, r.AddAll())

	_, err = r.Commit("chore: init", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.test"},
	})
	require.NoError(t, err)

	committed := committedPaths(t, r.repo)
	assert.Contains(t, committed, "main.go")
	assert.NotContains(t, committed, "skip.log")
}

func TestThreadSafeRepo_InitLocalRefusesExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	r := &ThreadSafeRepo{repo: &Repo{}}
	_, _, err = r.InitLocal(dir, "main")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyRepository))
}

// committedPaths returns the set of file paths in the repo's HEAD tree.
func committedPaths(t *testing.T, r *Repo) map[string]struct{} {
	t.Helper()

	paths := map[string]struct{}{}
	require.NoError(t, r.WalkTree(func(f *object.File) error {
		paths[f.Name] = struct{}{}

		return nil
	}))

	return paths
}
