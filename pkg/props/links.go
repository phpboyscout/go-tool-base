package props

import (
	"strings"

	"gitlab.com/phpboyscout/go/features"
)

// Link prefixes name the features a generated project declares for the
// adapters it blank-imports (#81): one per chat provider the author chose and
// one per forge adapter, so the running tool's feature set records the
// author's exact choice rather than only which modules happen to be linked.
const (
	// ChatLinkPrefix prefixes a chat provider link: "chat-claude".
	ChatLinkPrefix = "chat-"
	// ForgeLinkPrefix prefixes a forge adapter link: "forge-github".
	ForgeLinkPrefix = "forge-"
)

// DeclareLinks declares one link-kind feature per name under prefix on the
// default registry. A generated cmd/<name>/chat.go or forge.go calls it from
// init beside the blank imports it records, so the declaration and the link
// are the same file. It panics on a duplicate, as any init-time declaration
// does.
func DeclareLinks(prefix string, names ...string) {
	if err := DeclareLinksOn(features.Default(), prefix, names...); err != nil {
		panic(err)
	}
}

// DeclareLinksOn is DeclareLinks against a caller's registry. A link defaults
// on: its presence in the binary is its enablement (spec 0199 OQ3), and it
// carries no generated-code constant because nothing scaffolds a toggle for
// it.
func DeclareLinksOn(r features.Registry, prefix string, names ...string) error {
	for _, name := range names {
		if err := registerFeature(r, FeatureDescriptor{
			ID:      FeatureID(prefix + name),
			Kind:    KindLink,
			Default: true,
		}); err != nil {
			return err
		}
	}

	return nil
}

// LinkedNames returns the names declared as links under prefix and enabled in
// set, in the set's order, with the prefix stripped: the chat providers or
// forge adapters this binary declared it links. Empty when the tool declared
// none, which a hand-wired tool may not have.
func LinkedNames(set features.Set, prefix string) []string {
	var names []string

	for _, d := range set.EnabledDescriptors() {
		if d.FeatureKind() != KindLink {
			continue
		}

		if id := string(d.FeatureID()); strings.HasPrefix(id, prefix) {
			names = append(names, strings.TrimPrefix(id, prefix))
		}
	}

	return names
}
