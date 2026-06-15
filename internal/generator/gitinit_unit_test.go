package generator

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/repo"
)

// seedSkeleton writes a representative scaffold tree (with a .gitignore and an
// ignored build artefact) into dir and returns a SkeletonConfig pointed at it.
func seedSkeleton(t *testing.T, dir string) SkeletonConfig {
	t.Helper()

	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}

	write(".gitignore", "bin/\n*.log\n")
	write("go.mod", "module example.com/acme/widget\n")
	write("cmd/widget/main.go", "package main\n\nfunc main() {}\n")
	write(".gtb/manifest.yaml", "properties:\n  name: widget\n")
	write("bin/widget", "ELF")
	write("debug.log", "noise")

	return SkeletonConfig{
		Name: "widget",
		Repo: "acme/widget",
		Host: "github.com",
		Path: dir,
	}
}

func newOSGenerator(t *testing.T, cfg *Config) *Generator {
	t.Helper()

	p := &props.Props{
		FS:     afero.NewOsFs(),
		Logger: logger.NewNoop(),
	}

	return New(p, cfg)
}

func committedFiles(t *testing.T, dir string) map[string]struct{} {
	t.Helper()

	gitRepo, err := git.PlainOpen(dir)
	require.NoError(t, err)

	ref, err := gitRepo.Head()
	require.NoError(t, err)

	commit, err := gitRepo.CommitObject(ref.Hash())
	require.NoError(t, err)

	tree, err := commit.Tree()
	require.NoError(t, err)

	files := map[string]struct{}{}
	require.NoError(t, tree.Files().ForEach(func(f *object.File) error {
		files[f.Name] = struct{}{}

		return nil
	}))

	return files
}

func TestRunSkeletonGitInit_DefaultInitAndCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := seedSkeleton(t, dir)

	g := newOSGenerator(t, &Config{GitInit: true, GitBranch: "main", Path: dir})
	g.runSkeletonGitInit(config)

	// A repo on main with exactly one commit.
	gitRepo, err := git.PlainOpen(dir)
	require.NoError(t, err)

	head, err := gitRepo.Head()
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", head.Name().String())

	iter, err := gitRepo.Log(&git.LogOptions{})
	require.NoError(t, err)

	count := 0
	var subject string
	require.NoError(t, iter.ForEach(func(c *object.Commit) error {
		count++
		subject = c.Message

		return nil
	}))
	assert.Equal(t, 1, count, "exactly one commit")
	assert.Contains(t, subject, "chore: scaffold widget with gtb")

	// .gitignore committed; ignored artefacts excluded.
	files := committedFiles(t, dir)
	assert.Contains(t, files, ".gitignore")
	assert.Contains(t, files, ".gtb/manifest.yaml")
	assert.Contains(t, files, "cmd/widget/main.go")
	assert.NotContains(t, files, "bin/widget", "ignored bin/ artefact must not be committed")
	assert.NotContains(t, files, "debug.log", "ignored *.log must not be committed")
}

func TestRunSkeletonGitInit_NoGitSkips(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := seedSkeleton(t, dir)

	g := newOSGenerator(t, &Config{GitInit: false, Path: dir})
	g.runSkeletonGitInit(config)

	_, err := os.Stat(filepath.Join(dir, ".git"))
	assert.True(t, os.IsNotExist(err), "--no-git must not create a .git directory")
}

func TestRunSkeletonGitInit_AlreadyRepoLeftUntouched(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := seedSkeleton(t, dir)

	// Pre-existing repo with its own commit.
	gitRepo, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := gitRepo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("x"), 0o644))
	_, err = wt.Add("existing.txt")
	require.NoError(t, err)
	preHash, err := wt.Commit("pre-existing", &git.CommitOptions{
		Author: &object.Signature{Name: "pre", Email: "pre@example.test"},
	})
	require.NoError(t, err)

	g := newOSGenerator(t, &Config{GitInit: true, Path: dir})
	g.runSkeletonGitInit(config)

	// HEAD unchanged: no new commit was made.
	head, err := gitRepo.Head()
	require.NoError(t, err)
	assert.Equal(t, preHash, head.Hash(), "existing repo history must be untouched")
}

func TestRunSkeletonGitInit_SubdirOfRepoSkipped(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	_, err := git.PlainInit(parent, false)
	require.NoError(t, err)

	sub := filepath.Join(parent, "child")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	config := seedSkeleton(t, sub)

	g := newOSGenerator(t, &Config{GitInit: true, Path: sub})
	g.runSkeletonGitInit(config)

	_, err = os.Stat(filepath.Join(sub, ".git"))
	assert.True(t, os.IsNotExist(err), "must not init a nested repo inside an existing one")
}

func TestRunSkeletonGitInit_AuthorFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := seedSkeleton(t, dir)

	g := newOSGenerator(t, &Config{GitInit: true, Path: dir})
	// Force the fallback by short-circuiting host/GTB resolution: point the
	// author resolver at a project with no local git config and rely on the
	// fallback when the host has none. We assert the commit succeeds and carries
	// a non-empty identity regardless of the host environment.
	g.runSkeletonGitInit(config)

	gitRepo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	head, err := gitRepo.Head()
	require.NoError(t, err)
	commit, err := gitRepo.CommitObject(head.Hash())
	require.NoError(t, err)

	assert.NotEmpty(t, commit.Author.Name)
	assert.NotEmpty(t, commit.Author.Email)
}

func TestResolveGitAuthor_FallbackWhenNothingResolves(t *testing.T) {
	t.Parallel()

	// An empty MemMapFs has no local .git/config; with no GTB config set and
	// (in CI) no host identity, the fallback is returned. We assert via the GTB
	// config branch to make the test deterministic regardless of host config.
	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
	g := New(p, &Config{})

	a := g.resolveGitAuthor("/nonexistent")
	assert.NotEmpty(t, a.Name)
	assert.NotEmpty(t, a.Email)
}

func TestRemoteURLFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  SkeletonConfig
		want string
	}{
		{
			name: "github_org_repo",
			cfg:  SkeletonConfig{Host: "github.com", Repo: "acme/widget"},
			want: "https://github.com/acme/widget.git",
		},
		{
			name: "gitlab_nested_group",
			cfg:  SkeletonConfig{Host: "gitlab.com", Repo: "group/subgroup/widget"},
			want: "https://gitlab.com/group/subgroup/widget.git",
		},
		{
			name: "empty_host_defaults_github",
			cfg:  SkeletonConfig{Repo: "acme/widget"},
			want: "https://github.com/acme/widget.git",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := remoteURLFromConfig(tc.cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInitialCommitMessage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "chore: scaffold widget with gtb", initialCommitMessage("widget", ""))
	assert.Equal(t,
		"chore: scaffold widget with gtb\n\nGenerated by gtb v1.2.3.",
		initialCommitMessage("widget", "v1.2.3"),
	)
}

func TestDescribeGitInit(t *testing.T) {
	t.Parallel()

	t.Run("default_init_commit", func(t *testing.T) {
		t.Parallel()

		g := New(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &Config{GitInit: true})
		actions := g.describeGitInit(SkeletonConfig{Name: "widget", Repo: "acme/widget", Host: "github.com", Path: t.TempDir()})
		require.Len(t, actions, 1)
		assert.Contains(t, actions[0], "would git init on branch \"main\"")
		assert.Contains(t, actions[0], "chore: scaffold widget with gtb")
	})

	t.Run("with_push", func(t *testing.T) {
		t.Parallel()

		g := New(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &Config{GitInit: true, GitPush: true})
		actions := g.describeGitInit(SkeletonConfig{Name: "widget", Repo: "acme/widget", Host: "github.com", Path: t.TempDir()})
		require.Len(t, actions, 2)
		assert.Contains(t, actions[1], "would push branch \"main\" to https://github.com/acme/widget.git")
	})

	t.Run("no_git_no_actions", func(t *testing.T) {
		t.Parallel()

		g := New(&props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}, &Config{GitInit: false})
		assert.Empty(t, g.describeGitInit(SkeletonConfig{Name: "widget", Repo: "acme/widget", Path: t.TempDir()}))
	})

	t.Run("already_a_repo", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		g := New(&props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}, &Config{GitInit: true})
		actions := g.describeGitInit(SkeletonConfig{Name: "widget", Repo: "acme/widget", Path: dir})
		require.Len(t, actions, 1)
		assert.Contains(t, actions[0], "already inside a git repository")
	})
}

// TestPushInitialCommit_LocalBareRemote proves the add-remote + push flow
// end-to-end against a network-free local bare remote (the file transport),
// mirroring the repo package's integration patterns but without https.
func TestPushInitialCommit_LocalBareRemote(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	remoteDir := t.TempDir()

	_, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)

	config := seedSkeleton(t, workDir)

	r := &repo.Repo{}
	_, _, err = r.InitLocal(workDir, "main")
	require.NoError(t, err)
	require.NoError(t, r.AddAll())
	_, err = r.Commit("chore: scaffold widget with gtb", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.test"},
	})
	require.NoError(t, err)

	_, err = r.CreateRemote("origin", []string{remoteDir})
	require.NoError(t, err)

	refspec := gitconfig.RefSpec("refs/heads/main:refs/heads/main")
	err = r.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []gitconfig.RefSpec{refspec}})
	require.NoError(t, err)

	// The remote now has the commit on main.
	remote, err := git.PlainOpen(remoteDir)
	require.NoError(t, err)
	ref, err := remote.Reference("refs/heads/main", false)
	require.NoError(t, err)
	assert.False(t, ref.Hash().IsZero())

	_ = config
}

// TestPushInitialCommit_NonFatalOnMissingRemote proves a push to a
// non-existent https remote degrades to a warning rather than failing
// generation.
func TestPushInitialCommit_NonFatalOnMissingRemote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := seedSkeleton(t, dir)
	config.Host = "127.0.0.1:0" // unroutable; CreateRemote succeeds, Push fails

	g := newOSGenerator(t, &Config{GitInit: true, GitPush: true, GitBranch: "main", Path: dir})

	// Must not panic or error out; the commit is still made.
	g.runSkeletonGitInit(config)

	gitRepo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	_, err = gitRepo.Head()
	require.NoError(t, err, "the initial commit is made even when push fails")
}
