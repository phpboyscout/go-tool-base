package generate

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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

// slotName is slot i's name: as typed, else its kind, numbered from 2 when an
// earlier slot already has that name, so accepting the default never collides.
func (o *SkeletonOptions) slotName(i int) string {
	if o.sourceSlots[i].Name != "" {
		return o.sourceSlots[i].Name
	}

	return o.defaultSlotName(i)
}

func (o *SkeletonOptions) defaultSlotName(i int) string {
	taken := make([]string, 0, i)
	for j := range i {
		taken = append(taken, o.slotName(j))
	}

	kind := o.sourceSlots[i].Kind
	name := kind

	for n := 2; slices.Contains(taken, name); n++ {
		name = fmt.Sprintf("%s%d", kind, n)
	}

	return name
}

// kindGlosses say what each source kind reads, from its package doc.
var kindGlosses = map[string]string{
	"file":            "a fixed file beside the user's own",
	"keychain":        "tokens in the OS keychain; may prompt to unlock",
	"vault":           "a Vault KV v2 secret, or all under a prefix",
	"consul":          "the keys under a Consul KV prefix",
	"aws-s3":          "one config file in an S3 bucket",
	"aws-ssm":         "every Systems Manager parameter under a path",
	"aws-secrets":     "a Secrets Manager secret, or all under a prefix",
	"azure-blob":      "one config file in Azure Blob Storage",
	"azure-keyvault":  "a Key Vault secret, or every one in the vault",
	"azure-appconfig": "the settings under an App Configuration prefix",
	"gcp-gcs":         "one config file in a Cloud Storage bucket",
	"gcp-secret":      "a Secret Manager secret, or a project's secrets",
	"gcp-parameter":   "a Parameter Manager parameter, or all under a prefix",
	"etcd":            "the keys under an etcd prefix; your code connects",
	"sftp":            "one config file on an SFTP server; your code connects",
	"billy":           "a file in a go-billy filesystem your code supplies",
	"iofs":            "a file in an io/fs.FS your code supplies, e.g. embed",
	"afero":           "a file in an afero filesystem your code supplies",
}

// docsBase is the framework's documentation site.
const docsBase = "https://gtb.phpboyscout.uk"

// The pages the Configuration page links; a test holds each to docs/.
const (
	ownFormatDocs     = docsBase + "/explanation/components/config/#the-tools-own-format"
	configSourcesDocs = docsBase + "/explanation/components/config/#config-sources"
	overrideHowTo     = docsBase + "/how-to/override-a-config-source/"
	configStackHowTo  = docsBase + "/how-to/configure-the-config-stack/"
)

// kindListExtraRows sizes the kind list beyond the kinds: its title and the
// "No more config sources" row. The height holds every kind, so the list does
// not resize when the keychain is offered or withdrawn.
const kindListExtraRows = 2

// noteWidth is where the settings note wraps, inside the wizard's margin.
const noteWidth = 68

const (
	configurationBlurb = "Where your tool reads its settings from. YAML is always available.\n" +
		"Link another format when your users already keep their config in it,\n" +
		"or a source serves it. Each one adds its parser to your binary, so\n" +
		"link only what your users will bring.\n\n" +
		"Recorded under properties.config in the manifest. More:\n" +
		ownFormatDocs + "\n"

	sourcesBlurb = "A config source is somewhere beyond the user's own files that settings\n" +
		"come from. Add one when settings should be shared across a team, or\n" +
		"kept out of files on disk because they are secrets. Most tools need\n" +
		"none: choose No more config sources to skip.\n\n" +
		"The kinds come in three groups:\n" +
		"  file, keychain     on the user's own machine.\n" +
		"  vault to gcp-*     a service. Where it is comes from each user's\n" +
		"                     \"<tool> init config <name>\", or from the\n" +
		"                     tool's defaults; their usual login is used.\n" +
		"  etcd to afero      built by your own code. The wizard reserves the\n" +
		"                     slot; the override how-to shows the code.\n\n" +
		"Guide: " + configStackHowTo + "\n" +
		"Config sources: " + configSourcesDocs + "\n" +
		"Override how-to: " + overrideHowTo + "\n"
)

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
		gloss := "read and write; can be the tool's own file"
		if !generator.IsWritableConfigFormat(f) {
			gloss = "read only; users may supply one, nothing writes it"
		}

		formats = append(formats, huh.NewOption(optionLabel(f, gloss, len("properties")), f))
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
		newMultiSelect("Config formats", "Formats a user may hand the tool, beyond YAML.", formats).
			Key("config-formats").Value(&o.ConfigFormats),
		huh.NewSelect[string]().Key("config-format").Title("The tool's own config file").
			Description("The file \"init\" creates and \"config set\" edits, so the format your\n"+
				"users expect to open and edit by hand. Only formats that can be\n"+
				"written back are offered.").
			Options(own()...).OptionsFunc(own, &o.ConfigFormats).Value(&o.ConfigFormat).
			Validate(func(f string) error { return hintedValidation(generator.ValidateConfigFormats(o.ConfigFormats, f)) }),
	).
		Title("Configuration").
		Description(configurationBlurb)
}

func (o *SkeletonOptions) sourceKindGroup(i int) *huh.Group {
	kinds := o.kindOptions()

	return huh.NewGroup(
		huh.NewSelect[string]().Key(fmt.Sprintf("config-source-%d-kind", i)).
			Title(fmt.Sprintf("Config source %d", i+1)).
			Options(kinds...).OptionsFunc(o.kindOptions, &o.Features).
			Height(len(generator.ConfigSourceKinds()) + kindListExtraRows).Value(&o.sourceSlots[i].Kind),
	).
		Title("Configuration: sources").
		Description(sourcesBlurb).
		WithHideFunc(func() bool { return i > 0 && o.sourceSlots[i-1].Kind == "" })
}

// kindOptions are the kinds a slot may take, after "No more config sources".
// The keychain kind needs the OS Keychain feature (D12), so it is offered
// only while that is selected.
func (o *SkeletonOptions) kindOptions() []huh.Option[string] {
	kinds := make([]huh.Option[string], 0, 1+len(generator.ConfigSourceKinds()))
	kinds = append(kinds, huh.NewOption("No more config sources", ""))

	for _, k := range generator.ConfigSourceKinds() {
		if o.kindOffered(k) {
			kinds = append(kinds, huh.NewOption(optionLabel(k, kindGloss(k), len("azure-appconfig")), k))
		}
	}

	return kinds
}

func (o *SkeletonOptions) kindOffered(kind string) bool {
	return kind != "keychain" || slices.Contains(o.Features, generator.KeychainFeature)
}

func kindGloss(kind string) string { return kindGlosses[kind] }

// settingsNote tells the author that a slot's settings are not set in the
// wizard (spec 0204 R1), who sets them, where they land and what they look
// like, and where a default goes. The keys are the kind's own, from the
// catalogue its init config asks.
func (o *SkeletonOptions) settingsNote(i int) string {
	kind := o.sourceSlots[i].Kind
	if generator.IsOverrideOnlySourceKind(kind) {
		return noteText(wrapNote("Config is not set here: this only reserves the slot. Your own code "+
			"builds this source and reads its settings however it likes. The override how-to shows the code:") + "\n" + overrideHowTo)
	}

	tool := cmp.Or(o.Name, "<tool>")
	name := o.slotName(i)
	example := settingsExample(name, setup.ConfigSourceSettings(kind, tool, name))
	userFile := fmt.Sprintf("~/.%s/config.%s", strings.ToLower(tool), o.ownFormatExt())

	if !slices.Contains(o.Features, string(props.InitCmd)) {
		return noteText(strings.Join([]string{
			wrapNote("Config is not set here: this only wires in the adapter. The tool has no init command " +
				"(Initialization is not selected), so set this source's settings in " +
				"pkg/cmd/root/assets/config.yaml, where they look like:"),
			example,
			wrapNote(fmt.Sprintf("A user may still override them in their own config file (typically %s).", userFile)),
			"Every key this source reads:\n" + configSourcesDocs,
		}, "\n\n"))
	}

	initConfig := fmt.Sprintf("%q", tool+" init config "+name)

	return noteText(strings.Join([]string{
		wrapNote(fmt.Sprintf("Config is not set here: this only wires in the adapter. Each user must run %s, "+
			"which captures the settings for this source and saves them to the tool's config file "+
			"(typically %s), where they look like:", initConfig, userFile)),
		example,
		wrapNote(fmt.Sprintf("To predefine a default, put the values you want under config.sources.%s in "+
			"pkg/cmd/root/assets/config.yaml. They apply when %s has not been run.", name, initConfig)),
		"Every key, including the ones init config does not ask:\n" + configSourcesDocs,
	}, "\n\n"))
}

func (o *SkeletonOptions) ownFormatExt() string {
	if o.ConfigFormat == "" || o.ConfigFormat == "yaml" {
		return "yaml"
	}

	return o.ConfigFormat
}

// settingsExample is the block init config writes for a slot, as YAML: each
// key with its default or empty, dotted keys nested as the file holds them.
func settingsExample(name string, settings []setup.SourceSetting) string {
	lines := []string{"  config:", "    sources:", "      " + name + ":"}
	parents := map[string]bool{}

	for _, s := range settings {
		indent := "        "
		key := s.Key

		if parent, child, nested := strings.Cut(s.Key, "."); nested {
			if !parents[parent] {
				lines = append(lines, indent+parent+":")
				parents[parent] = true
			}

			indent += "  "
			key = child
		}

		lines = append(lines, fmt.Sprintf("%s%s: %q", indent, key, s.Default))
	}

	return strings.Join(lines, "\n")
}

// textBinding binds a note to its own text. huh re-renders a dynamic
// description when its binding's hash changes, and the hash skips unexported
// fields such as the source slots and cannot see answers given pages
// earlier, so the binding hashes what the note would show.
type textBinding struct{ text func() string }

// Hash implements hashstructure.Hashable.
func (b textBinding) Hash() (uint64, error) {
	h := fnv.New64a()
	_, err := h.Write([]byte(b.text()))

	return h.Sum64(), err
}

// noteText escapes what huh's Note renders as markup: _ and * toggle italic
// and bold, and a backtick opens a code span its renderer never closes.
func noteText(s string) string {
	return strings.NewReplacer("\\", "\\\\", "_", "\\_", "*", "\\*", "`", "'").Replace(s)
}

// wrapNote wraps text at noteWidth.
func wrapNote(text string) string {
	var lines []string

	line := ""

	for _, word := range strings.Fields(text) {
		if line != "" && len(line)+len(word)+1 > noteWidth {
			lines = append(lines, line)
			line = ""
		}

		line = strings.TrimSpace(line + " " + word)
	}

	return strings.Join(append(lines, line), "\n")
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
		huh.NewNote().Title("Settings").Description(o.settingsNote(i)).
			DescriptionFunc(func() string { return o.settingsNote(i) }, textBinding{func() string { return o.settingsNote(i) }}),
		huh.NewInput().Key(fmt.Sprintf("config-source-%d-name", i)).Title("Name").
			Description("Its key under config.sources and the word in \"<tool> init config <name>\";\n"+
				"a lower-case command word, unique among the slots. Empty uses the one shown.").
			PlaceholderFunc(func() string { return o.defaultSlotName(i) }, &slot.Kind).
			Value(&slot.Name).
			Validate(func(v string) error { return o.validateSlotName(i, v) }),
		huh.NewConfirm().Key(fmt.Sprintf("config-source-%d-required", i)).
			Title("Required?").
			Description("Yes: the tool will not start until this source is configured and\n"+
				"reachable. No: it runs without it and warns.").
			Affirmative("Yes").Negative("No").Value(&slot.Required),
		huh.NewSelect[string]().Key(fmt.Sprintf("config-source-%d-access", i)).Title("Writable?").
			Description("Whether \"config set\" may write to it. Read-only suits a store\n"+
				"someone else manages.").
			Options(access()...).OptionsFunc(access, &slot.Kind).Value(&slot.Access),
		huh.NewSelect[string]().Key(fmt.Sprintf("config-source-%d-placement", i)).Title("Precedence").
			Description("A higher layer overrides a lower one. Below the user's files, a team\n"+
				"source supplies values each user can override; above them, it\n"+
				"enforces them.").
			Options(o.placementOptions()...).Value(&slot.Above),
	).
		Title("Configuration: sources").
		WithHideFunc(func() bool { return slot.Kind == "" || (i > 0 && o.sourceSlots[i-1].Kind == "") })
}

// validateSlotName checks slot i's name against the slots before it, with
// the rules the manifest is held to.
func (o *SkeletonOptions) validateSlotName(i int, v string) error {
	if v == "" {
		v = o.defaultSlotName(i)
	}

	sources := make([]generator.ManifestConfigSource, 0, i+1)

	for j := range i {
		name := o.slotName(j)
		if name == v {
			return errors.Newf("%q is already config source %d's name: type another", v, j+1)
		}

		sources = append(sources, generator.ManifestConfigSource{Name: name, Kind: o.sourceSlots[j].Kind})
	}

	sources = append(sources, generator.ManifestConfigSource{Name: v, Kind: o.sourceSlots[i].Kind})

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

	declared := o.activeSourceSlots()
	for i := range declared {
		declared[i].Name = o.slotName(i)
	}

	active := slices.DeleteFunc(slices.Clone(declared), func(s wizardSource) bool { return !o.kindOffered(s.Kind) })

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
		o.ConfigSources = append(o.ConfigSources, s.Name+"="+s.Kind)

		if !s.Required {
			o.ConfigSourcesOptional = append(o.ConfigSourcesOptional, s.Name)
		}

		switch s.Access {
		case sourceAccessWritable:
			o.ConfigSourcesWritable = append(o.ConfigSourcesWritable, s.Name)
		case sourceAccessReadOnly:
			o.readOnlySources = append(o.readOnlySources, s.Name)
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
				layers = append(layers, s.Name)
			}
		}
	}

	return layers
}
