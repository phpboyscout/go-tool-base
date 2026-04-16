package setup

import (
	"context"
	"sync"

	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// InitialiserProvider is a function that creates an Initialiser.
type InitialiserProvider func(p *props.Props) Initialiser

// SubcommandProvider is a function that creates a slice of cobra subcommands.
type SubcommandProvider func(p *props.Props) []*cobra.Command

// FeatureFlag is a function that registers flags on a cobra command.
type FeatureFlag func(cmd *cobra.Command)

// CheckResult represents the outcome of a single diagnostic check.
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// CheckFunc is the signature for individual diagnostic checks.
type CheckFunc func(ctx context.Context, props *props.Props) CheckResult

// CheckProvider is a function that returns diagnostic checks for a feature.
type CheckProvider func(p *props.Props) []CheckFunc

// FeatureRegistry holds the registered initialisers, subcommands, flags, and
// checks for features. All access is serialised by registryMu so concurrent
// init() calls and parallel tests are race-free.
type FeatureRegistry struct {
	initialisers map[props.FeatureCmd][]InitialiserProvider
	subcommands  map[props.FeatureCmd][]SubcommandProvider
	flags        map[props.FeatureCmd][]FeatureFlag
	checks       map[props.FeatureCmd][]CheckProvider
}

// registryMu protects globalRegistry and registrySealed. Acquired for write
// by all Register* and Reset/Seal helpers; acquired for read by all Get*
// accessors. The mutex is required for memory visibility of registrySealed
// across goroutines, not only mutual exclusion on the maps — see
// docs/development/specs/2026-04-15-test-race-remediation.md.
var (
	registryMu     sync.RWMutex
	registrySealed bool
)

var globalRegistry = &FeatureRegistry{
	initialisers: make(map[props.FeatureCmd][]InitialiserProvider),
	subcommands:  make(map[props.FeatureCmd][]SubcommandProvider),
	flags:        make(map[props.FeatureCmd][]FeatureFlag),
	checks:       make(map[props.FeatureCmd][]CheckProvider),
}

// SealRegistry prevents further feature registration. Called after all
// commands have been registered. Subsequent Register* calls will panic.
func SealRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()

	registrySealed = true
}

// Register adds initialisers, subcommands, and flags for a specific feature.
// Panics if the registry has been sealed.
func Register(feature props.FeatureCmd, ips []InitialiserProvider, sps []SubcommandProvider, fps []FeatureFlag) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if registrySealed {
		panic("cannot register feature providers after the registry has been sealed")
	}

	if ips != nil {
		globalRegistry.initialisers[feature] = append(globalRegistry.initialisers[feature], ips...)
	}

	if sps != nil {
		globalRegistry.subcommands[feature] = append(globalRegistry.subcommands[feature], sps...)
	}

	if fps != nil {
		globalRegistry.flags[feature] = append(globalRegistry.flags[feature], fps...)
	}
}

// RegisterChecks adds diagnostic check providers for a specific feature.
// Panics if the registry has been sealed.
func RegisterChecks(feature props.FeatureCmd, cps []CheckProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if registrySealed {
		panic("cannot register checks after the registry has been sealed")
	}

	if cps != nil {
		globalRegistry.checks[feature] = append(globalRegistry.checks[feature], cps...)
	}
}

// GetInitialisers returns all registered initialiser providers.
func GetInitialisers() map[props.FeatureCmd][]InitialiserProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	return globalRegistry.initialisers
}

// GetSubcommands returns all registered subcommand providers.
func GetSubcommands() map[props.FeatureCmd][]SubcommandProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	return globalRegistry.subcommands
}

// GetFeatureFlags returns all registered feature flag providers.
func GetFeatureFlags() map[props.FeatureCmd][]FeatureFlag {
	registryMu.RLock()
	defer registryMu.RUnlock()

	return globalRegistry.flags
}

// GetChecks returns all registered check providers.
func GetChecks() map[props.FeatureCmd][]CheckProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	return globalRegistry.checks
}

// resetFeatureRegistry clears the feature registry under registryMu.
// Internal helper called from ResetRegistryForTesting (in middleware.go) so
// a single reset call clears both middleware and feature state — preserving
// the existing one-call API surface used across the codebase's tests.
func resetFeatureRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()

	globalRegistry = &FeatureRegistry{
		initialisers: make(map[props.FeatureCmd][]InitialiserProvider),
		subcommands:  make(map[props.FeatureCmd][]SubcommandProvider),
		flags:        make(map[props.FeatureCmd][]FeatureFlag),
		checks:       make(map[props.FeatureCmd][]CheckProvider),
	}
	registrySealed = false
}
