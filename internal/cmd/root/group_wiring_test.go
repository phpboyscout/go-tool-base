package root_test

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/root"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// Every command that only groups subcommands must wire a RunE — spec 0191.
//
// Cobra cannot express this any other way. A command with no Run or RunE returns
// flag.ErrHelp from execute() before ValidateArgs is reached, so Args is never
// consulted and `gtb generate zzbogus` answers a typo with help and exit 0; and
// cobra's own unknown-command report is produced for the root only. A group that
// wires nothing therefore cannot tell a user they mistyped.
//
// The nine call sites this spec fixed are a snapshot. The next group is written by
// somebody who has not read the spec, and the failure is silent — a missing RunE
// looks exactly like the eight groups that came before it. This walker is the part
// that keeps working after the edits are done.

// workingGroups are the commands with children that legitimately do work of their
// own, and so own their RunE rather than delegating to setup.GroupRunE.
//
// They are named individually, not pattern-matched: each is a judgement that this
// command takes arguments of its own, and a new group should have to be added here
// deliberately rather than qualify by accident.
var workingGroups = map[string]string{
	"gtb docs":             "opens the docs browser, or serves it",
	"gtb doctor":           "runs the checks and reports",
	"gtb enable":           "takes [feature...] with cobra.ArbitraryArgs",
	"gtb disable":          "takes [feature...] with cobra.ArbitraryArgs",
	"gtb generate command": "scaffolds a command; protect/unprotect are extras beside it",
}

// foreignGroups are groups gtb attaches but did not author. gtb does not own
// their semantics, and setting a RunE on someone else's command tree is a change
// their next release can silently undo — including by adding a subgroup this
// walker would then never see. They are named so the exemption is a stated
// ownership boundary rather than a hole.
//
// `keys` is NOT here. It comes from go/signing-cli, which is ours, so gtb wires
// the group default where it attaches it (see internal/cmd/root/root.go). That
// belongs upstream eventually, so sigillum's `keys` agrees with gtb's.
var foreignGroups = map[string]string{
	"gtb mcp":        "the ophis library builds this tree",
	"gtb mcp claude": "ophis",
	"gtb mcp cursor": "ophis",
	"gtb mcp vscode": "ophis",
}

func TestEveryCommandGroupWiresARunE(t *testing.T) {
	t.Parallel()

	rootCmd, _ := root.NewCmdRoot(ver.Info{Version: "test"})
	require.NotNil(t, rootCmd)

	var offenders []string

	walk(rootCmd.Command, func(cmd *cobra.Command) {
		if !cmd.HasSubCommands() || !cmd.HasParent() {
			return
		}

		if cmd.Runnable() {
			return
		}

		if _, foreign := foreignGroups[cmd.CommandPath()]; foreign {
			return
		}

		offenders = append(offenders, cmd.CommandPath())
	})

	assert.Empty(t, offenders,
		"a group with no RunE answers a mistyped subcommand with help and exit 0.\n"+
			"Wire `RunE: setup.GroupRunE` on: %s", strings.Join(offenders, ", "))
}

// The other half of the same property: a group that is Runnable should be running
// either the framework default or something of its own, and which one is a
// decision rather than an accident. This fails when a new group is added without
// either wiring GroupRunE or being declared as a working group above.
func TestEveryCommandGroupIsAccountedFor(t *testing.T) {
	t.Parallel()

	rootCmd, _ := root.NewCmdRoot(ver.Info{Version: "test"})

	var undeclared []string

	walk(rootCmd.Command, func(cmd *cobra.Command) {
		if !cmd.HasSubCommands() || !cmd.HasParent() || !cmd.Runnable() {
			return
		}

		if _, ok := workingGroups[cmd.CommandPath()]; ok {
			return
		}

		if _, foreign := foreignGroups[cmd.CommandPath()]; foreign {
			return
		}

		// A pure group's RunE is setup.GroupRunE. Comparing behaviour rather
		// than function pointers: a bare invocation of the framework default
		// prints usage and succeeds.
		if isGroupRunE(t, cmd) {
			return
		}

		undeclared = append(undeclared, cmd.CommandPath())
	})

	assert.Empty(t, undeclared,
		"each of these has a RunE that is neither setup.GroupRunE nor declared in\n"+
			"workingGroups. Either wire the framework default, or add it to that map\n"+
			"with a note saying what work it does: %s", strings.Join(undeclared, ", "))
}

// isGroupRunE reports whether a command's RunE behaves like setup.GroupRunE: an
// unknown verb is rejected by name.
func isGroupRunE(t *testing.T, cmd *cobra.Command) bool {
	t.Helper()

	err := cmd.RunE(cmd, []string{"zzbogus"})

	return err != nil && strings.Contains(err.Error(), `unknown command "zzbogus"`)
}

func walk(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)

	for _, child := range cmd.Commands() {
		walk(child, visit)
	}
}

// An exemption that stops matching anything is worse than no exemption: it reads
// as a covered case while covering nothing. If ophis renames or drops one of
// these, this fails and the entry should go.
func TestForeignGroupExemptionsStillMatchSomething(t *testing.T) {
	t.Parallel()

	rootCmd, _ := root.NewCmdRoot(ver.Info{Version: "test"})

	present := map[string]bool{}

	walk(rootCmd.Command, func(cmd *cobra.Command) {
		present[cmd.CommandPath()] = true
	})

	for path := range foreignGroups {
		assert.True(t, present[path],
			"%q is exempted but no longer exists — drop the entry", path)
	}

	for path := range workingGroups {
		assert.True(t, present[path],
			"%q is declared a working group but no longer exists — drop the entry", path)
	}
}
