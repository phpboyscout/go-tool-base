package generator

import (
	"path/filepath"
	"sort"

	"github.com/dave/jennifer/jen"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// syncSkeletonGoFiles brings the framework-owned Go files generation wrote
// beside the root to the current skeleton: the entry point, the version
// package and the generate directives. They carry no hash, so they cannot
// conflict, and until #84 nothing after generation rewrote them, so a project
// scaffolded by an older gtb kept an entry point the current root no longer
// compiled against. A rule outranks the write, as it does the root's.
func (g *Generator) syncSkeletonGoFiles(m Manifest) error {
	modulePath, err := g.getModuleName()
	if err != nil {
		return err
	}

	files := map[string]*jen.File{
		filepath.Join("cmd", m.Properties.Name, "main.go"): templates.SkeletonMain(modulePath),
		"internal/version/version.go":                      templates.SkeletonInternalVersion(),
		"pkg/cmd/root/generate.go":                         skeletonGenerateFile(calculateDisabledFeatures(m.Properties.Features)),
	}

	paths := make([]string, 0, len(files))
	for rel := range files {
		paths = append(paths, rel)
	}

	sort.Strings(paths)

	for _, rel := range paths {
		if !g.managedGeneratedFile(rel) {
			continue
		}

		if err := g.writeGeneratedGoFile(rel, files[rel]); err != nil {
			return err
		}
	}

	return nil
}

// managedGeneratedFile reports whether a hashless generated file may be
// written: `sealed` is "never written, wiring included", a plain rule is
// "leave it alone" (#32). Either refusal is recorded so the run's summary is
// true.
func (g *Generator) managedGeneratedFile(rel string) bool {
	switch g.ignoreRules().State(rel) {
	case StateSealed:
		g.props.Logger.Warn("sealed, not written", "path", rel)
		g.conflicts.recordSealed(rel)

		return false
	case StateIgnored:
		g.props.Logger.Debug("ignored by .gtb/ignore, leaving untouched", "path", rel)
		g.conflicts.recordIgnored(rel)

		return false
	case StateManaged:
	}

	return true
}
