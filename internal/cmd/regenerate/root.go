package regenerate

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

var dryRun bool

func NewCmdRegenerate(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate project or manifest",
		Long:  `Regenerate project components from manifest or rebuild the manifest from existing source code.`,
	}

	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview changes without writing files")

	regenerateCmd := setup.Wrap("", cmd)
	regenerateCmd.Register(
		setup.Wrap("", NewCmdProject(p)),
		setup.Wrap("", NewCmdManifest(p)),
	)

	return regenerateCmd
}
