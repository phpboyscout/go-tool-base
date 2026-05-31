package remove

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func NewCmdRemove(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove components from the project",
		Long:  `Remove commands or other components from an existing gtb project.`,
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Usage()
		},
	}

	removeCmd := setup.Wrap("", cmd)
	removeCmd.Register(setup.Wrap("", NewCmdCommand(p)))

	return removeCmd
}
