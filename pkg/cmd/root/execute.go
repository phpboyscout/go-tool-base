package root

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
)

// Execute runs the root command with centralized error handling.
// It silences Cobra's default error output and routes any error returned by
// the command tree through ErrorHandler.Check at Fatal level.
func Execute(rootCmd *cobra.Command, props *p.Props) {
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
	if props.Collector == nil {
		return
	}

	if props.Config != nil && !props.Config.GetBool("telemetry.enabled") {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
	defer cancel()

	_ = props.Collector.Close(ctx)
}
