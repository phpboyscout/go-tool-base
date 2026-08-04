package version

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/output"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const (
	// explicitCheckTimeout is the maximum time allowed for the opt-in
	// `--check` probe, which the caller runs precisely to test whether
	// the release source is reachable, so it keeps the previous generous
	// budget. The passive default check uses the much shorter shared
	// setup.VersionCheckTimeout: there it only bounds a courtesy
	// annotation on an otherwise-local command.
	explicitCheckTimeout = 60 * time.Second
)

// VersionInfo holds version details for structured output.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Current bool   `json:"current"`
	// CheckFailed marks a degraded response: the latest-version check was
	// attempted but the release source could not be reached, so Latest is
	// absent and Current is false because the answer is unknown — scripts
	// must not read this as "up to date" or "behind".
	CheckFailed bool `json:"check_failed,omitempty"`
}

// NewCmdVersion creates the version command that displays build information.
func NewCmdVersion(props *p.Props) *setup.Command {
	// Version is a nilable interface field. The root defaults it, but guard here
	// too (belt-and-braces) so a downstream constructing the version command
	// from a hand-built Props does not panic dereferencing a nil Version.
	if props.Version == nil {
		props.Version = ver.NewInfo("", "", "")
	}

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Long: `Print the running binary's version, commit, and build date, as injected
at build time via ldflags. Unless the update command is disabled or this
is a development build, it also contacts the configured release source to
report whether a newer version is available. That check is best-effort:
when the release source is unreachable the local details are still
printed and the command exits successfully. Pass --check to make an
unreachable release source a hard error instead (useful for scripted
update probes); --check also contacts the release source on development
builds.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("output")
			check, _ := cmd.Flags().GetBool("check")
			out := output.New(output.WithWriter(cmd.OutOrStdout()), output.WithFormat(output.Format(format)))

			info := &VersionInfo{
				Version: props.Version.GetVersion(),
				Commit:  props.Version.GetCommit(),
				Date:    props.Version.GetDate(),
				Current: true,
			}

			// --check bypasses the development-build skip so maintainers
			// can probe the release source from a dev build; the
			// disabled-update fast path always skips the network.
			if props.Tool.IsDisabled(p.UpdateCmd) || (props.Version.IsDevelopment() && !check) {
				return writeVersion(out, info)
			}

			timeout := setup.VersionCheckTimeout
			if check {
				timeout = explicitCheckTimeout
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			latest, err := fetchLatestVersion(ctx, props)
			if err != nil {
				if check {
					return errors.Wrap(err, "unable to fetch latest version")
				}

				// Degrade: the local details are already in hand, so an
				// unreachable release source must not turn a local
				// diagnostic command into a failure. Latest stays empty,
				// Current is false (unknown, not "behind"), and the
				// single warning below is the only trace of the failure.
				props.Logger.Warn("failed to check latest version", "error", err)

				info.Current = false
				info.CheckFailed = true

				return writeVersion(out, info)
			}

			info.Latest = latest
			info.Current = info.Version == latest

			if !info.Current {
				props.Logger.Warn("a new version is available", "version", latest)
			}

			return writeVersion(out, info)
		},
	}

	// version performs (and prints) its own latest-version lookup, so the
	// pre-run's update check would be redundant noise before it.
	setup.MarkSkipUpdateCheck(cmd)

	cmd.Flags().Bool("check", false,
		"fail with a non-zero exit when the release source is unreachable (also checks on development builds)")

	// "" feature: version is a generic built-in (no feature-specific middleware).
	return setup.Wrap("", cmd)
}

// fetchLatestVersion constructs the self-updater and asks the release source
// for the latest version string. Both failure modes — cannot construct the
// updater, cannot reach the release source — are the same condition from the
// caller's point of view: the latest-version lookup is unavailable.
func fetchLatestVersion(ctx context.Context, props *p.Props) (string, error) {
	updater, err := setup.NewUpdater(ctx, props, "", false)
	if err != nil {
		return "", err
	}

	return updater.GetLatestVersionString(ctx)
}

// writeVersion emits the standard success envelope for the version command.
func writeVersion(out *output.Renderer, info *VersionInfo) error {
	return out.Write(output.Response{
		Status:  output.StatusSuccess,
		Command: "version",
		Data:    info,
	}, func(w io.Writer) {
		printVersionText(w, info)
	})
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
