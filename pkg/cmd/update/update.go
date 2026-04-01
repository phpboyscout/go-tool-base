package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	ver "github.com/phpboyscout/go-tool-base/pkg/version"
)

func init() {
	setup.RegisterMiddleware(p.UpdateCmd, setup.WithAuthCheck(
	// "github.token", // Example: require github.token for updates
	))
}

var (
	// semVerPattern matches semantic version strings in the format v0.0.0 or v0.0.0-suffix.
	semVerPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-\w+)?$`)

	// allow mocking in tests.
	ExportExecCommand = exec.CommandContext
	ExportNewUpdater  = func(props *p.Props, version string, force bool) (Updater, error) {
		return setup.NewUpdater(props, version, force)
	}
	ExportNewOfflineUpdater = func(props *p.Props) Updater {
		return setup.NewOfflineUpdater(props.Tool, props.Logger, props.FS)
	}
)

// Updater defines the interface for self-updating functionality.
type Updater interface {
	GetLatestVersionString(ctx context.Context) (string, error)
	Update(ctx context.Context) (string, error)
	UpdateFromFile(filePath string) (string, error)
	GetReleaseNotes(ctx context.Context, from, to string) (string, error)
	GetCurrentVersion() string
}

// NewCmdUpdate creates the update command for self-updating the tool binary.
func NewCmdUpdate(props *p.Props) *cobra.Command {
	var updateCmd = &cobra.Command{
		Use:   "update",
		Short: "update to the latest available version",
		Long:  `update to the latest available version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromFile, err := cmd.Flags().GetString("from-file")
			if err != nil {
				return errors.Wrap(err, "failed to get from-file flag")
			}

			if fromFile != "" {
				return updateFromFile(cmd, props, fromFile)
			}

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

			result, err := Update(cmd.Context(), props, version, force)
			if err != nil {
				return err
			}

			return output.Emit(cmd, output.Response{
				Status:  output.StatusSuccess,
				Command: "update",
				Data:    result,
			})
		},
	}

	updateCmd.Flags().BoolP("force", "f", false, "force update to the latest version")
	updateCmd.Flags().StringP("version", "v", "", "specific version to update to. if not specified will target latest version")
	updateCmd.Flags().String("from-file", "", "path to a local .tar.gz release archive for offline installation")
	updateCmd.MarkFlagsMutuallyExclusive("from-file", "version")

	return updateCmd
}

// UpdateResult contains the outcome of a successful update.
type UpdateResult struct {
	PreviousVersion string `json:"previous_version"`
	NewVersion      string `json:"new_version"`
	Updated         bool   `json:"updated"`
}

// Update downloads and installs the specified version (or latest) of the tool.
func Update(ctx context.Context, props *p.Props, version string, force bool) (*UpdateResult, error) {
	updater, err := ExportNewUpdater(props, version, force)
	if err != nil {
		return nil, err
	}

	previousVersion := updater.GetCurrentVersion()

	target := version
	if version == "" {
		target, _ = updater.GetLatestVersionString(ctx)
	}

	currentVersion := ver.FormatVersionString(props.Version.GetVersion(), true)
	if target != "" && target != currentVersion {
		props.Logger.Info("Updating", "from", currentVersion, "to", target)
	}

	binPath, err := updater.Update(ctx)
	if err != nil {
		return nil, err
	}

	// update the config in the standard locations
	UpdateConfig(ctx, props, binPath)

	if version == "" {
		// we are in a standard upgrade
		latestVersion, latestErr := updater.GetLatestVersionString(ctx)
		if latestErr == nil {
			releaseNotes, relErr := updater.GetReleaseNotes(ctx, previousVersion, latestVersion)
			if relErr == nil {
				styledNotes := output.RenderMarkdown(releaseNotes)
				props.Logger.Print(styledNotes)
			}
		}
	}

	props.Logger.Info("Update complete")

	return &UpdateResult{
		PreviousVersion: previousVersion,
		NewVersion:      target,
		Updated:         true,
	}, nil
}

func updateFromFile(cmd *cobra.Command, props *p.Props, filePath string) error {
	updater := ExportNewOfflineUpdater(props)

	targetPath, err := updater.UpdateFromFile(filePath)
	if err != nil {
		return err
	}

	UpdateConfig(cmd.Context(), props, targetPath)

	props.Logger.Infof("successfully installed from %s to %s", filePath, targetPath)

	return output.Emit(cmd, output.Response{
		Status:  output.StatusSuccess,
		Command: "update",
		Data: &UpdateResult{
			Updated: true,
		},
	})
}

// UpdateConfig re-runs the init flow after a successful update to ensure config compatibility.
func UpdateConfig(ctx context.Context, props *p.Props, binPath string) {
	if props.Tool.IsDisabled(p.InitCmd) {
		props.Logger.Debug("Skipping config update as init command is disabled")
	} else {
		updatePaths := []string{
			setup.GetDefaultConfigDir(props.FS, props.Tool.Name),
			fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", props.Tool.Name)),
		}

		for _, path := range updatePaths {
			if _, err := props.FS.Stat(path); err == nil {
				cmd := ExportExecCommand(ctx, binPath, "init", "--dir", path, "--skip-login", "--skip-key")
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
