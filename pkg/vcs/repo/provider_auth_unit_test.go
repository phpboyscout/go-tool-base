package repo

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"testing"

	"github.com/cockroachdb/errors"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// TestRepo_Unit_ForgeTokenAuth proves NewRepo resolves clone/push credentials
// from the configured forge's subtree (D1) and that missing credentials are
// non-fatal for public repositories (D2).
func TestRepo_Unit_ForgeTokenAuth(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		env      map[string]string
		wantErr  string
		wantAuth transport.AuthMethod
	}{
		{
			name: "gitlab type resolves from gitlab settings",
			settings: Settings{
				ReleaseSource: release.ReleaseSourceConfig{Type: "gitlab"},
				Auth:          tokenConfig{"auth.env": "GTB_TEST_GL_TOKEN"},
			},
			env: map[string]string{"GTB_TEST_GL_TOKEN": "glpat-secret"},
			wantAuth: &http.BasicAuth{
				Username: "oauth2",
				Password: "glpat-secret",
			},
		},
		{
			name: "gitlab type with no gitlab auth ignores github env",
			settings: Settings{
				ReleaseSource: release.ReleaseSourceConfig{Type: "gitlab"},
				AuthEnabled:   true,
			},
			env: map[string]string{
				"GTB_TEST_GH_TOKEN": "ghp_secret",
				"GITLAB_TOKEN":      "",
			},
			wantAuth: nil, // public repo, no gitlab credential: unauthenticated
		},
		{
			name: "explicit forge overrides release source type",
			settings: Settings{
				ReleaseSource: release.ReleaseSourceConfig{Type: "github"},
				Forge:         "gitlab",
				Auth:          tokenConfig{"auth.env": "GTB_TEST_GL_TOKEN"},
			},
			env: map[string]string{"GTB_TEST_GL_TOKEN": "glpat-override"},
			wantAuth: &http.BasicAuth{
				Username: "oauth2",
				Password: "glpat-override",
			},
		},
		{
			name:     "github default unchanged",
			settings: Settings{Auth: tokenConfig{"auth.env": "GTB_TEST_GH_TOKEN"}},
			env:      map[string]string{"GTB_TEST_GH_TOKEN": "ghp_secret"},
			wantAuth: &http.BasicAuth{
				Username: "x-access-token",
				Password: "ghp_secret",
			},
		},
		{
			name:     "public repo with no token does not error",
			settings: Settings{ReleaseSource: release.ReleaseSourceConfig{Type: "github"}, AuthEnabled: true},
			env:      map[string]string{"GITHUB_TOKEN": ""},
		},
		{
			name: "private repo with no token errors",
			settings: Settings{ReleaseSource: release.ReleaseSourceConfig{
				Type:    "github",
				Private: true,
			}, AuthEnabled: true},
			env:     map[string]string{"GITHUB_TOKEN": ""},
			wantErr: "no GITHUB token available for private repository",
		},
		{
			name: "private gitlab repo with no token errors",
			settings: Settings{ReleaseSource: release.ReleaseSourceConfig{
				Type:    "gitlab",
				Private: true,
			}, AuthEnabled: true},
			env:     map[string]string{"GITLAB_TOKEN": ""},
			wantErr: "no GITLAB token available for private repository",
		},
		{
			name:     "gitea fallback env token",
			settings: Settings{ReleaseSource: release.ReleaseSourceConfig{Type: "gitea"}, AuthEnabled: true},
			env:      map[string]string{"GITEA_TOKEN": "gta_secret"},
			wantAuth: &http.BasicAuth{
				Username: "x-access-token",
				Password: "gta_secret",
			},
		},
		{
			name: "bitbucket uses x-token-auth username",
			settings: Settings{
				ReleaseSource: release.ReleaseSourceConfig{Type: "bitbucket"},
				Auth:          tokenConfig{"auth.env": "GTB_TEST_BB_TOKEN"},
			},
			env: map[string]string{"GTB_TEST_BB_TOKEN": "bb_secret"},
			wantAuth: &http.BasicAuth{
				Username: "x-token-auth",
				Password: "bb_secret",
			},
		},
		{
			name:     "empty type defaults to github",
			settings: Settings{ReleaseSource: release.ReleaseSourceConfig{Type: ""}, Auth: tokenConfig{"auth.env": "GTB_TEST_GH_TOKEN"}},
			env:      map[string]string{"GTB_TEST_GH_TOKEN": "ghp_default"},
			wantAuth: &http.BasicAuth{
				Username: "x-access-token",
				Password: "ghp_default",
			},
		},
		{
			name:     "direct type falls back to github",
			settings: Settings{ReleaseSource: release.ReleaseSourceConfig{Type: "direct"}, Auth: tokenConfig{"auth.env": "GTB_TEST_GH_TOKEN"}},
			env:      map[string]string{"GTB_TEST_GH_TOKEN": "ghp_direct"},
			wantAuth: &http.BasicAuth{
				Username: "x-access-token",
				Password: "ghp_direct",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			tt.settings.FS = afero.NewMemMapFs()
			tt.settings.Logger = logger.NewNoop()

			r, err := NewRepo(tt.settings)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAuth, r.GetAuth())
		})
	}
}

// TestRepo_Unit_ForgeSSHAuth proves the SSH subtree is selected per forge (D3).
func TestRepo_Unit_ForgeSSHAuth(t *testing.T) {
	t.Run("gitlab ssh subtree selected for gitlab tool", func(t *testing.T) {
		_, err := NewRepo(Settings{
			ReleaseSource: release.ReleaseSourceConfig{Type: "gitlab"},
			SSH:           SSHSettings{Configured: true, HasKey: true, Path: "/nonexistent/id_ed25519"},
			FS:            afero.NewMemMapFs(),
			Logger:        logger.NewNoop(),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get SSH key")
	})

	t.Run("gitlab ssh subtree ignored for github tool", func(t *testing.T) {
		t.Setenv("GTB_TEST_GH_TOKEN", "ghp_secret")

		r, err := NewRepo(Settings{
			ReleaseSource: release.ReleaseSourceConfig{Type: "github"},
			Auth:          tokenConfig{"auth.env": "GTB_TEST_GH_TOKEN"},
			FS:            afero.NewMemMapFs(),
			Logger:        logger.NewNoop(),
		})
		require.NoError(t, err)
		assert.Equal(t, &http.BasicAuth{Username: "x-access-token", Password: "ghp_secret"}, r.GetAuth())
	})
}

// encryptedTestKey writes a passphrase-protected ed25519 private key to fs.
func encryptedTestKey(t *testing.T, fs afero.Fs, path, passphrase string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	block, err := gossh.MarshalPrivateKeyWithPassphrase(priv, "gtb-test", []byte(passphrase))
	require.NoError(t, err)

	require.NoError(t, afero.WriteFile(fs, path, pem.EncodeToMemory(block), 0o600))
}

// TestRepo_Unit_GetSSHKey_PassphraseProtected proves typed passphrase
// detection (D3): the library returns *gossh.PassphraseMissingError instead
// of blocking on an interactive prompt.
func TestRepo_Unit_GetSSHKey_PassphraseProtected(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	encryptedTestKey(t, fs, "/id_ed25519", "testpass")

	t.Run("returns typed error without passphrase", func(t *testing.T) {
		t.Parallel()

		_, err := GetSSHKey("/id_ed25519", fs)
		require.Error(t, err)

		var missing *gossh.PassphraseMissingError
		assert.True(t, errors.As(err, &missing),
			"expected *gossh.PassphraseMissingError in the chain, got: %v", err)
	})

	t.Run("succeeds with correct passphrase", func(t *testing.T) {
		t.Parallel()

		keys, err := GetSSHKeyWithPassphrase("/id_ed25519", fs, "testpass")
		require.NoError(t, err)
		assert.Equal(t, "git", keys.User)
	})

	t.Run("fails with wrong passphrase", func(t *testing.T) {
		t.Parallel()

		_, err := GetSSHKeyWithPassphrase("/id_ed25519", fs, "wrong")
		require.Error(t, err)

		var missing *gossh.PassphraseMissingError
		assert.False(t, errors.As(err, &missing))
	})
}

// TestRepo_Unit_NewRepo_EncryptedSSHKey proves NewRepo surfaces the typed
// passphrase error to the caller instead of invoking a TUI (D3).
func TestRepo_Unit_NewRepo_EncryptedSSHKey(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	encryptedTestKey(t, fs, "/id_ed25519", "testpass")

	_, err := NewRepo(Settings{
		ReleaseSource: release.ReleaseSourceConfig{Type: "github"},
		SSH:           SSHSettings{Configured: true, HasKey: true, Path: "/id_ed25519"},
		FS:            fs,
		Logger:        logger.NewNoop(),
	})
	require.Error(t, err)

	var missing *gossh.PassphraseMissingError
	assert.True(t, errors.As(err, &missing),
		"expected *gossh.PassphraseMissingError in the chain, got: %v", err)
}

// TestRepo_Unit_UnopenedMethods proves every RepoLike method on an unopened
// repo returns the sentinel error instead of panicking (D4).
func TestRepo_Unit_UnopenedMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(RepoLike) error
		want error
	}{
		{"Checkout", func(r RepoLike) error {
			return r.Checkout(plumbing.NewBranchReferenceName("main"))
		}, ErrNoWorktree},
		{"CheckoutCommit", func(r RepoLike) error {
			return r.CheckoutCommit(plumbing.ZeroHash)
		}, ErrNoWorktree},
		{"CreateBranch", func(r RepoLike) error {
			return r.CreateBranch("feature")
		}, ErrNoRepository},
		{"Push", func(r RepoLike) error {
			return r.Push(nil)
		}, ErrNoRepository},
		{"Commit", func(r RepoLike) error {
			_, err := r.Commit("msg", nil)

			return err
		}, ErrNoWorktree},
		{"WalkTree", func(r RepoLike) error {
			return r.WalkTree(func(*object.File) error { return nil })
		}, ErrNoRepository},
		{"FileExists", func(r RepoLike) error {
			_, err := r.FileExists("a.txt")

			return err
		}, ErrNoRepository},
		{"DirectoryExists", func(r RepoLike) error {
			_, err := r.DirectoryExists("dir")

			return err
		}, ErrNoRepository},
		{"GetFile", func(r RepoLike) error {
			_, err := r.GetFile("a.txt")

			return err
		}, ErrNoRepository},
		{"WithRepo", func(r RepoLike) error {
			return r.WithRepo(func(*git.Repository) error { return nil })
		}, ErrNoRepository},
		{"WithTree", func(r RepoLike) error {
			return r.WithTree(func(*git.Worktree) error { return nil })
		}, ErrNoWorktree},
	}

	subjects := map[string]func() RepoLike{
		"Repo":           func() RepoLike { return &Repo{} },
		"ThreadSafeRepo": func() RepoLike { return &ThreadSafeRepo{repo: &Repo{}} },
	}

	for subject, newSubject := range subjects {
		for _, tt := range tests {
			t.Run(subject+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				r := newSubject()

				var err error

				assert.NotPanics(t, func() { err = tt.call(r) })
				assert.ErrorIs(t, err, tt.want)
			})
		}
	}
}

// TestRepo_Unit_UnopenedNonInterfaceMethods covers the public methods that are
// not part of RepoLike but must also not panic when the repo is unopened (D4).
func TestRepo_Unit_UnopenedNonInterfaceMethods(t *testing.T) {
	t.Parallel()

	r := &Repo{}

	t.Run("CreateRemote", func(t *testing.T) {
		t.Parallel()

		var err error

		assert.NotPanics(t, func() {
			_, err = r.CreateRemote("origin", []string{"https://example.test/repo.git"})
		})
		assert.ErrorIs(t, err, ErrNoRepository)
	})

	t.Run("Remote", func(t *testing.T) {
		t.Parallel()

		var err error

		assert.NotPanics(t, func() { _, err = r.Remote("origin") })
		assert.ErrorIs(t, err, ErrNoRepository)
	})
}
