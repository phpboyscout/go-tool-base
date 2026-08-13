package setup

import (
	"context"
	"io/fs"
	"maps"
	"sync"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
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
	// Gating marks a WARN that is a policy violation rather than an advisory,
	// so it can fail a run under a warn threshold.
	//
	// Most warnings are advice: "no AI provider API keys configured" is a
	// perfectly good state for a tool that does not use AI, and failing its
	// pipeline over that would be absurd — which is exactly what happened when
	// the exit code keyed on severity alone. A warning gates only when the
	// check says the condition is a policy failure, and a check author has to
	// opt in deliberately.
	//
	// It has no effect on a pass, a skip, or a fail: a failed check gates
	// regardless, because there is nothing advisory about one.
	Gating bool `json:"gating,omitempty"`
}

// CheckFunc is the signature for individual diagnostic checks.
type CheckFunc func(ctx context.Context, props *props.Props) CheckResult

// CheckProvider is a function that returns diagnostic checks for a feature.
type CheckProvider func(p *props.Props) []CheckFunc

// AssetBundle names an embedded asset bundle a feature contributes to
// props.Assets when the feature is enabled.
type AssetBundle struct {
	Name   string
	Bundle fs.FS
}

// FeatureRegistry holds the registered initialisers, subcommands, flags, and
// checks for features. All access is serialised by registryMu so concurrent
// init() calls and parallel tests are race-free.
type FeatureRegistry struct {
	initialisers map[props.FeatureID][]InitialiserProvider
	subcommands  map[props.FeatureID][]SubcommandProvider
	flags        map[props.FeatureID][]FeatureFlag
	checks       map[props.FeatureID][]CheckProvider
	assets       map[props.FeatureID][]AssetBundle
}

// registryMu protects globalRegistry and registrySealed. Acquired for write
// by all Register* and Reset/Seal helpers; acquired for read by all Get*
// accessors. The mutex is required for memory visibility of registrySealed
// across goroutines, not only mutual exclusion on the maps — see
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0058-test-race-remediation.
var (
	registryMu     sync.RWMutex
	registrySealed bool
)

var globalRegistry = &FeatureRegistry{
	initialisers: make(map[props.FeatureID][]InitialiserProvider),
	subcommands:  make(map[props.FeatureID][]SubcommandProvider),
	flags:        make(map[props.FeatureID][]FeatureFlag),
	checks:       make(map[props.FeatureID][]CheckProvider),
	assets:       make(map[props.FeatureID][]AssetBundle),
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
func Register(feature props.FeatureID, ips []InitialiserProvider, sps []SubcommandProvider, fps []FeatureFlag) {
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
func RegisterChecks(feature props.FeatureID, cps []CheckProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if registrySealed {
		panic("cannot register checks after the registry has been sealed")
	}

	if cps != nil {
		globalRegistry.checks[feature] = append(globalRegistry.checks[feature], cps...)
	}
}

// GetInitialisers returns a snapshot of all registered initialiser providers.
func GetInitialisers() map[props.FeatureID][]InitialiserProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cp := make(map[props.FeatureID][]InitialiserProvider, len(globalRegistry.initialisers))
	maps.Copy(cp, globalRegistry.initialisers)

	return cp
}

// GetSubcommands returns a snapshot of all registered subcommand providers.
func GetSubcommands() map[props.FeatureID][]SubcommandProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cp := make(map[props.FeatureID][]SubcommandProvider, len(globalRegistry.subcommands))
	maps.Copy(cp, globalRegistry.subcommands)

	return cp
}

// GetFeatureFlags returns a snapshot of all registered feature flag providers.
func GetFeatureFlags() map[props.FeatureID][]FeatureFlag {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cp := make(map[props.FeatureID][]FeatureFlag, len(globalRegistry.flags))
	maps.Copy(cp, globalRegistry.flags)

	return cp
}

// RegisterAssets adds an embedded asset bundle for a feature. The root command
// registers the bundles of enabled features onto props.Assets during
// construction, so a feature's assets/config.yaml (defaults) and
// assets/init/config.yaml (init template) participate in the merged reads only
// when the feature is enabled — see the segregated-default-config spec.
// Panics if the registry has been sealed.
func RegisterAssets(feature props.FeatureID, name string, bundle fs.FS) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if registrySealed {
		panic("cannot register assets after the registry has been sealed")
	}

	globalRegistry.assets[feature] = append(globalRegistry.assets[feature], AssetBundle{Name: name, Bundle: bundle})
}

// GetAssets returns a snapshot of all registered asset bundles.
func GetAssets() map[props.FeatureID][]AssetBundle {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cp := make(map[props.FeatureID][]AssetBundle, len(globalRegistry.assets))
	maps.Copy(cp, globalRegistry.assets)

	return cp
}

// GetChecks returns a snapshot of all registered check providers.
func GetChecks() map[props.FeatureID][]CheckProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cp := make(map[props.FeatureID][]CheckProvider, len(globalRegistry.checks))
	maps.Copy(cp, globalRegistry.checks)

	return cp
}

// resetFeatureRegistry clears the feature registry under registryMu.
// Internal helper called from ResetRegistryForTesting (in middleware.go) so
// a single reset call clears both middleware and feature state — preserving
// the existing one-call API surface used across the codebase's tests.
func resetFeatureRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()

	globalRegistry = &FeatureRegistry{
		initialisers: make(map[props.FeatureID][]InitialiserProvider),
		subcommands:  make(map[props.FeatureID][]SubcommandProvider),
		flags:        make(map[props.FeatureID][]FeatureFlag),
		checks:       make(map[props.FeatureID][]CheckProvider),
		assets:       make(map[props.FeatureID][]AssetBundle),
	}
	registrySealed = false
}
