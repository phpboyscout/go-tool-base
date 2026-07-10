package version

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// newTestProps builds a Props wired to an in-memory config that points the
// GitHub release source at the supplied API URL. Pass an unreachable apiURL for
// paths that never reach the VCS.
func newTestProps(t *testing.T, apiURL string) *p.Props {
	t.Helper()

	l := logger.NewBuffer()

	cfgContent := fmt.Sprintf(testConfig, apiURL, apiURL)
	memFS := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(memFS, "config.yaml", []byte(cfgContent), 0o644))

	cfgContainer, err := config.Load([]string{"config.yaml"}, memFS, false, config.WithLogger(logger.ToSlog(l)))
	require.NoError(t, err)

	t.Setenv("GITHUB_TOKEN", "dummy")

	return &p.Props{
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
		Version:      ver.NewInfo("v1.0.0", "abc123", "2026-06-20"),
		ErrorHandler: errorhandling.New(l, nil),
	}
}

// runVersionCmd executes the wrapped version command. It registers the
// "output" flag (normally supplied by the persistent root flag) so the JSON
// branch can be exercised, and routes cobra's own output to a buffer so test
// noise stays out of stdout.
func runVersionCmd(t *testing.T, props *p.Props, format string) error {
	t.Helper()

	cmd := NewCmdVersion(props)
	cmd.Flags().String("output", "text", "output format")
	require.NoError(t, cmd.Flags().Set("output", format))

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)

	return cmd.Execute()
}

// TestNewCmdVersion_DevelopmentSkipsUpdateCheck covers the early-return branch
// taken when the running build is a development version: no VCS call is made
// and the command still succeeds for both text and JSON output.
func TestNewCmdVersion_DevelopmentSkipsUpdateCheck(t *testing.T) {
	// Serial (no t.Parallel): exercises code that writes to os.Stdout and uses
	// t.Setenv via newTestProps.
	tests := []struct {
		name   string
		format string
	}{
		{name: "text", format: "text"},
		{name: "json", format: "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := newTestProps(t, "http://127.0.0.1:0")
			props.Version = ver.NewInfo("v1.2.3-dev", "abc123", "2026-06-20")

			require.True(t, props.Version.IsDevelopment())
			require.NoError(t, runVersionCmd(t, props, tt.format))
		})
	}
}

// TestNewCmdVersion_UpdateDisabledSkipsCheck covers the early-return branch
// taken when the update feature is disabled: the command reports the local
// build information without contacting the release source.
func TestNewCmdVersion_UpdateDisabledSkipsCheck(t *testing.T) {
	props := newTestProps(t, "http://127.0.0.1:0")
	props.Tool.Features = p.SetFeatures(p.Disable(p.UpdateCmd))

	require.True(t, props.Tool.IsDisabled(p.UpdateCmd))
	require.NoError(t, runVersionCmd(t, props, "text"))
}

// TestNewCmdVersion_UpdaterLoadFailureIsNonFatal covers the branch where
// setup.NewUpdater returns an error: the command logs a warning and still
// succeeds, reporting the local version only.
func TestNewCmdVersion_UpdaterLoadFailureIsNonFatal(t *testing.T) {
	props := newTestProps(t, "http://127.0.0.1:0")
	// An unknown vcs.provider makes release.Lookup (inside NewUpdater) fail,
	// driving the non-fatal "failed to load updater" branch.
	props.Config.Set("vcs.provider", "definitely-not-a-real-provider")

	require.NoError(t, runVersionCmd(t, props, "text"))
}

// TestNewCmdVersion_LatestFetchFailureIsFatal covers the branch where
// GetLatestVersionString fails: the error is wrapped and returned to the
// caller.
func TestNewCmdVersion_LatestFetchFailureIsFatal(t *testing.T) {
	// Server that always 500s the releases endpoint so the fetch fails.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	props := newTestProps(t, server.URL)

	err := runVersionCmd(t, props, "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to fetch latest version")
}

// TestNewCmdVersion_JSONHappyPath drives the JSON output format through the
// successful update-check path against a mock server reporting the same
// version (so the build is current).
func TestNewCmdVersion_JSONHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/owner/repo/releases/latest" {
			_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))

			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	props := newTestProps(t, server.URL)
	require.NoError(t, runVersionCmd(t, props, "json"))
}
