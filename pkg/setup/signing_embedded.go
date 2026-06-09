package setup

import (
	"context"

	"github.com/cockroachdb/errors"
)

// embeddedResolver is a KeyResolver backed by a fixed set of
// ASCII-armored public keys, parsed and validated once at construction.
// It performs no I/O at Resolve time and never returns a Resolve error;
// strength validation happens in the constructor.
type embeddedResolver struct {
	trustSet *TrustSet
}

// NewEmbeddedResolver returns a KeyResolver backed by the supplied
// ASCII-armored public keys. The keys are parsed and validated against
// the minimum-strength policy at construction time; if any key is weak
// or any input fails to parse, the constructor panics. This matches
// the spec's "binary refuses to start" invariant for a weak or
// malformed embedded key: such a problem must surface at startup, not
// at the first update attempt.
//
// Tool authors typically call this indirectly via an internal
// trustkeys helper that embeds the project's public keys with
// //go:embed and constructs the resolver at package init.
func NewEmbeddedResolver(armoredKeys ...[]byte) KeyResolver {
	r, err := newEmbeddedResolverChecked(armoredKeys...)
	if err != nil {
		panic(errors.Wrap(err, "NewEmbeddedResolver: invalid embedded keys"))
	}

	return r
}

// newEmbeddedResolverChecked is the error-returning form of
// NewEmbeddedResolver. It is used by BuildKeyResolver, which
// constructs the resolver lazily at NewUpdater time rather than at
// package init: a malformed key there must surface as an error from
// NewUpdater, not a panic mid-update.
func newEmbeddedResolverChecked(armoredKeys ...[]byte) (KeyResolver, error) {
	ts, err := LoadTrustSet(armoredKeys...)
	if err != nil {
		return nil, errors.Wrap(err, "invalid embedded keys")
	}

	return &embeddedResolver{trustSet: ts}, nil
}

// Name returns the diagnostic identifier for this resolver.
func (r *embeddedResolver) Name() string {
	return "embedded"
}

// Resolve returns the pre-loaded TrustSet. No I/O, no error path.
func (r *embeddedResolver) Resolve(_ context.Context) (*TrustSet, error) {
	return r.trustSet, nil
}
