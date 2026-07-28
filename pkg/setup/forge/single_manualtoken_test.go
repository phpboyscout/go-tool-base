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
