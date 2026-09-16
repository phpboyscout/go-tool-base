package features

import (
	"sync"

	"gitlab.com/phpboyscout/go/errors"
)

// Registry is where a package declares its features and contributions, and
// where a root takes its Snapshot. Default is the init-time instance; a test
// or a host builds its own with NewRegistry.
type Registry interface {
	Declare(d Descriptor) error
	Contribute(id ID, slot Slot, v any)
	Snapshot() Snapshot
}

// MemoryRegistry is the default Registry: append-only, mutex-guarded, never
// sealed. It is exported so a consumer can embed it.
type MemoryRegistry struct {
	mu            sync.RWMutex
	descriptors   []Descriptor
	contributions map[ID]map[Slot][]any
}

// NewRegistry returns an empty MemoryRegistry.
func NewRegistry() *MemoryRegistry {
	return &MemoryRegistry{contributions: map[ID]map[Slot][]any{}}
}

var defaultRegistry Registry = NewRegistry()

// Default is the process-wide registry that init-time declarations target.
// It is read-only by convention once main begins: a root snapshots it, and a
// test that needs its own features builds its own Registry.
func Default() Registry { return defaultRegistry }

// MustDeclare declares d on r and panics on a rejected descriptor. It is for
// init, where there is no caller to hand an error to and a silently dropped
// feature would surface much later as a missing entry somewhere else.
func MustDeclare(r Registry, d Descriptor) {
	if err := r.Declare(d); err != nil {
		panic(err)
	}
}

// Declare records d, refusing an incomplete descriptor or a second one for
// the same ID.
func (r *MemoryRegistry) Declare(d Descriptor) error {
	if d == nil || d.FeatureID() == "" || d.FeatureKind() == "" {
		return errors.Wrapf(ErrInvalidDescriptor, "%s", describe(d))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.descriptors {
		if existing.FeatureID() == d.FeatureID() {
			return errors.Wrapf(ErrDuplicateFeature, "%q", d.FeatureID())
		}
	}

	r.descriptors = append(r.descriptors, d)

	return nil
}

// Contribute records v under id and slot. A contribution for an ID nobody has
// declared is kept: declaration and contribution may come from different
// packages in either init order, and a Set only hands out the contributions
// of enabled (hence declared) features.
func (r *MemoryRegistry) Contribute(id ID, slot Slot, v any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slots, ok := r.contributions[id]
	if !ok {
		slots = map[Slot][]any{}
		r.contributions[id] = slots
	}

	slots[slot] = append(slots[slot], v)
}

// Snapshot returns an immutable copy of everything declared and contributed
// so far, in the total order Snapshot documents.
func (r *MemoryRegistry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return newSnapshot(r.descriptors, r.contributions)
}

func describe(d Descriptor) string {
	if d == nil {
		return "nil descriptor"
	}

	return "id=" + string(d.FeatureID()) + " kind=" + string(d.FeatureKind())
}
