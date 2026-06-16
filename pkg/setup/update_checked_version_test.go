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

	cases := map[string]time.Duration{
		"":        setup.DefaultCheckInterval,
		"24h":     24 * time.Hour,
		"168h":    168 * time.Hour,
		"0":       0,
		"0s":      0,
		"garbage": setup.DefaultCheckInterval,
		"-5h":     setup.DefaultCheckInterval,
		"  48h  ": 48 * time.Hour,
	}

	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, setup.ResolveCheckInterval(in))
		})
	}
}
