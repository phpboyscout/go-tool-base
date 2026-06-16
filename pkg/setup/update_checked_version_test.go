package setup_test

import (
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestCheckedVersion_RoundTrip proves the last_checked marker carries the
// latest version in its body (the persistent out-of-date reminder source)
// while its modtime still drives the interval throttle — one file, two jobs.
func TestCheckedVersion_RoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	const name = "checked-version-tool"

	// Nothing recorded yet.
	assert.Empty(t, setup.GetCheckedVersion(fs, name))

	// Recording a version stores it AND refreshes the check timestamp, so the
	// interval throttle (SkipUpdateCheck) sees a recent check.
	require.NoError(t, setup.SetCheckedVersion(fs, name, "v2.0.0"))
	assert.Equal(t, "v2.0.0", setup.GetCheckedVersion(fs, name))

	cmd := &cobra.Command{Use: "run"}
	assert.True(t, setup.SkipUpdateCheck(fs, name, cmd, 24*time.Hour),
		"a just-recorded check is within the interval, so the next check is skipped")

	// An empty version clears the body (tool up to date) but keeps stamping.
	require.NoError(t, setup.SetCheckedVersion(fs, name, ""))
	assert.Empty(t, setup.GetCheckedVersion(fs, name))
	assert.True(t, setup.SkipUpdateCheck(fs, name, cmd, 24*time.Hour),
		"clearing the version still refreshes the check timestamp")
}

func TestResolveCheckInterval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		toolDefault time.Duration
		configValue string
		want        time.Duration
	}{
		// No tool baseline: config drives the result, falling back to the
		// framework default for empty/invalid values.
		{name: "empty config, no baseline -> framework default", configValue: "", want: setup.DefaultCheckInterval},
		{name: "config 24h", configValue: "24h", want: 24 * time.Hour},
		{name: "config 168h", configValue: "168h", want: 168 * time.Hour},
		{name: "config 0 means every run", configValue: "0", want: 0},
		{name: "config 0s means every run", configValue: "0s", want: 0},
		{name: "config garbage -> framework default", configValue: "garbage", want: setup.DefaultCheckInterval},
		{name: "config negative -> framework default", configValue: "-5h", want: setup.DefaultCheckInterval},
		{name: "config trims whitespace", configValue: "  48h  ", want: 48 * time.Hour},

		// Tool baseline applies only when config does not supply a valid value.
		{name: "baseline used when config empty", toolDefault: 12 * time.Hour, configValue: "", want: 12 * time.Hour},
		{name: "baseline used when config invalid", toolDefault: 12 * time.Hour, configValue: "nope", want: 12 * time.Hour},
		{name: "config overrides baseline", toolDefault: 12 * time.Hour, configValue: "1h", want: time.Hour},
		{name: "config 0 overrides baseline (every run)", toolDefault: 12 * time.Hour, configValue: "0", want: 0},
		{name: "zero baseline falls through to framework default", toolDefault: 0, configValue: "", want: setup.DefaultCheckInterval},
		{name: "negative baseline ignored", toolDefault: -1, configValue: "", want: setup.DefaultCheckInterval},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, setup.ResolveCheckInterval(tc.toolDefault, tc.configValue))
		})
	}
}
