package credentialposture

import (
	"context"
	"sort"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// SlotCredential is the feature-registry slot a credential descriptor is
// contributed under, keyed by the feature that consumes it (spec 0199 OQ5). A
// descriptor with no feature is contributed under features.Global.
const SlotCredential features.Slot = "credential"

// Register declares a credential for posture reporting: the extension point
// spec 0189 Q6 settled on. A bundle declares the credential it owns, and every
// reporting surface picks it up from the feature registry without a second
// list to keep in step.
//
// Three such lists existed before this and none of them knew about the others:
// doctor's LiteralCredentialKeys, config migrate's knownCredentials (whose own
// comment asks the reader to keep them in sync by hand) and the forge
// profiles. A credential stored under a key nobody had added was invisible.
// A downstream tool built on GTB registers its own credentials the same way,
// so its secrets are reported rather than silently unexamined.
//
// Registering the same owner and literal key twice replaces the earlier entry
// rather than duplicating it, so a tool that re-registers during a second
// wiring pass does not produce two reports for one credential.
func Register(d Descriptor) {
	RegisterOn(features.Default(), d)
}

// RegisterOn is Register against a caller's registry.
func RegisterOn(r features.Registry, d Descriptor) {
	r.Contribute(featureOf(d), SlotCredential, d)
}

func featureOf(d Descriptor) features.ID {
	if d.Feature == "" {
		return features.Global
	}

	return features.ID(d.Feature)
}

func registryKey(d Descriptor) string { return d.Owner + "|" + d.LiteralKey }

// Registered returns every declared credential, ordered by owner then label so
// a report is stable between runs. It reads a snapshot of the default
// registry: every credential this binary declared, whichever features the
// tool enabled. DeclaredFor is the enabled-only view.
func Registered() []Descriptor {
	return RegisteredIn(features.Default().Snapshot())
}

// RegisteredIn is Registered over a snapshot.
func RegisteredIn(s features.Snapshot) []Descriptor {
	ids := s.Contributed()
	out := make([]Descriptor, 0, len(ids))

	for _, id := range ids {
		out = append(out, descriptorsIn(s.Contributions(id, SlotCredential))...)
	}

	return dedupeAndSort(out)
}

// DeclaredFor returns the credentials of the features enabled in set, plus
// those declared under no feature, so doctor speaks only about what the tool
// has (#55) with no predicate of its own.
func DeclaredFor(set features.Set) []Descriptor {
	out := descriptorsIn(set.Contributions(features.Global, SlotCredential))

	for _, d := range set.EnabledDescriptors() {
		out = append(out, descriptorsIn(set.Contributions(d.FeatureID(), SlotCredential))...)
	}

	return dedupeAndSort(out)
}

func descriptorsIn(values []any) []Descriptor {
	out := make([]Descriptor, 0, len(values))

	for _, v := range values {
		if d, ok := v.(Descriptor); ok {
			out = append(out, d)
		}
	}

	return out
}

// dedupeAndSort keeps the last registration for each owner and literal key,
// then orders by owner and label.
func dedupeAndSort(ds []Descriptor) []Descriptor {
	byKey := make(map[string]Descriptor, len(ds))
	for _, d := range ds {
		byKey[registryKey(d)] = d
	}

	out := make([]Descriptor, 0, len(byKey))
	for _, d := range byKey {
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
	return ReportWhere(ctx, cfg, func(Descriptor) bool { return true })
}

// ReportWhere is ReportAll over the declared descriptors include admits.
func ReportWhere(ctx context.Context, cfg Reader, include func(Descriptor) bool) []Result {
	return Report(ctx, cfg, Registered(), include)
}

// ReportFor resolves posture for the credentials of the features enabled in
// set (DeclaredFor), further narrowed by include: it is what lets doctor speak
// only about what the tool has (#55) and, for a chat credential, a provider
// that consumes it linked (spec 0196 D12).
func ReportFor(ctx context.Context, cfg Reader, set features.Set, include func(Descriptor) bool) []Result {
	return Report(ctx, cfg, DeclaredFor(set), include)
}

// Report resolves posture for the given descriptors that include admits.
func Report(ctx context.Context, cfg Reader, descriptors []Descriptor, include func(Descriptor) bool) []Result {
	results := make([]Result, 0, len(descriptors))

	for _, d := range descriptors {
		if include != nil && !include(d) {
			continue
		}

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
