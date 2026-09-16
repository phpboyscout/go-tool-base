package features

// ID is a feature's identity: its config and manifest name.
type ID string

// Global is the contribution target that applies to every feature, used for
// middleware that wraps every command.
const Global ID = ""

// Kind classifies a feature so "every forge" is a query rather than a list.
type Kind string

// Slot names a contribution channel. The core knows a slot is a name and a
// value; the consumer defines the slots and asserts the types.
type Slot string

// Descriptor is what the core needs to know about a feature. It is an
// interface so a consumer carries its own facts alongside (GTB adds the
// generated-code identifiers).
type Descriptor interface {
	FeatureID() ID
	FeatureKind() Kind
	// DefaultOn reports whether the feature is enabled when the tool expresses
	// no preference.
	DefaultOn() bool
	// IsDynamic reports whether a Backend may override the static state at
	// evaluation time (spec 0199 D7). A false answer keeps the feature off
	// every backend's path.
	IsDynamic() bool
}

// Ranked is an optional Descriptor capability: a descriptor that reports a
// rank (ok true) sorts before every unranked one, lowest first. It is how a
// consumer keeps a declared order for its own features while everything else
// sorts by kind and ID.
type Ranked interface {
	Rank() (rank int, ok bool)
}

// State is a tool's decision about one feature.
type State struct {
	ID      ID
	Enabled bool
}

// Mutator edits a list of states; Enable and Disable are the two.
type Mutator func([]State) []State

// Enable returns a Mutator that sets id on, replacing an earlier state for it.
func Enable(id ID) Mutator { return setState(id, true) }

// Disable returns a Mutator that sets id off, replacing an earlier state for it.
func Disable(id ID) Mutator { return setState(id, false) }

// Apply runs the mutators over states in order.
func Apply(states []State, mutators ...Mutator) []State {
	for _, m := range mutators {
		states = m(states)
	}

	return states
}

func setState(id ID, on bool) Mutator {
	return func(states []State) []State {
		for i, s := range states {
			if s.ID == id {
				states[i].Enabled = on

				return states
			}
		}

		return append(states, State{ID: id, Enabled: on})
	}
}
