package update_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0217 D4: every route that installs a binary refuses a managed install
// first, before the pin, the release source and any prompt.
func TestInstall_RefusesAnEnvManagedInstallFirst(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv(setup.InstallManagerEnvVariable, "mise")
	t.Setenv(setup.InstallUpgradeEnvVariable, "mise upgrade rollback-tool")

	routes := map[string]func(context.Context, *p.Props, *rollbackUpdater) (*update.UpdateResult, error){
		"update": func(ctx context.Context, props *p.Props, r *rollbackUpdater) (*update.UpdateResult, error) {
			return update.Update(ctx, props, "", true, io.Discard, r.factory(), noRefresh())
		},
		"update --version": func(ctx context.Context, props *p.Props, r *rollbackUpdater) (*update.UpdateResult, error) {
			return update.Update(ctx, props, "v1.3.0", false, io.Discard, r.factory(), noRefresh())
		},
		"update --rollback": func(ctx context.Context, props *p.Props, r *rollbackUpdater) (*update.UpdateResult, error) {
			return update.Rollback(ctx, props, "", false, io.Discard, r.factory(), noRefresh())
		},
		"update --pin <version>": func(ctx context.Context, props *p.Props, r *rollbackUpdater) (*update.UpdateResult, error) {
			return update.Pin(ctx, props, "v1.1.0", false, io.Discard, r.factory(), noRefresh())
		},
	}

	for name, run := range routes {
		t.Run(name, func(t *testing.T) {
			props := rollbackProps(attended("y"))
			require.NoError(t, setup.SetPinnedVersion(props.FS, props.Tool.Name, "v1.2.0"))
			r := &rollbackUpdater{latest: "v1.3.0", before: "v1.1.0"}

			_, err := run(t.Context(), props, r)

			require.ErrorIs(t, err, setup.ErrManagedInstall)
			assert.Contains(t, err.Error(), "managed by mise (GTB_INSTALL_MANAGER)")
			assert.Contains(t, errors.FlattenHints(err), "mise upgrade rollback-tool")
			assert.Empty(t, r.built, "no updater is built, so no release source is asked")
			assert.False(t, r.replaced)
			assert.Equal(t, "v1.2.0", pinned(t, props), "the pin is left as it was")
		})
	}
}

// Spec 0217 D4: the managed refusal comes before a pin that cannot be kept.
func TestInstall_RefusesAnEnvManagedInstallBeforeCanPin(t *testing.T) {
	t.Setenv(setup.InstallManagerEnvVariable, "mise")

	props := rollbackProps(unattended())
	checkoutHome(t, props)
	r := &rollbackUpdater{before: "v1.1.0"}

	_, err := update.Rollback(t.Context(), props, "", true, io.Discard, r.factory(), noRefresh())
	require.ErrorIs(t, err, setup.ErrManagedInstall)

	_, err = update.Pin(t.Context(), props, "v1.1.0", true, io.Discard, r.factory(), noRefresh())
	require.ErrorIs(t, err, setup.ErrManagedInstall)
}

// Spec 0217 D4: bare --pin and --unpin install nothing and stay allowed.
func TestPinAndUnpin_StayAllowedWhenManaged(t *testing.T) {
	t.Setenv(setup.InstallManagerEnvVariable, "mise")

	props := rollbackProps(unattended())

	_, err := update.Pin(t.Context(), props, "", false, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", pinned(t, props))

	_, err = update.Unpin(props)
	require.NoError(t, err)
	assert.Empty(t, pinned(t, props))
}

func TestUpdateFromFile_RefusesAnEnvManagedInstallFirst(t *testing.T) {
	t.Setenv(setup.InstallManagerEnvVariable, "mise")

	props := rollbackProps(unattended())
	built := false

	cmd := update.NewCmdUpdate(props, update.WithOfflineUpdater(func(*p.Props) update.Updater {
		built = true

		return nil
	}))
	cmd.SetArgs([]string{"--from-file", "/tmp/release.tar.gz"})

	err := cmd.Execute()
	require.ErrorIs(t, err, setup.ErrManagedInstall)
	assert.False(t, built, "the offline updater is never built")
}
