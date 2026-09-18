package vcs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// TestCredentialOption_HandsTheFactoryGTBsChain: the option a construction
// site passes carries ForgeCredential over the endpoint's section, so a factory
// that consults it and nothing else (go/forge spec 0025 D2) sees the credential
// auth.env points at, the bare-CI-image case from #76.
func TestCredentialOption_HandsTheFactoryGTBsChain(t *testing.T) {
	t.Setenv("MY_DEPLOYMENT_TOKEN", "tok-from-env")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "gitlab:\n  auth:\n    env: MY_DEPLOYMENT_TOKEN\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeGitLab}, cfg, "GITLAB_TOKEN")...)
	require.NotNil(t, o.Credential, "a single-token forge gets GTB's chain")

	token, err := o.Credential(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-from-env", token)
}

// TestCredentialOption_AnAbsentSectionStillReadsTheFallbackVariable: a tool
// configured purely by environment has no forge section at all, and the chain's
// last rung is what makes that work.
func TestCredentialOption_AnAbsentSectionStillReadsTheFallbackVariable(t *testing.T) {
	t.Setenv("GITEA_TOKEN", "tok-fallback")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "github:\n  url:\n    api: https://example.test\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeGitea}, cfg, "GITEA_TOKEN")...)
	require.NotNil(t, o.Credential)

	token, err := o.Credential(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-fallback", token)
}

// TestCredentialOption_ARefusedRungIsAnErrorNotAnAbsence: the view this
// replaces turned a chain error into an empty string, so "the keychain refused"
// reached the provider as "nothing configured". Through the option the factory
// receives the error and construction fails with the reason.
func TestCredentialOption_ARefusedRungIsAnErrorNotAnAbsence(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "github:\n  auth:\n    keychain: no-slash-here\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeGitHub}, cfg, "GITHUB_TOKEN")...)
	require.NotNil(t, o.Credential)

	token, err := o.Credential(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed keychain reference")
	assert.Empty(t, token)
}

// TestCredentialOption_TheCallersContextReachesTheChain: the view resolved on
// context.Background, so a keychain or remote store was reached with no
// deadline. The factory calls the source with the construction context.
func TestCredentialOption_TheCallersContextReachesTheChain(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "tok")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "gitlab:\n  auth:\n    env: GITLAB_TOKEN\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeGitLab}, cfg, "GITLAB_TOKEN")...)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := o.Credential(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// TestCredentialOptions_BitbucketGetsBothHalves (#89, go/forge spec 0026):
// Bitbucket authenticates with a username and an app password, so the
// factory is handed both, each walking GTB's keys: the env reference the
// bundle ships (username.env, app_password.env), then the literal, then the
// well-known variable. The adapter used to compose these itself and report
// the bundled pointers as stale, so a Bitbucket tool failed construction on
// its shipped defaults.
func TestCredentialOptions_BitbucketGetsBothHalves(t *testing.T) {
	t.Setenv("MY_BB_USER", "alice")
	t.Setenv("MY_BB_PASS", "app-pw")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "bitbucket:\n  username:\n    env: MY_BB_USER\n  app_password:\n    env: MY_BB_PASS\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeBitbucket}, cfg, "")...)
	require.NotNil(t, o.Username, "the username half")
	require.NotNil(t, o.Credential, "the app-password half")

	user, err := o.Username(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "alice", user)

	pw, err := o.Credential(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "app-pw", pw)
}

// TestCredentialOptions_BitbucketFallsBackToTheWellKnownVariables: the
// shipped bundle's pointers name the well-known variables; with the
// variables exported and nothing else configured, both halves resolve.
func TestCredentialOptions_BitbucketFallsBackToTheWellKnownVariables(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "bob")
	t.Setenv("BITBUCKET_APP_PASSWORD", "pw2")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "bitbucket:\n  username:\n    env: BITBUCKET_USERNAME\n  app_password:\n    env: BITBUCKET_APP_PASSWORD\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeBitbucket}, cfg, "")...)

	user, err := o.Username(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "bob", user)

	pw, err := o.Credential(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "pw2", pw)
}

// TestCredentialOptions_BitbucketWithNothingIsAnAbsence: the bare CI image,
// the shipped bundle and nothing exported. Both halves are empty and neither
// errors; the adapter is what refuses an unauthenticated private call.
func TestCredentialOptions_BitbucketWithNothingIsAnAbsence(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "bitbucket:\n  username:\n    env: BITBUCKET_USERNAME\n  app_password:\n    env: BITBUCKET_APP_PASSWORD\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeBitbucket}, cfg, "")...)

	user, err := o.Username(context.Background())
	require.NoError(t, err)
	assert.Empty(t, user)

	pw, err := o.Credential(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pw)
}

// TestCredentialOptions_BitbucketLiteralMode: a half stored as a literal
// (the wizard's literal mode writes bitbucket.username as a scalar, which
// leaves no room for an env child) is read, and the well-known variable
// stands below it.
func TestCredentialOptions_BitbucketLiteralMode(t *testing.T) {
	t.Setenv("BITBUCKET_USERNAME", "from-env")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	cfg := ConfigFromReader(testutil.ViewFromYAML(t, "bitbucket:\n  username: literal-user\n  app_password: literal-pw\n"))
	o := forge.NewOptions(CredentialOptions(forge.Endpoint{Type: forge.SourceTypeBitbucket}, cfg, "")...)

	user, err := o.Username(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "literal-user", user, "a literal beats the well-known variable")

	pw, err := o.Credential(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "literal-pw", pw)
}
