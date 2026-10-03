package generate

import (
	"fmt"
	"slices"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// minWizardSourceSlots is how many slot pages the Configuration page offers
// beyond those already declared; huh has no add-another field, so each is a
// page shown once the one before it has a kind.
const minWizardSourceSlots = 6

const (
	sourceAccessWritable = "writable"
	sourceAccessReadOnly = "read-only"
)

// wizardSource is one slot as the Configuration page edits it. Access is
// empty for the kind's default (D7, D12); Above is the built-in layer the slot
// sits directly above.
type wizardSource struct {
	Kind     string
	Name     string
	Required bool
	Access   string
	Above    string
}

func (s wizardSource) name() string {
	if s.Name == "" {
		return s.Kind
	}

	return s.Name
}

var formatAdds = map[string]string{
	"toml": "adds pelletier/go-toml",
	"json": "adds tidwall/gjson and sjson",
	"hcl":  "adds hashicorp/hcl and go-cty",
}

var layerLabels = map[string]string{
	string(props.LayerDefaults): "the embedded defaults",
	string(props.LayerFiles):    "the user's config files",
	string(props.LayerProject):  "the project file",
	string(props.LayerEnv):      "environment variables",
	string(props.LayerFlags):    "flags",
}

// configurationGroups is the Configuration page (spec 0204 D9): what the
// tool links and in what order, never how a source connects.
func (o *SkeletonOptions) configurationGroups() []*huh.Group {
	o.prepareSourceSlots()

	if o.ConfigFormat == "" {
		o.ConfigFormat = "yaml"
	}

	groups := make([]*huh.Group, 0, 1+2*len(o.sourceSlots))
	groups = append(groups, o.configFormatsGroup())

	for i := range o.sourceSlots {
		groups = append(groups, o.sourceKindGroup(i), o.sourceDetailGroup(i))
	}

	return groups
}

func (o *SkeletonOptions) configFormatsGroup() *huh.Group {
	formats := make([]huh.Option[string], 0, len(generator.ConfigFormats()))
	for _, f := range generator.ConfigFormats() {
		adds := formatAdds[f]
		if adds == "" {
			adds = "nothing beyond go/config"
		}

		if !generator.IsWritableConfigFormat(f) {
			adds = "read-only, " + adds
		}

		formats = append(formats, huh.NewOption(f+": "+adds, f))
	}

	own := func() []huh.Option[string] {
		opts := []huh.Option[string]{huh.NewOption("yaml", "yaml")}

		for _, f := range o.ConfigFormats {
			if generator.IsWritableConfigFormat(f) {
				opts = append(opts, huh.NewOption(f, f))
			}
		}

		return opts
	}

	return huh.NewGroup(
		huh.NewMultiSelect[string]().Key("config-formats").Title("Config formats").
			Description("YAML is built in. Each format chosen is linked into the binary.").
			Options(formats...).Value(&o.ConfigFormats),
		huh.NewSelect[string]().Key("config-format").Title("The tool's own config file").
			Description("The format of the file `init` writes and `config set` edits.").
			Options(own()...).OptionsFunc(own, &o.ConfigFormats).Value(&o.ConfigFormat).
			Validate(func(f string) error { return hintedValidation(generator.ValidateConfigFormats(o.ConfigFormats, f)) }),
	).
		Title("Configuration").
		Description("Recorded under properties.config in the manifest.\n")
}

func (o *SkeletonOptions) sourceKindGroup(i int) *huh.Group {
	kinds := make([]huh.Option[string], 0, 1+len(generator.ConfigSourceKinds()))
	kinds = append(kinds, huh.NewOption("No more config sources", ""))

	for _, k := range generator.ConfigSourceKinds() {
		kinds = append(kinds, huh.NewOption(kindLabel(k), k))
	}

	return huh.NewGroup(
		huh.NewSelect[string]().Key(fmt.Sprintf("config-source-%d-kind", i)).
			Title(fmt.Sprintf("Config source %d", i+1)).
			Description("A named slot in the config stack. Where it connects is the user's, set with `<tool> init config <name>`.").
			Options(kinds...).Value(&o.sourceSlots[i].Kind),
	).
		Title("Configuration: sources").
		WithHideFunc(func() bool { return i > 0 && o.sourceSlots[i-1].Kind == "" })
}

func kindLabel(kind string) string {
	switch {
	case generator.IsOverrideOnlySourceKind(kind):
		return kind + ": needs author code, see the override how-to"
	case kind == "keychain":
		return kind + ": set up with init config; may prompt for an unlock at startup"
	default:
		return kind + ": set up with init config"
	}
}

func (o *SkeletonOptions) sourceDetailGroup(i int) *huh.Group {
	slot := &o.sourceSlots[i]

	access := func() []huh.Option[string] {
		def := "Read-only, the default"
		if slot.Kind == "keychain" {
			def = "Writable, the keychain's default"
		}

		return []huh.Option[string]{
			huh.NewOption(def, ""),
			huh.NewOption("Writable", sourceAccessWritable),
			huh.NewOption("Read-only", sourceAccessReadOnly),
		}
	}

	return huh.NewGroup(
		huh.NewInput().Key(fmt.Sprintf("config-source-%d-name", i)).Title("Name").
			Description("A lower-case command word, unique among the slots. Empty means the kind's name.").
			PlaceholderFunc(func() string { return slot.Kind }, &slot.Kind).
			Value(&slot.Name).
			Validate(func(v string) error { return o.validateSlotName(i, v) }),
		huh.NewConfirm().Key(fmt.Sprintf("config-source-%d-required", i)).
			Title("Required?").Description("A required source that cannot be reached stops the tool starting.").
			Affirmative("Yes").Negative("No").Value(&slot.Required),
		huh.NewSelect[string]().Key(fmt.Sprintf("config-source-%d-access", i)).Title("Writable?").
			Options(access()...).OptionsFunc(access, &slot.Kind).Value(&slot.Access),
		huh.NewSelect[string]().Key(fmt.Sprintf("config-source-%d-placement", i)).Title("Precedence").
			Description("Where the source sits in the stack; a higher layer overrides a lower one.").
			Options(o.placementOptions()...).Value(&slot.Above),
	).
		Title("Configuration: sources").
		WithHideFunc(func() bool { return slot.Kind == "" || (i > 0 && o.sourceSlots[i-1].Kind == "") })
}

// validateSlotName checks slot i's name against the slots before it, with
// the rules the manifest is held to.
func (o *SkeletonOptions) validateSlotName(i int, v string) error {
	sources := make([]generator.ManifestConfigSource, 0, i+1)
	for _, s := range o.sourceSlots[:i] {
		sources = append(sources, generator.ManifestConfigSource{Name: s.name(), Kind: s.Kind})
	}

	current := o.sourceSlots[i]
	current.Name = v
	sources = append(sources, generator.ManifestConfigSource{Name: current.name(), Kind: current.Kind})

	return hintedValidation(generator.ValidateConfigSources(sources, nil, nil))
}

func (o *SkeletonOptions) placementOptions() []huh.Option[string] {
	builtins := o.builtinLayers()
	opts := make([]huh.Option[string], 0, len(builtins)-1)

	for k := 0; k < len(builtins)-1; k++ {
		label := fmt.Sprintf("Above %s, below %s", layerLabels[builtins[k]], layerLabels[builtins[k+1]])
		opts = append(opts, huh.NewOption(label, builtins[k]))
	}

	return opts
}

// declaredBuiltinLayers is the built-in part of a stated layer list, or nil
// when the tool states none.
func (o *SkeletonOptions) declaredBuiltinLayers() []string {
	var out []string

	for _, l := range o.ConfigLayers {
		if props.IsValidConfigLayer(props.ConfigLayer(l)) {
			out = append(out, l)
		}
	}

	return out
}

func (o *SkeletonOptions) builtinLayers() []string {
	if declared := o.declaredBuiltinLayers(); declared != nil {
		return declared
	}

	var out []string
	for _, l := range props.DefaultConfigLayers() {
		out = append(out, string(l))
	}

	return out
}

// prepareSourceSlots loads the declared slots into the page's state, with
// empty slots after them to add to.
func (o *SkeletonOptions) prepareSourceSlots() {
	if o.sourceSlots != nil {
		return
	}

	declared, _ := o.configSources()

	for _, s := range declared {
		slot := wizardSource{Kind: s.Kind, Name: s.Name, Required: s.Required == nil || *s.Required, Above: o.layerBelow(s.Name)}
		if s.Writable != nil {
			slot.Access = map[bool]string{true: sourceAccessWritable, false: sourceAccessReadOnly}[*s.Writable]
		}

		o.sourceSlots = append(o.sourceSlots, slot)
	}

	for len(o.sourceSlots) < max(minWizardSourceSlots, len(declared)+1) {
		o.sourceSlots = append(o.sourceSlots, wizardSource{Required: true, Above: string(props.LayerDefaults)})
	}
}

// layerBelow is the built-in layer a slot sits directly above.
func (o *SkeletonOptions) layerBelow(name string) string {
	below := string(props.LayerDefaults)

	for _, l := range o.ConfigLayers {
		if l == name {
			break
		}

		if props.IsValidConfigLayer(props.ConfigLayer(l)) {
			below = l
		}
	}

	return below
}

// applySourceSlots turns the page's slots back into the flags' shape and the
// layer list.
func (o *SkeletonOptions) applySourceSlots() {
	if o.sourceSlots == nil {
		return
	}

	active := o.activeSourceSlots()
	o.recordSourceSlots(active)
	o.ConfigLayers = o.slotLayers(active)
}

// activeSourceSlots are the slots up to the first left without a kind.
func (o *SkeletonOptions) activeSourceSlots() []wizardSource {
	end := slices.IndexFunc(o.sourceSlots, func(s wizardSource) bool { return s.Kind == "" })
	if end < 0 {
		end = len(o.sourceSlots)
	}

	return o.sourceSlots[:end]
}

func (o *SkeletonOptions) recordSourceSlots(active []wizardSource) {
	o.ConfigSources, o.ConfigSourcesOptional, o.ConfigSourcesWritable, o.readOnlySources = nil, nil, nil, nil

	for _, s := range active {
		o.ConfigSources = append(o.ConfigSources, s.name()+"="+s.Kind)

		if !s.Required {
			o.ConfigSourcesOptional = append(o.ConfigSourcesOptional, s.name())
		}

		switch s.Access {
		case sourceAccessWritable:
			o.ConfigSourcesWritable = append(o.ConfigSourcesWritable, s.name())
		case sourceAccessReadOnly:
			o.readOnlySources = append(o.readOnlySources, s.name())
		}
	}
}

// slotLayers is the layer list with each slot above its built-in layer.
// Slots placed only above the defaults in an unstated stack record no list,
// since R9 places them there.
func (o *SkeletonOptions) slotLayers(active []wizardSource) []string {
	declared := o.declaredBuiltinLayers()
	allDefault := !slices.ContainsFunc(active, func(s wizardSource) bool { return s.Above != string(props.LayerDefaults) })

	if len(active) == 0 || (declared == nil && allDefault) {
		return declared
	}

	var layers []string

	for _, b := range o.builtinLayers() {
		layers = append(layers, b)

		for _, s := range active {
			if s.Above == b {
				layers = append(layers, s.name())
			}
		}
	}

	return layers
}
