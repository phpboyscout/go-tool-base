package regenerate

import (
	"context"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type ManifestOptions struct {
	// shared carries `regenerate`'s persistent flags, injected by the
	// constructor rather than read from package state.
	shared *SharedFlags

	Path string
}

func NewCmdManifest(p *props.Props, shared *SharedFlags) *cobra.Command {
	opts := ManifestOptions{shared: shared}

	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Regenerate manifest from source code",
		Long: `Rebuild the project manifest from existing source code.

Scans the project's command definitions and rewrites manifest.yaml to match,
recovering the manifest when it has drifted from or is missing for the current
command tree.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")

	return cmd
}

func (o *ManifestOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	cfg := &generator.Config{
		Path:   o.Path,
		DryRun: o.shared.dryRun(),
	}

	return generator.New(p, cfg).RegenerateManifest(ctx)
}
