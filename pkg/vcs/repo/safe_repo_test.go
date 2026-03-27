package repo

import (
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Compile-time interface check.
var _ RepoLike = (*ThreadSafeRepo)(nil)

func newTestThreadSafeRepo(t *testing.T) *ThreadSafeRepo {
	t.Helper()

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
	}

	ts, err := NewThreadSafeRepo(p)
	require.NoError(t, err)

	return ts
}

func TestNewThreadSafeRepo_OptError(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
	}

	errOpt := func(r *Repo) error {
		return errors.New("opt failed")
	}

	ts, err := NewThreadSafeRepo(p, errOpt)
	assert.Nil(t, ts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "opt failed")
}

func TestNewThreadSafeRepo_Success(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)
	assert.NotNil(t, ts)
	assert.NotNil(t, ts.repo)
}

func TestThreadSafeRepo_SourceIs_SetSource(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	assert.False(t, ts.SourceIs(SourceLocal))
	ts.SetSource(SourceLocal)
	assert.True(t, ts.SourceIs(SourceLocal))
	assert.False(t, ts.SourceIs(SourceMemory))
}

func TestThreadSafeRepo_SetGetAuth(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	assert.Nil(t, ts.GetAuth())
	ts.SetBasicAuth("user", "pass")

	auth := ts.GetAuth()
	require.NotNil(t, auth)
	ba, ok := auth.(*http.BasicAuth)
	require.True(t, ok)
	assert.Equal(t, "user", ba.Username)
	assert.Equal(t, "pass", ba.Password)
}

func TestThreadSafeRepo_SetRepo_SetTree(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	tree, err := gitRepo.Worktree()
	require.NoError(t, err)

	ts.SetRepo(gitRepo)
	ts.SetTree(tree)

	err = ts.WithRepo(func(gr *git.Repository) error {
		assert.Equal(t, gitRepo, gr)

		return nil
	})
	assert.NoError(t, err)

	err = ts.WithTree(func(wt *git.Worktree) error {
		assert.Equal(t, tree, wt)

		return nil
	})
	assert.NoError(t, err)
}

func TestThreadSafeRepo_WithRepo_NoRepo(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	err := ts.WithRepo(func(_ *git.Repository) error {
		return nil
	})
	assert.ErrorIs(t, err, ErrNoRepository)
}

func TestThreadSafeRepo_WithRepo_CallsFn(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	ts.SetRepo(gitRepo)

	called := false
	err = ts.WithRepo(func(gr *git.Repository) error {
		called = true
		assert.Equal(t, gitRepo, gr)

		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestThreadSafeRepo_WithRepo_PropagatesError(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	ts.SetRepo(gitRepo)

	sentinel := errors.New("repo callback error")
	err = ts.WithRepo(func(_ *git.Repository) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
}

func TestThreadSafeRepo_WithTree_NoTree(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	err := ts.WithTree(func(_ *git.Worktree) error {
		return nil
	})
	assert.ErrorIs(t, err, ErrNoWorktree)
}

func TestThreadSafeRepo_WithTree_CallsFn(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	tree, err := gitRepo.Worktree()
	require.NoError(t, err)

	ts.SetTree(tree)

	called := false
	err = ts.WithTree(func(wt *git.Worktree) error {
		called = true
		assert.Equal(t, tree, wt)

		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestThreadSafeRepo_WithTree_PropagatesError(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	tree, err := gitRepo.Worktree()
	require.NoError(t, err)

	ts.SetTree(tree)

	sentinel := errors.New("tree callback error")
	err = ts.WithTree(func(_ *git.Worktree) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
}

func TestThreadSafeRepo_OpenLocal_Delegation(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)
	tmpDir := t.TempDir()

	repo, tree, err := ts.OpenLocal(tmpDir, "main")
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.NotNil(t, tree)
	assert.True(t, ts.SourceIs(SourceLocal))
}

func TestThreadSafeRepo_Open_Delegation(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)
	tmpDir := t.TempDir()

	repo, tree, err := ts.Open(LocalRepo, tmpDir, "main")
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.NotNil(t, tree)
}

func TestThreadSafeRepo_GitOperations(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)
	tmpDir := t.TempDir()

	_, wt, err := ts.OpenLocal(tmpDir, "main")
	require.NoError(t, err)

	// Create a file and commit
	require.NoError(t, afero.WriteFile(afero.NewOsFs(), tmpDir+"/test.txt", []byte("hello"), 0o644))
	_, err = wt.Add("test.txt")
	require.NoError(t, err)

	hash, err := ts.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.com", When: fixedTime},
	})
	require.NoError(t, err)
	assert.NotEqual(t, plumbing.ZeroHash, hash)

	// FileExists
	exists, err := ts.FileExists("test.txt")
	require.NoError(t, err)
	assert.True(t, exists)

	// DirectoryExists
	exists, err = ts.DirectoryExists("")
	require.NoError(t, err)
	assert.True(t, exists)

	// GetFile
	file, err := ts.GetFile("test.txt")
	require.NoError(t, err)
	assert.Equal(t, "test.txt", file.Name)

	// AddToFS
	memFS := afero.NewMemMapFs()
	err = ts.AddToFS(memFS, file, "/out.txt")
	require.NoError(t, err)

	content, err := afero.ReadFile(memFS, "/out.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))

	// WalkTree
	var files []string
	err = ts.WalkTree(func(f *object.File) error {
		files = append(files, f.Name)

		return nil
	})
	require.NoError(t, err)
	assert.Contains(t, files, "test.txt")

	// CreateBranch
	err = ts.CreateBranch("feature")
	require.NoError(t, err)

	// Checkout
	err = ts.Checkout(plumbing.NewBranchReferenceName("main"))
	require.NoError(t, err)

	// CheckoutCommit
	err = ts.CheckoutCommit(hash)
	require.NoError(t, err)

	// SetKey (just verify no panic)
	ts.SetKey(nil)

	// Push — will fail with no remote, but exercises the delegation
	err = ts.Push(nil)
	assert.Error(t, err)
}

func TestThreadSafeRepo_Clone_Delegation(t *testing.T) {
	t.Parallel()

	// Set up a source repo
	srcDir := t.TempDir()
	srcRepo, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)

	wt, err := srcRepo.Worktree()
	require.NoError(t, err)

	require.NoError(t, afero.WriteFile(afero.NewOsFs(), srcDir+"/readme.txt", []byte("hi"), 0o644))
	_, err = wt.Add("readme.txt")
	require.NoError(t, err)
	_, err = wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.com", When: fixedTime},
	})
	require.NoError(t, err)

	// Clone via ThreadSafeRepo
	ts := newTestThreadSafeRepo(t)
	dstDir := t.TempDir() + "/clone"

	repo, tree, err := ts.Clone(srcDir, dstDir)
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.NotNil(t, tree)
}

// --- Concurrency / race tests ---

const raceGoroutines = 10

func TestThreadSafeRepo_ConcurrentSetSource(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	var wg sync.WaitGroup

	wg.Add(raceGoroutines)

	for i := range raceGoroutines {
		go func(source int) {
			defer wg.Done()

			ts.SetSource(source)
			_ = ts.SourceIs(source)
		}(i)
	}

	wg.Wait()
}

func TestThreadSafeRepo_ConcurrentSetGetAuth(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	var wg sync.WaitGroup

	wg.Add(raceGoroutines)

	for range raceGoroutines {
		go func() {
			defer wg.Done()

			ts.SetBasicAuth("user", "pass")
			_ = ts.GetAuth()
		}()
	}

	wg.Wait()
}

func TestThreadSafeRepo_ConcurrentWithRepo(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	ts.SetRepo(gitRepo)

	var wg sync.WaitGroup

	wg.Add(raceGoroutines)

	for range raceGoroutines {
		go func() {
			defer wg.Done()

			_ = ts.WithRepo(func(gr *git.Repository) error {
				_ = gr

				return nil
			})
		}()
	}

	wg.Wait()
}

func TestThreadSafeRepo_ConcurrentSetRepo(t *testing.T) {
	t.Parallel()

	ts := newTestThreadSafeRepo(t)

	tmpDir := t.TempDir()
	gitRepo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	var wg sync.WaitGroup

	wg.Add(raceGoroutines * 2)

	for range raceGoroutines {
		go func() {
			defer wg.Done()

			ts.SetRepo(gitRepo)
		}()

		go func() {
			defer wg.Done()

			_ = ts.WithRepo(func(_ *git.Repository) error {
				return nil
			})
		}()
	}

	wg.Wait()
}
