package vcs

import (
	"testing"

	"github.com/stretchr/testify/assert"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
)

// ResolveToken precedence:
//  1. cfg.auth.env → os.Getenv(name)
//  2. cfg.auth.keychain → "<service>/<account>" → credentials.Retrieve
//     (always empty in the default build)
//  3. cfg.auth.value → literal (Viper-backed, so prefixed env surfaces here)
//  4. fallbackEnv → os.Getenv(fallbackEnv)
//
// Every cfg lookup now uses GetString directly (no Has pre-check)
// because Viper's AutomaticEnv surfaces prefixed env vars without
// them being present in the YAML — Has would hide them.

func TestResolveToken_FromConfigEnv(t *testing.T) {
	t.Setenv("MY_CUSTOM_TOKEN", "token-from-env")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("MY_CUSTOM_TOKEN")

	assert.Equal(t, "token-from-env", ResolveToken(mock, ""))
}

func TestResolveToken_FromConfigValue(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("")
	mock.EXPECT().GetString("auth.keychain").Return("")
	mock.EXPECT().GetString("auth.value").Return("literal-token")

	assert.Equal(t, "literal-token", ResolveToken(mock, ""))
}

func TestResolveToken_FromFallbackEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fallback-token")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("")
	mock.EXPECT().GetString("auth.keychain").Return("")
	mock.EXPECT().GetString("auth.value").Return("")

	assert.Equal(t, "fallback-token", ResolveToken(mock, "GITHUB_TOKEN"))
}

func TestResolveToken_PrecedenceConfigEnvOverValue(t *testing.T) {
	t.Setenv("PRIORITY_TOKEN", "env-wins")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("PRIORITY_TOKEN")

	assert.Equal(t, "env-wins", ResolveToken(mock, ""),
		"auth.env should short-circuit before auth.value is consulted")
}

func TestResolveToken_PrecedenceConfigOverFallback(t *testing.T) {
	t.Setenv("FALLBACK_TOKEN", "fallback-loses")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("")
	mock.EXPECT().GetString("auth.keychain").Return("")
	mock.EXPECT().GetString("auth.value").Return("config-wins")

	assert.Equal(t, "config-wins", ResolveToken(mock, "FALLBACK_TOKEN"),
		"config auth.value should take precedence over fallback env")
}

func TestResolveToken_NilConfig(t *testing.T) {
	t.Setenv("FALLBACK_TOKEN", "from-fallback")

	assert.Equal(t, "from-fallback", ResolveToken(nil, "FALLBACK_TOKEN"))
}

func TestResolveToken_NilConfigNoFallback(t *testing.T) {
	t.Parallel()
	assert.Empty(t, ResolveToken(nil, ""))
}

func TestResolveToken_EmptyEnvVarFallsThrough(t *testing.T) {
	t.Setenv("EMPTY_TOKEN", "")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("EMPTY_TOKEN")
	mock.EXPECT().GetString("auth.keychain").Return("")
	mock.EXPECT().GetString("auth.value").Return("literal-fallback")

	// A referenced env var set to empty must fall through to the
	// keychain/literal steps — otherwise a stale reference could
	// permanently mask a usable literal.
	assert.Equal(t, "literal-fallback", ResolveToken(mock, ""))
}

func TestResolveToken_NoTokenFound(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("")
	mock.EXPECT().GetString("auth.keychain").Return("")
	mock.EXPECT().GetString("auth.value").Return("")

	assert.Empty(t, ResolveToken(mock, ""))
}

// TestResolveToken_KeychainReferenceUnsupportedBuild verifies that a
// configured auth.keychain reference is silently skipped in the
// default (no-keychain-tag) build so the resolver falls through to
// auth.value instead of surfacing an error. The -tags keychain
// build covers the success path via its own integration tests.
func TestResolveToken_KeychainReferenceUnsupportedBuild(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().GetString("auth.env").Return("")
	mock.EXPECT().GetString("auth.keychain").Return("mytool/github.auth")
	mock.EXPECT().GetString("auth.value").Return("literal-wins")

	assert.Equal(t, "literal-wins", ResolveToken(mock, ""),
		"unavailable keychain should fall through to literal")
}

func TestParseKeychainRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ref         string
		wantService string
		wantAccount string
		wantOK      bool
	}{
		{name: "simple", ref: "mytool/github.auth", wantService: "mytool", wantAccount: "github.auth", wantOK: true},
		{name: "nested account", ref: "mytool/bitbucket/auth", wantService: "mytool", wantAccount: "bitbucket/auth", wantOK: true},
		{name: "empty", ref: "", wantOK: false},
		{name: "no slash", ref: "mytool", wantOK: false},
		{name: "leading slash", ref: "/mytool/auth", wantOK: false},
		{name: "trailing slash", ref: "mytool/", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service, account, ok := parseKeychainRef(tc.ref)
			assert.Equal(t, tc.wantOK, ok)

			if tc.wantOK {
				assert.Equal(t, tc.wantService, service)
				assert.Equal(t, tc.wantAccount, account)
			}
		})
	}
}
