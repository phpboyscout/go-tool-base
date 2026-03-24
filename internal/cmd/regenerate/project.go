package regenerate

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/internal/generator"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

type ProjectOptions struct {
	Path       string
	Force      bool
	Overwrite  string
	UpdateDocs bool
}

func NewCmdProject(p *props.Props) *cobra.Command {
	opts := ProjectOptions{}

	cmd := &cobra.Command{
		Use:   "project",
		Short: "Regenerate project from manifest",
		Long: `Regenerate all command registration files (cmd.go) based on the manifest.yaml.
Does not overwrite implementation files (main.go) unless --force is provided.`,
		Run: func(cmd *cobra.Command, args []string) {
			p.ErrorHandler.Fatal(opts.Run(cmd.Context(), p))
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite existing main.go implementation files")
	cmd.Flags().StringVar(&opts.Overwrite, "overwrite", "ask", "How to handle file conflicts: allow, deny, or ask")
	cmd.Flags().BoolVar(&opts.UpdateDocs, "update-docs", false, "Use AI to update existing documentation")

	return cmd
}

func (o *ProjectOptions) Run(ctx context.Context, p *props.Props) error {
	if o.Overwrite == "" {
		o.Overwrite = "ask"
	}

	if o.Overwrite != "allow" && o.Overwrite != "deny" && o.Overwrite != "ask" {
		return errors.Wrapf(ErrInvalidOverwriteValue, "%q", o.Overwrite)
	}

	cfg := &generator.Config{
		Path:       o.Path,
		DryRun:     dryRun,
		Force:      o.Force,
		Overwrite:  o.Overwrite,
		UpdateDocs: o.UpdateDocs,
	}

	return generator.New(p, cfg).RegenerateProject(ctx)
}
