package forge

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// newDualTestProps builds a fixture Props for the dual-credential tests and
// clears CI so the CI-refusal branches don't fire. It mutates the environment,
// so callers must not run in parallel.
func newDualTestProps(t *testing.T) *props.Props {
	t.Helper()

	t.Setenv("CI", "")

	return newTestProps(t)
}

// mockForms builds a DualFormOption that applies cfgMutate up-front and returns
// nil forms for every stage — letting tests drive the wizard without a TTY.
func mockForms(cfgMutate func(*DualConfig)) DualFormOption {
	return WithDualForm(func(cfg *DualConfig) []*huh.Form {
		cfgMutate(cfg)

		return nil
	})
}

// failingForm returns a form with a single input group that errors on Run in a
// non-TTY env. Used to exercise the error-return branches of runForms.
func failingForm() *huh.Form {
	var s string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("x").Value(&s),
		),
	)
}

// --- Name ---

func TestInitialiser_Name(t *testing.T) {
	t.Parallel()

	i := &Initialiser{profile: bitbucketProfile}
	assert.Equal(t, "Bitbucket authentication", i.Name())
}

// --- Configure flows ---

// TestConfigure_EnvVarMode pins the two-env-var write path: one transactional
// Apply sets the env refs and removes every key the other storage modes own,
// with each on-path literal cleared before the env ref that nests inside it.
func TestConfigure_EnvVarMode(t *testing.T) {
	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password"),
		config.Set("bitbucket.app_password.env", "BB_APP_PW"),
		config.Remove("bitbucket.username"),
		config.Set("bitbucket.username.env", "BB_USER"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	i := NewBitbucketInitialiser(p, WithDualForms(mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	})))

	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

// TestConfigure_KeychainMode — the captured username + app_password get
// serialised to a JSON blob and stored under one keychain entry; the config
// records only the reference.
func TestConfigure_KeychainMode(t *testing.T) {
	credtest.Install(t)

	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Set("bitbucket.keychain", "testtool/bitbucket.auth"),
		config.Remove("bitbucket.username.env"),
		config.Remove("bitbucket.app_password.env"),
		config.Remove("bitbucket.username"),
		config.Remove("bitbucket.app_password"),
	}).Return(nil)

	i := NewBitbucketInitialiser(p, WithDualForms(mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeKeychain
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	require.NoError(t, i.Configure(t.Context(), p, cfg))

	raw, err := credentials.Retrieve(t.Context(), "testtool", "bitbucket.auth")
	require.NoError(t, err)

	var blob map[string]string

	require.NoError(t, json.Unmarshal([]byte(raw), &blob))
	assert.Equal(t, "alice", blob["username"])
	assert.Equal(t, "s3cret", blob["app_password"])
}

// TestConfigure_LiteralMode — both fields land in config as plaintext, with the
// nested env refs removed before their parent scalars are set and the keychain
// ref removed last.
func TestConfigure_LiteralMode(t *testing.T) {
	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password.env"),
		config.Set("bitbucket.app_password", "s3cret"),
		config.Remove("bitbucket.username.env"),
		config.Set("bitbucket.username", "alice"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	i := NewBitbucketInitialiser(p, WithDualForms(mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeLiteral
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

// TestConfigure_CIRefusesLiteral — belt-and-braces guard: even if the form
// selection bypassed the CI filter, the configure step refuses before writing
// anything.
func TestConfigure_CIRefusesLiteral(t *testing.T) {
	p := newDualTestProps(t)
	// newDualTestProps cleared CI — re-set after.
	t.Setenv("CI", "true")

	cfg := setupmocks.NewMockEditor(t)

	i := NewBitbucketInitialiser(p, WithDualForms(mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeLiteral
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	err := i.Configure(t.Context(), p, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal credential storage is refused under CI")
}

// --- IsConfigured ---

func TestIsConfigured(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantYes bool
	}{
		{"empty", "", false},
		{"env-var username", "bitbucket:\n  username:\n    env: BB_USER\n", true},
		{"env-var app_password", "bitbucket:\n  app_password:\n    env: BB_APP_PW\n", true},
		{"keychain ref", "bitbucket:\n  keychain: tool/bitbucket.auth\n", true},
		{"literal username", "bitbucket:\n  username: alice\n", true},
		{"literal app_password", "bitbucket:\n  app_password: s3cret\n", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			view := testutil.ViewFromYAML(t, tc.yaml)
			i := &Initialiser{profile: bitbucketProfile}

			assert.Equal(t, tc.wantYes, i.IsConfigured(view))
		})
	}
}

// --- default form creators ---

// TestDualStorageModeForm constructs the storage-mode selector and asserts the
// default mode is seeded to env-var when unset, and that a pre-set mode is
// preserved.
func TestDualStorageModeForm(t *testing.T) {
	// CI runners set CI=true, which drops the literal option (refused under CI)
	// from the select; force non-CI so existing-mode preservation is
	// deterministic.
	t.Setenv("CI", "")

	cfg := &DualConfig{}
	form := dualStorageModeForm(bitbucketProfile, cfg)
	require.NotNil(t, form)
	assert.Equal(t, credentials.ModeEnvVar, cfg.StorageMode)

	cfg2 := &DualConfig{StorageMode: credentials.ModeLiteral}
	form2 := dualStorageModeForm(bitbucketProfile, cfg2)
	require.NotNil(t, form2)
	assert.Equal(t, credentials.ModeLiteral, cfg2.StorageMode)
}

func TestDualStorageModeDescription(t *testing.T) {
	t.Parallel()

	assert.Contains(t, dualStorageModeDescription(true), "CI environment detected")
	assert.Contains(t, dualStorageModeDescription(false), "Env-var references")
}

func TestDualEnvVarNamesForm(t *testing.T) {
	t.Parallel()

	cfg := &DualConfig{}
	form := dualEnvVarNamesForm(bitbucketProfile, cfg)
	require.NotNil(t, form)
	assert.Equal(t, "BITBUCKET_USERNAME", cfg.UsernameEnvName)
	assert.Equal(t, "BITBUCKET_APP_PASSWORD", cfg.AppPasswordEnvName)

	cfg2 := &DualConfig{UsernameEnvName: "U", AppPasswordEnvName: "P"}
	form2 := dualEnvVarNamesForm(bitbucketProfile, cfg2)
	require.NotNil(t, form2)
	assert.Equal(t, "U", cfg2.UsernameEnvName)
	assert.Equal(t, "P", cfg2.AppPasswordEnvName)
}

func TestDualCredentialsForm(t *testing.T) {
	t.Parallel()

	cfg := &DualConfig{}
	form := dualCredentialsForm(bitbucketProfile, cfg)
	require.NotNil(t, form)
}

func TestFormAtIndex(t *testing.T) {
	t.Parallel()

	creator := func(_ *DualConfig) []*huh.Form {
		return []*huh.Form{huh.NewForm()}
	}

	got := formAtIndex(creator, 0)(&DualConfig{})
	assert.NotNil(t, got)

	missing := formAtIndex(creator, 5)(&DualConfig{})
	assert.Nil(t, missing)
}

func TestRunFormStage(t *testing.T) {
	t.Parallel()

	require.NoError(t, runFormStage(nil, &DualConfig{}))

	nilForm := func(_ *DualConfig) *huh.Form { return nil }
	require.NoError(t, runFormStage(nilForm, &DualConfig{}))
}

func TestRunFormStage_Success(t *testing.T) {
	t.Parallel()

	creator := func(_ *DualConfig) *huh.Form { return huh.NewForm() }
	require.NoError(t, runFormStage(creator, &DualConfig{}))
}

// --- writeDualCredentials branches ---

func TestWriteBitbucketCredentials_DefaultMode(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	err := writeDualCredentials(t.Context(), bitbucketProfile, cfg, "tool", &DualConfig{
		StorageMode: credentials.Mode("bogus"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported Bitbucket credential storage mode")
}

func TestWriteBitbucketCredentials_EmptyMode(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password.env"),
		config.Set("bitbucket.app_password", "pw"),
		config.Remove("bitbucket.username.env"),
		config.Set("bitbucket.username", "alice"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	err := writeDualCredentials(t.Context(), bitbucketProfile, cfg, "tool", &DualConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.NoError(t, err)
}

func TestWriteBitbucketCredentials_EnvVarPartial(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.username"),
		config.Set("bitbucket.username.env", "ONLY_USER"),
		config.Remove("bitbucket.app_password.env"),
		config.Remove("bitbucket.app_password"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	err := writeDualCredentials(t.Context(), bitbucketProfile, cfg, "tool", &DualConfig{
		StorageMode:     credentials.ModeEnvVar,
		UsernameEnvName: "ONLY_USER",
	})
	require.NoError(t, err)
}

func TestWriteBitbucketCredentials_ModeSwitchClearsStaleKeys(t *testing.T) {
	openEditor := func(t *testing.T, yamlDoc string) (setup.Editor, *props.Props, string) {
		t.Helper()

		p := newDualTestProps(t)
		const dir = "/cfgdir"

		path := filepath.Join(dir, setup.DefaultConfigFilename)
		require.NoError(t, p.FS.MkdirAll(dir, 0o755))
		require.NoError(t, afero.WriteFile(p.FS, path, []byte(yamlDoc), 0o600))

		editor, _, err := setup.OpenConfigEditor(t.Context(), p, dir, false)
		require.NoError(t, err)

		return editor, p, path
	}

	t.Run("env-var mode clears stale literals", func(t *testing.T) {
		cfg, p, path := openEditor(t, "bitbucket:\n  username: alice\n  app_password: s3cret-stale\n")

		userVar, appVar := "BB_USER", "BB_APP_PW"

		require.NoError(t, writeDualCredentials(t.Context(), bitbucketProfile, cfg, "testtool", &DualConfig{
			StorageMode:        credentials.ModeEnvVar,
			UsernameEnvName:    userVar,
			AppPasswordEnvName: appVar,
		}))

		view := cfg.View()
		assert.Equal(t, "BB_USER", view.GetString("bitbucket.username.env"))
		assert.Equal(t, "BB_APP_PW", view.GetString("bitbucket.app_password.env"))

		content, err := afero.ReadFile(p.FS, path)
		require.NoError(t, err)
		assert.NotContains(t, string(content), "s3cret-stale",
			"switching to env-var mode must remove the stale literal app password from the file")
		assert.NotContains(t, string(content), "alice",
			"switching to env-var mode must remove the stale literal username from the file")
	})

	t.Run("literal mode clears stale env refs", func(t *testing.T) {
		cfg, _, _ := openEditor(t, "bitbucket:\n  username:\n    env: BB_USER\n  app_password:\n    env: BB_APP_PW\n")

		require.NoError(t, writeDualCredentials(t.Context(), bitbucketProfile, cfg, "testtool", &DualConfig{
			StorageMode: credentials.ModeLiteral,
			Username:    "alice",
			AppPassword: "s3cret",
		}))

		view := cfg.View()
		assert.Equal(t, "alice", view.GetString("bitbucket.username"))
		assert.Equal(t, "s3cret", view.GetString("bitbucket.app_password"))
		assert.False(t, view.IsSet("bitbucket.username.env"),
			"switching to literal mode must remove the stale username env ref")
		assert.False(t, view.IsSet("bitbucket.app_password.env"),
			"switching to literal mode must remove the stale app-password env ref")
	})
}

// --- writeKeychainBlob branches ---

func TestWriteKeychainBlob_MissingFields(t *testing.T) {
	credtest.Install(t)

	cfg := setupmocks.NewMockEditor(t)

	err := writeKeychainBlob(t.Context(), bitbucketProfile, cfg, "tool", &DualConfig{
		Username:    "alice",
		AppPassword: "", // missing
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires both username and app password")
}

func TestWriteKeychainBlob_NoToolName(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	err := writeKeychainBlob(t.Context(), bitbucketProfile, cfg, "", &DualConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a tool name")
}

func TestWriteKeychainBlob_Success(t *testing.T) {
	credtest.Install(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Set("bitbucket.keychain", "tool/bitbucket.auth"),
		config.Remove("bitbucket.username.env"),
		config.Remove("bitbucket.app_password.env"),
		config.Remove("bitbucket.username"),
		config.Remove("bitbucket.app_password"),
	}).Return(nil)

	err := writeKeychainBlob(t.Context(), bitbucketProfile, cfg, "tool", &DualConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.NoError(t, err)
}

func TestWriteKeychainBlob_StoreError(t *testing.T) {
	credtest.Install(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := setupmocks.NewMockEditor(t)
	err := writeKeychainBlob(ctx, bitbucketProfile, cfg, "tool", &DualConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing Bitbucket credentials in OS keychain")
}

// --- runForms error branches ---

func TestRunForms_EnvVarStageError(t *testing.T) {
	t.Setenv("CI", "")

	opt := WithDualForm(func(cfg *DualConfig) []*huh.Form {
		cfg.StorageMode = credentials.ModeEnvVar
		// slot 0 (storage mode) nil → skipped; slot 1 (env-var) fails.
		return []*huh.Form{nil, failingForm()}
	})

	_, err := runForms(bitbucketProfile, opt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth form cancelled")
}

func TestRunForms_CredentialsStageError(t *testing.T) {
	t.Setenv("CI", "")

	opt := WithDualForm(func(cfg *DualConfig) []*huh.Form {
		cfg.StorageMode = credentials.ModeLiteral
		// slot 0 nil → skipped; slot 2 (credentials) fails.
		return []*huh.Form{nil, nil, failingForm()}
	})

	_, err := runForms(bitbucketProfile, opt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth form cancelled")
}

func TestRunForms_StorageStageError(t *testing.T) {
	t.Setenv("CI", "")

	opt := WithDualForm(func(_ *DualConfig) []*huh.Form {
		return []*huh.Form{failingForm()}
	})

	_, err := runForms(bitbucketProfile, opt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth form cancelled")
}

// --- registration ---

func TestInitRegistry(t *testing.T) {
	feature := props.FeatureCmd("bitbucket")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	// InitialiserProvider: non-skip branch returns a live initialiser.
	ips := setup.GetInitialisers()[feature]
	require.NotEmpty(t, ips)

	skipBitbucket = false

	init0 := ips[0](p)
	require.NotNil(t, init0)
	assert.Equal(t, "Bitbucket authentication", init0.Name())

	// InitialiserProvider: skip branch returns nil.
	skipBitbucket = true

	skipped := ips[0](p)
	assert.Nil(t, skipped)

	skipBitbucket = false

	// SubcommandProvider yields the init bitbucket command.
	sps := setup.GetSubcommands()[feature]
	require.NotEmpty(t, sps)

	cmds := sps[0](p)
	require.Len(t, cmds, 1)
	assert.Equal(t, "bitbucket", cmds[0].Use)

	// FeatureFlag registers the --skip-bitbucket flag on a command.
	fps := setup.GetFeatureFlags()[feature]
	require.NotEmpty(t, fps)

	target := cmds[0]
	fps[0](target)
	assert.NotNil(t, target.Flags().Lookup("skip-bitbucket"))
}

func TestNewInitialiser_Options(t *testing.T) {
	t.Parallel()

	i := NewBitbucketInitialiser(nil, WithDualForms(mockForms(func(_ *DualConfig) {})))
	assert.Len(t, i.dualOpts, 1)
}

func TestNewCmdInitBitbucket(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cmd := NewCmdInitBitbucket(p)
	require.NotNil(t, cmd)
	assert.Equal(t, "bitbucket", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	f := cmd.Flags().Lookup("dir")
	require.NotNil(t, f)
}

// --- RunBitbucketInit / RunBitbucketInitCmd ---

func TestRunBitbucketInit_Success(t *testing.T) {
	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password"),
		config.Set("bitbucket.app_password.env", "BB_APP_PW"),
		config.Remove("bitbucket.username"),
		config.Set("bitbucket.username.env", "BB_USER"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	err := RunBitbucketInit(t.Context(), p, cfg, mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	}))
	require.NoError(t, err)
}

func TestRunBitbucketInit_FormError(t *testing.T) {
	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)

	err := RunBitbucketInit(t.Context(), p, cfg, WithDualForm(func(_ *DualConfig) []*huh.Form {
		return []*huh.Form{failingForm()}
	}))
	require.Error(t, err)
}

func TestRunInitCmd_LoadedConfig(t *testing.T) {
	t.Setenv("CI", "")

	fs := afero.NewMemMapFs()
	dir := t.TempDir()

	target := filepath.Join(dir, setup.DefaultConfigFilename)
	require.NoError(t, afero.WriteFile(fs, target, []byte("foo: bar\n"), 0o600))

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
	}

	err := RunBitbucketInitCmd(t.Context(), p, dir, mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	}))
	require.NoError(t, err)

	// The config was written to disk with the captured env-var names.
	written, rerr := afero.ReadFile(fs, target)
	require.NoError(t, rerr)
	assert.Contains(t, string(written), "BB_USER")

	info, serr := fs.Stat(target)
	require.NoError(t, serr)
	assert.Equal(t, "-rw-------", info.Mode().String())
}

func TestRunInitCmd_FormError(t *testing.T) {
	t.Setenv("CI", "")

	fs := afero.NewMemMapFs()
	dir := t.TempDir()

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
	}

	err := RunBitbucketInitCmd(t.Context(), p, dir, WithDualForm(func(_ *DualConfig) []*huh.Form {
		return []*huh.Form{failingForm()}
	}))
	require.Error(t, err)
}

func TestRunInitCmd_MkdirError(t *testing.T) {
	t.Setenv("CI", "")

	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	dir := filepath.Join(t.TempDir(), "newdir")

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
	}

	err := RunBitbucketInitCmd(t.Context(), p, dir, mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to create directory")
}

// TestCredentialsForm_UsernameValidator runs the username input from
// dualCredentialsForm in accessible mode with scripted input: an empty line
// trips the "username is required" validator (re-prompting), then a valid value
// satisfies it.
func TestCredentialsForm_UsernameValidator(t *testing.T) {
	t.Parallel()

	cfg := &DualConfig{}
	form := dualCredentialsForm(bitbucketProfile, cfg)
	require.NotNil(t, form)

	// Feed: blank (invalid) → "alice" (valid) for username, then EOF.
	in := strings.NewReader("\nalice\n")
	_ = form.WithAccessible(true).WithInput(in).WithOutput(io.Discard).Run()

	assert.Equal(t, "alice", cfg.Username)
}

func TestNewCmdInitBitbucket_RunE_Error(t *testing.T) {
	t.Setenv("CI", "")

	fs := afero.NewMemMapFs()
	dir := t.TempDir()

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cmd := NewCmdInitBitbucket(p)
	require.NoError(t, cmd.Flags().Set("dir", dir))

	cmd.SetContext(t.Context())

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure Bitbucket")
}
