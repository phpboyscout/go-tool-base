package regenerate

import (
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// SharedFlags carries `regenerate`'s persistent flags to the subcommands that
// read them. See generate.SharedFlags for why this is not a package-level
// variable: pflag writes to whatever it is given, so a package-level target
// makes building two command trees a data race.
type SharedFlags struct {
	DryRun     bool
	AI         bool
	NoAI       bool
	AIProvider string
}

// dryRun tolerates a nil receiver, so a zero-value Options struct built directly
// by a test reads the default rather than panicking.
func (f *SharedFlags) dryRun() bool {
	return f != nil && f.DryRun
}

// apply copies the run's AI answer and provider onto cfg (spec 0211 D4, D11).
func (f *SharedFlags) apply(cfg *generator.Config) {
	if f == nil {
		return
	}

	cfg.AI, cfg.NoAI, cfg.AIProvider = f.AI, f.NoAI, f.AIProvider
}

func NewCmdRegenerate(p *props.Props) *setup.Command {
	shared := &SharedFlags{}

	cmd := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate project or manifest",
		Long: `Regenerate scaffolding for an existing GTB project.

"project" rewrites command registration files from the manifest, while
"manifest" does the reverse, rebuilding the manifest by scanning the project's
existing source code.`,
		RunE: setup.GroupRunE,
	}

	cmd.PersistentFlags().BoolVar(&shared.DryRun, "dry-run", false, "preview changes without writing files")
	cmd.PersistentFlags().BoolVar(&shared.AI, "ai", false, "Let this run call a model for missing doc pages (spends API credit)")
	cmd.PersistentFlags().BoolVar(&shared.NoAI, "no-ai", false, "Call no model in this run, whatever the manifest or config says")
	cmd.PersistentFlags().StringVar(&shared.AIProvider, "provider", "", "AI provider to use when the run calls a model ("+strings.Join(generator.KnownChatProviders(), ", ")+")")

	regenerateCmd := setup.Wrap("", cmd)
	regenerateCmd.Register(
		setup.Wrap("", NewCmdProject(p, shared)),
		setup.Wrap("", NewCmdManifest(p, shared)),
	)

	return regenerateCmd
}
