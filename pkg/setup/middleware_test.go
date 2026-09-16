package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Every test contributes to a registry of its own and chains over its
// snapshot (spec 0199 D5), so the default registry is never written and the
// tests run in parallel.

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

func contributeMiddleware(r features.Registry, id features.ID, mws ...Middleware) {
	for _, m := range mws {
		r.Contribute(id, SlotMiddleware, m)
	}
}

func runChained(t *testing.T, r features.Registry, order *[]string) {
	t.Helper()

	wrapped := ChainIn(r.Snapshot(), testFeature, func(_ *cobra.Command, _ []string) error {
		*order = append(*order, "handler")

		return nil
	})

	require.NoError(t, wrapped(&cobra.Command{}, nil))
}

func TestChainIn_FeatureMiddleware(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
	contributeMiddleware(r, testFeature, testMiddleware("feature", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{"feature:before", "handler", "feature:after"}, order)
}

func TestChainIn_MultipleKeepRegistrationOrder(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
	contributeMiddleware(r, testFeature, testMiddleware("f1", &order), testMiddleware("f2", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{"f1:before", "f2:before", "handler", "f2:after", "f1:after"}, order)
}

func TestChainIn_GlobalOnly(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
	contributeMiddleware(r, features.Global, testMiddleware("global", &order))

	runChained(t, r, &order)
	assert.Equal(t, []string{"global:before", "handler", "global:after"}, order)
}

func TestChainIn_GlobalBeforeFeature(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
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

	runChained(t, features.NewRegistry(), &order)
	assert.Equal(t, []string{"handler"}, order)
}

func TestChainIn_NilRunE(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ChainIn(features.NewRegistry().Snapshot(), testFeature, nil))
}

// TestChainIn_SnapshotIsFixed is what replaced the seal (spec 0199 D1): a
// chain built from a snapshot does not change when middleware is contributed
// afterwards, and the later contribution does not panic.
func TestChainIn_SnapshotIsFixed(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
	contributeMiddleware(r, testFeature, testMiddleware("early", &order))
	snap := r.Snapshot()

	contributeMiddleware(r, testFeature, testMiddleware("late", &order))

	wrapped := ChainIn(snap, testFeature, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Equal(t, []string{"early:before", "early:after"}, order)

	order = nil
	wrapped = ChainIn(r.Snapshot(), testFeature, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Equal(t, []string{"early:before", "late:before", "late:after", "early:after"}, order)
}

// TestRegisterMiddleware_ContributesToTheDefault pins the init-time entry
// points onto the default registry through a unique feature ID, which is the
// one write to the default registry a test may make: an ID nobody else uses.
func TestRegisterMiddleware_ContributesToTheDefault(t *testing.T) {
	t.Parallel()

	const id = props.FeatureID("test-middleware-default-probe")

	var order []string

	RegisterMiddleware(id, testMiddleware("probe", &order))

	wrapped := Chain(id, func(_ *cobra.Command, _ []string) error { return nil })
	require.NoError(t, wrapped(&cobra.Command{}, nil))
	assert.Contains(t, order, "probe:before")
}
