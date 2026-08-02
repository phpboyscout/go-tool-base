package setup

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// These tests touch the process-global feature registry and middleware
// registry. They MUST NOT use t.Parallel() and must reset the registry via
// resetRegistry so they cannot race the other registry-touching tests.

func TestRegister_PopulatesSnapshots(t *testing.T) {
	resetRegistry(t)

	const feature = props.FeatureID("registry-coverage-feature")

	ip := func(_ *props.Props) Initialiser { return nil }
	sp := func(_ *props.Props) []*cobra.Command { return []*cobra.Command{{Use: "sub"}} }
	fp := func(_ *cobra.Command) {}
	cp := func(_ *props.Props) []CheckFunc {
		return []CheckFunc{
			func(_ context.Context, _ *props.Props) CheckResult {
				return CheckResult{Name: "c", Status: "ok"}
			},
		}
	}

	Register(feature, []InitialiserProvider{ip}, []SubcommandProvider{sp}, []FeatureFlag{fp})
	RegisterChecks(feature, []CheckProvider{cp})

	inits := GetInitialisers()
	require.Contains(t, inits, feature)
	assert.Len(t, inits[feature], 1)

	subs := GetSubcommands()
	require.Contains(t, subs, feature)
	assert.Len(t, subs[feature], 1)

	flags := GetFeatureFlags()
	require.Contains(t, flags, feature)
	assert.Len(t, flags[feature], 1)

	checks := GetChecks()
	require.Contains(t, checks, feature)
	assert.Len(t, checks[feature], 1)

	// The returned maps are snapshots: mutating them must not affect the
	// registry's internal state.
	delete(inits, feature)
	assert.Contains(t, GetInitialisers(), feature, "snapshot mutation must not leak into registry")
}

func TestRegister_NilSlicesAreNoops(t *testing.T) {
	resetRegistry(t)

	const feature = props.FeatureID("registry-nil-feature")

	// nil provider slices must be skipped without creating entries.
	Register(feature, nil, nil, nil)
	RegisterChecks(feature, nil)

	assert.NotContains(t, GetInitialisers(), feature)
	assert.NotContains(t, GetSubcommands(), feature)
	assert.NotContains(t, GetFeatureFlags(), feature)
	assert.NotContains(t, GetChecks(), feature)
}

func TestSealRegistry_PanicsOnRegister(t *testing.T) {
	resetRegistry(t)

	const feature = props.FeatureID("registry-seal-feature")

	SealRegistry()

	assert.Panics(t, func() {
		Register(feature, []InitialiserProvider{func(_ *props.Props) Initialiser { return nil }}, nil, nil)
	}, "Register must panic once the registry is sealed")

	assert.Panics(t, func() {
		RegisterChecks(feature, []CheckProvider{
			func(_ *props.Props) []CheckFunc { return nil },
		})
	}, "RegisterChecks must panic once the registry is sealed")
}

func TestIsSealed_ReflectsSealState(t *testing.T) {
	resetRegistry(t)

	assert.False(t, IsSealed(), "registry should not be sealed after reset")

	Seal()

	assert.True(t, IsSealed(), "registry should report sealed after Seal")
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
