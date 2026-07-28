package remove

import (
	"context"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type CommandOptions struct {
	Name   string
	Path   string
	Parent string
	Force  bool
}

func NewCmdCommand(p *props.Props) *cobra.Command {
	opts := CommandOptions{}

	cmd := &cobra.Command{
		Use:   "command",
		Short: "Remove a command from the project",
		Long: `Remove a command from the project: filesystem cleanup, manifest update,
and parent de-registration.

Examples:
  # Remove a command named 'test-command'
  gtb remove command --name test-command

  # Remove a subcommand 'child' under 'parent'
  gtb remove command --name child --parent parent

  # Force-remove a command marked protected in the manifest
  gtb remove command --name secret --force
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Command name (kebab-case)")
	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().StringVar(&opts.Parent, "parent", "root", "Parent command name (default: root)")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "Remove even if the command is marked protected")

	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func (o *CommandOptions) Run(ctx context.Context, p *props.Props) error {
	// A CLI-typed name or parent path is a hard error when invalid — the
	// removal would otherwise flow into FS.RemoveAll under pkg/cmd.
	if err := generator.ValidateCommandName(o.Name); err != nil {
		return err
	}

	if err := generator.ValidateParentPath(o.Parent); err != nil {
		return err
	}

	o.Path = icmd.ResolveProjectPath(p, o.Path)

	cfg := &generator.Config{
		Name:   o.Name,
		Path:   o.Path,
		Parent: o.Parent,
		Force:  o.Force,
	}

	return generator.New(p, cfg).Remove(ctx)
}
