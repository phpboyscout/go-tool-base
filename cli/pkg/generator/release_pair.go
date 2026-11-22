package generator

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

const (
	goreleaserFile = ".goreleaser.yaml"
	gitlabCIFile   = ".gitlab-ci.yml"

	releasePairMigration = "https://gtb.phpboyscout.uk/reference/migration/v0.x-generated-gitlab-releases-carry-their-assets/"
)

// releasePairChange is what to change in a kept release file so it matches
// the other one, rewritten with spec 0219's flow.
var releasePairChange = map[string]string{
	goreleaserFile: "set the release block to disable: true and drop mode, use_existing_draft, replace_existing_artifacts and make_latest; on the forge channel also add the gitlab-generic uploads entry",
	gitlabCIFile:   "give the colophon include assets: true and an asset_base naming where .goreleaser.yaml uploads",
}

// reportReleasePair warns when a regenerate kept one of a GitLab project's
// two release files on the old flow and rewrote the other on the new one:
// either half-migration ships an empty release or none (#196, spec 0219).
func (g *Generator) reportReleasePair() {
	for kept, other := range map[string]string{goreleaserFile: gitlabCIFile, gitlabCIFile: goreleaserFile} {
		if !g.heldBack(kept) || g.heldBack(other) {
			continue
		}

		keptNew, keptOK := g.attachesAssetsAtTag(kept)
		otherNew, otherOK := g.attachesAssetsAtTag(other)

		if keptOK && otherOK && !keptNew && otherNew {
			g.props.Logger.Warn("kept file is on the old release flow, and the other release file was rewritten for the new one",
				"path", kept,
				"rewritten", other,
				"consequence", "the two disagree, so the next release is created empty or refused",
				"change", releasePairChange[kept],
				"see", releasePairMigration)
		}
	}
}

// heldBack reports whether this run left rel as it was, by a declined
// conflict or an ignore rule.
func (g *Generator) heldBack(rel string) bool {
	return g.conflicts.wasKept(rel) || slices.Contains(g.conflicts.ignored, rel)
}

// attachesAssetsAtTag reads whether the release file at rel uses spec 0219's
// flow, and whether it could be read at all.
func (g *Generator) attachesAssetsAtTag(rel string) (attaches, ok bool) {
	raw, err := afero.ReadFile(g.props.FS, filepath.Join(g.config.Path, rel))
	if err != nil {
		return false, false
	}

	if rel == goreleaserFile {
		var doc struct {
			Release struct {
				Disable bool `yaml:"disable"`
			} `yaml:"release"`
		}

		if yaml.Unmarshal(raw, &doc) != nil {
			return false, false
		}

		return doc.Release.Disable, true
	}

	var doc struct {
		Include yaml.Node `yaml:"include"`
	}

	if yaml.Unmarshal(raw, &doc) != nil {
		return false, false
	}

	return colophonAssets(doc.Include)
}

// colophonAssets finds the colophon component among a GitLab CI include,
// which may be one entry or a list mixing strings and maps, and reports its
// assets input.
func colophonAssets(include yaml.Node) (attaches, ok bool) {
	entries := []*yaml.Node{&include}
	if include.Kind == yaml.SequenceNode {
		entries = include.Content
	}

	for _, entry := range entries {
		var inc struct {
			Component string         `yaml:"component"`
			Inputs    map[string]any `yaml:"inputs"`
		}

		if entry.Kind != yaml.MappingNode || entry.Decode(&inc) != nil {
			continue
		}

		if strings.Contains(inc.Component, "/colophon@") {
			assets, _ := inc.Inputs["assets"].(bool)

			return assets, true
		}
	}

	return false, false
}
