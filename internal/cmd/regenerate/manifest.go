package regenerate

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/internal/generator"
	"github.com/phpboyscout/gtb/pkg/props"
)

type ManifestOptions struct {
	Path string
}

func NewCmdManifest(p *props.Props) *cobra.Command {
	opts := ManifestOptions{}

	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Regenerate manifest from source code",
		Long:  `Scan the project for cobra.Command definitions and rebuild the manifest.yaml file.`,
		Run: func(cmd *cobra.Command, args []string) {
			p.ErrorHandler.Fatal(opts.Run(cmd.Context(), p))
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return cmd
}

func (o *ManifestOptions) Run(ctx context.Context, p *props.Props) error {
	cfg := &generator.Config{
		Path: o.Path,
	}

	return generator.New(p, cfg).RegenerateManifest(ctx)
}
