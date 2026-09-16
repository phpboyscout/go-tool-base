package regenerate

import (
	"context"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type ProjectOptions struct {
	// shared carries `regenerate`'s persistent flags, injected by the
	// constructor rather than read from package state.
	shared *SharedFlags

	Path       string
	Force      bool
	Overwrite  string
	UpdateDocs bool
	NoVerify   bool
}

func NewCmdProject(p *props.Props, shared *SharedFlags) *cobra.Command {
	opts := ProjectOptions{shared: shared}

	cmd := &cobra.Command{
		Use:   "project",
		Short: "Regenerate project from manifest",
		Long: `Regenerate all command registration files (cmd.go) based on the manifest.yaml.
Does not overwrite implementation files (main.go) unless --force is provided.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.Path, "path", "p", ".", "Path to project root")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Overwrite existing main.go implementation files")
	cmd.Flags().StringVar(&opts.Overwrite, "overwrite", "ask", "How to handle file conflicts: allow, deny, or ask")
	cmd.Flags().BoolVar(&opts.UpdateDocs, "update-docs", false, "Use AI to update existing documentation")
	cmd.Flags().BoolVar(&opts.NoVerify, "no-verify", false, "Skip go mod tidy and golangci-lint after regeneration (the run exits 0 unverified; without it a failed step exits 3)")

	return cmd
}

func (o *ProjectOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	mode, err := generator.ParseOverwriteMode(o.Overwrite)
	if err != nil {
		return errors.Wrapf(ErrInvalidOverwriteValue, "%q", o.Overwrite)
	}

	o.Overwrite = string(mode)

	cfg := &generator.Config{
		Path:       o.Path,
		DryRun:     o.shared.dryRun(),
		Force:      o.Force,
		Overwrite:  generator.OverwriteMode(o.Overwrite),
		UpdateDocs: o.UpdateDocs,
		NoVerify:   o.NoVerify,
	}

	return generator.New(p, cfg).RegenerateProject(ctx)
}
