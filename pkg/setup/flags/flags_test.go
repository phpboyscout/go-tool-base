package flags_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
	"gitlab.com/phpboyscout/go/controls"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/flags"
)

func dynamicSet(t *testing.T) features.Set {
	t.Helper()

	r := features.NewRegistry()
	require.NoError(t, r.Declare(props.FeatureDescriptor{ID: "update", ConstName: "UpdateCmd", ConstPackage: "x", Kind: props.KindBuiltin, Default: true}))
	require.NoError(t, r.Declare(props.FeatureDescriptor{ID: "beta", ConstName: "Beta", ConstPackage: "x", Kind: "plugin", Dynamic: true}))

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	return set
}

// TestConfigBackend_ReadsAndReloads is spec 0199 T7 for the config-store
// backend: an absent key leaves the static state standing (Disabled, no
// override), a present key is the backend's answer, and a change applied to
// the store reaches the next Evaluate through Watch with no cache between.
func TestConfigBackend_ReadsAndReloads(t *testing.T) {
	t.Parallel()

	// A file-backed store, so Apply has a writable layer and its reload fires
	// the observers the backend listens to.
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/cfg/config.yaml", []byte("log:\n  level: info\n"), 0o600))

	store, err := config.NewStore(context.Background(), config.WithFiles(configafero.Wrap(fs), "/cfg/config.yaml"))
	require.NoError(t, err)

	set := dynamicSet(t)

	b := flags.NewConfigBackend(store)
	d := features.Dynamic(set, b)

	ctx := context.Background()
	require.NoError(t, d.Init(ctx))
	assert.True(t, d.Ready())

	dec, err := d.Evaluate(ctx, "beta", features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: false, Reason: features.ReasonDisabled}, dec, "no key: the static state applies")

	changes := d.Watch(ctx)
	require.NotNil(t, changes)

	_, err = store.Apply(ctx, config.Set(flags.ConfigKey("beta"), true))
	require.NoError(t, err)

	select {
	case ch := <-changes:
		assert.Equal(t, features.Change{}, ch, "a reload may have changed anything")
	case <-time.After(2 * time.Second):
		t.Fatal("no change notified after Apply")
	}

	dec, err = d.Evaluate(ctx, "beta", features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonDefault}, dec, "the store's value is the backend's answer")

	dec, err = d.Evaluate(ctx, "update", features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.ReasonStatic, dec.Reason, "a static-only feature never reaches the store")

	require.NoError(t, d.Close())
}

func TestConfigBackend_RefusesANonBool(t *testing.T) {
	t.Parallel()

	store := testutil.StoreFromYAML(t, "features:\n  beta:\n    enabled: sometimes\n")
	d := features.Dynamic(dynamicSet(t), flags.NewConfigBackend(store))

	dec, err := d.Evaluate(context.Background(), "beta", features.EvalContext{})
	require.ErrorIs(t, err, flags.ErrNotABool)
	assert.Equal(t, features.ReasonFallback, dec.Reason)
}

// TestRegisterBackend_RidesTheController is spec 0199 T8: a backend registered
// as a controls service has Init called on start and Close on stop, so a
// service's flags follow its lifecycle.
func TestRegisterBackend_RidesTheController(t *testing.T) {
	t.Parallel()

	rec := &recordingBackend{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := controls.NewController(ctx, controls.WithLogger(logger.ToSlog(logger.NewNoop())))
	flags.RegisterBackend(ctrl, "flags", rec)

	ctrl.Start()
	require.Eventually(t, func() bool { return rec.inited.Load() }, 5*time.Second, 10*time.Millisecond, "Init rode the controller's start")

	ctrl.Stop()
	require.Eventually(t, func() bool { return ctrl.IsStopped() && rec.closed.Load() }, 5*time.Second, 10*time.Millisecond, "Close rode the controller's stop")
}
