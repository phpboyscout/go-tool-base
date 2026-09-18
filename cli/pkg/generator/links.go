package generator

import (
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// linkFile is the artefact of a framework link in a project: cmd/<name>/<id>.go.
func linkFile(name string, d props.FeatureDescriptor) string {
	return filepath.Join("cmd", name, string(d.ID)+".go")
}

// enabledLinks are the framework links whose effective state is on: the
// manifest's entry when it has one, the link's default otherwise (spec 0202
// D3), so a manifest that says nothing about mcp links it and one that says
// nothing about the keychain does not.
func enabledLinks(features []ManifestFeature) []props.FeatureDescriptor {
	var out []props.FeatureDescriptor

	for _, d := range templates.FrameworkLinks() {
		if featureEnabledIn(features, string(d.ID)) {
			out = append(out, d)
		}
	}

	return out
}

// syncLinkFiles writes each framework link's file while its feature is on
// and removes it when it is off (spec 0197 D8, spec 0202 D6). The files used
// to be emitted on generate and never touched again, so deleting one was the
// only durable way to drop a link, and the manifest did not know.
func (g *Generator) syncLinkFiles(name string, features []ManifestFeature) error {
	for _, d := range templates.FrameworkLinks() {
		rel := linkFile(name, d)

		if !featureEnabledIn(features, string(d.ID)) {
			path := filepath.Join(g.config.Path, rel)
			if err := g.props.FS.Remove(path); err != nil && !os.IsNotExist(err) {
				return errors.Newf("failed to remove %s: %w", path, err)
			}

			continue
		}

		if err := g.writeGeneratedGoFile(rel, templates.SkeletonLink(d)); err != nil {
			return err
		}
	}

	return nil
}

// recoverLinks appends a delta entry for every framework link whose artefact
// disagrees with its default: the keychain's presence (it defaults off), the
// mcp file's absence (it defaults on). The literal scanner never sees a link,
// so this is the only way its state reaches a rebuilt manifest.
func (g *Generator) recoverLinks(features []ManifestFeature) []ManifestFeature {
	for _, d := range templates.FrameworkLinks() {
		if present := g.linkArtefactExists(d); present != d.Default {
			features = append(features, ManifestFeature{Name: string(d.ID), Enabled: present})
		}
	}

	return features
}

// linkArtefactExists reports whether the project carries the link's file
// under any cmd/<name>.
func (g *Generator) linkArtefactExists(d props.FeatureDescriptor) bool {
	matches, err := afero.Glob(g.props.FS, filepath.Join(g.config.Path, "cmd", "*", string(d.ID)+".go"))

	return err == nil && len(matches) > 0
}
