package root

import (
	"context"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	forgesetup "gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// A tool on the static release channel (spec 0203 D10): `update` runs on a
// fresh install with no config file and with a forge feature enabled whose
// adapter is not linked, because the static updater reads neither. Every
// other command keeps both gates, and a tool on a forge keeps them for
// update too.
func TestPreRun_StaticUpdate_PassesTheGatesABrokenInstallWouldFail(t *testing.T) {
	t.Parallel()

	staticProps := func(features ...p.FeatureState) *p.Props {
		props := noConfigProps(t, "statictool", append([]p.FeatureState{p.Enable(p.UpdateCmd), p.Enable(forgesetup.GithubFeature)}, features...)...)
		props.Tool.ReleaseSource = p.ReleaseSource{Type: p.ReleaseSourceStatic, BaseURL: "https://pkg.example.internal/acme/statictool"}

		return props
	}

	// The github adapter is not linked into this test binary, so the pre-run
	// would refuse before reading any configuration.
	require.NotEmpty(t, forgesetup.Unlinked(staticProps().GetFeatures()), "the fixture needs an unlinked forge to prove the exemption")

	run := func(t *testing.T, props *p.Props, name string) (bool, error) {
		t.Helper()

		rootCmd := NewCmdRoot(props)

		var ran bool

		for _, c := range rootCmd.Commands() {
			if c.Name() == name {
				c.RunE = func(*cobra.Command, []string) error {
					ran = true

					return nil
				}
			}
		}

		rootCmd.SetArgs([]string{name})
		rootCmd.SetOut(io.Discard)
		rootCmd.SetErr(io.Discard)

		return ran, rootCmd.ExecuteContext(context.Background())
	}

	t.Run("update on the static channel runs", func(t *testing.T) {
		t.Parallel()

		props := staticProps()

		ran, err := run(t, props, "update")
		require.NoError(t, err)
		assert.True(t, ran)
		assert.NotNil(t, props.Config, "bootstrap still ran; the gate was relaxed, not skipped")
	})

	t.Run("another command on the same tool is still refused", func(t *testing.T) {
		t.Parallel()

		ran, err := run(t, staticProps(p.Enable(p.DoctorCmd)), "doctor")
		require.ErrorIs(t, err, forgesetup.ErrForgeNotLinked)
		assert.False(t, ran)
	})

	t.Run("update on a forge keeps the gates", func(t *testing.T) {
		t.Parallel()

		props := staticProps()
		props.Tool.ReleaseSource = p.ReleaseSource{Type: "github", Owner: "acme", Repo: "tool"}

		ran, err := run(t, props, "update")
		require.ErrorIs(t, err, forgesetup.ErrForgeNotLinked)
		assert.False(t, ran)
	})
}

func TestUpdateSkipsTheConfigGateOnlyOnTheStaticChannel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		source p.ReleaseSource
		skips  bool
	}{
		{p.ReleaseSource{Type: p.ReleaseSourceStatic, BaseURL: "https://pkg.example.internal/t"}, true},
		{p.ReleaseSource{Type: "github", Owner: "o", Repo: "r"}, false},
	} {
		props := noConfigProps(t, "gatetool", p.Enable(p.UpdateCmd))
		props.Tool.ReleaseSource = tc.source

		var found bool

		for _, c := range NewCmdRoot(props).Commands() {
			if c.Name() == "update" {
				found = true
				assert.Equal(t, tc.skips, setup.SkipsConfigCheck(c), "release source %q", tc.source.Type)
			}
		}

		require.True(t, found)
	}
}
