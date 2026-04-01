package regenerate

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	icmd "github.com/phpboyscout/go-tool-base/internal/cmd"
	"github.com/phpboyscout/go-tool-base/internal/generator"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

type ProjectOptions struct {
	Path            string
	Force           bool
	Overwrite       string
	UpdateDocs      bool
	WrapSubcommands *bool
}

func NewCmdProject(p *props.Props) *cobra.Command {
	opts := ProjectOptions{}

	var wrapSubcommandsFlag bool

	cmd := &cobra.Command{
		Use:   "project",
		Short: "Regenerate project from manifest",
		Long: `Regenerate all command registration files (cmd.go) based on the manifest.yaml.
Does not overwrite implementation files (main.go) unless --force is provided.`,
		Run: func(cmd *cobra.Command, args []string) {
			if cmd.Flags().Changed("wrap-subcommands") {
				opts.WrapSubcommands = &wrapSubcommandsFlag
			}

			p.ErrorHandler.Fatal(opts.Run(cmd.Context(), p))
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite existing main.go implementation files")
	cmd.Flags().StringVar(&opts.Overwrite, "overwrite", "ask", "How to handle file conflicts: allow, deny, or ask")
	cmd.Flags().BoolVar(&opts.UpdateDocs, "update-docs", false, "Use AI to update existing documentation")
	cmd.Flags().BoolVar(&wrapSubcommandsFlag, "wrap-subcommands", true, "Automatically wrap subcommands with middleware")

	return cmd
}

func (o *ProjectOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	if o.Overwrite == "" {
		o.Overwrite = "ask"
	}

	if o.Overwrite != "allow" && o.Overwrite != "deny" && o.Overwrite != "ask" {
		return errors.Wrapf(ErrInvalidOverwriteValue, "%q", o.Overwrite)
	}

	cfg := &generator.Config{
		Path:                          o.Path,
		DryRun:                        dryRun,
		Force:                         o.Force,
		Overwrite:                     o.Overwrite,
		UpdateDocs:                    o.UpdateDocs,
		WrapSubcommandsWithMiddleware: o.WrapSubcommands,
	}

	return generator.New(p, cfg).RegenerateProject(ctx)
}
