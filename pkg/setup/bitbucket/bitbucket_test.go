package bitbucket

import (
	"encoding/json"
	"path/filepath"
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

// newTestProps builds a fixture Props. CI env var is cleared so
// CI-refusal branches don't fire.
func newTestProps(t *testing.T) *props.Props {
	t.Helper()

	t.Setenv("CI", "")

	return &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Assets: props.NewAssets(),
		Tool:   props.Tool{Name: "testtool"},
	}
}

// mockForms builds a FormOption that applies cfgMutate up-front and
// returns nil forms for every stage — letting tests drive the wizard
// without rendering a TTY.
func mockForms(cfgMutate func(*BitbucketConfig)) FormOption {
	return WithForm(func(cfg *BitbucketConfig) []*huh.Form {
		cfgMutate(cfg)
		return nil
	})
}

// TestConfigure_EnvVarMode pins the two-env-var write path: one
// transactional Apply sets the env refs and removes every key the other
// storage modes own, with each on-path literal cleared before the env ref
// that nests inside it is set.
func TestConfigure_EnvVarMode(t *testing.T) {
	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password"),
		config.Set("bitbucket.app_password.env", "BB_APP_PW"),
		config.Remove("bitbucket.username"),
		config.Set("bitbucket.username.env", "BB_USER"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	})))

	require.NoError(t, i.Configure(p, cfg))
}

// TestConfigure_KeychainMode — the captured username + app_password
// get serialised to a JSON blob and stored under one keychain entry;
// the config records only the reference (the mock rejects any literal
// credential write as an unexpected call).
func TestConfigure_KeychainMode(t *testing.T) {
	credtest.Install(t)

	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Set("bitbucket.keychain", "testtool/bitbucket.auth"),
		config.Remove("bitbucket.username.env"),
		config.Remove("bitbucket.app_password.env"),
		config.Remove("bitbucket.username"),
		config.Remove("bitbucket.app_password"),
	}).Return(nil)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeKeychain
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	require.NoError(t, i.Configure(p, cfg))

	raw, err := credentials.Retrieve(t.Context(), "testtool", "bitbucket.auth")
	require.NoError(t, err)

	var blob map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &blob))
	assert.Equal(t, "alice", blob["username"])
	assert.Equal(t, "s3cret", blob["app_password"])
}

// TestConfigure_LiteralMode — both fields land in config as plaintext,
// with the nested env refs removed before their parent scalars are set
// and the keychain ref removed last.
func TestConfigure_LiteralMode(t *testing.T) {
	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Apply([]config.Change{
		config.Remove("bitbucket.app_password.env"),
		config.Set("bitbucket.app_password", "s3cret"),
		config.Remove("bitbucket.username.env"),
		config.Set("bitbucket.username", "alice"),
		config.Remove("bitbucket.keychain"),
	}).Return(nil)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeLiteral
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	require.NoError(t, i.Configure(p, cfg))
}

// TestConfigure_CIRefusesLiteral — belt-and-braces guard: even if the
// form selection bypassed the CI filter, the configure step refuses
// before writing anything (the mock expects no Set calls).
func TestConfigure_CIRefusesLiteral(t *testing.T) {
	p := newTestProps(t)
	// newTestProps cleared CI — re-set after.
	t.Setenv("CI", "true")

	cfg := setupmocks.NewMockEditor(t)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeLiteral
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	err := i.Configure(p, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "literal credential storage is refused under CI")
}

// TestIsConfigured — any of the three modes counts as configured.
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
			i := &Initialiser{}

			assert.Equal(t, tc.wantYes, i.IsConfigured(view))
		})
	}
}

// TestWriteBitbucketCredentials_ModeSwitchClearsStaleKeys drives the real
// store-backed editor through both directions of the literal↔env-ref switch:
// the invariant write must leave only the selected mode's keys in the
// document, with the dual-credential nesting (username vs username.env)
// surviving the in-place edit.
func TestWriteBitbucketCredentials_ModeSwitchClearsStaleKeys(t *testing.T) {
	openEditor := func(t *testing.T, yamlDoc string) (setup.Editor, *props.Props, string) {
		t.Helper()

		p := newTestProps(t)
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

		// Env var NAMES, not credentials — held in variables named to keep
		// gosec G101 from flagging a "hardcoded credential" test fixture.
		userVar, appVar := "BB_USER", "BB_APP_PW"

		require.NoError(t, writeBitbucketCredentials(t.Context(), cfg, "testtool", &BitbucketConfig{
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

		require.NoError(t, writeBitbucketCredentials(t.Context(), cfg, "testtool", &BitbucketConfig{
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

// The storage-mode choice matrix is now provided by
// credentials.ModeChoices (go/credentials module) and exercised in that
// module's tests — a single source of truth shared by every wizard.

// TestWriteKeychainBlob_MissingFields defends against a regression
// that would write a half-populated entry when the form is bypassed
// in tests. The mock expects no Set calls — nothing may be recorded.
func TestWriteKeychainBlob_MissingFields(t *testing.T) {
	credtest.Install(t)

	cfg := setupmocks.NewMockEditor(t)

	err := writeKeychainBlob(t.Context(), cfg, "tool", &BitbucketConfig{
		Username:    "alice",
		AppPassword: "", // missing
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires both username and app password")
}

// Env-var-name validation is now credentials.ValidateEnvVarName,
// covered by pkg/credentials's wizard_test.go.
