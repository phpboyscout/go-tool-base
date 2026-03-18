package regenerate

import (
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/props"
)

func NewCmdRegenerate(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "regenerate",
		Short: "Regenerate project or manifest",
		Long:  `Regenerate project components from manifest or rebuild the manifest from existing source code.`,
	}

	cmd.AddCommand(NewCmdProject(p))
	cmd.AddCommand(NewCmdManifest(p))

	return cmd
}
