package repo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLocalRepo initialises a local repository at dir with a single commit on
// the named branch and returns it. It uses an explicit author signature so the
// commit does not depend on the developer's global git config (CI determinism).
func seedLocalRepo(t *testing.T, dir, branch string) *git.Repository {
	t.Helper()

	gitRepo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{
			DefaultBranch: plumbing.NewBranchReferenceName(branch),
		},
	})
	require.NoError(t, err)

	wt, err := gitRepo.Worktree()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("seed"), 0o644))
	_, err = wt.Add("README.md")
	require.NoError(t, err)

	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Seed", Email: "seed@example.test", When: time.Now()},
	})
	require.NoError(t, err)

	return gitRepo
}

// TestRepo_Unit_OpenInMemory clones a local source repository into in-memory
// storage with no network access, covering the previously-uncovered
// OpenInMemory path (and, via Open, the InMemoryRepo dispatch branch).
func TestRepo_Unit_OpenInMemory(t *testing.T) {
	t.Parallel()

	t.Run("clone_local_into_memory", func(t *testing.T) {
		t.Parallel()

		src := t.TempDir()
		seedLocalRepo(t, src, "main")

		r := &Repo{}
		gitRepo, tree, err := r.OpenInMemory(src, "")
		require.NoError(t, err)
		assert.NotNil(t, gitRepo)
		assert.NotNil(t, tree)
		assert.True(t, r.SourceIs(SourceMemory))
	})

	t.Run("clone_local_into_memory_with_branch_checkout", func(t *testing.T) {
		t.Parallel()

		src := t.TempDir()
		seedLocalRepo(t, src, "main")

		r := &Repo{}
		// Passing the existing default branch name exercises the post-clone
		// Checkout branch of OpenInMemory.
		gitRepo, tree, err := r.OpenInMemory(src, "main")
		require.NoError(t, err)
		assert.NotNil(t, gitRepo)
		assert.NotNil(t, tree)
	})

	t.Run("clone_with_config_storer", func(t *testing.T) {
		t.Parallel()

		src := t.TempDir()
		seedLocalRepo(t, src, "main")

		// Supplying a non-nil go-git config exercises the storer.SetConfig
		// branch of OpenInMemory.
		r := &Repo{}
		require.NoError(t, WithConfig(gitconfig.NewConfig())(r))

		gitRepo, tree, err := r.OpenInMemory(src, "")
		require.NoError(t, err)
		assert.NotNil(t, gitRepo)
		assert.NotNil(t, tree)
	})

	t.Run("checkout_missing_branch_errors", func(t *testing.T) {
		t.Parallel()

		src := t.TempDir()
		seedLocalRepo(t, src, "main")

		r := &Repo{}
		// A branch that does not exist on the source makes the post-clone
		// Checkout fail, exercising OpenInMemory's checkout-error path.
		_, _, err := r.OpenInMemory(src, "does-not-exist")
		require.Error(t, err)
	})

	t.Run("clone_error_invalid_source", func(t *testing.T) {
		t.Parallel()

		r := &Repo{}
		// A non-repository directory makes git.Clone fail, exercising the
		// clone-error path of OpenInMemory.
		_, _, err := r.OpenInMemory(t.TempDir(), "")
		require.Error(t, err)
	})
}

// TestRepo_Unit_Open_InMemoryDispatch proves Open routes the (case-insensitive)
// InMemoryRepo type to OpenInMemory.
func TestRepo_Unit_Open_InMemoryDispatch(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	seedLocalRepo(t, src, "main")

	r := &Repo{}
	gitRepo, tree, err := r.Open("InMemory", src, "")
	require.NoError(t, err)
	assert.NotNil(t, gitRepo)
	assert.NotNil(t, tree)
	assert.True(t, r.SourceIs(SourceMemory))
}

// TestThreadSafeRepo_OpenInMemory covers the previously-uncovered
// ThreadSafeRepo.OpenInMemory delegation.
func TestThreadSafeRepo_OpenInMemory(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	seedLocalRepo(t, src, "main")

	r := &ThreadSafeRepo{repo: &Repo{}}
	gitRepo, tree, err := r.OpenInMemory(src, "")
	require.NoError(t, err)
	assert.NotNil(t, gitRepo)
	assert.NotNil(t, tree)
}

// TestRepo_Unit_CreateBranch_GuardErrors covers the early ErrNoRepository /
// ErrNoWorktree guards in CreateBranch.
func TestRepo_Unit_CreateBranch_GuardErrors(t *testing.T) {
	t.Parallel()

	t.Run("no_repository", func(t *testing.T) {
		t.Parallel()

		r := &Repo{}
		err := r.CreateBranch("feat")
		require.ErrorIs(t, err, ErrNoRepository)
	})

	t.Run("no_worktree", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		gitRepo, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		r := &Repo{}
		r.SetRepo(gitRepo)
		// repo set, tree nil -> ErrNoWorktree.
		err = r.CreateBranch("feat")
		require.ErrorIs(t, err, ErrNoWorktree)
	})
}

// TestRepo_Unit_CreateBranch_LocalExistingPull exercises CreateBranch on an
// already-existing branch in a SourceLocal repo wired to a network-free local
// bare remote, so the post-checkout Pull (NoErrAlreadyUpToDate) path runs.
func TestRepo_Unit_CreateBranch_LocalExistingPull(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	originDir := filepath.Join(root, "origin")
	cloneDir := filepath.Join(root, "clone")

	// Seed an origin repo with one commit, then clone it locally so the clone
	// has a configured remote and a remote-tracking ref — required for Pull.
	originRepo := seedLocalRepo(t, originDir, "main")
	head, err := originRepo.Head()
	require.NoError(t, err)
	branchName := head.Name().Short()

	r := &Repo{}
	gitRepo, tree, err := r.Clone(originDir, cloneDir)
	require.NoError(t, err)
	r.SetSource(SourceLocal)
	require.NotNil(t, gitRepo)
	require.NotNil(t, tree)

	// Branch already exists and is up to date with origin: must succeed,
	// covering the branchExists==true + non-memory Pull (NoErrAlreadyUpToDate)
	// branch.
	require.NoError(t, r.CreateBranch(branchName))
}

// TestRepo_Unit_OpenLocal_InitErrorOnFile covers OpenLocal's init-failure path:
// PlainOpen fails (no repo) and PlainInitWithOptions also fails because the
// target path is a regular file, not a directory.
func TestRepo_Unit_OpenLocal_InitErrorOnFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "afile")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o644))

	r := &Repo{}
	_, _, err := r.OpenLocal(filePath, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize Git repository")
}

// TestRepo_Unit_Clone_Errors covers Clone's failure paths.
func TestRepo_Unit_Clone_Errors(t *testing.T) {
	t.Parallel()

	t.Run("clone_failure_invalid_source", func(t *testing.T) {
		t.Parallel()

		// An empty, non-repository source directory makes PlainClone fail.
		r := &Repo{}
		_, _, err := r.Clone(t.TempDir(), filepath.Join(t.TempDir(), "dst"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to clone repository")
	})

	t.Run("mkdir_failure_target_is_file", func(t *testing.T) {
		t.Parallel()

		// Make the target path's parent a file so os.MkdirAll fails.
		base := t.TempDir()
		fileAsDir := filepath.Join(base, "notadir")
		require.NoError(t, os.WriteFile(fileAsDir, []byte("x"), 0o644))

		r := &Repo{}
		_, _, err := r.Clone(t.TempDir(), filepath.Join(fileAsDir, "child"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create target directory")
	})
}

// TestRepo_Unit_HeadTree_Errors covers headTree's guard and HEAD-resolution
// error paths, observed through its callers.
func TestRepo_Unit_HeadTree_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no_repository", func(t *testing.T) {
		t.Parallel()

		r := &Repo{}
		err := r.WalkTree(func(*object.File) error { return nil })
		require.ErrorIs(t, err, ErrNoRepository)
	})

	t.Run("head_error_no_commits", func(t *testing.T) {
		t.Parallel()

		// A freshly-initialised repo has no commits, so Head() fails inside
		// headTree, covering the "failed to get HEAD reference" wrap.
		dir := t.TempDir()
		gitRepo, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		r := &Repo{}
		r.SetRepo(gitRepo)

		_, err = r.FileExists("anything")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get HEAD reference")
	})
}

// TestRepo_Unit_FileExists_NoRepo / DirectoryExists_NoRepo cover the
// headTree-error propagation in those readers.
func TestRepo_Unit_TreeReaders_NoRepo(t *testing.T) {
	t.Parallel()

	r := &Repo{}

	_, err := r.FileExists("x")
	require.ErrorIs(t, err, ErrNoRepository)

	_, err = r.DirectoryExists("x")
	require.ErrorIs(t, err, ErrNoRepository)

	_, err = r.GetFile("x")
	require.ErrorIs(t, err, ErrNoRepository)
}

// TestRepo_Unit_DirectoryExists_NotFound covers the branch where DirectoryExists
// walks the tree and finds no file under the requested path.
func TestRepo_Unit_DirectoryExists_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := &Repo{}
	_, wt, err := r.OpenLocal(dir, "main")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.txt"), []byte("x"), 0o644))
	_, err = wt.Add("top.txt")
	require.NoError(t, err)
	_, err = r.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.test", When: time.Now()},
	})
	require.NoError(t, err)

	exists, err := r.DirectoryExists("no-such-dir")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestRepo_Unit_AddToFS_MkdirError covers AddToFS's failure to create the parent
// directory in the destination filesystem.
func TestRepo_Unit_AddToFS_MkdirError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := &Repo{}
	_, wt, err := r.OpenLocal(dir, "main")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644))
	_, err = wt.Add("f.txt")
	require.NoError(t, err)
	_, err = r.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.test", When: time.Now()},
	})
	require.NoError(t, err)

	gitFile, err := r.GetFile("f.txt")
	require.NoError(t, err)

	// A read-only filesystem makes MkdirAll fail inside AddToFS.
	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	err = r.AddToFS(roFS, gitFile, "/sub/out.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create directory in afero")
}

// TestRepo_Unit_Checkout_NoWorktree covers the worktree guards on Checkout and
// CheckoutCommit.
func TestRepo_Unit_Checkout_NoWorktree(t *testing.T) {
	t.Parallel()

	r := &Repo{}
	require.ErrorIs(t, r.Checkout(plumbing.NewBranchReferenceName("x")), ErrNoWorktree)
	require.ErrorIs(t, r.CheckoutCommit(plumbing.ZeroHash), ErrNoWorktree)
}

// TestRepo_Unit_Push_Commit_Guards covers the no-repo / no-worktree guards on
// Push and Commit.
func TestRepo_Unit_Push_Commit_Guards(t *testing.T) {
	t.Parallel()

	r := &Repo{}
	require.ErrorIs(t, r.Push(nil), ErrNoRepository)

	_, err := r.Commit("msg", nil)
	require.ErrorIs(t, err, ErrNoWorktree)
}

// TestRepo_Unit_CreateRemote_Remote_Guards covers the no-repo guards.
func TestRepo_Unit_CreateRemote_Remote_Guards(t *testing.T) {
	t.Parallel()

	r := &Repo{}

	_, err := r.CreateRemote("origin", []string{"https://example.test/x.git"})
	require.ErrorIs(t, err, ErrNoRepository)

	_, err = r.Remote("origin")
	require.ErrorIs(t, err, ErrNoRepository)
}
