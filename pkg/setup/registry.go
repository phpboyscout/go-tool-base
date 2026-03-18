package setup

import (
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/props"
)

// InitialiserProvider is a function that creates an Initialiser.
type InitialiserProvider func(p *props.Props) Initialiser

// SubcommandProvider is a function that creates a slice of cobra subcommands.
type SubcommandProvider func(p *props.Props) []*cobra.Command

// FeatureFlag is a function that registers flags on a cobra command.
type FeatureFlag func(cmd *cobra.Command)

// FeatureRegistry holds the registered initialisers, subcommands, and flags for features.
type FeatureRegistry struct {
	initialisers map[props.FeatureCmd][]InitialiserProvider
	subcommands  map[props.FeatureCmd][]SubcommandProvider
	flags        map[props.FeatureCmd][]FeatureFlag
}

var globalRegistry = &FeatureRegistry{
	initialisers: make(map[props.FeatureCmd][]InitialiserProvider),
	subcommands:  make(map[props.FeatureCmd][]SubcommandProvider),
	flags:        make(map[props.FeatureCmd][]FeatureFlag),
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
