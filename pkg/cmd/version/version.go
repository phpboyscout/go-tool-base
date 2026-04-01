package version

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

const (
	// versionCheckTimeout is the maximum time allowed for version checking.
	versionCheckTimeout = 60 * time.Second
)

// VersionInfo holds version details for structured output.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Current bool   `json:"current"`
}

// NewCmdVersion creates the version command that displays build information.
func NewCmdVersion(props *p.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version of this program",
		Long:  `Print version of this program`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("output")
			out := output.NewWriter(os.Stdout, output.Format(format))

			info := &VersionInfo{
				Version: props.Version.GetVersion(),
				Commit:  props.Version.GetCommit(),
				Date:    props.Version.GetDate(),
				Current: true,
			}

			if props.Tool.IsDisabled(p.UpdateCmd) || props.Version.IsDevelopment() {
				return out.Write(output.Response{
					Status:  output.StatusSuccess,
					Command: "version",
					Data:    info,
				}, func(w io.Writer) {
					printVersionText(w, info)
				})
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), versionCheckTimeout)
			defer cancel()

			updater, err := setup.NewUpdater(props, "", false)
			if err != nil {
				props.Logger.Warn("failed to load updater for version check", "error", err)

				return out.Write(output.Response{
					Status:  output.StatusSuccess,
					Command: "version",
					Data:    info,
				}, func(w io.Writer) {
					printVersionText(w, info)
				})
			}

			latest, err := updater.GetLatestVersionString(ctx)
			if err != nil {
				return errors.Wrap(err, "unable to fetch latest version")
			}

			info.Latest = latest
			info.Current = info.Version == latest

			if !info.Current {
				props.Logger.Warnf("A new version is available: %s", latest)
			}

			return out.Write(output.Response{
				Status:  output.StatusSuccess,
				Command: "version",
				Data:    info,
			}, func(w io.Writer) {
				printVersionText(w, info)
			})
		},
	}
}

func printVersionText(w io.Writer, info *VersionInfo) {
	_, _ = fmt.Fprintf(w, "Version: %s\n", info.Version)

	if info.Commit != "" {
		_, _ = fmt.Fprintf(w, "Build:   %s\n", info.Commit)
	}

	if info.Date != "" {
		_, _ = fmt.Fprintf(w, "Date:    %s\n", info.Date)
	}

	if info.Latest != "" && !info.Current {
		_, _ = fmt.Fprintf(w, "Latest:  %s (update available)\n", info.Latest)
	}
}
