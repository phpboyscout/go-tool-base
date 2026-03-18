package version

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"
)

const (
	// versionCheckTimeout is the maximum time allowed for version checking.
	versionCheckTimeout = 60 * time.Second
)

func NewCmdVersion(props *props.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version of this program",
		Long:  `Print version of this program`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			props.Logger.Print("",
				"version", props.Version.GetVersion(),
				"Build", props.Version.GetCommit(),
				"Built On", props.Version.GetDate())

			ctx, cancel := context.WithTimeout(cmd.Context(), versionCheckTimeout)
			defer cancel()

			current := props.Version.GetVersion()

			updater, err := setup.NewUpdater(props, "", false)
			if err != nil {
				props.Logger.Warn("failed to load updater for version check", "error", err)

				return nil
			}

			latest, err := updater.GetLatestVersionString(ctx)
			if err != nil {
				return errors.Wrap(err, "unable to fetch latest version")
			}

			if current != latest {
				props.Logger.Warnf("A new version is available: %s", latest)
			} else {
				props.Logger.Info("You are running the latest version")
			}

			return nil
		},
	}
}
