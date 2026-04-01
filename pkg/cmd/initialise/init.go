package initialise

import (
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	_ "github.com/phpboyscout/go-tool-base/pkg/setup/ai"
	_ "github.com/phpboyscout/go-tool-base/pkg/setup/github"
)

// InitOption configures the init command for testability.
type InitOption func(*initConfig)

type initConfig struct {
	// legacy opts could go here if needed
}

// NewCmdInit creates the init command for first-run configuration.
func NewCmdInit(props *p.Props, opts ...InitOption) *cobra.Command {
	cfg := &initConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	initOpts := setup.InitOptions{}

	var initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialises the configuration",
		Long:  `Initialises the default configuration`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			props.Logger.Info("Initialising configuration")

			// Dynamic Discovery of Initialisers
			initOpts.Initialisers = discoverInitialisers(props)

			location, err := setup.Initialise(props, initOpts)
			if err != nil {
				return errors.Wrap(err, "failed to initialise configuration")
			}

			props.Logger.Infof("Configuration initialised in %s", location)

			return output.Emit(cmd, output.Response{
				Status:  output.StatusSuccess,
				Command: "init",
				Data: map[string]any{
					"config_path": location,
				},
			})
		},
	}

	initCmd.Flags().StringVarP(&initOpts.Dir, "dir", "d", setup.GetDefaultConfigDir(props.FS, props.Tool.Name), "directory to initialise the config in")
	initCmd.Flags().BoolVarP(&initOpts.Clean, "clean", "c", false, "reset the existing configuration and replace with the defaults")

	// Dynamic Discovery of Flags
	registerFeatureFlags(initCmd)

	// Dynamic Discovery of Subcommands
	registerSubcommands(props, initCmd)

	return initCmd
}

func discoverInitialisers(props *p.Props) []setup.Initialiser {
	var initialisers []setup.Initialiser

	for feature, providers := range setup.GetInitialisers() {
		if props.Tool.IsEnabled(feature) {
			for _, provider := range providers {
				if init := provider(props); init != nil {
					initialisers = append(initialisers, init)
				}
			}
		}
	}

	return initialisers
}

func registerFeatureFlags(cmd *cobra.Command) {
	for _, providers := range setup.GetFeatureFlags() {
		for _, provider := range providers {
			provider(cmd)
		}
	}
}

func registerSubcommands(props *p.Props, cmd *cobra.Command) {
	for feature, providers := range setup.GetSubcommands() {
		if props.Tool.IsEnabled(feature) {
			for _, provider := range providers {
				for _, sub := range provider(props) {
					setup.AddCommandWithMiddleware(cmd, sub, feature)
				}
			}
		}
	}
}
