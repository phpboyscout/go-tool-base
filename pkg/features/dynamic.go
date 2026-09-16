package features

import (
	"context"
	"time"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrBackendNotReady reports a Backend that has not finished Init (or has gone
// stale); the Set answered instead.
var ErrBackendNotReady = errors.NewSentinel("gtb.features.backend_not_ready", "features: flag backend is not ready")

// Change is a Backend's notice that a flag's value may have moved. An empty
// ID means everything may have changed.
type Change struct {
	ID ID
}

// Backend resolves a dynamic feature given the caller's fallback: the value it
// hands back for a flag it does not know or has disabled, which is OpenFeature's
// defaultValue and this package's Set state (spec 0199 D10). It carries the
// lifecycle a vendor SDK needs: Init before the first Resolve, Ready while
// answers are trustworthy, Watch for pushed updates (nil when it does not
// push), Close on shutdown.
type Backend interface {
	Resolve(ctx context.Context, id ID, fallback bool, ec EvalContext) (Decision, error)
	Init(ctx context.Context) error
	Ready() bool
	Watch(ctx context.Context) <-chan Change
	Close() error
}

// DynamicOption configures Dynamic.
type DynamicOption func(*DynamicEvaluator)

// WithInitTimeout bounds Init: a short-lived process gives its backend a short
// budget and evaluates with fallbacks if it is not met. Zero means no bound.
func WithInitTimeout(d time.Duration) DynamicOption {
	return func(e *DynamicEvaluator) { e.initTimeout = d }
}

// WithOnFallback is called each time an evaluation falls back to the Set
// because the backend errored, was not ready, or did not know the flag; a
// service logs it.
func WithOnFallback(fn func(id ID, err error)) DynamicOption {
	return func(e *DynamicEvaluator) { e.onFallback = fn }
}

// DynamicEvaluator is the production Evaluator: a static-only feature is
// answered from the Set without consulting the backend; a dynamic one is
// resolved by the backend with the Set state as its fallback, and on any
// error, a not-ready backend, or an unknown flag the Set answers with
// ReasonFallback and the error alongside (spec 0199 D10). It is exported so a
// consumer can embed it.
type DynamicEvaluator struct {
	set         Set
	backend     Backend
	dynamic     map[ID]bool
	initTimeout time.Duration
	onFallback  func(id ID, err error)
}

// Dynamic builds the production Evaluator over set and backend. Which
// features are dynamic is fixed here from the Set's descriptors, so an
// evaluation is a map lookup before it decides whether to ask the backend.
func Dynamic(set Set, backend Backend, opts ...DynamicOption) *DynamicEvaluator {
	e := &DynamicEvaluator{set: set, backend: backend, dynamic: map[ID]bool{}}

	for _, d := range set.Descriptors() {
		if d.IsDynamic() {
			e.dynamic[d.FeatureID()] = true
		}
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Evaluate implements Evaluator.
func (e *DynamicEvaluator) Evaluate(ctx context.Context, id ID, ec EvalContext) (Decision, error) {
	static, err := e.set.Evaluate(ctx, id, ec)
	if err != nil {
		return static, err
	}

	if !e.dynamic[id] {
		return static, nil
	}

	if !e.backend.Ready() {
		return e.fallback(id, static, errors.Wrapf(ErrBackendNotReady, "%q", id))
	}

	dec, err := e.backend.Resolve(ctx, id, static.Enabled, ec)
	if err != nil {
		return e.fallback(id, static, err)
	}

	return dec, nil
}

func (e *DynamicEvaluator) fallback(id ID, static Decision, err error) (Decision, error) {
	if e.onFallback != nil {
		e.onFallback(id, err)
	}

	static.Reason = ReasonFallback

	return static, err
}

// Init initialises the backend within the configured timeout. An error leaves
// the evaluator usable: it answers with fallbacks until the backend is Ready.
func (e *DynamicEvaluator) Init(ctx context.Context) error {
	if e.initTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, e.initTimeout)
		defer cancel()
	}

	return e.backend.Init(ctx)
}

// Ready reports the backend's readiness.
func (e *DynamicEvaluator) Ready() bool { return e.backend.Ready() }

// Watch is the backend's change channel; nil when it does not push.
func (e *DynamicEvaluator) Watch(ctx context.Context) <-chan Change { return e.backend.Watch(ctx) }

// Close closes the backend.
func (e *DynamicEvaluator) Close() error { return e.backend.Close() }

// StaticBackend is a Backend over a Set: every answer is the static one. It is
// what lets Dynamic be tested and wired with no vendor at all.
type StaticBackend struct {
	set Set
}

// SetBackend wraps set as a Backend.
func SetBackend(set Set) *StaticBackend { return &StaticBackend{set: set} }

// Resolve answers from the Set.
func (b *StaticBackend) Resolve(ctx context.Context, id ID, _ bool, ec EvalContext) (Decision, error) {
	return b.set.Evaluate(ctx, id, ec)
}

// Init is a no-op.
func (b *StaticBackend) Init(context.Context) error { return nil }

// Ready is always true.
func (b *StaticBackend) Ready() bool { return true }

// Watch returns nil: a Set never changes.
func (b *StaticBackend) Watch(context.Context) <-chan Change { return nil }

// Close is a no-op.
func (b *StaticBackend) Close() error { return nil }
