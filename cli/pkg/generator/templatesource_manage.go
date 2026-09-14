package generator

// Manifest-editing operations behind the `gtb template {add,update,remove,
// list}` command group. Each mutates the project manifest's
// Properties.Templates list and then regenerates so the overlay (and any
// `replaces:` suppression) is applied or undone. The manifest-edit + regenerate
// path is the source of truth; these are ergonomics over it.

import (
	"context"

	"gitlab.com/phpboyscout/go/errors"
)

// ListTemplateSources returns the template sources recorded in the project
// manifest, in layer order.
func (g *Generator) ListTemplateSources() ([]TemplateSource, error) {
	m, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	return m.Properties.Templates, nil
}

// AddTemplateSource appends a source to the manifest (rejecting a duplicate
// name) and regenerates so the overlay is applied and the pin/hashes are
// recorded. The supplied source is validated first.
func (g *Generator) AddTemplateSource(ctx context.Context, ts TemplateSource) error {
	if err := ValidateTemplateSource(&ts); err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	for _, existing := range m.Properties.Templates {
		if existing.Name != "" && existing.Name == ts.Name {
			return errors.WithHintf(
				errors.Newf("a template source named %q already exists", ts.Name),
				"use `gtb template update %s` to advance it, or pass --name to add a distinct source", ts.Name,
			)
		}
	}

	prior := m.Properties.Templates
	next := append(append([]TemplateSource{}, prior...), ts)

	if err := g.saveManifestTemplates(next); err != nil {
		return err
	}

	// Regenerate validates the overlay actually applies (containment,
	// denylist, render). On failure, roll the manifest back so a rejected
	// source does not linger in the manifest.
	if err := g.RegenerateProject(ctx); err != nil {
		if rbErr := g.saveManifestTemplates(prior); rbErr != nil {
			g.props.Logger.Warn("failed to roll back manifest after a rejected template add", "error", rbErr)
		}

		return err
	}

	return nil
}

// UpdateTemplateSource re-resolves a named git source's ref to a new SHA
// (clearing the old pin so the clone re-resolves) and regenerates. This is the
// only pin-advancing path (D7). A local source has no pin to advance; updating
// it simply regenerates against the current on-disk tree.
func (g *Generator) UpdateTemplateSource(ctx context.Context, name string) error {
	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	idx := indexOfSource(m.Properties.Templates, name)
	if idx < 0 {
		return errors.Newf("no template source named %q", name)
	}

	// Clear the pin so resolveGitSource re-resolves the ref to a fresh SHA.
	m.Properties.Templates[idx].Resolved = ""

	if err := g.saveManifestTemplates(m.Properties.Templates); err != nil {
		return err
	}

	return g.RegenerateProject(ctx)
}

// RemoveTemplateSource drops a named source from the manifest and regenerates.
// Removing the source restores any embedded scaffold a `replaces:` had
// suppressed (the embedded files re-enter the walk) and drops the source's
// tracked overlay output from the manifest. The on-disk overlay files
// themselves are left in place unless a regenerated embedded file overwrites
// them — a clean, reversible swap consistent with the rest of regenerate.
func (g *Generator) RemoveTemplateSource(ctx context.Context, name string) error {
	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	idx := indexOfSource(m.Properties.Templates, name)
	if idx < 0 {
		return errors.Newf("no template source named %q", name)
	}

	removed := m.Properties.Templates[idx]

	// Drop the removed source's tracked output hashes from the top-level map
	// so a regenerated embedded file (or its absence) is not flagged as a user
	// edit.
	for relPath := range removed.Hashes {
		delete(m.Hashes, relPath)
	}

	m.Properties.Templates = append(m.Properties.Templates[:idx], m.Properties.Templates[idx+1:]...)

	if err := g.saveFullManifest(m); err != nil {
		return err
	}

	return g.RegenerateProject(ctx)
}

// saveManifestTemplates persists only the Properties.Templates list back to the
// manifest, preserving everything else.
func (g *Generator) saveManifestTemplates(sources []TemplateSource) error {
	manifestPath := ManifestPathFor(g.config.Path)

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return err
	}

	m.Properties.Templates = sources

	return g.marshalManifestFile(manifestPath, m)
}

// saveFullManifest persists the whole manifest (used by remove, which also
// edits Hashes).
func (g *Generator) saveFullManifest(m *Manifest) error {
	return g.marshalManifestFile(ManifestPathFor(g.config.Path), m)
}

// indexOfSource returns the index of the source matching name (by Name, or by
// Location when Name is empty), or -1.
func indexOfSource(sources []TemplateSource, name string) int {
	for i := range sources {
		if sources[i].Name == name || (sources[i].Name == "" && sources[i].Location == name) {
			return i
		}
	}

	return -1
}
