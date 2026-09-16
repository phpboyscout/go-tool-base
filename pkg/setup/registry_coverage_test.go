package setup

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestRegisterOn_PopulatesEveryReader contributes every slot to a registry of
// its own and reads each back through its *In accessor.
func TestRegisterOn_PopulatesEveryReader(t *testing.T) {
	t.Parallel()

	feature := props.FeatureID("cov-feature")
	ip := func(_ *props.Props, _ *pflag.FlagSet) Initialiser { return nil }
	sp := func(_ *props.Props) []*cobra.Command { return nil }
	fp := func(_ *cobra.Command) {}
	cp := func(_ *props.Props) []CheckFunc { return nil }

	r := features.NewRegistry()
	RegisterOn(r, feature, []InitialiserProvider{ip}, []SubcommandProvider{sp}, []FeatureFlag{fp})
	r.Contribute(feature, SlotCheck, CheckProvider(cp))

	s := r.Snapshot()
	assert.Len(t, InitialisersIn(s)[feature], 1)
	assert.Len(t, SubcommandsIn(s)[feature], 1)
	assert.Len(t, FeatureFlagsIn(s)[feature], 1)
	assert.Len(t, ChecksIn(s)[feature], 1)

	// The maps are the reader's own: mutating one does not reach the registry.
	inits := InitialisersIn(s)
	delete(inits, feature)
	assert.Contains(t, InitialisersIn(s), feature)
}

// fakeCollector records TrackCommand invocations for WithTelemetry coverage.
type fakeCollector struct {
	props.NoopCollector
	name     string
	duration int64
	exitCode int
	calls    int
}

func (f *fakeCollector) TrackCommand(name string, durationMs int64, exitCode int, _ map[string]string) {
	f.calls++
	f.name = name
	f.duration = durationMs
	f.exitCode = exitCode
}

func TestWithTelemetry_TracksCommand(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		fc := &fakeCollector{}
		p := &props.Props{Collector: fc}

		handler := WithTelemetry(p)(func(_ *cobra.Command, _ []string) error {
			return nil
		})

		err := handler(&cobra.Command{Use: "telemetry-cmd"}, nil)
		require.NoError(t, err)

		assert.Equal(t, 1, fc.calls)
		assert.Equal(t, "telemetry-cmd", fc.name)
		assert.Equal(t, 0, fc.exitCode)
		assert.GreaterOrEqual(t, fc.duration, int64(0))
	})

	t.Run("Error", func(t *testing.T) {
		t.Parallel()

		fc := &fakeCollector{}
		p := &props.Props{Collector: fc}

		wantErr := assert.AnError
		handler := WithTelemetry(p)(func(_ *cobra.Command, _ []string) error {
			return wantErr
		})

		err := handler(&cobra.Command{Use: "telemetry-cmd"}, nil)
		require.ErrorIs(t, err, wantErr)

		assert.Equal(t, 1, fc.calls)
		assert.Equal(t, 1, fc.exitCode)
	})
}

// TestRequireReleaseToken_GiteaCodebergDirect covers the gitea/codeberg/direct
// branches of the provider switch that the existing TestRequireReleaseToken
// (github/gitlab/bitbucket) does not exercise.
func TestRequireReleaseToken_GiteaCodebergDirect(t *testing.T) {
	tests := []struct {
		name        string
		vcsProvider string
		fallbackEnv string
	}{
		{name: "gitea", vcsProvider: "gitea", fallbackEnv: "GITEA_TOKEN"},
		{name: "codeberg", vcsProvider: "codeberg", fallbackEnv: "CODEBERG_TOKEN"},
		{name: "direct", vcsProvider: "direct", fallbackEnv: "DIRECT_TOKEN"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" missing token errors", func(t *testing.T) {
			// Not parallel: mutates process env via t.Setenv.
			t.Setenv(tt.fallbackEnv, "")

			p := &props.Props{Config: testutil.StoreFromYAML(t, "{}\n")}

			require.Error(t, requireReleaseToken(context.Background(), tt.vcsProvider, p))
		})

		t.Run(tt.name+" with config token succeeds", func(t *testing.T) {
			t.Setenv(tt.fallbackEnv, "")

			p := &props.Props{Config: testutil.StoreFromYAML(t, tt.vcsProvider+":\n  auth:\n    value: secret-token\n")}

			require.NoError(t, requireReleaseToken(context.Background(), tt.vcsProvider, p))
		})
	}
}

func TestGetCurrentVersion_ReturnsField(t *testing.T) {
	t.Parallel()

	s := &SelfUpdater{CurrentVersion: "v1.2.3"}
	assert.Equal(t, "v1.2.3", s.GetCurrentVersion())
}
