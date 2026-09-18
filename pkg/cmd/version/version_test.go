package version

import (
	"bytes"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const (
	testConfig = `
github:
  auth:
    env: GITHUB_TOKEN
`
)

func TestNewCmdVersion(t *testing.T) {
	t.Parallel()

	memFS := afero.NewMemMapFs()

	l := logger.NewNoop()
	cfgContainer := testutil.StoreFromYAML(t, testConfig)

	// Setup Props
	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
			ReleaseSource: p.ReleaseSource{
				Type:  "github",
				Owner: "owner",
				Repo:  "repo",
			},
			ReleaseProvider: releaseProvider("v1.0.0"),
		},
		Logger:       l,
		FS:           memFS,
		Config:       cfgContainer,
		Version:      ver.NewInfo("v1.0.0", "", ""), // Latest
		ErrorHandler: errorhandling.New(logger.ToSlog(l), nil),
	}

	cmd := NewCmdVersion(props)
	assert.NotNil(t, cmd)
	assert.Equal(t, "version", cmd.Use)

	// Execute command (Should be latest)
	err := cmd.Execute()
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

// TestNewCmdVersion_CISkipsTheLiveCheck (deferred item 8 of the v0.43.0
// round): version performed its own release lookup under --ci and CI=true,
// unlike every other network call the tool makes there. It now skips the
// call and says so; --check is an explicit request and still asks.
func TestNewCmdVersion_CISkipsTheLiveCheck(t *testing.T) {
	t.Setenv("CI", "true")

	l := logger.NewBuffer()
	props := &p.Props{
		Tool: p.Tool{Name: "test-tool", ReleaseSource: p.ReleaseSource{Type: "github", Owner: "owner", Repo: "repo"},
			ReleaseProvider: releaseProvider("v9.0.0")},
		Logger: l, FS: afero.NewMemMapFs(), Config: testutil.StoreFromYAML(t, testConfig),
		Version:      ver.NewInfo("v1.0.0", "", ""),
		ErrorHandler: errorhandling.New(logger.ToSlog(l), nil),
	}

	var out bytes.Buffer

	cmd := NewCmdVersion(props)
	cmd.Flags().String("output", "json", "")
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), `"check_skipped": true`)
	assert.NotContains(t, out.String(), `"latest"`, "no release-source call was made")
	assert.True(t, l.Contains("CI environment detected"))

	out.Reset()
	cmd = NewCmdVersion(props)
	cmd.Flags().String("output", "json", "")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--check"})
	_ = cmd.Execute()
	assert.Contains(t, out.String(), `"latest": "v9.0.0"`, "--check is an explicit request and still asks")
}
