package github

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go/forge"
)

// TestPullRequestsPerPage_WithinGitHubCap guards the per-page constant against
// regressing past GitHub's hard List cap of 100.
func TestPullRequestsPerPage_WithinGitHubCap(t *testing.T) {
	t.Parallel()

	assert.LessOrEqual(t, pullRequestsPerPage, 100, "GitHub clamps List PerPage to 100")
}

func TestDeriveUploadURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		apiURL string
		want   string
	}{
		{"ghe api derives uploads", "https://ghe.example.com/api/v3/", "https://ghe.example.com/api/uploads/"},
		{"bare host", "https://ghe.example.com", "https://ghe.example.com/api/uploads/"},
		{"unparseable returns input unchanged", "://no-scheme", "://no-scheme"},
		{"no host returns input unchanged", "/relative/path", "/relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, deriveUploadURL(tt.apiURL))
		})
	}
}

func TestEnterpriseURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		settings         ClientSettings
		wantAPIURL       string
		wantUploadURL    string
		wantNoEnterprise bool
	}{
		{
			name:             "public github.com uses defaults",
			settings:         ClientSettings{ReleaseSource: forge.ReleaseSourceConfig{Host: "github.com"}},
			wantNoEnterprise: true,
		},
		{
			name:             "empty host uses defaults",
			wantNoEnterprise: true,
		},
		{
			name: "host-derived enterprise URLs",
			settings: ClientSettings{
				ReleaseSource: forge.ReleaseSourceConfig{Host: "ghe.example.com"},
			},
			wantAPIURL:    "https://ghe.example.com/api/v3/",
			wantUploadURL: "https://ghe.example.com/api/uploads/",
		},
		{
			name:          "settings api override derives upload when unset",
			settings:      ClientSettings{APIURL: "https://ghe.example.com/api/v3/"},
			wantAPIURL:    "https://ghe.example.com/api/v3/",
			wantUploadURL: "https://ghe.example.com/api/uploads/",
		},
		{
			name: "settings api and upload both honoured",
			settings: ClientSettings{
				APIURL:    "https://ghe.example.com/api/v3/",
				UploadURL: "https://uploads.ghe.example.com/",
			},
			wantAPIURL:    "https://ghe.example.com/api/v3/",
			wantUploadURL: "https://uploads.ghe.example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			apiURL, uploadURL := enterpriseURLs(tt.settings)

			if tt.wantNoEnterprise {
				assert.Empty(t, apiURL)
				assert.Empty(t, uploadURL)

				return
			}

			assert.Equal(t, tt.wantAPIURL, apiURL)
			assert.Equal(t, tt.wantUploadURL, uploadURL,
				"upload URL must be derived from the API host when unset")
		})
	}
}
