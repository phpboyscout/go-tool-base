package vcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, "token-from-env", ResolveToken(AuthConfig{Env: "MY_CUSTOM_TOKEN"}, ""))
}

func TestResolveToken_FromConfigValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "literal-token", ResolveToken(AuthConfig{Value: "literal-token"}, ""))
}

func TestResolveToken_FromFallbackEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fallback-token")

	assert.Equal(t, "fallback-token", ResolveToken(AuthConfig{}, "GITHUB_TOKEN"))
}

func TestResolveToken_PrecedenceConfigEnvOverValue(t *testing.T) {
	t.Setenv("PRIORITY_TOKEN", "env-wins")

	assert.Equal(t, "env-wins", ResolveToken(AuthConfig{Env: "PRIORITY_TOKEN"}, ""),
		"auth.env should short-circuit before auth.value is consulted")
}

func TestResolveToken_PrecedenceConfigOverFallback(t *testing.T) {
	t.Setenv("FALLBACK_TOKEN", "fallback-loses")

	assert.Equal(t, "config-wins", ResolveToken(AuthConfig{Value: "config-wins"}, "FALLBACK_TOKEN"),
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

	// A referenced env var set to empty must fall through to the
	// keychain/literal steps — otherwise a stale reference could
	// permanently mask a usable literal.
	assert.Equal(t, "literal-fallback", ResolveToken(AuthConfig{
		Env:   "EMPTY_TOKEN",
		Value: "literal-fallback",
	}, ""))
}

func TestResolveToken_NoTokenFound(t *testing.T) {
	t.Parallel()

	assert.Empty(t, ResolveToken(AuthConfig{}, ""))
}

// TestResolveToken_KeychainReferenceUnsupportedBuild verifies that a
// configured auth.keychain reference is silently skipped in the
// default (no-keychain-tag) build so the resolver falls through to
// auth.value instead of surfacing an error. The -tags keychain
// build covers the success path via its own integration tests.
func TestResolveToken_KeychainReferenceUnsupportedBuild(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "literal-wins", ResolveToken(AuthConfig{
		Keychain: "mytool/github.auth",
		Value:    "literal-wins",
	}, ""),
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
