package transportcfg

import (
	"fmt"
	"strings"
)

// SharedPortKey is the port every transport falls back to when its own block
// sets none.
const SharedPortKey = "server.port"

// Selection carries the GTB config-selection knobs every *FromReader adapter
// accepts: which config block to read, and an optional explicit port that
// bypasses the lookup. The http and grpc packages each expose it through
// their own ServerOption type so callers never import this package.
type Selection struct {
	Prefix string
	Port   *int
}

// ResolvedPrefix returns the selected config block, or def when none was set.
func (s Selection) ResolvedPrefix(def string) string {
	if s.Prefix == "" {
		return def
	}

	return s.Prefix
}

// SplitOptions partitions a `...any` option bag into the GTB selection (every
// value of type G is applied to it), the transport's own options (every value
// of type T, in order) and anything else, which the caller rejects or warns
// about rather than dropping: a silently discarded middleware chain is a
// security footgun.
func SplitOptions[G ~func(*Selection), T any](opts []any) (sel Selection, transportOpts []T, unknown []any) {
	for _, o := range opts {
		switch v := o.(type) {
		case G:
			v(&sel)
		case T:
			transportOpts = append(transportOpts, v)
		default:
			unknown = append(unknown, v)
		}
	}

	return sel, transportOpts, unknown
}

// UnknownOptionTypes formats the concrete types of unrecognised option values
// for an error or warning message.
func UnknownOptionTypes(unknown []any) string {
	types := make([]string, 0, len(unknown))
	for _, u := range unknown {
		types = append(types, fmt.Sprintf("%T", u))
	}

	return strings.Join(types, ", ")
}

// Select applies every G in opts to the selection and returns the rest in
// order, for adapters whose transport takes the same `...any` families and
// so has nothing GTB can call unknown.
func Select[G ~func(*Selection)](opts []any) (sel Selection, rest []any) {
	for _, o := range opts {
		if v, ok := o.(G); ok {
			v(&sel)

			continue
		}

		rest = append(rest, o)
	}

	return sel, rest
}
