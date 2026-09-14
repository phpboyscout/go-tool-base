package generate

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// SharedFlags carries the values of `generate`'s persistent flags to the
// subcommands that read them.
//
// These were package-level variables, which pflag wrote to directly. That made
// building two command trees a data race — harmless in the binary, which builds
// one in main, but it meant no test could construct the tree in parallel, and it
// is the pattern AGENTS.md forbids for exactly this reason. One allocation per
// NewCmdGenerate has neither problem.
type SharedFlags struct {
	AIProvider string
	AIModel    string
	DryRun     bool
}

// The accessors below tolerate a nil receiver so that a zero-value Options
// struct — which tests build directly, without going through a constructor —
// reads the same defaults the package-level variables used to give it, rather
// than panicking.

func (f *SharedFlags) dryRun() bool {
	return f != nil && f.DryRun
}

func (f *SharedFlags) aiProvider() string {
	if f == nil {
		return ""
	}

	return f.AIProvider
}

func (f *SharedFlags) aiModel() string {
	if f == nil {
		return ""
	}

	return f.AIModel
}

func NewCmdGenerate(p *props.Props) *setup.Command {
	shared := &SharedFlags{}

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Scaffold new projects or commands",
		Long: `Scaffold new GTB-based CLI projects or extend an existing one.

Subcommands cover the full scaffolding surface: "project" generates a fresh
project skeleton, "command" adds a command or subcommand, "add-flag" appends a
flag to an existing command, and "docs" writes command documentation.`,
		RunE: setup.GroupRunE,
	}

	cmd.PersistentFlags().StringVar(&shared.AIProvider, "provider", "", "AI provider to use (openai/gemini/claude)")
	cmd.PersistentFlags().StringVar(&shared.AIModel, "model", "", "AI model to use (defaults: claude-opus-4-8, gemini-3.5-flash, gpt-5.4)")
	cmd.PersistentFlags().BoolVar(&shared.DryRun, "dry-run", false, "preview changes without writing files")

	generateCmd := setup.Wrap("", cmd)
	generateCmd.Register(
		setup.Wrap("", NewCmdSkeleton(p, shared)),
		setup.Wrap("", NewCmdCommand(p, shared)),
		setup.Wrap("", NewCmdAddFlag(p)),
		setup.Wrap("", NewCmdDocs(p, shared)),
		setup.Wrap("", NewCmdMan(p, shared)),
	)

	return generateCmd
}
