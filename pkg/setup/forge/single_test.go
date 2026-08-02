package forge

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// --- Name ---

func TestGitHubInitialiser_Name(t *testing.T) {
	t.Parallel()

	i := &Initialiser{profile: gitHubProfile}
	assert.Equal(t, "GitHub integration", i.Name())
}

// --- Initialiser flows ---

func TestGitHubInitialiser(t *testing.T) {
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)
	// Ensure env-var-mode detection does not short-circuit the OAuth path; this
	// test exercises the legacy literal-write behaviour and must not inherit
	// GITHUB_TOKEN from the runner.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("CI", "")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(authProviderFactory("mock-token", nil)),
		WithAuthForms(WithAuthForm(
			func(cfg *AuthConfig) []*huh.Form {
				cfg.StorageMode = credentials.ModeLiteral

				return nil // skip every staged form
			},
			func(_ string, _ string) *huh.Form { return nil },
		)),
	)
	require.NoError(t, init.Configure(t.Context(), p, cfg))

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
		WithProviderFactory(fatalOnLoginProvider(t)),
	)

	err := init.Configure(t.Context(), p, cfg)
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
		WithProviderFactory(fatalOnLoginProvider(t)),
	)

	require.NoError(t, init.Configure(t.Context(), p, cfg))
	// No literal token must be written — the env var is the source of truth.
	assert.Empty(t, cfg.View().GetString("github.auth.value"))
}

func TestConfigure_SkipBoth(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	// A mock editor proves both stages are skipped: only the unconditional
	// post-auth View read happens.
	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, ""))
	i := &Initialiser{profile: gitHubProfile, SkipLogin: true, SkipKey: true}
	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

func TestConfigure_LoginAlreadySet_SkipKey(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t, "github:\n  auth:\n    value: already-set-token\n"))
	i := &Initialiser{profile: gitHubProfile, SkipLogin: false, SkipKey: true}
	// SkipLogin=false but auth.value != "" so the login branch is not entered.
	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

func TestConfigure_BothAlreadyConfigured(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(testutil.ViewFromYAML(t,
		"github:\n  auth:\n    value: tok\n  ssh:\n    key:\n      path: /home/u/.ssh/id_ed25519\n"))

	i := &Initialiser{profile: gitHubProfile, SkipLogin: false, SkipKey: false}
	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

func TestConfigure_SSHRunsAndErrors(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	// hasAnySingleCredential → skip login; SSH default form errors (no TTY).
	cfg := newTestEditor(t, p, "github:\n  auth:\n    value: tok\n")

	i := &Initialiser{profile: gitHubProfile, SkipLogin: false, SkipKey: false}
	require.Error(t, i.Configure(t.Context(), p, cfg))
}

func TestConfigureSSH_FormError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")
	g := &Initialiser{profile: gitHubProfile}

	require.Error(t, g.configureSSH(p, cfg))
}

// --- IsConfigured ---

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

func TestIsConfigured_KeychainRef(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := testutil.ViewFromYAML(t,
		"github:\n  auth:\n    keychain: testtool/github.auth\n  ssh:\n    key:\n      path: /home/u/.ssh/id_ed25519\n")
	i := NewGitHubInitialiser(p, false, false)
	assert.True(t, i.IsConfigured(cfg))
}

// --- default auth form creators (construction only; not run against a TTY) ---

func TestSingleStorageModeForm(t *testing.T) {
	t.Parallel()

	cfg := &AuthConfig{}
	form := singleStorageModeForm(gitHubProfile, cfg)
	require.NotNil(t, form)
	assert.Equal(t, credentials.ModeEnvVar, cfg.StorageMode)
}

func TestSingleStorageModeForm_PreservesExistingMode(t *testing.T) {
	// CI runners set CI=true, which drops the literal option (refused under CI)
	// from the select; huh then resets the bound value off the missing option.
	// Force non-CI so the existing-mode preservation is deterministic.
	t.Setenv("CI", "")

	cfg := &AuthConfig{StorageMode: credentials.ModeLiteral}
	form := singleStorageModeForm(gitHubProfile, cfg)
	require.NotNil(t, form)
	assert.Equal(t, credentials.ModeLiteral, cfg.StorageMode)
}

func TestSingleStorageModeDescription(t *testing.T) {
	t.Parallel()

	assert.Contains(t, singleStorageModeDescription(true), "CI environment detected")
	assert.Contains(t, singleStorageModeDescription(false), "recommended")
}

func TestSingleEnvVarNameForm(t *testing.T) {
	t.Parallel()

	cfg := &AuthConfig{}
	form := singleEnvVarNameForm(gitHubProfile, cfg)
	require.NotNil(t, form)
	assert.Equal(t, "GITHUB_TOKEN", cfg.EnvVarName)
}

func TestSingleEnvVarNameForm_PreservesExistingName(t *testing.T) {
	t.Parallel()

	cfg := &AuthConfig{EnvVarName: "MYTOOL_GH"}
	form := singleEnvVarNameForm(gitHubProfile, cfg)
	require.NotNil(t, form)
	assert.Equal(t, "MYTOOL_GH", cfg.EnvVarName)
}

func TestSingleFetchTokenForm(t *testing.T) {
	t.Parallel()

	cfg := &AuthConfig{}
	form := singleFetchTokenForm(cfg)
	require.NotNil(t, form)
	assert.True(t, cfg.FetchToken)
}

func TestSingleDisplayOnceForm(t *testing.T) {
	t.Parallel()

	form := singleDisplayOnceForm("GITHUB_TOKEN", "ghp_secret")
	require.NotNil(t, form)
}

func TestNewAuthFormConfig_Defaults(t *testing.T) {
	t.Parallel()

	c := newAuthFormConfig(gitHubProfile)
	require.NotNil(t, c.storageModeFormCreator)
	require.NotNil(t, c.envVarNameFormCreator)
	require.NotNil(t, c.fetchTokenFormCreator)
	require.NotNil(t, c.displayOnceFormCreator)
}

// --- captureToken: OAuth success and manual fallback ---

func TestCaptureToken_OAuthSuccess(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	g := &Initialiser{profile: gitHubProfile, providerFactory: authProviderFactory("ghp_oauth", nil)}
	token, err := g.captureToken(t.Context(), p, cfg)
	require.NoError(t, err)
	assert.Equal(t, "ghp_oauth", token)
}

func TestCaptureToken_FallbackToManual(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	g := &Initialiser{profile: gitHubProfile, providerFactory: authProviderFactory("", assert.AnError)}
	_, err := g.captureToken(t.Context(), p, cfg)
	require.Error(t, err)
}

func TestCaptureToken_NotSupportedFallsBack(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := testutil.ViewFromYAML(t, "")
	g := &Initialiser{profile: gitHubProfile, providerFactory: func(config.Reader) (forge.Provider, error) {
		return noAuthProvider{}, nil
	}}
	_, err := g.captureToken(t.Context(), p, cfg)
	require.Error(t, err)
}

// --- runAuthCredentialStage: unsupported mode ---

func TestRunAuthCredentialStage_UnsupportedMode(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	// The dispatch fails before any config write, so a no-expectation mock
	// editor suffices.
	cfg := setupmocks.NewMockEditor(t)
	g := &Initialiser{profile: gitHubProfile, providerFactory: authProviderFactory("tok", nil)}
	authCfg := &AuthConfig{StorageMode: credentials.Mode("bogus")}

	err := g.runAuthCredentialStage(t.Context(), p, cfg, newAuthFormConfig(gitHubProfile), authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential storage mode")
}

// --- writeSingleCredential branches ---

func TestWriteGitHubCredential_EnvVarMode(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")
	authCfg := &AuthConfig{StorageMode: credentials.ModeEnvVar, EnvVarName: "MY_GH"}
	require.NoError(t, writeSingleCredential(t.Context(), gitHubProfile, cfg, "testtool", authCfg))
	assert.Equal(t, "MY_GH", cfg.View().GetString("github.auth.env"))
}

func TestWriteGitHubCredential_EnvVarModeEmptyName(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")
	authCfg := &AuthConfig{StorageMode: credentials.ModeEnvVar}
	require.NoError(t, writeSingleCredential(t.Context(), gitHubProfile, cfg, "testtool", authCfg))
	assert.Empty(t, cfg.View().GetString("github.auth.env"))
}

func TestWriteGitHubCredential_KeychainNoToolName(t *testing.T) {
	t.Parallel()

	// Fails before any write — no editor expectations needed.
	cfg := setupmocks.NewMockEditor(t)
	authCfg := &AuthConfig{StorageMode: credentials.ModeKeychain, Token: "tok"}
	err := writeSingleCredential(t.Context(), gitHubProfile, cfg, "", authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a tool name")
}

func TestWriteGitHubCredential_LiteralMode(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")
	authCfg := &AuthConfig{StorageMode: credentials.ModeLiteral, Token: "ghp_lit"}
	require.NoError(t, writeSingleCredential(t.Context(), gitHubProfile, cfg, "testtool", authCfg))
	assert.Equal(t, "ghp_lit", cfg.View().GetString("github.auth.value"))
}

func TestWriteGitHubCredential_EmptyModeDefaultsLiteral(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")
	authCfg := &AuthConfig{StorageMode: "", Token: "ghp_empty"}
	require.NoError(t, writeSingleCredential(t.Context(), gitHubProfile, cfg, "testtool", authCfg))
	assert.Equal(t, "ghp_empty", cfg.View().GetString("github.auth.value"))
}

func TestWriteGitHubCredential_UnsupportedMode(t *testing.T) {
	t.Parallel()

	// Fails on the mode switch before any write.
	cfg := setupmocks.NewMockEditor(t)
	authCfg := &AuthConfig{StorageMode: credentials.Mode("nope"), Token: "tok"}
	err := writeSingleCredential(t.Context(), gitHubProfile, cfg, "testtool", authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential storage mode")
}

// --- runAuthFormStage ---

func TestRunAuthFormStage_NilCreator(t *testing.T) {
	t.Parallel()
	require.NoError(t, runAuthFormStage(nil, &AuthConfig{}))
}

func TestRunAuthFormStage_NilForm(t *testing.T) {
	t.Parallel()
	require.NoError(t, runAuthFormStage(func(_ *AuthConfig) *huh.Form { return nil }, &AuthConfig{}))
}

func TestRunAuthFormStage_FormRunError(t *testing.T) {
	t.Parallel()

	var v string

	err := runAuthFormStage(func(_ *AuthConfig) *huh.Form {
		return huh.NewForm(huh.NewGroup(huh.NewInput().Value(&v)))
	}, &AuthConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth form cancelled")
}

// --- authFormAtIndex ---

func TestAuthFormAtIndex_OutOfRange(t *testing.T) {
	t.Parallel()

	getter := authFormAtIndex(func(_ *AuthConfig) []*huh.Form { return nil }, 5)
	assert.Nil(t, getter(&AuthConfig{}))
}

func TestAuthFormAtIndex_InRange(t *testing.T) {
	t.Parallel()

	want := huh.NewForm(huh.NewGroup(huh.NewNote().Title("x")))
	getter := authFormAtIndex(func(_ *AuthConfig) []*huh.Form {
		return []*huh.Form{want}
	}, 0)
	assert.Same(t, want, getter(&AuthConfig{}))
}

// --- Configure error paths routed through the wizard ---

func TestConfigure_EnvVarFetchTokenCaptureError(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(authProviderFactory("", assert.AnError)),
		WithAuthForms(authFormOverride(
			func(c *AuthConfig) {
				c.StorageMode = credentials.ModeEnvVar
				c.EnvVarName = "GITHUB_TOKEN"
				c.FetchToken = true
			},
			func(_, _ string) *huh.Form { return nil },
		)),
	)
	// OAuth fails → manual prompt (no TTY) fails → error propagates.
	require.Error(t, init.Configure(t.Context(), p, cfg))
}

func TestConfigure_KeychainWriteError_NoToolName(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: ""}, // forces the write-stage error
	}
	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(authProviderFactory("ghp_tok", nil)),
		WithAuthForms(authFormOverride(
			func(c *AuthConfig) { c.StorageMode = credentials.ModeKeychain },
			nil,
		)),
	)
	err := init.Configure(t.Context(), p, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a tool name")
}

func TestConfigure_EnvVarNameFormError(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")

	var name string

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(authProviderFactory("tok", nil)),
		WithAuthForms(WithAuthForm(
			func(c *AuthConfig) []*huh.Form {
				c.StorageMode = credentials.ModeEnvVar

				return []*huh.Form{
					nil, // storage-mode slot: skip
					huh.NewForm(huh.NewGroup(huh.NewInput().Value(&name))), // env-var-name slot: real form (no TTY → error)
				}
			},
			func(_, _ string) *huh.Form { return nil },
		)),
	)
	require.Error(t, init.Configure(t.Context(), p, cfg))
}

func TestConfigure_StorageModeFormError(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "")

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")

	var mode credentials.Mode

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(authProviderFactory("tok", nil)),
		WithAuthForms(WithAuthForm(
			func(_ *AuthConfig) []*huh.Form {
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
	require.Error(t, init.Configure(t.Context(), p, cfg))
}

// --- RunGitHubInit / command wiring ---

func TestRunGitHubInit_AuthShortCircuitThenSSHError(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-here")

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "")

	// Auth resolves from GITHUB_TOKEN; SSH default form errors (no TTY).
	require.Error(t, RunGitHubInit(t.Context(), p, cfg))
}

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

func TestNewCmdInitGitHub_RunE_Error(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-here")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
		Assets: props.NewAssets(),
	}
	cmd := NewCmdInitGitHub(p)
	cmd.SetContext(t.Context())
	require.NoError(t, cmd.Flags().Set("dir", "/cfgdir"))

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure GitHub")
}

func TestRunInitCmd_ExistingConfigFile(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")
	t.Setenv("GITHUB_TOKEN", "already-here")

	fs := afero.NewMemMapFs()
	dir := "/cfgdir"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	target := dir + "/" + setup.DefaultConfigFilename
	require.NoError(t, afero.WriteFile(fs, target, []byte("github:\n  auth:\n    env: GITHUB_TOKEN\n"), 0o600))

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
		Assets: props.NewAssets(),
	}

	// Auth short-circuits on GITHUB_TOKEN; SSH default form errors.
	require.Error(t, RunGitHubInitCmd(t.Context(), p, dir))
}

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

func TestDefaultConfigDirUsedForFlag(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := setup.GetDefaultConfigDir(fs, "testtool")
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, "testtool")
}

// --- init(): registered closures ---

func TestRegisteredInitialiserProvider(t *testing.T) {
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	ips := setup.GetInitialisers()[props.FeatureID("github")]
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

	sps := setup.GetSubcommands()[props.FeatureID("github")]
	require.NotEmpty(t, sps)

	cmds := sps[0](p)
	require.Len(t, cmds, 1)
	assert.Equal(t, "github", cmds[0].Use)
}

func TestRegisteredFeatureFlag(t *testing.T) {
	t.Parallel()

	fps := setup.GetFeatureFlags()[props.FeatureID("github")]
	require.NotEmpty(t, fps)

	cmd := &cobra.Command{Use: "init"}
	fps[0](cmd)
	require.NotNil(t, cmd.Flags().Lookup("skip-login"))
	require.NotNil(t, cmd.Flags().Lookup("skip-key"))
}
