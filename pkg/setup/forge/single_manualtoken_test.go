package forge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestManualTokenInstructions covers the two shapes: a profile with a
// token-creation URL template (GitHub) resolves the host and lists scopes; a
// template-less profile degrades to a generic host-named message with no
// forge-specific URL literal.
func TestManualTokenInstructions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		profile     Profile
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:    "github profile resolves URL and scopes",
			profile: gitHubProfile,
			wantContain: []string{
				"https://github.com/settings/tokens/new?scopes=repo,read:org,gist",
				"Required scopes: repo, read:org, gist",
			},
		},
		{
			name:    "enterprise host substituted into the template",
			profile: Profile{Host: "ghe.example.com", TokenCreateURLTemplate: gitHubProfile.TokenCreateURLTemplate, TokenScopes: gitHubProfile.TokenScopes},
			wantContain: []string{
				"https://ghe.example.com/settings/tokens/new",
			},
			wantAbsent: []string{"{host}", "github.com"},
		},
		{
			name:        "template-less profile degrades to a generic message",
			profile:     Profile{Host: "git.example.com"},
			wantContain: []string{"Create a personal access token on git.example.com"},
			wantAbsent:  []string{"settings/tokens", "Required scopes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := manualTokenInstructions(tt.profile)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}

			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestManualTokenInstructions_HostlessProfile covers the profile shape Gitea
// introduces: a forge with no default host (there is no "gitea.com" the way
// there is a "github.com"). Both branches of the instructions interpolate
// Profile.Host, so an empty host produced broken output rather than degraded
// output — "https:///user/settings/applications", or a sentence ending in a
// blank. Neither is acceptable to show a user.
func TestManualTokenInstructions_HostlessProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		profile     Profile
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "hostless profile with a URL template renders no broken URL",
			profile: Profile{ //nolint:gosec // G101: URL template, not a credential
				Label:                  "Gitea",
				TokenCreateURLTemplate: "https://{host}/user/settings/applications",
				TokenScopes:            "write:repository, read:user",
			},
			wantContain: []string{"Gitea", "write:repository, read:user"},
			wantAbsent:  []string{"https:///", "{host}"},
		},
		{
			name:        "hostless profile without a template names the forge, not a blank",
			profile:     Profile{Label: "Gitea"},
			wantContain: []string{"Gitea"},
			wantAbsent:  []string{"token on ,", "on , then", "  ,"},
		},
		{
			name:       "hostless and label-less profile still reads as a sentence",
			profile:    Profile{},
			wantAbsent: []string{"https:///", "{host}", "on , then", " for , "},
		},
		{
			name:        "whitespace-only host is treated as absent",
			profile:     Profile{Label: "Gitea", Host: "   "},
			wantContain: []string{"Gitea"},
			wantAbsent:  []string{"on    ,", "https://   /"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := manualTokenInstructions(tt.profile)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}

			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}

			// Whatever branch is taken, the output must never contain an empty
			// interpolation artefact.
			assert.NotContains(t, got, "https:///", "empty host interpolated into a URL")
			assert.NotRegexp(t, `\s,\s`, got, "empty host interpolated into prose")
		})
	}
}

// TestManualToken_NoForgeSpecificLiteralsInWizard guards the acceptance
// criterion that the wizard body carries no forge-specific URL literal — the
// GitHub PAT path now lives only on the Profile.
func TestManualToken_NoForgeSpecificLiteralsInWizard(t *testing.T) {
	t.Parallel()

	// A profile that mimics GitHub's shape but on a different host must not leak
	// a hard-coded github.com anywhere.
	got := manualTokenInstructions(Profile{ //nolint:gosec // G101: URL template, not a credential
		Host:                   "gitea.example.org",
		TokenCreateURLTemplate: "https://{host}/user/settings/applications",
	})
	assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "Open this URL"))
	assert.Contains(t, got, "https://gitea.example.org/user/settings/applications")
	assert.NotContains(t, got, "github")
}
