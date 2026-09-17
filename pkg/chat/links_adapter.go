package chat

import (
	"slices"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// LinkedProviders is what this binary says it links, in the order the
// author chose: the chat providers declared as link features (a generated
// tool declares one per provider in cmd/<name>/chat.go, #81), or, for a tool
// that declared none, every provider the linked modules register. The two
// differ when a module carries more providers than the author selected;
// doctor and the init wizard speak about the author's choice.
//
// Declared is the set's answer, Unregistered the declared providers whose
// module is not in the binary: a build mistake, never a configuration one.
func LinkedProviders(set features.Set) (linked, unregistered []gochat.Provider) {
	return LinkedProvidersIn(set, gochat.RegisteredProviders())
}

// LinkedProvidersIn is LinkedProviders over an explicit registry listing, so
// a test states what is registered rather than registering it.
func LinkedProvidersIn(set features.Set, registered []gochat.Provider) (linked, unregistered []gochat.Provider) {
	declared := props.LinkedNames(set, props.ChatLinkPrefix)
	if len(declared) == 0 {
		return registered, nil
	}

	for _, name := range declared {
		p := gochat.Provider(name)
		if slices.Contains(registered, p) {
			linked = append(linked, p)
		} else {
			unregistered = append(unregistered, p)
		}
	}

	return linked, unregistered
}
