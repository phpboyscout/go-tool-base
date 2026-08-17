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
		Long: `Remove components from an existing GTB project.

The "command" subcommand deletes a command's files, updates the manifest, and
de-registers it from its parent.`,
		RunE: setup.GroupRunE,
	}

	removeCmd := setup.Wrap("", cmd)
	removeCmd.Register(setup.Wrap("", NewCmdCommand(p)))

	return removeCmd
}
