package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrPackageNameCollision is a new command whose import name a sibling
// already has: the parent would import both under one alias and not compile.
var ErrPackageNameCollision = errors.NewSentinel("gtb.generator.package_name_collision",
	"another command under the same parent has the same Go import name")

// ErrVendorParent is a new command somewhere below one named vendor.
var ErrVendorParent = errors.NewSentinel("gtb.generator.vendor_parent",
	"Go cannot import a package below a directory named vendor")

const vendorDir = "vendor"

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
// and its optional imports (trustkeys for signing, forge for a forge feature)
// are included whether or not this project uses them. Fixing the set here,
// rather than reading the file, keeps generate and regenerate project choosing
// the same alias.
var parentNames = map[string]bool{
	"assets": true, "props": true, "opts": true, "cmd": true,
	"v": true, "l": true, "tool": true, "p": true, "err": true,
	"cobra": true, "setup": true, "version": true, "logger": true,
	"os": true, "embed": true, "afero": true, "base64": true, "external": true,
	"time": true, "trustkeys": true, "forge": true, "raw": true,
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

// declaredBeside is every package-level name the other files in file's package
// declare. An import bound to one of them does not compile. A file that does
// not parse is skipped.
func (g *Generator) declaredBeside(file string) map[string]bool {
	declared := make(map[string]bool)

	dir := filepath.Dir(file)

	entries, err := afero.ReadDir(g.props.FS, dir)
	if err != nil {
		return declared
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if path == file || filepath.Ext(path) != ".go" {
			continue
		}

		src, err := afero.ReadFile(g.props.FS, path)
		if err != nil {
			continue
		}

		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
		if err != nil || isExternalTest(path, f.Name.Name) {
			continue
		}

		for _, decl := range f.Decls {
			addDeclared(declared, decl)
		}
	}

	return declared
}

// isExternalTest reports a foo_test package, whose names are its own.
func isExternalTest(path, pkg string) bool {
	return strings.HasSuffix(path, "_test.go") && strings.HasSuffix(pkg, "_test")
}

func addDeclared(declared map[string]bool, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			declared[d.Name.Name] = true
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				declared[s.Name.Name] = true
			case *ast.ValueSpec:
				for _, name := range s.Names {
					declared[name.Name] = true
				}
			}
		}
	}
}

// checkVendorParent refuses a child of a command named vendor: Go will not
// import a package below a vendor directory.
func (g *Generator) checkVendorParent() error {
	if !slices.Contains(g.getParentPathParts(), vendorDir) {
		return nil
	}

	return errors.WithHintf(
		errors.Wrapf(ErrVendorParent, "%s", g.config.Name),
		"Put %s under a parent whose path has no command named vendor.", g.config.Name)
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
