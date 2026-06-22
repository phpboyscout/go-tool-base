package repo

import (
	git "github.com/go-git/go-git/v5"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo/aferobilly"
)

// WorktreeFS is a RepoLike role exposing the active worktree as an afero.Fs, so
// afero-based code can read and write an in-memory (or local) git worktree
// directly — single source of truth, no materialise/sync step (contrast with
// [TreeReader.AddToFS], which copies a git tree into a *separate* afero FS).
type WorktreeFS interface {
	// WorkFS returns the active worktree's filesystem as an afero.Fs, or
	// [ErrNoWorktree] if no worktree is open.
	//
	// On [ThreadSafeRepo] the returned FS is safe for concurrent use: every
	// operation re-locks the repo mutex. Operations are individually atomic, but
	// a SEQUENCE is not — a concurrent Commit/Add may interleave between two
	// writes. Use [WorktreeFS.WithWorkFS] when a sequence must be atomic.
	//
	// The returned FS (and any file it opens) MUST NOT be used from inside a
	// WithWorkFS / WithTree / WithRepo callback: that region already holds the
	// repo's (non-reentrant) mutex, so re-locking would deadlock.
	WorkFS() (afero.Fs, error)

	// WithWorkFS runs fn with an afero view of the worktree while holding the
	// repo lock for the whole callback, so the sequence is atomic relative to
	// other repo operations. The afero.Fs passed to fn is unsynchronised (the
	// lock is already held); fn MUST NOT retain it past return, call any other
	// repo method, or open a WorkFS() handle.
	WithWorkFS(fn func(afero.Fs) error) error
}

// WorkFS returns the worktree as an afero.Fs. *Repo is not safe for concurrent
// use, so the view uses the default no-op locker; the caller owns any external
// synchronisation.
func (r *Repo) WorkFS() (afero.Fs, error) {
	if r.tree == nil {
		return nil, ErrNoWorktree
	}

	return aferobilly.New(r.tree.Filesystem), nil
}

// WithWorkFS runs fn with an afero view of the worktree.
func (r *Repo) WithWorkFS(fn func(afero.Fs) error) error {
	return r.WithTree(func(wt *git.Worktree) error {
		return fn(aferobilly.New(wt.Filesystem))
	})
}
