package props

import (
	"slices"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// PackagePath is this package's import path, the ConstPackage every built-in
// feature descriptor carries. Declared once so a generated import cannot drift
// from the package it names.
const PackagePath = "gitlab.com/phpboyscout/go-tool-base/pkg/props"

// FeatureKind classifies what a feature is, so "every forge" becomes a query
// rather than a list somebody has to remember to update. It is the core's
// Kind; the constants are GTB's.
type FeatureKind = features.Kind

const (
	// KindBuiltin is a feature the framework itself ships and enumerates.
	KindBuiltin FeatureKind = "builtin"
	// KindForge is a forge integration contributed by a blank-imported package.
	KindForge FeatureKind = "forge"
	// KindLink is a feature whose only effect is a blank import: declaring it
	// is what links the backend (the OS keychain), so its presence in a binary
	// is its enablement, and the generator toggles it by writing or removing
	// the file that imports it (spec 0199 OQ3). It has no runtime switch and
	// never appears in SetFeatures.
	KindLink FeatureKind = "link"
)

// FeatureDescriptor is everything the framework needs to know about a feature.
//
// It is registered once, by the package that owns the feature, so a blank import
// is genuinely all it takes to make a feature visible everywhere: the doctor
// report, config validation, and the generator's handling. It implements
// features.Descriptor and adds the two generated-code identifiers, which are
// GTB's concern rather than the core's (spec 0199 D2).
type FeatureDescriptor struct {
	// ID is the feature's identity and its config/manifest name.
	ID FeatureID
	// ConstName is the exported Go identifier naming this feature's constant,
	// as it must appear in generated source (e.g. "AiCmd").
	//
	// It cannot be derived from ID ("mcp" yields "McpCmd", not "MvpCmd") and
	// the generator emits it verbatim, so it is carried rather than computed.
	// A feature with no exported constant cannot be scaffolded.
	ConstName string
	// ConstPackage is the import path of the package declaring ConstName.
	//
	// Built-in constants live in props, but a plugin's do not (the forge
	// features are declared by pkg/setup/forge) and generated source must
	// qualify the reference correctly. Carrying the path here is what keeps a
	// feature's generator handling a property of its registration rather than a
	// special case somewhere downstream.
	ConstPackage string
	// Kind classifies the feature. See [FeatureKind].
	Kind FeatureKind
	// Default reports whether the feature is enabled when a tool expresses no
	// preference. Only [KindBuiltin] may set it; see [ErrPluginDefaultOn].
	Default bool
	// Dynamic reports whether a flag backend may override the static state at
	// evaluation time (spec 0199 D7). Every built-in is static.
	Dynamic bool
	// Order places a non-builtin feature among its kind: features with an
	// Order sort by it after the built-ins, features without one sort after
	// those by kind and ID. The forges use it so a chooser lists them as the
	// framework intends (GitHub first, the default backend) rather than
	// alphabetically. Zero means unordered.
	Order int
}

// FeatureID implements features.Descriptor.
func (d FeatureDescriptor) FeatureID() features.ID { return d.ID }

// FeatureKind implements features.Descriptor.
func (d FeatureDescriptor) FeatureKind() features.Kind { return d.Kind }

// DefaultOn implements features.Descriptor.
func (d FeatureDescriptor) DefaultOn() bool { return d.Default }

// IsDynamic implements features.Descriptor.
func (d FeatureDescriptor) IsDynamic() bool { return d.Dynamic }

// Rank implements features.Ranked: built-ins keep the order the constant block
// declares, a feature with an Order follows them in that order, and everything
// else is unranked and sorts by kind and ID.
func (d FeatureDescriptor) Rank() (int, bool) {
	if i := slices.Index(builtinOrder, d.ID); i >= 0 {
		return i, true
	}

	if d.Order > 0 {
		return len(builtinOrder) + d.Order, true
	}

	return 0, false
}

var (
	// ErrInvalidDescriptor reports a descriptor missing a required field.
	ErrInvalidDescriptor = features.ErrInvalidDescriptor
	// ErrDuplicateFeature reports a second registration for one ID.
	ErrDuplicateFeature = features.ErrDuplicateFeature

	// ErrPluginDefaultOn reports a non-builtin feature declaring itself
	// default-enabled.
	//
	// Adding a blank import must change what is *available*, never what is *on*.
	// Without that rule an import list becomes a behavioural file, and a
	// downstream that deliberately omits a provider cannot reason about what its
	// remaining imports switched on behind it.
	ErrPluginDefaultOn = errors.NewSentinel("gtb.props.plugin_default_on", "props: only builtin features may be default-enabled")
)

// RegisterFeature declares a feature on the default registry. It is called
// from the owning package's init, so a blank import is all a consumer needs.
//
// It panics on a bad registration rather than returning an error: this runs at
// init, where there is no caller to handle a failure, and a silently dropped
// feature would surface much later as a missing entry in a doctor report or a
// scaffolded project.
func RegisterFeature(d FeatureDescriptor) {
	if err := registerFeature(features.Default(), d); err != nil {
		panic(err)
	}
}

// validateDescriptor applies GTB's rules beyond the core's: the generated-code
// identifiers must be present, and only a built-in may default on. It is pure
// (taking the current set rather than reading a registry) so the rules can be
// tested directly; duplicates are reported here too so a caller sees one error
// shape.
func validateDescriptor(d FeatureDescriptor, existing []FeatureDescriptor) error {
	if d.ID == "" || d.ConstName == "" || d.Kind == "" || d.ConstPackage == "" {
		return errors.Wrapf(ErrInvalidDescriptor,
			"id=%q const=%q pkg=%q kind=%q", d.ID, d.ConstName, d.ConstPackage, d.Kind)
	}

	if d.Default && d.Kind != KindBuiltin {
		return errors.Wrapf(ErrPluginDefaultOn, "%q is kind %q", d.ID, d.Kind)
	}

	if slices.ContainsFunc(existing, func(e FeatureDescriptor) bool { return e.ID == d.ID }) {
		return errors.Wrapf(ErrDuplicateFeature, "%q", d.ID)
	}

	return nil
}

// registerFeature is the testable half of [RegisterFeature].
func registerFeature(r features.Registry, d FeatureDescriptor) error {
	if err := validateDescriptor(d, descriptorsOf(r.Snapshot())); err != nil {
		return err
	}

	return r.Declare(d)
}

// Enumerator is anything that lists descriptors: a features.Snapshot or a
// features.Set.
type Enumerator interface {
	Descriptors() []features.Descriptor
}

// descriptorsOf narrows an enumeration to GTB's descriptors. A descriptor of
// another type on the same registry is a downstream's own and not GTB's to
// enumerate, so it is skipped rather than refused.
func descriptorsOf(s Enumerator) []FeatureDescriptor {
	all := s.Descriptors()
	out := make([]FeatureDescriptor, 0, len(all))

	for _, d := range all {
		if fd, ok := d.(FeatureDescriptor); ok {
			out = append(out, fd)
		}
	}

	return out
}

// DescriptorsIn returns GTB's descriptors in s, in the snapshot's order. It is
// what a reader with a Set or a Snapshot in hand uses in place of the removed
// process-wide enumerations (spec 0199 D3).
func DescriptorsIn(s Enumerator) []FeatureDescriptor { return descriptorsOf(s) }

// builtinOrder fixes the enumeration order of the built-in features, preserving
// the sequence the constant block and the historical AllFeatures var declared.
// Anything absent sorts after every built-in.
var builtinOrder = []FeatureID{
	UpdateCmd, InitCmd, McpCmd, DocsCmd, AiCmd, DoctorCmd,
	ConfigCmd, ChangelogCmd, ManCmd, TelemetryCmd,
}

func init() {
	for _, d := range []struct {
		id        FeatureID
		constName string
		enabled   bool
	}{
		{UpdateCmd, "UpdateCmd", true},
		{InitCmd, "InitCmd", true},
		{McpCmd, "McpCmd", true},
		{DocsCmd, "DocsCmd", true},
		{AiCmd, "AiCmd", false},
		{DoctorCmd, "DoctorCmd", true},
		{ConfigCmd, "ConfigCmd", false},
		{ChangelogCmd, "ChangelogCmd", true},
		{ManCmd, "ManCmd", false},
		{TelemetryCmd, "TelemetryCmd", false},
	} {
		RegisterFeature(FeatureDescriptor{
			ID:           d.id,
			ConstName:    d.constName,
			ConstPackage: PackagePath,
			Kind:         KindBuiltin,
			Default:      d.enabled,
		})
	}
}
