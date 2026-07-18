package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"charm.land/huh/v2"
	"github.com/charmbracelet/keygen"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"

	mockVCS "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/vcs/github"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	githubvcs "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/github"
)

// passphraseProtectedKeyPEM returns OpenSSH-format private key bytes that
// are encrypted with a passphrase, so ssh.ParseRawPrivateKey reports the
// "passphrase protected" sentinel that validateSSHKey/isValidSSHKey
// special-case.
func passphraseProtectedKeyPEM(t *testing.T) []byte {
	t.Helper()
	kp, err := keygen.New(filepath.Join(t.TempDir(), "id_test"),
		keygen.WithPassphrase("super-secret-pass"),
		keygen.WithKeyType(keygen.Ed25519),
	)
	require.NoError(t, err)

	return kp.RawProtectedPrivateKey()
}

// --- Name ---

func TestGitHubInitialiser_Name(t *testing.T) {
	t.Parallel()
	i := &GitHubInitialiser{}
	assert.Equal(t, "GitHub integration", i.Name())
}

// --- Default auth form creators (construction only; not run against a TTY) ---

func TestDefaultStorageModeForm(t *testing.T) {
	t.Parallel()
	cfg := &GitHubAuthConfig{}
	form := defaultStorageModeForm(cfg)
	require.NotNil(t, form)
	// Default storage mode is populated when initially empty.
	assert.Equal(t, credentials.ModeEnvVar, cfg.StorageMode)
}

func TestDefaultStorageModeForm_PreservesExistingMode(t *testing.T) {
	// CI runners set CI=true, which drops the literal option (refused under CI)
	// from the select; huh then resets the bound value off the missing option.
	// Force non-CI so the existing-mode preservation is deterministic.
	t.Setenv("CI", "")

	cfg := &GitHubAuthConfig{StorageMode: credentials.ModeLiteral}
	form := defaultStorageModeForm(cfg)
	require.NotNil(t, form)
	assert.Equal(t, credentials.ModeLiteral, cfg.StorageMode)
}

func TestGithubStorageModeDescription(t *testing.T) {
	t.Parallel()
	assert.Contains(t, githubStorageModeDescription(true), "CI environment detected")
	assert.Contains(t, githubStorageModeDescription(false), "recommended")
}

func TestDefaultEnvVarNameForm(t *testing.T) {
	t.Parallel()
	cfg := &GitHubAuthConfig{}
	form := defaultEnvVarNameForm(cfg)
	require.NotNil(t, form)
	assert.Equal(t, "GITHUB_TOKEN", cfg.EnvVarName)
}

func TestDefaultEnvVarNameForm_PreservesExistingName(t *testing.T) {
	t.Parallel()
	cfg := &GitHubAuthConfig{EnvVarName: "MYTOOL_GH"}
	form := defaultEnvVarNameForm(cfg)
	require.NotNil(t, form)
	assert.Equal(t, "MYTOOL_GH", cfg.EnvVarName)
}

func TestDefaultFetchTokenForm(t *testing.T) {
	t.Parallel()
	cfg := &GitHubAuthConfig{}
	form := defaultFetchTokenForm(cfg)
	require.NotNil(t, form)
	assert.True(t, cfg.FetchToken)
}

func TestDefaultDisplayOnceForm(t *testing.T) {
	t.Parallel()
	form := defaultDisplayOnceForm("GITHUB_TOKEN", "ghp_secret")
	require.NotNil(t, form)
}

// --- defaultGitHubClientFactory ---

func TestDefaultGitHubClientFactory(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	client, err := defaultGitHubClientFactory(cfg)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// --- Default SSH form creators (construction only) ---

func TestDefaultSSHKeySelectFormCreator(t *testing.T) {
	t.Parallel()
	var target string
	opts := []huh.Option[string]{huh.NewOption("a", "a")}
	form := defaultSSHKeySelectFormCreator(&target, opts)
	assert.NotNil(t, form)
}

func TestDefaultSSHKeyPathFormCreator(t *testing.T) {
	t.Parallel()
	var target string
	form := defaultSSHKeyPathFormCreator(&target)
	assert.NotNil(t, form)
}

func TestDefaultPassphraseFormCreator(t *testing.T) {
	t.Parallel()
	var pass string
	form := defaultPassphraseFormCreator(&pass)
	assert.NotNil(t, form)
}

func TestDefaultUploadConfirmFormCreator(t *testing.T) {
	t.Parallel()
	var upload bool
	form := defaultUploadConfirmFormCreator(&upload)
	assert.NotNil(t, form)
}

// --- captureToken: OAuth success and manual fallback ---

func TestCaptureToken_OAuthSuccess(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	g := &GitHubInitialiser{
		loginFunc: func(_ string) (string, error) { return "ghp_oauth", nil },
	}
	token, err := g.captureToken(p)
	require.NoError(t, err)
	assert.Equal(t, "ghp_oauth", token)
}

// TestCaptureToken_FallbackToManual exercises the OAuth-failure branch:
// loginFunc errors, captureToken falls back to the manual prompt which
// (without a TTY) errors. The fallback branch lines are what we cover.
func TestCaptureToken_FallbackToManual(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	g := &GitHubInitialiser{
		loginFunc: func(_ string) (string, error) { return "", assert.AnError },
	}
	_, err := g.captureToken(p)
	require.Error(t, err)
}

// --- runAuthCredentialStage: unsupported mode ---

func TestRunAuthCredentialStage_UnsupportedMode(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())
	g := &GitHubInitialiser{loginFunc: func(_ string) (string, error) { return "tok", nil }}
	authCfg := &GitHubAuthConfig{StorageMode: credentials.Mode("bogus")}

	err := g.runAuthCredentialStage(context.Background(), p, cfg, newAuthFormConfig(), authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential storage mode")
}

// --- writeGitHubCredential branches ---

func TestWriteGitHubCredential_EnvVarMode(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: credentials.ModeEnvVar, EnvVarName: "MY_GH"}
	require.NoError(t, writeGitHubCredential(context.Background(), cfg, "testtool", authCfg))
	assert.Equal(t, "MY_GH", cfg.GetString("github.auth.env"))
}

func TestWriteGitHubCredential_EnvVarModeEmptyName(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: credentials.ModeEnvVar}
	require.NoError(t, writeGitHubCredential(context.Background(), cfg, "testtool", authCfg))
	assert.Empty(t, cfg.GetString("github.auth.env"))
}

func TestWriteGitHubCredential_KeychainNoToolName(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: credentials.ModeKeychain, Token: "tok"}
	err := writeGitHubCredential(context.Background(), cfg, "", authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a tool name")
}

func TestWriteGitHubCredential_LiteralMode(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: credentials.ModeLiteral, Token: "ghp_lit"}
	require.NoError(t, writeGitHubCredential(context.Background(), cfg, "testtool", authCfg))
	assert.Equal(t, "ghp_lit", cfg.GetString("github.auth.value"))
}

func TestWriteGitHubCredential_EmptyModeDefaultsLiteral(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: "", Token: "ghp_empty"}
	require.NoError(t, writeGitHubCredential(context.Background(), cfg, "testtool", authCfg))
	assert.Equal(t, "ghp_empty", cfg.GetString("github.auth.value"))
}

func TestWriteGitHubCredential_UnsupportedMode(t *testing.T) {
	t.Parallel()
	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: credentials.Mode("nope"), Token: "tok"}
	err := writeGitHubCredential(context.Background(), cfg, "testtool", authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential storage mode")
}

// --- configureSSH: with no TTY the default select form errors, which
// exercises configureSSH's early lines and error return. ---

func TestConfigureSSH_FormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())
	g := &GitHubInitialiser{}

	err := g.configureSSH(p, cfg)
	require.Error(t, err)
}

// --- RunGitHubInit: GITHUB_TOKEN present short-circuits the auth stage,
// then configureSSH runs the default select form (no TTY → error). This
// covers RunGitHubInit's construction and auth short-circuit branch. ---

func TestRunGitHubInit_AuthShortCircuitThenSSHError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-here")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	err := RunGitHubInit(p, cfg)
	// Auth resolves from GITHUB_TOKEN; SSH default form errors (no TTY).
	require.Error(t, err)
}

// --- NewCmdInitGitHub: command wiring ---

func TestNewCmdInitGitHub(t *testing.T) {
	t.Parallel()
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cmd := NewCmdInitGitHub(p)
	require.NotNil(t, cmd)
	assert.Equal(t, "github", cmd.Use)
	require.NotNil(t, cmd.Flags().Lookup("dir"))
}

// --- NewCmdInitGitHub RunE: GITHUB_TOKEN short-circuits auth; the SSH
// stage errors without a TTY, so RunE wraps and returns the failure.
// Covers the RunE closure and RunInitCmd's LoadFilesContainer fallback
// + default-config read path. ---

func TestNewCmdInitGitHub_RunE_Error(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-here")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cmd := NewCmdInitGitHub(p)
	require.NoError(t, cmd.Flags().Set("dir", "/cfgdir"))

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure GitHub")
}

// --- runAuthFormStage: nil creator and nil form ---

func TestRunAuthFormStage_NilCreator(t *testing.T) {
	t.Parallel()
	require.NoError(t, runAuthFormStage(nil, &GitHubAuthConfig{}))
}

func TestRunAuthFormStage_NilForm(t *testing.T) {
	t.Parallel()
	require.NoError(t, runAuthFormStage(func(_ *GitHubAuthConfig) *huh.Form { return nil }, &GitHubAuthConfig{}))
}

func TestRunAuthFormStage_FormRunError(t *testing.T) {
	t.Parallel()
	var v string
	err := runAuthFormStage(func(_ *GitHubAuthConfig) *huh.Form {
		return huh.NewForm(huh.NewGroup(huh.NewInput().Value(&v)))
	}, &GitHubAuthConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GitHub auth form cancelled")
}

// --- authFormAtIndex: out-of-range index returns nil ---

func TestAuthFormAtIndex_OutOfRange(t *testing.T) {
	t.Parallel()
	getter := authFormAtIndex(func(_ *GitHubAuthConfig) []*huh.Form { return nil }, 5)
	assert.Nil(t, getter(&GitHubAuthConfig{}))
}

// --- runForm: nil ---

func TestRunForm_Nil(t *testing.T) {
	t.Parallel()
	require.NoError(t, runForm(nil))
}

// --- generateKey upload error propagation ---

func TestGenerateKey_UploadError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := config.NewContainerFromViper(nil, viper.New())

	mockClient := mockVCS.NewMockGitHubClient(t)
	mockClient.EXPECT().UploadKey(mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)

	_, err := generateKey(p, cfg,
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(b *bool) *huh.Form { *b = true; return nil }),
		WithGitHubClientFactory(func(_ config.Containable) (githubvcs.GitHubClient, error) {
			return mockClient, nil
		}),
	)
	require.Error(t, err)
}

// --- IsConfigured: keychain reference present ---

func TestIsConfigured_KeychainRef(t *testing.T) {
	t.Parallel()
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	v := viper.New()
	v.Set("github.auth.keychain", "testtool/github.auth")
	v.Set("github.ssh.key.path", "/home/u/.ssh/id_ed25519")
	i := NewGitHubInitialiser(p, false, false)
	assert.True(t, i.IsConfigured(config.NewContainerFromViper(nil, v)))
}

// --- newAuthFormConfig defaults wired ---

func TestNewAuthFormConfig_Defaults(t *testing.T) {
	t.Parallel()
	c := newAuthFormConfig()
	require.NotNil(t, c.storageModeFormCreator)
	require.NotNil(t, c.envVarNameFormCreator)
	require.NotNil(t, c.fetchTokenFormCreator)
	require.NotNil(t, c.displayOnceFormCreator)
}

// --- setup registration smoke ---

func TestSetupRegistration(t *testing.T) {
	t.Parallel()
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cmd := NewCmdInitGitHub(p)
	require.NotNil(t, cmd)
	assert.IsType(t, &cobra.Command{}, cmd)
}

// --- validateSSHKey: passphrase-protected key is accepted ---

func TestValidateSSHKey_PassphraseProtected(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	key := passphraseProtectedKeyPEM(t)
	require.NoError(t, validateSSHKey(key, p))
}

// --- isValidSSHKey: passphrase-protected key is treated as valid ---

func TestIsValidSSHKey_PassphraseProtected(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	key := passphraseProtectedKeyPEM(t)
	require.NoError(t, afero.WriteFile(fs, "/protected.key", key, 0o600))
	assert.True(t, isValidSSHKey(fs, "/protected.key"))
}

// --- promptAndValidateSSHKey: file exists but content is invalid →
// validateSSHKey error branch. ---

func TestHandleSSHKeySelection_Other_InvalidKeyContent(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())
	require.NoError(t, afero.WriteFile(p.FS, "/garbage.key", []byte("not-a-key"), 0o600))

	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(s *string) *huh.Form {
			*s = "/garbage.key"

			return nil
		},
	}
	_, _, err := handleSSHKeySelection(p, cfg, "other", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid private key")
}

// --- discoverSSHKeys: directory missing → it is created and an empty
// slice is returned. Uses a MemMapFs with no .ssh dir present. ---

func TestDiscoverSSHKeys_CreatesMissingDir(t *testing.T) {
	homeDir := "/home/freshuser"
	t.Setenv("HOME", homeDir)

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	assert.Empty(t, keys)

	exists, _ := afero.DirExists(fs, filepath.Join(homeDir, ".ssh"))
	assert.True(t, exists, "discoverSSHKeys should create the missing .ssh dir")
}

// --- Configure: SSH stage skipped because a key path is already set,
// and login stage skipped because a credential exists. Exercises both
// guard branches in Configure returning nil. ---

func TestConfigure_BothAlreadyConfigured(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	v := viper.New()
	v.Set("github.auth.value", "tok")
	v.Set("github.ssh.key.path", "/home/u/.ssh/id_ed25519")
	cfg := config.NewContainerFromViper(nil, v)

	i := &GitHubInitialiser{SkipLogin: false, SkipKey: false}
	require.NoError(t, i.Configure(p, cfg))
}

// --- Configure: login skipped (credential present) but SSH must run;
// without a TTY the default select form errors, surfacing the SSH
// branch of Configure. ---

func TestConfigure_SSHRunsAndErrors(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	v := viper.New()
	v.Set("github.auth.value", "tok") // hasAnyGitHubCredential → skip login
	cfg := config.NewContainerFromViper(nil, v)

	i := &GitHubInitialiser{SkipLogin: false, SkipKey: false}
	err := i.Configure(p, cfg)
	require.Error(t, err)
}

// --- RunInitCmd: an existing config file makes LoadFilesContainer
// succeed (covers the non-fallback branch). The forced SSH stage errors
// without a TTY, so RunInitCmd returns that error before writing. ---

func TestRunInitCmd_ExistingConfigFile(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-here")

	fs := afero.NewMemMapFs()
	dir := "/cfgdir"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	target := filepath.Join(dir, setup.DefaultConfigFilename)
	require.NoError(t, afero.WriteFile(fs, target, []byte("github:\n  auth:\n    env: GITHUB_TOKEN\n"), 0o600))

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	err := RunInitCmd(p, dir)
	// Auth short-circuits on GITHUB_TOKEN; SSH default form errors.
	require.Error(t, err)
}

// pubWriteFailFs wraps an afero.Fs and fails Create/OpenFile for paths
// ending in ".pub", so the public-key write step in generateAndSaveSSHKey
// fails while the private-key write succeeds.
type pubWriteFailFs struct {
	afero.Fs
}

func (f pubWriteFailFs) Create(name string) (afero.File, error) {
	if filepath.Ext(name) == ".pub" {
		return nil, assert.AnError
	}

	return f.Fs.Create(name)
}

func (f pubWriteFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Ext(name) == ".pub" {
		return nil, assert.AnError
	}

	return f.Fs.OpenFile(name, flag, perm)
}

// --- generateAndSaveSSHKey: the public-key write error branch. ---

func TestGenerateAndSaveSSHKey_PubWriteError(t *testing.T) {
	t.Parallel()
	fs := pubWriteFailFs{Fs: afero.NewMemMapFs()}
	_, err := generateAndSaveSSHKey(fs, "/home/u/.ssh/id_test", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write public key")
}

// --- generateAndSaveSSHKey: read-only FS → private-key write fails. ---

func TestGenerateAndSaveSSHKey_WriteError(t *testing.T) {
	t.Parallel()
	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	_, err := generateAndSaveSSHKey(roFS, "/tmp/key", "passphrase-1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write private key")
}

// --- generateAndSaveSSHKey: success returns public key bytes. ---

func TestGenerateAndSaveSSHKey_Success(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	pub, err := generateAndSaveSSHKey(fs, "/home/u/.ssh/id_test", "")
	require.NoError(t, err)
	assert.NotEmpty(t, pub)

	exists, _ := afero.Exists(fs, "/home/u/.ssh/id_test")
	assert.True(t, exists)
	exists, _ = afero.Exists(fs, "/home/u/.ssh/id_test.pub")
	assert.True(t, exists)
}

// --- runForm: a non-nil form is run; without a TTY it errors, covering
// the form.Run() return path. ---

func TestRunForm_NonNilErrors(t *testing.T) {
	t.Parallel()
	var v string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Options(huh.NewOption("a", "a")).Value(&v),
	))
	require.Error(t, runForm(form))
}

// --- discoverSSHKeys: a real key present is surfaced as an option. ---

func TestDiscoverSSHKeys_FindsKey(t *testing.T) {
	homeDir := "/home/keyuser"
	t.Setenv("HOME", homeDir)

	fs := afero.NewMemMapFs()
	sshDir := filepath.Join(homeDir, ".ssh")
	require.NoError(t, fs.MkdirAll(sshDir, 0o700))
	keyPath := filepath.Join(sshDir, "id_ed25519")
	require.NoError(t, afero.WriteFile(fs, keyPath, generateUnencryptedKeyPEM(t), 0o600))

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	keys, err := discoverSSHKeys(p)
	require.NoError(t, err)
	found := false
	for _, k := range keys {
		if k.Value == keyPath {
			found = true
		}
	}
	assert.True(t, found, "the valid key should be discovered")
}

// --- ConfigureSSHKey: select-form error is propagated. ---

func TestConfigureSSHKey_SelectFormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	var v string
	_, _, err := ConfigureSSHKey(p, cfg,
		WithSSHKeySelectForm(func(_ *string, _ []huh.Option[string]) *huh.Form {
			return huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().Options(huh.NewOption("a", "a")).Value(&v),
			))
		}),
	)
	require.Error(t, err)
}

// --- promptAndValidateSSHKey: path form error is propagated. ---

func TestHandleSSHKeySelection_Other_FormError(t *testing.T) {
	t.Parallel()
	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	var v string
	opts := &configureSSHKeyConfig{
		sshKeyPathFormCreator: func(_ *string) *huh.Form {
			return huh.NewForm(huh.NewGroup(
				huh.NewText().Value(&v),
			))
		},
	}
	_, _, err := handleSSHKeySelection(p, cfg, "other", opts)
	require.Error(t, err)
}

// --- generateKey: passphrase form error is propagated (no TTY). ---

func TestGenerateKey_PassphraseFormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	var v string
	_, err := generateKey(p, cfg,
		WithPassphraseForm(func(_ *string) *huh.Form {
			return huh.NewForm(huh.NewGroup(huh.NewInput().Value(&v)))
		}),
	)
	require.Error(t, err)
}

// --- generateKey: upload-confirm form error is propagated (no TTY). ---

func TestGenerateKey_UploadFormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	var v bool
	_, err := generateKey(p, cfg,
		WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
		WithUploadConfirmForm(func(_ *bool) *huh.Form {
			return huh.NewForm(huh.NewGroup(huh.NewConfirm().Value(&v)))
		}),
	)
	require.Error(t, err)
}

// --- init(): the registered provider closures are driven directly so
// the InitialiserProvider, SubcommandProvider, and FeatureFlag bodies
// are exercised. ---

func TestRegisteredInitialiserProvider(t *testing.T) {
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	ips := setup.GetInitialisers()[props.FeatureCmd("github")]
	require.NotEmpty(t, ips)

	// Default flags: skipLogin/skipKey both false → a real initialiser.
	skipLogin = false
	skipKey = false
	init := ips[0](p)
	require.NotNil(t, init)
	assert.Equal(t, "GitHub integration", init.Name())

	// Both skipped → provider returns nil.
	skipLogin = true
	skipKey = true
	t.Cleanup(func() { skipLogin = false; skipKey = false })
	assert.Nil(t, ips[0](p))
}

func TestRegisteredSubcommandProvider(t *testing.T) {
	t.Parallel()
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	sps := setup.GetSubcommands()[props.FeatureCmd("github")]
	require.NotEmpty(t, sps)

	cmds := sps[0](p)
	require.Len(t, cmds, 1)
	assert.Equal(t, "github", cmds[0].Use)
}

func TestRegisteredFeatureFlag(t *testing.T) {
	t.Parallel()
	fps := setup.GetFeatureFlags()[props.FeatureCmd("github")]
	require.NotEmpty(t, fps)

	cmd := &cobra.Command{Use: "init"}
	fps[0](cmd)
	require.NotNil(t, cmd.Flags().Lookup("skip-login"))
	require.NotNil(t, cmd.Flags().Lookup("skip-key"))
}

// --- authFormAtIndex: in-range index returns the form at that slot. ---

func TestAuthFormAtIndex_InRange(t *testing.T) {
	t.Parallel()
	want := huh.NewForm(huh.NewGroup(huh.NewNote().Title("x")))
	getter := authFormAtIndex(func(_ *GitHubAuthConfig) []*huh.Form {
		return []*huh.Form{want}
	}, 0)
	assert.Same(t, want, getter(&GitHubAuthConfig{}))
}

// --- NewGitHubInitialiser: mounts assets when Props.Assets is set. ---

func TestNewGitHubInitialiser_MountsAssets(t *testing.T) {
	t.Parallel()
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
		Assets: props.NewAssets(),
	}
	i := NewGitHubInitialiser(p, false, false)
	require.NotNil(t, i)
}

// --- discoverSSHKeys: read-only FS with no .ssh dir → MkdirAll error. ---

func TestDiscoverSSHKeys_MkdirError(t *testing.T) {
	homeDir := "/home/rohome"
	t.Setenv("HOME", homeDir)

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	p := &props.Props{
		FS:     roFS,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	_, err := discoverSSHKeys(p)
	require.Error(t, err)
}

// --- handleSSHKeySelection generate → generateKey error wrapped.
// A read-only FS makes the .ssh MkdirAll in generateKey fail. ---

func TestHandleSSHKeySelection_Generate_Error(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	p := &props.Props{
		FS:     roFS,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := config.NewContainerFromViper(nil, viper.New())

	opts := &configureSSHKeyConfig{
		generateKeyOpts: []GenerateKeyOption{
			WithPassphraseForm(func(s *string) *huh.Form { *s = ""; return nil }),
			WithUploadConfirmForm(func(b *bool) *huh.Form { *b = false; return nil }),
		},
	}
	_, _, err := handleSSHKeySelection(p, cfg, "generate", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate SSH key")
}

// --- runEnvVarAuth: FetchToken=true but OAuth + manual capture fail →
// the captureToken error is returned. Forms are skipped (nil), so the
// captured-token branch is what we exercise. ---

func TestConfigure_EnvVarFetchTokenCaptureError(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "", assert.AnError }),
		WithGitHubAuthForms(authFormOverride(
			func(c *GitHubAuthConfig) {
				c.StorageMode = credentials.ModeEnvVar
				c.EnvVarName = "GITHUB_TOKEN"
				c.FetchToken = true
			},
			func(_, _ string) *huh.Form { return nil },
		)),
	)
	// OAuth fails → manual prompt (no TTY) fails → error propagates.
	require.Error(t, init.Configure(p, cfg))
}

// --- runAuthCredentialStage: keychain capture succeeds but the write
// fails because the tool name is empty. Routed through Configure. ---

func TestConfigure_KeychainWriteError_NoToolName(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: ""}, // forces the write-stage error
	}
	cfg := config.NewContainerFromViper(nil, viper.New())

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "ghp_tok", nil }),
		WithGitHubAuthForms(authFormOverride(
			func(c *GitHubAuthConfig) { c.StorageMode = credentials.ModeKeychain },
			nil,
		)),
	)
	err := init.Configure(p, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a tool name")
}

// --- runEnvVarAuth: the env-var-name form stage error propagates.
// Injecting a real (TTY-less) form for that slot triggers the error. ---

func TestConfigure_EnvVarNameFormError(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	var name string
	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "tok", nil }),
		WithGitHubAuthForms(WithAuthForm(
			func(c *GitHubAuthConfig) []*huh.Form {
				c.StorageMode = credentials.ModeEnvVar

				return []*huh.Form{
					nil, // storage-mode slot: skip
					huh.NewForm(huh.NewGroup(huh.NewInput().Value(&name))), // env-var-name slot: real form (no TTY → error)
				}
			},
			func(_, _ string) *huh.Form { return nil },
		)),
	)
	require.Error(t, init.Configure(p, cfg))
}

// --- ConfigureSSHKey: discoverSSHKeys error (read-only FS, no .ssh dir)
// propagates out of ConfigureSSHKey. ---

func TestConfigureSSHKey_DiscoverError(t *testing.T) {
	t.Setenv("HOME", "/home/rohome2")

	roFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
	p := &props.Props{
		FS:     roFS,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := config.NewContainerFromViper(nil, viper.New())

	_, _, err := ConfigureSSHKey(p, cfg)
	require.Error(t, err)
}

// --- configureAuth: storage-mode form stage error propagates. A real
// (TTY-less) form is injected into the storage-mode slot. ---

func TestConfigure_StorageModeFormError(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	cfg := config.NewContainerFromViper(nil, viper.New())

	var mode credentials.Mode
	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "tok", nil }),
		WithGitHubAuthForms(WithAuthForm(
			func(_ *GitHubAuthConfig) []*huh.Form {
				return []*huh.Form{
					huh.NewForm(huh.NewGroup(
						huh.NewSelect[credentials.Mode]().
							Options(huh.NewOption("env", credentials.ModeEnvVar)).
							Value(&mode),
					)),
				}
			},
			func(_, _ string) *huh.Form { return nil },
		)),
	)
	require.Error(t, init.Configure(p, cfg))
}

func TestDefaultConfigDirUsedForFlag(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	dir := setup.GetDefaultConfigDir(fs, "testtool")
	assert.NotEmpty(t, dir)
	assert.Contains(t, filepath.ToSlash(dir), "testtool")
}
