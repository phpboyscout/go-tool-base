package setup

import (
	"context"

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

// FeatureRegistry holds the registered initialisers, subcommands, flags, and checks for features.
type FeatureRegistry struct {
	initialisers map[props.FeatureCmd][]InitialiserProvider
	subcommands  map[props.FeatureCmd][]SubcommandProvider
	flags        map[props.FeatureCmd][]FeatureFlag
	checks       map[props.FeatureCmd][]CheckProvider
}

// globalRegistry is the package-level command registry. It is NOT safe for
// concurrent use. All Register* calls MUST happen during init() — before
// main() starts and before any goroutines are spawned. Reading from the
// registry (via Get*) is safe after init() completes because the
// Go memory model guarantees that init() happens-before main().
var globalRegistry = &FeatureRegistry{
	initialisers: make(map[props.FeatureCmd][]InitialiserProvider),
	subcommands:  make(map[props.FeatureCmd][]SubcommandProvider),
	flags:        make(map[props.FeatureCmd][]FeatureFlag),
	checks:       make(map[props.FeatureCmd][]CheckProvider),
}

// Register adds initialisers, subcommands, and flags for a specific feature.
func Register(feature props.FeatureCmd, ips []InitialiserProvider, sps []SubcommandProvider, fps []FeatureFlag) {
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
func RegisterChecks(feature props.FeatureCmd, cps []CheckProvider) {
	if cps != nil {
		globalRegistry.checks[feature] = append(globalRegistry.checks[feature], cps...)
	}
}

// GetInitialisers returns all registered initialiser providers.
func GetInitialisers() map[props.FeatureCmd][]InitialiserProvider {
	return globalRegistry.initialisers
}

// GetSubcommands returns all registered subcommand providers.
func GetSubcommands() map[props.FeatureCmd][]SubcommandProvider {
	return globalRegistry.subcommands
}

// GetFeatureFlags returns all registered feature flag providers.
func GetFeatureFlags() map[props.FeatureCmd][]FeatureFlag {
	return globalRegistry.flags
}

// GetChecks returns all registered check providers.
func GetChecks() map[props.FeatureCmd][]CheckProvider {
	return globalRegistry.checks
}
