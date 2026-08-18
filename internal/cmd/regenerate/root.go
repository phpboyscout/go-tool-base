package regenerate

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// SharedFlags carries `regenerate`'s persistent flags to the subcommands that
// read them. See generate.SharedFlags for why this is not a package-level
// variable: pflag writes to whatever it is given, so a package-level target
// makes building two command trees a data race.
type SharedFlags struct {
	DryRun bool
}

// dryRun tolerates a nil receiver, so a zero-value Options struct built directly
// by a test reads the default rather than panicking.
func (f *SharedFlags) dryRun() bool {
	return f != nil && f.DryRun
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

	regenerateCmd := setup.Wrap("", cmd)
	regenerateCmd.Register(
		setup.Wrap("", NewCmdProject(p, shared)),
		setup.Wrap("", NewCmdManifest(p, shared)),
	)

	return regenerateCmd
}
