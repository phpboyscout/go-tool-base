package bitbucket

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"

	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestInitialiser_Name pins the human-readable label.
func TestInitialiser_Name(t *testing.T) {
	t.Parallel()

	i := &Initialiser{}
	assert.Equal(t, "Bitbucket authentication", i.Name())
}

// TestDefaultStorageModeForm constructs the storage-mode selector and
// asserts the default mode is seeded to env-var when unset, and that a
// pre-set mode is preserved.
func TestDefaultStorageModeForm(t *testing.T) {
	// CI runners set CI=true, which drops the literal option (refused under CI)
	// from the select; huh then resets a bound literal value off the missing
	// option. Force non-CI so the existing-mode preservation is deterministic.
	t.Setenv("CI", "")

	cfg := &BitbucketConfig{}
	form := defaultStorageModeForm(cfg)
	require.NotNil(t, form)
	assert.Equal(t, credentials.ModeEnvVar, cfg.StorageMode)

	cfg2 := &BitbucketConfig{StorageMode: credentials.ModeLiteral}
	form2 := defaultStorageModeForm(cfg2)
	require.NotNil(t, form2)
	assert.Equal(t, credentials.ModeLiteral, cfg2.StorageMode)
}

// TestStorageModeDescription covers both CI and non-CI branches.
func TestStorageModeDescription(t *testing.T) {
	t.Parallel()

	assert.Contains(t, storageModeDescription(true), "CI environment detected")
	assert.Contains(t, storageModeDescription(false), "Env-var references")
}

// TestDefaultEnvVarNamesForm seeds the upstream-standard names when the
// fields are empty and leaves pre-set names untouched.
func TestDefaultEnvVarNamesForm(t *testing.T) {
	t.Parallel()

	cfg := &BitbucketConfig{}
	form := defaultEnvVarNamesForm(cfg)
	require.NotNil(t, form)
	assert.Equal(t, "BITBUCKET_USERNAME", cfg.UsernameEnvName)
	assert.Equal(t, "BITBUCKET_APP_PASSWORD", cfg.AppPasswordEnvName)

	cfg2 := &BitbucketConfig{UsernameEnvName: "U", AppPasswordEnvName: "P"}
	form2 := defaultEnvVarNamesForm(cfg2)
	require.NotNil(t, form2)
	assert.Equal(t, "U", cfg2.UsernameEnvName)
	assert.Equal(t, "P", cfg2.AppPasswordEnvName)
}

// TestDefaultCredentialsForm constructs the username + app-password
// inputs form.
func TestDefaultCredentialsForm(t *testing.T) {
	t.Parallel()

	cfg := &BitbucketConfig{}
	form := defaultCredentialsForm(cfg)
	require.NotNil(t, form)
}

// TestFormAtIndex exercises the in-range and out-of-range slot lookups.
func TestFormAtIndex(t *testing.T) {
	t.Parallel()

	creator := func(_ *BitbucketConfig) []*huh.Form {
		return []*huh.Form{huh.NewForm()}
	}

	got := formAtIndex(creator, 0)(&BitbucketConfig{})
	assert.NotNil(t, got)

	missing := formAtIndex(creator, 5)(&BitbucketConfig{})
	assert.Nil(t, missing)
}

// TestRunFormStage covers the nil-creator and nil-form short-circuits.
func TestRunFormStage(t *testing.T) {
	t.Parallel()

	require.NoError(t, runFormStage(nil, &BitbucketConfig{}))

	nilForm := func(_ *BitbucketConfig) *huh.Form { return nil }
	require.NoError(t, runFormStage(nilForm, &BitbucketConfig{}))
}

// TestWriteBitbucketCredentials_DefaultMode hits the unsupported-mode
// error branch in the write switch. The mock expects no Set calls.
func TestWriteBitbucketCredentials_DefaultMode(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	err := writeBitbucketCredentials(t.Context(), cfg, "tool", &BitbucketConfig{
		StorageMode: credentials.Mode("bogus"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported Bitbucket credential storage mode")
}

// TestWriteBitbucketCredentials_EmptyMode treats an empty mode as
// literal and writes both fields.
func TestWriteBitbucketCredentials_EmptyMode(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Set("bitbucket.username", "alice").Return(nil)
	cfg.EXPECT().Set("bitbucket.app_password", "pw").Return(nil)

	err := writeBitbucketCredentials(t.Context(), cfg, "tool", &BitbucketConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.NoError(t, err)
}

// TestWriteBitbucketCredentials_EnvVarPartial exercises the env-var
// branch when only one of the two names is supplied — the absent
// app-password name must not be written (unexpected-call guard).
func TestWriteBitbucketCredentials_EnvVarPartial(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Set("bitbucket.username.env", "ONLY_USER").Return(nil)

	err := writeBitbucketCredentials(t.Context(), cfg, "tool", &BitbucketConfig{
		StorageMode:     credentials.ModeEnvVar,
		UsernameEnvName: "ONLY_USER",
	})
	require.NoError(t, err)
}

// TestWriteKeychainBlob_NoToolName guards the empty-tool-name branch.
// No Set call is expected.
func TestWriteKeychainBlob_NoToolName(t *testing.T) {
	t.Parallel()

	cfg := setupmocks.NewMockEditor(t)
	err := writeKeychainBlob(t.Context(), cfg, "", &BitbucketConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a tool name")
}

// TestWriteKeychainBlob_Success stores the JSON blob and records the
// reference.
func TestWriteKeychainBlob_Success(t *testing.T) {
	credtest.Install(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Set("bitbucket.keychain", "tool/bitbucket.auth").Return(nil)

	err := writeKeychainBlob(t.Context(), cfg, "tool", &BitbucketConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.NoError(t, err)
}

// TestWriteKeychainBlob_StoreError covers the credentials.Store error
// branch (with the WithHint wrap). A cancelled context makes the
// MemoryBackend's Store fail. No Set call is expected — the keychain
// reference must not be recorded when the store fails.
func TestWriteKeychainBlob_StoreError(t *testing.T) {
	credtest.Install(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := setupmocks.NewMockEditor(t)
	err := writeKeychainBlob(ctx, cfg, "tool", &BitbucketConfig{
		Username:    "alice",
		AppPassword: "pw",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storing Bitbucket credentials in OS keychain")
}

// failingForm returns a form with a single input group that errors on
// Run in a non-TTY env (no /dev/tty). Used to exercise the error-return
// branches of runForms for the env-var and credentials stages.
func failingForm() *huh.Form {
	var s string

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("x").Value(&s),
		),
	)
}

// TestRunFormStage_Success runs a zero-group form, which huh executes
// without a TTY (Run returns nil when the form has no groups). This
// covers the post-Run success return in runFormStage.
func TestRunFormStage_Success(t *testing.T) {
	t.Parallel()

	creator := func(_ *BitbucketConfig) *huh.Form { return huh.NewForm() }
	require.NoError(t, runFormStage(creator, &BitbucketConfig{}))
}

// TestRunForms_EnvVarStageError hits the error-return branch of the
// env-var-names stage inside runForms.
func TestRunForms_EnvVarStageError(t *testing.T) {
	t.Setenv("CI", "")

	opt := WithForm(func(cfg *BitbucketConfig) []*huh.Form {
		cfg.StorageMode = credentials.ModeEnvVar
		// slot 0 (storage mode) nil → skipped; slot 1 (env-var) fails.
		return []*huh.Form{nil, failingForm()}
	})

	_, err := runForms(opt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bitbucket auth form cancelled")
}

// TestRunForms_CredentialsStageError hits the error-return branch of the
// credentials stage inside runForms.
func TestRunForms_CredentialsStageError(t *testing.T) {
	t.Setenv("CI", "")

	opt := WithForm(func(cfg *BitbucketConfig) []*huh.Form {
		cfg.StorageMode = credentials.ModeLiteral
		// slot 0 nil → skipped; slot 2 (credentials) fails.
		return []*huh.Form{nil, nil, failingForm()}
	})

	_, err := runForms(opt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bitbucket auth form cancelled")
}

// TestRunForms_StorageStageError hits the error-return branch of the
// initial storage-mode stage inside runForms.
func TestRunForms_StorageStageError(t *testing.T) {
	t.Setenv("CI", "")

	opt := WithForm(func(_ *BitbucketConfig) []*huh.Form {
		return []*huh.Form{failingForm()}
	})

	_, err := runForms(opt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bitbucket auth form cancelled")
}

// TestInitRegistry drives the closures registered in init() by reading
// them back out of the setup registry and invoking them. This covers
// the InitialiserProvider (both skip and non-skip branches), the
// SubcommandProvider, and the FeatureFlag closure.
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

// TestNewInitialiser_Options confirms options are applied.
func TestNewInitialiser_Options(t *testing.T) {
	t.Parallel()

	i := NewInitialiser(nil, WithFormOptions(mockForms(func(_ *BitbucketConfig) {})))
	assert.Len(t, i.formOpts, 1)
}

// TestNewCmdInitBitbucket builds the cobra command and inspects its
// metadata and flags.
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

// TestRunBitbucketInit_Success drives RunBitbucketInit with an injected
// env-var-mode form creator and asserts the credentials are persisted
// through the editor.
func TestRunBitbucketInit_Success(t *testing.T) {
	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Set("bitbucket.username.env", "BB_USER").Return(nil)
	cfg.EXPECT().Set("bitbucket.app_password.env", "BB_APP_PW").Return(nil)

	err := RunBitbucketInit(p, cfg, mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	}))
	require.NoError(t, err)
}

// TestRunBitbucketInit_FormError drives RunBitbucketInit with a failing
// injected form; the wizard error propagates before any write.
func TestRunBitbucketInit_FormError(t *testing.T) {
	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)

	err := RunBitbucketInit(p, cfg, WithForm(func(_ *BitbucketConfig) []*huh.Form {
		return []*huh.Form{failingForm()}
	}))
	require.Error(t, err)
}

// TestRunInitCmd_LoadedConfig covers the full success write-back path
// when an existing config file is present: the editor opens over the
// file and the wizard's writes land in it immediately.
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

	err := RunInitCmd(t.Context(), p, dir, mockForms(func(c *BitbucketConfig) {
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

// TestRunInitCmd_FormError covers the wizard-error propagation out of
// RunInitCmd (after the editor opens, before any write).
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

	err := RunInitCmd(t.Context(), p, dir, WithForm(func(_ *BitbucketConfig) []*huh.Form {
		return []*huh.Form{failingForm()}
	}))
	require.Error(t, err)
}

// TestRunInitCmd_MkdirError covers the MkdirAll error branch: opening
// the config editor creates the config directory before the wizard
// runs, and a read-only FS makes that fail.
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

	err := RunInitCmd(t.Context(), p, dir, mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to create directory")
}

// TestCredentialsForm_UsernameValidator runs the username input from
// defaultCredentialsForm in accessible mode with scripted input: an
// empty line trips the "username is required" validator (re-prompting),
// then a valid value satisfies it. This exercises both branches of the
// username validation closure without a TTY.
func TestCredentialsForm_UsernameValidator(t *testing.T) {
	t.Parallel()

	cfg := &BitbucketConfig{}
	form := defaultCredentialsForm(cfg)
	require.NotNil(t, form)

	// Feed: blank (invalid) → "alice" (valid) for username, then EOF.
	// The password field requires a real TTY and returns an error after
	// the username validator has already executed both branches.
	in := strings.NewReader("\nalice\n")
	_ = form.WithAccessible(true).WithInput(in).WithOutput(io.Discard).Run()

	assert.Equal(t, "alice", cfg.Username)
}

// TestNewCmdInitBitbucket_RunE_Error invokes the command's RunE in a
// non-TTY env; the default wizard fails, and the error is wrapped.
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

	// RunE reads cmd.Context(), which is nil until Execute runs; seed it
	// for a direct invocation.
	cmd.SetContext(t.Context())

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure Bitbucket")
}
