package bitbucket

import (
	"encoding/json"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

// TestConfigure_EnvVarMode pins the two-env-var write path. The mock
// editor's expectations double as the exclusivity assertion: any write
// to a literal or keychain key would be an unexpected call.
func TestConfigure_EnvVarMode(t *testing.T) {
	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Set("bitbucket.username.env", "BB_USER").Return(nil)
	cfg.EXPECT().Set("bitbucket.app_password.env", "BB_APP_PW").Return(nil)

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
	cfg.EXPECT().Set("bitbucket.keychain", "testtool/bitbucket.auth").Return(nil)

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

// TestConfigure_LiteralMode — both fields land in config as plaintext;
// no env-var or keychain keys are written (unexpected-call guard).
func TestConfigure_LiteralMode(t *testing.T) {
	p := newTestProps(t)

	cfg := setupmocks.NewMockEditor(t)
	cfg.EXPECT().Set("bitbucket.username", "alice").Return(nil)
	cfg.EXPECT().Set("bitbucket.app_password", "s3cret").Return(nil)

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
