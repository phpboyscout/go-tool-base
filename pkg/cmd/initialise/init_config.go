package initialise

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// registerConfigSources adds `init config <name>` for each config source the
// tool declares (spec 0204 D3, R5). A tool that declares none has no
// `init config`.
func registerConfigSources(props *p.Props, cmd *setup.Command) {
	if len(props.Tool.Config.Sources) == 0 {
		return
	}

	group := setup.Wrap(p.InitCmd, &cobra.Command{
		Use:   "config",
		Short: "Configure where this tool reads its configuration from",
		Long: `Configure one of the config sources this tool declares: where it connects
and how it authenticates. The settings are written to your own config file
under config.sources.<name>, and take effect the next time the tool runs.`,
		RunE: setup.GroupRunE,
	})

	for _, slot := range props.Tool.Config.Sources {
		group.Register(setup.AnnotateMCP(setup.Wrap(p.InitCmd, newCmdInitConfigSource(props, slot)), setup.MCPOpenWorld()))
	}

	cmd.Register(group)
}

func newCmdInitConfigSource(props *p.Props, slot p.ConfigSource) *cobra.Command {
	cmd := &cobra.Command{
		Use:   slot.Name,
		Short: "Configure the " + slot.Name + " config source (" + slot.Kind + ")",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := cmd.Flags().GetString("dir")

			if err := setup.RunConfigSourceInit(cmd.Context(), props, slot, dir); err != nil {
				return errors.Wrapf(err, "configuring the %s config source", slot.Name)
			}

			props.Logger.Info("config source configured", "source", slot.Name)

			return nil
		},
	}

	cmd.Flags().String("dir", setup.GetDefaultConfigDir(props.FS, props.Tool.Name), "directory containing the config file")

	return cmd
}
