package repo

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	gitobject "github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memRepo returns a *Repo backed by an in-memory git repository with a worktree.
func memRepo(t *testing.T) *Repo {
	t.Helper()

	gr, err := git.Init(memory.NewStorage(), memfs.New())
	require.NoError(t, err)

	wt, err := gr.Worktree()
	require.NoError(t, err)

	r := &Repo{}
	r.repo = gr
	r.SetTree(wt)

	return r
}

func TestRepo_WorkFS_NoWorktree(t *testing.T) {
	t.Parallel()

	_, err := (&Repo{}).WorkFS()
	require.ErrorIs(t, err, ErrNoWorktree)

	err = (&Repo{}).WithWorkFS(func(afero.Fs) error { return nil })
	require.ErrorIs(t, err, ErrNoWorktree)
}

func TestRepo_WorkFS_ReadWrite(t *testing.T) {
	t.Parallel()

	r := memRepo(t)

	fs, err := r.WorkFS()
	require.NoError(t, err)

	require.NoError(t, afero.WriteFile(fs, "storyboard.json", []byte(`{"scene":1}`), 0o644))

	data, err := afero.ReadFile(fs, "storyboard.json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"scene":1}`, string(data))
}

func TestRepo_WithWorkFS(t *testing.T) {
	t.Parallel()

	r := memRepo(t)

	require.NoError(t, r.WithWorkFS(func(fs afero.Fs) error {
		return afero.WriteFile(fs, "a.txt", []byte("hi"), 0o644)
	}))

	fs, err := r.WorkFS()
	require.NoError(t, err)
	got, err := afero.ReadFile(fs, "a.txt")
	require.NoError(t, err)
	assert.Equal(t, "hi", string(got))
}

func TestThreadSafeRepo_WorkFS(t *testing.T) {
	t.Parallel()

	// No worktree → ErrNoWorktree.
	ts0 := &ThreadSafeRepo{repo: &Repo{}}
	_, err := ts0.WorkFS()
	require.ErrorIs(t, err, ErrNoWorktree)
	require.ErrorIs(t, ts0.WithWorkFS(func(afero.Fs) error { return nil }), ErrNoWorktree)

	ts := &ThreadSafeRepo{repo: memRepo(t)}

	fs, err := ts.WorkFS()
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "f", []byte("x"), 0o644))

	require.NoError(t, ts.WithWorkFS(func(fs afero.Fs) error {
		return afero.WriteFile(fs, "g", []byte("y"), 0o644)
	}))

	gotF, err := afero.ReadFile(fs, "f")
	require.NoError(t, err)
	assert.Equal(t, "x", string(gotF))
}

func TestThreadSafeRepo_WorkFS_ConcurrentIsRaceFree(t *testing.T) {
	t.Parallel()

	ts := &ThreadSafeRepo{repo: memRepo(t)}

	fs, err := ts.WorkFS()
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("f%d", i)
			// The handle serialises through the repo mutex; concurrent worktree
			// writes interleave safely with another goroutine's repo access.
			_ = afero.WriteFile(fs, name, []byte("payload"), 0o644)
			_, _ = ts.WorkFS() // re-derive a handle concurrently
		}(i)
	}
	wg.Wait()
}

// TestWorkFS_SingleSourceOfTruth proves the core claim end-to-end: a file
// written through the afero view IS the worktree, so go-git stages and commits
// it with no materialise/sync step.
func TestWorkFS_SingleSourceOfTruth(t *testing.T) {
	t.Parallel()

	r := memRepo(t)

	fs, err := r.WorkFS()
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "storyboard.json", []byte(`{"scene":1}`), 0o644))

	require.NoError(t, r.AddAll())

	hash, err := r.Commit("add storyboard", &git.CommitOptions{
		Author: &gitobject.Signature{Name: "t", Email: "t@example.com", When: time.Unix(0, 0)},
	})
	require.NoError(t, err)

	commit, err := r.repo.CommitObject(hash)
	require.NoError(t, err)
	tree, err := commit.Tree()
	require.NoError(t, err)

	_, err = tree.File("storyboard.json")
	require.NoError(t, err, "the afero-written file is present in the commit tree")
}
