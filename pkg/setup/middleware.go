package setup

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// RunE is cobra's run signature, the thing a Middleware wraps.
type RunE = func(cmd *cobra.Command, args []string) error

// Middleware wraps a cobra RunE function with additional behaviour.
// The middleware receives the next handler in the chain and returns
// a new handler that may execute logic before and/or after calling next.
type Middleware func(next RunE) RunE

// RegisterMiddleware contributes middleware for the commands of one feature to
// the default registry. Middleware is applied in registration order. Call it
// at init; the root snapshots the registry when it builds its chain.
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

// Chainer wraps a command's RunE with the middleware that applies to its
// feature. The root owns one (spec 0199 D3); a test or a consumer hands in
// its own through root.WithChain.
type Chainer interface {
	Chain(feature props.FeatureID, runE RunE) RunE
}

// MiddlewareChain is the default Chainer: the root's built-in middleware
// first, then the Set's global contributions, then the feature's own, each in
// registration order. It is exported so a consumer can embed it.
type MiddlewareChain struct {
	builtin []Middleware
	set     features.Set
}

// NewMiddlewareChain builds a chain from the root's built-in middleware and
// the contributions of the enabled features in set.
func NewMiddlewareChain(builtin []Middleware, set features.Set) *MiddlewareChain {
	return &MiddlewareChain{builtin: builtin, set: set}
}

// Chain wraps runE. A nil runE stays nil: a pure command group has nothing to
// wrap.
func (c *MiddlewareChain) Chain(feature props.FeatureID, runE RunE) RunE {
	if runE == nil {
		return nil
	}

	chain := append([]Middleware(nil), c.builtin...)
	chain = append(chain, middlewareOf(c.set, features.Global)...)

	if feature != "" {
		chain = append(chain, middlewareOf(c.set, feature)...)
	}

	// Apply in reverse order so that the first registered middleware
	// is the outermost wrapper (executes first).
	wrapped := runE
	for i := len(chain) - 1; i >= 0; i-- {
		wrapped = chain[i](wrapped)
	}

	return wrapped
}

func middlewareOf(set features.Set, id features.ID) []Middleware {
	if set == nil {
		return nil
	}

	mws, _ := features.ContributionsOf[Middleware](set, id, SlotMiddleware)

	return mws
}
