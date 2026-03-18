package generate

import (
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/props"
)

var (
	aiProvider string
	aiModel    string
)

func NewCmdGenerate(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Scaffold new projects or commands",
		Long:  `Scaffold new projects (skeletons) or add new commands to existing gtb projects.`,
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Usage()
		},
	}

	cmd.PersistentFlags().StringVar(&aiProvider, "provider", "", "AI provider to use (openai/gemini/claude)")
	cmd.PersistentFlags().StringVar(&aiModel, "model", "", "AI model to use (defaults: gemini-3-flash-preview, claude-sonnet-4-5)")

	cmd.AddCommand(NewCmdSkeleton(p))
	cmd.AddCommand(NewCmdCommand(p))
	cmd.AddCommand(NewCmdAddFlag(p))
	cmd.AddCommand(NewCmdDocs(p))

	return cmd
}
