package setup

import (
	"sync"

	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/props"
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

// AddCommandWithMiddleware adds a command to a parent and applies all registered
// middleware (global and feature-specific) to it and its subcommands.
func AddCommandWithMiddleware(parent, cmd *cobra.Command, feature props.FeatureCmd) {
	if cmd.RunE != nil {
		cmd.RunE = Chain(feature, cmd.RunE)
	}

	for _, sub := range cmd.Commands() {
		ApplyMiddlewareRecursively(sub, feature)
	}

	parent.AddCommand(cmd)
}

// ApplyMiddlewareRecursively applies middleware to a command and all of its
// children recursively. This is useful when adding a command tree that was
// built outside of the standard GTB registration flow.
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
