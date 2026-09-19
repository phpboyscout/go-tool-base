package generator

import (
	"context"
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// ToggleableFeatures is the set of features that `gtb enable <feature>` and
// `gtb disable <feature>` can flip in a generated project's manifest: every
// catalogue feature, the keychain link included, which the shared sync writes
// or removes as cmd/<name>/keychain.go from the manifest's entry (spec 0197
// D8, spec 0199 OQ3).
var ToggleableFeatures = featureNamesFromCatalogue()

// KeychainFeature is the keychain link's manifest name. It is a catalogue
// feature of kind link: selected by its file rather than a SetFeatures toggle.
const KeychainFeature = string(keychain.KeychainFeature)

// SelectableFeatures is the set `gtb generate project --features` accepts:
// every toggleable feature that is not a forge, plus keychain. Keychain is a
// real choice at generation time that `gtb enable`/`gtb disable` cannot flip
// afterwards. A forge is not chosen here: the backend implies one and
// --forge-credentials adds the rest (spec 0195 D1, D6), though a forge stays
// toggleable once the project exists.
var SelectableFeatures = nonForgeFeatures()

func nonForgeFeatures() []string {
	forges := ForgeBackends()

	names := make([]string, 0, len(ToggleableFeatures))

	for _, name := range ToggleableFeatures {
		if !slices.Contains(forges, props.FeatureID(name)) {
			names = append(names, name)
		}
	}

	return names
}

// DefaultSelectedFeatures is what `gtb generate project` selects when --features
// is omitted: every catalogue feature that is default-enabled in the framework,
// plus every link kind. A link (keychain) declares no runtime default because
// its presence is its enablement, but a new project gets it unless the author
// opts out, so selecting it by default is the generator's policy rather than
// the framework's. Derived rather than written out so a change to a framework
// default cannot leave the generator's default set stale.
//
// Forge features are Default:false, so they are correctly absent: a scaffolded
// tool opts into a forge explicitly.
var DefaultSelectedFeatures = defaultSelectedFromCatalogue()

func defaultSelectedFromCatalogue() []string {
	catalogue := templates.Catalogue()
	names := make([]string, 0, len(catalogue))

	for _, d := range catalogue {
		if d.Default || d.Kind == props.KindLink {
			names = append(names, string(d.ID))
		}
	}

	return names
}

func featureNamesFromCatalogue() []string {
	catalogue := templates.Catalogue()
	names := make([]string, 0, len(catalogue))

	for _, d := range catalogue {
		names = append(names, string(d.ID))
	}

	return names
}

// featureDefaultEnabled reports the framework-default enabled state of a
// toggleable feature, read from the catalogue (mirrors props.DefaultFeatures).
// Unknown names and link kinds default to false: a link's manifest entry is
// what turns it on.
func featureDefaultEnabled(name string) bool {
	if d, ok := templates.CatalogueEntry(name); ok {
		return d.Default
	}

	return false
}

// CurrentFeatures returns the project's current manifest feature entries so a
// caller (e.g. the enable/disable wizard) can present the current state. A
// project that has never toggled a feature returns an empty slice (every
// feature is at its framework default).
func (g *Generator) CurrentFeatures() ([]ManifestFeature, error) {
	if err := g.verifyProject(); err != nil {
		return nil, err
	}

	m, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	return m.Properties.Features, nil
}

// FeatureEnabled reports whether the named feature is currently enabled in the
// project, honouring a manifest override and otherwise falling back to the
// framework default.
func (g *Generator) FeatureEnabled(name string) (bool, error) {
	features, err := g.CurrentFeatures()
	if err != nil {
		return false, err
	}

	return featureEnabledIn(features, name), nil
}

// FeatureEnabledIn resolves a feature's effective enabled state from a
// manifest feature list: an explicit entry wins, else the framework default.
func FeatureEnabledIn(features []ManifestFeature, name string) bool {
	return featureEnabledIn(features, name)
}

// featureEnabledIn resolves a feature's effective enabled state from a manifest
// feature slice, falling back to the framework default when not overridden.
func featureEnabledIn(features []ManifestFeature, name string) bool {
	for _, f := range features {
		if f.Name == name {
			return f.Enabled
		}
	}

	return featureDefaultEnabled(name)
}

// ApplyFeatures flips the requested built-in features in the manifest's
// properties.features block and re-renders the generated root command so its
// props.SetFeatures(...) wiring matches. desired maps a toggleable feature name
// to its requested enabled state (the same method serves the single-name CLI
// path and the multi-select wizard).
//
// The manifest is normalised against the framework defaults: an entry is kept
// only when it differs from the default, so returning a feature to its default
// state removes the entry and the rendered root drops the now-redundant toggle
// (and the whole SetFeatures call when nothing differs). Unknown names are a
// hard error. It returns the feature names that actually changed — an empty
// result means every requested feature was already in the requested state, and
// nothing is written.
func (g *Generator) ApplyFeatures(ctx context.Context, desired map[string]bool) ([]string, error) {
	if err := g.verifyProject(); err != nil {
		return nil, err
	}

	if err := validateFeatureNames(desired); err != nil {
		return nil, err
	}

	m, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	features, changed := applyFeatureChanges(m.Properties.Features, desired)
	if len(changed) == 0 {
		return changed, nil
	}

	m.Properties.Features = features

	// One command leaves a consistent tree: the root's SetFeatures wiring,
	// the derived fields a newly enabled feature needs (the ai feature's
	// provider list), and the adapter files that follow them (spec 0197 D7).
	if err := g.syncDerivedFromManifest(m); err != nil {
		return nil, err
	}

	if err := g.writeManifest(m); err != nil {
		return nil, err
	}

	if slices.Contains(changed, string(props.AiCmd)) && !desired[string(props.AiCmd)] {
		g.warnProvidersOutliveAi(m)
	}

	// On a real filesystem, format/fix the re-rendered root so the tree stays
	// building and lint-clean. Skipped on in-memory fs (unit tests).
	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.runSkeletonPostProcessing(ctx, g.config.Path)
	}

	return changed, nil
}

// validateFeatureNames rejects any unknown name in the desired set before the
// manifest is loaded or touched.
func validateFeatureNames(desired map[string]bool) error {
	for name := range desired {
		if err := ValidateFeatureName(name); err != nil {
			return err
		}
	}

	return nil
}

// applyFeatureChanges folds the desired feature states into the manifest
// feature slice, normalising against the framework defaults, and returns the
// updated slice plus the sorted names that actually changed (those already in
// the requested state are skipped, keeping the operation idempotent).
func applyFeatureChanges(features []ManifestFeature, desired map[string]bool) ([]ManifestFeature, []string) {
	changed := []string{}

	for name, want := range desired {
		if featureEnabledIn(features, name) == want {
			continue
		}

		features = upsertOrClearFeature(features, name, want)
		changed = append(changed, name)
	}

	slices.Sort(changed)

	return features, changed
}

// upsertOrClearFeature sets the named feature to the requested state in the
// manifest feature slice, normalising against the framework default: when the
// requested state equals the default the entry is removed (keeping the manifest
// minimal so the rendered root omits the redundant toggle); otherwise the entry
// is added or updated.
func upsertOrClearFeature(features []ManifestFeature, name string, enabled bool) []ManifestFeature {
	out := make([]ManifestFeature, 0, len(features)+1)

	for _, f := range features {
		if f.Name != name {
			out = append(out, f)
		}
	}

	if enabled == featureDefaultEnabled(name) {
		return out // back to default: no entry needed
	}

	return append(out, ManifestFeature{Name: name, Enabled: enabled})
}

// warnProvidersOutliveAi: the provider list is the author's record of linked
// modules and outlives the ai feature (#94); a default project that ran
// `enable ai` once would otherwise carry every module without a word.
func (g *Generator) warnProvidersOutliveAi(m *Manifest) {
	linked := chatProvidersFor(m.Properties.Chat.Providers)
	if len(linked) == 0 {
		return
	}

	g.props.Logger.Warn("ai is off, but chat.providers still links " + strings.Join(linked, ", ") +
		"; run `gtb unset chat.providers` to drop the modules, or leave them for the tool's own use")
}
