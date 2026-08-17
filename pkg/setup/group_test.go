package setup_test

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// GroupRunE is the whole behaviour of a pure command group, in one place, used
// by every generated cmd.go and by gtb's own groups. These assertions are the
// contract the generator's output depends on — spec 0190.

func groupCmd(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()

	out := &bytes.Buffer{}

	parent := &cobra.Command{Use: "config", RunE: setup.GroupRunE}
	parent.SetOut(out)
	parent.SetErr(out)
	parent.AddCommand(&cobra.Command{
		Use:  "get",
		RunE: func(*cobra.Command, []string) error { return nil },
	})

	root := &cobra.Command{Use: "tool"}
	root.SetOut(out)
	root.SetErr(out)
	root.AddCommand(parent)

	return root, out
}

func TestGroupRunE_BareInvocationPrintsUsageAndSucceeds(t *testing.T) {
	t.Parallel()

	root, out := groupCmd(t)
	root.SetArgs([]string{"config"})

	require.NoError(t, root.Execute(), "a bare group is a request for help, not a failure")
	assert.Contains(t, out.String(), "Usage:")
	assert.Contains(t, out.String(), "get", "and the usage names the verbs available")
}

func TestGroupRunE_UnknownVerbIsReported(t *testing.T) {
	t.Parallel()

	root, _ := groupCmd(t)
	root.SetArgs([]string{"config", "bogus"})

	err := root.Execute()

	require.Error(t, err, "cobra reports an unknown command for the root only, so a group must itself")
	require.ErrorIs(t, err, errorhandling.ErrUnknownSubCommand)
	assert.Contains(t, err.Error(), `unknown command "bogus" for "tool config"`,
		"the message names the verb and the command that rejected it")
}

func TestGroupRunE_UnknownVerbYieldsTheUsageExitCode(t *testing.T) {
	t.Parallel()

	root, _ := groupCmd(t)
	root.SetArgs([]string{"config", "bogus"})

	outcome, ok := errorhandling.OutcomeOf(root.Execute())

	require.True(t, ok, "the sentinel carries its own disposition through the wrap")
	assert.Equal(t, errorhandling.ExitCodeUsage, outcome.Code, "a mistyped verb is a usage error")
	assert.True(t, outcome.Usage, "and usage is the useful response to one")
}

func TestGroupRunE_KnownVerbStillRoutesToTheChild(t *testing.T) {
	t.Parallel()

	root, _ := groupCmd(t)
	root.SetArgs([]string{"config", "get"})

	require.NoError(t, root.Execute(), "wiring a group's RunE must not shadow its children")
}

// The generator emits a call to this function rather than a copy of its body, so
// nothing downstream re-states the wording. This pins the shape a generated
// cmd.go relies on: a plain cobra.RunE, assignable without a wrapper.
func TestGroupRunE_IsAssignableAsARunE(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "config", RunE: setup.GroupRunE}

	require.NotNil(t, cmd.RunE)
	assert.True(t, cmd.Runnable(),
		"a group has to be Runnable, or cobra returns flag.ErrHelp before Args is ever evaluated")
}

// Cobra's own unknown-command report is root-only, which is the reason this
// helper exists at all. Pinning it means a cobra upgrade that changed it would
// show up here rather than as a silently redundant check.
func TestCobraStillReportsUnknownCommandsForTheRootOnly(t *testing.T) {
	t.Parallel()

	root, _ := groupCmd(t)
	root.SetArgs([]string{"zzbogus"})

	err := root.Execute()

	require.Error(t, err, "the root does report an unknown command")
	require.NotErrorIs(t, err, errorhandling.ErrUnknownSubCommand,
		"and it is cobra's own error, not ours — the root is not in scope for 0190")
	assert.Contains(t, err.Error(), "unknown command")
}

func TestGroupRunE_WrapsRatherThanReplacingTheSentinel(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "config"}
	cmd.SetOut(&bytes.Buffer{})

	err := setup.GroupRunE(cmd, []string{"bogus"})

	require.ErrorIs(t, err, errorhandling.ErrUnknownSubCommand)
	assert.NotEmpty(t, errors.KindOf(err), "the error keeps a queryable kind")
}
