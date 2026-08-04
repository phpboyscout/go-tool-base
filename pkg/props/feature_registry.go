package props

import (
	"cmp"
	"slices"
	"sync"

	"gitlab.com/phpboyscout/go/errors"
)

// PackagePath is this package's import path, the ConstPackage every built-in
// feature descriptor carries. Declared once so a generated import cannot drift
// from the package it names.
const PackagePath = "gitlab.com/phpboyscout/go-tool-base/pkg/props"

// FeatureKind classifies what a feature is, so "every forge" becomes a query
// rather than a list somebody has to remember to update.
//
// It is deliberately not a second identity type: [FeatureID] stays the currency
// of Enable, Disable, IsEnabled and the manifest. Kind is a property of a
// feature, not a different kind of thing.
type FeatureKind string

const (
	// KindBuiltin is a feature the framework itself ships and enumerates.
	KindBuiltin FeatureKind = "builtin"
	// KindForge is a forge integration contributed by a blank-imported package.
	KindForge FeatureKind = "forge"
)

// FeatureDescriptor is everything the framework needs to know about a feature.
//
// It is registered once, by the package that owns the feature, so a blank import
// is genuinely all it takes to make a feature visible everywhere — the doctor
// report, config validation, and the generator's handling.
type FeatureDescriptor struct {
	// ID is the feature's identity and its config/manifest name.
	ID FeatureID
	// ConstName is the exported Go identifier naming this feature's constant,
	// as it must appear in generated source (e.g. "AiCmd").
	//
	// It cannot be derived from ID — "mcp" yields "McpCmd", not "MvpCmd" — and
	// the generator emits it verbatim, so it is carried rather than computed.
	// A feature with no exported constant cannot be scaffolded.
	ConstName string
	// ConstPackage is the import path of the package declaring ConstName.
	//
	// Built-in constants live in props, but a plugin's do not — the forge
	// features are declared by pkg/setup/forge — and generated source must
	// qualify the reference correctly. Carrying the path here is what keeps a
	// feature's generator handling a property of its registration rather than a
	// special case somewhere downstream.
	ConstPackage string
	// Kind classifies the feature. See [FeatureKind].
	Kind FeatureKind
	// Default reports whether the feature is enabled when a tool expresses no
	// preference. Only [KindBuiltin] may set it — see [ErrPluginDefaultOn].
	Default bool
}

var (
	// ErrInvalidDescriptor reports a descriptor missing a required field.
	ErrInvalidDescriptor = errors.NewSentinel("gtb.props.invalid_descriptor", "props: feature descriptor is incomplete")
	// ErrDuplicateFeature reports a second registration for one ID.
	ErrDuplicateFeature = errors.NewSentinel("gtb.props.duplicate_feature", "props: feature is already registered")
	// ErrRegistrySealed reports a registration attempted after enumeration began.
	ErrRegistrySealed = errors.NewSentinel("gtb.props.registry_sealed", "props: feature registry is sealed")

	// ErrPluginDefaultOn reports a non-builtin feature declaring itself
	// default-enabled.
	//
	// Adding a blank import must change what is *available*, never what is *on*.
	// Without that rule an import list becomes a behavioural file, and a
	// downstream that deliberately omits a provider cannot reason about what its
	// remaining imports switched on behind it.
	ErrPluginDefaultOn = errors.NewSentinel("gtb.props.plugin_default_on", "props: only builtin features may be default-enabled")
)

//nolint:gochecknoglobals // process-wide registry, the point of the blank-import pattern
var (
	featureMu       sync.RWMutex
	featureRegistry []FeatureDescriptor
	featuresSealed  bool
)

// RegisterFeature adds a feature to the registry. It is called from the owning
// package's init, so a blank import is all a consumer needs.
//
// It panics on a bad registration rather than returning an error: this runs at
// init, where there is no caller to handle a failure, and a silently dropped
// feature would surface much later as a missing entry in a doctor report or a
// scaffolded project.
func RegisterFeature(d FeatureDescriptor) {
	if err := registerFeature(d); err != nil {
		panic(err)
	}
}

// validateDescriptor reports whether d may join existing.
//
// It is pure — taking the current set rather than reading the package registry —
// so the rules can be tested directly. Testing them through [registerFeature]
// would not work: enumeration seals the registry, so a parallel test that
// registers after any other test has enumerated gets ErrRegistrySealed instead
// of the rule it meant to exercise.
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
func registerFeature(d FeatureDescriptor) error {
	featureMu.Lock()
	defer featureMu.Unlock()

	if featuresSealed {
		return errors.Wrapf(ErrRegistrySealed, "registering %q", d.ID)
	}

	if err := validateDescriptor(d, featureRegistry); err != nil {
		return err
	}

	featureRegistry = append(featureRegistry, d)

	return nil
}

// SealFeatures closes the registry. Registration afterwards fails rather than
// producing an enumeration that depends on when it was read.
//
// Enumeration seals implicitly, so most callers never need this; it exists so a
// host can make the boundary explicit after its imports have run.
func SealFeatures() {
	featureMu.Lock()
	defer featureMu.Unlock()

	featuresSealed = true
}

// FeatureDescriptors returns every registered feature in a stable total order:
// built-ins in the order the constant block declares them, then everything else
// by (kind, id).
//
// The order is derived from data, never from init() sequencing — Go orders init
// by dependency then filename, which is stable for one build but moves with the
// import graph. The doctor report and the generator's golden files both depend
// on this order, so it must not shift when an import is added.
//
// Reading seals the registry.
func FeatureDescriptors() []FeatureDescriptor {
	featureMu.Lock()
	featuresSealed = true
	out := slices.Clone(featureRegistry)
	featureMu.Unlock()

	slices.SortStableFunc(out, func(a, b FeatureDescriptor) int {
		ai, bi := builtinRank(a), builtinRank(b)
		if ai != bi {
			return cmp.Compare(ai, bi)
		}

		if a.Kind != b.Kind {
			return cmp.Compare(string(a.Kind), string(b.Kind))
		}

		return cmp.Compare(string(a.ID), string(b.ID))
	})

	return out
}

// AllFeatures is the canonical enumeration of every registered feature.
//
// It reflects what this binary linked: a tool that blank-imports fewer providers
// enumerates fewer features, which is the correct runtime answer. The generator
// needs the complete *possible* set instead, and gets it from its own catalogue.
func AllFeatures() []FeatureID {
	ds := FeatureDescriptors()

	ids := make([]FeatureID, len(ds))
	for i, d := range ds {
		ids[i] = d.ID
	}

	return ids
}

// FeaturesOfKind returns the registered features of one kind, in the same order
// [AllFeatures] uses. It is what makes "every forge" a query rather than a list
// duplicated across the wizard, config validation and the doctor report.
func FeaturesOfKind(kind FeatureKind) []FeatureID {
	var ids []FeatureID

	for _, d := range FeatureDescriptors() {
		if d.Kind == kind {
			ids = append(ids, d.ID)
		}
	}

	return ids
}

// DescriptorFor returns the descriptor for id.
func DescriptorFor(id FeatureID) (FeatureDescriptor, bool) {
	for _, d := range FeatureDescriptors() {
		if d.ID == id {
			return d, true
		}
	}

	return FeatureDescriptor{}, false
}

// builtinOrder fixes the enumeration order of the built-in features, preserving
// the sequence the constant block and the historical AllFeatures var declared.
// Anything absent sorts after every built-in.
//
//nolint:gochecknoglobals // ordering table for the seeded built-ins
var builtinOrder = []FeatureID{
	UpdateCmd, InitCmd, McpCmd, DocsCmd, AiCmd, DoctorCmd,
	ConfigCmd, ChangelogCmd, ManCmd, TelemetryCmd,
}

func builtinRank(d FeatureDescriptor) int {
	if i := slices.Index(builtinOrder, d.ID); i >= 0 {
		return i
	}

	return len(builtinOrder)
}

// isDefaultEnabled reports the framework default for id, from the registry.
func isDefaultEnabled(id FeatureID) bool {
	d, ok := DescriptorFor(id)

	return ok && d.Default
}

//nolint:gochecknoinits // seeding the built-ins is what makes the registry the single source of truth
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
