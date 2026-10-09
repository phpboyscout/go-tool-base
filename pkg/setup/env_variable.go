package setup

import (
	"slices"

	"gitlab.com/phpboyscout/go/features"
)

// AccessibleEnvVariable asks for line prompts on any stdin. Every tool reads
// it by this name, whatever its prefix (spec 0198 D1).
const AccessibleEnvVariable = "GTB_ACCESSIBLE"

// SlotEnvVariable carries the name of an environment variable a binary reads
// directly rather than through the config store.
const SlotEnvVariable features.Slot = "env-variable"

// DeclareEnvVariable records on the default registry that this binary reads
// name directly. When name also falls under the tool's prefix it maps to a
// config key nobody declares, and doctor would otherwise report it as stray
// (spec 0205 D4).
func DeclareEnvVariable(name string) {
	DeclareEnvVariableOn(features.Default(), name)
}

// DeclareEnvVariableOn is DeclareEnvVariable on r.
func DeclareEnvVariableOn(r features.Registry, name string) {
	r.Contribute(features.Global, SlotEnvVariable, name)
}

// DeclaredEnvVariables lists the variables read by name: the framework's own
// and every one declared in set, sorted and once each.
func DeclaredEnvVariables(set features.Set) []string {
	names := []string{AccessibleEnvVariable}

	for _, v := range set.Contributions(features.Global, SlotEnvVariable) {
		if name, ok := v.(string); ok {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	return slices.Compact(names)
}
