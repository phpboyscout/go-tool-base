package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestRepo_Unit_OpenLocal(t *testing.T) {
	tmpDir := t.TempDir()
	p := &props.Props{
		FS:     afero.NewOsFs(),
		Logger: logger.NewNoop(),
		Config: nil,
	}

	r, err := NewRepo(p)
	require.NoError(t, err)

	t.Run("init_new_repo", func(t *testing.T) {
		repo, tree, err := r.OpenLocal(tmpDir, "main")
		require.NoError(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, tree)
	})

	t.Run("open_existing_repo", func(t *testing.T) {
		repo, tree, err := r.OpenLocal(tmpDir, "main")
		require.NoError(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, tree)
	})
}

func TestRepo_Unit_GitOperations(t *testing.T) {
	tmpDir := t.TempDir()
	p := &props.Props{
		FS:     afero.NewOsFs(),
		Logger: logger.NewNoop(),
		Config: nil,
	}

	r, _ := NewRepo(p)
	_, wt, _ := r.OpenLocal(tmpDir, "main")

	// Create a dummy file for initial commit to ensure HEAD exists
	_ = os.WriteFile(filepath.Join(tmpDir, ".initial"), []byte("init"), 0644)
	_, _ = wt.Add(".initial")
	_, _ = r.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})

	// Create and commit a file
	relPath := "test.txt"
	absPath := filepath.Join(tmpDir, relPath)
	_ = os.WriteFile(absPath, []byte("hello"), 0644)
	_, _ = wt.Add(relPath)
	_, _ = r.Commit("test commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()},
	})

	t.Run("FileExists", func(t *testing.T) {
		exists, err := r.FileExists(relPath)
		require.NoError(t, err)
		assert.True(t, exists)

		exists, err = r.FileExists("missing.txt")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("DirectoryExists", func(t *testing.T) {
		exists, err := r.DirectoryExists("")
		require.NoError(t, err)
		assert.True(t, exists)

		// Create a file in a subdirectory
		subDir := "subdir"
		_ = os.Mkdir(filepath.Join(tmpDir, subDir), 0755)
		subFile := filepath.Join(subDir, "file.txt")
		_ = os.WriteFile(filepath.Join(tmpDir, subFile), []byte("sub"), 0644)
		_, _ = wt.Add(subFile)
		_, _ = r.Commit("subdir commit", &git.CommitOptions{
			Author: &object.Signature{Name: "T", Email: "e", When: time.Now()},
		})

		exists, err = r.DirectoryExists(subDir)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("GetFile", func(t *testing.T) {
		file, err := r.GetFile(relPath)
		require.NoError(t, err)
		assert.NotNil(t, file)
		assert.Equal(t, relPath, file.Name)
	})

	t.Run("AddToFS", func(t *testing.T) {
		memFS := afero.NewMemMapFs()
		file, _ := r.GetFile(relPath)
		targetPath := "/copied.txt"

		err := r.AddToFS(memFS, file, targetPath)
		require.NoError(t, err)

		content, _ := afero.ReadFile(memFS, targetPath)
		assert.Equal(t, "hello", string(content))
	})

	t.Run("WalkTree", func(t *testing.T) {
		var files []string
		err := r.WalkTree(func(f *object.File) error {
			files = append(files, f.Name)
			return nil
		})
		require.NoError(t, err)
		assert.Contains(t, files, relPath)
	})

	t.Run("CheckoutAndCreateBranch", func(t *testing.T) {
		err := r.CreateBranch("feature")
		require.NoError(t, err)

		head, _ := r.repo.Head()
		assert.Equal(t, "refs/heads/feature", head.Name().String())

		err = r.Checkout(plumbing.NewBranchReferenceName("main"))
		require.NoError(t, err)

		head, _ = r.repo.Head()
		assert.Equal(t, "refs/heads/main", head.Name().String())
	})
}

func TestRepo_Unit_AuthConfig(t *testing.T) {
	fs := afero.NewMemMapFs()

	t.Run("token_auth", func(t *testing.T) {
		cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(`github: {auth: {env: "G"}}`))
		t.Setenv("G", "test-token")
		p := &props.Props{
			FS:     fs,
			Logger: logger.NewNoop(),
			Config: cfg,
		}
		r, err := NewRepo(p)
		require.NoError(t, err)
		assert.NotNil(t, r.GetAuth())
	})

	t.Run("ssh_auth_agent", func(t *testing.T) {
		cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(`github: {ssh: {key: {type: "agent"}}}`))
		p := &props.Props{
			FS:     fs,
			Logger: logger.NewNoop(),
			Config: cfg,
		}
		// This might fail if no agent is running, but let's see
		_, _ = NewRepo(p)
	})
}

func TestRepo_Unit_Options(t *testing.T) {
	t.Parallel()

	t.Run("WithConfig", func(t *testing.T) {
		r := &Repo{}
		cfg := &gitconfig.Config{}
		err := WithConfig(cfg)(r)
		require.NoError(t, err)
		assert.Equal(t, cfg, r.config)
	})

	t.Run("CloneOptions", func(t *testing.T) {
		opts := &git.CloneOptions{}

		WithShallowClone(1)(opts)
		assert.Equal(t, 1, opts.Depth)

		WithSingleBranch("develop")(opts)
		assert.True(t, opts.SingleBranch)
		assert.Equal(t, "refs/heads/develop", opts.ReferenceName.String())

		WithSingleBranch("")(opts) // empty branch — sets SingleBranch but no reference
		assert.True(t, opts.SingleBranch)

		WithNoTags()(opts)
		assert.Equal(t, git.NoTags, opts.Tags)

		WithRecurseSubmodules()(opts)
		assert.Equal(t, git.DefaultSubmoduleRecursionDepth, opts.RecurseSubmodules)
	})
}

func TestRepo_Unit_Getters(t *testing.T) {
	t.Parallel()

	t.Run("SetRepo/WithRepo", func(t *testing.T) {
		t.Parallel()
		r := &Repo{}
		require.ErrorIs(t, r.WithRepo(func(_ *git.Repository) error { return nil }), ErrNoRepository)

		tmpDir := t.TempDir()
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)
		r.SetRepo(repo)

		err = r.WithRepo(func(gr *git.Repository) error {
			assert.Equal(t, repo, gr)

			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("SetTree/WithTree", func(t *testing.T) {
		t.Parallel()
		r := &Repo{}
		require.ErrorIs(t, r.WithTree(func(_ *git.Worktree) error { return nil }), ErrNoWorktree)

		tmpDir := t.TempDir()
		repo, err := git.PlainInit(tmpDir, false)
		require.NoError(t, err)
		tree, err := repo.Worktree()
		require.NoError(t, err)
		r.SetTree(tree)

		err = r.WithTree(func(wt *git.Worktree) error {
			assert.Equal(t, tree, wt)

			return nil
		})
		assert.NoError(t, err)
	})

	t.Run("SourceIs/SetSource", func(t *testing.T) {
		t.Parallel()
		r := &Repo{}
		assert.False(t, r.SourceIs(SourceLocal))
		r.SetSource(SourceLocal)
		assert.True(t, r.SourceIs(SourceLocal))
		assert.False(t, r.SourceIs(SourceMemory))
	})
}

func TestRepo_Unit_WithRepo_PropagatesError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	repo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	r := &Repo{}
	r.SetRepo(repo)

	sentinel := errors.New("callback error")
	err = r.WithRepo(func(_ *git.Repository) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
}

func TestRepo_Unit_WithTree_PropagatesError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	repo, err := git.PlainInit(tmpDir, false)
	require.NoError(t, err)

	tree, err := repo.Worktree()
	require.NoError(t, err)

	r := &Repo{}
	r.SetTree(tree)

	sentinel := errors.New("callback error")
	err = r.WithTree(func(_ *git.Worktree) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
}

func TestRepo_Unit_Open(t *testing.T) {
	t.Run("Open_LocalRepo", func(t *testing.T) {
		tmpDir := t.TempDir()
		r := &Repo{}
		repo, tree, err := r.Open(LocalRepo, tmpDir, "main")
		require.NoError(t, err)
		assert.NotNil(t, repo)
		assert.NotNil(t, tree)
		assert.True(t, r.SourceIs(SourceLocal))
	})

	t.Run("Open_UnknownType", func(t *testing.T) {
		r := &Repo{}
		_, _, err := r.Open("bad-type", "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown repo type")
	})
}

func TestRepo_Unit_CreateBranch_Existing(t *testing.T) {
	tmpDir := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}

	r, err := NewRepo(p)
	require.NoError(t, err)
	_, wt, err := r.OpenLocal(tmpDir, "main")
	require.NoError(t, err)

	// Make an initial commit so HEAD exists
	_ = os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("x"), 0o644)
	_, _ = wt.Add("init.txt")
	_, _ = r.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.com", When: time.Now()},
	})

	// Create "feat" for the first time
	require.NoError(t, r.CreateBranch("feat"))

	// Go back to main so we can re-create feat
	require.NoError(t, r.Checkout(plumbing.NewBranchReferenceName("main")))

	// Creating an already-existing branch with SourceMemory skips the pull
	r.SetSource(SourceMemory)
	err = r.CreateBranch("feat")
	assert.NoError(t, err)
}

func TestRepo_Unit_Clone_LocalToLocal(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "clone")

	// Seed the source repo with a commit
	srcRepo, err := git.PlainInit(srcDir, false)
	require.NoError(t, err)
	wt, err := srcRepo.Worktree()
	require.NoError(t, err)
	_ = os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("hello"), 0o644)
	_, _ = wt.Add("readme.txt")
	_, err = wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.com", When: time.Now()},
	})
	require.NoError(t, err)

	r := &Repo{}
	repo, tree, err := r.Clone(srcDir, dstDir)
	require.NoError(t, err)
	assert.NotNil(t, repo)
	assert.NotNil(t, tree)

	_, statErr := os.Stat(filepath.Join(dstDir, "readme.txt"))
	assert.NoError(t, statErr)
}

func TestRepo_Unit_GetSSHKey_Errors(t *testing.T) {
	t.Parallel()

	t.Run("not_exist", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		_, err := GetSSHKey("/nonexistent/key", fs)
		assert.Error(t, err)
	})

	t.Run("is_directory", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.Mkdir("/keydir", 0o755))
		_, err := GetSSHKey("/keydir", fs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GITHUB_KEY")
	})

	t.Run("invalid_key_bytes", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/bad.key", []byte("not-a-pem-key"), 0o600))
		// ParsePrivateKey fails but does not report "passphrase protected",
		// so NewPublicKeys is called with the invalid bytes and fails.
		_, err := GetSSHKey("/bad.key", fs)
		assert.Error(t, err)
	})
}

func TestRepo_Unit_Commit_NilOpts(t *testing.T) {
	tmpDir := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}
	r, err := NewRepo(p)
	require.NoError(t, err)

	_, wt, err := r.OpenLocal(tmpDir, "main")
	require.NoError(t, err)

	_ = os.WriteFile(filepath.Join(tmpDir, "x.txt"), []byte("x"), 0o644)
	_, _ = wt.Add("x.txt")

	// Passing nil opts covers the `opts = &git.CommitOptions{}` branch.
	_, _ = r.Commit("nil opts commit", nil)
}

func TestRepo_Unit_AddToFS_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}
	r, err := NewRepo(p)
	require.NoError(t, err)

	_, wt, err := r.OpenLocal(tmpDir, "main")
	require.NoError(t, err)

	relPath := "readme.txt"
	_ = os.WriteFile(filepath.Join(tmpDir, relPath), []byte("original"), 0o644)
	_, _ = wt.Add(relPath)
	_, _ = r.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.com", When: time.Now()},
	})

	gitFile, err := r.GetFile(relPath)
	require.NoError(t, err)

	memFS := afero.NewMemMapFs()
	// Pre-populate the target path so AddToFS returns early.
	require.NoError(t, afero.WriteFile(memFS, "/out.txt", []byte("existing"), 0o644))

	err = r.AddToFS(memFS, gitFile, "/out.txt")
	require.NoError(t, err)

	// File must be unchanged — AddToFS skips overwriting.
	content, _ := afero.ReadFile(memFS, "/out.txt")
	assert.Equal(t, "existing", string(content))
}

func TestRepo_Unit_GetFile_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop()}
	r, err := NewRepo(p)
	require.NoError(t, err)

	_, wt, err := r.OpenLocal(tmpDir, "main")
	require.NoError(t, err)

	_ = os.WriteFile(filepath.Join(tmpDir, "x.txt"), []byte("x"), 0o644)
	_, _ = wt.Add("x.txt")
	_, _ = r.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "T", Email: "t@t.com", When: time.Now()},
	})

	_, err = r.GetFile("does-not-exist.txt")
	assert.Error(t, err)
}

func TestRepo_Unit_NewRepo_OptError(t *testing.T) {
	t.Parallel()
	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
	errOpt := func(r *Repo) error {
		return errors.New("opt failed")
	}
	_, err := NewRepo(p, errOpt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opt failed")
}

func TestRepo_Unit_NewRepo_TokenAuthFails(t *testing.T) {
	// Config has no github.ssh and no auth token → configureTokenAuth fails.
	t.Setenv("GITHUB_TOKEN", "")
	cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(`github: {}`))
	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), Config: cfg}
	_, err := NewRepo(p)
	assert.Error(t, err)
}

func TestRepo_Unit_configureSSHAuth_Paths(t *testing.T) {
	t.Run("path_key_not_found", func(t *testing.T) {
		cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(`
github:
  ssh:
    key:
      path: /nonexistent/id_rsa
`))
		fs := afero.NewOsFs()
		p := &props.Props{FS: fs, Logger: logger.NewNoop(), Config: cfg}
		_, err := NewRepo(p)
		assert.Error(t, err)
	})

	t.Run("env_empty_falls_back_to_agent", func(t *testing.T) {
		cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(`
github:
  ssh:
    key:
      env: GTB_TEST_SSH_KEY_EMPTY_XYZ
`))
		t.Setenv("GTB_TEST_SSH_KEY_EMPTY_XYZ", "")
		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), Config: cfg}
		// Falls back to ssh-agent; will fail if no agent running, but path is covered.
		_, _ = NewRepo(p)
	})

	t.Run("env_key_not_found", func(t *testing.T) {
		cfg := config.NewReaderContainer(logger.NewNoop(), "yaml", strings.NewReader(`
github:
  ssh:
    key:
      env: GTB_TEST_SSH_KEY_XYZ
`))
		t.Setenv("GTB_TEST_SSH_KEY_XYZ", "/nonexistent/key")
		p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: cfg}
		_, err := NewRepo(p)
		assert.Error(t, err)
	})
}
