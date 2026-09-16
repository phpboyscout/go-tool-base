// Package flags is GTB's wiring for dynamic feature flags (spec 0199 D10,
// D11): a features.Backend over the tool's own config store, so a service's
// flags can be flipped by editing its config with no vendor at all, and the
// controls registration that gives any backend the controller's lifecycle.
package flags

import (
	"context"
	"sync"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/controls"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// ErrNotABool reports a features.<id>.enabled value that is not a boolean.
var ErrNotABool = errors.NewSentinel("gtb.flags.not_a_bool", "flags: a feature's enabled value must be a boolean")

// ConfigKey is the config path the config-store backend reads for a feature:
// features.<id>.enabled. Absent means "no override".
func ConfigKey(id features.ID) string {
	return "features." + string(id) + ".enabled"
}

// ConfigBackend resolves dynamic features from the tool's config store. It
// takes a fresh view on every Resolve, so a reload is visible at once and
// there is no cache to go stale (the spike measured the view read at 135 ns
// either way). A store reload is forwarded on Watch as "everything may have
// changed", since the store does not say which keys moved.
type ConfigBackend struct {
	store *config.Store

	mu      sync.Mutex
	changes chan features.Change
}

// NewConfigBackend builds a backend over store.
func NewConfigBackend(store *config.Store) *ConfigBackend {
	return &ConfigBackend{store: store}
}

// Resolve reads features.<id>.enabled. An absent key is a clean Disabled
// answer (the fallback, the Set state, applies); a present boolean is the
// backend's answer; anything else is ErrNotABool.
func (b *ConfigBackend) Resolve(_ context.Context, id features.ID, fallback bool, _ features.EvalContext) (features.Decision, error) {
	view := b.store.View()
	key := ConfigKey(id)

	if !view.Has(key) {
		return features.Decision{Enabled: fallback, Reason: features.ReasonDisabled}, nil
	}

	on, ok := view.Get(key).(bool)
	if !ok {
		return features.Decision{Enabled: fallback, Reason: features.ReasonError}, errors.Wrapf(ErrNotABool, "%s", key)
	}

	return features.Decision{Enabled: on, Reason: features.ReasonDefault}, nil
}

// Init subscribes to the store's reloads. The store is loaded before it is
// handed to anything, so the backend is ready at once.
func (b *ConfigBackend) Init(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.changes != nil {
		return nil
	}

	b.changes = make(chan features.Change, 1)
	b.store.AddObserverFunc(func(config.Observed) error {
		// One pending notice is enough: the next Evaluate reads the store.
		select {
		case b.changes <- features.Change{}:
		default:
		}

		return nil
	})

	return nil
}

// Ready is always true: a store is loaded before anything holds it, so there
// is nothing to wait for. Init only wires the change notices.
func (b *ConfigBackend) Ready() bool { return true }

// Watch emits a Change on every successful reload of the store.
func (b *ConfigBackend) Watch(context.Context) <-chan features.Change {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.changes
}

// Close releases nothing: the store's lifecycle is the tool's.
func (b *ConfigBackend) Close() error { return nil }

// RegisterBackend registers backend as a controls service named name, so its
// Init runs on the controller's start and its Close on stop (spec 0199 D10):
// a streaming vendor backend then opens and closes with the service that
// evaluates against it.
func RegisterBackend(ctrl *controls.Controller, name string, backend features.Backend) {
	ctrl.Register(name,
		controls.WithStart(backend.Init),
		controls.WithStopErr(func(context.Context) error { return backend.Close() }),
	)
}
