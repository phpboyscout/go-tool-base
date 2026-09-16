package setup

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Middleware wraps a cobra RunE function with additional behaviour.
// The middleware receives the next handler in the chain and returns
// a new handler that may execute logic before and/or after calling next.
type Middleware func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error

// RegisterMiddleware contributes middleware for the commands of one feature to
// the default registry. Middleware is applied in registration order.
func RegisterMiddleware(feature props.FeatureID, mw ...Middleware) {
	for _, m := range mw {
		features.Default().Contribute(feature, SlotMiddleware, m)
	}
}

// RegisterGlobalMiddleware contributes middleware applied to every feature
// command. Global middleware runs before feature-specific middleware.
func RegisterGlobalMiddleware(mw ...Middleware) {
	for _, m := range mw {
		features.Default().Contribute(features.Global, SlotMiddleware, m)
	}
}

// Chain applies the default registry's middleware (global, then the feature's)
// to runE and returns the wrapped function.
func Chain(feature props.FeatureID, runE func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return ChainIn(features.Default().Snapshot(), feature, runE)
}

// ChainIn is Chain over a snapshot.
func ChainIn(s features.Snapshot, feature props.FeatureID, runE func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	if runE == nil {
		return nil
	}

	chain := middlewareIn(s, features.Global)
	chain = append(chain, middlewareIn(s, feature)...)

	// Apply in reverse order so that the first registered middleware
	// is the outermost wrapper (executes first).
	wrapped := runE
	for i := len(chain) - 1; i >= 0; i-- {
		wrapped = chain[i](wrapped)
	}

	return wrapped
}

func middlewareIn(s features.Snapshot, id features.ID) []Middleware {
	values := s.Contributions(id, SlotMiddleware)
	out := make([]Middleware, 0, len(values))

	for _, v := range values {
		if m, ok := v.(Middleware); ok {
			out = append(out, m)
		}
	}

	return out
}
