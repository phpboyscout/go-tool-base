package setup

import (
	"context"
	"io/fs"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
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
	// pipeline over that would be absurd, which is exactly what happened when
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

// The contribution slots GTB reads from the feature registry (spec 0199 D2).
// A package contributes under its feature's ID at init; the root and the
// commands read the contributions of enabled features.
const (
	SlotInitialiser features.Slot = "initialiser"
	SlotSubcommand  features.Slot = "subcommand"
	SlotInitFlag    features.Slot = "init-flag"
	SlotCheck       features.Slot = "check"
	SlotAssets      features.Slot = "assets"
	SlotMiddleware  features.Slot = "middleware"
)

// Register contributes initialisers, subcommands and init flags for a feature
// to the default registry. Nil slices contribute nothing.
func Register(feature props.FeatureID, ips []InitialiserProvider, sps []SubcommandProvider, fps []FeatureFlag) {
	RegisterOn(features.Default(), feature, ips, sps, fps)
}

// RegisterOn is Register against a caller's registry.
func RegisterOn(r features.Registry, feature props.FeatureID, ips []InitialiserProvider, sps []SubcommandProvider, fps []FeatureFlag) {
	for _, ip := range ips {
		r.Contribute(feature, SlotInitialiser, ip)
	}

	for _, sp := range sps {
		r.Contribute(feature, SlotSubcommand, sp)
	}

	for _, fp := range fps {
		r.Contribute(feature, SlotInitFlag, fp)
	}
}

// RegisterChecks contributes diagnostic check providers for a feature.
func RegisterChecks(feature props.FeatureID, cps []CheckProvider) {
	for _, cp := range cps {
		features.Default().Contribute(feature, SlotCheck, cp)
	}
}

// RegisterAssets contributes an embedded asset bundle for a feature. The root
// registers the bundles of enabled features onto props.Assets during
// construction, so a feature's assets/config.yaml (defaults) and
// assets/init/config.yaml (init template) participate in the merged reads only
// when the feature is enabled; see the segregated-default-config spec.
func RegisterAssets(feature props.FeatureID, name string, bundle fs.FS) {
	features.Default().Contribute(feature, SlotAssets, AssetBundle{Name: name, Bundle: bundle})
}

// GetInitialisers returns every registered initialiser provider by feature.
func GetInitialisers() map[props.FeatureID][]InitialiserProvider {
	return InitialisersIn(features.Default().Snapshot())
}

// InitialisersIn is GetInitialisers over a snapshot.
func InitialisersIn(s features.Snapshot) map[props.FeatureID][]InitialiserProvider {
	return contributionsBy[InitialiserProvider](s, SlotInitialiser)
}

// GetSubcommands returns every registered subcommand provider by feature.
func GetSubcommands() map[props.FeatureID][]SubcommandProvider {
	return SubcommandsIn(features.Default().Snapshot())
}

// SubcommandsIn is GetSubcommands over a snapshot.
func SubcommandsIn(s features.Snapshot) map[props.FeatureID][]SubcommandProvider {
	return contributionsBy[SubcommandProvider](s, SlotSubcommand)
}

// GetFeatureFlags returns every registered init-flag binder by feature.
func GetFeatureFlags() map[props.FeatureID][]FeatureFlag {
	return FeatureFlagsIn(features.Default().Snapshot())
}

// FeatureFlagsIn is GetFeatureFlags over a snapshot.
func FeatureFlagsIn(s features.Snapshot) map[props.FeatureID][]FeatureFlag {
	return contributionsBy[FeatureFlag](s, SlotInitFlag)
}

// GetAssets returns every registered asset bundle by feature.
func GetAssets() map[props.FeatureID][]AssetBundle {
	return AssetsIn(features.Default().Snapshot())
}

// AssetsIn is GetAssets over a snapshot.
func AssetsIn(s features.Snapshot) map[props.FeatureID][]AssetBundle {
	return contributionsBy[AssetBundle](s, SlotAssets)
}

// GetChecks returns every registered check provider by feature.
func GetChecks() map[props.FeatureID][]CheckProvider {
	return ChecksIn(features.Default().Snapshot())
}

// ChecksIn is GetChecks over a snapshot.
func ChecksIn(s features.Snapshot) map[props.FeatureID][]CheckProvider {
	return contributionsBy[CheckProvider](s, SlotCheck)
}

// contributionsBy collects one slot's contributions of type T across every
// contributing feature. A value of another type under the slot is somebody
// else's contribution and is skipped.
func contributionsBy[T any](s features.Snapshot, slot features.Slot) map[props.FeatureID][]T {
	out := map[props.FeatureID][]T{}

	for _, id := range s.Contributed() {
		for _, v := range s.Contributions(id, slot) {
			if typed, ok := v.(T); ok {
				out[id] = append(out[id], typed)
			}
		}
	}

	return out
}
