package features

import (
	"cmp"
	"slices"
)

// Snapshot is an immutable view of a Registry at one moment. Descriptors are
// in a total order derived from data, never from init sequencing: Ranked
// descriptors first by rank, then the rest by kind and ID. Go orders init by
// dependency then filename, which is stable for one build but moves with the
// import graph, and reports and golden files depend on this order.
type Snapshot interface {
	Descriptors() []Descriptor
	OfKind(k Kind) []Descriptor
	Lookup(id ID) (Descriptor, bool)
	Contributions(id ID, slot Slot) []any
	// Contributed lists every ID that has at least one contribution, sorted,
	// Global included when it has any. It is how a reader enumerates without
	// assuming every contributor was also declared.
	Contributed() []ID
}

type snapshot struct {
	descriptors   []Descriptor
	byID          map[ID]Descriptor
	contributions map[ID]map[Slot][]any
}

func newSnapshot(descriptors []Descriptor, contributions map[ID]map[Slot][]any) *snapshot {
	s := &snapshot{
		descriptors:   slices.Clone(descriptors),
		byID:          make(map[ID]Descriptor, len(descriptors)),
		contributions: make(map[ID]map[Slot][]any, len(contributions)),
	}

	slices.SortStableFunc(s.descriptors, compareDescriptors)

	for _, d := range s.descriptors {
		s.byID[d.FeatureID()] = d
	}

	for id, slots := range contributions {
		copied := make(map[Slot][]any, len(slots))
		for slot, values := range slots {
			copied[slot] = slices.Clone(values)
		}

		s.contributions[id] = copied
	}

	return s
}

func compareDescriptors(a, b Descriptor) int {
	ar, aRanked := rankOf(a)
	br, bRanked := rankOf(b)

	switch {
	case aRanked && bRanked:
		if ar != br {
			return cmp.Compare(ar, br)
		}
	case aRanked:
		return -1
	case bRanked:
		return 1
	}

	if a.FeatureKind() != b.FeatureKind() {
		return cmp.Compare(a.FeatureKind(), b.FeatureKind())
	}

	return cmp.Compare(a.FeatureID(), b.FeatureID())
}

func rankOf(d Descriptor) (int, bool) {
	if r, ok := d.(Ranked); ok {
		return r.Rank()
	}

	return 0, false
}

func (s *snapshot) Descriptors() []Descriptor { return slices.Clone(s.descriptors) }

func (s *snapshot) OfKind(k Kind) []Descriptor {
	var out []Descriptor

	for _, d := range s.descriptors {
		if d.FeatureKind() == k {
			out = append(out, d)
		}
	}

	return out
}

func (s *snapshot) Lookup(id ID) (Descriptor, bool) {
	d, ok := s.byID[id]

	return d, ok
}

func (s *snapshot) Contributions(id ID, slot Slot) []any {
	return slices.Clone(s.contributions[id][slot])
}

func (s *snapshot) Contributed() []ID {
	ids := make([]ID, 0, len(s.contributions))

	for id, slots := range s.contributions {
		for _, values := range slots {
			if len(values) > 0 {
				ids = append(ids, id)

				break
			}
		}
	}

	slices.Sort(ids)

	return ids
}
