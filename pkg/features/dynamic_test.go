package features_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// fakeBackend is a scripted Backend: an answer per ID, a readiness switch, a
// call counter and a change channel.
type fakeBackend struct {
	answers map[features.ID]features.Decision
	errs    map[features.ID]error
	ready   atomic.Bool
	calls   atomic.Int32
	changes chan features.Change
	initErr error
	initFor time.Duration
	closed  atomic.Bool
}

func (f *fakeBackend) Resolve(_ context.Context, id features.ID, fallback bool, _ features.EvalContext) (features.Decision, error) {
	f.calls.Add(1)

	if err, ok := f.errs[id]; ok {
		return features.Decision{Enabled: fallback, Reason: features.ReasonError}, err
	}

	if d, ok := f.answers[id]; ok {
		if d.Reason == features.ReasonDisabled {
			d.Enabled = fallback
		}

		return d, nil
	}

	return features.Decision{Enabled: fallback, Reason: features.ReasonError}, features.ErrUnknownFeature
}

func (f *fakeBackend) Init(ctx context.Context) error {
	if f.initFor > 0 {
		select {
		case <-time.After(f.initFor):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if f.initErr != nil {
		return f.initErr
	}

	f.ready.Store(true)

	return nil
}

func (f *fakeBackend) Ready() bool                                  { return f.ready.Load() }
func (f *fakeBackend) Watch(context.Context) <-chan features.Change { return f.changes }
func (f *fakeBackend) Close() error                                 { f.closed.Store(true); return nil }

func dynamicFixture(t *testing.T) (features.Set, *fakeBackend) {
	t.Helper()

	r := registry(t,
		desc{id: "update", kind: builtin, on: true},
		desc{id: "beta", kind: "plugin", dynamic: true},
		desc{id: "killed", kind: "plugin", on: false, dynamic: true},
		desc{id: "missing", kind: "plugin", dynamic: true},
	)

	set, err := features.Resolve(r.Snapshot(), []features.State{{ID: "killed", Enabled: true}})
	require.NoError(t, err)

	b := &fakeBackend{
		answers: map[features.ID]features.Decision{
			"beta":   {Enabled: true, Reason: features.ReasonTargeting, Variant: "on"},
			"killed": {Reason: features.ReasonDisabled},
		},
		errs: map[features.ID]error{"missing": features.ErrUnknownFeature},
	}
	b.ready.Store(true)

	return set, b
}

// TestDynamic_Evaluate is spec 0199 D7 and D10 end to end: static-only
// features never reach the backend; a dynamic one is resolved with the Set
// state as fallback; disabled is a clean answer that applies the fallback;
// an error or an unknown flag falls back with the error alongside.
func TestDynamic_Evaluate(t *testing.T) {
	t.Parallel()

	set, b := dynamicFixture(t)

	var fallbacks []features.ID

	d := features.Dynamic(set, b, features.WithOnFallback(func(id features.ID, _ error) { fallbacks = append(fallbacks, id) }))
	ctx := context.Background()

	dec, err := d.Evaluate(ctx, "update", features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonStatic}, dec)
	assert.Equal(t, int32(0), b.calls.Load(), "a static-only feature never consults the backend")

	dec, err = d.Evaluate(ctx, "beta", features.EvalContext{TargetingKey: "u1"})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonTargeting, Variant: "on"}, dec)

	dec, err = d.Evaluate(ctx, "killed", features.EvalContext{})
	require.NoError(t, err, "disabled in the backend is a clean answer")
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonDisabled}, dec, "the Set state (on) applies")

	dec, err = d.Evaluate(ctx, "missing", features.EvalContext{})
	require.Error(t, err)
	assert.Equal(t, features.Decision{Enabled: false, Reason: features.ReasonFallback}, dec)

	dec, err = d.Evaluate(ctx, "nope", features.EvalContext{})
	require.ErrorIs(t, err, features.ErrUnknownFeature)
	assert.Equal(t, features.ReasonError, dec.Reason)

	assert.Equal(t, []features.ID{"missing"}, fallbacks)
}

func TestDynamic_NotReadyFallsBack(t *testing.T) {
	t.Parallel()

	set, b := dynamicFixture(t)
	b.ready.Store(false)

	d := features.Dynamic(set, b)

	dec, err := d.Evaluate(context.Background(), "beta", features.EvalContext{})
	require.ErrorIs(t, err, features.ErrBackendNotReady)
	assert.Equal(t, features.Decision{Enabled: false, Reason: features.ReasonFallback}, dec)
	assert.Equal(t, int32(0), b.calls.Load(), "a backend that is not ready is not asked")
}

func TestDynamic_InitTimeoutLeavesTheEvaluatorUsable(t *testing.T) {
	t.Parallel()

	set, b := dynamicFixture(t)
	b.ready.Store(false)
	b.initFor = time.Second

	d := features.Dynamic(set, b, features.WithInitTimeout(20*time.Millisecond))

	err := d.Init(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)

	dec, err := d.Evaluate(context.Background(), "beta", features.EvalContext{})
	require.ErrorIs(t, err, features.ErrBackendNotReady)
	assert.Equal(t, features.ReasonFallback, dec.Reason)

	require.NoError(t, d.Close())
	assert.True(t, b.closed.Load())
}

func TestDynamic_InitAndWatchPassThrough(t *testing.T) {
	t.Parallel()

	set, b := dynamicFixture(t)
	b.ready.Store(false)
	b.changes = make(chan features.Change, 1)

	d := features.Dynamic(set, b)
	require.NoError(t, d.Init(context.Background()))
	assert.True(t, d.Ready())

	b.changes <- features.Change{ID: "beta"}
	assert.Equal(t, features.Change{ID: "beta"}, <-d.Watch(context.Background()))

	b.initErr = errors.New("boom")
	b.ready.Store(false)
	require.Error(t, d.Init(context.Background()))
}

// TestSetBackend: the Set as a Backend, so Dynamic can be exercised with no
// vendor at all, and every answer is the static one.
func TestSetBackend(t *testing.T) {
	t.Parallel()

	set, _ := dynamicFixture(t)
	b := features.SetBackend(set)

	require.NoError(t, b.Init(context.Background()))
	assert.True(t, b.Ready())
	assert.Nil(t, b.Watch(context.Background()), "a static backend pushes nothing")

	dec, err := b.Resolve(context.Background(), "killed", false, features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonStatic}, dec)

	d := features.Dynamic(set, b)
	dec, err = d.Evaluate(context.Background(), "beta", features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: false, Reason: features.ReasonStatic}, dec)

	require.NoError(t, b.Close())
}
