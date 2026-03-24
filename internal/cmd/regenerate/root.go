package regenerate

import (
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

var dryRun bool

func NewCmdRegenerate(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate project or manifest",
		Long:  `Regenerate project components from manifest or rebuild the manifest from existing source code.`,
	}

	cmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "preview changes without writing files")

	cmd.AddCommand(NewCmdProject(p))
	cmd.AddCommand(NewCmdManifest(p))

	return cmd
}
