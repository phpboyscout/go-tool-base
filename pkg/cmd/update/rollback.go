package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// The bare forms of the optional-value flags.
const (
	previousRelease = "previous"
	runningRelease  = "running"
)

// request is one install: a named target, or the latest release, or with
// rollback and no target the release before the running one.
type request struct {
	target   string
	force    bool
	rollback bool
	pin      bool
}

// Rollback installs target, or the release before the running one when target
// is empty, after confirmation, and pins it (spec 0104 D2, D3, D5). force skips
// the confirmation, which an unattended run cannot give otherwise.
func Rollback(ctx context.Context, props *p.Props, target string, force bool, out io.Writer, opts ...UpdateConfigOption) (*UpdateResult, error) {
	return install(ctx, props, request{target: target, force: force, rollback: true}, out, opts...)
}

// Pin keeps the tool on the running version when target is empty, with no
// network call; otherwise it installs target and keeps the tool there (spec
// 0104 D4).
func Pin(ctx context.Context, props *p.Props, target string, force bool, out io.Writer, opts ...UpdateConfigOption) (*UpdateResult, error) {
	if target != "" {
		return install(ctx, props, request{target: target, force: force, pin: true}, out, opts...)
	}

	current := runningVersion(props)
	if err := setup.SetPinnedVersion(props.FS, props.Tool.Name, current); err != nil {
		return nil, err
	}

	props.Logger.Info(fmt.Sprintf("pinned to %s; update and the update check leave it there until '%s update --unpin'",
		current, props.Tool.Name))

	return &UpdateResult{PreviousVersion: current, NewVersion: current, Pinned: current}, nil
}

// Unpin clears the pin so updates resume.
func Unpin(props *p.Props) (*UpdateResult, error) {
	if err := setup.SetPinnedVersion(props.FS, props.Tool.Name, ""); err != nil {
		return nil, err
	}

	props.Logger.Info("unpinned; updates resume")

	current := runningVersion(props)

	return &UpdateResult{PreviousVersion: current, NewVersion: current}, nil
}

func install(ctx context.Context, props *p.Props, req request, out io.Writer, opts ...UpdateConfigOption) (*UpdateResult, error) {
	o := newUpdateOptions(opts)
	current := runningVersion(props)
	pin := currentPin(props)

	if stayPinned(req, pin) {
		props.Logger.Info(fmt.Sprintf("pinned to %s; nothing to update. Run '%s update --unpin' to resume updates",
			pin, props.Tool.Name))

		return &UpdateResult{PreviousVersion: current, NewVersion: current, Pinned: pin}, nil
	}

	if err := refuseNotOlder(req, current); err != nil {
		return nil, err
	}

	updater, err := resolveUpdater(ctx, props, &req, current, o)
	if err != nil {
		return nil, err
	}

	downgrade, err := confirmDowngrade(ctx, props, current, req)
	if err != nil {
		return nil, err
	}

	result, binPath, err := apply(ctx, props, updater, req, current)
	if err != nil {
		return nil, err
	}

	if result.Updated {
		finishUpdate(ctx, props, updater, req, binPath, result.PreviousVersion, out, opts...)
		result.RolledBack = downgrade
	}

	result.Pinned = movePin(props, req, pin, result.NewVersion)

	return result, nil
}

func newUpdateOptions(opts []UpdateConfigOption) *updateConfigOptions {
	o := &updateConfigOptions{execCommand: exec.CommandContext}
	for _, opt := range opts {
		opt(o)
	}

	o.resolveUpdaterFactories()

	return o
}

// finishUpdate refreshes the config through the new binary and reports.
func finishUpdate(ctx context.Context, props *p.Props, updater Updater, req request, binPath, previousVersion string, out io.Writer, opts ...UpdateConfigOption) {
	UpdateConfig(ctx, props, binPath, opts...)

	if req.target == "" {
		showUpdateChangelog(ctx, updater, previousVersion, out)
	}

	props.Logger.Info("Update complete")

	if props.GetFeatures().Enabled(p.ChangelogCmd) {
		props.Logger.Info(fmt.Sprintf("Run '%s changelog --latest' to see the full changelog.", props.Tool.Name))
	}
}

// stayPinned is D7's no-op: a pinned tool with nothing named and no force.
func stayPinned(req request, pin string) bool {
	return pin != "" && req.target == "" && !req.rollback && !req.force
}

func refuseNotOlder(req request, current string) error {
	if !req.rollback || req.target == "" || ver.CompareVersions(req.target, current) < 0 {
		return nil
	}

	return errors.WithHint(
		errors.Newf("%s is not older than the running %s, so it is not a rollback", req.target, current),
		"To move to it, use 'update --version "+req.target+"'.")
}

// apply replaces the binary. A binary already current replaced nothing, so the
// result says so and nothing else runs: no config refresh, no changelog (#95).
func apply(ctx context.Context, props *p.Props, updater Updater, req request, current string) (*UpdateResult, string, error) {
	previousVersion := updater.GetCurrentVersion()

	target := req.target
	if target == "" {
		target, _ = updater.GetLatestVersionString(ctx)
	}

	if target != "" && target != current {
		props.Logger.Info("Updating", "from", current, "to", target)
	}

	binPath, err := updater.Update(ctx)
	if errors.Is(err, setup.ErrAlreadyCurrent) {
		props.Logger.Info("already running the current version; nothing to update", "version", current)

		return &UpdateResult{PreviousVersion: previousVersion, NewVersion: current}, "", nil
	}

	if err != nil {
		return nil, "", err
	}

	return &UpdateResult{PreviousVersion: previousVersion, NewVersion: target, Updated: true}, binPath, nil
}

// resolveUpdater builds the updater for req, resolving a no-tag rollback to
// the release before current and building again for it.
func resolveUpdater(ctx context.Context, props *p.Props, req *request, current string, o *updateConfigOptions) (Updater, error) {
	updater, err := o.newUpdater(ctx, props, req.target, req.force)
	if err != nil || !req.rollback || req.target != "" {
		return updater, err
	}

	if req.target, err = updater.ReleaseBefore(ctx, current); err != nil {
		return nil, err
	}

	return o.newUpdater(ctx, props, req.target, req.force)
}

// confirmDowngrade reports whether req is a downgrade, and asks before one is
// installed (spec 0104 D3). Unattended, a rollback needs force; a named
// --version proceeds, because naming it is the intent. The implicit path's
// older "latest" is not a downgrade anyone asked for, and the updater refuses
// it itself.
func confirmDowngrade(ctx context.Context, props *p.Props, current string, req request) (bool, error) {
	if req.target == "" || ver.CompareVersions(req.target, current) >= 0 {
		return false, nil
	}

	return true, askDowngrade(ctx, props, current, req)
}

func askDowngrade(ctx context.Context, props *p.Props, current string, req request) error {
	if req.force {
		return nil
	}

	if !attended(props) {
		if !req.rollback {
			return nil
		}

		return errors.WithHint(
			errors.Wrapf(setup.ErrDowngradeNotConfirmed, "rolling back from %s to %s", current, req.target),
			"Nobody is at a terminal to confirm it. Re-run with --force to roll back without asking.")
	}

	confirmed := false
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title(fmt.Sprintf("Roll back %s from %s to %s? This installs an older release.", props.Tool.Name, current, req.target)).
		Value(&confirmed)))

	if err := setup.RunFormOn(ctx, props.GetIO(), form); err != nil {
		return err
	}

	if !confirmed {
		return errors.Wrapf(setup.ErrDowngradeNotConfirmed, "declined rolling back from %s to %s", current, req.target)
	}

	return nil
}

// attended is spec 0208's test: someone can answer, and this is not CI.
func attended(props *p.Props) bool {
	ci := os.Getenv("CI") == "true" || (props.Config != nil && props.Config.View().GetBool("ci"))

	return !ci && setup.Promptable(props.GetIO())
}

// movePin records installed as the pin when the request pins, or when the tool
// was already pinned, and returns the pin that now stands. A failed write keeps
// the old pin and says so.
func movePin(props *p.Props, req request, pin, installed string) string {
	if !req.rollback && !req.pin && pin == "" {
		return ""
	}

	if installed == pin {
		return pin
	}

	if err := setup.SetPinnedVersion(props.FS, props.Tool.Name, installed); err != nil {
		props.Logger.Warn(fmt.Sprintf("installed %s but could not pin it: %v", installed, err))

		return pin
	}

	if pin == "" {
		props.Logger.Info(fmt.Sprintf("pinned to %s; run '%s update --unpin' to resume updates", installed, props.Tool.Name))
	} else {
		props.Logger.Info(fmt.Sprintf("pinned to %s (was %s)", installed, pin))
	}

	return installed
}

func currentPin(props *p.Props) string {
	pin, err := setup.GetPinnedVersion(props.FS, props.Tool.Name)
	if err != nil {
		props.Logger.Warn(fmt.Sprintf("ignoring the update pin: %v", err))
	}

	return pin
}

func runningVersion(props *p.Props) string {
	return ver.FormatVersionString(props.Version.GetVersion(), true)
}
