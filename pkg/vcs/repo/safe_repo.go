package repo

import (
	"sync"

	"github.com/cockroachdb/errors"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// ThreadSafeRepo wraps a *Repo with a mutex so that all RepoLike methods are safe
// to call concurrently from multiple goroutines.
//
// # Thread-safety guarantee
//
// Every method acquires the internal mutex for its full duration. Concurrent callers
// are serialised; no two calls to any method execute simultaneously.
//
// # WithRepo and WithTree
//
// These are the only way to interact with the underlying go-git objects safely.
// The callback executes while the mutex is held. Callers must not:
//   - Retain the pointer after the callback returns.
//   - Call any ThreadSafeRepo method from inside the callback (deadlock).
//   - Spawn goroutines inside the callback that access the pointer after it returns.
//
// # go-git concurrency model
//
// go-git mutates internal caches during read operations. ThreadSafeRepo uses
// sync.Mutex (exclusive) rather than sync.RWMutex; concurrent reads are not permitted.
type ThreadSafeRepo struct {
	mu   sync.Mutex
	repo *Repo
}

// NewThreadSafeRepo creates a ThreadSafeRepo backed by a freshly constructed *Repo.
// The props and opts arguments have the same semantics as NewRepo.
func NewThreadSafeRepo(p *props.Props, opts ...RepoOpt) (*ThreadSafeRepo, error) {
	r, err := NewRepo(p, opts...)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &ThreadSafeRepo{repo: r}, nil
}

func (r *ThreadSafeRepo) SourceIs(source int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.SourceIs(source)
}

func (r *ThreadSafeRepo) SetSource(source int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.repo.SetSource(source)
}

func (r *ThreadSafeRepo) SetRepo(repo *git.Repository) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.repo.SetRepo(repo)
}

func (r *ThreadSafeRepo) SetKey(key *ssh.PublicKeys) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.repo.SetKey(key)
}

func (r *ThreadSafeRepo) SetBasicAuth(username, password string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.repo.SetBasicAuth(username, password)
}

func (r *ThreadSafeRepo) GetAuth() transport.AuthMethod {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.GetAuth()
}

func (r *ThreadSafeRepo) SetTree(tree *git.Worktree) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.repo.SetTree(tree)
}

func (r *ThreadSafeRepo) Checkout(branch plumbing.ReferenceName) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.Checkout(branch)
}

func (r *ThreadSafeRepo) CheckoutCommit(hash plumbing.Hash) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.CheckoutCommit(hash)
}

func (r *ThreadSafeRepo) CreateBranch(branchName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.CreateBranch(branchName)
}

func (r *ThreadSafeRepo) Push(opts *git.PushOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.Push(opts)
}

func (r *ThreadSafeRepo) Commit(commitMsg string, opts *git.CommitOptions) (plumbing.Hash, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.Commit(commitMsg, opts)
}

func (r *ThreadSafeRepo) OpenInMemory(location string, branch string, opts ...CloneOption) (*git.Repository, *git.Worktree, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.OpenInMemory(location, branch, opts...)
}

func (r *ThreadSafeRepo) OpenLocal(location string, branch string) (*git.Repository, *git.Worktree, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.OpenLocal(location, branch)
}

func (r *ThreadSafeRepo) Open(repoType RepoType, location string, branch string, opts ...CloneOption) (*git.Repository, *git.Worktree, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.Open(repoType, location, branch, opts...)
}

func (r *ThreadSafeRepo) Clone(uri string, targetPath string, opts ...CloneOption) (*git.Repository, *git.Worktree, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.Clone(uri, targetPath, opts...)
}

func (r *ThreadSafeRepo) WalkTree(fn func(*object.File) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.WalkTree(fn)
}

func (r *ThreadSafeRepo) FileExists(relPath string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.FileExists(relPath)
}

func (r *ThreadSafeRepo) DirectoryExists(relPath string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.DirectoryExists(relPath)
}

func (r *ThreadSafeRepo) GetFile(relPath string) (*object.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.GetFile(relPath)
}

func (r *ThreadSafeRepo) AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.repo.AddToFS(fs, gitFile, fullPath)
}

// WithRepo acquires the mutex and calls fn with the underlying *git.Repository.
// The callback executes while the lock is held.
//
// Returns ErrNoRepository if the repository has not been initialised.
func (r *ThreadSafeRepo) WithRepo(fn func(*git.Repository) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.repo.repo == nil {
		return ErrNoRepository
	}

	return fn(r.repo.repo)
}

// WithTree acquires the mutex and calls fn with the underlying *git.Worktree.
// The callback executes while the lock is held.
//
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *ThreadSafeRepo) WithTree(fn func(*git.Worktree) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.repo.tree == nil {
		return ErrNoWorktree
	}

	return fn(r.repo.tree)
}
