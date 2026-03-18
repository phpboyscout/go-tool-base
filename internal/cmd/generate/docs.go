package generate

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/internal/generator"
	"github.com/phpboyscout/gtb/pkg/props"
)

type DocsOptions struct {
	Name        string
	Path        string
	Parent      string
	CommandName string
	PackagePath string
	LegacySrc   string
}

func NewCmdDocs(p *props.Props) *cobra.Command {
	opts := DocsOptions{}

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate documentation for a command using AI",
		Long: `Generate comprehensive Markdown documentation for a Go command using AI.
This command analyzes the source code of the specified command and uses the AI integration to generate docs following MkDocs conventions.

Examples:
  # Generate docs for a command
  gtb generate docs --path ./internal/cmd/mycmd
`,
		Run: func(cmd *cobra.Command, args []string) {
			p.ErrorHandler.Fatal(opts.Run(cmd.Context(), p))
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Command name (optional, inferred from path)")
	cmd.Flags().StringVar(&opts.Path, "path", ".", "Path to project root")
	cmd.Flags().StringVar(&opts.LegacySrc, "source", "", "Path to the command source code (deprecated, use --command)")
	cmd.Flags().StringVar(&opts.Parent, "parent", "", "Parent command name (optional, if not in manifest)")
	cmd.Flags().StringVar(&opts.CommandName, "command", "", "Name/Path of command to document")
	cmd.Flags().StringVar(&opts.PackagePath, "package", "", "Path to package to document (relative to project root)")

	cmd.MarkFlagsMutuallyExclusive("command", "package")
	cmd.MarkFlagsOneRequired("command", "package")

	if err := cmd.MarkFlagRequired("path"); err != nil {
		panic(err)
	}

	return cmd
}

func (o *DocsOptions) Run(ctx context.Context, p *props.Props) error {
	cfg := &generator.Config{
		Name:       o.Name,
		Path:       o.Path,
		Parent:     o.Parent,
		AIProvider: aiProvider,
		AIModel:    aiModel,
	}

	gen := generator.New(p, cfg)

	if o.PackagePath != "" {
		return gen.GenerateDocs(ctx, o.PackagePath, true)
	}

	target := o.CommandName
	if target == "" {
		target = o.LegacySrc
	}

	return gen.GenerateDocs(ctx, target, false)
}
