package setup

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// These tests mutate the package-global feature registry, so they must not run
// with t.Parallel(); each resets the registry first via resetRegistry (defined
// in middleware_test.go), which clears both the middleware and feature state.

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

func TestRegister_AddsProvidersForFeature(t *testing.T) {
	resetRegistry(t)

	Register(regFeatureA,
		[]InitialiserProvider{newInitialiserProvider()},
		[]SubcommandProvider{newSubcommandProvider()},
		[]FeatureFlag{newFeatureFlag()},
	)

	require.Len(t, GetInitialisers()[regFeatureA], 1)
	require.Len(t, GetSubcommands()[regFeatureA], 1)
	require.Len(t, GetFeatureFlags()[regFeatureA], 1)
}

func TestRegister_AppendsAcrossCalls(t *testing.T) {
	resetRegistry(t)

	Register(regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)
	Register(regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)

	assert.Len(t, GetInitialisers()[regFeatureA], 2)
}

func TestRegister_NilSlicesAreIgnored(t *testing.T) {
	resetRegistry(t)

	Register(regFeatureA, nil, nil, nil)

	assert.Empty(t, GetInitialisers())
	assert.Empty(t, GetSubcommands())
	assert.Empty(t, GetFeatureFlags())
}

func TestRegisterChecks_AddsAndAppends(t *testing.T) {
	resetRegistry(t)

	RegisterChecks(regFeatureA, []CheckProvider{newCheckProvider()})
	RegisterChecks(regFeatureA, []CheckProvider{newCheckProvider()})

	assert.Len(t, GetChecks()[regFeatureA], 2)
}

func TestRegisterChecks_NilIgnored(t *testing.T) {
	resetRegistry(t)

	RegisterChecks(regFeatureA, nil)

	assert.Empty(t, GetChecks())
}

func TestSealRegistry_PanicsOnSubsequentRegistration(t *testing.T) {
	resetRegistry(t)

	SealRegistry()

	assert.Panics(t, func() {
		Register(regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)
	}, "Register must panic after the registry is sealed")

	assert.Panics(t, func() {
		RegisterChecks(regFeatureA, []CheckProvider{newCheckProvider()})
	}, "RegisterChecks must panic after the registry is sealed")
}

// TestGetters_ReturnIsolatedSnapshots verifies the Get* accessors return a copy
// of the registry map: a registration made after the snapshot is taken must not
// appear in the earlier snapshot.
func TestGetters_ReturnIsolatedSnapshots(t *testing.T) {
	resetRegistry(t)

	Register(regFeatureA, []InitialiserProvider{newInitialiserProvider()}, nil, nil)

	snapshot := GetInitialisers()
	require.Contains(t, snapshot, regFeatureA)
	require.NotContains(t, snapshot, regFeatureB)

	// Register a second feature after the snapshot was taken.
	Register(regFeatureB, []InitialiserProvider{newInitialiserProvider()}, nil, nil)

	assert.NotContains(t, snapshot, regFeatureB, "earlier snapshot must not observe later registration")
	assert.Contains(t, GetInitialisers(), regFeatureB, "a fresh snapshot must observe the new registration")
}

func TestGetChecks_ReturnsRegisteredProviders(t *testing.T) {
	resetRegistry(t)

	RegisterChecks(regFeatureA, []CheckProvider{
		func(_ *props.Props) []CheckFunc {
			return []CheckFunc{
				func(_ context.Context, _ *props.Props) CheckResult {
					return CheckResult{Name: "demo", Status: "ok"}
				},
			}
		},
	})

	checks := GetChecks()
	require.Len(t, checks[regFeatureA], 1)

	results := checks[regFeatureA][0](nil)
	require.Len(t, results, 1)
	assert.Equal(t, "demo", results[0](context.Background(), nil).Name)
}
