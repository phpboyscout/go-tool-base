package root

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Execute runs the root command with centralized error handling.
// It silences Cobra's default error output and routes any error returned by
// the command tree through ErrorHandler.Check at Fatal level.
func Execute(rootCmd *setup.Command, props *p.Props) {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return errors.WithHintf(err, "Run '%s --help' for usage.", cmd.CommandPath())
	})

	defer flushTelemetry(props)

	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, ErrUpdateComplete) {
			props.Logger.Warnf("update complete — please run the command again")

			return
		}

		props.ErrorHandler.Check(err, "", errorhandling.LevelFatal)
	}
}

// flushTelemetry sends any buffered telemetry events and shuts down the
// backend. Uses a bounded background context so command-context cancellation
// does not interrupt the flush.
func flushTelemetry(props *p.Props) {
	// Gate on the collector's resolved enabled state, not the raw config key:
	// the collector may have been enabled via the TELEMETRY_ENABLED env var or
	// the tool-author ForceEnabled path even when telemetry.enabled is false,
	// and those buffered events must still be flushed.
	if props.Collector == nil || !props.Collector.Enabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
	defer cancel()

	_ = props.Collector.Close(ctx)
}
