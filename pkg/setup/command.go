package setup

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Command composes [cobra.Command] with the middleware feature key it
// belongs to. The feature is the lookup key [Chain] uses to find
// feature-specific middleware (registered via [RegisterMiddleware]).
//
// Composing rather than wrapping means callers can use any cobra.Command
// method directly (the embedded pointer satisfies the interface), and
// code that needs the raw *cobra.Command — e.g. to pass to a cobra API
// or store in a parent's Commands() slice — accesses it via .Command.
//
// Commands are typically built via [Wrap] in each generated NewCmd<Name>
// constructor and attached to a parent via the parent's [Command.Register]
// method, which wires middleware automatically. See the
// `2026-05-30-command-composition-registration` spec.
type Command struct {
	*cobra.Command

	// Feature is the middleware lookup key. The empty string means "no
	// feature-specific middleware" (global middleware still applies).
	Feature props.FeatureID
}

// FeatureAnnotation is the cobra.Command.Annotations key under which [Wrap]
// records the feature a command belongs to. Code that only has the raw
// *cobra.Command (e.g. a PersistentPreRunE hook) can identify the command by
// feature via [FeatureOf] instead of matching the fragile Use string.
const FeatureAnnotation = "gtb.feature"

// Wrap pairs a cobra command with the feature it belongs to. The
// returned *Command embeds cmd, so it behaves as a cobra.Command for
// every method cobra offers; .Command exposes the underlying pointer
// when the cobra API needs *cobra.Command directly.
//
// Wrap also stamps the feature onto the underlying command's Annotations
// (under [FeatureAnnotation]) so the feature is recoverable from the raw
// *cobra.Command via [FeatureOf] — even where only cobra's own type is in hand.
func Wrap(feature props.FeatureID, cmd *cobra.Command) *Command {
	if cmd != nil && feature != "" {
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}

		cmd.Annotations[FeatureAnnotation] = string(feature)
	}

	return &Command{Command: cmd, Feature: feature}
}

// FeatureOf returns the feature a command was wrapped with via [Wrap], or the
// empty FeatureID when the command carries no feature annotation. It works on
// the raw *cobra.Command, so it is usable from hooks that never see the
// composing *Command.
func FeatureOf(cmd *cobra.Command) props.FeatureID {
	if cmd == nil || cmd.Annotations == nil {
		return ""
	}

	return props.FeatureID(cmd.Annotations[FeatureAnnotation])
}

// SkipConfigCheckAnnotation is the cobra.Command.Annotations key under which
// [SkipConfigCheck] records that a command's missing-config gate should be
// relaxed to a tolerant load by the root pre-run. It is the rename-safe
// counterpart to the string list in [props.BootstrapPolicy.SkipConfigCheck].
const SkipConfigCheckAnnotation = "gtb.skip_config_check"

// SkipConfigCheck stamps cmd so the root pre-run relaxes its missing-config
// gate: when no config file exists, the framework falls back to the embedded
// default config rather than erroring, letting cmd's own PreRunE own bootstrap.
// It returns cmd for chaining, mirroring [Wrap]. A nil cmd is returned
// unchanged.
//
// Bootstrap itself still runs — only the missing-config outcome is relaxed —
// so props.Config is always populated (see the
// 2026-06-12-bootstrap-prerun-traversal invariant).
func SkipConfigCheck(cmd *cobra.Command) *cobra.Command {
	if cmd == nil {
		return cmd
	}

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}

	cmd.Annotations[SkipConfigCheckAnnotation] = "true"

	return cmd
}

// SkipsConfigCheck reports whether cmd carries the [SkipConfigCheck] annotation.
// It works on the raw *cobra.Command so it is usable from the root pre-run hook.
func SkipsConfigCheck(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Annotations == nil {
		return false
	}

	return cmd.Annotations[SkipConfigCheckAnnotation] == "true"
}

// SkipUpdateCheckAnnotation is the cobra.Command.Annotations key under which
// [MarkSkipUpdateCheck] records that the pre-run update check must never run
// for a command (or its subtree). It is the rename-safe counterpart to the
// old Use-string skip list inside [SkipUpdateCheck], mirroring
// [SkipConfigCheckAnnotation].
const SkipUpdateCheckAnnotation = "gtb.skip_update_check"

// MarkSkipUpdateCheck stamps cmd so [SkipUpdateCheck] exempts it (and, via the
// parent-chain walk, its whole subtree) from the pre-run update check. Used
// for commands where a version check is redundant or harmful: version prints
// its own check, mcp must not write spinner output to a protocol stdout, and
// doctor diagnoses the install it has. It returns cmd for chaining, mirroring
// [SkipConfigCheck]. A nil cmd is returned unchanged.
func MarkSkipUpdateCheck(cmd *cobra.Command) *cobra.Command {
	if cmd == nil {
		return cmd
	}

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}

	cmd.Annotations[SkipUpdateCheckAnnotation] = "true"

	return cmd
}

// SkipsUpdateCheck reports whether cmd itself carries the
// [MarkSkipUpdateCheck] annotation. Subtree semantics (a stamped parent
// covering its children) are applied by [SkipUpdateCheck]'s parent-chain walk,
// not here.
func SkipsUpdateCheck(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Annotations == nil {
		return false
	}

	return cmd.Annotations[SkipUpdateCheckAnnotation] == "true"
}

// Register adds each child as a subcommand and wraps the child's RunE
// with the middleware [Chain] for the child's own feature.
//
// Each child is wrapped exactly once, at the point its parent registers
// it. A child's own descendants are wired when the child registers them,
// so Register never re-wraps a subtree.
//
// Children with a nil RunE (pure command groups) are still attached but
// receive no RunE-wrapping — there is nothing to wrap.
func (c *Command) Register(children ...*Command) {
	for _, child := range children {
		if child.Command == nil {
			continue
		}

		if child.RunE != nil {
			child.RunE = Chain(child.Feature, child.RunE)
		}

		c.AddCommand(child.Command)
	}
}
