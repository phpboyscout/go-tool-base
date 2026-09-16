package templates

import (
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"

	// The catalogue is derived from what this binary declares, so the generator
	// links every scaffoldable feature: the forges and the keychain link. A
	// downstream tool's own registrations are not GTB's to scaffold and are
	// filtered by kind (spec 0184 D6, spec 0199 D6).
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"
)

// scaffoldableKinds are the kinds a generated project may select. Everything
// the framework offers a tool is one of these; a plugin kind registered by a
// downstream is that downstream's business.
var scaffoldableKinds = []props.FeatureKind{props.KindBuiltin, props.KindForge, props.KindLink}

// Catalogue is the ordered name<->constant<->default table for every
// scaffoldable feature, derived from the feature registry rather than written
// out (spec 0199 D6). The SetFeatures renderer, the manifest scanner, the
// feature toggles and the forge chooser all read it, so the mapping has one
// origin. The order is the snapshot's: built-ins as their constant block
// declares them, then the rest by kind and ID.
//
// It was a hand-written table until the registry stopped sealing on read:
// deriving it at package init would have made every later RegisterFeature
// panic. A snapshot is safe to take at any time.
func Catalogue() []props.FeatureDescriptor {
	return CatalogueIn(features.Default().Snapshot())
}

// CatalogueIn is Catalogue over an enumeration of the caller's choosing.
func CatalogueIn(e props.Enumerator) []props.FeatureDescriptor {
	var out []props.FeatureDescriptor

	for _, d := range props.DescriptorsIn(e) {
		for _, k := range scaffoldableKinds {
			if d.Kind == k {
				out = append(out, d)

				break
			}
		}
	}

	return out
}

// CatalogueEntry returns the catalogue row for name, if it is scaffoldable.
func CatalogueEntry(name string) (props.FeatureDescriptor, bool) {
	for _, d := range Catalogue() {
		if string(d.ID) == name {
			return d, true
		}
	}

	return props.FeatureDescriptor{}, false
}
