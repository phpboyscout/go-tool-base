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
	Feature props.FeatureCmd
}

// Wrap pairs a cobra command with the feature it belongs to. The
// returned *Command embeds cmd, so it behaves as a cobra.Command for
// every method cobra offers; .Command exposes the underlying pointer
// when the cobra API needs *cobra.Command directly.
func Wrap(feature props.FeatureCmd, cmd *cobra.Command) *Command {
	return &Command{Command: cmd, Feature: feature}
}

// Register adds each child as a subcommand and wraps the child's RunE
// with the middleware [Chain] for the child's own feature.
//
// Each child is wrapped exactly once, at the point its parent registers
// it. A child's own descendants are wired when the child registers them,
// so Register never re-wraps a subtree (unlike the legacy recursive
// ApplyMiddlewareRecursively path, which re-applied the parent's
// feature down the tree).
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
