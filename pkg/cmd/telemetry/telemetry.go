// Package telemetry provides CLI commands for managing pseudonymous usage telemetry.
package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
)

const resetTimeout = 10 * time.Second

// NewCmdTelemetry creates the telemetry command group.
func NewCmdTelemetry(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage pseudonymous usage telemetry",
		Long: `Manage opt-in pseudonymous usage analytics for this tool.

Telemetry is off unless you opt in. Use the enable, disable, status, and reset
subcommands to turn collection on or off, inspect the current state, and clear
local data while requesting remote deletion.`,
	}

	telCmd := setup.Wrap(props.TelemetryCmd, cmd)
	// Subcommands are wrapped with the parent's feature key so every
	// telemetry command carries the same middleware.
	telCmd.Register(
		setup.Wrap(props.TelemetryCmd, newEnableCmd(p)),
		setup.Wrap(props.TelemetryCmd, newDisableCmd(p)),
		setup.Wrap(props.TelemetryCmd, newStatusCmd(p)),
		setup.Wrap(props.TelemetryCmd, newResetCmd(p)),
	)

	return telCmd
}

func newEnableCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable pseudonymous usage telemetry",
		Long: `Opt in to pseudonymous usage telemetry for this tool.

No personally identifiable information is collected. The setting is written to
your config file and takes effect immediately.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return setTelemetryEnabled(p, true)
		},
	}
}

func newDisableCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable usage telemetry",
		Long: `Opt out of usage telemetry for this tool.

Any buffered or spilled events are discarded immediately. Tools whose authors
enforce telemetry by organisation policy cannot be disabled here.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if p.Tool.Telemetry.ForceEnabled {
				p.Logger.Print("Telemetry is enforced by your organisation and cannot be disabled.")

				return nil
			}

			if err := setTelemetryEnabled(p, false); err != nil {
				return err
			}

			// Immediately drop all buffered and spilled events — the user's
			// withdrawal of consent is immediate and total.
			_ = p.Collector.Drop()

			p.Logger.Print("All pending events have been discarded.")

			return nil
		},
	}
}

func newStatusCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current telemetry status",
		Long: `Show whether telemetry is enabled, the hashed machine ID, and the active
backend.

Use this to confirm your opt-in state and whether collection is local-only.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			enabled := p.Config.GetBool("telemetry.enabled")
			localOnly := p.Config.GetBool("telemetry.local_only")

			switch {
			case !enabled:
				p.Logger.Print("Telemetry: disabled")
			case localOnly:
				p.Logger.Print("Telemetry: enabled (local-only)")
			default:
				p.Logger.Print("Telemetry: enabled")
			}

			p.Logger.Print("Machine ID: " + telemetry.HashedMachineID())

			p.Logger.Print("Backend: " + p.Collector.BackendInfo())

			return nil
		},
	}
}

func newResetCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Clear local telemetry data and request remote deletion",
		Long: "Clears all local telemetry data (buffered events, spill files, " +
			"local-only logs) and sends a data deletion request to the remote " +
			"backend. Telemetry is disabled after reset.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			machineID := telemetry.HashedMachineID()

			// 1. Drop all local data
			_ = p.Collector.Drop()

			// Clear local-only log if it exists
			dataDir := telemetry.ResolveDataDir(p)
			logFile := filepath.Join(dataDir, "telemetry.log")

			if _, err := os.Stat(logFile); err == nil {
				_ = os.Remove(logFile)
			}

			// 2. Send deletion request via configured requestor
			requestor := buildDeletionRequestor(p)

			ctx, cancel := context.WithTimeout(cmd.Context(), resetTimeout)
			defer cancel()

			if err := requestor.RequestDeletion(ctx, machineID); err != nil {
				p.Logger.Print("Deletion request could not be sent: " + err.Error())

				if p.Tool.Help != nil {
					if msg := p.Tool.Help.SupportMessage(); msg != "" {
						p.Logger.Print("For manual deletion: " + msg)
					}
				}
			} else {
				p.Logger.Print("Deletion request sent for machine ID: " + machineID)
			}

			// 3. Disable telemetry (unless force-enabled by the tool author)
			if p.Tool.Telemetry.ForceEnabled {
				p.Logger.Print("Local telemetry data cleared. Telemetry remains enabled per organisation policy.")
			} else {
				if err := setTelemetryEnabled(p, false); err != nil {
					return err
				}

				p.Logger.Print("Local telemetry data cleared. Telemetry disabled.")
			}

			return nil
		},
	}
}

// setTelemetryEnabled writes the telemetry.enabled config value and persists to disk.
// If no config file exists (e.g. tools that disable InitCmd), the default config
// directory and file are created automatically.
func setTelemetryEnabled(p *props.Props, enabled bool) error {
	p.Config.Set("telemetry.enabled", enabled)

	v := p.Config.GetViper()

	if err := v.WriteConfig(); err != nil {
		// No config file loaded — create the default one
		if err := ensureConfigFile(p, v); err != nil {
			return err
		}
	}

	if enabled {
		p.Logger.Print("Telemetry enabled. Thank you for helping improve " + p.Tool.Name + "!")
		p.Logger.Print("No personally identifiable information is collected.")
	} else {
		p.Logger.Print("Telemetry disabled.")
	}

	return nil
}

// ensureConfigFile creates the default config directory and a minimal config
// file containing only the telemetry setting. This handles tools that disable
// InitCmd and therefore have no init flow to create the config file.
// It does NOT dump the full Viper state (which would include embedded defaults
// like GitHub auth definitions that don't belong in a user config file).
func ensureConfigFile(p *props.Props, v *viper.Viper) error {
	configDir := setup.GetDefaultConfigDir(p.FS, p.Tool.Name)
	if configDir == "" {
		return errors.New("unable to determine config directory")
	}

	configFile := filepath.Join(configDir, setup.DefaultConfigFilename)

	// Create the config dir lazily at first write — setup.GetDefaultConfigDir
	// is pure and no longer creates it as a side effect.
	const configDirPerm = 0o700
	if err := p.FS.MkdirAll(configDir, configDirPerm); err != nil {
		return errors.Wrap(err, "failed to create config directory")
	}

	// Create a fresh Viper with only the telemetry keys to avoid writing
	// embedded defaults (GitHub auth, log config, etc.) into the user's config.
	fresh := viper.New()
	fresh.SetFs(p.FS)
	fresh.SetConfigFile(configFile)
	fresh.SetConfigType("yaml")
	fresh.Set("telemetry.enabled", v.GetBool("telemetry.enabled"))

	if v.IsSet("telemetry.local_only") {
		fresh.Set("telemetry.local_only", v.GetBool("telemetry.local_only"))
	}

	// Point the main Viper at this file so subsequent writes go to the right place
	v.SetConfigFile(configFile)

	return errors.Wrap(fresh.WriteConfigAs(configFile), "failed to write config")
}

// buildDeletionRequestor constructs the appropriate DeletionRequestor.
// Uses the tool-author's custom requestor if configured, otherwise falls back
// to sending a data.deletion_request event through a noop backend (best-effort).
func buildDeletionRequestor(p *props.Props) telemetry.DeletionRequestor {
	if p.Tool.Telemetry.DeletionRequestor != nil {
		raw := p.Tool.Telemetry.DeletionRequestor(p)

		if r, ok := raw.(telemetry.DeletionRequestor); ok {
			return r
		}

		p.Logger.Warn("TelemetryConfig.DeletionRequestor did not return a telemetry.DeletionRequestor; falling back to event-based")
	}

	// Fall back to event-based deletion request.
	// Use the HTTP backend if an endpoint is configured, otherwise noop.
	var backend telemetry.Backend

	switch {
	case p.Tool.Telemetry.Endpoint != "":
		backend = telemetry.NewHTTPBackend(p.Tool.Telemetry.Endpoint, p.Logger)
	default:
		backend = telemetry.NewNoopBackend()
	}

	return telemetry.NewEventDeletionRequestor(backend)
}
