package setup

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Every test contributes to a registry of its own and reads it through the
// *In accessors (spec 0199 D5), so the default registry is never written and
// the tests run in parallel.

const (
	regFeatureA = props.FeatureID("feature-a")
	regFeatureB = props.FeatureID("feature-b")
)

func newInitialiserProvider() InitialiserProvider {
	return func(_ *props.Props) Initialiser { return nil }
}

func newSubcommandProvider() SubcommandProvider {
	return func(_ *props.Props) []*cobra.Command { return nil }
}

func newFeatureFlag() FeatureFlag {
	return func(_ *cobra.Command) {}
}

func newCheckProvider() CheckProvider {
	return func(_ *props.Props) []CheckFunc { return nil }
}

func TestRegisterOn_AddsProvidersForFeature(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	RegisterOn(r, regFeatureA,
		[]InitialiserProvider{newInitialiserProvider()},
		[]SubcommandProvider{newSubcommandProvider()},
		[]FeatureFlag{newFeatureFlag()},
	)

	s := r.Snapshot()
	require.Len(t, InitialisersIn(s)[regFeatureA], 1)
	require.Len(t, SubcommandsIn(s)[regFeatureA], 1)
	require.Len(t, FeatureFlagsIn(s)[regFeatureA], 1)
}

func TestRegisterOn_AppendsAcrossCalls(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	RegisterOn(r, regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)
	RegisterOn(r, regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)

	assert.Len(t, InitialisersIn(r.Snapshot())[regFeatureA], 2)
}

func TestRegisterOn_NilSlicesAreIgnored(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	RegisterOn(r, regFeatureA, nil, nil, nil)

	s := r.Snapshot()
	assert.Empty(t, InitialisersIn(s))
	assert.Empty(t, SubcommandsIn(s))
	assert.Empty(t, FeatureFlagsIn(s))
}

func TestChecksIn_AddsAndAppends(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	r.Contribute(regFeatureA, SlotCheck, newCheckProvider())
	r.Contribute(regFeatureA, SlotCheck, newCheckProvider())

	assert.Len(t, ChecksIn(r.Snapshot())[regFeatureA], 2)
}

// TestSnapshotsAreIsolated is what replaced the seal (spec 0199 D1): a
// registration made after a snapshot is taken does not appear in it, a fresh
// snapshot sees it, and nothing panics.
func TestSnapshotsAreIsolated(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	RegisterOn(r, regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)

	first := InitialisersIn(r.Snapshot())
	require.Contains(t, first, regFeatureA)
	require.NotContains(t, first, regFeatureB)

	RegisterOn(r, regFeatureB, []InitialiserProvider{newInitialiserProvider()}, nil, nil)

	assert.NotContains(t, first, regFeatureB, "an earlier snapshot must not observe a later registration")
	assert.Contains(t, InitialisersIn(r.Snapshot()), regFeatureB, "a fresh snapshot must")
}

func TestChecksIn_ReturnsRegisteredProviders(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	r.Contribute(regFeatureA, SlotCheck, CheckProvider(func(_ *props.Props) []CheckFunc {
		return []CheckFunc{
			func(_ context.Context, _ *props.Props) CheckResult {
				return CheckResult{Name: "demo", Status: "ok"}
			},
		}
	}))

	checks := ChecksIn(r.Snapshot())
	require.Len(t, checks[regFeatureA], 1)

	results := checks[regFeatureA][0](nil)
	require.Len(t, results, 1)
	assert.Equal(t, "demo", results[0](context.Background(), nil).Name)
}

func TestAssetsIn_RoundTrip(t *testing.T) {
	t.Parallel()

	bundle := fstest.MapFS{"assets/config.yaml": &fstest.MapFile{Data: []byte("x: 1\n")}}

	r := features.NewRegistry()
	r.Contribute("examplefeature", SlotAssets, AssetBundle{Name: "example", Bundle: bundle})

	got := AssetsIn(r.Snapshot())
	require.Len(t, got["examplefeature"], 1)
	assert.Equal(t, "example", got["examplefeature"][0].Name)
	assert.Equal(t, bundle, got["examplefeature"][0].Bundle)
}

// TestContributionsBy_SkipsOtherTypes: a value of another type under a slot is
// somebody else's contribution, not an error for this reader.
func TestContributionsBy_SkipsOtherTypes(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	r.Contribute(regFeatureA, SlotCheck, "not a provider")
	r.Contribute(regFeatureA, SlotCheck, newCheckProvider())

	assert.Len(t, ChecksIn(r.Snapshot())[regFeatureA], 1)
}

// TestRegister_ContributesToTheDefault pins the init-time entry points onto
// the default registry through IDs nobody else uses, the one write to the
// default registry a test may make.
func TestRegister_ContributesToTheDefault(t *testing.T) {
	t.Parallel()

	const id = props.FeatureID("test-registry-default-probe")

	Register(id, []InitialiserProvider{newInitialiserProvider()}, []SubcommandProvider{newSubcommandProvider()}, []FeatureFlag{newFeatureFlag()})
	RegisterChecks(id, []CheckProvider{newCheckProvider()})
	RegisterAssets(id, "probe", fstest.MapFS{})

	assert.Len(t, GetInitialisers()[id], 1)
	assert.Len(t, GetSubcommands()[id], 1)
	assert.Len(t, GetFeatureFlags()[id], 1)
	assert.Len(t, GetChecks()[id], 1)
	assert.Len(t, GetAssets()[id], 1)
}
