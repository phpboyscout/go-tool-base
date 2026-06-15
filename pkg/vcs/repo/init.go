package repo

import (
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/cockroachdb/errors"
)

// Initializer is the init-only role: it initialises a fresh repository on an
// explicit default branch and stages the worktree honouring .gitignore. It is
// deliberately NOT embedded in RepoLike — init/discovery and gitignore-aware
// staging are first-commit concerns a clone/checkout consumer never needs, so
// callers that need them (the generator's scaffold step) depend on this narrow
// role directly. Both *Repo and *ThreadSafeRepo satisfy it.
type Initializer interface {
	InitLocal(location, branch string) (*git.Repository, *git.Worktree, error)
	AddAll() error
}

// Compile-time proof that both concrete repository types satisfy Initializer.
var (
	_ Initializer = (*Repo)(nil)
	_ Initializer = (*ThreadSafeRepo)(nil)
)

// ErrAlreadyRepository is returned by InitLocal when the target path is already
// inside a git repository. Unlike OpenLocal — which opens an existing repo or
// initialises a new one, conflating the two outcomes — InitLocal refuses to act
// on an existing repository so a caller (e.g. the generator's first-commit step)
// can reliably distinguish "newly initialised" from "already tracked".
var ErrAlreadyRepository = errors.New("path is already inside a git repository")

// DiscoverRepository reports whether location is inside a git repository,
// walking upward from location to find an enclosing .git directory (matching
// git's own discovery semantics). It is a read-only probe: it never creates or
// modifies a repository. A subdirectory of an existing repository reports true.
//
// This is distinct from OpenLocal, which init-if-absent and cannot tell an
// opened existing repository apart from a freshly initialised one.
func DiscoverRepository(location string) (found bool, err error) {
	_, err = git.PlainOpenWithOptions(location, &git.PlainOpenOptions{DetectDotGit: true})
	if err == nil {
		return true, nil
	}

	if errors.Is(err, git.ErrRepositoryNotExists) {
		return false, nil
	}

	return false, errors.Wrapf(err, "failed to probe for git repository at %s", location)
}

// InitLocal initialises a new git repository at location on the named default
// branch and binds it (and its worktree) to the receiver. It is the init-only
// counterpart to OpenLocal: if location is already inside a repository it
// returns ErrAlreadyRepository rather than silently opening the existing one,
// so callers can keep init and open as separate, explicit decisions.
//
// branch defaults to "main" when empty.
func (r *Repo) InitLocal(location, branch string) (*git.Repository, *git.Worktree, error) {
	if branch == "" {
		branch = "main"
	}

	found, err := DiscoverRepository(location)
	if err != nil {
		return nil, nil, err
	}

	if found {
		return nil, nil, errors.Wrapf(ErrAlreadyRepository, "%s", location)
	}

	r.SetSource(SourceLocal)

	gitRepo, err := git.PlainInitWithOptions(location, &git.PlainInitOptions{
		InitOptions: git.InitOptions{
			DefaultBranch: plumbing.NewBranchReferenceName(branch),
		},
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to initialise git repository")
	}

	tree, err := gitRepo.Worktree()
	if err != nil {
		return gitRepo, nil, errors.WithStack(err)
	}

	r.repo = gitRepo
	r.tree = tree

	return gitRepo, tree, nil
}

// AddAll stages the entire worktree, honouring .gitignore rules so ignored
// paths (build artefacts and the like) are not committed. The .gitignore file
// itself is staged. It wraps go-git's Worktree.AddWithOptions{All: true}, which
// applies the repository's ignore patterns — a plain Add(".") would not.
//
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *Repo) AddAll() error {
	if r.tree == nil {
		return ErrNoWorktree
	}

	if err := r.tree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return errors.Wrap(err, "failed to stage worktree")
	}

	return nil
}
