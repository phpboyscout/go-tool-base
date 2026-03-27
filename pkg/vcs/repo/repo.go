package repo

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/phpboyscout/go-tool-base/pkg/props"
	githubvcs "github.com/phpboyscout/go-tool-base/pkg/vcs/github"

	"github.com/charmbracelet/huh"
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

type RepoType = string

var (
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

type RepoLike interface {
	SourceIs(int) bool
	SetSource(int)
	SetRepo(*git.Repository)
	SetKey(*ssh.PublicKeys)
	SetBasicAuth(string, string)
	GetAuth() transport.AuthMethod
	SetTree(*git.Worktree)
	Checkout(plumbing.ReferenceName) error
	CheckoutCommit(plumbing.Hash) error
	CreateBranch(string) error
	Push(*git.PushOptions) error
	Commit(string, *git.CommitOptions) (plumbing.Hash, error)

	OpenInMemory(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
	OpenLocal(string, string) (*git.Repository, *git.Worktree, error)
	Open(RepoType, string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)
	Clone(string, string, ...CloneOption) (*git.Repository, *git.Worktree, error)

	// Git tree operations for in-memory repositories
	WalkTree(func(*object.File) error) error
	FileExists(string) (bool, error)
	DirectoryExists(string) (bool, error)
	GetFile(string) (*object.File, error)
	AddToFS(fs afero.Fs, gitFile *object.File, fullPath string) error

	// WithRepo calls fn with the underlying *git.Repository.
	// Returns ErrNoRepository if the repository has not been initialised.
	WithRepo(func(*git.Repository) error) error

	// WithTree calls fn with the underlying *git.Worktree.
	// Returns ErrNoWorktree if the worktree has not been initialised.
	WithTree(func(*git.Worktree) error) error
}

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

func (r *Repo) Checkout(branch plumbing.ReferenceName) error {
	return r.tree.Checkout(&git.CheckoutOptions{
		Branch: branch,
	})
}

// CheckoutCommit checks out a specific commit by hash.
func (r *Repo) CheckoutCommit(hash plumbing.Hash) error {
	return r.tree.Checkout(&git.CheckoutOptions{
		Hash: hash,
	})
}

// CreateBranch creates a branch in the git tree.
func (r *Repo) CreateBranch(branchName string) error {
	bref := plumbing.NewBranchReferenceName(branchName)

	branchExists := false

	branches, err := r.repo.Branches()
	if err != nil {
		return errors.WithStack(err)
	}

	if err = branches.ForEach(func(branch *plumbing.Reference) error {
		if branch.Name().Short() == bref.Short() {
			branchExists = true

			return nil
		}

		return nil
	}); err != nil {
		return errors.WithStack(err)
	}

	if err := r.tree.Checkout(&git.CheckoutOptions{
		Create: !branchExists,
		Branch: bref,
	}); err != nil {
		return errors.WithStack(err)
	}

	if branchExists && !r.SourceIs(SourceMemory) {
		// make sure we pull the latest changes
		if err := r.tree.Pull(&git.PullOptions{
			Auth:     r.auth,
			Force:    true,
			Progress: gitProgressOutput,
		}); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (r *Repo) Push(opts *git.PushOptions) error {
	if opts == nil {
		opts = &git.PushOptions{}
	}

	if opts.Auth == nil {
		opts.Auth = r.auth
	}

	return r.repo.Push(opts)
}

func (r *Repo) Commit(commitMsg string, opts *git.CommitOptions) (plumbing.Hash, error) {
	if opts == nil {
		opts = &git.CommitOptions{}
	}

	return r.tree.Commit(commitMsg, opts)
}

func (r *Repo) CreateRemote(name string, urls []string) (*git.Remote, error) {
	return r.repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: urls,
	})
}

func (r *Repo) Remote(name string) (*git.Remote, error) {
	return r.repo.Remote(name)
}

func GetSSHKey(filePath string, localfs afero.Fs) (*ssh.PublicKeys, error) {
	fileHandle, err := localfs.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, errors.WithStack(err)
	}

	if fileHandle.IsDir() {
		return nil, errors.Newf("Could not open SSH key at '%s', make sure the GITHUB_KEY environment variable is set", filePath)
	}

	sshKey, err := afero.ReadFile(localfs, filePath)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	passphrase := ""

	_, err = gossh.ParsePrivateKey(sshKey)
	if err != nil && strings.Contains(err.Error(), "passphrase protected") {
		// If the error indicates an encrypted key, it has a passphrase
		err := huh.NewInput().
			Title("Please enter your SSH Key passphrase").
			EchoMode(huh.EchoModePassword).
			Value(&passphrase).
			Run()
		if err != nil {
			return nil, errors.WithStack(err)
		}
	}

	return ssh.NewPublicKeys("git", sshKey, passphrase)
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
	switch strings.ToLower(repoType) {
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

// WalkTree walks the git tree and calls the provided function for each file.
func (r *Repo) WalkTree(fn func(*object.File) error) error {
	// Get the current HEAD commit
	head, err := r.repo.Head()
	if err != nil {
		return errors.Wrap(err, "failed to get HEAD reference")
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return errors.Wrap(err, "failed to get HEAD commit")
	}

	// Get the tree from the commit
	tree, err := commit.Tree()
	if err != nil {
		return errors.Wrap(err, "failed to get commit tree")
	}

	// Walk the git tree and call the provided function for each file
	return tree.Files().ForEach(fn)
}

// FileExists checks if a file exists in the git repository at the given relative path.
func (r *Repo) FileExists(relPath string) (bool, error) {
	// Get the current HEAD commit
	head, err := r.repo.Head()
	if err != nil {
		return false, errors.Wrap(err, "failed to get HEAD reference")
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return false, errors.Wrap(err, "failed to get HEAD commit")
	}

	// Get the tree from the commit
	tree, err := commit.Tree()
	if err != nil {
		return false, errors.Wrap(err, "failed to get commit tree")
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
func (r *Repo) DirectoryExists(relPath string) (bool, error) {
	// Get the current HEAD commit
	head, err := r.repo.Head()
	if err != nil {
		return false, errors.Wrap(err, "failed to get HEAD reference")
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return false, errors.Wrap(err, "failed to get HEAD commit")
	}

	// Get the tree from the commit
	tree, err := commit.Tree()
	if err != nil {
		return false, errors.Wrap(err, "failed to get commit tree")
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
func (r *Repo) GetFile(relPath string) (*object.File, error) {
	// Get the current HEAD commit
	head, err := r.repo.Head()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get HEAD reference")
	}

	commit, err := r.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, errors.Wrap(err, "failed to get HEAD commit")
	}

	// Get the tree from the commit
	tree, err := commit.Tree()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get commit tree")
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

func configureSSHAuth(repo *Repo, props *props.Props) error {
	sshCfg := props.Config.Sub("github.ssh.key")

	if sshCfg.GetString("type") == "agent" {
		return setSSHAgent(repo)
	}

	sshPath := filepath.Clean(os.Getenv(sshCfg.GetString("env")))
	if sshCfg.Has("path") {
		sshPath = filepath.Clean(sshCfg.GetString("path"))
	}

	if sshPath == "" || sshPath == "." {
		props.Logger.Warn("No SSH key defined via config or GITHUB_KEY environment variable, defaulting to ssh-agent")

		return setSSHAgent(repo)
	}

	publicKey, err := GetSSHKey(sshPath, props.FS)
	if err != nil {
		return errors.Newf("failed to get SSH key: %w", err)
	}

	repo.auth = publicKey

	return nil
}

func configureTokenAuth(repo *Repo, props *props.Props) error {
	props.Logger.Warn("No SSH keys defined, defaulting to GITHUB_TOKEN")

	token, err := githubvcs.GetGitHubToken(props.Config.Sub("github"))
	if err != nil {
		return errors.Newf("failed to get GitHub token: %w", err)
	}

	repo.SetBasicAuth("x-access-token", token)

	return nil
}

type RepoOpt func(*Repo) error

func WithConfig(cfg *config.Config) RepoOpt {
	return func(r *Repo) error {
		r.config = cfg

		return nil
	}
}

func NewRepo(props *props.Props, ops ...RepoOpt) (*Repo, error) {
	repo := &Repo{}

	for _, opt := range ops {
		err := opt(repo)
		if err != nil {
			return nil, errors.WithStack(err)
		}
	}

	if props.Config == nil {
		return repo, nil
	}

	if props.Config.Has("github.ssh") {
		if err := configureSSHAuth(repo, props); err != nil {
			return nil, err
		}
	} else {
		if err := configureTokenAuth(repo, props); err != nil {
			return nil, err
		}
	}

	return repo, nil
}
