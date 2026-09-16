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

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
	// skipTelemetry is shared between the FeatureFlag closure (which binds the
	// flag during flag parsing) and the InitialiserProvider closure (which reads
	// it when creating the initialiser). A plain *bool avoids package-level state
	// while allowing both closures to reference the same value.
	skipTelemetry := new(bool)

	setup.Register(props.TelemetryCmd,
		[]setup.InitialiserProvider{
			func(p *props.Props) setup.Initialiser {
				if *skipTelemetry {
					return nil
				}

				return NewTelemetryInitialiser(p)
			},
		},
		nil, // no subcommands for init
		[]setup.FeatureFlag{
			func(cmd *cobra.Command) {
				isCI := os.Getenv("CI") == "true"
				cmd.Flags().BoolVar(skipTelemetry, "skip-telemetry", isCI,
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

	if err := setup.RunForm(ctx, p, consentForm(p, &optIn)); err != nil {
		return errors.Wrap(err, "telemetry consent form")
	}

	return cfg.Set(setup.ConfigKeyTelemetryEnabled, optIn)
}

func consentForm(p *props.Props, optIn *bool) *huh.Form {
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
