package forge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestGiteaProfile_OffersNoLogin pins spec 0185 D1. forge-gitea deliberately
// does not implement forge.Authenticator — its own docs say Gitea is
// personal-access-token only — so the wizard must go straight to manual entry
// rather than attempt a login that cannot succeed.
func TestGiteaProfile_OffersNoLogin(t *testing.T) {
	t.Parallel()

	assert.False(t, giteaProfile.OffersLogin,
		"Gitea has no Authenticator upstream; offering login would always fail")
	assert.True(t, giteaProfile.OffersSSH,
		"Gitea does implement KeyManager, so SSH upload is offered")
}

// TestGiteaProfile_HasNoDefaultHost pins the property that makes Gitea the
// first hostless single-token profile: there is no "gitea.com" the way there is
// a github.com, so every instance is somebody's.
func TestGiteaProfile_HasNoDefaultHost(t *testing.T) {
	t.Parallel()

	assert.Empty(t, giteaProfile.Host,
		"Gitea must carry no default host; see spec 0185 D3")
}

// TestManualTokenInstructions_HostlessRendersNoEmptyInterpolation is the
// regression guard for the defect spec 0185 D3 records. With an empty Host, the
// templated branch would render "https:///user/settings/applications" and the
// generic branch a sentence ending in a blank. Both are broken output, not
// degraded output.
func TestManualTokenInstructions_HostlessRendersNoEmptyInterpolation(t *testing.T) {
	t.Parallel()

	got := manualTokenInstructions(giteaProfile)

	require.NotEmpty(t, got)
	assert.NotContains(t, got, "https:///",
		"an empty host must never be interpolated into a URL")
	assert.NotContains(t, got, "://",
		"the hostless message must carry no URL at all")
	assert.Contains(t, got, "Gitea",
		"the message must still name the forge")
	assert.Contains(t, got, giteaProfile.TokenScopes,
		"scopes are guidance the user applies by hand, so they must still appear")

	for _, line := range strings.Split(got, "\n") {
		assert.Equal(t, strings.TrimRight(line, " "), line,
			"no line may end in a blank left by an empty interpolation")
	}
}

// TestManualTokenInstructions_HostedRendersTheURL is the paired assertion: a
// profile that does have a host still gets the templated URL, so the divert
// above has not swallowed the normal path.
func TestManualTokenInstructions_HostedRendersTheURL(t *testing.T) {
	t.Parallel()

	got := manualTokenInstructions(gitLabProfile)

	assert.Contains(t, got, "https://gitlab.com/-/user_settings/personal_access_tokens")
	assert.NotContains(t, got, "{host}", "the host placeholder must be substituted")
	assert.Contains(t, got, gitLabProfile.TokenScopes)
}

// TestGitLabProfile_ShipsTheClientIDForGitLabDotComOnly pins the pairing that
// makes the shipped ID safe: an ID without a host would apply everywhere.
func TestGitLabProfile_ShipsTheClientIDForGitLabDotComOnly(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gitLabOAuthClientID, gitLabProfile.LoginClientID)
	assert.Equal(t, gitLabAPIHost, gitLabProfile.LoginClientIDHost,
		"a shipped client ID must name the host it is registered against")

	assert.Empty(t, giteaProfile.LoginClientID, "Gitea ships no client ID")
	assert.Empty(t, gitHubProfile.LoginClientID,
		"GitHub's login needs no shipped ID; spec 0185 D7 holds its behaviour fixed")
}

// TestSingleTokenProfilesHaveDistinctConfigPrefixes guards the invariant that
// the deferred Codeberg profile would have broken: two forges sharing a config
// prefix share one credential slot, so configuring either silently overwrites
// the other. See spec 0185 D2 and forge-gitea#1.
func TestSingleTokenProfilesHaveDistinctConfigPrefixes(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}

	for _, p := range []Profile{gitHubProfile, gitLabProfile, giteaProfile, bitbucketProfile} {
		if prior, clash := seen[p.ConfigPrefix]; clash {
			t.Fatalf("%s and %s both write config prefix %q — one would overwrite the other",
				prior, p.Label, p.ConfigPrefix)
		}

		seen[p.ConfigPrefix] = p.Label
	}
}

// TestSkipKeyFlagReachesEveryForgeThatOffersSSH pins the wiring, not the field.
//
// --skip-key is a global `init` flag, but it was passed only to
// NewGitHubInitialiser — correct while the SSH stage was single-token only, and
// wrong the moment GitLab, Gitea and Bitbucket started reaching it. A test that
// sets Initialiser.SkipKey directly passes either way, so this drives the
// registered provider the flag actually feeds.
func TestSkipKeyFlagReachesEveryForgeThatOffersSSH(t *testing.T) {
	t.Setenv("CI", "")

	p := newTestProps(t)

	preserveSkipFlags(t)

	skipKey = true

	for _, id := range []props.FeatureID{GithubFeature, GitlabFeature, GiteaFeature, BitbucketFeature} {
		providers := setup.GetInitialisers()[id]
		require.NotEmptyf(t, providers, "forge %q has no initialiser", id)

		built := providers[0](p)
		require.NotNilf(t, built, "forge %q yielded no initialiser", id)

		i, ok := built.(*Initialiser)
		require.Truef(t, ok, "forge %q did not yield a *Initialiser", id)

		if !i.profile.OffersSSH {
			continue
		}

		assert.Truef(t, i.SkipKey, "--skip-key must reach forge %q, which offers SSH", id)
	}
}
