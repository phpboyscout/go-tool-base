package generator

import (
	"context"
	"slices"

	"github.com/spf13/afero"
)

// ToggleableFeatures is the set of built-in features that `gtb enable <feature>`
// and `gtb disable <feature>` can flip in a generated project's manifest. It is
// the nine props.FeatureCmd values the generator already understands via
// `generate project --features`.
//
// keychain is intentionally excluded: it is a build-time blank-import decision
// (the scaffolded cmd/<name>/keychain.go), not a runtime feature flag, so it is
// changed by adding/removing that file, not by a manifest toggle.
var ToggleableFeatures = []string{
	"init", "update", "mcp", "docs", "doctor", "changelog", "ai", "config", "telemetry",
}

// featureDefaultEnabled reports the framework-default enabled state of a
// toggleable feature, mirroring props.DefaultFeatures: update/init/mcp/docs/
// doctor/changelog are on by default; ai/config/telemetry are opt-in.
func featureDefaultEnabled(name string) bool {
	switch name {
	case "init", "update", "mcp", "docs", "doctor", "changelog":
		return true
	default:
		return false
	}
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

	// Re-render the one generated root-command file so the SetFeatures wiring
	// matches the manifest. cmd.go is a DO-NOT-EDIT generated file, so it is
	// always re-rendered — mirrors applySigningPosture.
	if err := g.regenerateRootCommand(*m); err != nil {
		return nil, err
	}

	if err := g.writeManifest(m); err != nil {
		return nil, err
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
