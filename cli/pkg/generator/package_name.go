package generator

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrPackageNameCollision is a new command whose import name a sibling
// already has: the parent would import both under one alias and not compile.
var ErrPackageNameCollision = errors.NewSentinel("gtb.generator.package_name_collision",
	"another command under the same parent has the same Go import name")

// unimportablePackages are package names that compile but cannot be imported:
// main is a program, and go build skips every file in package documentation.
var unimportablePackages = map[string]bool{"main": true, "documentation": true}

// commandPackageName is the Go package a command's code lives in. A Go keyword
// or an unimportable name takes a "cmd" suffix; the command's name, directory
// and constructor keep the name (spec 0216).
func commandPackageName(name string) string {
	pkg := strings.ReplaceAll(name, "-", "_")
	if token.IsKeyword(pkg) || unimportablePackages[pkg] {
		return pkg + "cmd"
	}

	return pkg
}

// parentNames are the identifiers and imports a generated parent's cmd.go
// may already use, so an import under one of them would shadow or clash with
// it. The root's NewCmdRoot adds v, l, tool, p and err to a child's locals,
// and its optional imports (base64 for telemetry, external for external
// commands) are included whether or not this project uses them. Fixing the
// set here, rather than reading the file, keeps generate and regenerate
// project choosing the same alias.
var parentNames = map[string]bool{
	"assets": true, "props": true, "opts": true, "cmd": true,
	"v": true, "l": true, "tool": true, "p": true, "err": true,
	"cobra": true, "setup": true, "version": true, "logger": true,
	"os": true, "embed": true, "afero": true, "base64": true, "external": true,
}

// commandImportAlias is the name a parent imports a command's package under.
// A predeclared identifier or one of the parent's own names takes a "cmd"
// suffix, so the import cannot shadow what the parent file uses, and so does
// init, which Go lets only a func declare. The package keeps its name (#189).
func commandImportAlias(name string) string {
	pkg := commandPackageName(name)
	if pkg == "init" || parentNames[pkg] || types.Universe.Lookup(pkg) != nil {
		return pkg + "cmd"
	}

	return pkg
}

// checkSiblingPackage refuses a command whose package name a sibling in the
// manifest already has, such as my-cmd beside my_cmd, or typecmd beside type.
func (g *Generator) checkSiblingPackage() error {
	if exists, _ := afero.Exists(g.props.FS, ManifestPathFor(g.config.Path)); !exists {
		return nil
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	siblings := m.Commands

	if parts := g.getParentPathParts(); len(parts) > 0 {
		parent := findCommandAt(m.Commands, parts[:len(parts)-1], parts[len(parts)-1])
		if parent == nil {
			return nil
		}

		siblings = parent.Commands
	}

	alias := commandImportAlias(g.config.Name)

	for _, s := range siblings {
		if s.Name != g.config.Name && commandImportAlias(s.Name) == alias {
			return errors.WithHintf(
				errors.Wrapf(ErrPackageNameCollision, "%s and %s would both be imported as %s", g.config.Name, s.Name, alias),
				"Choose a name for %s that doesn't reduce to %s.", g.config.Name, alias)
		}
	}

	return nil
}
