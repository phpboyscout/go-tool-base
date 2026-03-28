//go:build integration

package repo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"os"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

var (
	// testRepo defaults to a developer-local path; overridden by GITHUB_WORKSPACE in CI.
	testRepo   = "/home/mcockayne/workspace/phpboyscout/gtb"
	testConfig = `github:
  url:
    api: https://api.github.com
    upload: https://uploads.github.com
  auth:
    env: GITHUB_TOKEN
  ssh:
    key:
      env: GITHUB_KEY
train:
  memory:
    enabled: true
    repo: "/home/mcockayne/workspace/phpboyscout/gtb"
  source:
    org: phpboyscout
    repo: gtb
    branch: main
`
	testConfigNoSSH = `github:
  url:
    api: https://api.github.com
    upload: https://uploads.github.com
  auth:
    env: GITHUB_TOKEN
`
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
	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(testConfig))

	localfs := afero.NewMemMapFs()
	_ = afero.WriteFile(localfs, "id_rsa", genTestSSHKey(), 0600)
	_ = os.Setenv("GITHUB_KEY", "id_rsa")

	var assets embed.FS

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfg,
		FS:     localfs,
		Assets: props.NewAssets(props.AssetMap{"test": &assets}),
	}

	return NewRepo(p)
}

func TestCreateBranch(t *testing.T) {
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

func TestPush(t *testing.T) {
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
	repo, err := newTestRepo()
	require.NoError(t, err, "unable to open test repo")

	gitRepo, tree, err := repo.OpenInMemory(testRepo, "")
	require.NoError(t, err, "failed to open repo: %s", testRepo)
	assert.NotNil(t, gitRepo)
	assert.NotNil(t, tree)
}

func TestOpenRemoteHTTP(t *testing.T) {
	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(testConfigNoSSH))

	localfs := afero.NewMemMapFs()

	var assets embed.FS

	props := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfg,
		FS:     localfs,
		Assets: props.NewAssets(props.AssetMap{"test": &assets}),
	}

	// Ensure we have a token for NewRepo to succeed/init
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(props)
	if assert.NoError(t, err) {
		// Clear auth for public repo
		repo.auth = nil
		_, _, err = repo.OpenInMemory("https://github.com/octocat/Hello-World.git", "")
		assert.NoError(t, err, "failed to open repo")
	}
}

func TestCheckoutRemoteBranch(t *testing.T) {
	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(testConfigNoSSH))

	localfs := afero.NewMemMapFs()

	var assets embed.FS

	props := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfg,
		FS:     localfs,
		Assets: props.NewAssets(props.AssetMap{"test": &assets}),
	}

	// Ensure we have a token for NewRepo to succeed/init
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(props)
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
	// Create temporary directory for clone
	tmpDir, err := os.MkdirTemp("", "repo-clone-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test props
	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(testConfigNoSSH))
	propsConfig := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfg,
		FS:     afero.NewMemMapFs(),
	}

	// Ensure we have a token for NewRepo to succeed
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(propsConfig)
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
	// Setup a test repo with some content
	tmpDir, err := os.MkdirTemp("", "repo-file-ops-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Clone a repo that we know has content (using Hello-World for stability)
	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(testConfigNoSSH))
	propsConfig := &props.Props{
		Logger: logger.NewNoop(),
		Config: cfg,
		FS:     afero.NewMemMapFs(),
	}

	// Ensure we have a token for NewRepo to succeed
	t.Setenv("GITHUB_TOKEN", "dummy_token")

	repo, err := NewRepo(propsConfig)
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
