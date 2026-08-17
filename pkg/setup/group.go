package setup

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/errors"
)

// GroupRunE is the RunE for a command that only groups subcommands: it prints
// usage for a bare invocation and reports a verb the group does not have.
//
//	cmd := &cobra.Command{
//		Use:  "config",
//		RunE: setup.GroupRunE,
//	}
//
// A bare invocation is a request for help, so it prints usage and succeeds —
// cobra's Usage returns nil. A verb the group does not have is a mistake, so it
// is named and the command fails with [errorhandling.ErrUnknownSubCommand],
// whose outcome prints usage and yields exit code 2.
//
// # Why a group needs a RunE at all
//
// Cobra cannot express this through Args. A command with no Run or RunE returns
// flag.ErrHelp from execute() BEFORE ValidateArgs is reached, so Args is never
// evaluated and cobra.NoArgs on a group is silently inert. Cobra's own
// unknown-command report is no help either: legacyArgs only produces it for the
// root command (`!cmd.HasParent()`), so every group below the root answers a
// mistyped verb with help and success unless it checks for itself.
//
// # Why the generator emits a call to this rather than a copy of it
//
// A generated cmd.go is a wiring file that calls into this package for
// everything it does — Wrap, Register, the pre-run hooks. Emitting a copy of
// this body into every generated group would make behaviour the one thing that
// file states for itself, and would pin every project's group behaviour to the
// gtb build that scaffolded it. A reference to a published function cannot be
// sealed, deleted or drifted the way a same-package Run<Name> reference could —
// which is the failure mode that produced go-tool-base issues #21 and #22.
//
// See spec 0190.
func GroupRunE(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return errors.Wrapf(errorhandling.ErrUnknownSubCommand,
			"unknown command %q for %q", args[0], cmd.CommandPath())
	}

	return cmd.Usage()
}
