package authn

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"

	"github.com/cockroachdb/errors"
)

// KeyEntry pairs a valid API key with the Subject label recorded on success.
type KeyEntry struct {
	Key     string // the shared secret
	Subject string // identity label, e.g. "ci-runner", "admin"
}

// apiKeyVerifier accepts any of a fixed set of keys, comparing in constant time.
type apiKeyVerifier struct {
	digests  [][sha256.Size]byte
	subjects []string
}

// NewAPIKeyVerifier returns a Verifier that accepts any of the given keys.
//
// Comparison is constant-time. Because comparing variable-length input against a
// secret with subtle.ConstantTimeCompare is itself length-leaking (it
// short-circuits on unequal lengths), the verifier compares a fixed-width
// SHA-256 digest of the presented credential against the pre-computed digest of
// each configured key, iterating ALL entries with no early return — so neither
// match/no-match, key length, nor which key matched is timing-distinguishable.
//
// An empty key set is a construction error (fail-closed: never accept "any
// key"), as is an entry with an empty key.
func NewAPIKeyVerifier(entries ...KeyEntry) (Verifier, error) {
	if len(entries) == 0 {
		return nil, errors.New("authn: API-key verifier requires at least one key (fail-closed)")
	}

	v := &apiKeyVerifier{
		digests:  make([][sha256.Size]byte, len(entries)),
		subjects: make([]string, len(entries)),
	}

	for i, e := range entries {
		if e.Key == "" {
			return nil, errors.Newf("authn: API-key entry %d has an empty key", i)
		}

		v.digests[i] = sha256.Sum256([]byte(e.Key))
		v.subjects[i] = e.Subject
	}

	return v, nil
}

func (v *apiKeyVerifier) Verify(_ context.Context, credential string) (*Identity, error) {
	presented := sha256.Sum256([]byte(credential))

	// Branchless, constant-time scan over every entry: the work and timing do
	// not depend on which key (if any) matched.
	matched := -1

	for i := range v.digests {
		eq := subtle.ConstantTimeCompare(presented[:], v.digests[i][:])
		matched = subtle.ConstantTimeSelect(eq, i, matched)
	}

	if matched < 0 {
		return nil, errors.WithStack(ErrUnauthenticated)
	}

	return &Identity{Subject: v.subjects[matched], Method: "apikey"}, nil
}
