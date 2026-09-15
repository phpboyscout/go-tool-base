package initialise

import (
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/output"
	ocobra "gitlab.com/phpboyscout/go/output/cobra"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/ai"
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// InitOption configures the init command for testability.
type InitOption func(*initConfig)

type initConfig struct {
	// legacy opts could go here if needed
}

// NewCmdInit creates the init command for first-run configuration.
func NewCmdInit(props *p.Props, opts ...InitOption) *setup.Command {
	cfg := &initConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	initOpts := setup.InitOptions{}

	var initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialise configuration and bootstrap subsystems",
		Long: `Write the tool's configuration file and run the interactive first-run
bootstrap. Each enabled feature that needs setup (an AI provider, a
forge's credentials) contributes a subcommand. Re-run it any time to
reconfigure; use --clean to reset to defaults.

Without an interactive terminal (piped stdin, CI, or a test harness) the
credential wizards are skipped and only the base configuration is written;
configure a provider later with "init <provider>" from a terminal.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			props.Logger.Info("Initialising configuration")

			// Dynamic Discovery of Initialisers
			initOpts.Initialisers = discoverInitialisers(props)

			location, err := setup.Initialise(cmd.Context(), props, initOpts)
			if err != nil {
				return errors.Wrap(err, "failed to initialise configuration")
			}

			props.Logger.Info("configuration initialised", "path", location)

			return ocobra.Emit(cmd, output.Response{
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

	wrapped := setup.Wrap(p.InitCmd, initCmd)

	// Dynamic Discovery of Flags
	registerFeatureFlags(props, initCmd)

	// Dynamic Discovery of Subcommands
	registerSubcommands(props, wrapped)

	return wrapped
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

// registerFeatureFlags attaches each enabled feature's init flags, gated the
// way registerSubcommands gates its subcommands; a tool with no forge feature
// used to advertise every forge's --skip flag (#55).
func registerFeatureFlags(props *p.Props, cmd *cobra.Command) {
	registerFeatureFlagsWhere(cmd, props.Tool.IsEnabled)
}

// registerFeatureFlagsWhere attaches the init flags of every feature for
// which include reports true. Tests use it with an always-true predicate to
// reset the package-level flag targets.
func registerFeatureFlagsWhere(cmd *cobra.Command, include func(p.FeatureID) bool) {
	for feature, providers := range setup.GetFeatureFlags() {
		if !include(feature) {
			continue
		}

		for _, provider := range providers {
			provider(cmd)
		}
	}
}

func registerSubcommands(props *p.Props, cmd *setup.Command) {
	for feature, providers := range setup.GetSubcommands() {
		if props.Tool.IsEnabled(feature) {
			for _, provider := range providers {
				for _, sub := range provider(props) {
					cmd.Register(setup.Wrap(feature, sub))
				}
			}
		}
	}
}
