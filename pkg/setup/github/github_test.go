package github

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	mockVCS "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/github"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	githubvcs "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/github"
)

// generateUnencryptedKeyPEM generates a fresh ed25519 private key in OpenSSH PEM format.
func generateUnencryptedKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := gossh.MarshalPrivateKey(privKey, "")
	require.NoError(t, err)
	return pem.EncodeToMemory(block)
}

func newTestProps(t *testing.T) *props.Props {
	t.Helper()
	return &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
}

// newTestEditor materialises a config file under /cfgdir on p.FS (seeded
// with the given YAML document when non-empty) and opens a real
// store-backed editor over it, so wizard writes are observable through
// editor.View().
func newTestEditor(t *testing.T, p *props.Props, yamlDoc string) setup.Editor {
	t.Helper()

	const dir = "/cfgdir"
	if yamlDoc != "" {
		require.NoError(t, p.FS.MkdirAll(dir, 0o755))
		require.NoError(t, afero.WriteFile(p.FS,
			filepath.Join(dir, setup.DefaultConfigFilename), []byte(yamlDoc), 0o600))
	}

	editor, _, err := setup.OpenConfigEditor(t.Context(), p, dir, false)
	require.NoError(t, err)

	return editor
}

func TestDiscoverSSHKeys_Coverage(t *testing.T) {
	// Setup Afero FS
	fs := afero.NewMemMapFs()

	// Mock HOME directory
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// Create .ssh directory and some keys
	sshDir := filepath.Join(homeDir, ".ssh")
	err := fs.MkdirAll(sshDir, 0700)
	require.NoError(t, err)

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestGenerateAndDiscoverKey(t *testing.T) {
	// Setup
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// Mock Forms to avoid interaction
	mockPassphraseForm := func(s *string) *huh.Form {
		*s = "" // no passphrase
		return nil
	}
	mockUploadForm := func(b *bool) *huh.Form {
		*b = false // no upload
		return nil
	}

	// 1. Generate Key
	keyPath, err := generateKey(p, testutil.ViewFromYAML(t, ""),
		WithPassphraseForm(mockPassphraseForm),
		WithUploadConfirmForm(mockUploadForm),
	)
	require.NoError(t, err)
	assert.Contains(t, keyPath, ".ssh/id_testtool_")

	// Verify file exists in FS
	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists, "Private key should exist")
	exists, _ = afero.Exists(fs, keyPath+".pub")
	assert.True(t, exists, "Public key should exist")

	// 2. Discover Keys
	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.NotEmpty(t, keys)

	// Verify our generated key is found
	found := false
	for _, k := range keys {
		if k.Value == keyPath {
			found = true
			break
		}
	}
	assert.True(t, found, "Generated key should be discovered")
}

func TestGenerateKey_Upload(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := testutil.ViewFromYAML(t, "github:\n  token: dummy-token\n")

	mockClient := mockVCS.NewMockGitHubClient(t)
	mockClient.EXPECT().UploadKey(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	keyPath, err := generateKey(p, cfg,
		WithPassphraseForm(func(s *string) *huh.Form {
			*s = ""
			return nil
		}),
		WithUploadConfirmForm(func(b *bool) *huh.Form {
			*b = true
			return nil
		}),
		WithGitHubClientFactory(func(_ config.Reader) (githubvcs.GitHubClient, error) {
			return mockClient, nil
		}),
	)
	require.NoError(t, err)

	exists, _ := afero.Exists(fs, keyPath)
	assert.True(t, exists)
}

func TestGitHubInitialiser(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)
	// Ensure env-var-mode detection does not short-circuit the
	// OAuth path; this test exercises the legacy literal-write
	// behaviour and must not inherit GITHUB_TOKEN from the runner.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("CI", "")

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) {
			return "mock-token", nil
		}),
		WithGitHubAuthForms(WithAuthForm(
			func(cfg *GitHubAuthConfig) []*huh.Form {
				cfg.StorageMode = credentials.ModeLiteral
				return nil // skip every staged form
			},
			func(_ string, _ string) *huh.Form { return nil },
		)),
	)
	err := init.Configure(p, cfg)
	require.NoError(t, err)

	assert.Equal(t, "mock-token", cfg.View().GetString("github.auth.value"))
}

func TestGitHubInitialiser_CIRefusesOAuthLiteral(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("GITHUB_TOKEN", "")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) {
			t.Fatal("OAuth must not run under CI")
			return "", nil
		}),
	)

	err := init.Configure(p, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused under CI")
}

func TestGitHubInitialiser_SkipsOAuthWhenEnvVarConfigured(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-set-via-env")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) {
			t.Fatal("OAuth must not run when GITHUB_TOKEN is set")
			return "", nil
		}),
	)

	require.NoError(t, init.Configure(p, cfg))
	// No literal token must be written — the env var is the source of truth.
	assert.Empty(t, cfg.View().GetString("github.auth.value"))
}

func TestConfigure_SkipBoth(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	// A mock editor proves both stages are skipped: no Set calls are
	// permitted; only the unconditional post-auth View read happens.
	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))
	i := &GitHubInitialiser{SkipLogin: true, SkipKey: true}
	assert.NoError(t, i.Configure(p, cfg))
}

func TestConfigure_LoginAlreadySet_SkipKey(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, "github:\n  auth:\n    value: already-set-token\n"))
	i := &GitHubInitialiser{SkipLogin: false, SkipKey: true}
	// First if: SkipLogin=false but auth.value != "" so branch not entered
	assert.NoError(t, i.Configure(p, cfg))
}

func TestIsGitHubConfigured(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	tests := []struct {
		name     string
		yaml     string
		setup    func(t *testing.T)
		expected bool
	}{
		{
			name:     "empty config",
			yaml:     "",
			expected: false,
		},
		{
			name:     "token provided",
			yaml:     "github:\n  auth:\n    value: some-token\n  ssh:\n    key:\n      type: agent\n",
			expected: true,
		},
		{
			name: "env var name provided but not set",
			yaml: "github:\n  auth:\n    env: TEST_GH_TOKEN\n  ssh:\n    key:\n      type: agent\n",
			setup: func(t *testing.T) {
				t.Setenv("TEST_GH_TOKEN", "")
			},
			expected: false,
		},
		{
			name: "env var name provided and set",
			yaml: "github:\n  auth:\n    env: TEST_GH_TOKEN\n  ssh:\n    key:\n      type: agent\n",
			setup: func(t *testing.T) {
				t.Setenv("TEST_GH_TOKEN", "secret")
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			cfg := testutil.ViewFromYAML(t, tt.yaml)
			init := NewGitHubInitialiser(p, false, false)
			assert.Equal(t, tt.expected, init.IsConfigured(cfg))
		})
	}
}

// --- SSH option funcs ---

func TestWithSSHKeySelectForm(t *testing.T) {
	t.Parallel()
	called := false
	opt := WithSSHKeySelectForm(func(_ *string, _ []huh.Option[string]) *huh.Form {
		called = true
		return nil
	})
	c := &configureSSHKeyConfig{}
	opt(c)
	require.NotNil(t, c.sshKeySelectFormCreator)
	c.sshKeySelectFormCreator(nil, nil)
	assert.True(t, called)
}

func TestWithSSHKeyPathForm(t *testing.T) {
	t.Parallel()
	called := false
	opt := WithSSHKeyPathForm(func(_ *string) *huh.Form {
		called = true
		return nil
	})
	c := &configureSSHKeyConfig{}
	opt(c)
	require.NotNil(t, c.sshKeyPathFormCreator)
	c.sshKeyPathFormCreator(nil)
	assert.True(t, called)
}

func TestWithGenerateKeyOptions(t *testing.T) {
	t.Parallel()
	noop := func(_ *generateKeyConfig) {}
	opt := WithGenerateKeyOptions(noop)
	c := &configureSSHKeyConfig{}
	opt(c)
	assert.Len(t, c.generateKeyOpts, 1)
}

// --- validateSSHKey ---

func TestValidateSSHKey_Valid(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	keyPEM := generateUnencryptedKeyPEM(t)
	err := validateSSHKey(keyPEM, p)
	assert.NoError(t, err)
}

func TestValidateSSHKey_Invalid(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	err := validateSSHKey([]byte("not-a-key"), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid private key")
}

// --- isValidSSHKey ---

func TestIsValidSSHKey_Valid(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, afero.WriteFile(fs, "/valid.key", keyPEM, 0o600))
	assert.True(t, isValidSSHKey(fs, "/valid.key"))
}

func TestIsValidSSHKey_Invalid(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bad.key", []byte("garbage"), 0o600))
	assert.False(t, isValidSSHKey(fs, "/bad.key"))
}

func TestIsValidSSHKey_ReadError(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	// File does not exist → ReadFile fails → returns false
	assert.False(t, isValidSSHKey(fs, "/nonexistent.key"))
}

// --- handleSSHKeySelection ---

func TestHandleSSHKeySelection_Agent(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyType, keyPath, err := handleSSHKeySelection(p, cfg, "agent", &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "agent", keyType)
	assert.Empty(t, keyPath)
}

func TestHandleSSHKeySelection_Default(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyType, keyPath, err := handleSSHKeySelection(p, cfg, "/home/user/.ssh/id_ed25519", &configureSSHKeyConfig{})
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/home/user/.ssh/id_ed25519", keyPath)
}

func TestHandleSSHKeySelection_Other_Error(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	// Form returns nil, then tries to read a non-existent file.
	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(s *string) *huh.Form {
			*s = "/nonexistent/id_rsa"
			return nil
		},
	}
	_, _, err := handleSSHKeySelection(p, cfg, "other", opts)
	assert.Error(t, err)
}

func TestHandleSSHKeySelection_Other_ValidKey(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	keyPEM := generateUnencryptedKeyPEM(t)
	require.NoError(t, afero.WriteFile(p.FS, "/test.key", keyPEM, 0o600))

	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(s *string) *huh.Form {
			*s = "/test.key"
			return nil
		},
	}
	keyType, keyPath, err := handleSSHKeySelection(p, cfg, "other", opts)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/test.key", keyPath)
}

func TestHandleSSHKeySelection_Generate(t *testing.T) {
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	opts := &configureSSHKeyConfig{
		generateKeyOpts: []GenerateKeyOption{
			WithPassphraseForm(func(s *string) *huh.Form {
				*s = ""
				return nil
			}),
			WithUploadConfirmForm(func(b *bool) *huh.Form {
				*b = false
				return nil
			}),
		},
	}
	keyType, keyPath, err := handleSSHKeySelection(p, cfg, "generate", opts)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Contains(t, keyPath, ".ssh/id_testtool_")
}

// --- ConfigureSSHKey ---

func TestConfigureSSHKey_Agent(t *testing.T) {
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")

	keyType, keyPath, err := ConfigureSSHKey(p, cfg,
		WithSSHKeySelectForm(func(s *string, _ []huh.Option[string]) *huh.Form {
			*s = "agent"
			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "agent", keyType)
	assert.Empty(t, keyPath)
}

func TestConfigureSSHKey_ExistingPath(t *testing.T) {
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "github:\n  ssh:\n    key:\n      path: /home/testuser/.ssh/existing_key\n")

	keyType, keyPath, err := ConfigureSSHKey(p, cfg,
		WithSSHKeySelectForm(func(s *string, _ []huh.Option[string]) *huh.Form {
			// Form not shown; targetKey is pre-populated from config.
			return nil
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "file", keyType)
	assert.Equal(t, "/home/testuser/.ssh/existing_key", keyPath)
}

// --- uploadSSHKeyToGitHub error paths ---

func TestUploadSSHKeyToGitHub_ClientError(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	err := uploadSSHKeyToGitHub(p, cfg, "keyname", []byte("pubkey"), func(_ config.Reader) (githubvcs.GitHubClient, error) {
		return nil, assert.AnError
	})
	assert.Error(t, err)
}

func TestUploadSSHKeyToGitHub_UploadError(t *testing.T) {
	t.Parallel()

	mockClient := mockVCS.NewMockGitHubClient(t)
	mockClient.EXPECT().UploadKey(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	err := uploadSSHKeyToGitHub(p, cfg, "keyname", []byte("pubkey"), func(_ config.Reader) (githubvcs.GitHubClient, error) {
		return mockClient, nil
	})
	assert.Error(t, err)
}
