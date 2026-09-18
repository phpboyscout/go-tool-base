package forge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"

	"gitlab.com/phpboyscout/go/features"

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

// --- Name ---

func TestInitialiser_Name(t *testing.T) {
	t.Parallel()

	i := &Initialiser{profile: bitbucketProfile}
	assert.Equal(t, "Bitbucket authentication", i.Name())
}

// sshAlreadyRecorded is a config view in which the SSH stage has nothing left to
// do. Bitbucket reaches that stage now (0186 D1), so a test asserting only the
// credential writes seeds a recorded key to stay on its own subject — the stage
// itself has dedicated coverage in ssh_dual_test.go.
func sshAlreadyRecorded(t *testing.T) *config.View {
	t.Helper()

	return testutil.ViewFromYAML(t, "bitbucket:\n  ssh:\n    key:\n      path: /home/u/.ssh/id_x\n")
}

// --- Configure flows ---

// TestConfigure_EnvVarMode pins the two-env-var write path: one transactional
// Apply sets the env refs and removes every key the other storage modes own,
// with each on-path literal cleared before the env ref that nests inside it.
func TestConfigure_EnvVarMode(t *testing.T) {
	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(sshAlreadyRecorded(t))
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password"),
		config.Set("bitbucket.app_password.env", "BB_APP_PW"),
		config.Remove("bitbucket.username"),
		config.Set("bitbucket.username.env", "BB_USER"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	p.IO = dualEnvIO(t, "BB_USER", "BB_APP_PW")
	i := NewBitbucketInitialiser(p)

	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

// TestConfigure_KeychainMode — the captured username + app_password get
// serialised to a JSON blob and stored under one keychain entry; the config
// records only the reference.
func TestConfigure_KeychainMode(t *testing.T) {
	credtest.Install(t)

	p := newDualTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().View().Return(sshAlreadyRecorded(t))
	cfg.EXPECT().Apply([]config.Change{
		config.Set("bitbucket.keychain", "testtool/bitbucket.auth"),
		config.Remove("bitbucket.username.env"),
		config.Remove("bitbucket.app_password.env"),
		config.Remove("bitbucket.username"),
		config.Remove("bitbucket.app_password"),
	}).Return(nil)

	p.IO = dualCredentialIO(t, credentials.ModeKeychain, "alice", "s3cret")
	i := NewBitbucketInitialiser(p)

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
	cfg.EXPECT().View().Return(sshAlreadyRecorded(t))
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password.env"),
		config.Set("bitbucket.app_password", "s3cret"),
		config.Remove("bitbucket.username.env"),
		config.Set("bitbucket.username", "alice"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	p.IO = dualCredentialIO(t, credentials.ModeLiteral, "alice", "s3cret")
	i := NewBitbucketInitialiser(p)

	require.NoError(t, i.Configure(t.Context(), p, cfg))
}

// TestFinaliseDualConfig_CIRefusesLiteral — belt-and-braces guard: the
// selector hides literal under CI, and the step after the form refuses it
// too, before anything is written.
func TestFinaliseDualConfig_CIRefusesLiteral(t *testing.T) {
	t.Setenv("CI", "true")

	err := finaliseDualConfig(bitbucketProfile, &DualConfig{
		StorageMode: credentials.ModeLiteral,
		Username:    "alice",
		AppPassword: "s3cret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal credential storage is refused under CI")
}

func TestFinaliseDualConfig(t *testing.T) {
	t.Setenv("CI", "")

	t.Run("blank env names take the fallbacks", func(t *testing.T) {
		cfg := &DualConfig{StorageMode: credentials.ModeEnvVar}
		require.NoError(t, finaliseDualConfig(bitbucketProfile, cfg))
		assert.Equal(t, "BITBUCKET_USERNAME", cfg.UsernameEnvName)
		assert.Equal(t, "BITBUCKET_APP_PASSWORD", cfg.AppPasswordEnvName)
	})

	t.Run("given env names stand", func(t *testing.T) {
		cfg := &DualConfig{StorageMode: credentials.ModeEnvVar, UsernameEnvName: "U", AppPasswordEnvName: "P"}
		require.NoError(t, finaliseDualConfig(bitbucketProfile, cfg))
		assert.Equal(t, "U", cfg.UsernameEnvName)
		assert.Equal(t, "P", cfg.AppPasswordEnvName)
	})

	t.Run("a credential mode needs both values", func(t *testing.T) {
		for _, mode := range []credentials.Mode{credentials.ModeKeychain, credentials.ModeLiteral} {
			require.ErrorIs(t, finaliseDualConfig(bitbucketProfile, &DualConfig{StorageMode: mode, Username: "alice"}), ErrCredentialsIncomplete)
			require.ErrorIs(t, finaliseDualConfig(bitbucketProfile, &DualConfig{StorageMode: mode, AppPassword: "pw"}), ErrCredentialsIncomplete)
			require.NoError(t, finaliseDualConfig(bitbucketProfile, &DualConfig{StorageMode: mode, Username: "alice", AppPassword: "pw"}))
		}
	})
}

// A password cannot be read at an accessible prompt without a terminal; huh
// leaves it blank and says nothing, so the wizard has to refuse the blank.
func TestConfigure_AccessibleRunWithoutATerminalRefusesTheBlankPassword(t *testing.T) {
	p := newDualTestProps(t)
	cfg := setupmocks.NewMockEditor(t)

	p.IO, _ = answersIO(modeNumber(t, credentials.ModeLiteral), "", "", "alice")
	i := NewBitbucketInitialiser(p)

	require.ErrorIs(t, i.Configure(t.Context(), p, cfg), ErrCredentialsIncomplete)
}

// --- IsConfigured ---

// TestIsConfigured covers 0186 D5. Bitbucket now offers SSH, so it is only
// "configured" once both the credential and the key are recorded. Credentials
// alone used to suffice — correctly, because the dual flow could never reach the
// SSH stage, so there was never a key to have. Now that it can, treating
// credentials alone as done would mean never offering the key on a re-run.
func TestIsConfigured(t *testing.T) {
	const sshRecorded = "  ssh:\n    key:\n      path: /home/u/.ssh/id_x\n"

	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		skipKey bool
		wantYes bool
	}{
		{name: "empty", yaml: "", wantYes: false},
		{name: "credential alone is not enough", yaml: "bitbucket:\n  username: alice\n", wantYes: false},
		{name: "ssh alone is not enough", yaml: "bitbucket:\n" + sshRecorded, wantYes: false},

		// An env reference counts once it resolves (deferred item 6 of the
		// v0.43.0 round): the bundle ships both pointers as defaults, so
		// their presence alone said nothing.
		{name: "env-var username + ssh, variable set", yaml: "bitbucket:\n  username:\n    env: BB_USER\n" + sshRecorded, env: map[string]string{"BB_USER": "alice"}, wantYes: true},
		{name: "env-var username + ssh, variable unset", yaml: "bitbucket:\n  username:\n    env: BB_USER_UNSET\n" + sshRecorded, wantYes: false},
		{name: "env-var app_password + ssh, variable set", yaml: "bitbucket:\n  app_password:\n    env: BB_APP_PW\n" + sshRecorded, env: map[string]string{"BB_APP_PW": "pw"}, wantYes: true},
		{name: "keychain ref + ssh", yaml: "bitbucket:\n  keychain: tool/bitbucket.auth\n" + sshRecorded, wantYes: true},
		{name: "literal username + ssh", yaml: "bitbucket:\n  username: alice\n" + sshRecorded, wantYes: true},
		{name: "literal app_password + ssh", yaml: "bitbucket:\n  app_password: s3cret\n" + sshRecorded, wantYes: true},

		{
			name:    "an agent key satisfies the ssh half",
			yaml:    "bitbucket:\n  username: alice\n  ssh:\n    key:\n      type: agent\n",
			wantYes: true,
		},
		{
			name:    "--skip-key removes the ssh requirement",
			yaml:    "bitbucket:\n  username: alice\n",
			skipKey: true,
			wantYes: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Not parallel: the env-reference cases set variables.
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			view := testutil.ViewFromYAML(t, tc.yaml)
			i := &Initialiser{profile: bitbucketProfile, SkipKey: tc.skipKey}

			assert.Equal(t, tc.wantYes, i.IsConfigured(view))
		})
	}
}

// --- dualForm: one form, its pages follow the storage mode ---

func TestDualForm_EnvVarModeAsksTheTwoNames(t *testing.T) {
	t.Setenv("CI", "")

	io, out := answersIO(modeNumber(t, credentials.ModeEnvVar), "U_VAR", "P_VAR", "ignored")
	p := newTestProps(t)
	p.IO = io

	cfg := &DualConfig{}
	require.NoError(t, setup.RunForm(t.Context(), p, dualForm(t.Context(), p, bitbucketProfile, cfg)))
	assert.Equal(t, credentials.ModeEnvVar, cfg.StorageMode)
	assert.Equal(t, "U_VAR", cfg.UsernameEnvName)
	assert.Equal(t, "P_VAR", cfg.AppPasswordEnvName)
	assert.Contains(t, out.String(), "Username env var name")
	assert.Contains(t, out.String(), "App password env var name")
}

func TestDualForm_RejectsAnInvalidEnvVarName(t *testing.T) {
	t.Setenv("CI", "")

	// The invalid name is re-asked; blank is accepted and means the fallback.
	io, out := answersIO(modeNumber(t, credentials.ModeEnvVar), "not valid", "", "P_VAR", "ignored")
	p := newTestProps(t)
	p.IO = io

	cfg := &DualConfig{}
	require.NoError(t, setup.RunForm(t.Context(), p, dualForm(t.Context(), p, bitbucketProfile, cfg)))
	assert.Empty(t, cfg.UsernameEnvName)
	assert.Equal(t, "P_VAR", cfg.AppPasswordEnvName)
	assert.Contains(t, out.String(), "env var name must match")
}

// The credential page needs the TUI (its password field), and only the TUI
// honours the hide functions: literal mode skips the env-var page.
func TestDualForm_LiteralModeTakesTheCredentials(t *testing.T) {
	t.Setenv("CI", "")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	p := newTestProps(t)
	p.IO = dualCredentialIO(t, credentials.ModeLiteral, "alice", "s3cret")

	cfg := &DualConfig{}
	require.NoError(t, setup.RunForm(ctx, p, dualForm(ctx, p, bitbucketProfile, cfg)))
	assert.Equal(t, credentials.ModeLiteral, cfg.StorageMode)
	assert.Equal(t, "alice", cfg.Username)
	assert.Equal(t, "s3cret", cfg.AppPassword)
	assert.Empty(t, cfg.UsernameEnvName)
	assert.Empty(t, cfg.AppPasswordEnvName)
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

// --- the form refuses to run with nobody there ---

func TestConfigureDual_RefusesANonInteractiveRun(t *testing.T) {
	p := newDualTestProps(t)
	p.IO = nonInteractiveIO()

	cfg := setupmocks.NewMockEditor(t)
	i := NewBitbucketInitialiser(p)

	require.ErrorIs(t, i.Configure(t.Context(), p, cfg), setup.ErrNonInteractive)
}

// --- registration ---

func TestInitRegistry(t *testing.T) {

	feature := props.FeatureID("bitbucket")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	snapshot := features.Default().Snapshot()

	// FeatureFlag binds --skip-bitbucket on the init command; the provider
	// reads it back from those flags, never from package state (#37).
	fps := setup.FeatureFlagsIn(snapshot)[feature]
	require.NotEmpty(t, fps)

	target := &cobra.Command{Use: "init"}
	fps[0](target)
	require.NotNil(t, target.Flags().Lookup("skip-bitbucket"))

	// The flag defaults on under CI=true; pin the unskipped case explicitly.
	require.NoError(t, target.Flags().Set("skip-bitbucket", "false"))

	// InitialiserProvider: non-skip branch returns a live initialiser.
	ips := setup.InitialisersIn(snapshot)[feature]
	require.NotEmpty(t, ips)

	init0 := ips[0](p, target.Flags())
	require.NotNil(t, init0)
	assert.Equal(t, "Bitbucket authentication", init0.Name())

	// InitialiserProvider: skip branch returns nil.
	require.NoError(t, target.Flags().Set("skip-bitbucket", "true"))
	assert.Nil(t, ips[0](p, target.Flags()))

	// SubcommandProvider yields the init bitbucket command.
	sps := setup.SubcommandsIn(snapshot)[feature]
	require.NotEmpty(t, sps)

	cmds := sps[0](p)
	require.Len(t, cmds, 1)
	assert.Equal(t, "bitbucket", cmds[0].Use)
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
	cfg.EXPECT().View().Return(sshAlreadyRecorded(t))
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password"),
		config.Set("bitbucket.app_password.env", "BB_APP_PW"),
		config.Remove("bitbucket.username"),
		config.Set("bitbucket.username.env", "BB_USER"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	p.IO = dualEnvIO(t, "BB_USER", "BB_APP_PW")
	require.NoError(t, RunBitbucketInit(t.Context(), p, cfg))
}

func TestRunBitbucketInit_FormError(t *testing.T) {
	p := newDualTestProps(t)
	p.IO = nonInteractiveIO()

	cfg := setupmocks.NewMockEditor(t)

	require.ErrorIs(t, RunBitbucketInit(t.Context(), p, cfg), setup.ErrNonInteractive)
}

func TestRunInitCmd_LoadedConfig(t *testing.T) {
	t.Setenv("CI", "")

	fs := afero.NewMemMapFs()
	dir := t.TempDir()

	target := filepath.Join(dir, setup.DefaultConfigFilename)
	// A recorded SSH key so the stage Bitbucket now reaches (0186 D1) has
	// nothing to do — this test is about the credential reaching disk.
	require.NoError(t, afero.WriteFile(fs, target,
		[]byte("foo: bar\nbitbucket:\n  ssh:\n    key:\n      path: /home/u/.ssh/id_x\n"), 0o600))

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
		IO:     dualEnvIO(t, "BB_USER", "BB_APP_PW"),
	}

	require.NoError(t, RunBitbucketInitCmd(t.Context(), p, dir))

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
		IO:     nonInteractiveIO(),
	}

	require.ErrorIs(t, RunBitbucketInitCmd(t.Context(), p, dir), setup.ErrNonInteractive)
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
		IO:     dualEnvIO(t, "BB_USER", "BB_APP_PW"),
	}

	err := RunBitbucketInitCmd(t.Context(), p, dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to create directory")
}

// TestDualForm_UsernameIsRequired: a blank username is re-asked, then the
// valid value is kept.
func TestDualForm_UsernameIsRequired(t *testing.T) {
	t.Setenv("CI", "")

	io, out := answersIO(modeNumber(t, credentials.ModeLiteral), "", "", "", "alice")
	p := newTestProps(t)
	p.IO = io

	cfg := &DualConfig{}
	require.NoError(t, setup.RunForm(t.Context(), p, dualForm(t.Context(), p, bitbucketProfile, cfg)))
	assert.Equal(t, "alice", cfg.Username)
	assert.Contains(t, out.String(), "username is required")
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

	p.IO = nonInteractiveIO()

	cmd := NewCmdInitBitbucket(p)
	require.NoError(t, cmd.Flags().Set("dir", dir))

	cmd.SetContext(t.Context())

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure Bitbucket")
}
