package generator

import (
	"go/token"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrPackageNameCollision is a new command whose package name a sibling
// already has: the parent would import both under one alias and not compile.
var ErrPackageNameCollision = errors.NewSentinel("gtb.generator.package_name_collision",
	"another command under the same parent has the same Go package name")

// commandPackageName is the Go package a command's code lives in. A name that
// is a Go keyword takes a "cmd" suffix, since `package import` cannot compile;
// the command's name, directory and constructor keep the name (spec 0216).
func commandPackageName(name string) string {
	pkg := strings.ReplaceAll(name, "-", "_")
	if token.IsKeyword(pkg) {
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

	pkg := commandPackageName(g.config.Name)

	for _, s := range siblings {
		if s.Name != g.config.Name && commandPackageName(s.Name) == pkg {
			return errors.WithHintf(
				errors.Wrapf(ErrPackageNameCollision, "%s and %s would both be package %s", g.config.Name, s.Name, pkg),
				"Choose a name for %s that doesn't reduce to %s.", g.config.Name, pkg)
		}
	}

	return nil
}
