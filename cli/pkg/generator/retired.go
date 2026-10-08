package generator

import (
	"path/filepath"

	"github.com/spf13/afero"
)

// retiredSkeletonFiles are files the skeleton once rendered and no longer
// does. Regenerate removes each from a project that still has it as
// generated, because a stale one keeps working against its replacement: the
// releaser-pleaser workflow would run beside colophon's on every push (#110).
var retiredSkeletonFiles = []string{
	".github/workflows/releaser-pleaser.yaml",
}

// retireSkeletonFiles removes the retired files whose content is still what
// the generator wrote, and their hashes; an edited file, or one the
// generator has no record of, is the author's and is left with a warning.
func (g *Generator) retireSkeletonFiles(storedHashes map[string]string) {
	for _, rel := range retiredSkeletonFiles {
		path := filepath.Join(g.config.Path, rel)

		content, err := afero.ReadFile(g.props.FS, path)
		if err != nil {
			delete(storedHashes, rel)

			continue
		}

		if stored, ok := storedHashes[rel]; !ok || stored != calculateHash(content) {
			g.props.Logger.Warn(rel + " is no longer generated and has been edited, so it is left in place; remove it if nothing needs it")

			continue
		}

		if err := g.props.FS.Remove(path); err != nil {
			g.props.Logger.Warn("could not remove "+rel+", which is no longer generated", "error", err)

			continue
		}

		delete(storedHashes, rel)
		g.props.Logger.Info("removed " + rel + ", which is no longer generated")
	}
}
