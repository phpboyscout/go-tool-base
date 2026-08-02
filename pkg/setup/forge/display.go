package forge

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Display is the subset of a forge's definition that user-facing choosers need
// — most immediately the project generator's git-backend selector.
//
// It exists so callers outside this package can render a forge without
// [Profile] becoming public API. A Profile carries config-key layout, credential
// shape, fallback env vars and token-creation URLs; none of that is any of the
// generator's business, and exporting it to supply four display strings would
// freeze the whole surface. See spec 0185 D5.
type Display struct {
	// ID is the forge's feature identity, and the value --git-backend takes.
	ID props.FeatureID
	// Label is the human-facing forge name (e.g. "GitLab").
	Label string
	// Host is the default host, empty for a forge that has none. Gitea is the
	// case in point: every instance is self-hosted.
	Host string
	// RepoDescription and RepoPlaceholder describe the repository path this
	// forge expects.
	RepoDescription string
	RepoPlaceholder string
	// NestedNamespaces marks a forge whose owner segment may contain slashes,
	// as GitLab subgroups do.
	NestedNamespaces bool
}

// profilesByFeature indexes every profile this package registers. It is built
// once at init from the same values the initialisers use, so a forge cannot
// appear in a chooser with display data that disagrees with its wizard.
//
//nolint:gochecknoglobals // the profile set is fixed at build time
var profilesByFeature = map[props.FeatureID]Profile{}

//nolint:gochecknoinits // index the profiles the other init registers
func init() {
	for _, p := range []Profile{gitHubProfile, gitLabProfile, giteaProfile, bitbucketProfile} {
		profilesByFeature[p.Feature] = p
	}
}

// DisplayFor returns the display data for a registered forge feature, and
// reports whether one exists. It is the only route from a feature ID to a
// forge's presentation, so a caller cannot half-know a forge.
func DisplayFor(id props.FeatureID) (Display, bool) {
	profile, ok := profilesByFeature[id]
	if !ok {
		return Display{}, false
	}

	return Display{
		ID:               profile.Feature,
		Label:            profile.Label,
		Host:             profile.Host,
		RepoDescription:  profile.RepoDescription,
		RepoPlaceholder:  profile.RepoPlaceholder,
		NestedNamespaces: profile.NestedNamespaces,
	}, true
}

// Displays returns every registered forge's display data, in the registry's
// deterministic order. A chooser built from this cannot offer a forge with no
// initialiser behind it, nor omit one that has an initialiser — which is how
// the generator came to offer GitLab, which had no credential path, while
// hiding Bitbucket, which did.
func Displays() []Display {
	ids := props.FeaturesOfKind(props.KindForge)
	out := make([]Display, 0, len(ids))

	for _, id := range ids {
		if d, ok := DisplayFor(id); ok {
			out = append(out, d)
		}
	}

	return out
}
