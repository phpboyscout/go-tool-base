package root

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	p "github.com/phpboyscout/gtb/pkg/props"
	ver "github.com/phpboyscout/gtb/pkg/version"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/log"
	"github.com/google/go-github/v80/github"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNewCmdRoot(t *testing.T) {
	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger: log.New(io.Discard),
		FS:     afero.NewMemMapFs(),
	}
	// Needs a valid config load capability or skip it.
	// NewCmdRootPersistentPreRunE loads config.
	// We can point it to a temp file.

	props.Assets = p.NewAssets()
	cmd := NewCmdRoot(props)
	assert.NotNil(t, cmd)
	assert.Equal(t, "test-tool", cmd.Use)
}

func TestCheckForUpdates(t *testing.T) {
	// Setup Mock GitHub API
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/owner/repo/releases/latest" {
			v := "v1.0.0"
			resp := github.RepositoryRelease{
				TagName: &v,
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	// Setup Config pointing to mock server
	cfgContent := fmt.Sprintf(testConfig, server.URL, server.URL)
	memFS := afero.NewMemMapFs()
	afero.WriteFile(memFS, "config.yaml", []byte(cfgContent), 0644)

	logger := log.New(io.Discard)
	cfgContainer, err := config.Load([]string{"config.yaml"}, memFS, logger, false)
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
		Logger: logger,
		FS:     memFS,
		Config: cfgContainer,
		Version: ver.NewInfo("v0.0.1", "", ""), // Outdated
		ErrorHandler: errorhandling.New(logger, nil),
	}

	props.Assets = p.NewAssets()
	cmd := NewCmdRoot(props)
	flags := &FlagValues{
		CI:    false,
		Debug: true,
	}

	// Create state with mock form creator to avoid interactive prompt
	state := newRootState()
	state.formCreator = func(runUpdate *bool) *huh.Form {
		*runUpdate = false // Decline update
		return nil
	}

	result := checkForUpdates(context.Background(), cmd, props, flags, state)

	// Verify we attempted update check but declined
	assert.False(t, result.HasUpdated)
	assert.False(t, result.ShouldExit)
	assert.NoError(t, result.Error)

	// Test "Already Latest" scenario
	props.Version = ver.NewInfo("v1.0.0", "", "")
	result = checkForUpdates(context.Background(), cmd, props, flags, state)
	assert.False(t, result.HasUpdated)
	assert.False(t, result.ShouldExit)
	assert.NoError(t, result.Error)
}
