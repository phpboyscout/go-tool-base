package initialise

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/output"
	ocobra "gitlab.com/phpboyscout/go/output/cobra"

	"gitlab.com/phpboyscout/go/features"

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

			// Each enabled feature's initialiser, reading this run's flags.
			initOpts.Initialisers = discoverInitialisers(props, cmd.Flags())

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
	// --skip-key is the init command's own: every profile that offers an SSH
	// key honours it, whichever forges are linked (spec 0199 D3).
	initCmd.Flags().BoolP(setup.SkipKeyFlag, "k", setup.CIDefault(), "skip configuring ssh key")

	wrapped := setup.Wrap(p.InitCmd, initCmd)

	// Dynamic Discovery of Flags
	registerFeatureFlags(props, initCmd)

	// Dynamic Discovery of Subcommands
	registerSubcommands(props, wrapped)

	return wrapped
}

// discoverInitialisers asks each enabled feature's providers for an
// initialiser, handing them this run's flags; a nil answer is a feature that
// asked to be skipped.
func discoverInitialisers(props *p.Props, flags *pflag.FlagSet) []setup.Initialiser {
	var initialisers []setup.Initialiser

	for _, d := range props.GetFeatures().EnabledDescriptors() {
		providers, _ := features.ContributionsOf[setup.InitialiserProvider](props.GetFeatures(), d.FeatureID(), setup.SlotInitialiser)
		for _, provider := range providers {
			if init := provider(props, flags); init != nil {
				initialisers = append(initialisers, init)
			}
		}
	}

	return initialisers
}

// registerFeatureFlags binds each enabled feature's init flags on cmd; a tool
// with no forge feature used to advertise every forge's --skip flag (#55).
// The targets are the command's own, so two roots never share one (#37).
func registerFeatureFlags(props *p.Props, cmd *cobra.Command) {
	for _, d := range props.GetFeatures().EnabledDescriptors() {
		binders, _ := features.ContributionsOf[setup.FeatureFlag](props.GetFeatures(), d.FeatureID(), setup.SlotInitFlag)
		for _, bind := range binders {
			bind(cmd)
		}
	}
}

func registerSubcommands(props *p.Props, cmd *setup.Command) {
	for _, d := range props.GetFeatures().EnabledDescriptors() {
		providers, _ := features.ContributionsOf[setup.SubcommandProvider](props.GetFeatures(), d.FeatureID(), setup.SlotSubcommand)
		for _, provider := range providers {
			for _, sub := range provider(props) {
				cmd.Register(setup.Wrap(d.FeatureID(), sub))
			}
		}
	}
}
