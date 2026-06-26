package generator

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// migrateFlatDocsToDataxis converts a project on the legacy flat docs layout
// (docs/commands, docs/packages) to the Diátaxis layout. It moves existing
// command docs to docs/reference/cli and package docs to docs/explanation/
// components — preserving content and the leaf/parent split — removes the old
// flat trees, and stamps docs_layout: diataxis on the manifest. Invoked by
// `regenerate project --force`. Moving (not regenerating) preserves any
// hand-written content; subsequent regeneration sees the docs at their new paths
// and leaves them intact.
func (g *Generator) migrateFlatDocsToDataxis() error {
	manifestPath := ManifestPathFor(g.config.Path)

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return errors.Wrap(err, "migrate docs layout: load manifest")
	}

	g.props.Logger.Info("Migrating documentation to the Diátaxis layout...")

	if err := g.moveFlatDocTree(m, "commands", false); err != nil {
		return err
	}

	if err := g.moveFlatDocTree(m, "packages", true); err != nil {
		return err
	}

	for _, dir := range []string{"commands", "packages"} {
		if err := g.props.FS.RemoveAll(filepath.Join(g.config.Path, "docs", dir)); err != nil {
			return errors.Wrapf(err, "migrate docs layout: remove docs/%s", dir)
		}
	}

	m.Properties.DocsLayout = DocsLayoutDiataxis
	if err := g.writeManifestFile(manifestPath, *m); err != nil {
		return errors.Wrap(err, "migrate docs layout: stamp manifest")
	}

	return nil
}

// moveFlatDocTree relocates every `<root>/<path>/index.md` (and the tree's own
// `<root>/index.md`) to its Diátaxis destination. root is "commands" or
// "packages".
func (g *Generator) moveFlatDocTree(m *Manifest, root string, isPackage bool) error {
	src := filepath.Join(g.config.Path, "docs", root)
	if exists, _ := afero.DirExists(g.props.FS, src); !exists {
		return nil
	}

	return afero.Walk(g.props.FS, src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// docRel is the command/package path: "" for the tree's own index, else
		// "deploy", "a/run", "vcs/release", …
		docRel := filepath.Dir(rel)
		if docRel == "." {
			docRel = ""
		}

		dest := g.migratedDocDestination(m, docRel, isPackage)
		if err := g.props.FS.MkdirAll(filepath.Dir(dest), DefaultDirMode); err != nil {
			return err
		}

		g.props.Logger.Debugf("migrate doc: %s -> %s", path, dest)

		return g.props.FS.Rename(path, dest)
	})
}

// migratedDocDestination maps a flat doc (identified by its command/package
// relative path) to its absolute Diátaxis path, applying the leaf-flat /
// parent-subsection rule for commands.
func (g *Generator) migratedDocDestination(m *Manifest, docRel string, isPackage bool) string {
	if isPackage {
		if docRel == "" {
			return filepath.Join(g.config.Path, "docs", "explanation", "components", "index.md")
		}

		return filepath.Join(g.config.Path, "docs", "explanation", "components", docRel+".md")
	}

	cliBase := filepath.Join(g.config.Path, "docs", "reference", "cli")
	if docRel == "" {
		return filepath.Join(cliBase, "index.md")
	}

	parts := strings.Split(docRel, string(filepath.Separator))
	name := parts[len(parts)-1]
	parentParts := parts[:len(parts)-1]

	if cmd := findCommandAt(m.Commands, parentParts, name); cmd != nil && len(cmd.Commands) > 0 {
		return filepath.Join(cliBase, docRel, "index.md")
	}

	return filepath.Join(cliBase, docRel+".md")
}
