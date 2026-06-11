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
		Long:  `Scaffold new projects (skeletons) or add new commands to existing gtb projects.`,
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Usage()
		},
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
	)

	return generateCmd
}
