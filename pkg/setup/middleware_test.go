package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Every test contributes to a registry of its own and chains through a
// MiddlewareChain over the Set it resolves (spec 0199 D3, D5), so the default
// registry is never written and the tests run in parallel.

func testMiddleware(name string, order *[]string) Middleware {
	return func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
		return func(cmd *cobra.Command, args []string) error {
			*order = append(*order, name+":before")

			err := next(cmd, args)

			*order = append(*order, name+":after")

			return err
		}
	}
}

const testFeature = props.FeatureID("test-feature")

// testRegistry declares testFeature so its contributions are handed out once
// the feature is enabled.
func testRegistry(t *testing.T) features.Registry {
	t.Helper()

	r := features.NewRegistry()
	require.NoError(t, r.Declare(props.FeatureDescriptor{ID: testFeature, ConstName: "TestFeature", ConstPackage: "example.com/t", Kind: "test"}))

	return r
}

func contributeMiddleware(r features.Registry, id features.ID, mws ...Middleware) {
	for _, m := range mws {
		r.Contribute(id, SlotMiddleware, m)
	}
}

// chainOver resolves r with testFeature enabled and returns a chain with no
// built-ins, so only the contributions show in the order.
func chainOver(t *testing.T, r features.Registry) *MiddlewareChain {
	t.Helper()

	set, err := features.Resolve(r.Snapshot(), []features.State{{ID: testFeature, Enabled: true}})
	require.NoError(t, err)

	return NewMiddlewareChain(nil, set)
}

func runChained(t *testing.T, r features.Registry, order *[]string) {
	t.Helper()

	wrapped := chainOver(t, r).Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		*order = append(*order, "handler")

		return nil
	})

	require.NoError(t, wrapped(&cobra.Command{}, nil))
}

func TestChainIn_FeatureMiddleware(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, testFeature, testMiddleware("feature", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{"feature:before", "handler", "feature:after"}, order)
}

func TestChainIn_MultipleKeepRegistrationOrder(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, testFeature, testMiddleware("f1", &order), testMiddleware("f2", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{"f1:before", "f2:before", "handler", "f2:after", "f1:after"}, order)
}

func TestChainIn_GlobalOnly(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, features.Global, testMiddleware("global", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{"global:before", "handler", "global:after"}, order)
}

func TestChainIn_GlobalBeforeFeature(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, features.Global, testMiddleware("g1", &order), testMiddleware("g2", &order))
	contributeMiddleware(r, testFeature, testMiddleware("f1", &order), testMiddleware("f2", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{
		"g1:before", "g2:before", "f1:before", "f2:before",
		"handler",
		"f2:after", "f1:after", "g2:after", "g1:after",
	}, order)
}

func TestChainIn_EmptyRegistry(t *testing.T) {
	t.Parallel()

	var order []string

	runChained(t, testRegistry(t), &order)
	assert.Equal(t, []string{"handler"}, order)
}

func TestChainIn_NilRunE(t *testing.T) {
	t.Parallel()

	assert.Nil(t, chainOver(t, testRegistry(t)).Chain(testFeature, nil))
}

// TestChainIn_SnapshotIsFixed is what replaced the seal (spec 0199 D1): a
// chain built from a snapshot does not change when middleware is contributed
// afterwards, and the later contribution does not panic.
func TestChainIn_SnapshotIsFixed(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, testFeature, testMiddleware("early", &order))
	early := chainOver(t, r)

	contributeMiddleware(r, testFeature, testMiddleware("late", &order))

	wrapped := early.Chain(testFeature, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Equal(t, []string{"early:before", "early:after"}, order)

	order = nil
	wrapped = chainOver(t, r).Chain(testFeature, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Equal(t, []string{"early:before", "late:before", "late:after", "early:after"}, order)
}

// TestBuiltinsRunBeforeContributions pins the chain's order: the root's
// built-ins first, then the Set's global contributions, then the feature's.
func TestBuiltinsRunBeforeContributions(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, features.Global, testMiddleware("global", &order))
	contributeMiddleware(r, testFeature, testMiddleware("feature", &order))

	set, err := features.Resolve(r.Snapshot(), []features.State{{ID: testFeature, Enabled: true}})
	require.NoError(t, err)

	wrapped := NewMiddlewareChain([]Middleware{testMiddleware("builtin", &order)}, set).
		Chain(testFeature, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Equal(t, []string{"builtin:before", "global:before", "feature:before", "feature:after", "global:after", "builtin:after"}, order)
}

// TestDisabledFeatureContributesNoMiddleware: a feature's middleware applies
// only while the feature is enabled in the Set the chain was built from.
func TestDisabledFeatureContributesNoMiddleware(t *testing.T) {
	t.Parallel()

	var order []string

	r := testRegistry(t)
	contributeMiddleware(r, testFeature, testMiddleware("feature", &order))

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	wrapped := NewMiddlewareChain(nil, set).Chain(testFeature, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Empty(t, order)
}
