// Package telemetry registers the telemetry initialiser with the setup system.
// When TelemetryCmd is enabled and the user runs `init`, they are prompted
// to opt into anonymous usage telemetry.
package telemetry

import (
	"context"
	"os"
	"strconv"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
	setup.Register(props.TelemetryCmd,
		[]setup.InitialiserProvider{
			func(p *props.Props, flags *pflag.FlagSet) setup.Initialiser {
				if setup.FlagSkips(flags, "skip-telemetry")["skip-telemetry"] {
					return nil
				}

				return NewTelemetryInitialiser(p)
			},
		},
		nil, // no subcommands for init
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				cmd.Flags().Bool("skip-telemetry", setup.CIDefault(),
					"skip telemetry consent prompt (non-interactive environments)")
			},
		},
	)
}

// TelemetryInitialiser implements setup.Initialiser.
// It prompts the user to opt into telemetry during init.
type TelemetryInitialiser struct {
	props *props.Props
}

// NewTelemetryInitialiser creates a new TelemetryInitialiser.
func NewTelemetryInitialiser(p *props.Props) *TelemetryInitialiser {
	return &TelemetryInitialiser{props: p}
}

// Name returns the human-readable name for this initialiser.
func (t *TelemetryInitialiser) Name() string {
	return "telemetry"
}

// IsConfigured returns true if the telemetry.enabled key has been explicitly
// set in config, OR if the TELEMETRY_ENABLED environment variable is set
// (any value counts as "configured — no prompt needed").
func (t *TelemetryInitialiser) IsConfigured(cfg config.Reader) bool {
	if _, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
		return true
	}

	return cfg.IsSet(setup.ConfigKeyTelemetryEnabled)
}

// Configure prompts the user to opt into telemetry.
// If TELEMETRY_ENABLED is set, applies it directly without prompting.
func (t *TelemetryInitialiser) Configure(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	// Non-interactive bypass
	if val, ok := os.LookupEnv("TELEMETRY_ENABLED"); ok {
		enabled, _ := strconv.ParseBool(val)

		return cfg.Set(setup.ConfigKeyTelemetryEnabled, enabled)
	}

	var optIn bool

	if err := setup.RunForm(ctx, p, ConsentForm(p, &optIn)); err != nil {
		return errors.Wrap(err, "telemetry consent form")
	}

	return cfg.Set(setup.ConfigKeyTelemetryEnabled, optIn)
}

// ConsentForm asks the one-time opt-in question in the tool's name. The root
// pre-run asks it too, for tools that never run init.
func ConsentForm(p *props.Props, optIn *bool) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Key("telemetry").
			Title("Anonymous usage telemetry").
			Description(
				"Help improve " + p.Tool.Name + " by sending anonymous usage statistics.\n" +
					"No personally identifiable information is collected.\n" +
					"You can change this at any time with `" + p.Tool.Name + " telemetry enable/disable`.",
			).
			Value(optIn),
	))
}
