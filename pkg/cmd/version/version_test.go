package version

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v80/github"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	ver "github.com/phpboyscout/go-tool-base/pkg/version"
)

const (
	testConfig = `
github:
  url:
    api: %s
    upload: %s
  auth:
    env: GITHUB_TOKEN
`
)

func TestNewCmdVersion(t *testing.T) {
	// Setup Mock GitHub API
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/owner/repo/releases/latest" {
			v := "v1.0.0"
			resp := github.RepositoryRelease{
				TagName: &v,
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	// Setup Config pointing to mock server
	cfgContent := fmt.Sprintf(testConfig, server.URL, server.URL)
	memFS := afero.NewMemMapFs()
	_ = afero.WriteFile(memFS, "config.yaml", []byte(cfgContent), 0644)

	l := logger.NewNoop()
	cfgContainer, err := config.Load([]string{"config.yaml"}, memFS, l, false)
	require.NoError(t, err)

	t.Setenv("GITHUB_TOKEN", "dummy")

	// Setup Props
	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
			ReleaseSource: p.ReleaseSource{
				Type:  "github",
				Owner: "owner",
				Repo:  "repo",
			},
		},
		Logger:       l,
		FS:           memFS,
		Config:       cfgContainer,
		Version:      ver.NewInfo("v1.0.0", "", ""), // Latest
		ErrorHandler: errorhandling.New(l, nil),
	}

	cmd := NewCmdVersion(props)
	assert.NotNil(t, cmd)
	assert.Equal(t, "version", cmd.Use)

	// Execute command (Should be latest)
	err = cmd.Execute()
	require.NoError(t, err)

	// Test Outdated
	props.Version = ver.NewInfo("v0.0.1", "", "")
	cmd = NewCmdVersion(props)
	err = cmd.Execute()
	assert.NoError(t, err)
}

func TestPrintVersionText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		info     *VersionInfo
		contains []string
		excludes []string
	}{
		{
			name:     "full info current",
			info:     &VersionInfo{Version: "v1.0.0", Commit: "abc123", Date: "2026-03-23", Latest: "v1.0.0", Current: true},
			contains: []string{"Version: v1.0.0", "Build:   abc123", "Date:    2026-03-23"},
			excludes: []string{"update available"},
		},
		{
			name:     "outdated version",
			info:     &VersionInfo{Version: "v0.9.0", Commit: "abc123", Date: "2026-03-23", Latest: "v1.0.0", Current: false},
			contains: []string{"Version: v0.9.0", "Latest:  v1.0.0 (update available)"},
		},
		{
			name:     "minimal info",
			info:     &VersionInfo{Version: "v1.0.0", Current: true},
			contains: []string{"Version: v1.0.0"},
			excludes: []string{"Build:", "Date:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			printVersionText(&buf, tt.info)
			text := buf.String()

			for _, s := range tt.contains {
				assert.Contains(t, text, s)
			}

			for _, s := range tt.excludes {
				assert.NotContains(t, text, s)
			}
		})
	}
}
