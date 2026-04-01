package remove

import (
	"context"

	"github.com/spf13/cobra"

	icmd "github.com/phpboyscout/go-tool-base/internal/cmd"
	"github.com/phpboyscout/go-tool-base/internal/generator"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

type CommandOptions struct {
	Name   string
	Path   string
	Parent string
}

func NewCmdCommand(p *props.Props) *cobra.Command {
	opts := CommandOptions{}

	cmd := &cobra.Command{
		Use:   "command",
		Short: "Remove a command from the project",
		Long: `Remove a command from the project, including filesystem cleanup, manifest update, and parent de-registration.

Examples:
  # Remove a command named 'test-command'
  gtb remove command --name test-command

  # Remove a subcommand 'child' under 'parent'
  gtb remove command --name child --parent parent
`,
		Run: func(cmd *cobra.Command, args []string) {
			p.ErrorHandler.Fatal(opts.Run(cmd.Context(), p))
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Command name (kebab-case)")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().StringVar(&opts.Parent, "parent", "root", "Parent command name (default: root)")

	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func (o *CommandOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	cfg := &generator.Config{
		Name:   o.Name,
		Path:   o.Path,
		Parent: o.Parent,
	}

	return generator.New(p, cfg).Remove(ctx)
}
