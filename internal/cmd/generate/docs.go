package generate

import (
	"context"

	"github.com/spf13/cobra"

	icmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd"
	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

type DocsOptions struct {
	// shared carries `generate`'s persistent flags, injected by the
	// constructor rather than read from package state.
	shared *SharedFlags

	Name            string
	Path            string
	Parent          string
	CommandName     string
	PackagePath     string
	LegacySrc       string
	Agentless       bool
	PublicAPI       bool
	NoAIAttribution bool
}

func NewCmdDocs(p *props.Props, shared *SharedFlags) *cobra.Command {
	opts := DocsOptions{shared: shared}

	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate Markdown documentation for a command",
		Long: `Generate Markdown documentation for a Go command.

When an AI provider is configured (via --provider or the ai.provider config
key) and --agentless is not set, the command source is analysed and the AI
integration produces rich docs following the docs-site conventions. Otherwise
GTB writes deterministic boilerplate documentation with no API call — AI
doc-generation is opt-in.

Examples:
  # Boilerplate docs (no AI), the default
  gtb generate docs --command mycmd

  # AI-assisted docs (requires a configured provider)
  gtb generate docs --command mycmd --provider claude

  # Force boilerplate even with a provider configured
  gtb generate docs --command mycmd --agentless
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.Run(cmd.Context(), p)
		},
	}

	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Command name (optional, inferred from path)")
	cmd.Flags().StringVar(&opts.Path, "path", ".", "Path to project root")
	cmd.Flags().StringVar(&opts.LegacySrc, "source", "", "Path to the command source code (deprecated, use --command)")
	cmd.Flags().StringVar(&opts.Parent, "parent", "", "Parent command name (optional, if not in manifest)")
	cmd.Flags().StringVar(&opts.CommandName, "command", "", "Name/Path of command to document")
	cmd.Flags().StringVar(&opts.PackagePath, "package", "", "Path to package to document (relative to project root)")
	cmd.Flags().BoolVar(&opts.Agentless, "agentless", false, "Skip AI doc-generation and write boilerplate docs only")
	cmd.Flags().BoolVar(&opts.PublicAPI, "public-api", false, "Module is publicly published: defer package API reference to pkg.go.dev (default: stub to a local 'go doc' hint)")
	cmd.Flags().BoolVar(&opts.NoAIAttribution, "no-ai-attribution", false, "Do not attribute the AI model in generated doc frontmatter: authors carry the project's human author(s) only (default: AI model appended as an additive co-author)")

	// --source is a deprecated alias for --command (Run() falls back to it
	// when --command is empty). It must therefore participate in the
	// one-required group, otherwise a source-only invocation is wrongly
	// rejected before Run() can read it.
	_ = cmd.Flags().MarkDeprecated("source", "use --command instead")

	cmd.MarkFlagsMutuallyExclusive("command", "package")
	cmd.MarkFlagsMutuallyExclusive("source", "package")
	cmd.MarkFlagsOneRequired("command", "package", "source")

	return cmd
}

func (o *DocsOptions) Run(ctx context.Context, p *props.Props) error {
	o.Path = icmd.ResolveProjectPath(p, o.Path)

	cfg := &generator.Config{
		Name:            o.Name,
		Path:            o.Path,
		Parent:          o.Parent,
		DryRun:          o.shared.dryRun(),
		AIProvider:      o.shared.aiProvider(),
		AIModel:         o.shared.aiModel(),
		Agentless:       o.Agentless,
		PublicAPI:       o.PublicAPI,
		NoAIAttribution: o.NoAIAttribution,
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
