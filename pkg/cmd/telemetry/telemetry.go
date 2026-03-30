// Package telemetry provides CLI commands for managing anonymous usage telemetry.
package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/telemetry"
)

const resetTimeout = 10 * time.Second

// NewCmdTelemetry creates the telemetry command group.
func NewCmdTelemetry(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Manage anonymous usage telemetry",
	}

	cmd.AddCommand(
		newEnableCmd(p),
		newDisableCmd(p),
		newStatusCmd(p),
		newResetCmd(p),
	)

	return cmd
}

func newEnableCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable anonymous usage telemetry",
		RunE: func(_ *cobra.Command, _ []string) error {
			return setTelemetryEnabled(p, true)
		},
	}
}

func newDisableCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable usage telemetry",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := setTelemetryEnabled(p, false); err != nil {
				return err
			}

			// Immediately drop all buffered and spilled events — the user's
			// withdrawal of consent is immediate and total.
			if p.Collector != nil {
				_ = p.Collector.Drop()
			}

			p.Logger.Print("All pending events have been discarded.")

			return nil
		},
	}
}

func newStatusCmd(p *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current telemetry status",
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
			if p.Collector != nil {
				_ = p.Collector.Drop()
			}

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

			// 3. Disable telemetry
			if err := setTelemetryEnabled(p, false); err != nil {
				return err
			}

			p.Logger.Print("Local telemetry data cleared. Telemetry disabled.")

			return nil
		},
	}
}

// setTelemetryEnabled writes the telemetry.enabled config value and persists to disk.
func setTelemetryEnabled(p *props.Props, enabled bool) error {
	p.Config.Set("telemetry.enabled", enabled)

	v := p.Config.GetViper()
	if err := v.WriteConfig(); err != nil {
		if err2 := v.SafeWriteConfig(); err2 != nil {
			return errors.Wrap(err2, "failed to write config")
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
