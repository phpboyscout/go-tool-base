package credentialposture

import (
	"context"
	"sort"
	"sync"
)

// The registry is the extension point spec 0189 Q6 settled on: a bundle
// declares the credential it owns, and every reporting surface picks it up
// without a second list to keep in step.
//
// Three such lists existed before this and none of them knew about the others:
// doctor's LiteralCredentialKeys, config migrate's knownCredentials — whose own
// comment asks the reader to keep them in sync by hand — and the forge
// profiles. A credential stored under a key nobody had added was invisible.
//
// A downstream tool built on GTB registers its own credentials the same way, so
// its secrets are reported rather than silently unexamined.
//
//nolint:gochecknoglobals // process-wide registry, guarded below
var (
	registryMu  sync.RWMutex
	registry    = map[string]Descriptor{}
	registryKey = func(d Descriptor) string { return d.Owner + "|" + d.LiteralKey }
)

// Register declares a credential for posture reporting.
//
// Registering the same owner and literal key twice replaces the earlier entry
// rather than duplicating it, so a tool that re-registers during a test or a
// second wiring pass does not produce two reports for one credential.
func Register(d Descriptor) {
	registryMu.Lock()
	defer registryMu.Unlock()

	registry[registryKey(d)] = d
}

// Registered returns every declared credential, ordered by owner then label so
// a report is stable between runs.
func Registered() []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()

	out := make([]Descriptor, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}

		return out[i].Label < out[j].Label
	})

	return out
}

// ReportAll resolves posture for every registered credential.
//
// The keychain rung bounds its own read (see readKeychain), so there is no
// deadline around the whole walk here. There used to be, with the same
// duration — and it meant a locked keychain consumed the credential's entire
// budget, so the walk aborted on a context error before it could judge the
// rungs below. The invariant that refuses a plaintext fallback could never fire
// in the one case it exists for.
//
// A resolution error is attached to its own credential rather than aborting the
// run: "this one is configured but broken" is a finding, and losing the other
// nine to it would be a worse report than any of them.
func ReportAll(ctx context.Context, cfg Reader) []Result {
	descriptors := Registered()
	results := make([]Result, 0, len(descriptors))

	for _, d := range descriptors {
		posture, err := Resolve(ctx, cfg, d)

		results = append(results, Result{Posture: posture, Err: err})
	}

	return results
}

// Result pairs one credential's posture with the error, if any, that stopped it
// resolving.
type Result struct {
	Posture Posture
	Err     error
}
