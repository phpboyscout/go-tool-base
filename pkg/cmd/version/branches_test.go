package version

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	propstest "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// newTestProps builds a Props whose release source is answered by provider,
// injected through Tool.ReleaseProvider so no forge adapter is consulted.
func newTestProps(t *testing.T, provider forge.Provider) *p.Props {
	t.Helper()

	l := logger.NewBuffer()

	return propstest.New(
		propstest.WithTool(p.Tool{
			Name: "test-tool",
			ReleaseSource: p.ReleaseSource{
				Type:  "github",
				Owner: "owner",
				Repo:  "repo",
			},
			ReleaseProvider: provider,
		}),
		propstest.WithLogger(l),
		propstest.WithConfig(testutil.StoreFromYAML(t, testConfig)),
		propstest.WithVersion(ver.NewInfo("v1.0.0", "abc123", "2026-06-20")),
		propstest.WithErrorHandler(errorhandling.New(logger.ToSlog(l), nil)),
	)
}

// runVersionCmd executes the wrapped version command with the given extra
// arguments and returns the captured stdout-equivalent output. It registers
// the "output" flag (normally supplied by the persistent root flag) so the
// JSON branch can be exercised, and routes all command output to a buffer so
// test noise stays out of stdout.
func runVersionCmd(t *testing.T, props *p.Props, format string, args ...string) (string, error) {
	t.Helper()

	cmd := NewCmdVersion(props)
	cmd.Flags().String("output", "text", "output format")
	require.NoError(t, cmd.Flags().Set("output", format))

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return buf.String(), err
}

// warningCount counts buffered log messages containing substr.
func warningCount(t *testing.T, props *p.Props, substr string) int {
	t.Helper()

	bl, ok := props.Logger.(interface{ Messages() []string })
	require.True(t, ok, "test logger must expose Messages()")

	count := 0

	for _, m := range bl.Messages() {
		if strings.Contains(m, substr) {
			count++
		}
	}

	return count
}

// TestNewCmdVersion_DevelopmentSkipsUpdateCheck covers the early-return branch
// taken when the running build is a development version: no VCS call is made
// and the command still succeeds for both text and JSON output.
func TestNewCmdVersion_DevelopmentSkipsUpdateCheck(t *testing.T) {
	// Serial (no t.Parallel): uses t.Setenv via newTestProps.
	tests := []struct {
		name   string
		format string
	}{
		{name: "text", format: "text"},
		{name: "json", format: "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := newTestProps(t, failingReleaseProvider())
			props.Version = ver.NewInfo("v1.2.3-dev", "abc123", "2026-06-20")

			require.True(t, props.Version.IsDevelopment())

			out, err := runVersionCmd(t, props, tt.format)
			require.NoError(t, err)
			assert.Contains(t, out, "v1.2.3-dev")
		})
	}
}

// TestNewCmdVersion_UpdateDisabledSkipsCheck covers the early-return branch
// taken when the update feature is disabled: the command reports the local
// build information without contacting the release source, with or without
// --check.
func TestNewCmdVersion_UpdateDisabledSkipsCheck(t *testing.T) {
	for _, args := range [][]string{nil, {"--check"}} {
		props := newTestProps(t, failingReleaseProvider())
		props.Tool.Features = p.SetFeatures(p.Disable(p.UpdateCmd))
		props.Features = nil // re-resolve from the changed Tool.Features
		props.ApplyDefaults()

		require.False(t, props.Features.Enabled(p.UpdateCmd))

		out, err := runVersionCmd(t, props, "text", args...)
		require.NoError(t, err)
		assert.Contains(t, out, "Version: v1.0.0")
	}
}

// TestNewCmdVersion_UpdaterLoadFailureIsNonFatal covers the branch where
// setup.NewUpdater returns an error: the command warns and still succeeds,
// reporting the local version with the degraded check_failed marker.
func TestNewCmdVersion_UpdaterLoadFailureIsNonFatal(t *testing.T) {
	props := newTestProps(t, failingReleaseProvider())
	// An unknown vcs.provider makes release.Lookup (inside NewUpdater) fail,
	// driving the degraded "failed to check latest version" branch.
	require.NoError(t, props.Config.AddLayer(t.Context(), "override",
		strings.NewReader("vcs:\n  provider: definitely-not-a-real-provider\n")))

	out, err := runVersionCmd(t, props, "text")
	require.NoError(t, err)
	assert.Contains(t, out, "Version: v1.0.0")
	assert.Equal(t, 1, warningCount(t, props, "failed to check latest version"))
}

// TestNewCmdVersion_UpdaterLoadFailureIsFatalWithCheck covers the same
// updater-construction failure under --check: the caller asked for hard
// semantics, so the failure is returned as a wrapped error.
func TestNewCmdVersion_UpdaterLoadFailureIsFatalWithCheck(t *testing.T) {
	props := newTestProps(t, failingReleaseProvider())
	require.NoError(t, props.Config.AddLayer(t.Context(), "override",
		strings.NewReader("vcs:\n  provider: definitely-not-a-real-provider\n")))

	_, err := runVersionCmd(t, props, "text", "--check")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to fetch latest version")
}

// TestNewCmdVersion_LatestFetchFailureDegrades is the regression test for the
// offline-degradation fix (spec 2026-07-23-version-command-offline-degradation):
// when GetLatestVersionString fails, the command must still exit 0 and print
// the local build information, emitting a single warning about the failed
// check instead of a hard error.
func TestNewCmdVersion_LatestFetchFailureDegrades(t *testing.T) {
	props := newTestProps(t, failingReleaseProvider())

	out, err := runVersionCmd(t, props, "text")
	require.NoError(t, err, "an unreachable release source must not fail the version command")

	assert.Contains(t, out, "Version: v1.0.0")
	assert.Contains(t, out, "Build:   abc123")
	assert.Contains(t, out, "Date:    2026-06-20")
	assert.NotContains(t, out, "Latest:")
	assert.Equal(t, 1, warningCount(t, props, "failed to check latest version"),
		"exactly one warning about the failed check")
}

// TestNewCmdVersion_LatestFetchFailureJSONCarriesMarker asserts the degraded
// JSON contract: success envelope, local fields present, no latest key,
// current false, and an explicit check_failed marker so scripts can
// distinguish "up to date" from "could not check".
func TestNewCmdVersion_LatestFetchFailureJSONCarriesMarker(t *testing.T) {
	props := newTestProps(t, failingReleaseProvider())

	out, err := runVersionCmd(t, props, "json")
	require.NoError(t, err)

	var resp struct {
		Status  string         `json:"status"`
		Command string         `json:"command"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &resp))

	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "version", resp.Command)
	assert.Equal(t, "v1.0.0", resp.Data["version"])
	assert.Equal(t, true, resp.Data["check_failed"])
	assert.Equal(t, false, resp.Data["current"])
	assert.NotContains(t, resp.Data, "latest")
}

// TestNewCmdVersion_LatestFetchFailureIsFatalWithCheck covers the opt-in
// --check flag: a lookup failure keeps the pre-fix hard semantics — wrapped
// error, non-zero exit, nothing written to the success stream.
func TestNewCmdVersion_LatestFetchFailureIsFatalWithCheck(t *testing.T) {
	props := newTestProps(t, failingReleaseProvider())

	out, err := runVersionCmd(t, props, "text", "--check")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to fetch latest version")
	assert.NotContains(t, out, "Version: v1.0.0")
}

// TestNewCmdVersion_CheckBypassesDevelopmentSkip covers the resolved
// spec decision that --check contacts the release source even on a
// development build, so maintainers can probe reachability from dev builds.
func TestNewCmdVersion_CheckBypassesDevelopmentSkip(t *testing.T) {
	t.Run("unreachable source fails", func(t *testing.T) {
		props := newTestProps(t, failingReleaseProvider())
		props.Version = ver.NewInfo("v1.2.3-dev", "abc123", "2026-06-20")

		require.True(t, props.Version.IsDevelopment())

		_, err := runVersionCmd(t, props, "text", "--check")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unable to fetch latest version")
	})

	t.Run("reachable source reports latest", func(t *testing.T) {
		props := newTestProps(t, releaseProvider("v2.0.0"))
		props.Version = ver.NewInfo("v1.2.3-dev", "abc123", "2026-06-20")

		out, err := runVersionCmd(t, props, "text", "--check")
		require.NoError(t, err)
		assert.Contains(t, out, "Latest:  v2.0.0 (update available)")
	})
}

// TestNewCmdVersion_JSONHappyPath drives the JSON output format through the
// successful update-check path against a provider reporting the same
// version (so the build is current and no degraded marker is present).
func TestNewCmdVersion_JSONHappyPath(t *testing.T) {
	props := newTestProps(t, releaseProvider("v1.0.0"))

	out, err := runVersionCmd(t, props, "json")
	require.NoError(t, err)

	var resp struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &resp))

	assert.Equal(t, true, resp.Data["current"])
	assert.NotContains(t, resp.Data, "check_failed")
}

// TestNewCmdVersion_OutdatedWarnsAndAnnotates covers the unchanged
// reachable-source default behaviour: when the release source reports a newer
// version, the Latest line is appended and a warning is logged.
func TestNewCmdVersion_OutdatedWarnsAndAnnotates(t *testing.T) {
	props := newTestProps(t, releaseProvider("v2.0.0"))

	out, err := runVersionCmd(t, props, "text")
	require.NoError(t, err)
	assert.Contains(t, out, "Latest:  v2.0.0 (update available)")
	assert.Equal(t, 1, warningCount(t, props, "a new version is available"))
}
