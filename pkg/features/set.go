package features

import (
	"context"
	"fmt"

	"gitlab.com/phpboyscout/go/errors"
)

// Resolver turns a Snapshot and a tool's states into a Set. The default
// applies every descriptor's DefaultOn, then the states in order; a consumer
// with another precedence supplies its own.
type Resolver interface {
	Resolve(s Snapshot, states []State) (Set, error)
}

// Set is what a tool has: the resolved enabled state of every declared
// feature, the contributions of the enabled ones, and a static Evaluator.
type Set interface {
	Evaluator
	Enabled(id ID) bool
	Descriptors() []Descriptor
	EnabledDescriptors() []Descriptor
	// Contributions returns the values registered under id and slot when id
	// is enabled, and nothing otherwise.
	Contributions(id ID, slot Slot) []any
	// Ignored lists the disable states that named an ID the snapshot lacks
	// (spec 0199 OQ2): you can switch off what you did not link, and doctor
	// reports that you did.
	Ignored() []State
}

// StateResolver is the default Resolver.
type StateResolver struct{}

// DefaultResolver returns the default Resolver.
func DefaultResolver() *StateResolver { return &StateResolver{} }

// Resolve is DefaultResolver().Resolve.
func Resolve(s Snapshot, states []State) (Set, error) {
	return DefaultResolver().Resolve(s, states)
}

// Resolve applies defaults then states. Enabling an unknown ID is an error;
// disabling one is ignored and listed.
func (StateResolver) Resolve(s Snapshot, states []State) (Set, error) {
	set := &resolvedSet{snapshot: s, enabled: map[ID]bool{}}

	for _, d := range s.Descriptors() {
		set.enabled[d.FeatureID()] = d.DefaultOn()
	}

	for _, st := range states {
		if _, known := s.Lookup(st.ID); !known {
			if st.Enabled {
				return nil, errors.Wrapf(ErrUnknownFeature, "cannot enable %q: no such feature is declared", st.ID)
			}

			set.ignored = append(set.ignored, st)

			continue
		}

		set.enabled[st.ID] = st.Enabled
	}

	return set, nil
}

type resolvedSet struct {
	snapshot Snapshot
	enabled  map[ID]bool
	ignored  []State
}

func (r *resolvedSet) Enabled(id ID) bool { return r.enabled[id] }

func (r *resolvedSet) Descriptors() []Descriptor { return r.snapshot.Descriptors() }

func (r *resolvedSet) EnabledDescriptors() []Descriptor {
	var out []Descriptor

	for _, d := range r.snapshot.Descriptors() {
		if r.enabled[d.FeatureID()] {
			out = append(out, d)
		}
	}

	return out
}

func (r *resolvedSet) Contributions(id ID, slot Slot) []any {
	if id != Global && !r.enabled[id] {
		return nil
	}

	return r.snapshot.Contributions(id, slot)
}

func (r *resolvedSet) Ignored() []State { return r.ignored }

// Evaluate answers from the static state (spec 0199 D10): a Set is the
// Evaluator a tool holds until it wires a Dynamic one.
func (r *resolvedSet) Evaluate(_ context.Context, id ID, _ EvalContext) (Decision, error) {
	on, known := r.enabled[id]
	if !known {
		return Decision{Reason: ReasonError}, errors.Wrapf(ErrUnknownFeature, "%q", id)
	}

	return Decision{Enabled: on, Reason: ReasonStatic}, nil
}

// ContributionsOf returns the contributions under id and slot that are a T,
// and an error naming the feature and slot when any is not. The well-typed
// values are returned either way so a reader can carry on.
func ContributionsOf[T any](s Set, id ID, slot Slot) ([]T, error) {
	values := s.Contributions(id, slot)
	out := make([]T, 0, len(values))

	var err error

	for _, v := range values {
		typed, ok := v.(T)
		if !ok {
			err = errors.Join(err, errors.Wrapf(ErrContributionType,
				"feature %q slot %q: got %T, want %s", id, slot, v, typeName[T]()))

			continue
		}

		out = append(out, typed)
	}

	return out, err
}

func typeName[T any]() string {
	var zero T

	return fmt.Sprintf("%T", zero)
}
