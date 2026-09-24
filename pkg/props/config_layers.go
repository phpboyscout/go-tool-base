package props

import (
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
)

// ConfigSpec is how a tool declares its configuration stack. See spec 0204.
type ConfigSpec struct {
	// Layers is the stack by name, lowest precedence first: the order is the
	// precedence. Empty resolves to DefaultConfigLayers.
	Layers []ConfigLayer `json:"layers,omitempty" yaml:"layers,omitempty"`

	// Format is the format of the tool's own config file, the one init writes
	// and config set edits: yaml, toml, json or hcl. Empty is yaml. A format
	// other than yaml must be linked (spec 0204 D2).
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
}

// writableConfigFormats are the formats a tool's own file may be in, each
// with the extension its file is named by.
var writableConfigFormats = map[string]string{
	"":     "yaml",
	"yaml": "yaml",
	"toml": "toml",
	"json": "json",
	"hcl":  "hcl",
}

// ConfigFilename is the base name of the tool's own config file: config
// plus its format's extension.
func (t Tool) ConfigFilename() string {
	ext, ok := writableConfigFormats[t.Config.Format]
	if !ok {
		ext = "yaml"
	}

	return "config." + ext
}

// ValidateConfigFormat refuses an own format that is not one of the writable
// four, since init writes the file and config set edits it.
func ValidateConfigFormat(format string) error {
	if _, ok := writableConfigFormats[format]; ok {
		return nil
	}

	return errors.WithHint(errors.Wrapf(ErrConfigFormat, "%q", format),
		"a tool's own config file is yaml, toml, json or hcl; a read-only format can still be read through --config")
}

var (
	// ErrUnknownConfigLayer is a declared layer the framework does not know.
	ErrUnknownConfigLayer = errors.NewSentinel("gtb.props.unknown_config_layer", "unknown config layer")
	// ErrDuplicateConfigLayer is a layer declared more than once.
	ErrDuplicateConfigLayer = errors.NewSentinel("gtb.props.duplicate_config_layer", "duplicate config layer")
	// ErrConfigLayerOrder is a declared order that breaks one of spec 0204
	// D1's constraints.
	ErrConfigLayerOrder = errors.NewSentinel("gtb.props.config_layer_order", "config layer order")
	// ErrConfigFormat is an own config format that is not writable.
	ErrConfigFormat = errors.NewSentinel("gtb.props.config_format", "config format cannot be a tool's own")
)

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
// what the framework wired before the set became declarable, in the same
// order.
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

// AllConfigLayers is every layer a tool may declare, in the framework's order.
// Used to validate a declaration and to enumerate the set in generated output.
func AllConfigLayers() []ConfigLayer {
	return DefaultConfigLayers()
}

// ResolveConfigLayers returns the layers a tool wires, lowest precedence
// first: Config.Layers as declared, else the deprecated ConfigLayers in the
// framework's order, else the framework default.
//
// An empty declaration means "unstated", not "none". A tool wanting genuinely
// no layers has nothing to configure and no reason to build a store, so reading
// empty as an opt-out would turn an omitted field into a silently broken tool.
func (t Tool) ResolveConfigLayers() []ConfigLayer {
	switch {
	case len(t.Config.Layers) > 0:
		return t.Config.Layers
	case len(t.ConfigLayers) > 0:
		// Its order never reached the store, so honouring it now would
		// silently change what an existing tool resolves.
		return CanonicalConfigLayers(t.ConfigLayers)
	default:
		return DefaultConfigLayers()
	}
}

// CanonicalConfigLayers returns a copy of layers in the framework's order,
// which is what a declaration made before spec 0204 resolved to.
func CanonicalConfigLayers(layers []ConfigLayer) []ConfigLayer {
	all := AllConfigLayers()
	sorted := slices.Clone(layers)

	slices.SortStableFunc(sorted, func(a, b ConfigLayer) int {
		return slices.Index(all, a) - slices.Index(all, b)
	})

	return sorted
}

// WiresConfigLayer reports whether the tool wires the named layer.
func (t Tool) WiresConfigLayer(layer ConfigLayer) bool {
	return slices.Contains(t.ResolveConfigLayers(), layer)
}

// IsValidConfigLayer reports whether name is a layer this framework knows.
func IsValidConfigLayer(name ConfigLayer) bool {
	return slices.Contains(AllConfigLayers(), name)
}

// ValidateConfigLayers refuses a declaration naming an unknown or repeated
// layer, or breaking one of spec 0204 D1's ordering constraints. Each
// constraint is a way to make a tool quietly unsafe, so each refusal says which
// one and why it exists.
func ValidateConfigLayers(layers []ConfigLayer) error {
	seen := make(map[ConfigLayer]bool, len(layers))

	for _, l := range layers {
		if !IsValidConfigLayer(l) {
			return errors.WithHintf(errors.Wrapf(ErrUnknownConfigLayer, "%q", string(l)),
				"known layers: %s", joinLayers(AllConfigLayers()))
		}

		if seen[l] {
			return errors.WithHint(errors.Wrapf(ErrDuplicateConfigLayer, "%q", string(l)),
				"a layer's one position in the list is its precedence")
		}

		seen[l] = true
	}

	return validateConfigLayerOrder(layers)
}

func validateConfigLayerOrder(layers []ConfigLayer) error {
	if i := slices.Index(layers, LayerDefaults); i > 0 {
		return errors.WithHint(errors.Wrap(ErrConfigLayerOrder, "defaults must be the lowest layer"),
			"a layer below the compiled-in defaults can never be read")
	}

	if i := slices.Index(layers, LayerFlags); i >= 0 && i != len(layers)-1 {
		return errors.WithHint(errors.Wrap(ErrConfigLayerOrder, "flags must be the highest layer"),
			"a layer above flags means a flag the user passed does not take")
	}

	project := slices.Index(layers, LayerProject)
	if env := slices.Index(layers, LayerEnv); project >= 0 && env >= 0 && project > env {
		return errors.WithHint(errors.Wrap(ErrConfigLayerOrder, "project must sit below env"),
			"the trust filter assumes a repository's file cannot outrank the environment")
	}

	return nil
}

func joinLayers(layers []ConfigLayer) string {
	names := make([]string, len(layers))
	for i, l := range layers {
		names[i] = string(l)
	}

	return strings.Join(names, ", ")
}
