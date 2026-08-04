package root

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// execArgs builds a root command over props (plus any extra subcommands) and
// executes it with the given args, mirroring execChild but with a free-form
// argument list so cobra's own generated commands (help, completion,
// __complete) can be driven.
func execArgs(t *testing.T, props *p.Props, args []string, extra ...*setup.Command) error {
	t.Helper()

	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	rootCmd := NewCmdRoot(props, extra...)
	rootCmd.SetArgs(args)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)

	return rootCmd.ExecuteContext(context.Background())
}

// TestPreRun_FreshInstall_HelpRuns: on a fresh install (no config file, init
// enabled) `tool help` must print usage and exit cleanly instead of failing
// the missing-config gate.
func TestPreRun_FreshInstall_HelpRuns(t *testing.T) {
	props := noConfigProps(t, "fresh-help-tool")

	err := execArgs(t, props, []string{"help"})
	require.NoError(t, err, "'tool help' must succeed on a fresh install with no config file")
}

// TestPreRun_FreshInstall_CompletionRuns: `tool completion bash` must emit the
// completion script on a fresh install rather than erroring on missing config.
func TestPreRun_FreshInstall_CompletionRuns(t *testing.T) {
	props := noConfigProps(t, "fresh-completion-tool")

	err := execArgs(t, props, []string{"completion", "bash"})
	require.NoError(t, err, "'tool completion bash' must succeed on a fresh install with no config file")
}

// TestPreRun_FreshInstall_CompleteRequestRuns: the hidden __complete command
// cobra generates for shell tab-completion must not error on a fresh install —
// it fires on every completion keystroke.
func TestPreRun_FreshInstall_CompleteRequestRuns(t *testing.T) {
	props := noConfigProps(t, "fresh-complete-tool")

	err := execArgs(t, props, []string{cobra.ShellCompRequestCmd, "ver"})
	require.NoError(t, err, "'tool __complete' must succeed on a fresh install with no config file")
}

// TestPreRun_FreshInstall_InitSubtreeRuns: a provider subcommand registered
// under an init-feature command (e.g. `init github`) must run on a configless
// machine — it is the command tree that exists to fix that state. The subtree
// is identified by walking up to the nearest InitCmd feature annotation, so a
// synthetic tree reproduces the mechanism without the real wizards.
func TestPreRun_FreshInstall_InitSubtreeRuns(t *testing.T) {
	props := noConfigProps(t, "fresh-initsub-tool")

	var providerRan bool

	parent := setup.Wrap(p.InitCmd, &cobra.Command{Use: "myinit"})
	parent.Register(setup.Wrap("providerfeature", &cobra.Command{
		Use: "provider",
		RunE: func(_ *cobra.Command, _ []string) error {
			providerRan = true

			return nil
		},
	}))

	err := execArgs(t, props, []string{"myinit", "provider"}, parent)
	require.NoError(t, err, "an init-subtree provider subcommand must run with no config file")
	assert.True(t, providerRan, "provider subcommand must actually execute")
}

// TestPreRun_FreshInstall_MissingConfigErrorHasInitHint: a non-exempt command
// still hard-fails on missing config, but the error must now carry a hint
// telling the user to run '<tool> init'.
func TestPreRun_FreshInstall_MissingConfigErrorHasInitHint(t *testing.T) {
	props := noConfigProps(t, "fresh-hint-tool")

	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error { return nil }}

	err := execArgs(t, props, []string{"child"}, setup.Wrap("", child))
	require.Error(t, err, "missing config with init enabled must still hard-fail for normal commands")

	hints := errors.GetAllHints(err)
	require.NotEmpty(t, hints, "missing-config error must carry a user-facing hint")
	assert.Contains(t, hints[0], "fresh-hint-tool init",
		"hint must name the tool's init command")
}

// TestPreRun_Auxiliary_SkipsBootstrapWithConfigPresent: even when a config
// file exists, completion must take the fast path — no config store is built,
// so no telemetry consent, collector wiring, or update check can occur.
func TestPreRun_Auxiliary_SkipsBootstrapWithConfigPresent(t *testing.T) {
	props := noConfigProps(t, "cfg-completion-tool")

	cfgPath := filepath.Join(setup.GetDefaultConfigDir(props.FS, "cfg-completion-tool"), setup.DefaultConfigFilename)
	require.NoError(t, props.FS.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, afero.WriteFile(props.FS, cfgPath, []byte("log:\n  level: info\n"), 0o644))

	err := execArgs(t, props, []string{"completion", "bash"})
	require.NoError(t, err)
	assert.Nil(t, props.Config,
		"completion must skip the framework bootstrap entirely (no config store built)")
}

// TestPreRun_AuxiliarySkipList_FastPath: a downstream tool can extend the
// auxiliary fast-path set via Tool.Bootstrap.AuxiliaryCommands without a
// framework release. Unlike SkipConfigCheck's tolerant load, the fast path
// skips the bootstrap entirely — props.Config stays nil.
func TestPreRun_AuxiliarySkipList_FastPath(t *testing.T) {
	props := noConfigProps(t, "auxlist-tool")
	props.Tool.Bootstrap = p.BootstrapPolicy{AuxiliaryCommands: []string{"plumbing"}}

	var ran bool

	child := setup.Wrap("", &cobra.Command{
		Use: "plumbing",
		RunE: func(_ *cobra.Command, _ []string) error {
			ran = true

			return nil
		},
	})

	err := execArgs(t, props, []string{"plumbing"}, child)
	require.NoError(t, err, "a listed auxiliary command must run with no config file")
	assert.True(t, ran)
	assert.Nil(t, props.Config, "auxiliary fast path must skip the bootstrap entirely")
}

// TestPreRun_DownstreamCompletionCommand_GetsNormalBootstrap: a downstream
// tool's own feature-wrapped command that happens to be named "completion" is
// NOT cobra's generated command and must get the normal bootstrap — on a fresh
// install that means the missing-config hard fail, not a silent fast path.
func TestPreRun_DownstreamCompletionCommand_GetsNormalBootstrap(t *testing.T) {
	props := noConfigProps(t, "downstream-completion-tool")

	var ran bool

	own := setup.Wrap("somefeature", &cobra.Command{
		Use: "completion",
		RunE: func(_ *cobra.Command, _ []string) error {
			ran = true

			return nil
		},
	})

	err := execArgs(t, props, []string{"completion"}, own)
	require.Error(t, err, "a downstream feature command named 'completion' must get the normal bootstrap")
	assert.False(t, ran, "downstream 'completion' must not run when bootstrap hard-fails")
}
