package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/output"
	ocobra "gitlab.com/phpboyscout/go/output/cobra"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// NewUpdaterFunc constructs an [Updater] for online updates.
type NewUpdaterFunc func(ctx context.Context, props *p.Props, version string, force bool) (Updater, error)

// NewOfflineUpdaterFunc constructs an [Updater] for offline,
// file-based updates.
type NewOfflineUpdaterFunc func(props *p.Props) Updater

// UpdateConfigOption configures the UpdateConfig function.
type UpdateConfigOption func(*updateConfigOptions)

type updateConfigOptions struct {
	execCommand       func(context.Context, string, ...string) *exec.Cmd
	newUpdater        NewUpdaterFunc
	newOfflineUpdater NewOfflineUpdaterFunc
}

// WithExecCommand overrides exec.CommandContext for testing.
func WithExecCommand(fn func(context.Context, string, ...string) *exec.Cmd) UpdateConfigOption {
	return func(o *updateConfigOptions) { o.execCommand = fn }
}

// WithUpdater injects the online [Updater] factory, replacing the default
// that builds a real [setup.NewUpdater]. Each call site receives its own
// factory, so concurrent tests cannot clobber one another.
func WithUpdater(fn NewUpdaterFunc) UpdateConfigOption {
	return func(o *updateConfigOptions) { o.newUpdater = fn }
}

// WithOfflineUpdater injects the offline [Updater] factory, replacing the
// default that builds a real [setup.NewOfflineUpdater].
func WithOfflineUpdater(fn NewOfflineUpdaterFunc) UpdateConfigOption {
	return func(o *updateConfigOptions) { o.newOfflineUpdater = fn }
}

// resolveUpdaterFactories fills in the updater factories from the supplied
// options, falling back to the real constructors when an option is not given.
// Tests inject their own factories with WithUpdater / WithOfflineUpdater (the
// parallel-safe replacement for the removed package-level test seams).
func (o *updateConfigOptions) resolveUpdaterFactories() {
	if o.newUpdater == nil {
		o.newUpdater = func(ctx context.Context, props *p.Props, version string, force bool) (Updater, error) {
			return setup.NewUpdater(ctx, props, version, force)
		}
	}

	if o.newOfflineUpdater == nil {
		o.newOfflineUpdater = func(props *p.Props) Updater {
			opts := []setup.UpdaterOption{setup.WithIO(props.GetIO())}
			if props.Config != nil {
				opts = append(opts, setup.WithVerificationFrom(props.Config.View()))
			}

			return setup.NewOfflineUpdater(props.Tool, props.Logger, props.FS, opts...)
		}
	}
}

// Updater defines the interface for self-updating functionality.
type Updater interface {
	GetLatestVersionString(ctx context.Context) (string, error)
	Update(ctx context.Context) (string, error)
	UpdateFromFile(ctx context.Context, filePath string) (string, error)
	GetReleaseNotes(ctx context.Context, from, to string) (string, error)
	GetCurrentVersion() string
	ReleaseBefore(ctx context.Context, current string) (string, error)
}

// NewCmdUpdate creates the update command for self-updating the tool
// binary. The optional [UpdateConfigOption]s are threaded into both
// the online and offline update paths — pass [WithUpdater] /
// [WithOfflineUpdater] to inject test doubles without mutating any
// package-level seam (parallel-safe).
func NewCmdUpdate(props *p.Props, opts ...UpdateConfigOption) *setup.Command {
	var updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Update to the latest available version",
		Long: `Update the running binary to the latest version. Checks the configured
release source for a newer signed release, downloads and verifies it, then
replaces the current binary in place and re-runs the init flow to keep config
compatible. Pass a specific version to install that one, or an offline release
archive to install without network access.

--rollback installs the release before this one, or a named older one with
--rollback=vX.Y.Z, verified as any update is, and asks first. A rollback pins
the tool there: neither update nor the update check moves it until --unpin.
--pin stays on the running version, and --pin=vX.Y.Z installs that one and
stays on it.`,
		Args: noPositionalVersion,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := runUpdate(cmd, props, opts...)
			if err != nil || result == nil {
				return err
			}

			return ocobra.Emit(cmd, output.Response{
				Status:  output.StatusSuccess,
				Command: "update",
				Data:    result,
			})
		},
	}

	updateCmd.Flags().BoolP("force", "f", false, "force update to the latest version, or roll back without asking")
	updateCmd.Flags().StringP("version", "v", "", "specific version to update to. if not specified will target latest version")
	updateCmd.Flags().String("from-file", "", "path to a local .tar.gz release archive for offline installation")
	updateCmd.Flags().String("rollback", "", "roll back to the release before this one, or to --rollback=vX.Y.Z, and pin it")
	updateCmd.Flags().Lookup("rollback").NoOptDefVal = previousRelease
	updateCmd.Flags().String("pin", "", "stay on the running version, or install --pin=vX.Y.Z and stay on it")
	updateCmd.Flags().Lookup("pin").NoOptDefVal = runningRelease
	updateCmd.Flags().Bool("unpin", false, "clear the pin so updates resume")
	updateCmd.MarkFlagsMutuallyExclusive("from-file", "version", "rollback", "pin", "unpin")

	return setup.AnnotateMCP(setup.Wrap(p.UpdateCmd, updateCmd), setup.MCPOpenWorld())
}

// UpdateResult contains the outcome of a successful update.
type UpdateResult struct {
	PreviousVersion string `json:"previous_version"`
	NewVersion      string `json:"new_version"`
	Updated         bool   `json:"updated"`
	RolledBack      bool   `json:"rolled_back"`
	Pinned          string `json:"pinned,omitempty"`
}

// Update downloads and installs the specified version (or latest) of the tool.
// A pinned tool stays put unless a version is named or force is set, and then
// the pin moves to what was installed (spec 0104 D7).
func Update(ctx context.Context, props *p.Props, version string, force bool, out io.Writer, opts ...UpdateConfigOption) (*UpdateResult, error) {
	return install(ctx, props, request{target: version, force: force}, out, opts...)
}

// runUpdate dispatches on the command's flags; a nil result means the command
// emitted its own.
func runUpdate(cmd *cobra.Command, props *p.Props, opts ...UpdateConfigOption) (*UpdateResult, error) {
	flags := cmd.Flags()

	fromFile, _ := flags.GetString("from-file")
	force, _ := flags.GetBool("force")
	unpin, _ := flags.GetBool("unpin")
	version, _ := flags.GetString("version")
	rollback, _ := flags.GetString("rollback")
	pin, _ := flags.GetString("pin")

	switch {
	case fromFile != "":
		return nil, updateFromFile(cmd, props, fromFile, opts...)
	case unpin:
		return Unpin(props)
	case flags.Changed("rollback"):
		target, err := namedTarget(rollback, previousRelease)
		if err != nil {
			return nil, err
		}

		return Rollback(cmd.Context(), props, target, force, cmd.OutOrStdout(), opts...)
	case flags.Changed("pin"):
		target, err := namedTarget(pin, runningRelease)
		if err != nil {
			return nil, err
		}

		return Pin(cmd.Context(), props, target, force, cmd.OutOrStdout(), opts...)
	default:
		target, err := namedTarget(version, "")
		if err != nil {
			return nil, err
		}

		return Update(cmd.Context(), props, target, force, cmd.OutOrStdout(), opts...)
	}
}

// namedTarget is the release tag a flag names, or "" for the flag's bare form.
func namedTarget(value, bare string) (string, error) {
	if value == "" || value == bare {
		return "", nil
	}

	if !ver.IsReleaseTag(value) {
		return "", errors.Newf("invalid version format %q, expected a release tag such as v1.2.3 or v1.2.3-rc.1", value)
	}

	return value, nil
}

// noPositionalVersion refuses `update --rollback v1.2.3`, where the optional
// flag value takes nothing and the version would otherwise be ignored.
func noPositionalVersion(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	return errors.WithHintf(errors.Newf("update takes no arguments, got %q", args[0]),
		"To name a version, attach it to the flag: --rollback=%s, --pin=%s or --version %s.", args[0], args[0], args[0])
}

// showUpdateChangelog displays release notes after an update using the
// release source API.
func showUpdateChangelog(ctx context.Context, updater Updater, previousVersion string, out io.Writer) {
	// Try release source API
	latestVersion, latestErr := updater.GetLatestVersionString(ctx)
	if latestErr != nil {
		return
	}

	releaseNotes, relErr := updater.GetReleaseNotes(ctx, previousVersion, latestVersion)
	if relErr != nil {
		return
	}

	styledNotes := output.RenderMarkdown(releaseNotes)
	_, _ = fmt.Fprintln(out, styledNotes)
}

func updateFromFile(cmd *cobra.Command, props *p.Props, filePath string, opts ...UpdateConfigOption) error {
	o := newUpdateOptions(opts)

	updater := o.newOfflineUpdater(props)

	targetPath, err := updater.UpdateFromFile(cmd.Context(), filePath)
	if err != nil {
		return err
	}

	UpdateConfig(cmd.Context(), props, targetPath, opts...)

	props.Logger.Info("successfully installed", "from", filePath, "to", targetPath)

	return ocobra.Emit(cmd, output.Response{
		Status:  output.StatusSuccess,
		Command: "update",
		Data: &UpdateResult{
			Updated: true,
		},
	})
}

// UpdateConfig re-runs the init flow after a successful update to ensure config compatibility.
func UpdateConfig(ctx context.Context, props *p.Props, binPath string, opts ...UpdateConfigOption) {
	o := &updateConfigOptions{
		execCommand: exec.CommandContext,
	}

	for _, opt := range opts {
		opt(o)
	}

	if !props.GetFeatures().Enabled(p.InitCmd) {
		props.Logger.Debug("Skipping config update as init command is disabled")
	} else {
		updatePaths := []string{
			setup.GetDefaultConfigDir(props.FS, props.Tool.Name),
			fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join("etc", props.Tool.Name)),
		}

		for _, path := range updatePaths {
			if _, err := props.FS.Stat(path); err == nil {
				// No profile's skip flag is named: --skip-login belongs to the
				// GitHub profile alone and a GitLab tool's init refused it, so
				// the refresh silently never ran (#96). Off a terminal and under
				// --ci, init skips every credential wizard by itself.
				cmd := o.execCommand(ctx, binPath, "init", "--dir", path, "--ci", "--skip-key")
				cmd.Stdout = props.GetIO().Out()
				cmd.Stderr = props.GetIO().Err()

				initErr := cmd.Run()
				if initErr != nil {
					props.Logger.Warn("could not update config", "dir", path, "error", initErr)
				}
			}
		}
	}
}
