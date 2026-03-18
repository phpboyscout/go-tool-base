package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/term"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	p "github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/setup"
)

// semVerPattern matches semantic version strings in the format v0.0.0 or v0.0.0-suffix.
var semVerPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-\w+)?$`)

func NewCmdUpdate(props *p.Props) *cobra.Command {
	var updateCmd = &cobra.Command{
		Use:   "update",
		Short: "update to the latest available version",
		Long:  `update to the latest available version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return errors.Wrap(err, "failed to get force flag")
			}

			version, err := cmd.Flags().GetString("version")
			if err != nil {
				return errors.Wrap(err, "failed to get version flag")
			}

			if version != "" && !semVerPattern.MatchString(version) {
				return errors.Newf("invalid version format %q, expected semVer pattern v0.0.0", version)
			}

			return Update(cmd.Context(), props, version, force)
		},
	}

	updateCmd.Flags().BoolP("force", "f", false, "force update to the latest version")
	updateCmd.Flags().StringP("version", "v", "", "specific version to update to. if not specified will target latest version")

	return updateCmd
}

func Update(ctx context.Context, props *p.Props, version string, force bool) error {
	updater, err := setup.NewUpdater(props, version, force)
	if err != nil {
		return err
	}

	target := version
	if version == "" {
		target, _ = updater.GetLatestVersionString(ctx)
	}

	props.Logger.Info("Updating", "from", props.Version.GetVersion(), "to", target)

	binPath, err := updater.Update(ctx)
	if err != nil {
		return err
	}

	// update the config in the standard locations
	updateConfig(ctx, props, binPath)

	if version == "" {
		// we are in a standard upgrade
		latestVersion, latestErr := updater.GetLatestVersionString(ctx)
		if latestErr == nil {
			releaseNotes, err := updater.GetReleaseNotes(ctx, updater.CurrentVersion, latestVersion)
			if err == nil {
				styledNotes := renderMarkdown(releaseNotes)
				props.Logger.Print(styledNotes)
			}
		}
	}

	props.Logger.Info("Update complete")

	return nil
}

func updateConfig(ctx context.Context, props *p.Props, binPath string) {
	if props.Tool.IsDisabled(p.InitCmd) {
		props.Logger.Debug("Skipping config update as init command is disabled")
	} else {
		updatePaths := []string{
			setup.GetDefaultConfigDir(props.FS, props.Tool.Name),
			fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", props.Tool.Name)),
		}

		for _, path := range updatePaths {
			if _, err := os.Stat(path); err == nil {
				cmd := exec.CommandContext(ctx, binPath, "init", "--dir", path, "--skip-login", "--skip-key")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				initErr := cmd.Run()
				if initErr != nil {
					props.Logger.Warnf("could not update config in dir '%s': %s", path, initErr)
				}
			}
		}
	}
}

// renderMarkdown uses glamour to style markdown content.
func renderMarkdown(content string) string {
	// Get terminal width, fallback to 80 if detection fails
	width := 80
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		width = w
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content // fallback to plain text if glamour fails
	}

	out, err := r.Render(content)
	if err != nil {
		return content // fallback to plain text if rendering fails
	}

	return strings.TrimSpace(out)
}
