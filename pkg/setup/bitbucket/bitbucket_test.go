package bitbucket

import (
	"encoding/json"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	"github.com/phpboyscout/go-tool-base/pkg/credentials/credtest"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// newTestProps builds a fixture Props plus a fresh in-memory config
// container. CI env var is cleared so CI-refusal branches don't fire.
func newTestProps(t *testing.T) (*props.Props, config.Containable) {
	t.Helper()

	t.Setenv("CI", "")

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	return p, config.NewContainerFromViper(nil, viper.New())
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

// TestConfigure_EnvVarMode pins the two-env-var write path.
func TestConfigure_EnvVarMode(t *testing.T) {
	p, cfg := newTestProps(t)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeEnvVar
		c.UsernameEnvName = "BB_USER"
		c.AppPasswordEnvName = "BB_APP_PW"
	})))

	require.NoError(t, i.Configure(p, cfg))
	assert.Equal(t, "BB_USER", cfg.GetString("bitbucket.username.env"))
	assert.Equal(t, "BB_APP_PW", cfg.GetString("bitbucket.app_password.env"))
	assert.Empty(t, cfg.GetString("bitbucket.username"))
	assert.Empty(t, cfg.GetString("bitbucket.app_password"))
	assert.Empty(t, cfg.GetString("bitbucket.keychain"))
}

// TestConfigure_KeychainMode — the captured username + app_password
// get serialised to a JSON blob and stored under one keychain entry;
// the config records only the reference.
func TestConfigure_KeychainMode(t *testing.T) {
	credtest.Install(t)

	p, cfg := newTestProps(t)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeKeychain
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	require.NoError(t, i.Configure(p, cfg))
	assert.Equal(t, "testtool/bitbucket.auth", cfg.GetString("bitbucket.keychain"))
	assert.Empty(t, cfg.GetString("bitbucket.username"))
	assert.Empty(t, cfg.GetString("bitbucket.app_password"))

	raw, err := credentials.Retrieve(t.Context(), "testtool", "bitbucket.auth")
	require.NoError(t, err)

	var blob map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &blob))
	assert.Equal(t, "alice", blob["username"])
	assert.Equal(t, "s3cret", blob["app_password"])
}

// TestConfigure_LiteralMode — both fields land in config as plaintext.
func TestConfigure_LiteralMode(t *testing.T) {
	p, cfg := newTestProps(t)

	i := NewInitialiser(p, WithFormOptions(mockForms(func(c *BitbucketConfig) {
		c.StorageMode = credentials.ModeLiteral
		c.Username = "alice"
		c.AppPassword = "s3cret"
	})))

	require.NoError(t, i.Configure(p, cfg))
	assert.Equal(t, "alice", cfg.GetString("bitbucket.username"))
	assert.Equal(t, "s3cret", cfg.GetString("bitbucket.app_password"))
	assert.Empty(t, cfg.GetString("bitbucket.username.env"))
	assert.Empty(t, cfg.GetString("bitbucket.keychain"))
}

// TestConfigure_CIRefusesLiteral — belt-and-braces guard: even if the
// form selection bypassed the CI filter, the configure step refuses.
func TestConfigure_CIRefusesLiteral(t *testing.T) {
	t.Setenv("CI", "true")

	p, cfg := newTestProps(t)
	// newTestProps cleared CI — re-set after.
	t.Setenv("CI", "true")

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
		keyVal  [2]string
		wantYes bool
	}{
		{"empty", [2]string{"", ""}, false},
		{"env-var username", [2]string{"bitbucket.username.env", "BB_USER"}, true},
		{"env-var app_password", [2]string{"bitbucket.app_password.env", "BB_APP_PW"}, true},
		{"keychain ref", [2]string{"bitbucket.keychain", "tool/bitbucket.auth"}, true},
		{"literal username", [2]string{"bitbucket.username", "alice"}, true},
		{"literal app_password", [2]string{"bitbucket.app_password", "s3cret"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := viper.New()

			if tc.keyVal[0] != "" {
				v.Set(tc.keyVal[0], tc.keyVal[1])
			}

			cfg := config.NewContainerFromViper(nil, v)
			i := &Initialiser{}

			assert.Equal(t, tc.wantYes, i.IsConfigured(cfg))
		})
	}
}

// TestStorageModeOptions_CIHidesLiteral — under CI, the selector
// never offers literal, regardless of keychain reachability.
func TestStorageModeOptions_CIHidesLiteral(t *testing.T) {
	t.Parallel()

	opts := storageModeOptions(true, true)
	require.Len(t, opts, 2)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeKeychain, opts[1].Value)
}

// TestStorageModeOptions_ProbeHidesKeychain — when the probe returns
// false, the keychain option is suppressed regardless of CI.
func TestStorageModeOptions_ProbeHidesKeychain(t *testing.T) {
	t.Parallel()

	opts := storageModeOptions(false, false)
	require.Len(t, opts, 2)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeLiteral, opts[1].Value)
}

// TestWriteKeychainBlob_MissingFields defends against a regression
// that would write a half-populated entry when the form is bypassed
// in tests.
func TestWriteKeychainBlob_MissingFields(t *testing.T) {
	credtest.Install(t)

	cfg := config.NewContainerFromViper(nil, viper.New())

	err := writeKeychainBlob(t.Context(), cfg, "tool", &BitbucketConfig{
		Username:    "alice",
		AppPassword: "", // missing
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires both username and app password")
}

// TestValidateEnvVarName pins the accepted shapes — shared contract
// with pkg/setup/ai and pkg/setup/github.
func TestValidateEnvVarName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", true},
		{"lowercase", "bitbucket_username", true},
		{"valid", "BITBUCKET_USERNAME", false},
		{"starts with digit", "2TOKEN", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateEnvVarName(tc.value)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
