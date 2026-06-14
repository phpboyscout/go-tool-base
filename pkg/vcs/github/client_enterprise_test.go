package github

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
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

func subFromYAML(t *testing.T, yaml string) config.Containable {
	t.Helper()

	cfg := config.NewReaderContainer(
		afero.NewMemMapFs(),
		config.WithConfigFormat("yaml"),
		config.WithConfigReaders(strings.NewReader(yaml)),
	)

	return cfg.Sub("github")
}

func TestEnterpriseURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		src              release.ReleaseSourceConfig
		cfgYAML          string
		wantAPIURL       string
		wantUploadURL    string
		wantNoEnterprise bool
	}{
		{
			name:             "public github.com uses defaults",
			src:              release.ReleaseSourceConfig{Host: "github.com"},
			wantNoEnterprise: true,
		},
		{
			name:             "empty host uses defaults",
			src:              release.ReleaseSourceConfig{},
			wantNoEnterprise: true,
		},
		{
			name:          "host-derived enterprise URLs",
			src:           release.ReleaseSourceConfig{Host: "ghe.example.com"},
			wantAPIURL:    "https://ghe.example.com/api/v3/",
			wantUploadURL: "https://ghe.example.com/api/uploads/",
		},
		{
			name: "config api override derives upload when unset",
			src:  release.ReleaseSourceConfig{},
			cfgYAML: "github:\n  url:\n    api: https://ghe.example.com/api/v3/\n" +
				"  placeholder: x\n",
			wantAPIURL:    "https://ghe.example.com/api/v3/",
			wantUploadURL: "https://ghe.example.com/api/uploads/",
		},
		{
			name: "config api and upload both honoured",
			src:  release.ReleaseSourceConfig{},
			cfgYAML: "github:\n  url:\n    api: https://ghe.example.com/api/v3/\n" +
				"    upload: https://uploads.ghe.example.com/\n",
			wantAPIURL:    "https://ghe.example.com/api/v3/",
			wantUploadURL: "https://uploads.ghe.example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var cfg config.Containable
			if tt.cfgYAML != "" {
				cfg = subFromYAML(t, tt.cfgYAML)
			}

			apiURL, uploadURL := enterpriseURLs(tt.src, cfg)

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
