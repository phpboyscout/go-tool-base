package generate

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

var (
	aiProvider string
	aiModel    string
	dryRun     bool
)

func NewCmdGenerate(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Scaffold new projects or commands",
		Long: `Scaffold new GTB-based CLI projects or extend an existing one.

Subcommands cover the full scaffolding surface: "project" generates a fresh
project skeleton, "command" adds a command or subcommand, "add-flag" appends a
flag to an existing command, and "docs" writes command documentation.`,
		RunE: setup.GroupRunE,
	}

	cmd.PersistentFlags().StringVar(&aiProvider, "provider", "", "AI provider to use (openai/gemini/claude)")
	cmd.PersistentFlags().StringVar(&aiModel, "model", "", "AI model to use (defaults: claude-opus-4-8, gemini-3.5-flash, gpt-5.4)")
	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview changes without writing files")

	generateCmd := setup.Wrap("", cmd)
	generateCmd.Register(
		setup.Wrap("", NewCmdSkeleton(p)),
		setup.Wrap("", NewCmdCommand(p)),
		setup.Wrap("", NewCmdAddFlag(p)),
		setup.Wrap("", NewCmdDocs(p)),
		setup.Wrap("", NewCmdMan(p)),
	)

	return generateCmd
}
