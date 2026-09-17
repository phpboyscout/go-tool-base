package setup

import (
	"reflect"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errorhandling"
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
// The message itself comes from [errorhandling.UnknownSubCommand], so that CLIs
// in this estate that build cobra commands without this framework — go/signing-cli
// does, deliberately — report a mistyped verb in the same words. What is shared is
// the message and the sentinel; the three lines of cobra glue are not, and cannot
// be, since go/errorhandling imports no CLI framework.
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
		return errorhandling.UnknownSubCommand(args[0], cmd.CommandPath())
	}

	return cmd.Usage()
}

// GroupAnnotation marks a command whose RunE was GroupRunE before the root's
// middleware chain wrapped it, so IsGroup still answers afterwards.
const GroupAnnotation = "gtb.group"

// IsGroup reports whether cmd only groups subcommands, that is, its RunE is
// GroupRunE (or was, before the chain wrapped it). Cobra counts such a command
// as runnable, so anything publishing "runnable commands" (the MCP surface)
// asks this to leave groups out: a tool that prints usage is noise to a client.
func IsGroup(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	if cmd.Annotations[GroupAnnotation] == "true" {
		return true
	}

	return cmd.RunE != nil && reflect.ValueOf(cmd.RunE).Pointer() == reflect.ValueOf(GroupRunE).Pointer()
}

func markGroup(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}

	cmd.Annotations[GroupAnnotation] = "true"
}
