package update_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/exectest"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// rollbackUpdater is an Updater double that records what the command asked
// of it: which targets it was built for and whether it replaced anything.
type rollbackUpdater struct {
	latest    string
	before    string
	beforeErr error
	updateErr error

	built    []string
	replaced bool
}

func (r *rollbackUpdater) factory() update.UpdateConfigOption {
	return update.WithUpdater(func(_ context.Context, _ *p.Props, version string, _ bool) (update.Updater, error) {
		r.built = append(r.built, version)

		return r, nil
	})
}

func (r *rollbackUpdater) GetLatestVersionString(context.Context) (string, error) {
	return r.latest, nil
}

func (r *rollbackUpdater) Update(context.Context) (string, error) {
	if r.updateErr != nil {
		return "", r.updateErr
	}

	r.replaced = true

	return "/tmp/new-bin", nil
}

func (r *rollbackUpdater) UpdateFromFile(context.Context, string) (string, error) { return "", nil }

func (r *rollbackUpdater) GetReleaseNotes(context.Context, string, string) (string, error) {
	return "", nil
}

func (r *rollbackUpdater) GetCurrentVersion() string { return "v1.2.0" }

func (r *rollbackUpdater) ReleaseBefore(context.Context, string) (string, error) {
	return r.before, r.beforeErr
}

func rollbackProps(io p.IO) *p.Props {
	return &p.Props{
		FS:      afero.NewMemMapFs(),
		Tool:    p.Tool{Name: "rollback-tool"},
		Logger:  logger.NewBuffer(),
		Version: version.NewInfo("v1.2.0", "head", "now"),
		IO:      io,
	}
}

func unattended() p.IO {
	return p.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
}

func attended(answer string) p.IO {
	return formtest.AccessibleTTY(formtest.Answers(answer))
}

func pinned(t *testing.T, props *p.Props) string {
	t.Helper()

	v, err := setup.GetPinnedVersion(props.FS, props.Tool.Name)
	require.NoError(t, err)

	return v
}

func noRefresh() update.UpdateConfigOption {
	var executed []string

	return update.WithExecCommand(exectest.TrackingCommand(&executed))
}

func TestRollback_UnattendedWithoutForceIsRefused(t *testing.T) {
	t.Setenv("CI", "")

	props := rollbackProps(unattended())
	r := &rollbackUpdater{before: "v1.1.0"}

	_, err := update.Rollback(context.Background(), props, "", false, io.Discard, r.factory(), noRefresh())

	require.ErrorIs(t, err, setup.ErrDowngradeNotConfirmed)
	assert.Contains(t, strings.Join(errors.GetAllHints(err), " "), "--force")
	assert.False(t, r.replaced)
	assert.Empty(t, pinned(t, props))
}

func TestRollback_ForcedResolvesTheReleaseBeforeAndPinsIt(t *testing.T) {
	t.Setenv("CI", "")

	props := rollbackProps(unattended())
	r := &rollbackUpdater{before: "v1.1.0"}

	result, err := update.Rollback(context.Background(), props, "", true, io.Discard, r.factory(), noRefresh())

	require.NoError(t, err)
	assert.True(t, r.replaced)
	assert.Equal(t, []string{"", "v1.1.0"}, r.built, "the second updater targets the release before")
	assert.True(t, result.Updated)
	assert.True(t, result.RolledBack)
	assert.Equal(t, "v1.1.0", result.NewVersion)
	assert.Equal(t, "v1.1.0", result.Pinned)
	assert.Equal(t, "v1.1.0", pinned(t, props), "a rollback pins its target (D5)")
}

func TestRollback_AttendedAsks(t *testing.T) {
	t.Setenv("CI", "")

	for _, tt := range []struct {
		answer   string
		replaced bool
	}{
		{answer: "y", replaced: true},
		{answer: "n", replaced: false},
	} {
		t.Run(tt.answer, func(t *testing.T) {
			props := rollbackProps(attended(tt.answer))
			r := &rollbackUpdater{}

			_, err := update.Rollback(context.Background(), props, "v1.1.0", false, io.Discard, r.factory(), noRefresh())

			assert.Equal(t, tt.replaced, r.replaced)

			if tt.replaced {
				require.NoError(t, err)
				assert.Equal(t, "v1.1.0", pinned(t, props))

				return
			}

			require.ErrorIs(t, err, setup.ErrDowngradeNotConfirmed)
			assert.Empty(t, pinned(t, props), "a declined rollback leaves no pin")
		})
	}
}

func TestRollback_ANamedTargetThatIsNotOlderIsRefused(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"v1.2.0", "v1.3.0"} {
		props := rollbackProps(unattended())
		r := &rollbackUpdater{}

		_, err := update.Rollback(context.Background(), props, target, true, io.Discard, r.factory(), noRefresh())

		require.Error(t, err, target)
		assert.Contains(t, strings.Join(errors.GetAllHints(err), " "), "--version", target)
		assert.Empty(t, r.built, "nothing is fetched for a refused target")
	}
}

func TestRollback_NothingEarlierIsReported(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	r := &rollbackUpdater{beforeErr: setup.ErrNoEarlierRelease}

	_, err := update.Rollback(context.Background(), props, "", true, io.Discard, r.factory(), noRefresh())

	require.ErrorIs(t, err, setup.ErrNoEarlierRelease)
	assert.False(t, r.replaced)
}

// Spec 0104 D3: every attended downgrade asks; an unattended --version
// downgrade proceeds, as it always has.
func TestUpdate_VersionDowngrade(t *testing.T) {
	t.Setenv("CI", "")

	t.Run("attended and declined", func(t *testing.T) {
		props := rollbackProps(attended("n"))
		r := &rollbackUpdater{}

		_, err := update.Update(context.Background(), props, "v1.1.0", false, io.Discard, r.factory(), noRefresh())

		require.ErrorIs(t, err, setup.ErrDowngradeNotConfirmed)
		assert.False(t, r.replaced)
	})

	t.Run("unattended proceeds without pinning", func(t *testing.T) {
		props := rollbackProps(unattended())
		r := &rollbackUpdater{}

		result, err := update.Update(context.Background(), props, "v1.1.0", false, io.Discard, r.factory(), noRefresh())

		require.NoError(t, err)
		assert.True(t, r.replaced)
		assert.True(t, result.RolledBack)
		assert.Empty(t, pinned(t, props), "only --rollback and --pin write a pin")
	})
}

func TestPin_TheRunningVersion(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	r := &rollbackUpdater{}

	result, err := update.Pin(context.Background(), props, "", false, io.Discard, r.factory(), noRefresh())

	require.NoError(t, err)
	assert.Empty(t, r.built, "pinning the running version makes no network call")
	assert.False(t, result.Updated)
	assert.Equal(t, "v1.2.0", result.Pinned)
	assert.Equal(t, "v1.2.0", pinned(t, props))
}

func TestPin_ANamedVersionInstallsThenPins(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	r := &rollbackUpdater{}

	result, err := update.Pin(context.Background(), props, "v1.3.0", false, io.Discard, r.factory(), noRefresh())

	require.NoError(t, err)
	assert.True(t, r.replaced)
	assert.Equal(t, "v1.3.0", result.Pinned)
	assert.Equal(t, "v1.3.0", pinned(t, props))
}

func TestPin_ARefusedVersionIsNotPinned(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	r := &rollbackUpdater{updateErr: errors.New("checksum mismatch")}

	_, err := update.Pin(context.Background(), props, "v1.3.0", false, io.Discard, r.factory(), noRefresh())

	require.Error(t, err)
	assert.Empty(t, pinned(t, props))
}

func TestUnpin(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.1.0"))

	result, err := update.Unpin(props)

	require.NoError(t, err)
	assert.Empty(t, result.Pinned)
	assert.Empty(t, pinned(t, props))
}

// Spec 0104 D7: update while pinned does nothing unless told to.
func TestUpdate_WhilePinned(t *testing.T) {
	t.Setenv("CI", "")

	t.Run("no flags is a no-op naming the pin", func(t *testing.T) {
		props := rollbackProps(unattended())
		require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.2.0"))
		r := &rollbackUpdater{latest: "v1.3.0"}

		result, err := update.Update(context.Background(), props, "", false, io.Discard, r.factory(), noRefresh())

		require.NoError(t, err)
		assert.Empty(t, r.built, "a pinned update makes no network call")
		assert.False(t, result.Updated)
		assert.Equal(t, "v1.2.0", result.Pinned)
		assert.True(t, props.Logger.(interface{ Contains(string) bool }).Contains("--unpin"))
	})

	t.Run("force updates and moves the pin", func(t *testing.T) {
		props := rollbackProps(unattended())
		require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.2.0"))
		r := &rollbackUpdater{latest: "v1.3.0"}

		result, err := update.Update(context.Background(), props, "", true, io.Discard, r.factory(), noRefresh())

		require.NoError(t, err)
		assert.True(t, r.replaced)
		assert.Equal(t, "v1.3.0", result.Pinned)
		assert.Equal(t, "v1.3.0", pinned(t, props))
	})

	t.Run("a named version moves the pin", func(t *testing.T) {
		props := rollbackProps(unattended())
		require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.2.0"))
		r := &rollbackUpdater{}

		_, err := update.Update(context.Background(), props, "v1.3.0", false, io.Discard, r.factory(), noRefresh())

		require.NoError(t, err)
		assert.Equal(t, "v1.3.0", pinned(t, props))
	})

	t.Run("a failed named version leaves the pin", func(t *testing.T) {
		props := rollbackProps(unattended())
		require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.2.0"))
		r := &rollbackUpdater{updateErr: errors.New("checksum mismatch")}

		_, err := update.Update(context.Background(), props, "v1.3.0", false, io.Discard, r.factory(), noRefresh())

		require.Error(t, err)
		assert.Equal(t, "v1.2.0", pinned(t, props))
	})
}

func TestNewCmdUpdate_FlagExclusivity(t *testing.T) {
	t.Parallel()

	flags := map[string]string{
		"from-file": "/tmp/release.tar.gz",
		"version":   "v1.0.0",
		"rollback":  "v1.0.0",
		"pin":       "v1.0.0",
		"unpin":     "true",
	}

	names := []string{"from-file", "version", "rollback", "pin", "unpin"}
	for i, a := range names {
		for _, b := range names[i+1:] {
			cmd := update.NewCmdUpdate(rollbackProps(unattended()))
			require.NoError(t, cmd.Flags().Set(a, flags[a]))
			require.NoError(t, cmd.Flags().Set(b, flags[b]))

			assert.Error(t, cmd.ValidateFlagGroups(), "%s with %s", a, b)
		}
	}
}

func TestNewCmdUpdate_Validation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{"--rollback=v1.2"}, want: "invalid version format"},
		{args: []string{"--pin=latest"}, want: "invalid version format"},
		{args: []string{"--rollback", "v1.1.0"}, want: "--rollback=v1.1.0"},
	} {
		cmd := update.NewCmdUpdate(rollbackProps(unattended()), (&rollbackUpdater{}).factory())
		cmd.SetArgs(tt.args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)

		err := cmd.Execute()

		require.Error(t, err, tt.args)
		assert.Contains(t, err.Error()+" "+strings.Join(errors.GetAllHints(err), " "), tt.want, tt.args)
	}
}

func TestUpdate_ACorruptPinIsIgnored(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	marker := setup.GetDefaultConfigDir(props.FS, props.Tool.Name) + "/pinned_version"
	require.NoError(t, afero.WriteFile(props.FS, marker, []byte("garbage"), 0o600))
	r := &rollbackUpdater{latest: "v1.3.0"}

	result, err := update.Update(context.Background(), props, "", false, io.Discard, r.factory(), noRefresh())

	require.NoError(t, err)
	assert.True(t, r.replaced, "a corrupt pin reads as no pin")
	assert.Empty(t, result.Pinned)
	assert.True(t, props.Logger.(interface{ Contains(string) bool }).Contains(marker), "the warning names the file")
}

func TestUpdate_APinThatCannotMoveStaysAndSaysSo(t *testing.T) {
	t.Parallel()

	props := rollbackProps(unattended())
	require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.2.0"))
	r := &rollbackUpdater{latest: "1.3"}

	result, err := update.Update(context.Background(), props, "", true, io.Discard, r.factory(), noRefresh())

	require.NoError(t, err, "the install itself succeeded")
	assert.Equal(t, "v1.2.0", result.Pinned)
	assert.Equal(t, "v1.2.0", pinned(t, props))
	assert.True(t, props.Logger.(interface{ Contains(string) bool }).Contains("could not pin"))
}
