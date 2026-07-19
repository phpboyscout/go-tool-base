package repo

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/spf13/afero"
	gossh "golang.org/x/crypto/ssh"
)

const (
	SourceUnknown = iota
	SourceMemory
	SourceLocal

	// dirPermStandard is the standard permission mode for directories (0755).
	dirPermStandard = 0o755
	// filePermStandard is the standard permission mode for files (0644).
	filePermStandard = 0o644
)

// RepoType identifies the repository backend (local filesystem or in-memory).
// It is a defined type rather than a bare string alias so the backend
// discriminator carries its own type at call sites.
type RepoType string

const (
	LocalRepo    RepoType = "local"
	InMemoryRepo RepoType = "inmemory"
)

// CloneOption represents a function that configures clone options.
type CloneOption func(*git.CloneOptions)

// WithShallowClone configures a shallow clone with the specified depth.
func WithShallowClone(depth int) CloneOption {
	return func(config *git.CloneOptions) {
		config.Depth = depth
	}
}

// WithSingleBranch configures the clone to only fetch a single branch.
func WithSingleBranch(branch string) CloneOption {
	return func(config *git.CloneOptions) {
		config.SingleBranch = true
		if branch != "" {
			config.ReferenceName = plumbing.NewBranchReferenceName(branch)
		}
	}
}

// WithNoTags configures the clone to skip fetching tags.
func WithNoTags() CloneOption {
	return func(config *git.CloneOptions) {
		config.Tags = git.NoTags
	}
}

// WithRecurseSubmodules configures recursive submodule cloning.
func WithRecurseSubmodules() CloneOption {
	return func(config *git.CloneOptions) {
		config.RecurseSubmodules = git.DefaultSubmoduleRecursionDepth
	}
}

var (
	// ErrNoRepository is returned by WithRepo when no *git.Repository has been set.
	ErrNoRepository = errors.New("repository not initialised; call Open, Clone, or SetRepo first")

	// ErrNoWorktree is returned by WithTree when no *git.Worktree has been set.
	ErrNoWorktree = errors.New("worktree not initialised; call Open, Clone, or SetTree first")

	gitProgressOutput io.Writer
)

func init() {
	if _, ok := os.LookupEnv("GTB_GIT_ENABLE_PROGRESS"); ok {
		gitProgressOutput = os.Stderr
	}
}

// The role interfaces below decompose git-repository operations into focused,
// single-concern contracts following the pkg/controls precedent (where
// Controllable is composed from Runner, HealthReporter, …). A consumer should
// depend on the narrowest role that covers what it actually touches rather than
// the full RepoLike composite. They cover the common subset that both *Repo and
// *ThreadSafeRepo satisfy; the named-remote operations (CreateRemote, Remote)
// remain concrete-only on *Repo and are deliberately not part of any role.

// TreeReader provides read-only queries over the committed git tree.
type TreeReader interface {
	WalkTree(func(*object.File) error) error
	FileExists(string) (bool, error)
	DirectoryExists(string) (bool, error)
	GetFile(string) (*object.File, error)
	AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error
}

// Opener acquires a repository via local open, in-memory open, or clone.
type Opener interface {
	Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
	OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
	OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
	Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
}

// Authenticator configures credentials and the underlying repository handle.
type Authenticator interface {
	SetKey(*ssh.PublicKeys)
	SetBasicAuth(string, string)
	GetAuth() transport.AuthMethod
	SetRepo(*git.Repository)
}

// WorktreeController manages working-tree and checkout state.
type WorktreeController interface {
	SetTree(*git.Worktree)
	Checkout(plumbing.ReferenceName) error
	CheckoutCommit(plumbing.Hash) error
}

// Committer records and publishes changes.
type Committer interface {
	Commit(string, *git.CommitOptions) (plumbing.Hash, error)
	Push(*git.PushOptions) error
}

// SourceState discriminates the repository backend (in-memory vs local).
type SourceState interface {
	SourceIs(int) bool
	SetSource(int)
}

// GitAccessor exposes raw go-git escape hatches.
type GitAccessor interface {
	// WithRepo calls fn with the underlying *git.Repository.
	// Returns ErrNoRepository if the repository has not been initialised.
	WithRepo(func(*git.Repository) error) error

	// WithTree calls fn with the underlying *git.Worktree.
	// Returns ErrNoWorktree if the worktree has not been initialised.
	WithTree(func(*git.Worktree) error) error
}

// Brancher creates branches.
type Brancher interface {
	CreateBranch(string) error
}

// RepoLike is the full git-repository interface, composed of focused role
// interfaces. Prefer depending on the narrower roles (TreeReader, Committer, …)
// where possible. Its method set is identical to the sum of the embedded roles,
// so *Repo and *ThreadSafeRepo satisfy it unchanged.
type RepoLike interface {
	TreeReader
	Opener
	Authenticator
	WorktreeController
	Committer
	SourceState
	GitAccessor
	Brancher
	WorktreeFS
}

// Compile-time proof that both concrete repository types satisfy the full
// composite and every individual role interface.
var (
	_ RepoLike = (*Repo)(nil)
	_ RepoLike = (*ThreadSafeRepo)(nil)

	_ TreeReader         = (*Repo)(nil)
	_ TreeReader         = (*ThreadSafeRepo)(nil)
	_ Opener             = (*Repo)(nil)
	_ Opener             = (*ThreadSafeRepo)(nil)
	_ Authenticator      = (*Repo)(nil)
	_ Authenticator      = (*ThreadSafeRepo)(nil)
	_ WorktreeController = (*Repo)(nil)
	_ WorktreeController = (*ThreadSafeRepo)(nil)
	_ Committer          = (*Repo)(nil)
	_ Committer          = (*ThreadSafeRepo)(nil)
	_ SourceState        = (*Repo)(nil)
	_ SourceState        = (*ThreadSafeRepo)(nil)
	_ GitAccessor        = (*Repo)(nil)
	_ GitAccessor        = (*ThreadSafeRepo)(nil)
	_ Brancher           = (*Repo)(nil)
	_ Brancher           = (*ThreadSafeRepo)(nil)
	_ WorktreeFS         = (*Repo)(nil)
	_ WorktreeFS         = (*ThreadSafeRepo)(nil)
)

// Repo implements RepoLike using go-git for local and in-memory repositories.
type Repo struct {
	source int
	config *config.Config
	repo   *git.Repository
	auth   transport.AuthMethod
	tree   *git.Worktree
}

func (r *Repo) SourceIs(source int) bool {
	return r.source == source
}

func (r *Repo) SetSource(source int) {
	r.source = source
}

func (r *Repo) SetRepo(repo *git.Repository) {
	r.repo = repo
}

func (r *Repo) SetKey(key *ssh.PublicKeys) {
	r.auth = key
}

func (r *Repo) SetBasicAuth(username, password string) {
	r.auth = &http.BasicAuth{
		Username: username,
		Password: password,
	}
}

func (r *Repo) GetAuth() transport.AuthMethod {
	return r.auth
}

func (r *Repo) SetTree(tree *git.Worktree) {
	r.tree = tree
}

// WithRepo calls fn with the underlying *git.Repository.
// Repo is not safe for concurrent use; callers are responsible for external
// synchronisation if sharing a *Repo across goroutines.
//
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) WithRepo(fn func(*git.Repository) error) error {
	if r.repo == nil {
		return ErrNoRepository
	}

	return fn(r.repo)
}

// WithTree calls fn with the underlying *git.Worktree.
// Repo is not safe for concurrent use; callers are responsible for external
// synchronisation if sharing a *Repo across goroutines.
//
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *Repo) WithTree(fn func(*git.Worktree) error) error {
	if r.tree == nil {
		return ErrNoWorktree
	}

	return fn(r.tree)
}

// Checkout checks out the given branch.
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *Repo) Checkout(branch plumbing.ReferenceName) error {
	if r.tree == nil {
		return ErrNoWorktree
	}

	return r.tree.Checkout(&git.CheckoutOptions{
		Branch: branch,
	})
}

// CheckoutCommit checks out a specific commit by hash.
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *Repo) CheckoutCommit(hash plumbing.Hash) error {
	if r.tree == nil {
		return ErrNoWorktree
	}

	return r.tree.Checkout(&git.CheckoutOptions{
		Hash: hash,
	})
}

// CreateBranch creates a branch in the git tree.
// Returns ErrNoRepository / ErrNoWorktree if the repo has not been opened.
func (r *Repo) CreateBranch(branchName string) error {
	if r.repo == nil {
		return ErrNoRepository
	}

	if r.tree == nil {
		return ErrNoWorktree
	}

	bref := plumbing.NewBranchReferenceName(branchName)

	branchExists, err := r.branchExists(bref)
	if err != nil {
		return err
	}

	if err := r.tree.Checkout(&git.CheckoutOptions{
		Create: !branchExists,
		Branch: bref,
	}); err != nil {
		return errors.WithStack(err)
	}

	if branchExists && !r.SourceIs(SourceMemory) {
		// make sure we pull the latest changes; an already-up-to-date branch
		// is the common case and not a failure.
		if err := r.tree.Pull(&git.PullOptions{
			Auth:     r.auth,
			Force:    true,
			Progress: gitProgressOutput,
		}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return errors.WithStack(err)
		}
	}

	return nil
}

// branchExists reports whether the named branch exists in the repository.
func (r *Repo) branchExists(bref plumbing.ReferenceName) (bool, error) {
	branches, err := r.repo.Branches()
	if err != nil {
		return false, errors.WithStack(err)
	}

	exists := false

	if err = branches.ForEach(func(branch *plumbing.Reference) error {
		if branch.Name().Short() == bref.Short() {
			exists = true
		}

		return nil
	}); err != nil {
		return false, errors.WithStack(err)
	}

	return exists, nil
}

// Push pushes the repository using the configured auth when opts.Auth is nil.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) Push(opts *git.PushOptions) error {
	if r.repo == nil {
		return ErrNoRepository
	}

	if opts == nil {
		opts = &git.PushOptions{}
	}

	if opts.Auth == nil {
		opts.Auth = r.auth
	}

	return r.repo.Push(opts)
}

// Commit records a commit on the current worktree.
// Returns ErrNoWorktree if the worktree has not been initialised.
func (r *Repo) Commit(commitMsg string, opts *git.CommitOptions) (plumbing.Hash, error) {
	if r.tree == nil {
		return plumbing.ZeroHash, ErrNoWorktree
	}

	if opts == nil {
		opts = &git.CommitOptions{}
	}

	return r.tree.Commit(commitMsg, opts)
}

// CreateRemote creates a named remote with the given URLs.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) CreateRemote(name string, urls []string) (*git.Remote, error) {
	if r.repo == nil {
		return nil, ErrNoRepository
	}

	return r.repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: urls,
	})
}

// Remote returns the named remote.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) Remote(name string) (*git.Remote, error) {
	if r.repo == nil {
		return nil, ErrNoRepository
	}

	return r.repo.Remote(name)
}

// GetSSHKey loads an SSH private key from the filesystem for git
// authentication. If the key is passphrase-protected, the returned error
// wraps [gossh.PassphraseMissingError]; interactive callers (the CLI layer)
// should detect it with errors.As, prompt the user, and retry via
// [GetSSHKeyWithPassphrase]. This package never blocks on a TUI.
func GetSSHKey(filePath string, localfs afero.Fs) (*ssh.PublicKeys, error) {
	return GetSSHKeyWithPassphrase(filePath, localfs, "")
}

// GetSSHKeyWithPassphrase loads an SSH private key from the filesystem,
// decrypting it with the supplied passphrase when non-empty.
func GetSSHKeyWithPassphrase(filePath string, localfs afero.Fs, passphrase string) (*ssh.PublicKeys, error) {
	sshKey, err := readSSHKeyFile(filePath, localfs)
	if err != nil {
		return nil, err
	}

	if passphrase == "" {
		if _, err := gossh.ParsePrivateKey(sshKey); err != nil {
			var missing *gossh.PassphraseMissingError
			if errors.As(err, &missing) {
				return nil, errors.WithHint(
					errors.Wrapf(err, "SSH key '%s' is passphrase-protected", filePath),
					"Prompt for the passphrase and retry with GetSSHKeyWithPassphrase, or load the key into ssh-agent",
				)
			}
		}
	}

	return ssh.NewPublicKeys("git", sshKey, passphrase)
}

// readSSHKeyFile reads the raw private-key bytes from localfs.
func readSSHKeyFile(filePath string, localfs afero.Fs) ([]byte, error) {
	fileHandle, err := localfs.Stat(filePath)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if fileHandle.IsDir() {
		return nil, errors.Newf("could not open SSH key at '%s': path is a directory", filePath)
	}

	sshKey, err := afero.ReadFile(localfs, filePath)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return sshKey, nil
}

func (r *Repo) OpenInMemory(location string, branch string, opts ...CloneOption) (*git.Repository, *git.Worktree, error) {
	var err error

	r.SetSource(SourceMemory)

	fs := memfs.New()

	storer := memory.NewStorage()
	if r.config != nil {
		err := storer.SetConfig(r.config)
		if err != nil {
			return nil, nil, errors.WithStack(err)
		}
	}

	// Build clone options based on configuration
	cloneOpts := &git.CloneOptions{
		URL:      location,
		Auth:     r.auth,
		Progress: gitProgressOutput,
	}

	for _, opt := range opts {
		opt(cloneOpts)
	}

	r.repo, err = git.Clone(storer, fs, cloneOpts)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	r.tree, err = r.repo.Worktree()
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	if branch != "" {
		err = r.Checkout(plumbing.NewBranchReferenceName(branch))
		if err != nil {
			return nil, nil, errors.WithStack(err)
		}
	}

	return r.repo, r.tree, nil
}

// Open opens a local git repository. if no repo exists will init a repo.
func (r *Repo) OpenLocal(location string, branch string) (*git.Repository, *git.Worktree, error) {
	r.SetSource(SourceLocal)

	repo, err := git.PlainOpen(location)
	if err != nil {
		repo, err = git.PlainInitWithOptions(location, &git.PlainInitOptions{
			InitOptions: git.InitOptions{
				DefaultBranch: plumbing.NewBranchReferenceName(branch),
			},
		})
		if err != nil {
			return repo, nil, errors.Newf("failed to initialize Git repository: %w", err)
		}
	}

	tree, err := repo.Worktree()
	if err != nil {
		return repo, tree, errors.WithStack(err)
	}

	r.repo = repo
	r.tree = tree

	return repo, tree, nil
}

// Open opens a local git repository. if no repo exists will init a repo.
func (r *Repo) Open(repoType RepoType, location string, branch string, opts ...CloneOption) (*git.Repository, *git.Worktree, error) {
	switch RepoType(strings.ToLower(string(repoType))) {
	case LocalRepo:
		return r.OpenLocal(location, branch)
	case InMemoryRepo:
		return r.OpenInMemory(location, branch, opts...)
	}

	return nil, nil, errors.Newf("unknown repo type: %s", repoType)
}

// Clone clones a git repository to a target path on the filesystem
// Supports both remote URLs and local git repository paths with clone options.
func (r *Repo) Clone(uri string, targetPath string, opts ...CloneOption) (*git.Repository, *git.Worktree, error) {
	var err error

	r.SetSource(SourceLocal)

	// Create target directory if it doesn't exist
	if err := os.MkdirAll(targetPath, dirPermStandard); err != nil {
		return nil, nil, errors.Wrap(err, "failed to create target directory")
	}

	// Build clone options based on configuration
	cloneOpts := &git.CloneOptions{
		URL:      uri,
		Auth:     r.auth,
		Progress: gitProgressOutput,
	}

	for _, opt := range opts {
		opt(cloneOpts)
	}

	// Clone the repository to the target path
	r.repo, err = git.PlainClone(targetPath, false, cloneOpts)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to clone repository")
	}

	// Get the worktree
	r.tree, err = r.repo.Worktree()
	if err != nil {
		return r.repo, nil, errors.WithStack(err)
	}

	return r.repo, r.tree, nil
}

// headTree resolves the tree of the current HEAD commit.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) headTree() (*object.Tree, error) {
	if r.repo == nil {
		return nil, ErrNoRepository
	}

	head, err := r.repo.Head()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get HEAD reference")
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, errors.Wrap(err, "failed to get HEAD commit")
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get commit tree")
	}

	return tree, nil
}

// WalkTree walks the git tree and calls the provided function for each file.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) WalkTree(fn func(*object.File) error) error {
	tree, err := r.headTree()
	if err != nil {
		return err
	}

	// Walk the git tree and call the provided function for each file
	return tree.Files().ForEach(fn)
}

// FileExists checks if a file exists in the git repository at the given relative path.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) FileExists(relPath string) (bool, error) {
	tree, err := r.headTree()
	if err != nil {
		return false, err
	}

	_, err = tree.File(relPath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return false, nil
		}

		return false, errors.Wrap(err, "failed to check file in git")
	}

	return true, nil
}

// DirectoryExists checks if a directory exists in the git repository at the given relative path
// In git, directories don't exist as separate entities - we check if any files exist under the path.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) DirectoryExists(relPath string) (bool, error) {
	tree, err := r.headTree()
	if err != nil {
		return false, err
	}

	// Normalize the path - ensure it doesn't end with separator
	dirPath := strings.TrimSuffix(relPath, "/")
	if dirPath == "" {
		return true, nil // Root directory always exists
	}

	// Walk the tree to find any files under this directory path
	found := false
	err = tree.Files().ForEach(func(file *object.File) error {
		if strings.HasPrefix(file.Name, dirPath+"/") || file.Name == dirPath {
			found = true

			return io.EOF // Stop iteration early
		}

		return nil
	})

	if err != nil && !errors.Is(err, io.EOF) {
		return false, errors.Wrap(err, "failed to walk git tree")
	}

	return found, nil
}

// GetFile retrieves a file from the git repository at the given relative path.
// Returns ErrNoRepository if the repository has not been initialised.
func (r *Repo) GetFile(relPath string) (*object.File, error) {
	tree, err := r.headTree()
	if err != nil {
		return nil, err
	}

	file, err := tree.File(relPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get file from git")
	}

	return file, nil
}

// AddToFS ensures that a git file is available in the afero filesystem.
func (r *Repo) AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error {
	// Check if file already exists in afero
	if _, err := fs.Stat(fullPath); err == nil {
		return nil // File already exists
	}

	// Read content from git
	reader, err := gitFile.Reader()
	if err != nil {
		return errors.Wrap(err, "failed to get git file reader")
	}

	defer func() { _ = reader.Close() }()

	content, err := io.ReadAll(reader)
	if err != nil {
		return errors.Wrap(err, "failed to read git file content")
	}

	// Ensure directory exists in afero
	dir := filepath.Dir(fullPath)
	if err := fs.MkdirAll(dir, dirPermStandard); err != nil {
		return errors.Wrap(err, "failed to create directory in afero")
	}

	// Write content to afero filesystem
	return afero.WriteFile(fs, fullPath, content, filePermStandard)
}

func setSSHAgent(repo *Repo) error {
	auth, err := ssh.DefaultAuthBuilder("git")
	if err != nil {
		return errors.Newf("failed to create SSH auth: %w", err)
	}

	repo.auth = auth

	return nil
}

// resolveForge determines which forge settings NewRepo reads clone/push
// credentials from. The forge comes from Settings.Forge, falling back to
// ReleaseSource.Type. An empty or "direct" type falls back to "github" — the
// `direct` release source is a download URL with no git remote, so the repo
// layer has no forge of its own to read.
func resolveForge(settings Settings) string {
	forge := strings.ToLower(strings.TrimSpace(settings.Forge))

	// "direct" is a plain download source with no git remote, so it has no
	// forge convention of its own; fall back to GitHub's.
	if forge == "" || forge == ForgeDirect {
		return ForgeGitHub
	}

	return forge
}

// basicAuthUsername returns the username each forge expects for
// git-over-HTTPS token authentication.
func basicAuthUsername(forge string) string {
	switch forge {
	case ForgeGitLab:
		return "oauth2"
	case ForgeBitbucket:
		return "x-token-auth"
	default:
		return "x-access-token"
	}
}

func configureSSHAuth(repo *Repo, settings Settings, forge string) error {
	if !settings.SSH.HasKey {
		// <forge>.ssh is present but has no key subtree (e.g. a scalar
		// `github.ssh: true`); fall back to ssh-agent.
		warn(settings.Logger, "No ssh.key subtree configured for forge, defaulting to ssh-agent", "forge", forge)

		return setSSHAgent(repo)
	}

	if settings.SSH.Type == "agent" {
		return setSSHAgent(repo)
	}

	sshPath := filepath.Clean(os.Getenv(settings.SSH.Env))
	if settings.SSH.Path != "" {
		sshPath = filepath.Clean(settings.SSH.Path)
	}

	if sshPath == "" || sshPath == "." {
		warn(settings.Logger, "No SSH key path resolved from forge ssh.key config, defaulting to ssh-agent", "forge", forge)

		return setSSHAgent(repo)
	}

	publicKey, err := GetSSHKey(sshPath, settings.fs())
	if err != nil {
		return errors.Wrap(err, "failed to get SSH key")
	}

	repo.auth = publicKey

	return nil
}

// configureTokenAuth resolves a token from the forge's config subtree via
// the shared vcs.ResolveToken chain (auth.env → auth.keychain → auth.value →
// <FORGE>_TOKEN env var). Missing credentials are non-fatal for public
// repositories: the repo proceeds unauthenticated. Private repositories
// require a token and fail fast with a hint.
func configureTokenAuth(repo *Repo, settings Settings, forge string) error {
	fallbackEnv := strings.ToUpper(forge) + "_TOKEN"

	// Resolved here and nowhere earlier: a token may live in the OS keychain,
	// and a repository using SSH auth must never trigger that lookup.
	token := settings.Token.resolve()
	if token == "" {
		if settings.Private {
			return errors.WithHint(
				errors.Newf("no %s token available for private repository", strings.ToUpper(forge)),
				fmt.Sprintf("Set %s or configure %s.auth.env in your config to enable git operations", fallbackEnv, forge),
			)
		}

		debug(settings.Logger, "No credential configured for forge; using unauthenticated git access", "forge", forge)

		return nil
	}

	repo.SetBasicAuth(basicAuthUsername(forge), token)

	return nil
}

// RepoOpt is a functional option for configuring a Repo.
type RepoOpt func(*Repo) error

// WithConfig sets the go-git config for the repository.
func WithConfig(cfg *config.Config) RepoOpt {
	return func(r *Repo) error {
		r.config = cfg

		return nil
	}
}

// NewRepo creates a Repo with the given typed settings and options.
//
// Clone/push authentication is resolved from the selected forge (github,
// gitlab, bitbucket, gitea, codeberg). When SSH settings are configured, SSH
// auth is used; otherwise token auth is resolved via vcs.ResolveToken. Missing
// credentials are only an error when ReleaseSource.Private is true.
func NewRepo(settings Settings, ops ...RepoOpt) (*Repo, error) {
	repo := &Repo{}

	for _, opt := range ops {
		err := opt(repo)
		if err != nil {
			return nil, errors.WithStack(err)
		}
	}

	forge := resolveForge(settings)

	if settings.SSH.Configured {
		if err := configureSSHAuth(repo, settings, forge); err != nil {
			return nil, err
		}
	} else if settings.AuthEnabled || settings.Token != nil {
		if err := configureTokenAuth(repo, settings, forge); err != nil {
			return nil, err
		}
	}

	return repo, nil
}

func (settings Settings) fs() afero.Fs {
	if settings.FS != nil {
		return settings.FS
	}

	return afero.NewOsFs()
}

func debug(log diagnosticLogger, msg string, keyvals ...any) {
	if log != nil {
		log.Debug(msg, keyvals...)
	}
}

func warn(log diagnosticLogger, msg string, keyvals ...any) {
	if log != nil {
		log.Warn(msg, keyvals...)
	}
}
