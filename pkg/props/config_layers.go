package props

import "slices"

// ConfigLayer names one layer of the configuration stack.
//
// Which layers a tool wires used to be the framework's decision, inherited
// wholesale. That stops working as soon as credentials depend on layers: a
// regulated downstream that must not link a keychain backend, or a CI-only tool
// with no interactive environment, needs to decline a layer rather than have it
// wired on its behalf. See spec 0183 D4.
type ConfigLayer string

const (
	// LayerDefaults is the embedded defaults every tool ships — merged
	// assets/config.yaml plus each feature bundle's own defaults.
	LayerDefaults ConfigLayer = "defaults"
	// LayerFiles is the tool's config files, in ConfigPaths order.
	LayerFiles ConfigLayer = "files"
	// LayerProject is a discovered project-local ".<tool>.yaml", subject to the
	// trust filter.
	LayerProject ConfigLayer = "project"
	// LayerEnv is environment variables under the tool's EnvPrefix.
	LayerEnv ConfigLayer = "env"
	// LayerFlags is changed CLI flags — the highest-precedence layer.
	LayerFlags ConfigLayer = "flags"
)

// DefaultConfigLayers is what a tool wires when it declares nothing: exactly
// what the framework wired before the set became declarable.
//
// The order here is documentation, not mechanism — precedence is fixed by the
// store construction, so removing a layer cannot reorder the rest.
//
// The keychain layer is deliberately absent. Wiring it is a decision the host
// binary makes through a blank import, precisely so a regulated build can omit
// it and have the linker drop go-keyring entirely; a default that switched it
// on would take that choice away. See spec 0183 D3 and D9.
func DefaultConfigLayers() []ConfigLayer {
	return []ConfigLayer{
		LayerDefaults,
		LayerFiles,
		LayerProject,
		LayerEnv,
		LayerFlags,
	}
}

// AllConfigLayers is every layer a tool may declare, in precedence order. Used
// to validate a declaration and to enumerate the set in generated output.
func AllConfigLayers() []ConfigLayer {
	return DefaultConfigLayers()
}

// ResolveConfigLayers returns the layer set a tool actually wires: its own
// declaration, or the framework default when it declares none.
//
// An empty declaration means "unstated", not "none". A tool wanting genuinely
// no layers has nothing to configure and no reason to build a store, so reading
// empty as an opt-out would turn an omitted field into a silently broken tool.
func (t Tool) ResolveConfigLayers() []ConfigLayer {
	if len(t.ConfigLayers) == 0 {
		return DefaultConfigLayers()
	}

	return t.ConfigLayers
}

// WiresConfigLayer reports whether the tool wires the named layer.
func (t Tool) WiresConfigLayer(layer ConfigLayer) bool {
	return slices.Contains(t.ResolveConfigLayers(), layer)
}

// IsValidConfigLayer reports whether name is a layer this framework knows.
func IsValidConfigLayer(name ConfigLayer) bool {
	return slices.Contains(AllConfigLayers(), name)
}
