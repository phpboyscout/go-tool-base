package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// resetRegistry is a test helper to clear the package-level middleware state.
func resetRegistry(t *testing.T) {
	t.Helper()
	ResetRegistryForTesting()
	t.Cleanup(ResetRegistryForTesting)
}

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

const testFeature = props.FeatureCmd("test-feature")

func TestRegisterMiddleware_Single(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterMiddleware(testFeature, testMiddleware("feature", &order))

	wrapped := Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		order = append(order, "handler")
		return nil
	})

	err := wrapped(&cobra.Command{}, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{"feature:before", "handler", "feature:after"}, order)
}

func TestRegisterMiddleware_Multiple(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterMiddleware(testFeature, testMiddleware("f1", &order), testMiddleware("f2", &order))

	wrapped := Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		order = append(order, "handler")
		return nil
	})

	err := wrapped(&cobra.Command{}, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{
		"f1:before",
		"f2:before",
		"handler",
		"f2:after",
		"f1:after",
	}, order)
}

func TestRegisterGlobalMiddleware(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterGlobalMiddleware(testMiddleware("global", &order))

	wrapped := Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		order = append(order, "handler")
		return nil
	})

	err := wrapped(&cobra.Command{}, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{"global:before", "handler", "global:after"}, order)
}

func TestSeal_PanicsOnRegistration(t *testing.T) {
	resetRegistry(t)

	Seal()

	assert.Panics(t, func() { RegisterMiddleware(testFeature, testMiddleware("m", nil)) },
		"RegisterMiddleware must panic after Seal")
	assert.Panics(t, func() { RegisterGlobalMiddleware(testMiddleware("m", nil)) },
		"RegisterGlobalMiddleware must panic after Seal")
}

func TestChain_GlobalBeforeFeature(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterGlobalMiddleware(testMiddleware("global", &order))
	RegisterMiddleware(testFeature, testMiddleware("feature", &order))

	wrapped := Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		order = append(order, "handler")
		return nil
	})

	err := wrapped(&cobra.Command{}, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{
		"global:before",
		"feature:before",
		"handler",
		"feature:after",
		"global:after",
	}, order)
}

func TestChain_EmptyRegistry(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	wrapped := Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		order = append(order, "handler")
		return nil
	})

	err := wrapped(&cobra.Command{}, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{"handler"}, order)
}

func TestChain_ExecutionOrder(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterGlobalMiddleware(testMiddleware("g1", &order))
	RegisterGlobalMiddleware(testMiddleware("g2", &order))
	RegisterMiddleware(testFeature, testMiddleware("f1", &order))
	RegisterMiddleware(testFeature, testMiddleware("f2", &order))

	wrapped := Chain(testFeature, func(_ *cobra.Command, _ []string) error {
		order = append(order, "handler")
		return nil
	})

	err := wrapped(&cobra.Command{}, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{
		"g1:before",
		"g2:before",
		"f1:before",
		"f2:before",
		"handler",
		"f2:after",
		"f1:after",
		"g2:after",
		"g1:after",
	}, order)
}

func TestChain_NilRunE(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	wrapped := Chain(testFeature, nil)
	assert.Nil(t, wrapped)
}

func TestAddCommandWithMiddleware_WiresRunE(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterGlobalMiddleware(testMiddleware("global", &order))

	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{
		Use: "child",
		RunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "handler")
			return nil
		},
	}

	AddCommandWithMiddleware(parent, child, testFeature)

	err := child.RunE(child, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{"global:before", "handler", "global:after"}, order)
	assert.Equal(t, []*cobra.Command{child}, parent.Commands())
}

func TestAddCommandWithMiddleware_NilRunE(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{Use: "child"} // no RunE

	AddCommandWithMiddleware(parent, child, testFeature)

	assert.Nil(t, child.RunE, "command without RunE should not be wrapped")
	assert.Equal(t, []*cobra.Command{child}, parent.Commands())
}

func TestAddCommandWithMiddleware_WiresSubcommands(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterGlobalMiddleware(testMiddleware("global", &order))

	parent := &cobra.Command{Use: "parent"}
	sub := &cobra.Command{
		Use: "sub",
		RunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "sub-handler")
			return nil
		},
	}
	child := &cobra.Command{Use: "child"}
	child.AddCommand(sub)

	AddCommandWithMiddleware(parent, child, testFeature)

	err := sub.RunE(sub, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{"global:before", "sub-handler", "global:after"}, order)
}

func TestApplyMiddlewareRecursively_Deep(t *testing.T) {
	t.Parallel()

	resetRegistry(t)

	var order []string

	RegisterGlobalMiddleware(testMiddleware("global", &order))

	level1 := &cobra.Command{Use: "l1"}
	level2 := &cobra.Command{Use: "l2"}
	level3 := &cobra.Command{
		Use: "l3",
		RunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "deep-handler")
			return nil
		},
	}
	level2.AddCommand(level3)
	level1.AddCommand(level2)

	ApplyMiddlewareRecursively(level1, testFeature)

	err := level3.RunE(level3, nil)

	assert.NoError(t, err)
	assert.Equal(t, []string{"global:before", "deep-handler", "global:after"}, order)
}
