package github

import (
	"context"
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

// authFormOverride composes the slice-returning creator and the
// display-once creator into a single [AuthFormOption]. Keeps the
// test call sites tight.
func authFormOverride(
	cfgMutate func(*GitHubAuthConfig),
	displayOnce func(string, string) *huh.Form,
) AuthFormOption {
	return WithAuthForm(
		func(cfg *GitHubAuthConfig) []*huh.Form {
			cfgMutate(cfg)
			return nil
		},
		displayOnce,
	)
}

// newAuthProps constructs the Props fixture used by the auth tests
// and also neutralises env vars that could otherwise short-circuit
// the wizard (already-configured detection) or flip CI branches.
func newAuthProps(t *testing.T) (*props.Props, config.Containable) {
	t.Helper()

	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("CI", "")

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	return p, config.NewContainerFromViper(nil, viper.New())
}

// TestGitHubAuth_LiteralModeWritesAuthValue pins the legacy literal
// behaviour: OAuth succeeds, the token lands under github.auth.value,
// nothing else is touched.
func TestGitHubAuth_LiteralModeWritesAuthValue(t *testing.T) {
	p, cfg := newAuthProps(t)

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "ghp_lit_token", nil }),
		WithGitHubAuthForms(authFormOverride(
			func(c *GitHubAuthConfig) { c.StorageMode = credentials.ModeLiteral },
			nil,
		)),
	)

	require.NoError(t, init.Configure(p, cfg))
	assert.Equal(t, "ghp_lit_token", cfg.GetString("github.auth.value"))
	assert.Empty(t, cfg.GetString("github.auth.env"))
	assert.Empty(t, cfg.GetString("github.auth.keychain"))
}

// TestGitHubAuth_EnvVarModeWritesReferenceOnly exercises the
// "FetchToken=false" branch — the user already has a token in their
// shell profile and just wants to record the env-var reference.
func TestGitHubAuth_EnvVarModeWritesReferenceOnly(t *testing.T) {
	p, cfg := newAuthProps(t)

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) {
			t.Fatal("OAuth must not run when FetchToken=false")

			return "", nil
		}),
		WithGitHubAuthForms(authFormOverride(
			func(c *GitHubAuthConfig) {
				c.StorageMode = credentials.ModeEnvVar
				c.EnvVarName = "MYTOOL_GH_TOKEN"
				c.FetchToken = false
			},
			nil,
		)),
	)

	require.NoError(t, init.Configure(p, cfg))
	assert.Equal(t, "MYTOOL_GH_TOKEN", cfg.GetString("github.auth.env"))
	assert.Empty(t, cfg.GetString("github.auth.value"))
	assert.Empty(t, cfg.GetString("github.auth.keychain"))
}

// TestGitHubAuth_EnvVarModeDisplayOnce — FetchToken=true path: OAuth
// runs, display-once form receives the captured token and the env-var
// name, the config stores only the env-var reference.
func TestGitHubAuth_EnvVarModeDisplayOnce(t *testing.T) {
	p, cfg := newAuthProps(t)

	var (
		displayCalled   bool
		tokenShown      string
		envVarNameShown string
	)

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "ghp_envvar_token", nil }),
		WithGitHubAuthForms(authFormOverride(
			func(c *GitHubAuthConfig) {
				c.StorageMode = credentials.ModeEnvVar
				c.EnvVarName = "GITHUB_TOKEN"
				c.FetchToken = true
			},
			func(envVarName, token string) *huh.Form {
				displayCalled = true
				tokenShown = token
				envVarNameShown = envVarName

				// Returning nil causes the wizard to skip the render
				// step, which is what we want under test — we've
				// already captured the token and env-var name by the
				// time the creator is called.
				return nil
			},
		)),
	)

	require.NoError(t, init.Configure(p, cfg))
	assert.True(t, displayCalled, "display-once form should be invoked")
	assert.Equal(t, "ghp_envvar_token", tokenShown)
	assert.Equal(t, "GITHUB_TOKEN", envVarNameShown)

	assert.Equal(t, "GITHUB_TOKEN", cfg.GetString("github.auth.env"))
	assert.Empty(t, cfg.GetString("github.auth.value"), "token must not be written to config")
	assert.Empty(t, cfg.GetString("github.auth.keychain"))
}

// TestGitHubAuth_KeychainModeStoresTokenAndRef — OAuth returns a
// token, the wizard stashes it via the credentials.Backend, and the
// config records a "<tool>/github.auth" reference with no plaintext.
func TestGitHubAuth_KeychainModeStoresTokenAndRef(t *testing.T) {
	credtest.Install(t)

	p, cfg := newAuthProps(t)

	init := NewGitHubInitialiser(p, false, true,
		WithGHLogin(func(_ string) (string, error) { return "ghp_kc_token", nil }),
		WithGitHubAuthForms(authFormOverride(
			func(c *GitHubAuthConfig) { c.StorageMode = credentials.ModeKeychain },
			nil,
		)),
	)

	require.NoError(t, init.Configure(p, cfg))
	assert.Equal(t, "testtool/github.auth", cfg.GetString("github.auth.keychain"))
	assert.Empty(t, cfg.GetString("github.auth.value"), "token must not leak into config")
	assert.Empty(t, cfg.GetString("github.auth.env"))

	got, err := credentials.Retrieve(t.Context(), "testtool", "github.auth")
	require.NoError(t, err)
	assert.Equal(t, "ghp_kc_token", got)
}

// TestWriteGitHubCredential_KeychainWithoutToken — defensive: the
// routing layer refuses keychain mode when the capture step produced
// no token. The wizard should never reach this state, but the guard
// prevents an empty keychain entry if a future refactor regresses.
func TestWriteGitHubCredential_KeychainWithoutToken(t *testing.T) {
	credtest.Install(t)

	cfg := config.NewContainerFromViper(nil, viper.New())
	authCfg := &GitHubAuthConfig{StorageMode: credentials.ModeKeychain}

	err := writeGitHubCredential(t.Context(), cfg, "testtool", authCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GitHub token captured")
}

// TestGitHubStorageModeOptions_CIHidesLiteral — under CI, the
// selector must never offer literal mode, even if the keychain is
// reachable.
func TestGitHubStorageModeOptions_CIHidesLiteral(t *testing.T) {
	opts := githubStorageModeOptions(true, true)

	require.Len(t, opts, 2)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeKeychain, opts[1].Value)
}

// TestGitHubStorageModeOptions_ProbeHidesKeychain — when the probe
// returns false, the keychain option is suppressed regardless of CI.
func TestGitHubStorageModeOptions_ProbeHidesKeychain(t *testing.T) {
	opts := githubStorageModeOptions(false, false)

	require.Len(t, opts, 2)
	assert.Equal(t, credentials.ModeEnvVar, opts[0].Value)
	assert.Equal(t, credentials.ModeLiteral, opts[1].Value)
}

// TestValidateEnvVarName pins the accepted / rejected shapes.
func TestValidateEnvVarName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty", "", true},
		{"lowercase", "github_token", true},
		{"valid", "GITHUB_TOKEN", false},
		{"valid with digits", "TOKEN_2", false},
		{"starts with digit", "2TOKEN", true},
		{"too long", "A" + string(make([]byte, 65)), true},
	}

	for _, tc := range tests {
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

// Compile-time guard: AuthFormOption is exposed and composable with
// WithAuthForm. Protects the public test-extension surface from
// accidental unexport.
var _ AuthFormOption = WithAuthForm(nil, nil)

// Compile-time guard: displayOnceForm's signature matches what
// configureAuth expects.
var _ func(ctx context.Context, cfg config.Containable, toolName string, authCfg *GitHubAuthConfig) error = writeGitHubCredential
