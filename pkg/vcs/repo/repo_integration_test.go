package repo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

var (
	// testRepo defaults to a developer-local path; overridden by GITHUB_WORKSPACE in CI.
	testRepo = "/home/mcockayne/workspace/phpboyscout/gtb"
)

func init() {
	// if in a GH Action replace the testRepo variable
	if workspace, found := os.LookupEnv("GITHUB_WORKSPACE"); found {
		testRepo = workspace
	}
}

func genTestSSHKey() []byte {
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
}

func newTestRepo() (*Repo, error) {
	localfs := afero.NewMemMapFs()
	_ = afero.WriteFile(localfs, "id_rsa", genTestSSHKey(), 0600)
	_ = os.Setenv("GITHUB_KEY", "id_rsa")

	return NewRepo(Settings{
		Forge:  "github",
		SSH:    SSHSettings{Configured: true, HasKey: true, Env: "GITHUB_KEY"},
		Logger: logger.NewNoop(),
		FS:     localfs,
	})
}

func TestCreateBranch(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")
	repo, err := newTestRepo()
	require.NoError(t, err)

	gitRepo, tree, err := repo.OpenInMemory(testRepo, "")
	if assert.NoError(t, err, "failed to open repo: %s", testRepo) {
		assert.NotNil(t, gitRepo)
		assert.NotNil(t, tree)

		repo.SetRepo(gitRepo)
		repo.SetTree(tree)

		err = repo.CreateBranch("new-branch")
		assert.NoError(t, err)

		branches, err := gitRepo.Branches()
		assert.NoError(t, err)

		found := false
		_ = branches.ForEach(func(branch *plumbing.Reference) error {
			if branch.Name().Short() == "new-branch" {
				found = true
			}
			return nil
		})
		assert.True(t, found)
	}
}

// TestCreateBranch_ExistingBranchUpToDate proves that CreateBranch does not
// fail when the post-checkout Pull reports the branch is already up to date.
// go-git surfaces git.NoErrAlreadyUpToDate as a non-nil error in the common
// up-to-date case (see TestPush), which CreateBranch must not treat as failure.
// Uses a network-free local bare remote so the non-memory Pull path runs.
func TestCreateBranch_ExistingBranchUpToDate(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	workDir := filepath.Join(root, "work")

	// Bare remote.
	_, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)

	// Working repo with one commit.
	gitRepo, err := git.PlainInit(workDir, false)
	require.NoError(t, err)

	tree, err := gitRepo.Worktree()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello"), 0o644))
	_, err = tree.Add("README.md")
	require.NoError(t, err)

	_, err = tree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	// Determine the default branch name and wire up origin.
	head, err := gitRepo.Head()
	require.NoError(t, err)
	branchName := head.Name().Short()

	_, err = gitRepo.CreateRemote(&gogitconfig.RemoteConfig{Name: "origin", URLs: []string{remoteDir}})
	require.NoError(t, err)
	require.NoError(t, gitRepo.Push(&git.PushOptions{RemoteName: "origin"}))

	// Build a SourceLocal Repo over the working tree so CreateBranch reaches
	// the branchExists + non-memory Pull path.
	r := &Repo{}
	r.SetRepo(gitRepo)
	r.SetTree(tree)
	r.SetSource(SourceLocal)

	// The branch already exists and is up to date with origin: must succeed.
	err = r.CreateBranch(branchName)
	assert.NoError(t, err, "CreateBranch must not fail when the branch is already up to date")
}

func TestPush(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")
	repo := &Repo{}
	gitRepo, tree, err := repo.OpenInMemory(testRepo, "")
	if assert.NoError(t, err, "failed to open repo: %s", testRepo) {
		repo.SetRepo(gitRepo)
		repo.SetTree(tree)

		err = repo.Push(nil)
		assert.Error(t, err)
		assert.ErrorIs(t, err, git.NoErrAlreadyUpToDate)
	}
}

func TestInMemoryRepo(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")
	repo, err := newTestRepo()
	require.NoError(t, err, "unable to open test repo")

	gitRepo, tree, err := repo.OpenInMemory(testRepo, "")
	require.NoError(t, err, "failed to open repo: %s", testRepo)
	assert.NotNil(t, gitRepo)
	assert.NotNil(t, tree)
}

func TestOpenRemoteHTTP(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	// Ensure we have a token for NewRepo to succeed/init
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(Settings{
		Forge:       "github",
		AuthEnabled: true,
		Logger:      logger.NewNoop(),
		FS:          afero.NewMemMapFs(),
	})
	if assert.NoError(t, err) {
		// Clear auth for public repo
		repo.auth = nil
		_, _, err = repo.OpenInMemory("https://github.com/octocat/Hello-World.git", "")
		assert.NoError(t, err, "failed to open repo")
	}
}

func TestCheckoutRemoteBranch(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	// Ensure we have a token for NewRepo to succeed/init
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(Settings{
		Forge:       "github",
		AuthEnabled: true,
		Logger:      logger.NewNoop(),
		FS:          afero.NewMemMapFs(),
	})
	require.NoError(t, err, "unable to open test repo")

	// Clear auth for public repo
	repo.auth = nil
	_, _, err = repo.OpenInMemory("https://github.com/octocat/Hello-World.git", "")
	require.NoError(t, err, "failed to open repo")
	// Hello-World has a 'master' branch
	err = repo.Checkout(plumbing.NewRemoteReferenceName("origin", "master"))
	assert.NoError(t, err)
}

// TestClone tests the Clone functionality with various options
func TestClone(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")
	// Create temporary directory for clone
	tmpDir, err := os.MkdirTemp("", "repo-clone-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Ensure we have a token for NewRepo to succeed
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(Settings{
		Forge:       "github",
		AuthEnabled: true,
		Logger:      logger.NewNoop(),
		FS:          afero.NewMemMapFs(),
	})
	if err != nil {
		t.Fatalf("Failed to create repo: %v", err)
	}
	// Clear auth for public repo clone
	repo.auth = nil

	// Test basic clone without options
	t.Run("basic_clone", func(t *testing.T) {
		cloneDir := tmpDir + "/basic"
		_, _, err := repo.Clone("https://github.com/octocat/Hello-World.git", cloneDir)
		require.NoError(t, err, "failed to clone repository")

		// Verify repository was cloned correctly
		assert.DirExists(t, cloneDir+"/.git", "git directory should exist")
		assert.FileExists(t, cloneDir+"/README", "README should exist")
	})

	// Test clone with single branch option
	t.Run("clone_with_single_branch", func(t *testing.T) {
		cloneDir := tmpDir + "/single-branch"
		_, _, err := repo.Clone("https://github.com/octocat/Hello-World.git", cloneDir, WithSingleBranch("master"))
		require.NoError(t, err, "failed to clone repository with single branch")

		// Verify repository was cloned correctly
		assert.DirExists(t, cloneDir+"/.git", "git directory should exist")
		assert.FileExists(t, cloneDir+"/README", "README should exist")
	})

	// Test clone with no tags option
	t.Run("clone_with_no_tags", func(t *testing.T) {
		cloneDir := tmpDir + "/no-tags"
		_, _, err := repo.Clone("https://github.com/octocat/Hello-World.git", cloneDir, WithNoTags())
		require.NoError(t, err, "failed to clone repository without tags")

		// Verify repository was cloned correctly
		assert.DirExists(t, cloneDir+"/.git", "git directory should exist")
		assert.FileExists(t, cloneDir+"/README", "README should exist")
	})

	// Test clone with shallow clone option
	t.Run("clone_with_shallow", func(t *testing.T) {
		cloneDir := tmpDir + "/shallow"
		_, _, err := repo.Clone("https://github.com/octocat/Hello-World.git", cloneDir, WithShallowClone(1))
		require.NoError(t, err, "failed to clone repository with shallow option")

		// Verify repository was cloned correctly
		assert.DirExists(t, cloneDir+"/.git", "git directory should exist")
		assert.FileExists(t, cloneDir+"/README", "README should exist")
	})

	// Test clone with combined options
	t.Run("clone_with_combined_options", func(t *testing.T) {
		cloneDir := tmpDir + "/combined"
		_, _, err := repo.Clone("https://github.com/octocat/Hello-World.git", cloneDir,
			WithSingleBranch("master"), WithNoTags(), WithShallowClone(5))
		require.NoError(t, err, "failed to clone repository with combined options")

		// Verify repository was cloned correctly
		assert.DirExists(t, cloneDir+"/.git", "git directory should exist")
		assert.FileExists(t, cloneDir+"/README", "README should exist")
	})
}

func TestFileOperations(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")
	// Setup a test repo with some content
	tmpDir, err := os.MkdirTemp("", "repo-file-ops-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Ensure we have a token for NewRepo to succeed
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(Settings{
		Forge:       "github",
		AuthEnabled: true,
		Logger:      logger.NewNoop(),
		FS:          afero.NewMemMapFs(),
	})
	require.NoError(t, err)
	// Clear auth for public repo clone
	repo.auth = nil

	_, _, err = repo.Clone("https://github.com/octocat/Hello-World.git", tmpDir)
	require.NoError(t, err)

	t.Run("FileExists", func(t *testing.T) {
		exists, err := repo.FileExists("README")
		require.NoError(t, err)
		assert.True(t, exists, "README should exist")

		exists, err = repo.FileExists("nonexistent-file")
		require.NoError(t, err)
		assert.False(t, exists, "nonexistent-file should not exist")
	})

	t.Run("DirectoryExists", func(t *testing.T) {
		// Hello-World is flat, let's try a repo with dirs or just check root
		exists, err := repo.DirectoryExists("")
		require.NoError(t, err)
		assert.True(t, exists, "root directory should exist")

		// We can try to check a non-existent dir
		exists, err = repo.DirectoryExists("nonexistent-dir")
		require.NoError(t, err)
		assert.False(t, exists, "nonexistent-dir should not exist")
	})

	t.Run("WalkTree", func(t *testing.T) {
		foundReadme := false
		err := repo.WalkTree(func(f *object.File) error {
			if f.Name == "README" {
				foundReadme = true
			}
			return nil
		})
		require.NoError(t, err)
		assert.True(t, foundReadme, "WalkTree should find README")
	})

	t.Run("AddToFS", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		file, err := repo.GetFile("README")
		require.NoError(t, err)

		err = repo.AddToFS(fs, file, "/README")
		require.NoError(t, err)

		exists, _ := afero.Exists(fs, "/README")
		assert.True(t, exists, "README should be added to FS")
	})
}

// TestForgeAuthClonePush proves the provider-aware auth path end-to-end:
// NewRepo configured for each forge's token shape can clone from and push to
// a local bare remote. The file transport ignores credentials, so this
// exercises the full NewRepo wiring (forge selection, token resolution,
// BasicAuth construction) around real clone/commit/push operations.
func TestForgeAuthClonePush(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	forges := []struct {
		forge    string
		envName  string
		dummy    string
		username string
	}{
		{
			forge:    "github",
			envName:  "GTB_INT_GH_TOKEN",
			dummy:    "ghp_integration",
			username: "x-access-token",
		},
		{
			forge:    "gitlab",
			envName:  "GTB_INT_GL_TOKEN",
			dummy:    "glpat-integration",
			username: "oauth2",
		},
	}

	for _, tc := range forges {
		t.Run(tc.forge, func(t *testing.T) {
			t.Setenv(tc.envName, tc.dummy)

			root := t.TempDir()
			remoteDir := filepath.Join(root, "remote.git")
			seedDir := filepath.Join(root, "seed")
			cloneDir := filepath.Join(root, "clone")

			// Bare remote seeded with one commit.
			_, err := git.PlainInit(remoteDir, true)
			require.NoError(t, err)

			seedRepo, err := git.PlainInit(seedDir, false)
			require.NoError(t, err)
			seedTree, err := seedRepo.Worktree()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(seedDir, "README.md"), []byte("seed"), 0o644))
			_, err = seedTree.Add("README.md")
			require.NoError(t, err)
			_, err = seedTree.Commit("seed", &git.CommitOptions{
				Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
			})
			require.NoError(t, err)
			_, err = seedRepo.CreateRemote(&gogitconfig.RemoteConfig{Name: "origin", URLs: []string{remoteDir}})
			require.NoError(t, err)
			require.NoError(t, seedRepo.Push(&git.PushOptions{RemoteName: "origin"}))

			// Forge-configured Repo: clone, commit, push.
			r, err := NewRepo(Settings{
				Forge:  tc.forge,
				Token:  func() string { return os.Getenv(tc.envName) },
				Logger: logger.NewNoop(),
				FS:     afero.NewOsFs(),
			})
			require.NoError(t, err)

			auth, ok := r.GetAuth().(*githttp.BasicAuth)
			require.True(t, ok, "expected BasicAuth from forge token config")
			assert.Equal(t, tc.username, auth.Username)
			assert.Equal(t, tc.dummy, auth.Password)

			_, _, err = r.Clone(remoteDir, cloneDir)
			require.NoError(t, err)

			require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "new.txt"), []byte("new"), 0o644))
			err = r.WithTree(func(wt *git.Worktree) error {
				_, addErr := wt.Add("new.txt")
				return addErr
			})
			require.NoError(t, err)
			_, err = r.Commit("add new.txt", &git.CommitOptions{
				Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
			})
			require.NoError(t, err)

			require.NoError(t, r.Push(nil))
		})
	}
}

// TestInitAddPushForgeAuth proves the generator's scaffold-and-push sequence
// end-to-end against a network-free local bare remote: a forge-configured Repo
// inits a fresh tree, stages it honouring .gitignore, commits, adds the remote
// as origin, and pushes — the exact InitLocal/AddAll/CreateRemote/Push flow the
// generator's git step performs, with provider-aware auth selected by NewRepo.
func TestInitAddPushForgeAuth(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "vcs")

	t.Setenv("GTB_INT_GH_TOKEN", "ghp_integration")

	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	workDir := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	_, err := git.PlainInit(remoteDir, true)
	require.NoError(t, err)

	// Seed a scaffold tree with a .gitignore and an ignored artefact.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".gitignore"), []byte("*.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "build.log"), []byte("noise\n"), 0o644))

	r, err := NewRepo(Settings{
		Forge:  "github",
		Token:  func() string { return os.Getenv("GTB_INT_GH_TOKEN") },
		Logger: logger.NewNoop(),
		FS:     afero.NewOsFs(),
	})
	require.NoError(t, err)

	auth, ok := r.GetAuth().(*githttp.BasicAuth)
	require.True(t, ok, "expected provider-aware BasicAuth")
	assert.Equal(t, "x-access-token", auth.Username)

	_, _, err = r.InitLocal(workDir, "main")
	require.NoError(t, err)
	require.NoError(t, r.AddAll())
	_, err = r.Commit("chore: scaffold widget with gtb", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})
	require.NoError(t, err)

	_, err = r.CreateRemote("origin", []string{remoteDir})
	require.NoError(t, err)

	refspec := gogitconfig.RefSpec("refs/heads/main:refs/heads/main")
	require.NoError(t, r.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []gogitconfig.RefSpec{refspec}}))

	// Remote received the commit on main; ignored artefact absent.
	remote, err := git.PlainOpen(remoteDir)
	require.NoError(t, err)
	ref, err := remote.Reference("refs/heads/main", false)
	require.NoError(t, err)

	commit, err := remote.CommitObject(ref.Hash())
	require.NoError(t, err)
	tree, err := commit.Tree()
	require.NoError(t, err)

	_, err = tree.File("main.go")
	require.NoError(t, err)
	_, err = tree.File("build.log")
	require.Error(t, err, "ignored *.log must not have been pushed")
}
