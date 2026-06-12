package setup

import (
	"sync"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Middleware wraps a cobra RunE function with additional behaviour.
// The middleware receives the next handler in the chain and returns
// a new handler that may execute logic before and/or after calling next.
type Middleware func(next func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error

var (
	mu                sync.RWMutex
	globalMiddleware  []Middleware
	featureMiddleware = make(map[props.FeatureCmd][]Middleware)
	sealed            bool
)

// RegisterMiddleware adds middleware that will be applied to commands
// belonging to the specified feature. Middleware is applied in
// registration order.
func RegisterMiddleware(feature props.FeatureCmd, mw ...Middleware) {
	mu.Lock()
	defer mu.Unlock()

	if sealed {
		panic("cannot register middleware after command registration is complete")
	}

	featureMiddleware[feature] = append(featureMiddleware[feature], mw...)
}

// RegisterGlobalMiddleware adds middleware that is applied to all
// feature commands. Global middleware runs before feature-specific
// middleware in the chain.
func RegisterGlobalMiddleware(mw ...Middleware) {
	mu.Lock()
	defer mu.Unlock()

	if sealed {
		panic("cannot register global middleware after command registration is complete")
	}

	globalMiddleware = append(globalMiddleware, mw...)
}

// Seal prevents further middleware registration. Called after all
// commands have been registered.
func Seal() {
	mu.Lock()
	defer mu.Unlock()

	sealed = true
}

// IsSealed reports whether the middleware registry has been sealed. Callers
// that register built-in middleware once per process use this to stay
// idempotent — a second root construction reuses the already-sealed registry
// instead of re-registering and panicking.
func IsSealed() bool {
	mu.RLock()
	defer mu.RUnlock()

	return sealed
}

// Chain applies all registered middleware (global + feature-specific)
// to the given RunE function and returns the wrapped function.
func Chain(feature props.FeatureCmd, runE func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	mu.RLock()
	defer mu.RUnlock()

	if runE == nil {
		return nil
	}

	// Build the full chain: global first, then feature-specific.
	chain := make([]Middleware, 0, len(globalMiddleware)+len(featureMiddleware[feature]))
	chain = append(chain, globalMiddleware...)
	chain = append(chain, featureMiddleware[feature]...)

	// Apply in reverse order so that the first registered middleware
	// is the outermost wrapper (executes first).
	wrapped := runE
	for i := len(chain) - 1; i >= 0; i-- {
		wrapped = chain[i](wrapped)
	}

	return wrapped
}

// AddCommandWithMiddleware adds cmd as a subcommand of parent and wraps
// cmd.RunE with the middleware [Chain] for feature.
//
// Deprecated: use [Command.Register] instead. AddCommandWithMiddleware
// remains as a thin shim that delegates to Register and will be removed
// in v1.0. Unlike the prior implementation it does NOT recursively
// re-wrap descendants with feature — each command should be wrapped
// with its own feature at construction (via [Wrap]) and added to its
// parent via the parent's Register method, which wires middleware
// exactly once per command.
func AddCommandWithMiddleware(parent, cmd *cobra.Command, feature props.FeatureCmd) {
	// Wrap parent solely to reuse Register's AddCommand path; the
	// parent's own feature is irrelevant here because Register only
	// wraps the child's RunE, never the parent's.
	Wrap("", parent).Register(Wrap(feature, cmd))
}

// ApplyMiddlewareRecursively applies middleware to cmd and all of its
// descendants with the same feature key.
//
// Deprecated: prefer wrapping each command with its own feature at
// construction (via [Wrap]) and registering subcommands via
// [Command.Register], which wires middleware exactly once per command.
// ApplyMiddlewareRecursively remains for backward compatibility and
// will be removed in v1.0.
func ApplyMiddlewareRecursively(cmd *cobra.Command, feature props.FeatureCmd) {
	if cmd.RunE != nil {
		cmd.RunE = Chain(feature, cmd.RunE)
	}

	for _, sub := range cmd.Commands() {
		ApplyMiddlewareRecursively(sub, feature)
	}
}

// ResetRegistryForTesting clears both the middleware and feature registries.
// This should only be used in tests to avoid state leakage between test runs.
func ResetRegistryForTesting() {
	mu.Lock()
	globalMiddleware = nil
	featureMiddleware = make(map[props.FeatureCmd][]Middleware)
	sealed = false
	mu.Unlock()

	resetFeatureRegistry()
}
