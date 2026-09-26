package props

import (
	"regexp"
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

	// Sources are the config source slots the tool declares, each placed by
	// name in Layers (spec 0204 D15). Where each connects is runtime
	// configuration under config.sources.<name>, never declared here.
	Sources []ConfigSource `json:"sources,omitempty" yaml:"sources,omitempty"`
}

// ConfigSource is one declared config source slot.
type ConfigSource struct {
	// Name is unique in the stack, a config key segment and a command word:
	// it names the slot's layer, its settings and `init config <name>`.
	Name string `json:"name" yaml:"name"`
	// Kind picks the adapter: vault, consul, aws-s3, file, ...
	Kind string `json:"kind" yaml:"kind"`
	// Required, when nil, is true: an unreachable or unconfigured slot stops
	// the tool (spec 0204 D6).
	Required *bool `json:"required,omitempty" yaml:"required,omitempty"`
	// Writable, when nil, is the kind's default, which is read-only for every
	// kind but keychain (spec 0204 D7, D12).
	Writable *bool `json:"writable,omitempty" yaml:"writable,omitempty"`
}

// IsRequired reports whether the slot's absence stops the tool.
func (s ConfigSource) IsRequired() bool {
	return s.Required == nil || *s.Required
}

// IsWritable reports whether config writes may land in the slot, given its
// kind's default.
func (s ConfigSource) IsWritable(kindDefault bool) bool {
	if s.Writable == nil {
		return kindDefault
	}

	return *s.Writable
}

// sourceNamePattern is a config key segment that is also a command word.
var sourceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

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
	// ErrConfigSource is a source slot declared in a way spec 0204 D15
	// refuses.
	ErrConfigSource = errors.NewSentinel("gtb.props.config_source", "config source declaration")
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

// ResolvedConfigSpec is Config with its layer list resolved, the declaration
// the store builds from.
func (t Tool) ResolvedConfigSpec() ConfigSpec {
	spec := t.Config
	spec.Layers = t.ResolveConfigLayers()

	return spec
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
	return validateLayers(layers, nil)
}

// ValidateConfigSpec refuses a declaration spec 0204 D1 or D15 refuses: its
// layers as ValidateConfigLayers does, with each source's name a layer too,
// and every source a valid, unique, placed slot.
func ValidateConfigSpec(spec ConfigSpec) error {
	names := make([]ConfigLayer, 0, len(spec.Sources))

	for _, s := range spec.Sources {
		if err := validateSource(s, names); err != nil {
			return err
		}

		names = append(names, ConfigLayer(s.Name))
	}

	if len(names) > 0 && len(spec.Layers) == 0 {
		return errors.WithHint(errors.Wrap(ErrConfigSource, "sources declared with no layer list"),
			"a source's place in the stack is its precedence, so the layer list must place every one")
	}

	if err := validateLayers(spec.Layers, names); err != nil {
		return err
	}

	for _, n := range names {
		if !slices.Contains(spec.Layers, n) {
			return errors.WithHint(errors.Wrapf(ErrConfigSource, "%q is not in the layer list", string(n)),
				"a source's place in the stack is its precedence, so the layer list must place every one")
		}
	}

	return nil
}

func validateSource(s ConfigSource, earlier []ConfigLayer) error {
	switch {
	case !sourceNamePattern.MatchString(s.Name):
		return errors.WithHint(errors.Wrapf(ErrConfigSource, "name %q", s.Name),
			"a source name is lower-case letters, digits and hyphens, starting with a letter: it is a config key and a command word")
	case IsValidConfigLayer(ConfigLayer(s.Name)):
		return errors.WithHint(errors.Wrapf(ErrConfigSource, "name %q", s.Name),
			"a source cannot take a built-in layer's name, since the layer list holds both")
	case slices.Contains(earlier, ConfigLayer(s.Name)):
		return errors.Wrapf(ErrConfigSource, "%q is declared twice", s.Name)
	case s.Kind == "":
		return errors.Wrapf(ErrConfigSource, "%q has no kind", s.Name)
	}

	return nil
}

func validateLayers(layers, sources []ConfigLayer) error {
	seen := make(map[ConfigLayer]bool, len(layers))

	for _, l := range layers {
		if !IsValidConfigLayer(l) && !slices.Contains(sources, l) {
			return errors.WithHintf(errors.Wrapf(ErrUnknownConfigLayer, "%q", string(l)),
				"known layers: %s", joinLayers(append(AllConfigLayers(), sources...)))
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
