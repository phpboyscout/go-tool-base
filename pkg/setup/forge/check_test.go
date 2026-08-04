package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// The check reports which rung supplied a credential, never the credential. A
// doctor report is pasted into issues and support bundles, so a test that only
// asserted the happy path would not notice the day it started echoing a token.

func TestCheckForgeCredential(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		envRef      string
		fallback    string
		wantStatus  string
		wantMessage string
	}{
		{
			name:        "resolves from the env reference",
			yaml:        "github:\n  auth:\n    env: GTB_CHECK_TOKEN\n",
			envRef:      "tok-from-env-ref",
			wantStatus:  "pass",
			wantMessage: "resolves from auth.env",
		},
		{
			name:        "resolves from the literal",
			yaml:        "github:\n  auth:\n    value: tok-literal\n",
			wantStatus:  "pass",
			wantMessage: "resolves from auth.value",
		},
		{
			name:        "resolves from the well-known fallback",
			yaml:        "github: {}\n",
			fallback:    "tok-from-fallback",
			wantStatus:  "pass",
			wantMessage: "resolves from fallback environment variable",
		},
		{
			// The case the check exists for: configured, but broken. Before it,
			// this was indistinguishable from "not configured" until something
			// tried to authenticate and got a 401.
			name:        "a malformed keychain reference is diagnosed",
			yaml:        "github:\n  auth:\n    keychain: no-slash-here\n",
			wantStatus:  "warn",
			wantMessage: "credential configured but does not resolve",
		},
		{
			// Not a failure: a tool may legitimately never talk to this forge.
			name:        "nothing configured is a skip, not a failure",
			yaml:        "github: {}\n",
			wantStatus:  "skip",
			wantMessage: "no credential configured",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GTB_CHECK_TOKEN", tc.envRef)
			t.Setenv("GTB_CHECK_FALLBACK", tc.fallback)

			profile := gitHubProfile
			profile.FallbackEnv = "GTB_CHECK_FALLBACK"

			p := &props.Props{Config: testutil.StoreFromYAML(t, tc.yaml)}

			got := checkForgeCredential(profile)(t.Context(), p)

			assert.Equal(t, tc.wantStatus, got.Status)
			assert.Equal(t, tc.wantMessage, got.Message)
			assert.Equal(t, "GitHub credential", got.Name)

			// Whichever branch ran, no credential may appear anywhere in the
			// result — this is the property that makes the check safe to paste.
			for _, secret := range []string{"tok-from-env-ref", "tok-literal", "tok-from-fallback"} {
				assert.NotContains(t, got.Message+got.Details, secret,
					"a doctor result must never carry a credential value")
			}
		})
	}
}

// TestCheckForgeCredential_NoConfigIsSkipped covers the nil-store path: doctor
// runs before configuration is guaranteed, and a check that panicked there
// would take the whole report down.
func TestCheckForgeCredential_NoConfigIsSkipped(t *testing.T) {
	t.Parallel()

	got := checkForgeCredential(gitHubProfile)(t.Context(), &props.Props{})

	assert.Equal(t, "skip", got.Status)
	assert.Equal(t, "no configuration loaded", got.Message)
}

// TestCredentialCheckRegisteredForEverySingleTokenForge is the parity guard for
// this check, in the same spirit as the rest of the package: a forge added later
// gets one automatically, and if it does not, this fails.
//
// The dual-credential shape is excluded deliberately — see registerCredentialCheck.
func TestCredentialCheckRegisteredForEverySingleTokenForge(t *testing.T) {
	t.Parallel()

	checks := setup.GetChecks()

	for _, p := range []Profile{gitHubProfile, gitLabProfile, giteaProfile, codebergProfile} {
		require.NotEmptyf(t, checks[p.Feature],
			"%s is a single-token forge with no credential-resolution check", p.Label)
	}

	assert.Emptyf(t, checks[bitbucketProfile.Feature],
		"the dual-credential shape resolves from different keys entirely; a single-token "+
			"check would report a working Bitbucket as unconfigured")
}
