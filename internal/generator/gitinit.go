package generator

import (
	"fmt"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/repo"

	gtbrepo "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo"
)

const (
	// defaultGitBranch is the framework default branch the initial commit lands
	// on. It matches the rendered CI (releaser-pleaser operates on main).
	defaultGitBranch = "main"

	// fallbackAuthorName / fallbackAuthorEmail are the last-resort commit
	// identity when neither the host git config nor a GTB config key yields one.
	// The email is a non-routable placeholder so the commit never carries a real
	// address the operator did not provide.
	fallbackAuthorName  = "gtb"
	fallbackAuthorEmail = "gtb@localhost"
)

// gitAuthor is a resolved commit identity.
type gitAuthor struct {
	Name  string
	Email string
}

// runSkeletonGitInit performs the post-generation git step on a freshly
// scaffolded tree: when the destination is not already a git repository, it
// initialises one on the configured default branch, stages the tree honouring
// .gitignore, and records a single conventional initial commit. When push is
// requested it then adds the derived remote as origin and pushes.
//
// The whole step is best-effort: the skeleton has already been written, so any
// git failure is logged as a warning and generation still succeeds. It is only
// ever called against the real OS filesystem and never under --dry-run.
func (g *Generator) runSkeletonGitInit(config SkeletonConfig) {
	if !g.config.GitInit {
		g.props.Logger.Debug("Skipping git initialisation (--no-git)")

		return
	}

	found, err := repo.DiscoverRepository(config.Path)
	if err != nil {
		g.props.Logger.Warn("Failed to probe for an existing git repository; skipping git initialisation", "error", err)

		return
	}

	if found {
		g.props.Logger.Info("destination is already inside a git repository; skipping git initialisation", "path", config.Path)

		return
	}

	branch := g.config.GitBranch
	if branch == "" {
		branch = defaultGitBranch
	}

	g.props.Logger.Info("initialising git repository", "branch", branch)

	r, err := gtbrepo.NewRepoFromProps(g.props)
	if err != nil {
		g.props.Logger.Warn("Failed to construct repository; skipping git initialisation", "error", err)

		return
	}

	if _, _, err := r.InitLocal(config.Path, branch); err != nil {
		g.props.Logger.Warn("Failed to initialise git repository; skipping initial commit", "error", err)

		return
	}

	if !g.makeInitialCommit(r, config, branch) {
		return
	}

	if g.config.GitPush {
		g.pushInitialCommit(r, config, branch)
	}
}

// makeInitialCommit stages the worktree and records the single scaffold commit.
// It returns false (and logs a warning) when staging or committing fails so the
// caller can skip a subsequent push.
func (g *Generator) makeInitialCommit(r *repo.Repo, config SkeletonConfig, branch string) bool {
	if err := r.AddAll(); err != nil {
		g.props.Logger.Warn("Failed to stage skeleton for the initial commit", "error", err)

		return false
	}

	author := g.resolveGitAuthor(config.Path)
	sig := &object.Signature{Name: author.Name, Email: author.Email, When: time.Now()}

	hash, err := r.Commit(initialCommitMessage(config.Name, g.currentVersion()), &git.CommitOptions{Author: sig})
	if err != nil {
		g.props.Logger.Warn("Failed to record the initial commit", "error", err)

		return false
	}

	g.props.Logger.Info("created initial commit", "commit", hash.String()[:7], "branch", branch)

	return true
}

// pushInitialCommit derives the remote from the release source, adds it as
// origin, and pushes the default branch. Any failure (remote absent, auth
// missing, network down) degrades to an actionable warning — push never fails
// generation, and creating the remote on the forge is out of scope.
func (g *Generator) pushInitialCommit(r *repo.Repo, config SkeletonConfig, branch string) {
	remoteURL, err := remoteURLFromConfig(config)
	if err != nil {
		g.props.Logger.Warn("Could not derive a remote URL from the release source; skipping push", "error", err)

		return
	}

	if _, err := r.CreateRemote("origin", []string{remoteURL}); err != nil {
		g.props.Logger.Warn("Failed to add origin remote; skipping push", "error", err, "remote", remoteURL)

		return
	}

	g.props.Logger.Info("pushing branch", "branch", branch, "remote", remoteURL)

	refspec := gitconfig.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))

	if err := r.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{refspec},
	}); err != nil {
		g.props.Logger.Warn(fmt.Sprintf(
			"Failed to push to %s: %v. Create the repository on the forge, then run: git push -u origin %s",
			remoteURL, err, branch,
		))

		return
	}

	g.props.Logger.Info("pushed branch", "branch", branch, "remote", remoteURL)
}

// resolveGitAuthor resolves the initial-commit identity, in order:
//  1. the host git config (repo-local, then global, then system user.{name,email});
//  2. the GTB config keys generator.git.author.{name,email};
//  3. a safe framework fallback so the commit never fails for lack of identity.
//
// go-git does not read the host user.* config automatically, so it is read
// explicitly here.
func (g *Generator) resolveGitAuthor(projectPath string) gitAuthor {
	if a, ok := hostGitAuthor(g.props.FS, projectPath); ok {
		return a
	}

	if a, ok := g.gtbConfigAuthor(); ok {
		return a
	}

	return gitAuthor{Name: fallbackAuthorName, Email: fallbackAuthorEmail}
}

// gtbConfigAuthor reads the generator.git.author.{name,email} GTB config keys.
func (g *Generator) gtbConfigAuthor() (gitAuthor, bool) {
	if g.props.Config == nil {
		return gitAuthor{}, false
	}

	name := strings.TrimSpace(g.props.Config.View().GetString("generator.git.author.name"))
	email := strings.TrimSpace(g.props.Config.View().GetString("generator.git.author.email"))

	if name != "" && email != "" {
		return gitAuthor{Name: name, Email: email}, true
	}

	return gitAuthor{}, false
}

// hostGitAuthor reads user.{name,email} from the host git configuration. The
// repo-local config (projectPath/.git/config) takes precedence over the global
// and system scopes, mirroring git's own precedence.
func hostGitAuthor(fs afero.Fs, projectPath string) (gitAuthor, bool) {
	if a, ok := localGitAuthor(fs, projectPath); ok {
		return a, true
	}

	for _, scope := range []gitconfig.Scope{gitconfig.GlobalScope, gitconfig.SystemScope} {
		cfg, err := gitconfig.LoadConfig(scope)
		if err != nil {
			continue
		}

		if cfg.User.Name != "" && cfg.User.Email != "" {
			return gitAuthor{Name: cfg.User.Name, Email: cfg.User.Email}, true
		}
	}

	return gitAuthor{}, false
}

// localGitAuthor reads user.{name,email} from the repository-local git config,
// if present, via the injected filesystem so the lookup is testable.
func localGitAuthor(fs afero.Fs, projectPath string) (gitAuthor, bool) {
	raw, err := afero.ReadFile(fs, projectPath+"/.git/config")
	if err != nil {
		return gitAuthor{}, false
	}

	cfg := gitconfig.NewConfig()
	if err := cfg.Unmarshal(raw); err != nil {
		return gitAuthor{}, false
	}

	if cfg.User.Name != "" && cfg.User.Email != "" {
		return gitAuthor{Name: cfg.User.Name, Email: cfg.User.Email}, true
	}

	return gitAuthor{}, false
}

// repoDiscover is a thin indirection over repo.DiscoverRepository so the
// dry-run preview and the write path share one probe.
func repoDiscover(path string) (bool, error) {
	return repo.DiscoverRepository(path)
}

// initialCommitSubject is the conventional, non-releasing scaffold subject.
func initialCommitSubject(toolName string) string {
	return fmt.Sprintf("chore: scaffold %s with gtb", toolName)
}

// initialCommitMessage builds the conventional scaffold commit message. The
// chore: type is non-releasing so the empty scaffold never makes
// releaser-pleaser cut a release.
func initialCommitMessage(toolName, version string) string {
	subject := initialCommitSubject(toolName)

	if version == "" {
		return subject
	}

	return fmt.Sprintf("%s\n\nGenerated by gtb %s.", subject, version)
}

// remoteURLFromConfig derives the https remote URL from the release source the
// generator already collected: https://{host}/{owner}/{repo}.git. The owner
// segment preserves GitLab nested group paths (group/subgroup).
func remoteURLFromConfig(config SkeletonConfig) (string, error) {
	host := config.Host
	if host == "" {
		host = "github.com"
	}

	org, repoName, err := splitRepoPath(config.Repo)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://%s/%s/%s.git", host, org, repoName), nil
}
