package setup

import (
	"context"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// InitialiserProvider creates an Initialiser for one init run, reading the
// run's flags (the ones a FeatureFlag bound on the init command) rather than
// package state, so two roots never share a flag target (spec 0199 D3, #37).
// It returns nil when its feature asks to be skipped.
type InitialiserProvider func(p *props.Props, flags *pflag.FlagSet) Initialiser

// SubcommandProvider is a function that creates a slice of cobra subcommands.
type SubcommandProvider func(p *props.Props) []*cobra.Command

// FeatureFlag is a function that registers flags on a cobra command.
type FeatureFlag func(cmd *cobra.Command)

// RootCommandProvider builds a top-level command for the root, or returns nil
// when its feature has nothing to add for this tool. The root registers what
// it returns and stamps it with the contributing feature when it carries no
// stamp of its own (spec 0202 D2).
type RootCommandProvider func(p *props.Props) *Command

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
	SlotRootCommand features.Slot = "root-command"
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

// RegisterRootCommands contributes top-level commands for a feature to the
// default registry. A linked package (pkg/mcp) calls it from init so the root
// need not name the package.
func RegisterRootCommands(feature props.FeatureID, providers ...RootCommandProvider) {
	RegisterRootCommandsOn(features.Default(), feature, providers...)
}

// RegisterRootCommandsOn is RegisterRootCommands against a caller's registry.
// Nil providers contribute nothing.
func RegisterRootCommandsOn(r features.Registry, feature props.FeatureID, providers ...RootCommandProvider) {
	for _, rp := range providers {
		if rp != nil {
			r.Contribute(feature, SlotRootCommand, rp)
		}
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

// InitialisersIn is GetInitialisers over a snapshot.
func InitialisersIn(s features.Snapshot) map[props.FeatureID][]InitialiserProvider {
	return contributionsBy[InitialiserProvider](s, SlotInitialiser)
}

// SubcommandsIn is GetSubcommands over a snapshot.
func SubcommandsIn(s features.Snapshot) map[props.FeatureID][]SubcommandProvider {
	return contributionsBy[SubcommandProvider](s, SlotSubcommand)
}

// FeatureFlagsIn is GetFeatureFlags over a snapshot.
func FeatureFlagsIn(s features.Snapshot) map[props.FeatureID][]FeatureFlag {
	return contributionsBy[FeatureFlag](s, SlotInitFlag)
}

// AssetsIn is GetAssets over a snapshot.
func AssetsIn(s features.Snapshot) map[props.FeatureID][]AssetBundle {
	return contributionsBy[AssetBundle](s, SlotAssets)
}

// RootCommandsIn is the root-command providers of every contributing feature.
func RootCommandsIn(s features.Snapshot) map[props.FeatureID][]RootCommandProvider {
	return contributionsBy[RootCommandProvider](s, SlotRootCommand)
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

// SkipKeyFlag is the init flag every profile that offers an SSH key honours.
// The init command binds it; a provider reads it through FlagSkips.
const SkipKeyFlag = "skip-key"

// FlagSkips reads a set of boolean init flags by name. A flag that was not
// bound (its feature is off) reads as false, so a provider can ask for a flag
// another feature owns without caring whether that feature is linked.
func FlagSkips(flags *pflag.FlagSet, names ...string) map[string]bool {
	out := make(map[string]bool, len(names))

	for _, name := range names {
		if flags == nil {
			continue
		}

		if v, err := flags.GetBool(name); err == nil {
			out[name] = v
		}
	}

	return out
}

// CIDefault is the default an init skip flag takes: on under CI, where nobody
// can answer a wizard.
func CIDefault() bool { return os.Getenv("CI") == "true" }
