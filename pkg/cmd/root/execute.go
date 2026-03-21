package root

import (
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/errorhandling"
	p "github.com/phpboyscout/gtb/pkg/props"
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

	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, ErrUpdateComplete) {
			props.Logger.Warnf("update complete — please run the command again")

			return
		}

		props.ErrorHandler.Check(err, "", errorhandling.LevelFatal)
	}
}
