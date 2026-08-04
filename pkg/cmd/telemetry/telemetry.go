// Package telemetry provides the `telemetry` command group for managing
// pseudonymous usage telemetry: enable, disable, status, and reset subcommands.
// Telemetry is opt-in; `reset` clears local state and requests deletion of
// previously sent data.
package telemetry

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setTelemetryEnabled(cmd.Context(), p, true, cmd.OutOrStdout())
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if p.Tool.Telemetry.ForceEnabled {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Telemetry is enforced by your organisation and cannot be disabled.")

				return nil
			}

			if err := setTelemetryEnabled(cmd.Context(), p, false, cmd.OutOrStdout()); err != nil {
				return err
			}

			// Immediately drop all buffered and spilled events — the user's
			// withdrawal of consent is immediate and total.
			_ = p.Collector.Drop()

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "All pending events have been discarded.")

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
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := p.Config.View()
			enabled := cfg.GetBool("telemetry.enabled")
			localOnly := cfg.GetBool("telemetry.local_only")

			out := cmd.OutOrStdout()

			switch {
			case !enabled:
				_, _ = fmt.Fprintln(out, "Telemetry: disabled")
			case localOnly:
				_, _ = fmt.Fprintln(out, "Telemetry: enabled (local-only)")
			default:
				_, _ = fmt.Fprintln(out, "Telemetry: enabled")
			}

			_, _ = fmt.Fprintln(out, "Machine ID: "+telemetry.HashedMachineID())

			_, _ = fmt.Fprintln(out, "Backend: "+p.Collector.BackendInfo())

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
			dataDir := telemetry.ResolveDataDirFromProps(p)
			logFile := filepath.Join(dataDir, "telemetry.log")

			if _, err := os.Stat(logFile); err == nil {
				_ = os.Remove(logFile)
			}

			// 2. Send deletion request via configured requestor
			requestor := buildDeletionRequestor(p)

			ctx, cancel := context.WithTimeout(cmd.Context(), resetTimeout)
			defer cancel()

			out := cmd.OutOrStdout()

			if err := requestor.RequestDeletion(ctx, machineID); err != nil {
				_, _ = fmt.Fprintln(out, "Deletion request could not be sent: "+err.Error())

				if p.Tool.Help != nil {
					if msg := p.Tool.Help.SupportMessage(); msg != "" {
						_, _ = fmt.Fprintln(out, "For manual deletion: "+msg)
					}
				}
			} else {
				_, _ = fmt.Fprintln(out, "Deletion request sent for machine ID: "+machineID)
			}

			// 3. Disable telemetry (unless force-enabled by the tool author)
			if p.Tool.Telemetry.ForceEnabled {
				_, _ = fmt.Fprintln(out, "Local telemetry data cleared. Telemetry remains enabled per organisation policy.")
			} else {
				if err := setTelemetryEnabled(cmd.Context(), p, false, out); err != nil {
					return err
				}

				_, _ = fmt.Fprintln(out, "Local telemetry data cleared. Telemetry disabled.")
			}

			return nil
		},
	}
}

// setTelemetryEnabled persists the telemetry.enabled choice through the
// store: Apply edits the target document in place, writing only this key, and
// a missing file is a legitimate target it creates. This replaces a
// fresh-viper workaround (ensureConfigFile) that existed solely to avoid
// dumping embedded defaults into the user's file — a problem the store's
// provenance-routed writes do not have (migration spec D9).
func setTelemetryEnabled(ctx context.Context, p *props.Props, enabled bool, out io.Writer) error {
	// The default config dir may not exist yet (tools without InitCmd);
	// create it lazily so the write's target directory is present.
	if _, err := setup.EnsureDefaultConfigDir(p.FS, p.Tool.Name); err != nil {
		return err
	}

	if _, err := p.Config.Apply(ctx, config.Set("telemetry.enabled", enabled)); err != nil {
		return errors.Wrap(err, "failed to persist telemetry setting")
	}

	if enabled {
		_, _ = fmt.Fprintln(out, "Telemetry enabled. Thank you for helping improve "+p.Tool.Name+"!")
		_, _ = fmt.Fprintln(out, "No personally identifiable information is collected.")
	} else {
		_, _ = fmt.Fprintln(out, "Telemetry disabled.")
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
		backend = telemetry.NewHTTPBackend(p.Tool.Telemetry.Endpoint, logger.ToSlog(p.Logger))
	default:
		backend = telemetry.NewNoopBackend()
	}

	return telemetry.NewEventDeletionRequestor(backend)
}
